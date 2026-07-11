# 0007 集成契约：可靠连接与重连（实现 agent 共享）

Labels: wayfinder:task
Status: active
Parent: 0006-reliable-reconnect-spec.md

本文件钉死服务端与 Android 端之间的线协议，作为两端并行实现的唯一事实来源。任何与
本契约冲突的实现以本契约为准。决策依据见 0002–0006。

## 1. event_id

- `event_id` 是**按 receiver 单调递增的正整数**（1, 2, 3, …），从 1 开始。
- 同一 receiver 内严格递增、连续分配；不同 receiver 互相独立，不保证全局顺序。
- 服务端在创建通知时，于同一事务内 `SELECT COALESCE(MAX(event_id),0)+1 FROM notifications WHERE receiver_id=?` 取下一个值（单连接 SQLite，串行安全）。
- 客户端把 `event_id` 当作 64 位整数处理。

## 2. SSE 事件线格式

所有事件块以单个空行结尾。`id:` 行是标准 SSE id 字段。

### 2.1 notification（通知）

```
id: 42
event: notification
data: {"type":"notification","notification_id":"ntf_...","event_id":42,"title":"...","body":"...","priority":"normal","group_key":"...","data":{...},"expires_at":"<RFC3339 UTC>"}

```

- `event_id` 同时出现在 `id:` 行和 JSON `event_id` 字段，二者必须相等。
- 其余字段沿用现有契约（`notification_id`、`title`、`body`、`priority`、`group_key`、`data`、`expires_at`）。`group_key` 为空时 JSON 可省略。

### 2.2 info（连接建立后首发控制事件）

流握手成功、补发游标确定后，立即且仅发一次：

```
event: info
data: {"type":"info","acked_event_id":<int|null>,"oldest_unacked_event_id":<int|null>,"newest_event_id":<int|null>,"backlog_count":<int>}

```

- `backlog_count`：当前 receiver 处于 `queued` 或 `sent` 状态、未过期、未 ack 的条数。
- 任一字段无值时为 `null`（如无任何积压）。

### 2.3 backlog_gap（补发缺口控制事件）

当客户端游标 `cursor` 之后存在不可补发的空洞时，在 info 之后、补发之前发一次：

```
event: backlog_gap
data: {"type":"backlog_gap","from_event_id":<int>,"to_event_id":<int>,"reason":"retention_exceeded|expired|dropped"}

```

- `from_event_id`..`to_event_id` 为缺口区间（闭区间，`from <= to`）。
- `reason` 取集合内值。

### 2.4 heartbeat（不变）

```
: heartbeat

```

## 3. 重连请求头

客户端每次连接（含首次）都发送：

- `Last-Event-ID: <int>`：客户端已**收到**的最高 event_id；首次连接为 0 或省略。
- `X-Receiver-Ack: <int>`：客户端已**本地持久化**的最高连续 event_id；首次连接为 0 或省略。

两者均为十进制整数。对本 MVP 客户端而言二者通常相等（先持久化再视为已 ack）。服务端
对二者分开处理：ack 用于清理，cursor 用于决定补发起点。

## 4. 服务端连接握手语义（handleStream）

1. 鉴权、receiver 存在性/允许性检查不变。
2. 解析 `Last-Event-ID`、`X-Receiver-Ack`（缺失或非正整数视为 0）。ack 必须幂等：旧 ack
   不报错。
3. **应用 ack**（单事务）：
   `UPDATE notifications SET status='acked', acked_at=now WHERE receiver_id=? AND event_id<=? AND status IN ('queued','sent')`。
4. **确定补发游标** `cursor`：
   - 有合法 `Last-Event-ID` → `cursor = Last-Event-ID`。
   - 否则 → `cursor = max(已应用 ack 值, 0)`。
5. **补发集合**（按 event_id 升序）：
   `SELECT ... WHERE receiver_id=? AND event_id>cursor AND status IN ('queued','sent') AND expires_at>now ORDER BY event_id ASC`。
6. **缺口检测**：令 `oldest = min(event_id where status IN ('queued','sent') AND expires_at>now)`。
   若 `cursor >= 1` 且 `oldest` 存在且 `cursor+1 < oldest` → 发 `backlog_gap {from=cursor+1, to=oldest-1}`。
   `reason` 统一记 `retention_exceeded`。
