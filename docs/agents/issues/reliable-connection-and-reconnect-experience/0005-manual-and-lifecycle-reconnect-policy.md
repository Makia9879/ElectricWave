# 决定一键重连与应用活动状态自动重连策略

Labels: wayfinder:grilling
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map.md
Blocked by: 0003-android-hyperos-autostart-and-background-recovery.md

## Question

手机端在用户点击一键重连、App 冷启动、回到前台、进入后台、前台服务被重启、网络恢复、设备开机和 profile 启停时分别应执行什么重连动作？需要决定自动重连与手动重连的优先级、退避策略、失败提示、幂等行为，以及哪些场景必须停止重连。

## Resolution

重连策略以“用户显式启用接收 + 前台服务负责连接 + 生命周期事件触发检查”为核心。自动重连只在 profile enabled 且系统允许前台服务运行时发生；用户停止接收、强行停止、token 失效、receiver revoked、通知前台服务无法显示时必须停止或转为需要用户处理。

### 统一连接守卫

任何自动或手动重连前都执行同一组 guard：

- profile enabled 为 true。
- server endpoint、receiver_id、receiver identity token 存在且未标记 revoked。
- 前台服务可启动或已运行。
- 网络可用。
- 没有处于用户手动停止接收状态。
- 没有认证失败、receiver disabled/revoked、证书不可用等配置级阻断。

guard 失败时不重试连接，直接写入诊断状态并给 UI 提供下一步动作。

### 生命周期动作表

| 场景 | 动作 |
| --- | --- |
| 用户点击“开启接收” | 启动前台服务，立即建立 SSE，携带最近 ack/cursor，请求补发。 |
| 用户点击“一键重连” | 重置当前退避，取消等待中的 reconnect timer，立即尝试一次；失败后恢复指数退避。 |
| App 冷启动 | 刷新权限/渠道/系统诊断；若 profile enabled，确保前台服务运行并触发连接检查。 |
| App 回到前台 | 刷新状态；若 SSE 未连接或心跳过期，立即触发一次连接检查。 |
| App 进入后台 | 不主动断开；连接由前台服务继续维护。 |
| 前台服务 `onStartCommand` 重启 | 读取 profile 与最近 ack/cursor；guard 通过则连接，否则停止自身并写诊断。 |
| SSE EOF/心跳超时/HTTP 5xx/网络错误 | 标记“正在重连”，按指数退避重连。 |
| 网络恢复 | 若前台服务运行，立即尝试一次并重置退避。 |
| 设备开机或 App 更新 | 在系统允许的前提下尝试启动前台服务；HyperOS 自启动未授权时等待用户打开 App。 |
| 用户关闭 profile 或点击“停止接收” | 主动关闭 SSE，停止前台服务，同步服务端 receiver paused 状态；不再自动重连。 |
| 认证 401/403、receiver revoked/disabled | 停止自动重连，进入“不可用/重新绑定”。 |

### 退避策略

- 初始延迟 1 秒，随后 2、5、10、30、60 秒，最大 5 分钟，加 20% jitter。
- 用户点击“一键重连”、网络从不可用变可用、App 回到前台时，允许立即尝试一次，不受当前退避等待限制。
- HTTP 429 或服务端返回 retry-after 时尊重服务端延迟。
- 认证/配置错误不退避重连，直接停止。
- 服务端 backlog gap 不是重连错误；应更新诊断并继续拉取可补发范围内的消息。

### 幂等与 ack

客户端本地保存最高连续 ack event id。连接建立时带上 `Last-Event-ID` 和 `X-Receiver-Ack`；收到重复事件时按 `event_id` 去重。ack 只表示 App 已收到并持久化到本地处理队列，不等待系统通知展示成功。

### 必须停止重连的场景

- 用户关闭接收。
- receiver token 失效、receiver 被服务端禁用或撤销。
- endpoint 配置错误或 TLS 校验失败。
- 前台服务无法显示必要通知。
- App 检测到自身处于被系统/用户停止后的首次恢复诊断，但用户尚未重新开启接收。

这些场景进入 UI 的“需要授权”或“不可用”，不在后台消耗电量做无意义重试。
