# 汇总可靠连接与重连体验规格

Labels: wayfinder:task
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map.md
Blocked by: 0002-server-backlog-and-replay-semantics.md, 0003-android-hyperos-autostart-and-background-recovery.md, 0004-connection-status-ui-model.md, 0005-manual-and-lifecycle-reconnect-policy.md

## Question

把本地图已关闭票据的决策汇总成一份可交给实现 agent 的可靠连接与重连体验规格。需要覆盖服务端积压/补发、Android 自启动与恢复边界、连接状态 UX、一键重连、应用活动状态驱动的自动重连、日志诊断和验收矩阵。

## Resolution

可靠连接与重连体验规格收束为：服务端提供短期有界 backlog 和基于 ack/cursor 的补发；Android App 通过用户显式启用的前台服务维护 SSE；UI 把连接、展示能力和积压拆开呈现；自动重连只在系统和用户设置允许的范围内发生，不承诺绕过强行停止、HyperOS 自启动限制、省电策略、通知权限或渠道关闭。

### 服务端规格

- webhook 合法请求不再因 receiver 暂时离线而直接 `503`；进入 receiver backlog，返回 `202 queued`、`notification_id`、`expires_at` 和 backlog 概要。
- 服务端为每个 receiver 维护单调递增 `event_id`，SSE 使用 `id:` 字段。
- 客户端重连时提交 `Last-Event-ID` 和 `X-Receiver-Ack`；服务端以 ack 清理积压，以 cursor 决定补发起点。
- 状态最小集合：`accepted`、`queued`、`sent`、`acked`、`expired`、`dropped`。
- 默认 TTL 3600 秒，范围 60 到 86400；建议每 receiver 最多 1000 条或 24 小时内消息。
- 同一 receiver 内保证 event_id 顺序；不同 receiver 不保证全局顺序。
- 超 TTL、超容量或补发缺口必须记录诊断；出现缺口时向客户端发送 `backlog_gap` 控制事件。
- 服务端不记录完整 token 或完整通知正文；正文排障只允许截断或 hash。

### Android 连接规格

- 前台服务是唯一长期连接持有者；Activity 只负责配置、状态展示和触发动作。
- profile enabled 后，服务启动 SSE：`GET /api/v1/receivers/{receiver_id}/stream`，携带 receiver identity token、`Last-Event-ID`、`X-Receiver-Ack`。
- SSE 心跳超时、EOF、网络错误、HTTP 5xx 使用指数退避重连；认证/配置错误停止自动重连。
- 收到事件后先本地去重和持久化，再 ack；ack 不等待系统通知展示。
- App 回前台、网络恢复、用户点击一键重连可重置退避并立即尝试一次。
- 用户关闭 profile 必须停止 SSE 和前台服务，并同步服务端 receiver paused 状态。

### Android/HyperOS 边界

- 可自动恢复：App 前台/回前台、前台服务仍存活、网络恢复、SSE 心跳超时、服务端短暂不可用、设备开机且系统允许自启动与 foreground service 启动。
- 需要用户介入：通知权限拒绝、渠道关闭、HyperOS 自启动/后台运行/电池策略未放行、认证失效、receiver revoked、TLS/endpoint 配置错误。
- 不承诺自动恢复：用户强行停止 App、Android 13+ 前台服务任务管理器 Stop、HyperOS 安全中心清理、最近任务清理导致进程被杀、长期 Doze 或系统禁止后台运行。

### UX 规格

首页状态只展示：`接收中`、`正在重连`、`有待补发`、`需要授权`、`已暂停`、`不可用`。

详情页展示：

- SSE 状态、最近心跳、最近连接/断开、下一次重连时间。
- backlog 数量、最老积压时间、最近 ack event id。
- 通知权限、默认渠道、紧急渠道、前台服务通知状态。
- 自启动/后台运行/电池策略诊断；无法程序读取时显示“需要用户确认”。
- 最近错误分类和可执行下一步。

一键动作：

- `正在重连`：立即重连。
- `有待补发`：重连并补发。
- `需要授权`：打开对应权限或系统设置引导。
- `不可用`：检查配置/重新绑定。
- `已暂停`：开启接收。

禁用文案：`100% 可达`、`后台永久在线`、`已展示通知`、`强停后自动恢复`。

### 验收矩阵

服务端：

- receiver 在线、离线、断线重连、ack 清理、重复事件、backlog gap、TTL 过期、积压满、幂等冲突。
- 401/403/404/429/5xx 与 retry-after。
- 日志脱敏和正文 hash/截断。

Android：

- 首次开启接收、停止接收、一键重连、App 冷启动、回前台、进后台、前台服务重启、网络切换、飞行模式恢复。
- 通知权限允许/拒绝、渠道开启/关闭、前台服务通知可见性。
- SSE 重连携带 ack/cursor，重复事件不重复通知。
- 设备重启、App 更新、Doze 5/30/120 分钟、最近任务清理、强行停止、Android 13+ Stop。
- HyperOS 自启动关闭/开启、电池策略限制/无限制；每项必须记录系统设置前置条件。

实施切片：

1. 服务端 notification/backlog 数据模型、event_id、TTL、容量限制。
2. SSE stream 支持 `id:`、heartbeat、控制事件、`Last-Event-ID`、`X-Receiver-Ack`。
3. webhook 响应语义从离线 `503` 调整为短期 `202 queued`，并补 backlog full 错误。
4. Android 前台服务连接管理器、ack/cursor 本地持久化、去重。
5. 生命周期和网络回调驱动的重连策略。
6. 状态 UI 与诊断页。
7. 真机和服务端集成验收矩阵。
