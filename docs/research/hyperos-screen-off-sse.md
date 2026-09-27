# HyperOS 锁屏后 SSE 中断

> 记录日期：2026-09-28
> 范围：Android 接收端在 HyperOS 锁屏后不再收通知、服务端出现积压，直到用户手动打开 App 才补发。
> 不记录设备型号、系统小版本、真实 token 或服务器凭据。

## 现象

接收开关已经打开，系统里的自启动、后台运行和电池相关权限也已放行。表现分两段：

1. 改动前：锁屏仍能收到通知。进程被系统杀掉之后，不会自己再起来，必须打开 App 才会恢复接收。
2. 第一次修复之后：情况变差。锁屏后一条通知都没有。服务端把消息记成积压（webhook `202 queued`，诊断页「有待补发」）。用户打开 App 后连接恢复，积压一次性补进来。

「打开 App 才收到」不是通知栏把锁屏通知藏起来。服务端在接收端离线时才会排队；在线实时投递是 `201`。积压说明锁屏期间 SSE 已经断了，不是渠道静音。

## 原因

接收链路是 App 自己维持的认证 HTTP SSE，不是系统推送。连接只活在 `NoticeForegroundService` 里。进程被冻住或服务被停掉之后，没有任何系统组件会替它收消息。

第一次修复选错了保活手段，HyperOS 在灭屏时把连接掐断：

- 前台服务类型从 `dataSync` 改成了 `specialUse`。这台系统对 `dataSync` 的长连接在锁屏后仍放行；`specialUse` 会被当成不认识的后台服务，灭屏后冻结网络。
- 服务在整个生命周期里持有 partial wake lock 和 Wi-Fi lock。HyperOS 把这种常驻唤醒视为异常耗电，锁屏后更容易冻结进程。CPU 被冻住时，OkHttp 的读超时不会触发，重连循环也不跑。
- 前台通知渠道被抬到 `IMPORTANCE_DEFAULT`。渠道创建后重要级别不能原地修改，这次改动还让系统把接收服务当成普通通知应用来省电。

所以服务进程可以还在，诊断上看起来也像开着，但服务端已经没有这条 SSE。用户打开 App 时，`ProcessLifecycleOwner` 的前台回调才会重连，于是表现为「必须手动打开才有通知」。

另外有一个独立的缺口，改动前就存在：`Application.onCreate` 不会在进程被拉起时恢复接收。开机广播只覆盖开机。`START_STICKY` 在 HyperOS 上不可靠。连接假死时，协程仍是 active，只调用 `ensure()` 不会拆掉已经死掉的 socket。

Android 15 起 `dataSync` 前台服务有大约 6 小时的超时。这是平台限制，不是这次锁屏故障的直接原因，但超时后如果没有重新拉起，结果同样是积压。

## 修复

仍使用 `dataSync`，不要再改成 `specialUse`，也不要为这条 SSE 持常驻唤醒锁。前台渠道保持 `IMPORTANCE_LOW`；若设备上已经建成更高重要级别，启动时删掉重建。

接收服务在这些时机恢复，不要求用户正打开界面：

- 进程启动且 profile 已启用、可连接：`NoticeForegroundService.ensure()`。
- 开机、厂商 quick boot、应用更新：`BootReceiver`。
- 灭屏：登记 60 秒一次的精确闹钟（未授权精确闹钟时退回 idle-aware 非精确闹钟）。
- 亮屏、用户解锁、闹钟触发：`watchdog`。心跳不新鲜或读循环已结束时，取消当前 Call 并重连。心跳仍新鲜则不动连接，避免把健康的 SSE 打掉。
- 系统对 `dataSync` 调用 `onTimeout`：先预约闹钟再 `stopSelf()`，由闹钟拉起，避免超时后一直停着。

相关代码：

- `android/app/src/main/AndroidManifest.xml`
- `android/app/src/main/kotlin/com/makia98/electricwave/NoticeApplication.kt`
- `android/app/src/main/kotlin/com/makia98/electricwave/service/NoticeForegroundService.kt`
- `android/app/src/main/kotlin/com/makia98/electricwave/service/ReceiverScheduler.kt`
- `android/app/src/main/kotlin/com/makia98/electricwave/service/KeepAliveReceiver.kt`
- `android/app/src/main/kotlin/com/makia98/electricwave/service/BootReceiver.kt`
- `android/app/src/main/kotlin/com/makia98/electricwave/notify/NoticeChannels.kt`

修复装上后，在灭屏状态下向线上 webhook 发探针，返回 `201 accepted`，说明当时 SSE 在线。常驻 `electricwave:sse` 唤醒锁已释放。前台服务类型为 `dataSync`（`types=0x1`）。

## 仍然做不到的事

系统设置里的「强行停止」，以及 HyperOS 的「结束运行」，会禁止应用自行再起。这两种停止之后必须用户手动打开一次。App 不能绕过这个限制。

通知栏里的「ElectricWave 正在接收通知」是前台服务通知，划掉或关掉该渠道会让系统停掉接收服务。业务通知渠道 `default` / `urgent` 不要和这条服务渠道混在一起关。

自启动、电池无限制、精确闹钟仍是 HyperOS 上的前置条件。代码只能在这些权限已经放行时把自己拉起来，不能代替用户在系统设置里授权。
