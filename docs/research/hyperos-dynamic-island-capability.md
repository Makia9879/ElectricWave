# HyperOS 灵动岛 / 焦点通知能力边界调研

调研日期：2026-07-10  
对应问题：issue 0003  
证据范围：小米澎湃 OS 官方开发者平台、Xiaomi HyperOS 官方产品页、Android Developers 官方文档。

## 结论摘要

1. **第三方 Android 应用可以主动发布小米官方的“焦点通知 / 小米超级岛”样式。** 小米已经公开开发者文档：Xiaomi HyperOS 2 对应“焦点通知”，Xiaomi HyperOS 3 对应“小米超级岛通知”。客户端方式是在标准 `Notification` 上附加 `miui.focus.param` 等小米扩展参数；云端方式可经 MiPush 下发。[小米开发指南][mi-dev-guide]明确说明，在支持设备上，系统会把携带扩展参数的通知按岛通知样式显示。
2. **这不是任意 APK 可自由使用的标准 Android API。** 它是小米官方公开说明、但受平台准入控制的 OEM 扩展能力。开发者需要企业认证、创建并完善在架应用、启用服务、配置 App Id 与证书指纹、逐场景提交方案、通过设备白名单联调和上线验证，之后平台才授予正式权限。[接入流程][mi-access-flow]还说明，新场景需要重新审核，未经审核上线可能导致服务停用。
3. **不能把“普通 webhook 消息”统一承诺为上岛。** 官方要求场景有明确且有限的生命周期（不超过 12 小时）、无营销属性，并满足履约、实时高关注或错过后有严重后果等价值条件。纯告知、普通社交消息、长期常驻、营销推广等明确属于禁止或不适合场景。[业务介绍][mi-business-intro]
4. **版本与设备覆盖必须运行时判断，不能只按“小米手机”或“HyperOS”判断。** 最新开发者版本页写明“暂不限制机型，以系统版本为主”，但 HyperOS 3 官方产品页同时提示超级岛仍仅支持部分机型、部分应用。开发指南也要求查询系统是否支持岛、焦点协议版本及应用焦点通知权限。因此产品承诺应以“支持的系统版本 + 系统能力检测 + 应用权限 + 已审核场景”四项同时成立为条件。
5. **未获小米专项权限或设备不支持时，只能可靠降级为标准 Android 通知。** 标准通知可进入状态栏、通知抽屉、锁屏，并在系统判定为重要时可能显示 heads-up 浮动通知；它不能保证变成焦点通知、超级岛摘要态/展开态、息屏岛或 OS2 状态栏焦点信息。

## 1. 官方是否向第三方应用开放

### 1.1 已存在公开接入文档和发送协议

小米澎湃 OS 开发者平台把“小米超级岛”列为“应用开发 / 服务能力”，并将其定义为“为开发者提供的创新通知交互能力”。[业务介绍][mi-business-intro]说明该能力建立在焦点通知之上，可在当前应用上层展示后台持续服务并提供即时交互。

[开发指南][mi-dev-guide]公开了两种发送方式：

- **客户端实现**：按 Android 原生通知流程创建 `NotificationChannel` 和 `Notification`，再把 JSON 数据写入 `notification.extras["miui.focus.param"]`，最后调用 `NotificationManager.notify()`。
- **MiPush 实现**：沿用 MiPush 接口，以 `miui.focus.param`、`miui.focus.pic_XXX` 等 extra 参数发送；当前文档写明只支持按 `regId` 发送。

这足以确认第三方应用在通过准入后可以主动创建、更新和结束焦点通知 / 超级岛，不应再表述为“没有任何官方接入 API”。

### 1.2 “公开”不等于“Android 标准 API”或“无审核开放”

该能力混合使用标准 Android 通知 API 与小米私有协议：

- 标准部分是 `NotificationManager`、`NotificationChannel`、`Notification`、`Notification.Action`、`PendingIntent` 等 Android API。
- 小米专用部分包括 `miui.focus.param`、`miui.focus.pics`、`miui.focus.actions` 等扩展字段，以及小米定义的模板、场景和权限。
- 官方能力检测示例还使用 `persist.sys.feature.island`、`notification_focus_protocol` 和 `content://miui.statusbar.notification.public`。其中反射读取 `android.os.SystemProperties` 并不是面向所有 Android 设备的标准公开 SDK 合约。

因此更准确的分类是：**有公开官方开发文档和接入协议，但属于小米 OEM 专用、平台鉴权和审核控制的能力，不是普通 Android 应用天然拥有的通用通知能力。**

## 2. 准入、账号、权限和发布限制

根据[接入流程][mi-access-flow]，正式接入至少包含以下门槛：

