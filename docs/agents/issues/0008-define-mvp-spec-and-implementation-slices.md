# 汇总 MVP 规格与实施切片

Labels: wayfinder:task
Status: closed
Assignee: Codex Agent Team
Parent: 0001-map-hyperos-dynamic-island-notification-app.md
Blocked by: 0002-define-mvp-user-scenarios.md, 0003-research-hyperos-dynamic-island-capability.md, 0004-research-android-notification-and-background-model.md, 0006-decide-endpoint-configuration-model.md, 0007-define-security-and-abuse-boundary.md, 0009-evaluate-bark-server-reuse.md

## Question

把已关闭票据的决策汇总成一份可实现的 MVP 规格，并拆成可交给实现 agent 的工程切片。需要包含功能范围、非目标、协议草案、权限/运行模型、兼容性风险和验收标准。

## Resolution

> 2026-07-10 决策更新：个人 MVP 的默认下行由 `MiPushProvider` 调整为 [SelfHostedSseProvider](../../architecture/self-hosted-bootstrap-channel.md)。App 通过用户显式启动的前台服务维持 HTTP SSE 连接并发布标准 Android 通知；服务端心跳、客户端指数退避重连是唯一的连接维护动作。MiPush/FCM 退为未来可选 provider。SSE 不绕过 Android 的强行停止、省电和重启限制，接受常驻通知与重连成本。

MVP 规格以“标准 Android 通知可见”为基线，以自建 HTTP SSE 和 Android 前台服务作为个人版默认下行链路；HyperOS 焦点通知/超级岛不进入个人 MVP，小米推送/FCM 只保留为未来可选 provider。

### 范围与非目标

进入 MVP：

- 单向通知链路：外部系统通过服务端 webhook 创建一条通知，服务端按接收端白名单投递到一个 Android/HyperOS 手机接收端。
- 服务端使用 webhook access token 控制谁可以发送，使用 `receiver_id` 和 allowlist/enabled 状态控制能发给谁。
- Android App 配置自建服务器 endpoint、`receiver_id`、receiver identity token；用户显式开启接收后运行前台服务并建立认证 SSE 连接。
- 手机端创建稳定通知渠道并在权限允许时展示标准系统通知。
- 服务端记录最小审计、幂等、限流和 SSE 连接状态；接收端离线时明确返回不可用，不伪造已投递。

不进入 MVP：

- 无服务器直连、局域网直连、手机主动订阅任意外部 webhook。
- 多用户账号体系、组织权限、公开自助注册、批量发送和管理后台。
- 离线持久队列、轮询补偿、WebSocket 和 UnifiedPush；bootstrap 版本只服务在线的 SSE 连接。
- 任意 webhook 文本上岛、营销/普通社交通知上岛、绕过小米场景审核或用户通知设置。
- 承诺用户关闭通知权限、关闭渠道、强行停止应用、设备/系统不支持时仍可展示通知。

### 三段架构

```text
外部系统
  -> POST /api/v1/notifications
  -> 自建服务端
  -> SelfHostedSseProvider
  -> Android 前台服务和系统通知
```

第一段 `外部系统 -> 服务端` 只接受 HTTPS JSON webhook，使用 `Authorization: Bearer <webhook_access_token>`。第二段服务端负责鉴权、校验、幂等、白名单、限流、审计、TTL 和 provider 调度。第三段 `服务端 -> 手机` 默认通过已认证 HTTP SSE 连接下发；Android 前台服务收到事件后发布本地通知。App 不持有 webhook access token、小米 AppSecret 或 FCM service account。用户强行停止 App、设备重启或系统省电可能中断连接，服务端不承诺离线送达。

Bark Server 只能有条件复用 HTTP/存储/部署骨架和部分字段经验；其 APNs 下游、公开 `/register`、`device_key` capability 模型不能直接用于 Android/HyperOS MVP。

### 通知端点契约

Endpoint：

