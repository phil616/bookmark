# 第三方组件声明

online-bookmarks 自有代码使用 [MIT License](LICENSE)，Copyright © 2026 phil616。第三方组件不改变其原有许可证。

发行包和 Docker 镜像附带[第三方运行时许可全文](docs/licenses/THIRD_PARTY_LICENSES.txt)，涵盖前端运行依赖、服务端 Go 模块及 Go 标准库。Noto Sans SC 字体使用 SIL Open Font License 1.1，具体版权及条款包含在该文件中。

许可文本按 `go.mod`、`go.sum` 和 `web/package-lock.json` 对应的已安装依赖收集；更新依赖后执行：

```bash
npm --prefix web ci
python3 scripts/release.py --licenses
```

两个 npm 包未随安装包提供完整 LICENSE，补充文本保留在 `docs/licenses/upstream/`，来源如下：

- `@ant-design/icons-svg`：上游 [ant-design-icons LICENSE](https://github.com/ant-design/ant-design-icons/blob/master/LICENSE)。
- `is-mobile`：上游 [is-mobile LICENSE](https://github.com/juliangruber/is-mobile/blob/master/LICENSE)。

这些补充文本的适用包版本由打包脚本检查，升级时需同步核验。生成文件保留上游版权和许可原文，不使用项目的 phil616 署名替换第三方版权。
