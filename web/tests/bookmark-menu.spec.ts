import { test, expect } from "@playwright/test";
for (const touch of [false, true]) {
  test(`${touch ? "touch" : "mouse"} more menu opens, copies and confirms deletion`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      baseURL: "http://127.0.0.1:18082",
      viewport: { width: 1024, height: 900 },
      hasTouch: touch,
    });
    await context.addInitScript(() =>
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            (window as any).__copiedText = text;
          },
        },
      }),
    );
    const page = await context.newPage();
    try {
      await page.goto("/");
      await page.getByLabel("APIKey").fill("browser-test-key-123456789");
      await page.getByRole("button", { name: "登录", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "全部收藏", exact: true }),
      ).toBeVisible();
      const raw = `menu-test-${touch}-${Date.now()}\n原始内容与中文`;
      const response = await page.request.post("/api/v1/bookmarks", {
        headers: { "X-Requested-With": "online-bookmarks" },
        data: { raw, description: `菜单操作测试 ${touch}`, score: 1 },
      });
      expect(response.ok()).toBeTruthy();
      const bookmark = await response.json();
      await page.reload();
      const card = page
        .locator(".bookmark")
        .filter({ hasText: `菜单操作测试 ${touch}` });
      await expect(card).toBeVisible();
      const trigger = card.getByRole("button", { name: "更多收藏操作" });
      const click = async (locator: typeof trigger) =>
        touch ? locator.tap() : locator.click();
      await click(trigger);
      await expect(trigger).toHaveAttribute("aria-expanded", "true");
      const menu = page.locator(".bookmark-menu:not(.ant-dropdown-hidden)");
      await expect(menu).toBeVisible();
      await expect(menu).toHaveCSS("opacity", "1");
      await click(menu.getByRole("menuitem", { name: "复制原文" }));
      await expect(page.getByText("原文已复制", { exact: true })).toBeVisible();
      expect(await page.evaluate(() => (window as any).__copiedText)).toBe(raw);
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      // No silent failure when the secure-context Clipboard API is missing.
      await page.evaluate(() => {
        Object.defineProperty(navigator, "clipboard", {
          configurable: true,
          value: undefined,
        });
        document.execCommand = () => {
          (window as any).__fallbackText = (
            document.activeElement as HTMLTextAreaElement
          ).value;
          return true;
        };
      });
      await click(trigger);
      await click(menu.getByRole("menuitem", { name: "复制原文" }));
      expect(await page.evaluate(() => (window as any).__fallbackText)).toBe(
        raw,
      );
      await click(trigger);
      await click(menu.getByRole("menuitem", { name: "删除收藏" }));
      await expect(
        page.getByRole("dialog", { name: "永久删除所选收藏？" }),
      ).toBeVisible();
      await click(page.getByRole("button", { name: "取消", exact: true }));
      await expect(card).toBeVisible();
      await click(trigger);
      await click(menu.getByRole("menuitem", { name: "删除收藏" }));
      await click(page.getByRole("button", { name: "确定", exact: true }));
      await expect(card).toHaveCount(0);
      expect(
        (await page.request.get("/api/v1/bookmarks/" + bookmark.id)).status(),
      ).toBe(404);
    } finally {
      await context.close();
    }
  });
}