- Method: `POST`
- Path: `/api/v1/notifications`
- Content-Type: `application/json; charset=utf-8`
- Auth: `Authorization: Bearer <webhook_access_token>`

请求体：

```json
{
  "receiver_id": "phone-main",
  "idempotency_key": "order-123-paid-v1",
  "title": "订单已支付",
  "body": "订单 123 已完成支付",
  "priority": "normal",
  "ttl_seconds": 3600,
  "group_key": "orders",
  "icon": "default",
  "data": {
    "order_id": "123"
  }
}
```

主要字段：

- `receiver_id` required string：目标接收端，MVP 只支持单个 receiver，不接受数组。
- `title` required string：1 到 80 字符。
- `body` required string：1 到 500 字符。
- `idempotency_key` optional string：同一 `webhook_token_id + receiver_id` 下 24 小时内去重。
- `priority` optional enum：`low`、`normal`、`high`，默认 `normal`；只映射服务端调度和 provider 优先级，不能越过用户渠道设置。
- `ttl_seconds` optional integer：60 到 86400，默认 3600。
- `group_key` optional string：最大 64 字符，用于通知栏聚合，不保证跨厂商 UI 一致。
- `icon` optional string：只允许预置键，默认 `default`，不接受任意图片 URL。
- `data` optional object：最大 4096 bytes；不得解释为上岛、全屏、常驻或系统级展示指令。

请求体最大 8 KiB。未知字段返回 `400 invalid_request`，避免调用方误以为未实现字段已生效。

成功响应：

- `201 Created`：新通知已被服务端接受，返回 `{"notification_id":"...","status":"accepted"}`。
- `200 OK`：幂等重复请求，返回原 `notification_id` 和 `{"status":"duplicate"}`。

错误响应统一为 `{"error":{"code":"invalid_request","message":"..."}}`。主要错误码：`400 invalid_request`、`401 unauthorized`、`403 receiver_not_allowed`、`404 receiver_not_found`、`409 idempotency_conflict`、`413 payload_too_large`、`429 rate_limited`、`500 internal_error`、`503 delivery_unavailable`。错误不得回显任何 token、RegID/FCM token 或 provider 凭据。

服务端语义是“接受并尝试投递”，不承诺手机最终展示；通知权限、渠道、系统策略、用户设置和 provider 状态均可能阻止显示。

### 接收端注册与配置

App 采用 profile 模型。MVP UI 只需一个启用 profile，数据模型保留多 profile 扩展空间。profile 至少保存：

- `server_endpoint`
- `receiver_id`
- `receiver_identity_token`
- `enabled`
- `provider_type`（个人 MVP 固定为 `self_hosted_sse`）
- SSE 连接状态与最后心跳时间
- 最近注册时间
- 最近测试结果

绑定流程：

1. App 创建 `default` 和 `urgent` 通知渠道。
2. Android 13+ 在测试通知前申请并检查 `POST_NOTIFICATIONS`。
3. 用户输入或扫码导入 HTTPS `server_endpoint`、`receiver_id`、`receiver_identity_token`；生产环境拒绝 HTTP，开发模式才允许 localhost 或内网 HTTP。
4. 用户显式开启接收；App 启动前台服务并以 receiver identity token 请求 `GET /api/v1/receivers/{receiver_id}/stream`，`Accept: text/event-stream`。
5. 服务端校验 receiver 存在、allowlisted/enabled 与 identity token hash 匹配；同一 receiver 的新 SSE 连接替换旧连接。
6. 连接建立后调用 `POST /api/v1/receivers/{receiver_id}/test`，同样使用 receiver identity token；验收只要求标准 Android 通知可见。

App 在首次绑定、用户开启接收、网络错误、心跳超时、启动/回到前台、切换 profile、清除数据或卸载重装后重新建立 SSE 连接。客户端只使用指数退避重连；本地禁用 profile 时必须停止前台服务并同步请求服务端禁用 receiver。服务端的 `enabled/revoked/allowlisted` 优先于 App 本地设置。

