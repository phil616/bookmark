# 发布指南

## v1.0.0 发布内容

当前首版以 `v1.0.0` 发布，包含源码包、当前 Linux 架构的原生运行包及 SHA-256 校验文件。用户数据、环境密钥、主密钥、依赖缓存和测试报告均不进入发布包。

项目代码采用 MIT，署名 phil616。许可文本位于根目录 `LICENSE`，第三方运行依赖的许可保留在 `docs/licenses/THIRD_PARTY_LICENSES.txt`。

## 发布前验证

```bash
make check
make web-test
```

CNB 配置可使用官方 Schema 进行静态校验；静态通过不代表已在 CNB 运行。默认流水线只测试和构建，不会自动发布或部署。

检查版本一致：

```bash
python3 scripts/release.py --check
```

应用版本、前端包与锁文件、OpenAPI 发行版本和 `CHANGELOG.md` 必须一致。数据格式版本与 API URL 前缀单独维护。`--version` 可在不初始化数据库、不配置密钥的情况下运行。

## 生成发行包

在 Linux 下安装 Go、C 编译器、Node.js 24、npm 和 Python 3，执行：

```bash
make release
```

脚本按锁文件安装前端依赖并收集运行依赖许可，构建本机 Linux API 运行包，检查文档链接，然后在 `dist/` 生成：

```text
online-bookmarks-v1.0.0-source.tar.gz
online-bookmarks-v1.0.0-linux-amd64.tar.gz  # 实际架构由 Go 宿主环境决定
SHA256SUMS
```

原生包包含 API 服务端、配置示例、许可、文档及 `BUILD_INFO.json`；前端产物不进入发布包，由部署方按目标地址单独构建。SQLite 使用 CGO，原生程序动态链接构建环境的 C 库，不是通用静态二进制；部署机器需提供兼容的 C 库。跨系统或跨架构部署优先使用 Docker，不声称已提供 Windows/macOS 二进制。

源码包通过明确的目录和文件清单收集，不通过整个工作目录递归打包。前端 API 地址由构建时的 `VITE_API_BASE_URL` 决定，与发布包内容无关。

脚本默认拒绝覆盖已有同名文件。重建本地尚未发布的包可执行 `python3 scripts/release.py --force`。已经正式发布的版本不要覆盖，修复使用新版本号。

## 验收

```bash
cd dist
sha256sum -c SHA256SUMS
tar -tzf online-bookmarks-v1.0.0-source.tar.gz
tar -tzf online-bookmarks-v1.0.0-linux-amd64.tar.gz
```

解压原生包，在包目录运行 `./online-bookmarks --version`，再以临时数据目录启动服务，检查 `/healthz`、登录页和管理员登录。解压源码包后应能按根目录 README 构建。上传前复核压缩包中没有 `.env`、数据库、`master.key`、`node_modules` 或测试报告。

## Docker

```bash
# API
docker build -t online-bookmarks:1.0.0 .
# 静态前端，API 地址在构建时注入
docker build -t online-bookmarks-web:1.0.0 \
  --build-arg VITE_API_BASE_URL=https://api.example.com ./web

docker run --rm online-bookmarks:1.0.0 --version
```

两个镜像分别部署，API 镜像只提供接口，前端镜像只提供静态文件。`docker compose up --build` 会同时构建并启动两者（默认前端 `8080`、API `8081`）。实际部署按[部署指南](deployment.md)配置持久化数据、来源白名单与密钥。

## 建立首版发布

当前打包流程只生成本地发行材料，不推送代码、标签、镜像或远端 Release。仓库托管配置完成后，将经过验收的提交标记为 `v1.0.0`，以 `CHANGELOG.md` 的首版条目作为发布说明，上传两个压缩包和 `SHA256SUMS`。

源码和附件必须对应同一提交。发布前需要确认远端仓库地址和发布权限；不要将本地生成校验和描述为签名或构建来源证明。
