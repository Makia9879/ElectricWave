# 决定服务端消息积压与重连补发语义

Labels: wayfinder:grilling
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map.md
Blocked by:

## Question

服务端在手机 SSE 断开时是否保留消息积压，并在重连后补发？需要决定消息状态机、ack/cursor 机制、TTL、最大积压、去重、顺序保证、投递失败响应，以及“已接受、已下发、已展示未知”之间的产品语义。

## Resolution

服务端进入“短期、有界、按 receiver 积压并在重连后补发”的模型。原 MVP 的“离线即 `503 delivery_unavailable`”过于简单，无法满足本话题要求的“消息积压，重连发送”；但也不应升级为无限可靠队列或承诺最终展示。新的语义是：webhook 创建的是一条有 TTL 的待投递通知，服务端负责在 TTL 和容量限制内尽力下发到已认证 receiver，客户端负责 ack 已收到的 SSE 事件；服务端不能知道系统通知是否最终展示。

### 产品语义

- `accepted`：服务端鉴权、校验、幂等、receiver allowlist 和容量检查通过，通知进入 receiver 的短期积压区。
- `queued`：receiver 当前没有可用 SSE 连接，或连接不可写；消息仍在 TTL 和积压容量内，等待重连。
- `sent`：服务端已把事件写入 SSE 连接，但尚未收到客户端 ack。这个状态只能表示“已下发到连接”，不表示 App 已发布系统通知。
- `acked`：客户端确认收到事件并持久化到本地处理队列，服务端可从积压区移除。仍不承诺系统通知可见，因为通知权限、渠道、锁屏、Doze、系统策略和用户设置可能阻止展示。
- `expired`：超过 TTL 未 ack，服务端丢弃并记录最后错误。
- `dropped`：超过 receiver 积压上限或内容不再可投递时丢弃；默认丢弃最旧的未 ack 普通优先级消息，紧急消息只在达到独立上限或 TTL 到期时丢弃。

### Webhook 响应变化

- 合法请求在 receiver 离线时也返回 `202 Accepted`，而不是 `503 delivery_unavailable`，响应体包含 `notification_id`、`status:"queued"`、`expires_at` 和当前 receiver backlog 概要。
- receiver 存在但被禁用、撤销或不在 allowlist 时仍返回 `403/404`，不能因为有队列而接受不可投递目标。
- receiver 积压已满且本次请求无法通过丢弃策略腾挪时，返回 `429 backlog_full` 或 `503 receiver_backlog_unavailable`；不要伪装为已接受。
- 幂等重复请求仍返回原 `notification_id`；同一幂等键但核心内容不同返回 `409 idempotency_conflict`。

### SSE 与 ack/cursor

SSE 事件必须带单调递增的 per-receiver `event_id`，并使用 SSE 标准 `id:` 字段。客户端重连时同时提交：

- `Last-Event-ID`：来自 SSE 协议或 App 本地记录的最后处理事件 ID。
- `X-Receiver-Ack: <event_id>`：客户端已持久化并进入本地通知处理队列的最高连续 event ID。

服务端以 ack 为准清理积压；`Last-Event-ID` 只作为断线补发游标。若客户端只带 `Last-Event-ID` 但未 ack，服务端可以从该 ID 之后补发，但不能清理之前未 ack 事件。客户端 ack 必须幂等，重复 ack 或旧 ack 不报错。

### TTL、容量与顺序

- 默认 `ttl_seconds` 沿用 webhook 字段，范围 60 到 86400，默认 3600。过期后不补发。
- MVP 建议每个 receiver 最多保留 1000 条或 24 小时内消息，二者先到为准；同时设置总存储上限，避免单 receiver 或恶意 token 撑爆服务端。
- 同一 receiver 内按 `event_id` 保持发送顺序；不同 receiver 之间不保证全局顺序。
- 相同 `webhook_token_id + receiver_id + idempotency_key` 在 24 小时内去重。
- 若补发窗口内存在缺口，服务端应发送 `backlog_gap` 控制事件，提示客户端刷新诊断状态；不能让 UI 显示“全部已补发”。

### 数据状态机

最小状态字段：

- `notification_id`
- `receiver_id`
- `event_id`
- `idempotency_key`
- `priority`
- `ttl_expires_at`
- `status`
- `accepted_at`
- `first_sent_at`
- `last_sent_at`
- `acked_at`
- `attempt_count`
- `last_error`

状态迁移：

```text
accepted -> queued -> sent -> acked
accepted -> sent -> acked
queued/sent -> expired
queued/sent -> dropped
```

`sent` 可因连接断开回到待补发视角，但不需要另建状态；以 `acked_at` 是否为空和 `ttl_expires_at` 是否过期判断是否还需重放。

### 约束

服务端只承诺“短期保留并尽力补发到 App 连接”，不承诺强行停止、系统禁止自启动、通知权限关闭、渠道关闭或长期离线后的最终送达。这个边界必须进入 UI 文案、诊断页和验收标准。
