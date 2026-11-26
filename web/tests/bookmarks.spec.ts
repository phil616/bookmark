import { test, expect } from "@playwright/test";
test("login, draft, create, archive and restore", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("APIKey").fill("browser-test-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "全部收藏" })).toBeVisible();
  await page.getByRole("button", { name: "新建收藏", exact: true }).click();
  const drawer = page.locator(".ant-drawer");
  await drawer
    .locator("textarea")
    .nth(0)
    .fill("https://example.com/" + Date.now());
  await drawer.locator("textarea").nth(1).fill("端到端测试收藏");
  await page.waitForTimeout(700);
  await drawer.locator(".ant-drawer-close").click();
  await page.getByText("本地草稿", { exact: true }).click();
  await page.getByRole("button", { name: "继续编辑" }).first().click();
  await drawer.getByRole("button", { name: "确认保存" }).click();
  await page.getByText("全部收藏", { exact: true }).first().click();
  const card = page
    .locator(".bookmark")
    .filter({ hasText: "端到端测试收藏" })
    .first();
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: /归\s*档/, exact: true }).click();
  await page.getByRole("button", { name: "确定" }).click();
  await expect(card).not.toBeVisible();
  await page.getByText("已归档", { exact: true }).first().click();
  const archived = page
    .locator(".bookmark")
    .filter({ hasText: "端到端测试收藏" })
    .first();
  await expect(archived).toBeVisible();
  await archived.getByRole("button", { name: /恢\s*复/, exact: true }).click();
  await page.getByRole("button", { name: "确定" }).click();
  await page.getByText("全部收藏", { exact: true }).first().click();
  await expect(
    page.locator(".bookmark").filter({ hasText: "端到端测试收藏" }).first(),
  ).toBeVisible();
  await page.screenshot({
    animations: "disabled",
    path: "test-results/bookmarks-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(500);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
  await page.screenshot({
    animations: "disabled",
    path: "test-results/bookmarks-mobile.png",
    fullPage: true,
  });
});

test("visual layouts, view switch and mobile navigation", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/");
  await page.getByLabel("APIKey", { exact: true }).waitFor();
  await page.screenshot({
    animations: "disabled",
    path: "test-results/redesign-login.png",
  });
  await page.getByLabel("APIKey").fill("browser-test-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "全部收藏", exact: true }),
  ).toBeVisible();
  const examples = [
    [
      "https://developer.mozilla.org/zh-CN/docs/Web/CSS",
      "CSS 布局的另一种可能\n从 Grid 到 Flexbox，重新理解网页布局的基本原则与实践方法。",
      ["知识/前端开发"],
      0.95,
    ],
    [
      "https://react.dev/learn",
      "构建更好的 React 组件\n组件设计、状态管理与交互模式。记录值得在下一个项目中实践的思路。",
      ["知识/前端开发"],
      0.85,
    ],
    [
      "https://www.figma.com/community",
      "设计灵感与组件收藏\n从优秀作品中观察留白、字体和色彩，建立自己的设计参考资料库。",
      ["设计/灵感"],
      0.8,
    ],
    [
      "关于阅读的一点想法",
      "让阅读成为一种日常\n不急于读完每一本书，先记住那些真正带来改变的观点。",
      ["生活/阅读"],
      0.7,
    ],
    [
      "https://go.dev/doc/",
      "Go 开发文档与实践\n接口、并发与错误处理，回顾项目中经常用到的语言特性。",
      ["知识/编程"],
      0.65,
    ],
    [
      "周末计划：走一条没走过的路。",
      "在城市里寻找新的风景\n给周末留一点空白，带上相机和好奇心，随意探索。",
      ["生活/旅行"],
      0.5,
    ],
  ] as const;
  for (const [raw, description, tags, score] of examples) {
    const r = await page.request.post("/api/v1/bookmarks", {
      headers: { "X-Requested-With": "online-bookmarks" },
      data: { raw, description, tags, score, create_tags: true },
    });
    expect(r.ok()).toBeTruthy();
  }
  await page.reload();
  await expect(
    page.locator(".bookmark").filter({ hasText: "CSS 布局" }),
  ).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({
    animations: "disabled",
    path: "test-results/redesign-desktop.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "列表视图", exact: true }).click();
  await expect(page.locator(".list-view")).toBeVisible();
  await expect
    .poll(async () =>
      Math.round((await page.locator(".sidebar").boundingBox())!.width),
    )
    .toBe(248);
  await page.screenshot({
    animations: "disabled",
    path: "test-results/redesign-list.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "卡片视图", exact: true }).click();
  await page.getByRole("button", { name: "新建收藏", exact: true }).click();
  await expect(page.locator(".bookmark-editor")).toBeVisible();
  await page.waitForTimeout(400);
  await page.screenshot({
    animations: "disabled",
    path: "test-results/redesign-editor.png",
  });
  await page.locator(".ant-drawer-close").click();
  for (const width of [768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.waitForTimeout(400);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(width);
    await page.screenshot({
      animations: "disabled",
      path: `test-results/redesign-${width}.png`,
      fullPage: true,
    });
  }
  await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("menuitem", { name: "设置", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "设置", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".nav-scrim")).toHaveCount(0);
  await page.screenshot({
    animations: "disabled",
    path: "test-results/redesign-settings-mobile.png",
    fullPage: true,
  });
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
  expect(errors).toEqual([]);
});
