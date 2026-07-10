# 评估 Bark Server 复用方案

Labels: wayfinder:research
Status: closed
Assignee:
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by: 0002-define-mvp-user-scenarios.md

## Question

`Finb/bark-server` 已具备哪些可复用于本项目静态三段架构的能力？需要从源码确认其 webhook/API、设备身份、下游推送、鉴权与访问控制模型，并判断通过配置、小幅改造或架构性改造后，能否满足 webhook 访问令牌、接收端白名单和手机接收端身份令牌要求。

## Resolution

调研报告：[Finb/bark-server 服务端适配性调研](../../research/bark-server-fit.md)。

结论为**有条件复用服务端骨架，不直接采用现成 bark-server 镜像，也不应先 fork 再决定 Android 下行方案**。

- 已有可复用能力：HTTP 推送入口、接收端映射的基础存储、Go/Fiber 工程结构与部署资产；可选全局 Basic Auth 能临时保护推送入口。
- 不满足的安全语义：`/register` 默认免鉴权，`device_key` 混合了寻址和 capability secret，数据库没有受控白名单、receiver identity、撤销状态、调用方限流或安全审计。因此要满足 webhook access token、接收端白名单、接收端身份令牌，仍需小到中等的服务端安全改造。
- 不满足的下游能力：当前投递层只实现并硬编码 Bark iOS 的 APNs 身份与 topic；没有 Android/HyperOS 客户端、FCM、厂商推送或长连接投递。因此“服务器 -> HyperOS Android 手机”是大改，必须在 Android 下行模型确定后实现平台无关的 DeliveryProvider 与 Android receiver 注册/续期链路。

建议后续以平台无关的正式接口和三种独立凭据为基线：webhook access token（谁能发送）、receiver ID/白名单状态（能发给谁）、receiver identity token（哪台手机可注册或更新 endpoint）。Bark 的 APNs provider 只保留为未来 iOS 兼容的可选实现。
