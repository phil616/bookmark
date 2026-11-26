import { request } from "@playwright/test";
export default async function setup() {
  const api = await request.newContext({ baseURL: "http://127.0.0.1:18083" });
  const r = await api.post("/api/v1/admin/users", {
    headers: { Authorization: "Bearer browser-admin-key-123456789" },
    data: {
      username: "browser-user",
      api_key: "browser-test-key-123456789",
      disabled: false,
    },
  });
  if (!r.ok()) throw new Error(await r.text());
  await api.dispose();
}
