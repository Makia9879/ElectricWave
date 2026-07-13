# ElectricWave

`ElectricWave` 是一个个人自托管的 Android 通知通道。外部系统通过 HTTPS webhook 创建通知，服务端经由认证 HTTP SSE 将在线通知下发给 Android 前台服务，再由 App 发布标准系统通知。

```text
外部 webhook
  -> https://notice.example.com
  -> VPS Nginx
  -> 通知服务（127.0.0.1:8788）
  -> HTTP SSE
  -> Android 前台服务
  -> 原生 Android 通知
```

## 当前进度

服务端与 Android 客户端均已实现并部署上线：公网 webhook → VPS Nginx → 通知服务（认证 SSE）→ Android 前台服务 → 原生系统通知的端到端链路已在测试设备上验证通过。

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| MVP 架构与 API 规格 | 已完成 | webhook、认证、SSE、离线语义、权限和验收标准已确定。 |
| HyperOS/Android 能力调研 | 已完成 | MVP 基线是标准 Android 通知；超级岛与焦点通知不在个人 MVP 范围。 |
| Bark Server 复用评估 | 已完成 | 仅可参考 HTTP/存储/部署骨架，不能直接满足 Android SSE 下行模型。 |
| 本机 Android 环境 | 已完成 | Android SDK 35、JDK 17、Android Studio 和无线 ADB 真机均可用。 |
| 域名、Nginx、TLS | 已完成 | `notice.example.com` 已启用 Let's Encrypt；HTTP 跳转 HTTPS，自动续期已验证。 |
| Go 通知服务 | 已完成 | webhook、receiver 白名单/鉴权（hash+常量时间）、SSE（30s 心跳/新连接替换旧连接）、幂等(24h)、TTL、限流、审计、日志脱敏；纯 Go 持久化（modernc SQLite，无 CGO）。 |
| Android App | 已完成 | profile + Android Keystore 加密存储、default/urgent/foreground 通知渠道、前台 SSE 服务（指数退避/永久错误诊断）、normal→default/high→urgent 渠道映射、收件箱（列表页+详情页）。 |
| 端到端部署与真机验收 | 已完成 | `linux/amd64` 镜像部署至 VPS（仅回环发布 `127.0.0.1:8788`），Nginx 反代 SSE（`proxy_buffering off`），公网真机端到端验证通过。 |
| 可靠连接与重连体验 | 已实现并部署 | 服务端 backlog/补发/ack、SSE `id:`+`info`/`backlog_gap` 控制事件、webhook 离线 `202 queued`/`429 backlog_full`、Android ack/cursor 去重、退避+生命周期/网络重连、6 态 UI 与诊断页均已落地；服务端已部署上线，真机新 App 已连上线上服务端。 |

通知服务已部署：`https://notice.example.com/healthz` 返回 `200`；根路径返回 `404`（仅暴露既定 API，不泄露默认站点）。

## 已确定的 MVP 边界

- 默认下行使用认证 HTTP SSE，不使用 WebSocket。
- SSE 服务端每 30 秒发送注释心跳；客户端在 EOF、网络错误或心跳超时后指数退避重连。
- Android 使用原生 `NotificationChannel`：`default` 用于普通通知，`urgent` 请求高优先级 heads-up。
- 顶部弹出和锁屏显示属于 Android 原生通知能力，但最终展示由系统、通知渠道、勿扰模式和用户设置决定，应用无法强制保证。
- 不依赖 MiPush、FCM、小米企业身份、超级岛能力或应用商店上架。
- 接收端离线时 webhook 返回 `202 queued` 进入短期有界 backlog（每 receiver 最多 1000 条 / TTL 60–86400s），客户端重连后按 `event_id` 补发并以 `X-Receiver-Ack` 清理积压；ack 只表示 App 已收到并持久化，不表示系统通知已展示。
- 服务端容器监听 `:8788`，只发布为 `127.0.0.1:8788:8788`；公网只开放 `80/tcp` 和 `443/tcp`，不开放 `8788/tcp`。

## 已准备环境

- App 名称：`ElectricWave`
- Android 包名：`com.example.electricwave`
- 公网地址：`https://notice.example.com`
- VPS：`ssh <SSH_TARGET> -p <SSH_PORT>`
- VPS 架构：`linux/amd64`，已有 host-network Nginx 容器
- 测试设备：已通过无线 ADB 连接；具体型号和系统版本不写入文档
- Android 构建环境：执行 `source scripts/android-env.sh`，固定使用 JDK 17

真实 token、`.env`、证书、私钥和 Android 签名文件不得提交、打印到日志或放入 URL。

## 二期可靠连接与重连（交付状态）

二期已实现并部署，规格与集成契约见 [AGENT_HANDOFF.md](AGENT_HANDOFF.md) 与
`docs/agents/issues/reliable-connection-and-reconnect-experience/0006-reliable-reconnect-spec.md`、
`0007-integration-contract.md`。

已完成：

1. 服务端 notification/backlog 数据模型：`event_id`（按 receiver 单调递增）、状态机
   `accepted→queued→sent→acked`（及 `expired`/`dropped`）、TTL 与每 receiver 容量限制（默认 1000）。
2. SSE 支持 `id:`、`Last-Event-ID`、`X-Receiver-Ack`、heartbeat，以及 `info` / `backlog_gap` 控制事件。
3. webhook 离线语义从 `503 delivery_unavailable` 升级为短期 `202 queued`，并补 `429 backlog_full`。
4. Android 前台服务 ack/cursor 加密持久化、按 `event_id` 去重、补发处理。
5. Android 退避（1/2/5/10/30/60s，上限 300s，+20% jitter）、生命周期/网络恢复/一键重连触发、
   `Retry-After` 尊重，以及 6 态 UI（接收中/正在重连/有待补发/需要授权/已暂停/不可用）与诊断详情页。
6. VPS 重新部署：公网 `healthz` 200，旧库迁移成功，真机新 App 已连上线上服务端（POST 实时投递 201）。

待人工真机验收（需稳定无线 ADB 与系统设置前置条件，见 AGENT_HANDOFF 验收矩阵）：Doze 5/30/120 分钟、
最近任务清理、强行停止、Android 13+ 前台服务 Stop、HyperOS 自启动/电池策略等自动恢复边界；App 不会承诺绕过这些系统限制。

## 文档索引

- [MVP 规格与实施切片](docs/agents/issues/0008-define-mvp-spec-and-implementation-slices.md)
- [自托管 SSE 架构](docs/architecture/self-hosted-bootstrap-channel.md)
- [Cloudflare DNS、Nginx 与 HTTP 服务设计](docs/architecture/cloudflare-nginx-http-server.md)
- [TLS 部署清单](docs/deployment/notice-service-tls-runbook.md)
- [开发前置条件](docs/development-prerequisites.md)
- [Bark Server 适配评估](docs/research/bark-server-fit.md)
- [Android 通知与后台运行调研](docs/research/android-delivery-model.md)
- [HyperOS 能力调研](docs/research/hyperos-dynamic-island-capability.md)

## 实施约束

实现前先阅读上述 MVP 规格。所有改动须保留已有未提交文件；不要重置工作区，也不要将任何真实凭据加入版本控制。
