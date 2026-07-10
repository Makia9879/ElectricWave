# 调研 HyperOS 灵动岛能力边界

Labels: wayfinder:research
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by:

## Question

HyperOS 对第三方应用展示“灵动岛”样式通知的公开能力边界是什么？需要确认是否有官方 API、是否只能依赖系统通知样式、是否存在机型/系统版本限制，以及这些限制如何影响 MVP。

## Resolution

研究报告：[HyperOS 灵动岛 / 焦点通知能力边界调研](../../research/hyperos-dynamic-island-capability.md)。

小米已为第三方应用公开焦点通知 / 小米超级岛的接入文档与协议：HyperOS 2 使用焦点通知，HyperOS 3 使用小米超级岛；可由客户端原生通知扩展参数或 MiPush 下发。但它是受小米平台鉴权、场景审核、签名和应用权限控制的 OEM 能力，并非任意 APK 可自由调用的标准 Android API。设备、系统版本、用户通知设置和焦点通知权限均会影响最终展示。

推荐将标准 Android 通知设为 MVP 基线；只有预先定义、符合小米准入原则且已获审核的持续服务场景才尝试增强为焦点通知 / 超级岛。通用 webhook 文本不得承诺上岛，未满足条件时降级为标准系统通知。
