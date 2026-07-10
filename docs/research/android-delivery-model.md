# Android / HyperOS 手机接收链路调研

> 调研日期：2026-07-10  
> 对应：issue 0004  
> 范围：`自建服务器 -> Android / HyperOS 手机 -> 系统通知` 的后台送达模型；同时评估 Finb/bark-server 的可复用程度。

## 结论摘要

面向“主要运行在小米 HyperOS/MIUI 设备”的 MVP，推荐链路是：

```text
外部系统
  -> 带 webhook 访问令牌的 HTTPS 请求
  -> 自建服务器校验令牌、接收端白名单
  -> 小米推送服务（按 RegID 发送通知栏消息）
  -> HyperOS/MIUI 系统级通道
  -> 手机系统通知
```

原因：小米官方说明，MIUI 上的小米推送长连接由系统维护；“通知栏消息”由系统级通道下发，不要求应用驻留后台。相反，透传消息会受系统自启动管理影响，应用未启动时可能无法送达。[小米推送产品说明](https://dev.mi.com/console/doc/detail?pId=863)

建议同时把 FCM 设计成第二种 `DeliveryProvider`，用于已安装 Google Play 服务的国际版 HyperOS 或其他 Android 设备。FCM 高优先级消息可以在 Doze 中尝试立即送达并唤醒设备，但客户端要求设备具备 Google Play Store/兼容的 Google Play services，因此不能假定覆盖中国大陆版 HyperOS。[FCM Android 客户端要求](https://firebase.google.com/docs/cloud-messaging/android/client) [FCM 消息优先级](https://firebase.google.com/docs/cloud-messaging/android/message-priority)

不推荐把应用自建 WebSocket、轮询或自建 UnifiedPush 作为默认 MVP 主链路：它们不能绕过 Android/HyperOS 的后台和省电限制。WebSocket 若要接近实时，需要长期前台服务、常驻通知、电池优化豁免和 HyperOS 自启动配置；轮询不实时；自建 UnifiedPush 最终仍需要一个能在手机后台稳定连接的 distributor。

Bark Server **不能通过现有 APNs 链路直接服务 Android**。它已有 HTTP 接口、设备键映射、数据库、批量发送、Basic Auth、健康检查和部署文件，可作为 Go 服务端外壳参考；但其实际下行核心是 APNs，Android 不会获得 APNs device token。若改造成 Android 服务端，必须替换下行提供方并重做注册鉴权/白名单，属于“复用外壳、替换核心”，不是配置后即可使用。

## 1. Android 通用约束

### 1.1 通知权限

- Android 13（API 33）起，普通通知需要运行时权限 `POST_NOTIFICATIONS`；新安装应用默认关闭通知，必须由用户授权。[Android：通知运行时权限](https://developer.android.com/develop/ui/views/notifications/notification-permission)
- 用户拒绝后，应用不能在通知抽屉中发布普通通知。前台服务仍必须创建通知，但拒绝权限时只会在系统任务管理器中显示前台服务提示，不等于业务通知可见。
- MVP 应在用户完成“配置发布点/绑定接收身份”后、首次测试通知前解释并申请权限；申请后调用 `NotificationManagerCompat.areNotificationsEnabled()` 检查实际状态。

### 1.2 通知渠道

- Android 8.0（API 26）起，每条通知必须属于一个 `NotificationChannel`，否则通知不会显示。[Android：通知渠道](https://developer.android.com/develop/ui/views/notifications/channels)
- 渠道创建后，声音、振动和重要级别等行为由用户掌控，应用不能静默改回。因此服务端的 `priority` 不能越过用户在系统设置中关闭的渠道。
- MVP 至少建立两个稳定渠道：`default`（普通通知）和 `urgent`（真正需要及时提醒的通知）。渠道 ID 一旦发布应保持稳定。

### 1.3 后台执行与 Doze

- Android 8.0 起，应用进入后台后，普通后台服务只保留数分钟执行窗口，随后系统会停止服务。[Android 8.0：后台执行限制](https://developer.android.com/about/versions/oreo/background)
- Doze 会暂停普通网络访问、忽略 wake lock、推迟普通闹钟、JobScheduler 和 WorkManager。Android 官方明确建议实时下行优先使用 FCM，而不是每个应用维护自己的持久连接。[Android：Doze 与 App Standby](https://developer.android.com/training/monitoring-device-state/doze-standby)
- WorkManager 的周期任务最短间隔为 15 分钟，而且执行时刻受系统优化和约束影响，不能充当实时通知通道。[Android：定义 WorkRequest](https://developer.android.com/develop/background-work/background-tasks/persistent/getting-started/define-work)

### 1.4 前台服务

- 前台服务可以提高进程存活性，但必须持续显示系统通知，并且只应承载用户明确感知、需要立即或不中断执行的任务；不能只为了阻止应用进入空闲状态而启动。[Android：Doze 与 App Standby](https://developer.android.com/training/monitoring-device-state/doze-standby)
- Android 12（API 31）起，应用在后台启动前台服务通常被禁止，只有文档列出的例外情形可用。[Android：后台启动前台服务限制](https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start)
- 因此“开机后静默拉起前台服务并永久保持 WebSocket”不是稳固的通用 Android 契约。即便产品接受常驻通知，也必须在目标 HyperOS 版本上验证开机、升级、强杀、锁屏和省电模式后的恢复行为。

### 1.5 电池优化与自启动

- 被用户加入电池优化豁免列表的应用在 Doze 中可以使用网络并持有 partial wake lock，但其他限制仍可能存在。[Android：电池优化豁免](https://developer.android.com/training/monitoring-device-state/doze-standby#support_for_other_use_cases)
- 普通应用可引导用户打开电池优化设置。直接请求 `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` 只适用于核心功能确实被破坏、且不能合理使用 FCM 等平台通道的场景；Google Play 对此有限制。
- “自启动”不是 Android 标准权限或统一 API。小米官方明确指出 MIUI 的自启动管理会影响透传消息；HyperOS 的具体设置入口、默认策略及版本差异必须真机验证。[小米推送产品说明](https://dev.mi.com/console/doc/detail?pId=863)
- 对 MVP 的实际含义：采用小米推送“通知栏消息”时，不应把自启动和电池豁免作为基本送达前提；采用自建长连接时，两者会变成用户必须完成的部署步骤，但仍不能承诺绝对送达。

## 2. 候选路径比较

| 路径 | HyperOS 后台送达 | 对 Doze 的处理 | 用户/运维成本 | MVP 判断 |
| --- | --- | --- | --- | --- |
| 小米推送通知栏消息 | 小米官方称 MIUI 长连接由系统维护，通知不要求应用驻留 | 系统级通道承担后台连接 | 需小米开发者应用、客户端 SDK、服务端 API 凭据 | **首选**，目标设备是小米时最小且可靠 |
| FCM 高优先级 | 有 GMS 的 HyperOS 上可用；中国大陆版覆盖不能假定 | 可唤醒 Doze 设备；滥用会被降级 | Firebase 项目、FCM token、Google Play services | **第二提供方/国际版首选** |
| 其他厂商推送 | 在对应厂商设备上通常较好，在小米设备上无系统级优势 | 由各厂商系统服务处理 | 多套 SDK、账号、证书和服务端适配 | 非小米多品牌扩展时再做 |
| 应用 WebSocket/长连接 | 进程存活时实时；强杀、自启动限制、Doze 下不可靠 | 普通网络会被暂停；需前台服务/豁免缓解 | 常驻通知、耗电、断线重连、心跳、队列、用户设置 | 不作默认主链路 |
| 周期轮询 | 最终可达但延迟大，且可能被继续推迟 | WorkManager/JobScheduler 在 Doze 中延后 | 实现简单、服务器压力可控 | 只作补偿/对账，不作通知主链路 |
| 自建 UnifiedPush | 取决于 distributor 的实际下行技术；自建本身不等于系统级豁免 | WebSocket 型 distributor 仍需后台存活和电池豁免；FCM distributor 又依赖 FCM | 多一个 distributor、endpoint 生命周期、Web Push 加密和自建 push server | 非 MVP；适合明确要求去 Google 化且接受运维成本的用户 |

### 2.1 小米推送

小米官方资料给出的关键边界：

- MIUI 系统级通道由系统维护长连接，通知栏消息由“小米服务框架”弹出，不需要应用驻留后台。
- 透传消息交给应用自行处理，因此可能因自启动限制、应用未运行而无法送达；高送达要求应使用通知栏消息。
- 每台设备、每个应用注册后获得唯一 RegID；服务端可以按 RegID 定向推送。
- 服务端提供 HTTP API，客户端 SDK 为 Android SDK。[小米推送产品说明](https://dev.mi.com/console/doc/detail?pId=863)

这正好匹配当前静态架构：手机首次启动并授权通知，取得 RegID；再使用“接收端身份令牌”向自建服务器绑定。外部系统只接触自建服务器的 webhook，不能接触小米 AppSecret。

需要进一步真机验证的 HyperOS 项目：

- HyperOS 目标版本是否仍以与文档所述 MIUI 相同的系统级通道展示通知；
- 用户手动“强行停止”应用后，小米通知栏消息是否仍投递；
- 应用升级、设备重启、清除数据后的 RegID 刷新行为；
- 通知渠道的重要级别、锁屏展示、横幅/悬浮通知与后续“灵动岛”能力的关系。

### 2.2 FCM

- 普通优先级消息在设备进入 Doze 后可能延迟；高优先级消息会尝试立即送达并允许系统唤醒设备，但应只用于会产生用户可见通知的时效消息。[FCM 消息优先级](https://firebase.google.com/docs/cloud-messaging/android/message-priority)
- 如果高优先级消息长期不产生用户可见通知，FCM 可能将其降为普通优先级或交给 Google Play services 代理展示。
- `onMessageReceived()` 只有很短的执行窗口。消息应携带展示通知所需的完整小载荷；耗时工作交给 WorkManager。[FCM：接收消息](https://firebase.google.com/docs/cloud-messaging/android/receive)
- FCM 客户端要求 Android 6.0+ 且设备安装 Google Play Store/兼容 Google Play services。是否覆盖目标 HyperOS 设备必须按 ROM 和销售区域确认。[FCM Android 客户端要求](https://firebase.google.com/docs/cloud-messaging/android/client)

### 2.3 WebSocket / 长连接

自建 WebSocket 的优势是协议、鉴权和数据完全可控，但可靠性责任也全部转移到应用和服务端：

- 手机侧需要连接保活、网络切换重连、指数退避、心跳、去重和消息确认；
- 服务端需要在线连接表、离线队列、TTL、重放和多实例连接路由；
- 普通后台进程会被限制，Doze 会暂停网络；前台服务会带来常驻通知、后台启动限制和审核/用户体验成本；
- HyperOS 还要引导自启动和“无限制”电池策略，并覆盖用户强杀、系统清理和重启场景。

因此它适合作为开发调试通道，或面向用户明确接受常驻前台服务的私有部署模式，不适合作为默认“最小可靠”方案。

### 2.4 轮询

轮询可以作为丢消息后的补偿机制，例如应用回到前台时按游标拉取未读通知；不应承担实时唤醒。建议 MVP 服务端保留最近通知和递增游标，客户端在启动/恢复前台时对账，而不是每几分钟轮询。

### 2.5 自建 UnifiedPush

UnifiedPush 是一套角色和端点协议，不是一个绕过 Android 后台限制的系统服务。其典型链路为：

```text
Application Server
  -> Push Server（可自建 ntfy/NextPush 等）
  -> 手机上的 Push Distributor
  -> 本应用中的 UnifiedPush Connector
```

官方规范要求 distributor 与 push server 保持连接，再通过 Android 广播把消息交给注册应用；应用拿到 endpoint 后上传给 application server。[UnifiedPush 术语](https://github.com/UnifiedPush/specifications/blob/main/definitions.md) [Android 规范](https://github.com/UnifiedPush/specifications/blob/main/specifications/android.md)

官方列出的 ntfy distributor 默认使用 WebSocket 或 HTTP JSON stream，并明确要求授予通知和电池优化豁免以保证后台运行。[UnifiedPush：ntfy Android distributor](https://unifiedpush.org/users/distributors/ntfy/) 这说明自建 UnifiedPush 解决的是开放协议、自托管和多个应用共享一个连接的问题，并没有天然获得 HyperOS 系统级后台特权。

对于当前只有一个自有 App 的 MVP，引入 distributor + connector + push server 会增加组件数量。除非“完全不使用 FCM/小米推送”是硬性目标，否则不建议首期采用。

## 3. Bark Server 评估

本次检查的是 `Finb/bark-server` 的 `master`，提交 `3df8990fcbc407a3f5638eea8cedc3289d1a405d`。

### 3.1 已具备的可复用能力

- Go/Fiber HTTP 服务，提供 `/push`、兼容旧版路径和健康检查；
- `device_key -> device_token` 注册与查询；
- 单发和批量发送；
- bbolt、MySQL 和 serverless 环境变量存储；
- 全局 Basic Auth、TLS、URL 前缀、Docker/Helm 等部署入口；
- 通知标题、正文、分组、声音、TTL 等已有请求字段。

来源：[README](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/README.md) [API V2](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/docs/API_V2.md) [route_push.go](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_push.go) [数据库接口](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/database.go)

### 3.2 为什么现有 Bark 不能直接服务 Android

- Bark README 明确将 Bark 定义为向 iPhone 推送自定义通知的 iOS App。
- 注册接口保存的是 Bark iOS 客户端提交的 device token；发送时按 `device_key` 取出 token。
- `apns/apns.go` 固定创建 APNs HTTP/2 客户端，固定 topic `me.fin.bark`，并把 token 作为 APNs `DeviceToken` 发送到 Apple 生产环境。
- Android 应用无法向 APNs 注册并获得属于该 Android 应用的 APNs device token；APNs token 是 Apple 平台应用实例与 APNs 的标识。因此 APNs 不能作为 Android 的直接下行通道。

来源：[route_register.go](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_register.go) [route_push.go](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_push.go) [apns/apns.go](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/apns/apns.go) [Apple：向 APNs 注册](https://developer.apple.com/documentation/usernotifications/registering-your-app-with-apns)

### 3.3 与本项目安全模型的差距

- Bark 的 Basic Auth 是全局用户名/密码，不等同于可轮换、可审计的 webhook Bearer token。
- `/register` 被明确排除在 Basic Auth 外。持有某个现有 `device_key` 的调用方可以更新其 device token；这不满足“手机使用接收端身份令牌注册，服务器只允许白名单接收端”的要求。[route_auth.go](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_auth.go) [bbolt.go](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/bbolt.go)
- 当前数据库抽象只有 device key/token CRUD，没有接收端状态、白名单、provider 类型、token 轮换时间、最后成功送达时间和审计记录。
- APNs 失败后的处理主要是删除无效 token，没有与本项目 webhook 请求对应的幂等键、持久队列、重试策略或端到端回执。

### 3.4 改造可行性

技术上可改造，建议最少做以下结构变化：

```text
Webhook API
  -> WebhookTokenAuth
  -> ReceiverWhitelist
  -> NotificationService
  -> DeliveryProvider
       - MiPushProvider（MVP）
       - FcmProvider（可选）
       - ApnsProvider（若未来保留 iOS/Bark）
```

数据模型至少从 `device_key -> device_token` 扩展为：

```text
receiver_id
receiver_identity_token_hash
enabled / allowlisted
provider_type
provider_registration_token
provider_token_updated_at
last_delivery_at / last_error
```

注册接口必须验证接收端身份令牌，且只能更新与该身份绑定的 receiver；webhook 接口必须单独验证 webhook 访问令牌。服务端向小米/FCM 发送时使用各自的服务端密钥，不能下发给手机或 webhook 调用方。

判断：

- 如果未来要同时支持 Bark/iOS，fork Bark Server 并把 APNs 抽成多 provider 有现实价值。
- 如果项目只做 Android/HyperOS，Bark 能复用的主要是 HTTP/数据库/部署外壳，核心推送和安全模型都要更换；直接实现一个小型、按当前契约设计的服务通常更清晰。
- 无论是否 fork，Bark 的请求字段和 `device_key` 使用体验可以作为 API 设计参考，但不能把“兼容 Bark API”当成 Android 后台可靠性的来源。

## 4. MVP 最小可靠方案

### 4.1 手机首次配置

1. 创建 `default`/`urgent` 通知渠道。
2. 在 Android 13+ 申请 `POST_NOTIFICATIONS`，并检查应用级、渠道级通知状态。
3. 初始化小米推送 SDK，取得 RegID。
4. 用户配置服务器发布点和接收端身份令牌。
5. App 通过 HTTPS 调用服务器注册接口，提交接收端身份令牌和 RegID；服务器只接受白名单中的接收端。
6. 服务器发送一条测试通知；App 展示明确的成功/失败状态。

### 4.2 日常发送

1. 外部调用方携带 webhook 访问令牌发送通知。
2. 服务器验证令牌、请求大小、幂等键和接收端白名单。
3. 服务器按 receiver 找到 `MiPushProvider + RegID`，发送小米“通知栏消息”，不要用透传消息作为默认路径。
4. 服务器记录 provider message id、返回码和失败原因；无效 RegID 标记为需重新注册。
5. App 启动或回到前台时重新确认 RegID，并按游标拉取可能遗漏的通知作为补偿。

### 4.3 MVP 不做

- 不用后台常驻 WebSocket 作为默认链路；
- 不用 15 分钟轮询伪装实时推送；
- 不在首期引入完整 UnifiedPush distributor；
- 不依赖透传消息启动应用后再自行弹通知；
- 不承诺用户关闭通知权限、关闭渠道或强行停止应用后仍可展示通知。

## 5. 验证清单

至少在一台目标 HyperOS 真机上验证：

- 首次安装未授权、授权、拒绝后再次引导；
- 普通/紧急渠道分别关闭后的行为；
- 熄屏进入 Doze 后 5、30、120 分钟发送；
- 从最近任务划掉、系统内存清理、设备重启后发送；
- HyperOS 电池策略“默认/无限制”和自启动开关的组合；
- 手动强行停止应用后发送；
- Wi-Fi/蜂窝网络切换、离线后恢复；
- RegID 更新、清除数据、卸载重装；
- webhook 重复请求、服务端超时、provider 限流和无效 token；
- 通知点击后 App 冷启动时携带的数据是否完整。

其中“强行停止后仍能送达”不能只凭平台文档推断，必须以目标 HyperOS 版本真机结果作为产品边界。

## 参考资料

- [Android：通知运行时权限](https://developer.android.com/develop/ui/views/notifications/notification-permission)
- [Android：通知渠道](https://developer.android.com/develop/ui/views/notifications/channels)
- [Android：Doze 与 App Standby](https://developer.android.com/training/monitoring-device-state/doze-standby)
- [Android 8.0：后台执行限制](https://developer.android.com/about/versions/oreo/background)
- [Android：后台启动前台服务限制](https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start)
- [Android：定义 WorkRequest](https://developer.android.com/develop/background-work/background-tasks/persistent/getting-started/define-work)
- [Firebase：Android 客户端设置](https://firebase.google.com/docs/cloud-messaging/android/client)
- [Firebase：Android 消息优先级](https://firebase.google.com/docs/cloud-messaging/android/message-priority)
- [Firebase：Android 接收消息](https://firebase.google.com/docs/cloud-messaging/android/receive)
- [小米推送产品说明](https://dev.mi.com/console/doc/detail?pId=863)
- [UnifiedPush definitions](https://github.com/UnifiedPush/specifications/blob/main/definitions.md)
- [UnifiedPush Android specification](https://github.com/UnifiedPush/specifications/blob/main/specifications/android.md)
- [UnifiedPush distributors](https://unifiedpush.org/users/distributors/)
- [Finb/bark-server](https://github.com/Finb/bark-server)
- [Apple：向 APNs 注册](https://developer.apple.com/documentation/usernotifications/registering-your-app-with-apns)
