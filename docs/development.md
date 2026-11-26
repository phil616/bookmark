# 开发指南

## 环境

- Go 1.26.2 或更高的兼容版本。
- C 编译器与开发库：SQLite 驱动使用 CGO。
- Node.js 24 和 npm。
- Make；文档检查与发布打包需要 Python 3。

## 构建与运行

```bash
make build
./bin/online-bookmarks --version
INITIAL_ADMIN_KEY=change-this-admin-key ./bin/online-bookmarks
```

`make build` 生成 API 二进制 `bin/online-bookmarks` 和静态前端 `web/dist`，两者分开部署。二进制只提供 `/api/v1`、`/mcp` 和 `/healthz`，其他路径返回 JSON 404；数据默认写入 `data`。程序不会自动读取 `.env`，JSON 配置使用 `CONFIG_FILE` 指定。

前端热更新：

```bash
npm --prefix web run dev
```

开发端口为 `5173`。`web/.env` 中 `VITE_API_BASE_URL` 非空时浏览器直连该地址，需要后端设置 `ALLOWED_ORIGINS=http://localhost:5173`；留空则 Vite 把 `/api` 和 `/mcp` 代理至 `localhost:8080`，属于同源开发。

## 验证

```bash
make check       # Go 测试、竞态检测、vet 和前端构建
make web-test    # 安装 Chromium 后运行浏览器测试
```

浏览器测试启动两个独立服务：`vite preview` 在 `127.0.0.1:18082` 托管构建产物，Go API 在 `127.0.0.1:18083`；前端按 `web/.env.e2e` 直连 API，使用自动生成的临时数据目录，不读取生产数据卷。测试覆盖用户隔离、分享、复制回退、草稿及 320–1440 像素响应式行为。

PostgreSQL 集成测试：

```bash
TEST_DATABASE_URL='postgres://user:password@localhost:5432/bookmarks_test?sslmode=disable' \
  go test ./internal/app -count=1
```

必须使用专用测试库，该测试会清空测试库中的用户、收藏、标签和会话等数据。

## 目录

```text
cmd/server/       程序入口
internal/app/     数据模型、迁移、认证、REST、MCP、分享和测试
web/              前端构建配置、环境变量示例与静态站点镜像
web/src/          React 界面与样式
web/public/       静态托管配置
web/tests/        Playwright 端到端测试
scripts/          版本检查与发布打包
docs/            使用、部署、开发和设计文档
```

`web/dist`、`web/node_modules`、浏览器测试结果、`bin` 和 `dist` 为生成内容，不提交。`.env`、数据库和主密钥不进入源码或发布包。

## 版本与 CI

应用版本定义在 `internal/app/version.go`；前端包、OpenAPI 文档和更新记录需保持一致，`make release` 会检查版本匹配。数据交换格式 `schema_version=1` 和接口前缀 `/api/v1` 有独立含义，不随发行版本机械修改。

CNB 默认流水线仅做 Go 测试、前端构建和 Docker 构建。生产部署模板单独放在文档示例中，避免未配置的仓库在第一次推送时触发部署。
