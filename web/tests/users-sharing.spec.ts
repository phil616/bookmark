import { test, expect } from "@playwright/test";

test("administrator manages users without collection views", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByLabel("APIKey", { exact: true })
    .fill("browser-admin-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
  await expect(page.getByRole("button", { name: "新建收藏" })).toHaveCount(0);
  await expect(page.getByRole("menuitem", { name: "全部收藏" })).toHaveCount(0);
  expect((await page.request.get("/api/v1/bookmarks")).status()).toBe(403);
  await page.getByRole("button", { name: "新增用户" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("用户名", { exact: true }).fill("ui-created-user");
  await dialog
    .getByLabel("登录 APIKey（至少16字符）")
    .fill("ui-user-secret-123456789");
  await dialog.getByRole("button", { name: "确定", exact: true }).click();
  const row = page.getByRole("row").filter({ hasText: "ui-created-user" });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await dialog.getByLabel("用户名", { exact: true }).fill("ui-renamed-user");
  await dialog.getByRole("button", { name: "确定", exact: true }).click();
  const updated = page.getByRole("row").filter({ hasText: "ui-renamed-user" });
  await expect(updated).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
  await expect(dialog).not.toBeVisible();
  await page.screenshot({
    animations: "disabled",
    path: "test-results/user-management-mobile.png",
    fullPage: true,
  });
  const mobileUser = page.getByRole("article", {
    name: "ui-renamed-user",
    exact: true,
  });
  await expect(mobileUser).toBeVisible();
  await mobileUser.getByRole("button", { name: "删除", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "确定", exact: true })
    .click();
  await expect(mobileUser).toHaveCount(0);
});

test("share a subtree and import it from a deep link after login", async ({
  page,
  browser,
}) => {
  const headers = { "X-Requested-With": "online-bookmarks" };
  await page.goto("/");
  await page
    .getByLabel("APIKey", { exact: true })
    .fill("browser-test-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "全部收藏", exact: true }),
  ).toBeVisible();
  expect(
    (
      await page.request.post("/api/v1/bookmarks", {
        headers,
        data: {
          raw: "共享原文-" + Date.now(),
          description: "浏览器分享测试",
          tags: ["共享测试/编程/Go", "私人/不共享"],
          create_tags: true,
          score: 0.8,
        },
      })
    ).ok(),
  ).toBeTruthy();
  await page.reload();
  await page.getByRole("menuitem", { name: "管理分类" }).click();
  await page.locator(".content").getByText("共享测试", { exact: true }).click();
  await page.getByRole("button", { name: "分享此分类及子树" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  const url = await dialog.locator(".ant-typography").innerText();
  expect(url).toMatch(/\/share\/[a-f0-9]{64}/);
  const context = await browser.newContext();
  await context.addInitScript(() =>
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          (window as any).__sharedCopy = text;
        },
      },
    }),
  );
  const r = await context.request.post(
    "http://127.0.0.1:18083/api/v1/admin/users",
    {
      headers: { Authorization: "Bearer browser-admin-key-123456789" },
      data: {
        username: "browser-recipient",
        api_key: "browser-recipient-secret-1234",
      },
    },
  );
  expect(r.ok()).toBeTruthy();
  const receiver = await context.newPage();
  await receiver.goto(url.trim());
  await receiver
    .getByLabel("APIKey", { exact: true })
    .fill("browser-recipient-secret-1234");
  await receiver.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    receiver.getByRole("heading", { name: "收藏分享", exact: true }),
  ).toBeVisible();
  await expect(receiver.getByText("私人/不共享", { exact: true })).toHaveCount(
    0,
  );
  await receiver
    .locator(".shared-record")
    .getByRole("button", { name: "复制原文", exact: true })
    .click();
  expect(await receiver.evaluate(() => (window as any).__sharedCopy)).toBe(
    await receiver.locator(".shared-raw").innerText(),
  );
  await receiver.getByRole("button", { name: "预检合并" }).click();
  await expect(receiver.getByText(/新增 1 条/)).toBeVisible();
  await receiver.setViewportSize({ width: 390, height: 844 });
  expect(
    await receiver.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
  await expect(
    receiver.getByRole("button", { name: "确认导入" }),
  ).toBeEnabled();
  await receiver.screenshot({
    animations: "disabled",
    path: "test-results/shared-preview-mobile.png",
    fullPage: true,
  });
  await receiver.getByRole("button", { name: "确认导入" }).click();
  await expect(
    receiver.getByRole("heading", { name: "全部收藏", exact: true }),
  ).toBeVisible();
  await expect(
    receiver.locator(".bookmark").filter({ hasText: "浏览器分享测试" }),
  ).toBeVisible();
  const imported = await (
    await context.request.get("http://127.0.0.1:18083/api/v1/tags")
  ).json();
  expect(imported.map((t: any) => t.path)).toEqual([
    "共享测试",
    "共享测试/编程",
    "共享测试/编程/Go",
  ]);
  await context.close();
});

test("drafts and import buffers are isolated when switching accounts", async ({
  page,
}) => {
  const created = await page.request.post("/api/v1/admin/users", {
    headers: { Authorization: "Bearer browser-admin-key-123456789" },
    data: {
      username: "draft-recipient",
      api_key: "draft-recipient-secret-1234",
    },
  });
  expect(created.ok()).toBeTruthy();
  await page.goto("/");
  await page
    .getByLabel("APIKey", { exact: true })
    .fill("browser-test-key-123456789");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("button", { name: "新建收藏", exact: true }).click();
  await page
    .locator(".ant-drawer textarea")
    .first()
    .fill("这是属于第一个用户的草稿");
  await page.waitForTimeout(700);
  await page.locator(".ant-drawer-close").click();
  await page.getByRole("menuitem", { name: "导入 / 导出" }).click();
  await page.locator('input[type="file"]').setInputFiles({
    name: "private-import.json",
    mimeType: "application/json",
    buffer: Buffer.from(
      JSON.stringify({
        schema_version: 1,
        tags: [],
        bookmarks: [
          {
            raw_base64: "cHJpdmF0ZQ==",
            tags: [],
            description: "private-import-content",
            score: 0.5,
          },
        ],
      }),
    ),
  });
  await expect(page.getByText(/已选择\s+1\s+条记录/)).toBeVisible();
  await page.getByRole("button", { name: "退出登录" }).click();
  await page
    .getByLabel("APIKey", { exact: true })
    .fill("draft-recipient-secret-1234");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "数据迁移", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/已选择\s+1\s+条记录/)).toHaveCount(0);
  await page.getByRole("menuitem", { name: "本地草稿" }).click();
  await expect(page.getByText("这是属于第一个用户的草稿")).toHaveCount(0);
});
