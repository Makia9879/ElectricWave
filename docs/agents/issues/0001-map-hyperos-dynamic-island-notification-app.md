# 小米澎湃 OS 灵动岛通知应用 Wayfinder Map

Labels: wayfinder:map
Status: closed
Assignee:

## Destination

明确一个面向小米澎湃 OS 的原生 Android 通知应用 MVP：包括目标用户场景、端点通知协议、端点配置方式、Android 权限与后台运行模型、HyperOS 兼容性风险、MVP 功能边界，并产出可交给实现 agent 的规格与实施拆分。MVP 的必需展示基线是通知栏、横幅和锁屏通知；焦点通知 / 小米超级岛仅作为满足小米准入条件后的可选增强。

## Notes

- 先做规划与决策，不直接实现应用。
- 所有票据只解决一个可交付决策；每次推进最多关闭一个票据。
- 需要外部知识时优先查官方 Android 文档、小米/HyperOS 可获得资料，以及现有同类实现的公开证据。
- 需要用户偏好判断时使用 grilling，一次只问一个问题。
- 当前仓库使用本地 Markdown tracker，约定见 `docs/agents/issue-tracker.md`。

## Decisions so far

- [设计通知端点协议](0005-design-notification-endpoint-contract.md) — MVP 为带 Bearer token 的 `POST /api/v1/notifications` JSON webhook，显式单个 `receiver_id`、24 小时幂等和普通通知展示语义。
- [决定端点配置模型](0006-decide-endpoint-configuration-model.md) — App 配置服务器 profile，并以独立 receiver identity token 建立认证 SSE；绑定与测试以标准 Android 通知为准。
- [定义安全与滥用边界](0007-define-security-and-abuse-boundary.md) — 三类业务凭据和下游服务端凭据隔离，预置接收端白名单、HTTPS Bearer、限流、最小审计与 TTL 内重试是 MVP 基线。
- [汇总 MVP 规格与实施切片](0008-define-mvp-spec-and-implementation-slices.md) — 已形成可实现规格、服务端与 Android 分层切片、真机验证矩阵，以及超级岛受控增强的降级规则。
- [自举通知通道](../../architecture/self-hosted-bootstrap-channel.md) — 个人 MVP 的默认下行改为 Android 前台服务维持的认证 HTTP SSE；不依赖小米企业开发者身份，接受常驻通知和 Android 后台限制。
- MVP 用户场景采用静态三段架构：`webhook -> 服务器 -> 手机`。服务器侧预先配置 webhook 访问令牌和接收端白名单；手机 App 预先配置通知发布点和接收端身份令牌；外部系统只有持有 webhook 访问令牌才能发送通知。详见 `0002-define-mvp-user-scenarios.md`。
- [调研 Android 通知与后台运行模型](0004-research-android-notification-and-background-model.md) — MVP 下行主链路采用小米推送通知栏消息，FCM 作为有 GMS 设备的第二提供方；Bark/APNs 不能直接服务 Android，自建长连接/轮询/UnifiedPush 不作默认主链路。
- [评估 Bark Server 复用方案](0009-evaluate-bark-server-reuse.md) — 可复用 HTTP、存储和部署骨架，但 Android/HyperOS 下游投递与受控接收端模型都需重做，不能直接采用现成镜像。
- [调研 HyperOS 灵动岛能力边界](0003-research-hyperos-dynamic-island-capability.md) — 小米已公开受控的焦点通知 / 超级岛接入；MVP 以标准 Android 通知为基线，仅将已审核的持续服务场景增强为岛通知。

## Not yet specified

MVP 规格路径已清晰；UI 视觉细节、账号体系、多设备和替代传输模式属于后续独立规划，不再阻塞本地图目的地。

## Out of scope

- 直接开发完整生产应用。
- 上架应用商店、商业化、支付、账号体系。
- 非小米/非 HyperOS 平台的完整适配；最多记录兼容性影响。
