import { test, expect, type Page } from "@playwright/test";

async function layoutFrames(page: Page, selectors: string[], frames = 24) {
  return page.evaluate(
    async ({ selectors, frames }) => {
      const samples: number[][] = [];
      for (let i = 0; i < frames; i++) {
        await new Promise(requestAnimationFrame);
        samples.push(
          selectors.flatMap((selector) => {
            const rect = document
              .querySelector(selector)!
              .getBoundingClientRect();
            return [rect.x, rect.y, rect.width, rect.height];
          }),
        );
      }
      return samples;
    },
    { selectors, frames },
  );
}
function expectStable(samples: number[][]) {
  for (let i = 0; i < samples[0].length; i++) {
    const values = samples.map((s) => s[i]);
    expect(Math.max(...values) - Math.min(...values)).toBeLessThan(0.25);
  }
}

test("responsive breakpoints and card hover do not oscillate", async ({
  page,
}) => {
  test.setTimeout(90000);
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto("/");
  await page.getByLabel("APIKey").fill("browser-test-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "全部收藏", exact: true }),
  ).toBeVisible();
  await page.request.post("/api/v1/bookmarks", {
    headers: { "X-Requested-With": "online-bookmarks" },
    data: {
      raw: "responsive-regression-" + Date.now(),
      description:
        "布局稳定性回归测试\n检查卡片边缘悬停，以及断点两侧的布局，确保尺寸不会自行振荡。",
      tags: [],
      score: 0.99,
    },
  });
  for (let i = 0; i < 5; i++)
    await page.request.post("/api/v1/bookmarks", {
      headers: { "X-Requested-With": "online-bookmarks" },
      data: {
        raw: "responsive-menu-" + Date.now() + "-" + i,
        description:
          "右侧菜单测试 " +
          i +
          "\n用于复现中等设备上点击三个点后右侧元素抖动的情况。",
        score: 0.9,
      },
    });
  await page.reload();
  await page.locator(".bookmark").first().waitFor();
  await page.evaluate(() => document.fonts.ready);
  // The former translateY(-2px) hover effect repeatedly moved this edge
  // out from under the pointer, toggling :hover without user interaction.
  const card = page.locator(".bookmark").first();
  let rect = (await card.boundingBox())!;
  const originalY = rect.y;
  await page.mouse.move(rect.x + rect.width / 2, rect.y + rect.height - 1);
  const hovered = await layoutFrames(page, [".bookmark"], 60);
  expectStable(hovered);
  expect(hovered.every((s) => Math.abs(s[1] - originalY) < 0.25)).toBeTruthy();
  await page.mouse.move(0, 0);
  for (const view of ["卡片视图", "列表视图"]) {
    await page.getByRole("button", { name: view, exact: true }).click();
    await page.mouse.move(0, 0);
    for (const width of [
      320, 360, 390, 600, 639, 640, 768, 991, 992, 1024, 1199, 1200, 1440,
    ]) {
      await page.setViewportSize({ width, height: 900 });
      await page.waitForTimeout(400);
      expectStable(
        await layoutFrames(page, [
          ".sidebar",
          ".workspace",
          ".bookmark",
          ".toolbar",
        ]),
      );
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth),
      ).toBeLessThanOrEqual(width);
      expect(await page.locator(".sidebar").evaluate((el) => el.inert)).toBe(
        width < 992,
      );
    }
  }
  // Reproduce the reported trigger on the rightmost card, then move the
  // pointer into the portal menu and back. Its anchor must remain stable.
  for (const view of ["卡片视图", "列表视图"]) {
    await page.getByRole("button", { name: view, exact: true }).click();
    for (const width of [768, 991, 992, 1024, 1199]) {
      await page.setViewportSize({ width, height: 900 });
      await page.waitForTimeout(400);
      await page
        .locator(".bookmark")
        .nth(1)
        .getByRole("button", { name: "更多收藏操作" })
        .click();
      const menu = page.locator(".ant-dropdown:not(.ant-dropdown-hidden)");
      await expect(menu).toBeVisible();
      await page.waitForTimeout(300);
      for (const target of [
        menu.getByText("复制原文"),
        page
          .locator(".bookmark")
          .nth(1)
          .getByRole("button", { name: "更多收藏操作" }),
      ]) {
        await target.hover();
        await page.waitForTimeout(200);
        expectStable(
          await layoutFrames(
            page,
            [
              ".workspace",
              ".toolbar",
              ".bookmark:nth-child(2)",
              ".ant-dropdown:not(.ant-dropdown-hidden)",
            ],
            30,
          ),
        );
      }
      const menuRect = (await menu.boundingBox())!;
      expect(menuRect.x).toBeGreaterThanOrEqual(0);
      expect(menuRect.x + menuRect.width).toBeLessThanOrEqual(width);
      await page.locator(".page-header h1").click();
      await expect(menu).toBeHidden();
    }
  }
  // Opening the mobile sidebar must not shrink or shift the content.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(400);
  const before = (await page.locator(".workspace").boundingBox())!;
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.waitForTimeout(400);
  expect(await page.locator(".workspace").boundingBox()).toEqual(before);
  await expect(page.locator(".sidebar")).not.toHaveAttribute("inert");
  const scrim = page.getByRole("button", { name: "关闭导航" });
  const scrimRect = (await scrim.boundingBox())!;
  await scrim.click({ position: { x: scrimRect.width - 10, y: 100 } });
  await page.waitForTimeout(400);
  await page
    .getByRole("button", { name: "新建收藏", exact: true })
    .first()
    .click();
  await page.waitForTimeout(400);
  for (const width of [768, 390, 1024]) {
    await page.setViewportSize({ width, height: 844 });
    await page.waitForTimeout(300);
    const drawer = await page
      .locator(".ant-drawer-content-wrapper")
      .boundingBox();
    expect(drawer!.width).toBeLessThanOrEqual(Math.min(width, 640));
    expect(drawer!.x).toBeGreaterThanOrEqual(0);
    expectStable(await layoutFrames(page, [".ant-drawer-content-wrapper"]));
  }
});
