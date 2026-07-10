# 自举通知通道

更新日期：2026-07-10

## 决策

个人 MVP 的默认下行改为 `SelfHostedSseProvider`，不要求小米开发者企业身份、MiPush、FCM 或应用商店发布。

```text
外部系统
  -> HTTPS POST /api/v1/notifications
  -> 自建服务端
  -> 已认证 HTTP SSE
  -> Android 前台服务
  -> 原生 Android 通知
```

服务端保留既定的 webhook token、receiver allowlist、receiver identity token、单 receiver、TTL、幂等和限流规则。Android App 使用 receiver identity token 在 SSE 请求的 `Authorization: Bearer` header 中认证；token 不出现在 URL 或日志中。

生产 HTTP 链路使用 `Cloudflare -> VPS Nginx -> 回环发布的应用容器`。Nginx 负责源站 TLS、SSE 禁用缓冲和路径代理；应用仅接受来自本机 Nginx 的受信任转发 header。完整部署协议见[Cloudflare 与 Nginx HTTP 服务设计](cloudflare-nginx-http-server.md)。

## Android 运行模型

- 用户在 App 内显式开启“接收通知”后，启动一个类型受限的前台服务，并显示一个低重要性常驻状态通知。
- 该服务维持一个 SSE HTTP 请求。服务端每 30 秒发送一条 SSE 注释心跳；客户端只在 EOF、网络错误或心跳超时后采用指数退避重连。收到通知事件后立即创建 `default` 或 `urgent` 原生通知。
- Android 13+ 仍须授予 `POST_NOTIFICATIONS`；用户关闭应用通知或渠道后，App 只能诊断，不能强制展示。
- 通知展示基线包含通知栏、顶部 heads-up 和锁屏通知。`urgent` 渠道请求较高重要性以获得 heads-up 资格；锁屏内容使用 Android 标准可见性控制。两者的最终展示均由 Android/HyperOS、通知渠道及用户设置决定，App 不能保证或强制显示。
- App 被用户“强行停止”后，Android 不允许服务自动恢复；设备重启、系统省电和厂商清理也会影响连接恢复。App 重新打开并由用户启动服务后才恢复。
- 这条链路适合个人、测试和可接受常驻通知/耗电成本的场景，不承诺系统级推送送达率。

## 服务端接口增量

除既定 webhook 外，增加一个仅供 App 使用的连接端点：

```http
GET /api/v1/receivers/{receiver_id}/stream
Accept: text/event-stream
Authorization: Bearer <receiver_identity_token>
```

连接建立后服务端校验 receiver 存在、allowlisted/enabled、identity token 匹配；同一 receiver 新连接替换旧连接。响应必须包含 `Content-Type: text/event-stream`、`Cache-Control: no-cache` 与禁用代理缓冲的响应头。服务端以 `event: notification` 和 JSON `data:` 字段下发以下已验证的通知事件：

```json
{
  "type": "notification",
  "notification_id": "ntf_...",
  "title": "订单已支付",
  "body": "订单 123 已完成支付",
  "priority": "normal",
  "group_key": "orders",
  "data": {"order_id": "123"},
  "expires_at": "2026-07-10T12:00:00Z"
}
```

连接离线时，bootstrap 版本返回 `503 delivery_unavailable`，不伪造“已送达”。持久离线队列和前台补偿拉取可在服务端基础稳定后再增加。

## 与原规格的关系

本文件取代“MiPush 为个人 MVP 默认下行”的产品决策，但不否定既有调研结论：小米推送在获得相应账号与凭据后仍可作为更可靠的可选 `DeliveryProvider`。焦点通知/超级岛仍不进入个人 MVP。

## 原生展示边界

顶部 heads-up 和锁屏通知是 Android 原生 `Notification` 与 `NotificationChannel` 的能力，不是小米超级岛能力。客户端实现应：

- 创建稳定的 `default` 与 `urgent` 渠道；`urgent` 初始请求较高重要性，`default` 使用普通重要性。
- 把 webhook 的 `priority=high` 映射到 `urgent`，其余映射到 `default`；服务端优先级不能越过用户对渠道的修改。
- 对可能包含敏感内容的通知采用 `VISIBILITY_PRIVATE`，并允许用户在 App 内选择锁屏显示完整正文或隐藏敏感内容。系统仍可能按用户的锁屏隐私设置隐藏内容。
- 在目标设备上分别验证授权、关闭应用通知、关闭 `urgent` 渠道、关闭锁屏通知以及“勿扰”状态。上述状态下不将未出现 heads-up 或锁屏通知判为 App 投递失败。
