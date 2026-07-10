# 设计通知端点协议

Labels: wayfinder:grilling
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by: 0002-define-mvp-user-scenarios.md

## Question

通知端点应该如何定义请求协议？需要决定 HTTP 方法、路径、payload 字段、优先级/过期时间/分组/图标等元数据、错误响应，以及 MVP 是否只支持 JSON webhook。

## Resolution

MVP 通知端点采用一个保守、明确、可实现的 JSON webhook 协议：外部系统只向服务端提交单条通知，服务端只把通知投递给 body 中显式声明的单个 `receiver_id`。MVP 不支持批量发送，不支持任意上岛声明，不把动态岛/灵动岛展示能力作为服务端协议承诺。

### Endpoint

- Method: `POST`
- Path: `/api/v1/notifications`
- Content-Type: `application/json; charset=utf-8`
- Response Content-Type: `application/json; charset=utf-8`

### Authentication

请求方必须使用服务端配置的 webhook access token：

```http
Authorization: Bearer <webhook_access_token>
```

认证失败时返回 `401 unauthorized`。服务端还必须校验 `receiver_id` 是否在 receiver allowlist 中；不在 allowlist 中时返回 `403 receiver_not_allowed`。App 侧的 receiver identity token 用于 App 与服务端建立或维持接收端身份，不由 webhook 调用方传入，也不能被 webhook 调用方覆盖。

### Request Payload

```json
{
  "receiver_id": "phone-main",
  "idempotency_key": "order-123-paid-v1",
  "title": "订单已支付",
  "body": "订单 123 已完成支付",
  "priority": "normal",
  "ttl_seconds": 3600,
  "group_key": "orders",
  "icon": "default",
  "data": {
    "order_id": "123"
  }
}
```

字段定义：

- `receiver_id` required string: 目标接收端 ID。必须显式传入，MVP 只允许单个接收端，不接受数组。
- `idempotency_key` optional string: 幂等键。建议由调用方按业务事件生成；同一 `receiver_id` 下相同幂等键在保留窗口内只创建一次通知。
- `title` required string: 通知标题，1 到 80 个字符。
- `body` required string: 通知正文，1 到 500 个字符。
- `priority` optional enum: `low`、`normal`、`high`，默认 `normal`。
- `ttl_seconds` optional integer: 通知有效期，范围 60 到 86400，默认 3600。过期后服务端可以丢弃未投递通知。
- `group_key` optional string: 通知分组键，用于下游通知栏聚合；最大 64 个字符。MVP 不保证跨厂商一致的 UI 聚合效果。
- `icon` optional string: 预置图标键，默认 `default`。MVP 只接受服务端/App 已知图标键，不接受任意图片 URL。
- `data` optional object: 业务透传数据，只允许 JSON object，最大 4096 bytes。服务端不得把 `data` 中的字段解释为上岛、全屏、常驻或系统级展示指令。

整体请求体最大 8 KiB。未知字段默认拒绝并返回 `400 invalid_request`，避免调用方误以为未实现字段已经生效。

### Delivery Semantics

主链路为服务端调用小米推送向 Android 通知栏投递消息；FCM 是可选下游通道，只能作为设备或环境需要时的补充。服务端协议只表达“向某个 receiver 发送一条普通通知栏消息”，不承诺通知会进入动态岛、灵动岛、悬浮窗、锁屏常驻或其他厂商私有展示形态。

`priority` 只映射服务端调度和下游推送优先级：

- `low`: 可延迟投递，适合低时效提醒。
- `normal`: 默认优先级。
- `high`: 尽快投递，适合用户明确需要及时看到的提醒。

`ttl_seconds` 控制服务端和下游推送的最大有效期。若通知过期前未能投递，服务端返回给调用方的创建结果不需要回滚，但后续状态应标记为 expired 或 dropped。

### Idempotency

如果提供 `idempotency_key`，服务端以 `(receiver_id, idempotency_key)` 作为幂等范围。首次成功请求返回 `201 created`；后续完全相同语义的重复请求返回 `200 duplicate`，并返回原 `notification_id`。如果同一幂等键携带不同核心内容，返回 `409 idempotency_conflict`。

MVP 幂等保留窗口为 24 小时。未提供 `idempotency_key` 时，每次成功请求都创建新通知。

### Success Responses

创建成功：

```json
{
  "notification_id": "ntf_01JZ0000000000000000000000",
  "status": "accepted"
}
```

重复请求：

```json
{
  "notification_id": "ntf_01JZ0000000000000000000000",
  "status": "duplicate"
}
```

HTTP 状态码：

- `201 Created`: 新通知已被服务端接受。
- `200 OK`: 幂等重复请求，返回既有通知。

### Error Responses

所有错误响应使用统一 JSON 结构：

```json
{
  "error": {
    "code": "invalid_request",
    "message": "title is required"
  }
}
```

错误码：

- `400 invalid_request`: JSON 格式错误、缺少必填字段、字段类型错误、字段超过限制、未知字段、`receiver_id` 为数组或批量结构。
- `401 unauthorized`: 缺少或错误的 webhook access token。
- `403 receiver_not_allowed`: `receiver_id` 不在服务端 allowlist 中。
- `404 receiver_not_found`: `receiver_id` 语法合法但服务端不存在该接收端。
- `409 idempotency_conflict`: 同一 `receiver_id` 和 `idempotency_key` 下提交了不同核心内容。
- `413 payload_too_large`: 请求体超过 8 KiB。
- `429 rate_limited`: 请求频率超过服务端限制。
- `500 internal_error`: 服务端内部错误。
- `503 delivery_unavailable`: 服务端暂时无法接入下游推送通道。

错误响应不得回显 webhook access token、receiver identity token 或下游推送凭据。
