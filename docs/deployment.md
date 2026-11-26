# 部署指南

## Docker 与 SQLite

在项目根目录执行：

```bash
cp .env.example .env
# 设置 INITIAL_ADMIN_KEY，至少 16 字符
docker compose up -d --build --wait
curl --fail http://localhost:8080/healthz
```

默认端口 `8080`，管理员名 `admin`。管理员只负责创建和管理普通用户。初始化用户名与密钥仅首次建库生效，修改配置不会重置数据库中的现有密钥。

应用以容器内 UID `10001` 运行。SQLite 数据库和加密主密钥保存在 `bookmarks-data` 数据卷；使用绑定目录时需允许该 UID 读写。

## 配置

程序支持环境变量，或通过 `CONFIG_FILE=/path/config.json` 读取 JSON，环境变量优先。Go 程序不会自动加载 `.env`；该文件由 Docker Compose 使用。示例见[环境变量](../.env.example)和[JSON 配置](../config.example.json)。

| 变量 | 默认值与用途 |
| --- | --- |
| `INITIAL_ADMIN_USERNAME` | 首次初始化管理员名，默认 `admin` |
| `INITIAL_ADMIN_KEY` | 首次初始化管理员密钥，至少 16 字符 |
| `DATABASE_URL` | 为空时使用 SQLite；非空时使用 PostgreSQL |
| `DATA_DIR` | 原生运行默认 `data`，Docker 为 `/data`；保存 SQLite 与 `master.key` |
| `LISTEN_ADDR` | 默认 `:8080`，容器内端口 |
| `WEB_DIR` | 原生运行默认 `web/dist`，Docker 为 `/app/web` |
| `ALLOWED_ORIGINS` | 独立前端的精确来源，逗号分隔；同源部署留空 |
| `COOKIE_SECURE` | 默认 `false`，HTTPS 部署设为 `true` |
| `CONFIG_FILE` | 可选的 JSON 配置文件路径 |
| `PORT` | 仅用于 Compose 的宿主机端口，默认 `8080` |
| `POSTGRES_PASSWORD` | 仅用于 Compose 的 PostgreSQL 容器密码 |

模型参数由各普通用户在网页设置中维护，加密存入数据库。APIKey 使用 Argon2id 哈希，会话仅存令牌摘要。

## PostgreSQL

在 `.env` 中设置密码和连接串：

```dotenv
POSTGRES_PASSWORD=replace-with-a-database-password
DATABASE_URL=postgres://bookmarks:replace-with-a-database-password@postgres:5432/bookmarks?sslmode=disable
```

```bash
docker compose --profile postgres up -d --wait postgres
docker compose up -d --build --wait app
```

远程数据库应按其配置启用 TLS。配置 PostgreSQL 后连接失败会退出，不会退回 SQLite。首次初始化前选定数据库；改变连接串不会自动搬迁现有数据。

## HTTPS 与独立前端

推荐反向代理到应用，由同一个来源提供网页和 API，并设置 `COOKIE_SECURE=true`。

如需独立部署前端，在构建时设置公开 API 地址：

```bash
npm --prefix web ci
VITE_API_BASE_URL=https://api.example.com npm --prefix web run build
```

将 `web/dist` 上传至静态托管服务，并配置 SPA 路由回退。在后端设置 `ALLOWED_ORIGINS=https://bookmarks.example.com`，浏览器发送带凭据请求。

前端和 API 应位于同站的 HTTPS 域名，例如上述两个子域。会话 Cookie 使用 `SameSite=Lax`，任意跨站前端不能依靠 CORS 获得 Cookie 会话。不要将服务端或模型密钥放入任何 `VITE_*` 变量。

应用已为 HTML 入口设置重新校验，为哈希静态资源设置长期缓存。反向代理或 CDN 不应覆盖入口的更新策略。端口可访问不等于部署正常，应检查 `/healthz` 和实际登录。

## 更新与备份

更新前备份数据库及 `master.key`，然后在新源码目录执行：

```bash
docker compose up -d --build --wait app
docker compose logs --tail 50 app
```

保留原数据卷，不使用 `down -v`。当前支持单个应用实例，数据库迁移按版本在启动时执行。

完整备份应停止应用后备份整个数据目录；SQLite 数据库和 WAL 必须配套保存。PostgreSQL 可使用数据库备份工具，但仍需单独备份 `DATA_DIR/master.key`。恢复时匹配原主密钥，否则原模型密钥无法解密，可清除模型配置后重填。登录密钥为不可逆哈希，不能从备份恢复明文。

收藏 JSON 适合个人数据迁移，不包含账号、会话、凭据或本地草稿，不能代替完整备份。

## 旧版迁移

从未发布的单用户版本升级时，在保留原数据和 `master.key` 的同时设置新的 `INITIAL_ADMIN_KEY`，且必须与旧个人密钥不同。

迁移创建超级管理员，将旧收藏、标签 ID、层级和模型配置转移至普通账号 `legacy`（管理员名恰为 `legacy` 时使用 `legacy-user`）。该普通账号继续使用旧个人密钥，所有旧浏览器会话作废。旧版无归属的浏览器草稿不会自动分配给账号。

迁移不会在重启时重复创建用户或覆盖凭据。旧 `INITIAL_API_KEY` 仅兼容全新数据库的管理员初始化，新的部署使用 `INITIAL_ADMIN_KEY`。

## CNB 部署示例

根目录 `.cnb.yml` 只进行测试和构建，不推送镜像、不连接生产服务器。原有生产部署配置移至[示例文件](examples/cnb.deploy.yml)，其中的仓库、域名、主机和密钥引用必须配置后再启用；示例不代表已部署。
