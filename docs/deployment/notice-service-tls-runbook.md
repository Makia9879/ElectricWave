# 通知服务 TLS 部署清单

本文档不记录具体主机、运维时间或证书有效期。

本清单只覆盖 `notice.example.com` 的 TLS、Nginx 与 Certbot 准备。它不创建通知服务容器，不生成业务 token，也不替换已有站点。

## 当前已部署状态

- `notice.example.com` 为 Cloudflare DNS only A 记录，指向 `203.0.113.10`。
- Let's Encrypt 证书已签发；有效期以现场 `certbot certificates` 输出为准。
- Nginx 已启用 `80 -> 443` 跳转；当前 HTTPS 根路径返回 `404`，等待通知服务容器上线。
- 远程用户 crontab 定期执行 `$DEPLOY_ROOT/nginx/renew-letsencrypt.sh`；具体调度时间不写入文档。

## 前置确认

- Cloudflare DNS 中 `notice.example.com` 的 A 记录为 `203.0.113.10`，状态为 **DNS only**（灰云）。
- VPS 的公网 `80/tcp` 和 `443/tcp` 可达；`8788/tcp` 不开放；`<SSH_PORT>/tcp` 仅管理 IP 可访问。
- Let's Encrypt 注册邮箱仅在远程签发命令中临时传入，不写入仓库、Compose 文件或 Nginx 配置。
- 现有 Nginx 容器名为 `nginx`，使用 host 网络，并由 `$DEPLOY_ROOT/nginx/docker-compose.yaml` 管理。

## 远程目录与挂载

在 `$DEPLOY_ROOT/nginx/` 下创建仅供 Certbot 使用的目录：

```text
certbot/conf/  # Let's Encrypt account、证书与续期状态
certbot/www/   # HTTP-01 challenge webroot
```

在 Nginx Compose 中新增两个只读挂载：

```yaml
- ./certbot/conf:$LETSENCRYPT_DIR:ro
- ./certbot/www:$CERTBOT_WEBROOT:ro
```

证书私钥只保留在远程 `certbot/conf`，不上传到工作区。

## 首次签发顺序

1. 新增仅 HTTP 的 `notice.example.com.conf`：`/.well-known/acme-challenge/` 直接读取 `$CERTBOT_WEBROOT`；其他路径不代理到应用。
2. 用 `docker exec nginx nginx -t` 校验后 reload Nginx。
3. 使用 `certbot/certbot` 容器执行 webroot 的 `certonly`，签发 `notice.example.com` 证书。
4. 将该虚拟主机扩展为 `80 -> 443` 跳转和 TLS server，证书路径固定为：
   ```text
   $TLS_CERT_FILE
   $TLS_KEY_FILE
   ```
5. 再次校验 Nginx 后 reload，验证 HTTPS 证书链和 HTTP 跳转。
6. 通知服务实现完成后，才添加 `127.0.0.1:8788` 的 webhook、SSE 与 test 路由代理。

完整虚拟主机与 SSE 指令见[Cloudflare DNS 与 Nginx HTTP 服务设计](../architecture/cloudflare-nginx-http-server.md)。

## 自动续期

使用每日 cron 或 systemd timer 执行 Certbot Docker 容器的 `renew --webroot`。只在实际续期成功时运行：

```text
docker exec nginx nginx -s reload
```

续期任务必须沿用同一 `certbot/conf` 和 `certbot/www` 挂载。部署完成后先执行一次 `certbot renew --dry-run` 验证自动续期，不等待证书临近到期。

## 验收

1. Let's Encrypt HTTP-01 校验成功，证书 Subject/SAN 包含 `notice.example.com`。
2. `http://notice.example.com/anything` 返回至对应 HTTPS URL；ACME challenge 路径不跳转。
3. `https://notice.example.com/` 在通知服务尚未部署时返回 `404`，不暴露默认站点内容。
4. `certbot renew --dry-run` 成功，Nginx reload 后仍通过配置校验。
5. 完成通知服务部署后，SSE 连接持续 5 分钟，并每 30 秒收到心跳。
