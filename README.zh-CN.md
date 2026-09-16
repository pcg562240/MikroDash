# MikroDash 简体中文社区版

基于 [SecOps-7/MikroDash](https://github.com/SecOps-7/MikroDash) 的独立公开 fork，保留原项目 MIT 许可与署名。**非官方中文版**，当前基线为 **v0.8.55**，中文构建版本 **0.8.55-zh.1**。

中文界面在构建时生成；默认汉化构建的 RouterOS 通信、权限判断、数据库、备份与服务端程序使用对应版本的官方实现。**本仓库不包含真实路由器配置、账号密码或 NAS 数据，也不会自动操作你的路由器。**

## 可选：OpenClash 与设备累计流量扩展

第一期本地实现提供 OpenClash 实时曲线、日/月历史用量、ROS 设备累计排行及基础设施单列。统计服务、加密配置和数据卷独立于官方后端，汉化版默认镜像不包含此扩展。

详见 [流量扩展说明](extensions/traffic/README.md)。使用 `docker-compose.traffic.yml` 构建本地预览；当前扩展镜像尚未公开发布，也不会自动替换 NAS 实例。每设备“仅代理流量”精确归属不在第一期范围。

## 中文范围

- 登录、首次配置、导航、仪表盘、连接、网络、设置等主要页面的静态界面文案。
- 表单的名称、提示、确认操作与应用自带的资源字段说明。
- 本次扫描的 1,732 项中翻译 1,479 项，253 项为经审阅保留的技术名、单位、协议或示例。此数字只覆盖提取器识别的显示文案，**不代表全应用百分之百汉化**。
- RouterOS 原始日志、服务端错误、部分动态提示与插件文案可能仍为英文。
- 接口名、地址、路由规则、枚举提交值、脚本、备注与用户自行填写的名称原样保留；不会把用户数据翻译后写回路由器。
- 不调用在线翻译服务，不加载远程中文字体，不修改页面运行中的任意文本。

已完成的检查与手机界面示例见 [首版验证记录](docs/zh-CN/VALIDATION.md)。

## 先预览，不替换现有实例

需要 Git 与 Docker Compose。以下命令启动**全新空数据**的中文实例：

```sh
git clone --branch zh-CN https://github.com/pcg562240/MikroDash.git mikrodash-zh
cd mikrodash-zh
docker compose -f docker-compose.zh-CN.yml up -d --build
```

打开 <http://127.0.0.1:3082>。默认只接受这台机器本地访问，不暴露到公网。首次运行先设置仪表盘管理员；添加路由器时建议使用专用、最小权限账号，先以只读方式验证。

在 NAS 上预览时，将端口左侧 `127.0.0.1` 改为 NAS 的实际局域网 IP，并通过 NAS 防火墙仅允许可信局域网访问。建议通过现有 HTTPS 反向代理访问，不直接将管理界面发布到公网。

默认使用独立卷 `mikrodash-zh-preview`，不要改成生产实例正在使用的数据卷。停止预览：

```sh
docker compose -f docker-compose.zh-CN.yml down
```

不添加 `-v`，以保留预览数据。

### 预构建镜像

首版已通过完整检查并发布，已验证匿名拉取。镜像位于：

```text
ghcr.io/pcg562240/mikrodash-zh:0.8.55-zh.1
```

[首版发布记录](https://github.com/pcg562240/MikroDash/actions/runs/34937577886)。建议固定版本，而不是让自动更新工具直接追随 `latest`。镜像支持 `linux/amd64` 与 `linux/arm64`；其他架构尚未纳入中文发布流程。

## 如何跟随官方更新

1. `main` 保留 fork 起点的官方代码；**`zh-CN` 是中文维护分支与默认分支**。日常开发与发布使用 `zh-CN`，不要直接用网页“同步 fork”覆盖它。
2. GitHub Actions 每日检查官方最新稳定 release（预计北京时间 10:17，调度可能延迟），也可手动运行 **Check upstream releases**。
3. 有新版本时创建 `update/upstream-v…` 分支并合并上游，更新版本元数据和镜像基线，提交更新 PR。新增英文/失效例外会阻止测试通过；合并冲突会记录为 issue，交给维护者处理。
4. PR 必须经过翻译复核、回归测试与页面检查，**不自动合并**。
5. 维护者合并后，手动运行 **Publish Chinese image**，输入与基线匹配的中文版本，例如 `0.8.55-zh.2`。该工作流先跑完整检查，再构建并发布。
6. **不会自动更新 NAS 容器。** 生产切换需要单独备份、预览和确认。

GitHub 仓库需要启用 Actions，以及允许 Actions 创建 PR。fork 默认可能禁用工作流；长期没有活动时 GitHub 也可能暂停定时工作流。检查失败不等于已完成升级。详细操作见 [维护指南](docs/zh-CN/MAINTAINING.md)。

## 设计：翻译层与业务源码分开

```text
官方对应 release 源码 ──→ 隔离复制 web 源码 ──→ AST / HTML 文案转换 ──→ 官方前端构建器
                                                                       ↓
同版本官方 Docker 镜像（后端与运行环境不变） ←── 仅替换构建后的浏览器资源
```

- `localization/zh-CN.json`：英文原文 → 中文词典。
- `localization/lib.mjs`：只识别展示文案的 TypeScript AST / HTML 转换器。
- `localization/resource-adapter.ts`：仅翻译应用定义的表单显示元数据，保留数据和权限字段。
- `localization/english-baseline.json`：明确允许保持英文的项目，新增内容需要审阅。
- `localization/upstream.json`：官方版本、经过核对的源码提交与对应镜像。
- `localization/verify-upstream.mjs`：检查业务源码仍与官方基线完全一致。
- `.localization-build/`：临时生成目录，不提交到 Git。

兼容更新不是“永不冲突”：上游改变页面结构、表单协议或构建工具时仍需要维护。转换锚点移动、占位符丢失、未审阅文案等会使检查失败，而不是静默发布。

## 从现有英文版迁移或回滚

见 [NAS 迁移与回滚](docs/zh-CN/DEPLOYMENT.md)。不要让两台实例同时写同一个 `/data`；不要提交 `/data`、`.secret`、`.env`、私钥或配置导出到本仓库。

## 原项目

功能说明、RouterOS 权限要求与部署参数以对应版本的 [原始 README](README.md)、[变更记录](CHANGELOG.md) 为准。中文层不会减小路由器账号原有权限；界面可以执行的写操作仍需谨慎使用。