### 通知权限与渠道

Android 端至少实现：

- Android 13+ `POST_NOTIFICATIONS` 权限申请、拒绝态展示和再次引导。
- Android 8+ `NotificationChannel`：`default` 普通通知、`urgent` 高时效通知；渠道 ID 发布后保持稳定。
- 应用级通知开关、渠道级开关、锁屏/顶部 heads-up 行为的诊断展示。`priority=high` 映射到 `urgent` 渠道以请求 heads-up；系统和用户设置可以拒绝最终展示。
- 锁屏通知使用 Android 标准可见性控制。包含敏感正文时使用 `VISIBILITY_PRIVATE`，并提供“显示完整正文/隐藏敏感内容”的 App 内偏好；最终锁屏可见性由用户系统设置决定。
- 通知点击后冷启动 App，并把 `data` 中的小载荷交给应用内处理。

自举通道要求用户显式启动前台服务，服务显示低重要性常驻状态通知；不要求电池优化豁免，但必须接受常驻通知、耗电和 HyperOS 自启动/省电策略成本。

### Provider 策略

MVP 定义平台无关 `DeliveryProvider`：

- `SelfHostedSseProvider`：默认 provider，向已认证的在线 SSE 连接发送通知事件。
- `MiPushProvider` 与 `FcmProvider`：后续可选 provider；缺少相应开发者凭据时不启用。

SSE provider 维护在线连接和最后心跳时间；连接断开时不保存离线消息，直接让 webhook 返回 `503 delivery_unavailable`。小米推送/FCM 在后续接入时再定义各自的临时/永久错误分类与重新注册语义。

### HyperOS 超级岛降级边界

标准 Android 通知是无条件基线。HyperOS 2 焦点通知/HyperOS 3 小米超级岛只在以下条件全部成立时启用：

- 应用和场景已通过小米平台准入、签名鉴权、场景审核和上线验证。
- 业务场景有明确开始/结束，不超过 12 小时，非营销、非普通社交、非纯告知。
- 运行时检测设备支持、协议版本匹配、应用焦点通知权限开启、用户通知权限和渠道允许。
- 服务端把该通知映射到预配置且已审核的场景模板；webhook 调用方不能用任意 payload 声明上岛。

降级顺序为：OS3 超级岛 -> OS2 焦点通知 -> 标准 Android 通知。任一条件不满足时必须静默降级为标准通知，并在诊断信息中记录原因。产品文案和验收标准不得承诺“所有 HyperOS/小米手机均上岛”。

### 数据与安全

服务端至少持久化：

- webhook token hash、token ID、enabled/revoked 状态。
- receiver：`receiver_id`、receiver identity token hash、allowlisted/enabled/revoked、provider 类型、SSE 连接状态、最后心跳、last_delivery、last_error。
- notification：`notification_id`、receiver、幂等键、核心内容 hash、状态、TTL、provider message ID/hash、错误分类、创建/更新时间。
- audit：request ID、token ID、receiver ID、provider、状态码、耗时、错误分类。

安全要求：

- 生产接口只接受 HTTPS；所有 token 只经 Bearer header 传输，禁止出现在 URL、query、body 或日志。
- 服务端只存 token hash，比较使用常量时间；下游 provider 凭据只在服务端 secret 配置中。
- receiver identity token 用 Android Keystore 保护的加密存储；SSE 认证 header 不进入日志。
- 按 webhook token、来源 IP、receiver 和全局并发限流；认证失败和 receiver 不可用路径统一节流，避免枚举。
- 日志不得记录 Authorization header、完整正文或任何 token；排障正文只允许截断或 hash，且短留存。

### 验收标准

服务端验收：

