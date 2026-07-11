# 调研 Android/HyperOS 自启动与后台恢复边界

Labels: wayfinder:research
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map.md
Blocked by:

## Question

Android 与 HyperOS/MIUI 对开机自启动、后台网络、前台服务恢复、最近任务划掉、强行停止、省电策略和网络切换的真实限制是什么？需要确认 App 可自动恢复 SSE 的边界、必须用户手动授权的系统设置，以及这些限制如何影响验收标准。

## Resolution

调研依据以 Android 官方文档为主，并把 HyperOS/MIUI 作为 Android 之上的 OEM 电池与自启动策略处理：没有公开证据表明普通第三方 App 能绕过用户手动限制、强行停止或厂商省电策略。结论是：SSE 接收链路只能在“用户已显式启用接收、前台服务可运行、系统未禁止后台/自启动、网络可用”的条件下自动恢复；其他情况必须暴露为需要用户操作的诊断状态。

### Android 官方边界

- Android 12+ 对后台启动前台服务有硬限制。目标 API 31+ 的 App 在后台不能随意启动 foreground service，除非命中特例；否则会抛 `ForegroundServiceStartNotAllowedException`。可用特例包括从用户可见状态切换、用户点击通知/小组件/Activity、收到 `BOOT_COMPLETED` 等开机广播、用户关闭电池优化等。
- Android 14+ 对从 `BOOT_COMPLETED` receiver 启动某些 foreground service type 继续加限制。因此“开机后自动恢复 SSE 前台服务”不能只靠声明开机广播，还必须在目标 SDK 和服务类型上做真机验证。
- Android 13+ 的前台服务任务管理器允许用户从通知抽屉停止正在运行前台服务的 App。用户点击 Stop 后，系统会移除整个 App 进程、清空 activity back stack、移除前台服务通知，且不会给 App 回调；下次启动后只能通过 `ApplicationExitInfo.REASON_USER_REQUESTED` 等信息诊断。
- Doze 会暂停网络访问、忽略 wake lock、推迟 JobScheduler/WorkManager 和普通 alarm。官方明确指出依赖持久网络连接接收实时消息会受影响，并建议能用 FCM 时使用 FCM。个人 MVP 选择自建 SSE，就必须接受 Doze 下实时性下降。
- Android 8+ 对隐式广播有后台限制，但 `ACTION_BOOT_COMPLETED` 和 `ACTION_LOCKED_BOOT_COMPLETED` 是例外，可用于开机后安排任务或尝试恢复。

### HyperOS/MIUI 风险

HyperOS/MIUI 通常提供自启动、后台运行、省电策略、锁屏清理等厂商级控制。公开、稳定、面向普通第三方 App 的标准 API 不足以保证这些开关默认放行。产品规格应按最保守边界写：

- 未获得用户在系统设置中的自启动/后台运行允许时，不承诺开机后自动恢复。
- 未关闭针对本 App 的严格省电限制时，不承诺长时间熄屏下 SSE 持续实时。
- 用户从最近任务清理、系统安全中心清理、前台服务任务管理器 Stop、强行停止 App 后，不承诺自动恢复；下次用户打开 App 时再诊断并重连。
- 不把“引导用户去打开自启动/无限制电池”包装成系统保证，只作为提高可靠性的可选操作。

### 可自动恢复的场景

- App 仍在前台或刚从后台回到前台：立即检查 profile enabled、通知权限、网络状态和 SSE 状态，必要时重连。
- 前台服务仍存活但 SSE EOF、心跳超时、TLS/HTTP 错误或网络切换：由服务内连接管理器指数退避重连。
- 网络从不可用变为可用，且前台服务仍运行：收到网络回调后重置退避并立即尝试一次。
- 设备重启后：若用户此前显式启用接收、系统允许 `BOOT_COMPLETED`、目标 SDK/服务类型允许启动，并且 HyperOS 自启动未拦截，可以尝试启动前台服务并重连；否则等待用户打开 App。
- App 更新后收到 `MY_PACKAGE_REPLACED`：可按与开机类似的保守策略恢复。

### 必须用户介入的场景

- 用户关闭 profile 或点击 App 内停止接收：必须停止前台服务并停止自动重连。
- 用户拒绝通知权限、关闭通知渠道、关闭前台服务通知可见性：可以继续连接，但必须显示“无法保证通知展示”的诊断；若系统不允许前台服务通知，则接收链路不可运行。
- 用户强行停止 App、使用 Android 13+ Task Manager Stop、或 HyperOS 安全中心/最近任务清理导致进程被杀：不承诺自动恢复；下次用户打开时显示原因和一键重连。
- HyperOS 自启动/后台运行/省电策略未授权：引导用户进入系统设置，授权前只承诺 App 前台或前台服务存活期间的连接。
- 长时间 Doze、无网络、服务端不可达、证书错误、receiver token 失效：不做无休止高频重试，进入退避和诊断状态。

### 验收影响

真机验收必须覆盖：首次启动、通知权限允许/拒绝、前台服务常驻通知、熄屏 5/30/120 分钟、Doze、网络切换、飞行模式恢复、服务端断开、设备重启、App 更新、最近任务划掉、Android 13+ 任务管理器 Stop、系统强行停止、HyperOS 自启动关闭/开启、电池策略限制/无限制。所有“自动恢复”验收都必须记录系统设置前置条件。

### 来源

- Android Developers: Restrictions on starting a foreground service from the background, updated 2026-06-24.
- Android Developers: Handle user-initiated stopping of apps running foreground services.
- Android Developers: Optimize for Doze and App Standby.
- Android Developers: Implicit broadcast exceptions.
