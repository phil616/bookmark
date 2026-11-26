import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  globalSetup: "./tests/setup.ts",
  // Tests share a temporary server; setup provisions a regular account.
  workers: 1,
  use: { baseURL: "http://127.0.0.1:18083", headless: true },
  webServer: {
    command: "go run ../cmd/server",
    env: {
      INITIAL_ADMIN_KEY: "browser-admin-key-123456789",
      DATA_DIR: "/tmp/online-bookmarks-e2e-" + Date.now(),
      LISTEN_ADDR: "127.0.0.1:18083",
      WEB_DIR: "dist",
    },
    url: "http://127.0.0.1:18083/healthz",
    reuseExistingServer: false,
  },
  timeout: 30000,
});
