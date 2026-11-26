import { test, expect, type Page } from "@playwright/test";

async function login(page: Page) {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "登录", exact: true }),
  ).toBeVisible();
  await expect(page.locator("body")).not.toContainText(
    /好内容|值得被留下|WELCOME|YOUR PERSONAL|灵感|私人空间/,
  );
  await page
    .getByLabel("APIKey", { exact: true })
    .fill("browser-test-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "全部收藏", exact: true }),
  ).toBeVisible();
}

for (const mode of ["clipboard", "fallback", "denied"] as const) {
  test(`direct raw copy in cards and editor: ${mode}`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 740 });
    await page.addInitScript((mode) => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value:
          mode === "fallback"
            ? undefined
            : {
                writeText: async (text: string) => {
                  if (mode === "denied")
                    throw new DOMException("denied", "NotAllowedError");
                  (window as any).__rawCopy = text;
                },
              },
      });
      document.execCommand = () => {
        const data = new DataTransfer();
        const event = new ClipboardEvent("copy", {
          clipboardData: data,
          bubbles: true,
          cancelable: true,
        });
        document.dispatchEvent(event);
        (window as any).__rawCopy = event.defaultPrevented
          ? data.getData("text/plain")
          : (document.activeElement as HTMLTextAreaElement).value;
        return true;
      };
    }, mode);
    await login(page);
    const raw =
      "  复制测试\n中文与 emoji 😊\r\n" +
      "https://example.com/?q=原始参数&keep=1".repeat(12) +
      "\n  ";
    const name = "原文复制测试-" + mode;
    const response = await page.request.post("/api/v1/bookmarks", {
      headers: { "X-Requested-With": "online-bookmarks" },
      data: { raw: raw + mode, description: name, score: 1 },
    });
    expect(response.ok()).toBeTruthy();
    const bookmark = await response.json();
    try {
      await page.reload();
      const card = page.locator(".bookmark").filter({ hasText: name });
      await expect(card).toBeVisible();
      const button = card.getByRole("button", {
        name: "复制原文",
        exact: true,
      });
      await button.click();
      expect(await page.evaluate(() => (window as any).__rawCopy)).toBe(
        raw + mode,
      );
      const rect = (await button.boundingBox())!;
      expect(rect.x).toBeGreaterThanOrEqual(0);
      expect(rect.x + rect.width).toBeLessThanOrEqual(320);
      await card.getByRole("button", { name: "编辑收藏", exact: true }).click();
      const editor = page.locator(".bookmark-editor");
      const edited = "  编辑中的原始内容\n尚未保存 😊 \n";
      await editor.getByLabel("原始内容", { exact: true }).fill(edited);
      await editor
        .getByRole("button", { name: "复制原文", exact: true })
        .click();
      expect(await page.evaluate(() => (window as any).__rawCopy)).toBe(edited);
      if (mode === "clipboard")
        await page.screenshot({
          path: "test-results/functional-editor-320.png",
          fullPage: true,
          animations: "disabled",
        });
      // Copying an edit must neither save it nor alter the persisted record.
      expect(
        (
          await (
            await page.request.get("/api/v1/bookmarks/" + bookmark.id)
          ).json()
        ).raw,
      ).toBe(raw + mode);
    } finally {
      await page.request.delete("/api/v1/bookmarks/" + bookmark.id, {
        headers: { "X-Requested-With": "online-bookmarks" },
      });
    }
  });
}

test("manual copy is available when both browser copy methods fail", async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });
    document.execCommand = () => false;
  });
  await login(page);
  await page.getByRole("button", { name: "新建收藏", exact: true }).click();
  const raw = "手动复制\n完整原文  ";
  const drawer = page.locator(".bookmark-editor");
  await drawer.getByLabel("原始内容", { exact: true }).fill(raw);
  await drawer.getByRole("button", { name: "复制原文", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByLabel("待复制内容")).toHaveValue(raw);
  await expect(page.getByText("原文已复制", { exact: true })).toHaveCount(0);
});

