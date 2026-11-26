# online-bookmarks

多用户收藏管理应用。支持原文收藏、树形标签、链接分享、智能分析、JSON 导入导出及 MCP，提供桌面和移动端界面。

当前版本：**v1.0.0**。许可证：[MIT](LICENSE)，Copyright © 2026 phil616。

## 快速启动

需要 Docker 与 Docker Compose v2。

```bash
cp .env.example .env
# 修改 INITIAL_ADMIN_KEY，设置至少 16 字符的随机密钥
docker compose up -d --build --wait
```

打开 <http://localhost:8080>，使用 `admin` 和配置的密钥登录，在“用户管理”中创建普通账号。超级管理员只管理用户，普通账号使用收藏功能。初始化配置仅首次启动生效。

默认使用 SQLite，数据库及加密主密钥保存在 Docker 数据卷中。更新时保留数据卷；部署、备份与旧版迁移见[部署指南](docs/deployment.md)。

## 功能

- 用户管理：创建、编辑、停用、重置密钥和删除普通用户；用户数据相互隔离。
- 收藏管理：保存链接或文本，按标签、关键词和归档状态筛选；支持草稿、原文复制与卡片/列表视图。
- 标签树：创建、移动、重命名和删除子树，同步更新收藏关联。
- 链接分享：分享单条收藏或标签子树，接收者预检后导入；支持过期和撤销。
- 数据迁移：Base64 JSON 导入导出，重复内容按规则合并。
- 智能分析：配置兼容 Anthropic Messages 的模型，预览后应用解释和标签。
- MCP：普通用户通过 Bearer 密钥管理自己的收藏和标签。

## 文档

| 文档 | 内容 |
| --- | --- |
| [使用指南](docs/usage.md) | 账号、收藏、标签、分享、分析与导入导出 |
| [部署指南](docs/deployment.md) | Docker、PostgreSQL、配置、升级和备份 |
| [REST 与 MCP](docs/api.md) | 认证、接口与客户端接入 |
| [开发指南](docs/development.md) | 源码结构、本地开发和测试 |
| [分享设计](docs/sharing-design.md) | 分享边界、标签树合并及事务一致性 |
| [发布指南](docs/releasing.md) | 版本检查、打包和发布验证 |
| [更新记录](CHANGELOG.md) | 首版功能与兼容说明 |

## 本地开发

需要 Go 1.26.2 或更高的兼容版本、C 编译器和 Node.js 24。

```bash
make build
INITIAL_ADMIN_KEY=change-this-admin-key ./bin/online-bookmarks
# 另一个终端运行前端热更新：
npm --prefix web run dev
```

后端默认端口 `8080`，前端开发端口 `5173`。其他命令见 `make help`。

## 许可

项目代码采用 [MIT License](LICENSE)。第三方组件保留各自许可证，见[第三方声明](THIRD_PARTY_NOTICES.md)。
