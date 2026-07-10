# Makia通知器

`Makia通知器` 是一个个人自托管的 Android 通知通道。外部系统通过 HTTPS webhook 创建通知，服务端经由认证 HTTP SSE 将在线通知下发给 Android 前台服务，再由 App 发布标准系统通知。

```text
外部 webhook
  -> https://notice.example.com
  -> VPS Nginx
  -> 通知服务（127.0.0.1:<port>）
  -> HTTP SSE
  -> Android 前台服务
  -> 原生 Android 通知
```

## 当前进度

截至 2026-07-10，项目已完成架构、MVP 规格、风险调研和部署基础设施准备，尚未开始服务端与 Android 应用实现。

| 范围 | 状态 | 说明 |
| --- | --- | --- |
| MVP 架构与 API 规格 | 已完成 | webhook、认证、SSE、离线语义、权限和验收标准已确定。 |
| HyperOS/Android 能力调研 | 已完成 | MVP 基线是标准 Android 通知；超级岛与焦点通知不在个人 MVP 范围。 |
| Bark Server 复用评估 | 已完成 | 仅可参考 HTTP/存储/部署骨架，不能直接满足 Android SSE 下行模型。 |
| 本机 Android 环境 | 已完成 | Android SDK 35、JDK 17、Android Studio 和无线 ADB 真机均可用。 |
| 域名、Nginx、TLS | 已完成 | `notice.example.com` 已启用 Let's Encrypt；HTTP 跳转 HTTPS，自动续期已验证。 |
| Go 通知服务 | 待实现 | 包括 webhook、接收端白名单、鉴权、SSE、限流、幂等和审计。 |
| Android App | 待实现 | 包括 profile、Keystore、前台 SSE 服务、重连和原生通知渠道。 |
| 端到端部署与真机验收 | 待实现 | 服务实现后构建 `linux/amd64` 镜像并部署至 VPS。 |

当前访问 `https://notice.example.com/` 返回 `404` 属于预期状态，表示 TLS 入口就绪但通知服务尚未部署。

## 已确定的 MVP 边界

- 默认下行使用认证 HTTP SSE，不使用 WebSocket。
- SSE 服务端每 30 秒发送注释心跳；客户端在 EOF、网络错误或心跳超时后指数退避重连。
- Android 使用原生 `NotificationChannel`：`default` 用于普通通知，`urgent` 请求高优先级 heads-up。
- 顶部弹出和锁屏显示属于 Android 原生通知能力，但最终展示由系统、通知渠道、勿扰模式和用户设置决定，应用无法强制保证。
- 不依赖 MiPush、FCM、小米企业身份、超级岛能力或应用商店上架。
- 接收端离线时，bootstrap MVP 返回 `503 delivery_unavailable`；不做离线队列和补偿拉取。
- 服务端容器监听 `:<port>`，只发布为 `127.0.0.1:<port>:<port>`；公网只开放 `80/tcp` 和 `443/tcp`，不开放 `<port>/tcp`。

## 已准备环境

- App 名称：`Makia通知器`
- Android 包名：`com.example.notice`
- 公网地址：`https://notice.example.com`
- VPS：`ssh <ssh-user>@<vps-host> -p<ssh-port>`
- VPS 架构：`linux/amd64`，已有 host-network Nginx 容器
- 首个真机：Android 16 / HyperOS 3.0，已通过无线 ADB 连接
- Android 构建环境：执行 `source scripts/android-env.sh`，固定使用 JDK 17

真实 token、`.env`、证书、私钥和 Android 签名文件不得提交、打印到日志或放入 URL。

## 下一步

按以下顺序进入实现：

1. 初始化 Go 服务端，实现 webhook、认证、receiver allowlist、SSE、心跳、幂等、TTL、限流、最小审计和 `/healthz`。
2. 初始化 Android 工程，实现 profile 配置、Android Keystore、前台 SSE 服务、重连、通知权限及 `default`/`urgent` 通知渠道。
3. 为服务端编写 `linux/amd64` Docker 镜像与仅本地保存的运行配置。
4. 更新 VPS Nginx 的 HTTPS 虚拟主机，反代 webhook、SSE 与测试接口；SSE 关闭缓冲并设置至少 75 秒 `proxy_read_timeout`。
5. 部署到 VPS，并在已连接真机上完成 webhook 到系统通知的端到端验收。

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