- 无 Bearer 或错误 Bearer 返回 `401`；未 allowlist receiver 返回 `403`；未知 receiver 返回 `404`。
- 合法请求返回 `201 accepted`，重复幂等请求返回 `200 duplicate`，同幂等键不同核心内容返回 `409`。
- 超长字段、未知字段、数组 receiver、超过 8 KiB 请求体均被拒绝。
- receiver 离线时返回 `503 delivery_unavailable`；服务端不会将消息伪装为已送达或在 bootstrap 版本持久排队。
- 结构化日志和错误响应不泄露任何凭据。

Android 验收：

- 首次安装能创建渠道、申请通知权限、绑定 profile、由用户显式开启前台服务和 SSE 连接。
- 授权通知后测试通知可见；拒绝权限或关闭渠道时 App 能明确诊断失败原因。
- 收到普通/紧急通知时使用对应渠道；点击通知可冷启动并读取 `data`。
- 对高优先级通知，在允许 heads-up 的渠道与系统设置下验证顶部弹出；在允许锁屏通知的系统设置下验证锁屏可见性与敏感内容隐藏策略。
- 网络错误、EOF 或心跳超时后 App 会以指数退避重建 SSE 连接。
- 本地敏感凭据不以明文 SharedPreferences 或日志形式出现。

集成验收：

- 在至少一台目标 Android 或 HyperOS 真机上验证安装授权、拒绝、渠道关闭、熄屏/Doze 5/30/120 分钟、最近任务划掉、设备重启、网络切换、清除数据/卸载重装、SSE 重连。
- 验证 SSE 为默认送达路径，且 Android 前台服务状态通知始终可见。
- 验证 SSE 心跳、断线重连和强行停止后的恢复边界；强行停止后不承诺自动恢复。
- 未完成小米官方审核的普通 webhook 通知必须降级为标准 Android 通知；如果具备已审核场景，再单独验证超级岛/焦点通知增强。

### 工程切片与依赖顺序

服务端切片：

1. 数据模型与配置：webhook token hash、receiver allowlist、identity token hash、provider endpoint、notification、audit、secret 配置。
2. `POST /api/v1/receivers/{receiver_id}/endpoint` 和 test endpoint：Bearer receiver identity、provider token 注册、enabled/revoked/needs_reregister 状态。
3. `POST /api/v1/notifications`：协议校验、Bearer webhook auth、receiver allowlist、8 KiB 限制、统一错误响应。
4. 幂等、TTL、限流、结构化日志和敏感字段脱敏。
5. `DeliveryProvider` 接口与 `SelfHostedSseProvider` 实现；随后按需补 MiPush/FCM 可选实现。
6. provider 错误分类、短期重试、endpoint 失效标记和最小状态查询/诊断。

Android 切片：

1. profile 存储和配置 UI：`server_endpoint`、`receiver_id`、receiver identity token、enabled、最近测试状态。
2. Android Keystore 加密存储、生产 HTTPS 校验、开发 HTTP 例外。
3. 通知渠道、Android 13+ 权限申请、通知/渠道状态诊断。
4. 前台服务、SSE 认证请求、心跳超时、断线重连、启停与 receiver 状态同步。
5. 通知展示、点击打开、`data` 处理、普通/紧急渠道映射。
6. MiPush/FCM token 获取和注册作为后续可选 provider。
7. HyperOS 能力检测和三段降级诊断；只对已审核场景接入焦点通知/超级岛参数。

集成验证切片：

1. 本地/测试服务端端到端：注册 receiver endpoint，发送合法通知，验证响应和日志脱敏。
2. 错误矩阵：认证、receiver、schema、幂等、限流、payload 过大、provider 不可用。
3. Android/HyperOS 真机送达矩阵：权限、`default`/`urgent` 渠道、顶部 heads-up、锁屏可见性、勿扰、Doze、重启、划掉、网络切换、清除数据、SSE 心跳与重连。
4. SSE 在线、断线、重连和强行停止后的恢复边界；后续再对照 MiPush/FCM provider。
5. 超级岛边界：未审核场景必须标准通知；已审核场景才进入 OS3/OS2 增强验证。
