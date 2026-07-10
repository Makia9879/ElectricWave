# 决定端点配置模型

Labels: wayfinder:grilling
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by: 0005-design-notification-endpoint-contract.md

## Question

用户如何在 App 内自定义通知端点？需要决定是本机生成 endpoint、用户填写外部 endpoint、还是两者都支持；并明确配置项、持久化方式、启停状态和测试发送体验。

## Resolution

> 2026-07-10 决策更新：个人 MVP 默认 provider 为 HTTP SSE。profile 不再需要 provider registration token；App 使用 receiver identity token 建立 `GET /api/v1/receivers/{receiver_id}/stream` 的认证 SSE 请求，并且只在用户显式开启接收时运行前台服务。MiPush/FCM 字段仅为后续扩展保留。

MVP 采用“用户配置自建服务器 profile，手机向该服务器受控注册 provider endpoint”的模型，不生成公开推送 URL，也不让手机直连外部 webhook。一个 profile 对应一个显式 `receiver_id`；MVP UI 先支持一个启用的 profile，但数据模型可保留多 profile 扩展空间。

profile 至少保存：`server_endpoint`、`receiver_id`、`receiver_identity_token`、`enabled`、`provider_type`、provider registration token、token 更新时间、最近注册时间和最近测试结果。三种标识保持独立：`receiver_id` 用于 webhook 寻址，`receiver_identity_token` 证明手机有权更新该 receiver，RegID/FCM token 只是下游投递 endpoint。

首次绑定流程：

1. 创建稳定的 `default` 与 `urgent` 通知渠道；Android 13+ 在测试通知前申请并检查 `POST_NOTIFICATIONS`。
2. 用户输入或扫码导入 HTTPS `server_endpoint`、`receiver_id` 和 `receiver_identity_token`；生产环境拒绝 HTTP，开发模式才可显式允许 localhost 或内网 HTTP。
3. App 初始化小米推送并获得 RegID；有 Google Play services 的环境可获得 FCM token 作为第二 provider，但 HyperOS MVP 的默认 provider 仍是小米推送通知栏消息。
4. App 调用 `POST /api/v1/receivers/{receiver_id}/endpoint`，在 `Authorization: Bearer <receiver_identity_token>` 下提交 provider、registration token、app instance ID 与最小诊断信息。服务器必须同时校验 receiver 存在、处于 allowlisted/enabled 状态、identity token hash 匹配、provider 被允许及 token 格式。
5. 注册成功后调用 `POST /api/v1/receivers/{receiver_id}/test`，同样使用 receiver identity token。验收只要求一条标准 Android 通知可见，不把焦点通知或超级岛作为绑定成功条件。

App 在首次绑定、启动或回到前台、provider token 刷新、切换 profile、收到 `re-register required`、清除数据或卸载重装后重新上报 endpoint。provider 明确表示 endpoint 无效时，服务器标记为 `needs_reregister`，不删除 receiver 白名单身份。

本地 `enabled=false` 暂停该 profile 的接收和测试；App 应同步请求服务端禁用 endpoint。删除 profile 时清除本地 token，并尽量请求服务端解绑或撤销 endpoint。服务器的 `enabled/revoked/allowlisted` 状态优先于 App 本地设置。

`receiver_identity_token` 必须使用 Android Keystore 保护的加密存储；RegID/FCM token 按敏感 endpoint 脱敏保存和记录。手机绝不保存小米 AppSecret、FCM service-account 或 webhook access token。