test("functional screens fit narrow viewports and long data", async ({
  page,
}) => {
  test.setTimeout(60000);
  await login(page);
  const response = await page.request.post("/api/v1/bookmarks", {
    headers: { "X-Requested-With": "online-bookmarks" },
    data: {
      raw: "长文本".repeat(200),
      description: "长标题".repeat(80),
      tags: ["长标签".repeat(30) + "/子级"],
      create_tags: true,
      score: 0.99,
    },
  });
  expect(response.ok()).toBeTruthy();
  const bookmark = await response.json();
  try {
    for (const width of [320, 390, 640, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      for (const view of [
        "bookmarks",
        "tags",
        "shares",
        "settings",
        "transfer",
      ]) {
        await page.goto("/" + view);
        await expect(page.locator(".page-header h1")).toBeVisible();
        await expect(page.locator(".login")).toHaveCount(0);
        expect(
          await page.evaluate(() => document.documentElement.scrollWidth),
        ).toBeLessThanOrEqual(width);
        if (view === "bookmarks") {
          const copy = page
            .locator(".bookmark")
            .filter({ hasText: "长标题" })
            .getByRole("button", { name: "复制原文", exact: true });
          await expect(copy).toBeVisible();
          const bounds = (await copy.boundingBox())!;
          expect(bounds.x).toBeGreaterThanOrEqual(0);
          expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
        }
        if (["tags", "shares", "settings", "transfer"].includes(view))
          await expect(
            page.getByRole("button", { name: "新建收藏", exact: true }),
          ).toHaveCount(0);
      }
    }
  } finally {
    await page.request.delete("/api/v1/bookmarks/" + bookmark.id, {
      headers: { "X-Requested-With": "online-bookmarks" },
    });
  }
});

test("user management and share records reflow without horizontal scrolling", async ({
  page,
}) => {
  test.setTimeout(60000);
  await login(page);
  const tagPath = "移动端共享分类".repeat(10);
  const csrf = { "X-Requested-With": "online-bookmarks" };
  expect(
    (
      await page.request.post("/api/v1/tags", {
        headers: csrf,
        data: { path: tagPath },
      })
    ).ok(),
  ).toBeTruthy();
  const tags = await (await page.request.get("/api/v1/tags")).json();
  const tag = tags.find((t: any) => t.path === tagPath);
  const share = await (
    await page.request.post("/api/v1/shares", {
      headers: csrf,
      data: { kind: "tag", tag_id: tag.id },
    })
  ).json();
  let userID = "";
  try {
    for (const width of [320, 390, 640, 768]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto("/shares");
      const record =
        width < 640
          ? page.getByRole("article", { name: tagPath })
          : page.getByRole("row").filter({ hasText: tagPath });
      await expect(record).toBeVisible();
      await expect(
        record.getByRole("button", { name: "撤销", exact: true }),
      ).toBeVisible();
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth),
      ).toBeLessThanOrEqual(width);
      if (width === 320)
        await page.screenshot({
          path: "test-results/functional-shares-320.png",
          fullPage: true,
          animations: "disabled",
        });
    }
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await page
      .getByLabel("APIKey", { exact: true })
      .fill("browser-admin-key-123456789");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "用户管理", exact: true }),
    ).toBeVisible();
    const username = "mobile-user-" + Date.now() + "x".repeat(40);
    const created = await page.request.post("/api/v1/admin/users", {
      headers: csrf,
      data: { username, api_key: "long-user-key-" + Date.now() },
    });
    expect(created.ok()).toBeTruthy();
    userID = (await created.json()).id;
    await page.reload();
    for (const width of [320, 390, 639, 640, 768, 1024]) {
      await page.setViewportSize({ width, height: 900 });
      const record =
        width < 640
          ? page.getByRole("article", { name: username, exact: true })
          : page.getByRole("row").filter({ hasText: username });
      await expect(record).toBeVisible();
      const button = record.getByRole("button", { name: "编辑", exact: true });
      await button.click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByLabel("用户名", { exact: true })).toHaveValue(
        username,
      );
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth),
      ).toBeLessThanOrEqual(width);
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      await expect(dialog).toBeHidden();
    }
  } finally {
    await page.request.delete("/api/v1/shares/" + share.share.id, {
      headers: { Authorization: "Bearer browser-test-key-123456789" },
    });
    if (userID)
      await page.request.delete("/api/v1/admin/users/" + userID, {
        headers: { Authorization: "Bearer browser-admin-key-123456789" },
      });
  }
});
