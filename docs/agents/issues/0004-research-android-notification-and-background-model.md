# 调研 Android 通知与后台运行模型

Labels: wayfinder:research
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by:

## Question

为了接收端点发送的通知并稳定展示，Android 侧需要哪些权限、前台服务/后台任务/网络监听能力、通知渠道配置和电池优化处理？需要给出 MVP 可接受的运行模型与主要风险。

## Resolution

调研报告：[Android / HyperOS 手机接收链路调研](../../research/android-delivery-model.md)。

结论：面向小米 HyperOS/MIUI 设备的 MVP，下行主链路推荐使用“小米推送通知栏消息”：`webhook -> 自建服务器 -> 小米推送 RegID -> HyperOS/MIUI 系统通道 -> 手机通知`。FCM 应作为具备 Google Play services 设备的第二 `DeliveryProvider`；自建 WebSocket/长连接、轮询和自建 UnifiedPush 不适合作为默认最小可靠链路，只能作为调试、补偿或用户明确接受后台常驻成本的扩展方案。

Bark Server 现有下行核心是 Apple APNs，Android 应用无法注册 APNs device token，因此不能直接服务 Android。它可参考或复用 HTTP、存储、批量发送、鉴权入口和部署骨架，但 Android/HyperOS MVP 需要替换为小米推送/FCM 等 provider，并重做接收端身份令牌、白名单、webhook 访问令牌、审计与重试模型。