1. 注册小米账号并完成企业开发者认证。
2. 在国内开发者平台创建应用、完善资料并上传包体；文档明确说明海外应用暂不支持接入国内平台能力。
3. 在应用的开放服务中启用“小米超级岛”，填写联系人并配置证书指纹。
4. 逐个场景进行预审，之后提交完整展示节点、内容和效果图进行正式方案审核。
5. 配置平台生成的 `com.xiaomi.xms.APP_ID`，并让 APK 签名通过鉴权。
6. 在设备白名单中联调；白名单有效期为 30 天，每个应用最多 10 台设备。
7. 提交正式环境 APK 和验证方法，通过上线验证后获得正式权限；初次开通还会经历约 7 到 15 天灰度放量。
8. 每个新增场景重新走预审、正式审核和上线验证。

发送路径也有不同运行条件：

- **客户端实现**要求应用保持活跃（官方括注为后台运行），由客户端本地发送岛通知。
- **MiPush 实现**不要求应用进程存活；但必须先为项目开通推送服务和小米超级岛服务，并已取得对应场景权限。

OS2 还存在额外差异：接入流程要求通过邮件申请 OS2 焦点通知开发测试包及临时权限。由此可见，即便协议字段公开，也不能把能力视为“安装后即可调用”。

## 3. 场景限制及其对 webhook 产品的影响

[业务介绍][mi-business-intro]给出的基础准入条件是两项同时满足：

- 服务有明确的开始和结束条件，整个进程不超过 12 小时。
- 不含推广、广告、促活等营销属性。

满足基础条件后，还需至少满足以下一种核心价值：

- 用户主动发起或预约，通知承担履约进度。
- 信息在当前时刻高度受关注，需要反复查看或快捷操作。
- 错过并未及时处理会导致经济、安全、行程或服务失效等实质后果。

官方列出的禁止或不适合场景包括：长期天气/实时股价等长期常驻服务、营销推广、应用或内容更新等纯告知、普通社交消息与点赞评论，以及由系统或指定服务统一承接的重复场景。

这对本项目的含义是：

- `webhook -> 服务器 -> 手机` 可以作为通用通知链路。
- webhook 的任意文本消息默认只能按**标准 Android 通知**处理。
- 只有预先定义、符合准入原则、已经小米审核通过的业务类型，才可映射为焦点通知 / 超级岛模板。
- 服务端协议需要显式区分普通通知与已批准的岛场景，不能让调用方用任意 payload 自行声明“上岛”。

## 4. 系统版本、机型和权限边界

### 4.1 OS 版本形态

[版本信息][mi-version]给出的当前范围是：

| 系统版本 | 官方形态 | 主要展示范围 |
| --- | --- | --- |
| Xiaomi HyperOS 2 | 焦点通知 | 状态栏、通知中心、锁屏、息屏、小折叠外屏 |
| Xiaomi HyperOS 3 | 小米超级岛通知 | 岛摘要态、岛展开态，以及焦点通知除状态栏外的其他位置 |

OS2 与 OS3 使用相同的数据接口，但模板有升级与新增；开发者可以选择兼容模板，也可以按 HyperOS 版本下发不同数据。OS1 需要单独适配，官方 Q&A 明确表示当前阶段不建议接入。[常见 Q&A][mi-qa]

### 4.2 机型与实际覆盖

[版本信息][mi-version]写明“暂不限制机型，以系统版本为主”。但 [Xiaomi HyperOS 3 官方产品页][hyperos-product]脚注又说明，“小米超级岛”目前仅支持部分机型和部分应用，下拉小窗与拖拽分享还依赖第三方开发者适配。

两处资料并不适合简单二选一：开发者版本页描述接入规则，产品页描述实际发布覆盖。工程上应采用更保守的运行时判定：

1. 检测设备是否声明支持岛功能。
2. 读取焦点通知协议版本，区分 OS1 / OS2 / OS3 模板。
3. 查询当前应用的焦点通知权限是否开启。
4. 对不满足任一条件的设备执行普通通知降级。

不能仅凭手机型号列表做长期硬编码，也不能承诺“所有 HyperOS 3 手机均上岛”。

### 4.3 用户和平台权限

权限至少存在三层：

- Android 13 及以上的 `POST_NOTIFICATIONS` 运行时权限；新安装应用通知默认关闭，必须由用户授权。[Android 通知权限][android-permission]
- Android 8.0 及以上的通知渠道；渠道重要性和锁屏等行为受系统与用户设置控制。[创建通知][android-create]
- 小米平台配置的焦点通知 / 超级岛正式权限，以及用户侧焦点通知开关。[开发指南][mi-dev-guide]

任何一层被关闭，都可能导致不展示、降低打扰程度或退化为普通通知。

## 5. 标准 Android 通知可获得什么效果

未接入小米扩展协议，或小米能力检测/权限不满足时，应用仍可通过标准 Android 通知获得以下效果：

