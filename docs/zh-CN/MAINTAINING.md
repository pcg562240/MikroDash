# 中文版维护与上游同步

## 分支和来源

- `origin`：`pcg562240/MikroDash`，开发分支 `zh-CN`。
- `upstream`：`SecOps-7/MikroDash`，只从稳定 release 引入更新。
- `main` 不参与中文构建。同步流程将官方 release 合并到更新分支，再由 PR 合入 `zh-CN`。
- 官方源码保持原样，不手工编辑生成目录。中文适配集中在 `localization/`。

## 本地检查

需要 Node.js 24 或兼容版本、Go 1.25、Git。

```sh
npm ci --prefix web
npm ci --prefix localization
node localization/verify-upstream.mjs
node localization/cli.mjs check
npm test --prefix localization
npm run typecheck --prefix web
TZ=Europe/Berlin npm test --prefix web
TZ=Europe/Berlin go test ./...
node localization/cli.mjs prepare
web/node_modules/.bin/tsc --noEmit -p .localization-build/web/tsconfig.json
go run ./cmd/webbuild -dir .localization-build/web
node localization/finish.mjs
```

后端测试需要干净的源码目录：先测试，再生成隔离副本。上游路径核验会扫描目录，不识别中文构建目录内的测试副本；已有构建产物时，请在干净 worktree 测试，或先移走自己生成的目录。安装工具不应通过 npm 生命周期钩子自动生成副本。

`prepare` 要求输出目录不存在，防止误覆盖源码。重建时可使用新的、未存在的临时路径作为第三个参数，或先清理自己创建的旧构建目录。

`check` 的报告在 `.localization-report/`。这不是全项目所有字符串的穷举覆盖；抽取器采用保守规则，优先保护业务语义。

## 翻译约定

- 保留 `__MD_SLOT_0__` 等动态占位符，数量与编号必须相同，不引入 HTML。
- 不翻译 API 键、事件名、枚举值、路由器配置、用户输入和访问控制字段。
- 表单选项没有显式 `value` 时，其文本也是提交值，因此保持原文。
- 新英文先检查上下文。应翻译的加入 `zh-CN.json`；必须保持原样的才加入 `english-baseline.json`，禁止把全部缺失项直接加入例外来绕过检查。
- Go 服务端 schema 只用于提取应用自带的 label/title/help/placeholder，运行时适配器只修改这类显示属性，不翻译整个 API 响应。
- 修改词典后检查仪表盘、登录、连接表单、资源编辑确认框与手机窄屏。使用空白测试实例或虚构数据，不连接生产路由器进行 UI 自动化。

## 工作流

- **Chinese UI checks**：基线核验、文案检查、中文回归测试、原项目前后端测试、中文构建与类型检查。
- **Check upstream releases**：每天及手动触发。GitHub token 创建 PR 不会自动触发普通 PR CI，因此脚本会显式触发检查工作流。
- **Publish Chinese image**：只允许在 `zh-CN` 手动执行；先检查，后发布 `amd64` / `arm64` 镜像。不会发布到官方命名空间，不会连接 NAS。

首次启用需检查仓库 Settings → Actions。允许工作流创建 PR，并启用 fork 的定时工作流。可通过手动运行同步检查验证权限；没有新版本时只报告版本已一致。

遇到合并冲突时：阅读 issue 和运行日志，在更新分支人工合并；保留中文工作流、文案、适配层；核对官方 commit（带注释 tag 需要解析到 commit 而非 tag 对象）。禁止强推 `zh-CN` 来消除冲突。

更新后核对 `localization/upstream.json`、`Dockerfile.zh-CN` 与 Compose 的官方镜像版本一致。不得用旧前端搭配新后端。上游镜像标签可能被重新发布；需要更严格复现时，应同时锁定并核验多架构 manifest digest，随后同步调整更新脚本。

## 发布检查清单

- [ ] 上游业务源码无修改，前后端基线匹配。
- [ ] 全部检查通过，页面人工检查完成；记录仍为英文的范围。
- [ ] `README.zh-CN.md` 版本与计数按实测更新（不会自动改写文档中的历史数字）。
- [ ] 新中文版本号没有发布过；不要复用旧版本标签。
- [ ] 发布工作流成功；GHCR 包设为 public，并验证未登录用户也能拉取。
- [ ] 发行说明记录官方版本、已知限制和部署/回滚方式。
- [ ] 生产部署另行确认，不在 CI 中存储路由器或 NAS 密钥。

公开源码不等于容器包自动公开；GHCR 包可见性需在首次发布后核对。