7. 先发 `info`，再发（可能的）`backlog_gap`，再按序补发补发集合，每条补发的同时
   `UPDATE status='sent'`（`first_sent_at` 置空时写入，`last_sent_at=now`，`attempt_count++`）。
8. 之后进入实时推送循环：`hub` 投递的新事件同样带 `id:` 并置 `sent`。
9. 心跳间隔不变。

## 5. webhook `POST /api/v1/notifications` 响应语义

鉴权、限流、校验、receiver 存在/允许检查、幂等 pre-check 全部保持现有顺序与语义。**唯一
改变第 11 步「离线即 503」**：

- **幂等重复**（同 key 同 hash）→ `200 {notification_id, status:"duplicate"}`（不变）。
- **幂等冲突**（同 key 不同 hash）→ `409 idempotency_conflict`（不变）。
- receiver 不存在 → `404 receiver_not_found`；不允许/禁用/撤销 → `403 receiver_not_allowed`（不变）。
- 创建通知（分配 event_id，初始 `accepted`）后：
  - **receiver 在线**（`hub.IsOnline`）且 `hub.Send` 成功 → 置 `sent`，返回
    `201 {notification_id, status:"accepted", event_id, expires_at}`。
  - **receiver 离线** 或 `hub.Send` 未投递成功 → 置 `queued`，返回
    `202 {notification_id, status:"queued", event_id, expires_at, backlog:{count}}`。
- **backlog 已满**（见 §6）→ `429`，`error.code = "backlog_full"`，带 `Retry-After`，不创建行。
  - 删除旧错误码 `delivery_unavailable`/`CodeDeliveryUnavailable` 的离线分支用法（保留常量
    定义无碍，但不再用于离线）。

`expires_at` 为 RFC3339 UTC。`backlog.count` 同 §2.2 `backlog_count`。

## 6. backlog 容量与 TTL

- **容量计数**只算 `status IN ('accepted','queued')` 且 `expires_at>now` 的条数（即「尚未投递
  出去」的积压）。一旦 `sent`（已投递给某次连接），即离开容量计数，但仍保留供补发直到
  `acked`/`expired`/`dropped`。这样在线 receiver 的实时流量不会撑爆容量。
- 每 receiver 容量上限默认 **1000**（env `BACKLOG_MAX_PER_RECEIVER`，范围 100–10000）。
- TTL 默认 3600s，范围 60–86400（沿用 domain 校验）。
- 24 小时硬保留上限：housekeeping 删除 `created_at < now-24h` 且 `status='acked'` 的行（仅在
  单连接 DB 下安全）。
- housekeeping 周期：把 `status IN ('accepted','queued','sent') AND expires_at<now` 置
  `expired`；以及上面的 24h 清理。

## 7. 通知状态机

字段集合：`status, accepted_at, first_sent_at, last_sent_at, acked_at, attempt_count,
last_error`。

```
accepted -> queued  (离线/backlog)
accepted -> sent    (在线直接投递)
queued   -> sent    (补发或在线重投)
sent     -> acked   (客户端 X-Receiver-Ack)
accepted/queued/sent -> expired  (TTL 到期)
accepted/queued/sent -> dropped  (保留语义；当前不主动产生，留作未来 backlog 淘汰)
```

`accepted_at` = 入库时间（复用现有 `created_at` 语义，新增 `accepted_at` 列与之一致）。

## 8. 数据模型迁移（SQLite，单连接）

在 `notifications` 表 `ALTER TABLE ADD COLUMN`（带默认值，幂等）：

| 列 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `event_id` | INTEGER NOT NULL | 0 | 旧行回填 0；新建走 §1 |
| `accepted_at` | TEXT NOT NULL | '' | 复用 created_at 值 |
| `first_sent_at` | TEXT NOT NULL | '' | |
| `last_sent_at` | TEXT NOT NULL | '' | |
| `acked_at` | TEXT NOT NULL | '' | |
| `attempt_count` | INTEGER NOT NULL | 0 | |
| `last_error` | TEXT NOT NULL | '' | |

