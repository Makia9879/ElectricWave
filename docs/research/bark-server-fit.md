# Finb/bark-server 服务端适配性调研

> 调研日期：2026-07-10  
> bark-server 主分支基线：[`3df8990fcbc407a3f5638eea8cedc3289d1a405d`](https://github.com/Finb/bark-server/tree/3df8990fcbc407a3f5638eea8cedc3289d1a405d)（2026-07-07）  
> Bark iOS 客户端参考基线：[`d8766ea6b95c121625d8dc2a7dda47401b427be3`](https://github.com/Finb/Bark/tree/d8766ea6b95c121625d8dc2a7dda47401b427be3)（2026-06-24）  
> 最新正式版：[`v2.3.5`](https://github.com/Finb/bark-server/releases/tag/v2.3.5)（2026-05-21）

## 1. 结论

**bark-server 已具备本项目服务端的一部分骨架能力，但不能原样满足面向小米/HyperOS Android 手机的 MVP。**

- 对 `webhook -> 服务器`：复用度高。已有 GET/POST 推送 API、单设备/批量寻址、设备标识到推送令牌的持久化映射、同步投递、Docker/二进制/Helm 部署。
- 对“webhook 访问令牌”：已有可选的全局 Basic Auth，可作为单一共享密钥的临时替代；但没有独立 Bearer/API token 模型、token 生命周期、权限范围或多调用方管理。
- 对“接收端白名单”：数据库中存在的 `device_key` 可近似视为可投递集合，但 `/register` 默认公开且被 Basic Auth 明确豁免，因此它不是管理员控制的白名单。
- 对“手机配置通知发布点与接收端身份令牌”：Bark iOS 客户端原生支持配置服务器地址，并注册 APNs device token 后取得 `device_key`。这与目标概念相近，但 `device_key` 同时充当公开推送地址中的 capability secret，并不是独立的接收端认证凭据。
- 对 `服务器 -> HyperOS Android 手机`：不具备。服务端只实现 APNs，APNs topic、Team ID、Key ID 和私钥均绑定 Bark iOS；Android/HyperOS 没有对应客户端或投递通道。此部分是大改。

因此推荐结论是：

1. **若接收端是官方 Bark iOS App**：可以复用 bark-server，经过配置和小到中等安全改造即可满足静态架构。
2. **若接收端是本项目自研 HyperOS Android App**：只建议复用其 HTTP API、Go/Fiber 工程骨架、设备映射和部署经验；需要重做设备注册安全模型和整个下行投递层。是否 fork 应在 Android 下行协议确定后再决定。
3. **当前不建议直接把 bark-server 镜像作为本项目正式服务端**。它会让“API 能收到请求”看似完成，但核心的 Android 到达率、后台保活和灵动岛展示链路仍未解决。

## 2. 评级口径

| 评级 | 含义 |
|---|---|
| 原生已有 | 当前主分支直接提供，语义基本符合 |
| 配置即可 | 不改源码，通过现有参数或标准反向代理即可实现 |
| 小改 | 可在现有路由、存储或中间件边界内补齐，不需要替换核心投递机制 |
| 大改 | 需要新增平台协议、重构核心数据模型或客户端配套 |
| 不适用 | Bark 的实现只服务于 iOS/APNs，不能迁移为 Android 能力 |

## 3. 能力矩阵

| 本项目要求/关注点 | Bark 当前能力 | 评级 | 判断 |
|---|---|---|---|
| 外部 HTTP/webhook 调用 | `/push` JSON POST；兼容 `/:device_key/...` GET/POST；支持 `device_keys` 批量 | 原生已有 | 可直接作为“外部系统调用服务器”的入口 |
| webhook 访问令牌 | 可选全局 Basic Auth，单用户名/密码 | 配置即可（临时）/小改（正式） | 把固定用户名和随机密码当共享 token 可临时使用；正式接口应增加 `Authorization: Bearer` 或 `X-Webhook-Token` |
| 外部无 token 不得发送 | 启用 Basic Auth 后推送路由受保护 | 配置即可 | 对单实例全局共享凭据成立，但并非 API token 语义 |
| 接收端身份标识 | `device_key -> device_token` 映射 | 原生已有（iOS） | `device_key` 可作为逻辑接收端 ID；`device_token` 实际是 APNs token |
| 接收端白名单 | 只有数据库中可查到的 key 才能推送 | 小改 | 当前公开注册可增加条目，缺少管理员审批、enabled/revoked 状态和策略 |
| 手机配置发布点 | Bark iOS 可保存多个 server address | 原生已有（iOS）/不适用（Android） | 自研 Android App 需另行实现 |
| 手机配置接收端身份令牌 | Bark iOS 向 `/register` 上传 APNs token，服务端返回 key | 原生已有（iOS）/大改（Android） | 目标若要求预置 receiver token 鉴权，现模型还需拆分“身份凭据”和“推送地址” |
| 数据持久化 | 默认 bbolt；可选 MySQL；serverless 环境变量单设备模式 | 原生已有 | 适合简单设备映射，不含权限、调用方、审计实体 |
| APNs 路由 | APNs HTTP/2、client pool、token auth、失效 token 处理 | 原生已有（iOS）/不适用（Android） | 完全绑定 Bark bundle/topic 和 APNs |
| Android/HyperOS 下行 | 无 FCM、厂商推送、WebSocket、SSE、MQTT 或长轮询实现 | 大改 | 必须先确定 Android 下行策略 |
| 限流 | 仅批量条数上限、连接并发和超时参数 | 小改 | 没有按 webhook token/IP/receiver 的请求速率限制 |
| 审计 | stdout 请求日志 | 小改 | 没有结构化持久审计；现日志还会记录 URL 和 body，需做敏感字段脱敏 |
| 消息队列、重试、幂等 | APNs 同步调用；`id` 被用作 APNs Collapse ID | 大改（若要求可靠投递） | 没有 durable queue、重试状态、投递记录或 webhook 幂等键 |
| 部署 | Docker、GHCR/Docker Hub、二进制、systemd、Helm、TLS、Unix socket | 原生已有 | 部署成熟度可复用 |

## 4. 源码证据

### 4.1 请求 API

V2 提供 `POST /push`，兼容 API 还提供多种将 `device_key` 放入路径的 GET/POST 路由：

- [`route_push.go#L19-L38`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_push.go#L19-L38)
- 官方 V2 字段和 curl 示例：[`docs/API_V2.md#L20-L59`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/docs/API_V2.md#L20-L59)

JSON V2 支持 `device_keys` 数组或逗号分隔字符串，按设备并发调用同一个 `push()`：[`route_push.go#L92-L175`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_push.go#L92-L175)。这已经覆盖单接收端和批量接收端的基础寻址。

它还提供 `/mcp` 和 `/mcp/:device_key`，最终也调用相同的 `push()`：[`route_mcp.go#L18-L36`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_mcp.go#L18-L36)、[`route_mcp.go#L73-L100`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_mcp.go#L73-L100)。这不是本项目 MVP 必需能力，若启用统一鉴权需避免遗漏该入口。

### 4.2 device key/token 与注册模型

注册 API 为：

- `POST /register`
- `GET /register/:device_key` 检查
- 兼容 `GET /register`

证据：[`route_register.go#L17-L27`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_register.go#L17-L27)。

请求接受 `device_key` 和 `device_token`（兼容字段为 `key`、`devicetoken`），保存后把 `key`、`device_key`、`device_token` 全部返回：[`route_register.go#L8-L15`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_register.go#L8-L15)、[`route_register.go#L29-L72`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_register.go#L29-L72)。

推送时，服务端只验证 key 能否在数据库查到 token，然后直接交给 APNs：[`route_push.go#L250-L275`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_push.go#L250-L275)。因此：

- `device_key` 是服务器侧的逻辑寻址键，也是一种“知道即可发送”的 capability secret。
- `device_token` 是 Apple APNs 分配给 App 实例的 token，不是 Bark 自己签发的接收端身份令牌。
- 当前模型没有单独的 receiver credential、注册挑战、设备证明、所有者或租户关系。

默认 bbolt 在 key 为空或 key 不存在时生成 short UUID：[`database/bbolt.go#L72-L92`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/bbolt.go#L72-L92)。MySQL 则只在 key 为空时生成，非空 key 会直接 upsert：[`database/mysql.go#L114-L125`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/mysql.go#L114-L125)。两个后端对“客户端提交一个尚不存在的 key”语义并不一致，改造 receiver identity 时应先统一。

### 4.3 鉴权和访问控制

原生鉴权是可选的单用户 Basic Auth：[`route_auth.go#L12-L34`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_auth.go#L12-L34)。启动参数通过 `BARK_SERVER_BASIC_AUTH_USER` 和 `BARK_SERVER_BASIC_AUTH_PASSWORD` 配置：[`main.go#L265-L276`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/main.go#L265-L276)。

重要限制：`/ping`、`/register`、`/healthz` 被明确豁免，其中判断使用前缀匹配：[`route_auth.go#L19-L29`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/route_auth.go#L19-L29)。所以即使开启 Basic Auth，任何能访问服务端的人仍可调用注册接口。

这意味着：

- “外部必须有 webhook token 才能发送”可用 Basic Auth 临时实现。
- “只有白名单接收端可以注册/接收”不能仅靠现有配置实现。
- 如果用反向代理实现 Bearer token，必须同时明确 `/push`、兼容推送路径和 `/mcp` 的暴露策略，不能只保护 `/push`。

### 4.4 存储与白名单

存储接口仅有设备计数、key 查 token、保存 key/token、删除设备：[`database/database.go#L3-L11`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/database.go#L3-L11)。

- 默认 bbolt 的 bucket 只保存 `key -> token`：[`database/bbolt.go#L21-L23`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/bbolt.go#L21-L23)、[`database/bbolt.go#L51-L69`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/bbolt.go#L51-L69)。
- MySQL `devices` 表也只有 `id`、`key`、`token`：[`database/mysql.go#L21-L30`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/mysql.go#L21-L30)。
- serverless 模式可通过 `BARK_KEY`、`BARK_DEVICE_TOKEN` 固定单个映射：[`database/envbase.go#L15-L34`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/database/envbase.go#L15-L34)。这可视为“单接收端静态白名单”的配置型特例，但仍是 APNs token。

没有 `enabled`、`revoked_at`、`platform`、`owner`、`created_at`、`last_seen_at`、注册凭证或审计字段。因此多接收端白名单应扩展数据模型，不能把“数据库里存在”直接宣称为完整白名单能力。

### 4.5 APNs 路由及 iOS 耦合

投递实现硬编码为 Apple APNs：

- topic 固定为 `me.fin.bark`，Key ID 和 Team ID 固定：[`apns/apns.go#L41-L46`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/apns/apns.go#L41-L46)。
- APNs token auth 和生产环境 host：[`apns/apns.go#L55-L104`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/apns/apns.go#L55-L104)。
- 最终构造 `apns2.Notification`，使用 device token、固定 topic、4KB 载荷和 alert/background push type：[`apns/apns.go#L107-L149`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/apns/apns.go#L107-L149)。
- Bark APNs 私钥直接编译在源码中：[`apns/apns_certs.go#L1-L10`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/apns/apns_certs.go#L1-L10)。

Bark iOS 客户端也验证了这套耦合：

- iOS 从系统取得 APNs device token 后同步所有服务器：[`Bark/AppDelegate.swift#L106-L112`](https://github.com/Finb/Bark/blob/d8766ea6b95c121625d8dc2a7dda47401b427be3/Bark/AppDelegate.swift#L106-L112)。
- 注册请求把 `devicetoken` 和可选 `key` 发到服务器 `/register`：[`Common/Moya/BarkApi.swift#L11-L50`](https://github.com/Finb/Bark/blob/d8766ea6b95c121625d8dc2a7dda47401b427be3/Common/Moya/BarkApi.swift#L11-L50)。
- 客户端保存 server address 和 key，并可同步多个服务器：[`Common/ServerManager.swift#L13-L30`](https://github.com/Finb/Bark/blob/d8766ea6b95c121625d8dc2a7dda47401b427be3/Common/ServerManager.swift#L13-L30)、[`Common/ServerManager.swift#L136-L187`](https://github.com/Finb/Bark/blob/d8766ea6b95c121625d8dc2a7dda47401b427be3/Common/ServerManager.swift#L136-L187)。

所以“通知发布点”概念可以借鉴，但 Android 无法使用 APNs device token，也不能接收 topic 为 `me.fin.bark` 的通知。仅替换 App UI 或请求 URL不能解决该问题。

### 4.6 部署、限流与审计

官方 README 提供 Docker、Compose、预编译二进制和 MySQL 配置：[`README.md#L9-L67`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/README.md#L9-L67)。仓库还包含 Helm chart，最新 release `v2.3.5` 提供多平台二进制和部署文件。

现有限制手段包括：

- 批量推送条数上限 `BARK_SERVER_MAX_BATCH_PUSH_COUNT`：[`main.go#L283-L289`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/main.go#L283-L289)。
- APNs client 数量：[`main.go#L290-L296`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/main.go#L290-L296)。
- Fiber 全局并发和读写超时：[`main.go#L297-L324`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/main.go#L297-L324)。

这些不是按调用方的限流。源码未发现 Fiber limiter、token bucket 或持久配额实现。

请求日志会输出 IP、状态、方法、耗时、路由、完整 URL 和 body：[`router.go#L69-L76`](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/router.go#L69-L76)。它可用于基本运维，但不等于审计：没有 caller ID、receiver ID、投递结果记录、事件 ID 或留存策略。若 token/device key 放在 URL、query 或 body 中，还会被日志记录。

## 5. 如何改造成目标服务端

### 5.1 最小安全改造（仍使用 Bark iOS/APNs）

适合快速验证 `webhook -> server -> Bark iOS`，不代表 HyperOS MVP 完成。

1. 关闭或限制兼容 GET 推送路由，统一使用 `POST /v1/notifications`。
2. 增加 webhook 专用 Bearer token 中间件；常量时间比较，token 仅保存 hash，拒绝 query token。
3. 将 `/register` 改为受控注册：要求 receiver identity token，或只允许管理员预置 receiver。
4. 将设备表扩展为 `receiver_id / endpoint_token / platform / enabled / created_at / last_seen_at`；推送前必须检查 `enabled`。
5. 增加按 webhook token、IP 和 receiver 的限流，设置 body 大小、批量数和并发上限。
6. 增加结构化审计，记录 request ID、caller、receiver、结果、耗时；不记录 access token、APNs token 和完整消息正文。
7. 生产环境通过反向代理终止 TLS，并只暴露必要路由。

其中 2 至 6 均可围绕现有 Fiber 中间件和 Database 接口完成，属于小到中等改造。

### 5.2 HyperOS Android 必需改造

在上述安全改造之外，还必须：

1. 定义平台无关的 `DeliveryProvider`，把当前 `apns.Push()` 从业务路由中抽离。
2. 选择 Android 下行协议：厂商推送/FCM、自建长连接（WebSocket/MQTT）或受系统限制的轮询。该选择取决于 HyperOS 后台限制与目标设备环境，不能由 bark-server 现状代替。
3. 将 `device_token` 泛化为 endpoint，并保存平台、App 实例、公钥/凭据、最近在线时间及 token 轮换状态。
4. 在 Android App 实现服务器地址配置、receiver identity 安全保存、注册/续期/撤销和通知展示。
5. 若要求可靠投递，增加消息表、durable queue、重试退避、幂等键、过期时间和 delivery receipt。

这是核心路径重构，评级为“大改”。bark-server 的 APNs 包可以保留为未来 iOS provider，但不能作为 Android provider。

## 6. 推荐的目标接口边界

建议不要继续复用 `/:device_key/:body` 作为正式 webhook。可保留 Bark 兼容层，但新接口至少分成三个安全域：

```text
POST /v1/notifications
Authorization: Bearer <webhook-access-token>
{
  "receiver": "receiver-id",
  "title": "...",
  "body": "...",
  "idempotency_key": "..."
}

POST /v1/receivers/register
Authorization: Bearer <receiver-identity-token>
{
  "platform": "android",
  "endpoint": "..."
}

POST /v1/admin/receivers/{id}/revoke
Authorization: Bearer <admin-token>
```

这里必须保持三个概念独立：

- webhook access token：谁能发送。
- receiver ID/白名单状态：能发给谁。
- receiver identity token：手机是否有权注册或更新该 receiver 的 endpoint。

Bark 当前 `device_key` 把后两者和“推送 URL 秘密”部分混在一起，这是改造时最需要拆开的地方。

## 7. 采用决策

### 建议：有条件复用，不直接采用

可复用：

- Go + Fiber 的轻量服务骨架。
- JSON 推送参数解析及单/批量接收端寻址思路。
- Database 接口、bbolt/MySQL 双后端的基础实现。
- Docker、二进制、systemd、Helm 部署资产。
- APNs provider（仅未来需要兼容 iOS 时）。

不应原样复用：

- 公开且免鉴权的 `/register`。
- 把 `device_key` 当唯一访问控制的 URL 模式。
- 硬编码 Bark APNs 身份和 topic 的投递层。
- 包含 URL/body 的默认日志格式。
- 无速率限制、无审计、无可靠队列的生产暴露方式。

**最终判断：bark-server 证明了该静态架构的“服务器入口 + 接收端映射 + 平台推送”形态可行，也能节省部分服务端工程工作；但它不是 Android/HyperOS 通知服务器。若 Android 调研最终选择厂商推送或长连接，建议比较“在 bark-server 上抽象 provider”与“新建更小的服务”两条方案，按需复用代码，而不是先 fork 再被 APNs 数据模型牵制。**

## 8. 验证记录与局限

- 已从 GitHub 实时确认主分支 HEAD 为 `3df8990fcbc407a3f5638eea8cedc3289d1a405d`。
- 已通过 GitHub Releases API 确认最新正式版为 `v2.3.5`，发布时间 2026-05-21。
- 在隔离的临时 Go cache 下执行 `go build ./...`、`go test ./database ./apns`，均通过。
- 未执行真实 APNs 推送，因为需要有效 Apple device token，且会产生外部通知副作用。
- “Android/HyperOS 不支持”的判断针对 bark-server 与官方 Bark 客户端当前源码；具体 HyperOS 下行方案需由 Android 后台与通知能力调研另行确定。

## 9. 主要一手资料

- [Finb/bark-server 主仓库](https://github.com/Finb/bark-server)
- [bark-server README（固定提交）](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/README.md)
- [bark-server API V2（固定提交）](https://github.com/Finb/bark-server/blob/3df8990fcbc407a3f5638eea8cedc3289d1a405d/docs/API_V2.md)
- [bark-server v2.3.5 release](https://github.com/Finb/bark-server/releases/tag/v2.3.5)
- [Finb/Bark iOS 客户端（固定提交）](https://github.com/Finb/Bark/tree/d8766ea6b95c121625d8dc2a7dda47401b427be3)
