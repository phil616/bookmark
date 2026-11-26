import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

// 前端在构建时读取 VITE_API_BASE_URL：留空表示与 API 同源（由反向代理转发），
// 填写后端公开地址则浏览器直接跨源调用 /api/v1 与 /mcp。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "VITE_");
  const api = env.VITE_API_BASE_URL || "";
  const devTarget = process.env.DEV_API_TARGET || "http://localhost:8080";
  return {
    plugins: [react()],
    server: {
      // 只有 API 地址留空时才走同源代理；分离开发时浏览器直连 VITE_API_BASE_URL。
      proxy: api ? undefined : { "/api": devTarget, "/mcp": devTarget },
    },
    // vite preview 仅用于本地查看构建产物（浏览器测试也用它托管 dist）。
    // 页面自身按 VITE_API_BASE_URL 直连 API，这里的代理只服务测试用的接口请求。
    preview: {
      proxy: {
        "/api": process.env.PREVIEW_API_TARGET || "http://127.0.0.1:18083",
        "/mcp": process.env.PREVIEW_API_TARGET || "http://127.0.0.1:18083",
      },
    },
  };
});
