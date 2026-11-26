# REST 与 MCP

## REST

完整接口契约见 [OpenAPI 3.1](api/openapi.json)，API 前缀为 `/api/v1`。

浏览器登录后通过 HttpOnly 会话 Cookie 认证，写请求附带：

```http
X-Requested-With: online-bookmarks
```

程序客户端也可在每次请求中使用：

```http
Authorization: Bearer <APIKey>
```

密钥属于具体账号。普通用户只能访问自身数据，超级管理员只能使用会话和用户管理接口。Cookie 不适用于 MCP。

| 接口组 | 路径 | 身份 |
| --- | --- | --- |
| 登录、会话、退出 | `/auth/login`、`/auth/session`、`/auth/logout` | 账号 |
| 用户管理 | `/admin/users`、`/admin/users/{id}` | 超级管理员 |
| 收藏与批量操作 | `/bookmarks`、`/bookmarks/{id}`、`/bookmarks/batch` | 普通用户 |
| 标签树 | `/tags`、`/tags/{id}` | 普通用户 |
| 模型与分析 | `/settings/llm`、`/analysis` | 普通用户 |
| 导入导出 | `/exports`、`/imports/preview`、`/imports/commit` | 普通用户 |
| 分享 | `/shares`、`/shares/{token}`、`/shares/{token}/preview`、`/shares/{token}/commit` | 普通用户 |

修改收藏为完整替换，需先读取记录并提交当前 `version`。创建不存在的标签需显式允许。原文重复返回已有记录的 `existing_id`。分享撤销使用 `DELETE /shares/{id}`，其中 `id` 是分享 ID，预览 URL 中使用的则是随机令牌。

业务错误 JSON 包含 `code` 和 `message`。常见状态：400 参数错误、401 未认证、403 角色/来源/CSRF 拒绝、404 不存在或分享不可用、409 重复或版本冲突、429 认证限流。认证和来源拒绝可能为纯文本响应。

## MCP

端点：`http://localhost:8080/mcp`。传输方式为 Streamable HTTP，无状态；每个请求携带普通用户的 Bearer 密钥，无 OAuth。

```json
{
  "mcpServers": {
    "online-bookmarks": {
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer <APIKey>" }
    }
  }
}
```

| 工具 | 用途 |
| --- | --- |
| `bookmarks_list`、`bookmarks_get` | 查询和读取收藏 |
| `bookmarks_create`、`bookmarks_update`、`bookmarks_delete` | 创建、完整替换和永久删除 |
| `bookmarks_archive`、`bookmarks_restore` | 归档和恢复 |
| `tags_list`、`tags_create` | 读取分类树、创建路径 |
| `tags_rename`、`tags_move`、`tags_delete` | 修改或删除分类子树 |

调用方先读取 resource `bookmarks://analysis-spec/v1` 或 prompt `analyze_bookmark_v1`。MCP 不暴露模型凭据或抓取工具，也不调用本系统模型；调用方分析后显式创建标签、写入收藏。更新必须包含当前 `version`，建议先读取原记录。

## 数据格式

导入导出示例见 [export.json](examples/export.json)。文件导入与分享合并的评分规则不同，详见[使用指南](usage.md#导入导出)。分享的授权边界与合并算法见[分享设计](sharing-design.md)。
