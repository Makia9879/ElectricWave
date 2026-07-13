# Cloudflare DNS 与 Nginx HTTP 服务设计

更新日期：2026-07-10

## 决策

生产链路固定为：

```text
外部 webhook / Android App
  -> Cloudflare DNS（DNS only）
  -> VPS Nginx（Let's Encrypt TLS、路径路由、SSE 代理）
  -> 通知服务容器（HTTP，仅宿主机回环端口）
```

通知服务不自行处理公网 TLS，不直接暴露宿主机端口，也不信任 Cloudflare header 作为业务身份来源。应用内仍由 Bearer token、receiver allowlist 和 identity token 执行业务鉴权。

对外域名固定为 `notice.example.com`。`PUBLIC_BASE_URL` 固定为 `https://notice.example.com`；Android 生产 profile 只接受该 HTTPS 地址。

## 端口与 Docker 网络

远程主机为 `linux/amd64`。通知服务容器使用 Docker bridge 网络，容器内监听：

```text
HTTP_ADDR=:8788
```

Docker 仅发布宿主机回环端口：

```text
127.0.0.1:8788:8788
```

现有 Nginx 容器使用 host 网络，因此通过 `http://127.0.0.1:8788` 访问通知服务。VPS 防火墙和云安全组不得开放 `8788/tcp`；公网仅开放 `443/tcp`，`80/tcp` 只用于跳转 HTTPS。

## 应用 HTTP 服务器

应用只提供 HTTP/1.1 语义，TLS 在 Nginx 终止。最小生产配置：

```dotenv
HTTP_ADDR=:8788
PUBLIC_BASE_URL=https://notice.example.com
TRUSTED_PROXY_ADDRS=127.0.0.1,::1
DELIVERY_PROVIDER=self_hosted_sse
SSE_HEARTBEAT_INTERVAL_SECONDS=30
```

规则：

- 应用只在远端连接地址属于 `TRUSTED_PROXY_ADDRS` 时读取 `X-Forwarded-Proto` 和 `X-Real-IP`；其他来源一律忽略这些 header。
- Nginx 覆盖而非透传 `X-Forwarded-Proto`、`X-Real-IP`、`X-Forwarded-For` 和 `Host`。应用不信任客户端自行发送的同名 header。
- 生产请求的外部 scheme 必须为 `https`。`/healthz` 仅作为容器健康检查使用，不作为公开 API 承诺。
- 公共 API 只暴露既定的 `/api/v1/notifications`、receiver SSE stream 与 receiver test 路由；其余路径返回 `404`。
- 应用继续禁止 URL、query、日志包含 token；Nginx 访问日志使用 `$uri` 而不是 `$request_uri`，避免记录 query。

## Cloudflare DNS 与 Let's Encrypt

- `notice.example.com` 的 A 记录指向 `203.0.113.10`，并保持 **DNS only**（灰云）。
- Cloudflare 免费套餐仅作为权威 DNS；Cloudflare SSL/TLS encryption mode、Cache Rule、Cloudflare Origin Certificate 和 Cloudflare IP 白名单均不适用于此链路。
- VPS Nginx 通过 Let's Encrypt 的 HTTP-01 challenge 获取公信 TLS 证书。`80/tcp` 与 `443/tcp` 必须允许公网访问；SSH `<SSH_PORT>/tcp` 仍应按管理 IP 限制。
- 证书保存于 `$DEPLOY_ROOT/nginx/certbot/conf`，Nginx 以只读方式挂载并使用 `fullchain.pem` 与 `privkey.pem`。
- Certbot 使用 webroot `$DEPLOY_ROOT/nginx/certbot/www`，Nginx 在 `/.well-known/acme-challenge/` 直接提供该目录，不代理至应用。

网络切换或源站重启导致 SSE 重建是正常故障模型，Android 前台服务按既定指数退避策略重连。30 秒 SSE 注释心跳必须持续发送，以避免 Nginx 和中间代理将空闲连接关闭。

## Nginx 虚拟主机

现有 `nginx` 容器使用 host 网络，并只读挂载 `$NGINX_CONFIG_DIR`。新增独立的示例站点配置，不修改其他已有站点。

证书与 ACME webroot 应单独挂载：

```text
$DEPLOY_ROOT/nginx/certbot/conf:$LETSENCRYPT_DIR:ro
$DEPLOY_ROOT/nginx/certbot/www:$CERTBOT_WEBROOT:ro
```

HTTPS server 的核心策略如下。下面是部署蓝图，不在文档阶段写入远程配置：

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name notice.example.com;

    location ^~ /.well-known/acme-challenge/ {
        root $CERTBOT_WEBROOT;
        default_type text/plain;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name notice.example.com;

    ssl_certificate     $TLS_CERT_FILE;
    ssl_certificate_key $TLS_KEY_FILE;

    client_max_body_size 64k;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

    location = /api/v1/notifications {
        proxy_pass http://127.0.0.1:8788;
        proxy_http_version 1.1;
        proxy_request_buffering on;
    }

    location ~ ^/api/v1/receivers/[^/]+/stream$ {
        proxy_pass http://127.0.0.1:8788;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 75s;
        proxy_send_timeout 75s;
        add_header X-Accel-Buffering no always;
    }

    location ~ ^/api/v1/receivers/[^/]+/test$ {
        proxy_pass http://127.0.0.1:8788;
        proxy_http_version 1.1;
    }

    location / {
        return 404;
    }
}
```

Nginx 不读取或转发 `CF-Connecting-IP`。应用仅信任来自本机 Nginx 的 `X-Forwarded-*` header，并以 Nginx 写入的 `X-Real-IP` 用于来源 IP 限流和审计。

Certbot 以 Docker 容器运行，签发后配置每日 `certbot renew`，仅在续期成功时执行 `docker exec nginx nginx -s reload`。Let's Encrypt 注册邮箱仅用于账户与过期告警，禁止写入仓库或日志。

## 验收

1. `http://notice.example.com/.well-known/acme-challenge/<token>` 可由 Let's Encrypt 直接访问；其他 HTTP 路径跳转到 HTTPS。
2. `curl -N` 到已认证 SSE 路由能立即收到响应头，并每 30 秒收到注释心跳；持续 5 分钟无缓冲聚合或超时。
3. 正常 webhook 请求经 `443` 到达应用；`Authorization` 不出现在 Nginx 或应用日志。
4. 重启通知服务容器会令 SSE 断开；Android 按退避重连后恢复。
5. `127.0.0.1:8788` 在 VPS 本机可达，外部网络不可达。