- 状态栏图标与通知抽屉中的详细条目。
- 应用图标角标（具体启动器行为由系统决定）。
- 重要通知在设备解锁时**可能**短暂显示 heads-up 浮动窗口。
- 锁屏通知及公开/私密内容控制，最终可见性由用户设置决定。
- 标准标题、正文、图片、进度、分组、展开样式和操作按钮。
- 使用相同通知 ID 更新已有通知，或在结束时取消通知。

这些能力由 [About notifications][android-about] 和 [Create a notification][android-create] 定义。Android 官方还明确提醒：即使应用设置了通知重要性或优先级，系统也不保证最终提醒行为，且用户始终控制锁屏可见性。

自定义 `RemoteViews` 也不能绕过系统获得岛形态。[Android 自定义通知][android-custom]说明，自 Android 12 起，目标 API 31 及以上的应用不能创建完全自定义通知，系统会套用标准模板；不同系统版本对折叠、heads-up 和展开布局空间也有限制。

## 6. 产品不可承诺项

除非对应应用、版本、设备和场景均已通过小米正式验证，否则产品文案和验收标准不能承诺：

- “所有小米 / HyperOS 手机都会显示灵动岛或超级岛”。
- “发送普通 Android 通知即可自动上岛”。
- “任意 webhook 文本、营销消息或普通聊天消息都能上岛”。
- “应用可自行决定系统最终展示位置、展开时长、提醒强度和锁屏可见性”。
- “自定义通知布局可以模拟并替代官方超级岛”。
- “只要拥有 MiPush 通道即可绕过场景审核和焦点通知权限”。
- “岛通知可永久常驻”；官方准入要求服务不超过 12 小时，且岛与通知还存在默认消失时间。

建议 MVP 对外能力表述为：

> 在 Android 通知权限和系统设置允许时，手机端展示标准系统通知；对满足小米准入规则、已完成官方审核和适配、且运行时能力检测通过的场景，可增强为 HyperOS 2 焦点通知或 HyperOS 3 小米超级岛。具体样式和位置由系统版本、设备能力、用户设置及小米平台权限共同决定。

## 7. 对后续规格和验证的建议

1. 把“收到并展示标准 Android 通知”设为 MVP 基线，不把上岛设为无条件验收项。
2. 在通知协议中加入服务端控制的 `presentation` / `approvedScenario` 概念；未匹配已审批场景时强制普通通知。
3. 手机端实现三段降级：OS3 超级岛 -> OS2 焦点通知 -> 标准 Android 通知。
4. 将系统能力、焦点通知协议版本、用户通知权限、焦点通知权限记录为诊断信息。
5. 在真实设备上覆盖至少四类验证：OS3 支持岛、OS2 支持焦点通知、HyperOS 不支持/未授权、非小米 Android 普通通知。
6. 正式投入超级岛适配前，先用一个明确符合准入原则的垂直场景向小米提报；不要以“通用 webhook 通知器”整体申请。

## 官方来源

### 小米 / HyperOS

- [业务介绍 | 小米澎湃OS开发者平台][mi-business-intro]：超级岛定义、交互、准入原则和禁止场景；更新时间 2026-01-29。
- [版本信息 | 小米澎湃OS开发者平台][mi-version]：OS2 / OS3 形态、机型和模板兼容范围。
- [接入流程 | 小米澎湃OS开发者平台][mi-access-flow]：企业认证、应用创建、MiPush、场景审核、白名单、鉴权与正式权限。
- [开发指南 | 小米澎湃OS开发者平台][mi-dev-guide]：客户端和 MiPush 协议、`miui.focus.param`、模板参数及能力检测。
- [常见 Q&A | 小米澎湃OS开发者平台][mi-qa]：权限申请、OS1 / OS2 支持、MiPush 进程条件和用户开关。
- [小米澎湃OS 3 | Xiaomi HyperOS 3][hyperos-product]：实际机型/应用覆盖和第三方适配说明。

### Android

- [About notifications | Android Developers][android-about]：状态栏、通知抽屉、heads-up、锁屏等标准展示位置。
- [Create a notification | Android Developers][android-create]：标准通知创建、通知渠道、重要性、更新与锁屏控制。
- [Notification runtime permission | Android Developers][android-permission]：Android 13+ `POST_NOTIFICATIONS` 权限。
- [Create a custom notification layout | Android Developers][android-custom]：自定义布局限制和 Android 12 标准模板约束。

[mi-business-intro]: https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2140
[mi-version]: https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2141
[mi-access-flow]: https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2132
[mi-dev-guide]: https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2131
[mi-qa]: https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2146
[hyperos-product]: https://hyperos.mi.com/
[android-about]: https://developer.android.com/develop/ui/compose/notifications
[android-create]: https://developer.android.com/develop/ui/views/notifications/build-notification
[android-permission]: https://developer.android.com/develop/ui/compose/notifications/notification-permission
[android-custom]: https://developer.android.com/develop/ui/views/notifications/custom-notification
