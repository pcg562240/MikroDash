# 流量扩展 0.8.55-traffic.1

发布日期：2026-09-16。独立社区扩展，官方业务源码基线保持 MikroDash v0.8.55。

## 正式镜像

| 用途 | 镜像 |
| --- | --- |
| 中文界面＋原官方后端 | `ghcr.io/pcg562240/mikrodash-traffic-dashboard:0.8.55-traffic.1` |
| 独立采集与统计网关 | `ghcr.io/pcg562240/mikrodash-traffic-collector:0.8.55-traffic.1` |

均已公开，支持 `linux/amd64`、`linux/arm64`；匿名 manifest 访问与部署主机拉取验证成功。纯汉化镜像 `mikrodash-zh:0.8.55-zh.1` 和其 `latest` 没有改动。

多架构 index digest：

```text
dashboard sha256:543ad2188129770a58e69cb8d02c798a367990e6589b386ad0bd692bdfcbf673
collector sha256:263474f8b491603d066cb68d49b49c4f11b59b4e48feb09dbfb2ab13fa9fb4f4
```

需要严格固定时，Compose 的 `image` 使用 `镜像名:版本@sha256:对应digest`。

- 构建源代码：`4afc47bc9389354a75299312376de3c5243432dc`。
- [发布工作流及完整 CI](https://github.com/pcg562240/MikroDash/actions/runs/35083101915)：全部通过。
- 官方 AMD64 后端 SHA-256：`147175e801ea3f3fe9a08ab3130e1b1d81a74065c46f94e5f639bd2e55cfcad8`，发布镜像与官方一致。

## 功能与边界

- OpenClash 实时上传／下载曲线、当前累计量与连接数。
- 今日、昨日、本月、自定义日期和 365 天保留期内的累计历史。
- ROS 设备下载／上传／合计排行，基础设施单列，独立持久化。
- 历史从开始采集后计入；首次已有计数、重置／断连的未知区间不伪造补算。
- OpenClash 统计包括核心 DIRECT，不是机场账单，不能与 WAN 或 ROS 设备量相加；本期不做每设备仅代理流量归属。

## 部署与核验

使用 [Portainer image-only 模板](../../docker-compose.traffic.release.yml)，先替换两个数据目录占位符。保留已有 MikroDash 数据，统计使用另一目录；不要同时运行两个写入相同 `/data` 的容器。用户单独确认后才执行部署，不包含自动更新生产实例。

已完成 AMD64 NAS 部署核验：原用户、连接配置、设置和加密密钥保持；原入口由统计网关承接，官方服务不再重复暴露端口；健康检查成功、匿名统计接口 401、真实来源计数持续更新、独立 SQLite 校验通过。实际设备和部署路径不进入本公开记录。ARM64 已完成镜像构建发布，未宣称该架构的 NAS 长期实机验收。

完整本地测试结果见 [本地验收记录](TRAFFIC-VALIDATION.md)。该记录保留发布前时间点的状态；当前发布状态以本页为准。

## 备份与回滚

切换前冷备原数据与原 Stack，保留原镜像。统计 `.key` 与数据库必须一起备份；暂停统计服务备份完整统计目录，或使用 SQLite 在线备份 API。

回滚时恢复旧 Stack／镜像和原访问入口，保留统计数据目录；同一官方后端版本通常不需要还原原数据。确需数据还原时先停本应用所有写入者，再保留故障副本并恢复已验证冷备。不要删除其他应用的容器、数据或网络。