新增索引：`CREATE INDEX IF NOT EXISTS idx_notif_replay ON notifications(receiver_id, event_id)`。
迁移须对已存在的库幂等（`ALTER TABLE ADD COLUMN` 若列已存在会报错，需先查
`PRAGMA table_info` 跳过已存在列）。

## 9. 日志与脱敏（不变 + 强化）

- 严禁记录：`Authorization` 头、receiver identity token、webhook access token、完整通知正文。
- 通知正文排障只允许：截断（title/body 各 ≤ 32 字符）或 content hash。
- 新增诊断日志须脱敏：event_id、status、receiver_id、count 可记录。

## 10. Android 端契约

### 10.1 SseClient
- 连接时发送 `Last-Event-ID` 与 `X-Receiver-Ack` 头（值来自持久化的最高已持久化 event_id）。
- 解析 `id:` 行 → `eventId: Long?`，注入 `NotificationEvent`。
- 新增回调：`onInfo(InfoEvent)`、`onBacklogGap(BacklogGapEvent)`；`info`/`backlog_gap` 不走
  `onEvent`。
- `Disconnect` 增加：`RetryAfter(val seconds: Long)`（HTTP 429 或带 `Retry-After` 时），
  `Transient` 仍用于 EOF/5xx/网络/心跳超时。

### 10.2 前台服务连接管理器
- 维护 `ackedEventId: Long`（持久化，0 表示无）。每收到一条 notification 事件：若
  `eventId <= ackedEventId` → 去重丢弃；否则先写入加密 inbox（带 event_id），再更新
  `ackedEventId = max(ackedEventId, eventId)` 并持久化，再发系统通知。
- `Last-Event-ID` 与 `X-Receiver-Ack` 均取持久化的 `ackedEventId`。
- 退避序列：**1, 2, 5, 10, 30, 60 秒，上限 300 秒**，+20% jitter。
- 立即重连触发（重置退避并立即试一次）：用户开启接收、一键重连、App 回前台、网络恢复、
  `onStartCommand`。
- 必须尊重服务端 `Retry-After` / 429（按 `RetryAfter.seconds` 等待，超过上限则截断）。
- 停止自动重连（进入不可用/需授权态）：401/403/404、receiver disabled/revoked、TLS/endpoint
  配置错误、前台服务必要通知不可展示。
- 用户关闭 profile：停 SSE + 停前台服务，并把本地状态置为「已暂停」。（服务端 receiver
  paused 暂不同步——MVP 单 receiver，profile 关闭即本地暂停。）

### 10.3 状态模型（UI）
首页展示且仅展示 6 态：`接收中`、`正在重连`、`有待补发`、`需要授权`、`已暂停`、`不可用`。
`UiStatus` 派生自 `RunState` + 诊断标志：

| UiStatus | 条件 | 主按钮 |
|---|---|---|
| 接收中 | SSE CONNECTED | （无/查看详情） |
| 正在重连 | CONNECTING 或 BACKOFF 且无积压 | 立即重连 |
| 有待补发 | 收到 backlog_gap 或 info.backlog_count>0 | 重连并补发 |
| 需要授权 | 通知权限拒绝/渠道关闭/前台通知不可见 | 去授权/设置 |
| 已暂停 | profile enabled=false | 开启接收 |
| 不可用 | AUTH_FAILED/NOT_FOUND/配置错误 | 检查配置/重新绑定 |

禁用文案：`100% 可达`、`后台永久在线`、`已展示通知`、`强停后自动恢复`。

### 10.4 诊断详情页字段
SSE 状态、最近心跳、最近连接、最近断开、下次重连时间；backlog 数量、最老积压时间、最近
ack event id；通知权限/默认渠道/紧急渠道/前台服务通知状态；自启动/后台/电池策略诊断（不可
程序读取时显示「需要用户确认」）；最近错误分类 + 可执行下一步。

## 11. 不在本次范围
- 不实现单独的 live-ack 通道（ack 仅在重连时通过 header 提交）。
- 不承诺绕过强行停止 / HyperOS 自启动 / 省电 / 通知权限 / 渠道关闭。
- 不为重连做高频后台轮询。
