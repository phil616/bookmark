import { defineConfig } from "@playwright/test";

// 前端与后端分离运行：18082 托管构建产物（vite preview），18083 是 Go API。
// 测试通过 `npm run build -- --mode e2e` 构建，产物按 web/.env.e2e 直连 API。
export default defineConfig({
  testDir: "./tests",
  globalSetup: "./tests/setup.ts",
  // Tests share a temporary server; setup provisions a regular account.
  workers: 1,
  use: { baseURL: "http://127.0.0.1:18082", headless: true },
  webServer: [
    {
      command: "go run ../cmd/server",
      env: {
        INITIAL_ADMIN_KEY: "browser-admin-key-123456789",
        DATA_DIR: "/tmp/online-bookmarks-e2e-" + Date.now(),
        LISTEN_ADDR: "127.0.0.1:18083",
        ALLOWED_ORIGINS: "http://127.0.0.1:18082",
      },
      url: "http://127.0.0.1:18083/healthz",
      reuseExistingServer: false,
      timeout: 180000,
    },
    {
      command:
        "npm run build -- --mode e2e && npx vite preview --host 127.0.0.1 --port 18082 --strictPort",
      url: "http://127.0.0.1:18082",
      reuseExistingServer: false,
      timeout: 180000,
    },
  ],
  timeout: 30000,
});
