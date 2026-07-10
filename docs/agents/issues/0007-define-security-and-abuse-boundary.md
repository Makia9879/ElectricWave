# 定义安全与滥用边界

Labels: wayfinder:grilling
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by: 0005-design-notification-endpoint-contract.md

## Question

MVP 对端点鉴权、防刷、隐私和本地数据保留的最低要求是什么？需要决定 token/API key、签名、来源限制、通知内容日志、敏感信息处理和失败重试边界。

## Resolution

MVP 的最低安全边界是四类相互独立的凭据：webhook access token（谁可发送）、`receiver_id` 加 allowlist/enabled 状态（能发给谁）、receiver identity token（手机是否可注册或更新该 receiver）、小米推送/FCM 服务端凭据（服务端如何调用下游）。不得把 RegID、FCM token、Bark `device_key` 或 URL path 作为发送权限。

所有生产接口只接受 HTTPS，token 只能通过 `Authorization: Bearer` 传输，禁止放入 URL、query、JSON body 或日志。服务端只存 token hash，比较使用常量时间；手机端把 receiver identity token 存入 Android 安全存储。下游服务端凭据只存在于服务器 secret 配置，绝不下发给 App 或 webhook 调用方。

服务端预置 receiver 白名单。注册或更新 `POST /api/v1/receivers/{receiver_id}/endpoint` 必须校验 identity token，且只能修改已绑定的 receiver；不提供公开自助注册。webhook 请求只能携带 `receiver_id`，服务器不能让调用方指定任意下游 token。

最小防刷措施：按 webhook token、来源 IP、receiver 及全局并发做可配置限流；限制 8 KiB 请求体和协议定义的字段长度；认证失败与 receiver 不可用采用统一节流，避免 receiver 枚举。命中限流返回 `429`，可附 `Retry-After`。

日志采用结构化最小化记录：request ID、调用方 token ID、receiver ID、provider 类型、状态码、错误分类、耗时以及 provider message ID 的 hash。不得记录 Authorization header、任何 token、RegID/FCM token 或完整正文；排障正文仅允许截断或 hash，且使用短留存期。错误响应不得泄露接收端或下游凭据细节。

幂等键只采用协议已定义的 JSON `idempotency_key`，作用域为 `webhook_token_id + receiver_id + idempotency_key`，保留 24 小时。重复请求不重复下发；同键不同核心内容返回 `409`。没有幂等键时允许生成 event ID，但调用方重试可能导致重复通知。

下游临时错误、超时和限流可在 `ttl_seconds` 内以指数退避短期重试；永久错误（例如 RegID/FCM token 无效）标记 endpoint 为 `needs_reregister`，不无限重试。服务端只承诺已接受并尝试投递，不承诺手机最终展示，因为通知权限、渠道、系统限制和用户设置均可能阻止显示。普通 webhook 不允许携带任意小米焦点通知或超级岛原始参数；只有服务端预配置且经审核的场景才可增强，否则降级为标准通知。
