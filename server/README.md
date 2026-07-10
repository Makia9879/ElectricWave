# Makia 通知器 - Go 服务端

个人自托管的 Android 通知 MVP 服务端。外部系统通过 HTTPS webhook 创建通知，服务端鉴权、校验后经认证 HTTP SSE 下发给 Android 前台服务。本目录是纯 Go 模块（无 CGO，可静态编译为 `linux/amd64`）。

```text
外部 webhook --POST--> /api/v1/notifications --> 服务端 --SSE--> Android 前台服务
```

## 目录结构

```
server/
  cmd/notice/main.go         程序入口：加载配置、seed bootstrap、启动 HTTP、TTL 清理、优雅关闭
  internal/
    config/                  .env + 环境变量加载与校验（自定义加载器，无额外依赖）
    logging/                 slog 结构化日志 + 脱敏辅助
    idgen/                   单调可排序 ULID（notification_id / request_id）
    auth/                    token hash（SHA-256 / HMAC-SHA256 + pepper）+ 常量时间校验
    domain/                  请求类型、字段校验（rune 计数）、核心内容 hash
    store/                   纯 Go SQLite（modernc.org/sqlite）：schema、CRUD、幂等、审计
    hub/                     SSE 连接 hub：同 receiver 新连接替换旧连接、投递 []byte
    ratelimit/               按 key 的 token bucket 限流（golang.org/x/time/rate）
    api/                     4 个路由 + 中间件（request_id / recovery / 审计 / 受信代理 IP）
  Dockerfile                 多阶段，CGO_ENABLED=0 静态构建，alpine 运行时
  .env.example               环境变量样例（无真实值）
```

## 接口

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 无 | 健康检查，返回 `{"status":"ok"}`，不泄露配置。 |
| POST | `/api/v1/notifications` | webhook token | 创建并投递通知。在线→201；离线→503。 |
| GET | `/api/v1/receivers/{receiver_id}/stream` | receiver identity token | SSE 流，每 30s 心跳，新连接替换旧连接。 |
| POST | `/api/v1/receivers/{receiver_id}/test` | receiver identity token | 在线时投递一条测试通知。 |

统一错误格式：`{"error":{"code":"<code>","message":"<msg>"}}`。

## 关键设计

- **凭据只存 hash**：webhook token 与 receiver identity token 以 SHA-256（可选 HMAC-SHA256 + pepper）hex 落库，比较用 `crypto/subtle` 常量时间。原始 token 永不下发、不入日志、不入审计。
- **幂等**：作用域 `webhook_token_id + receiver_id + idempotency_key`，保留 24 小时。核心内容 hash = SHA-256 of 稳定 JSON `{title, body, priority, group_key, data}`（不含 ttl/idempotency_key/receiver_id）。同键同内容→200 duplicate；同键不同内容→409。查插在单连接事务内完成，并发重复请求不会重复创建或重复下发。
- **离线语义**：receiver 无 SSE 连接时 webhook 返回 `503 delivery_unavailable`，不伪造送达、不持久排队。已持久化的重复请求即便当前离线也返回 200 duplicate。
- **限流**：按 webhook token、来源 IP、receiver_id 三维度 token bucket（内存），命中返回 429 + `Retry-After`。
- **SSE hub**：每个 receiver 至多一条连接；新连接取消旧连接 context。心跳为 SSE 注释 `: heartbeat\n\n`，立即 flush。
- **审计**：每请求记录 request_id、token_id、receiver_id、provider、状态码、错误分类、耗时，落库 `audit` 表。
- **受信代理**：仅当 `RemoteAddr` 属于 `TRUSTED_PROXY_ADDRS` 时才读 `X-Real-IP`/`X-Forwarded-For`，否则用 `RemoteAddr`。

## 构建与测试

```sh
cd server

# 本机构建
go build ./...

# 静态交叉编译（与 Dockerfile 一致，VPS 适配）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o notice ./cmd/notice

# 静态检查与全量测试（含竞态）
go vet ./...
go test ./... -race
```

依赖说明：`modernc.org/sqlite` 是纯 Go 的 SQLite（无 CGO），`github.com/oklog/ulid/v2` 生成单调 ID，`golang.org/x/time/rate` 提供令牌桶。go.mod 要求 `go 1.25`（本机 1.24 会由 `GOTOOLCHAIN=auto` 自动下载对应工具链）。

## 运行

复制并填写 `.env`（**不要提交**），用随机长串作为 token：

```sh
cp .env.example .env
# 编辑 .env，至少填入：
#   BOOTSTRAP_WEBHOOK_ACCESS_TOKEN=$(openssl rand -hex 32)
#   BOOTSTRAP_RECEIVER_IDENTITY_TOKEN=$(openssl rand -hex 32)
go run ./cmd/notice
```

服务监听 `:8788`（默认）。生产部署：容器仅发布 `127.0.0.1:8788:8788`，由宿主机 Nginx 反代并终止 TLS；公网只开放 80/443。

## 环境变量

见 `.env.example`。必填：`BOOTSTRAP_WEBHOOK_ACCESS_TOKEN`、`BOOTSTRAP_RECEIVER_IDENTITY_TOKEN`。启动时把 bootstrap webhook token（`token_id=bootstrap`）与 bootstrap receiver 写入/更新到 DB。

## Docker

```sh
docker buildx build --platform linux/amd64 -t makia-notice:latest -f Dockerfile .
# 运行（token 通过环境变量注入，不挂载 .env 文件）
docker run --rm -p 127.0.0.1:8788:8788 \
  -e BOOTSTRAP_WEBHOOK_ACCESS_TOKEN=... \
  -e BOOTSTRAP_RECEIVER_IDENTITY_TOKEN=... \
  -v notice-data:/data \
  makia-notice:latest
```

镜像基于 alpine，含 `ca-certificates`，`HEALTHCHECK` 用 `wget` 探测 `/healthz`，以非 root 用户 `app` 运行。

## 部署（概要）

完整部署协议见仓库 `docs/architecture/cloudflare-nginx-http-server.md` 与 `docs/deployment/notice-service-tls-runbook.md`。要点：

- 通知服务容器监听 `:8788`，Docker 仅映射 `127.0.0.1:8788:8788`。
- Nginx 反代 webhook / SSE / test / healthz；SSE 必须 `proxy_buffering off`、`proxy_read_timeout` ≥ 75s。
- `notice.makia98.com` Cloudflare 为 DNS only，TLS 由 VPS Nginx + Let's Encrypt 提供。

## 安全边界

- 生产接口只接受 HTTPS（由 Nginx 保证）；token 仅经 `Authorization: Bearer` 传输，禁止出现在 URL、query、body 或日志。
- 日志采用结构化最小化记录，绝不输出 Authorization 头、完整 token 或完整正文。
- 单一 bootstrap receiver 由本地配置初始化；App 不得绕过 identity token 自注册。
