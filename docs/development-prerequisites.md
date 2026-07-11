# 开发前置条件

更新日期：2026-07-11

本文件把进入 MVP 实现前的依赖分成“已准备”“我可继续处理”和“需要你完成”。正式规格见[汇总 MVP 规格与实施切片](agents/issues/0008-define-mvp-spec-and-implementation-slices.md)。

## 已准备

- 已确定个人 MVP 主链路为 `webhook -> 自建服务端 -> HTTP SSE -> Android 前台服务 -> 原生通知`；不依赖小米开发者身份。
- 应用名称已确定为 `ElectricWave`，Android 包名固定为 `com.makia98.electricwave`。
- 已固定 webhook 与接收端协议、安全边界、Android 配置模型和验收矩阵。
- 已定义未来服务端所需的环境变量边界，暂不创建服务端配置文件或实现。
- 已创建根目录 `.gitignore`，阻止 `.env`、Android 签名文件、Gradle 构建产物和本地服务端数据被提交。
- 本机已检测到 Go `1.24.0` 与 Docker `29.4.0`。
- 已安装 Android SDK Command-line Tools、`platform-tools`、Android 35 platform 与 build-tools `35.0.0`，目录为 `/opt/homebrew/share/android-commandlinetools`。
- 已安装 Temurin JDK `17.0.19`。已在 [scripts/android-env.sh](../scripts/android-env.sh) 固定 `JAVA_HOME` 至 JDK 17；执行 `source scripts/android-env.sh` 后可使用 `adb`、`sdkmanager` 与 JDK 17。
- 已完成一台真机的无线 ADB 配对：设备型号 `2509FPN0BC`（`popsicle`）、Android `16`、HyperOS `3.0`。后续 MVP 验收以该设备为首个基线。
- 已确定远程部署主机为 `mk@cloud.makia98.com`（SSH：`ssh mk@cloud.makia98.com -p5022`），使用 Docker 部署；本地构建镜像可导出为 tar 后上传并在远程导入运行。已验证远程环境为 `linux/amd64`、Docker `26.1.4`，且已有 host-network Nginx 容器。
- `notice.makia98.com` 已配置为 Cloudflare DNS only A 记录，指向 `47.119.174.236`；VPS Nginx 已签发并启用 Let's Encrypt 证书，HTTP 跳转 HTTPS 已验证。
- 2026-07-11 复核：`ssh mk@cloud.makia98.com -p5022` 可连接；远程历史运维名 `notice` 容器运行 `electricwave-server:latest`，仅发布 `127.0.0.1:8788->8788/tcp`，状态 healthy；`notice` 此处不是产品名；`https://notice.makia98.com/healthz` 返回 HTTP 200。
- 2026-07-11 复核：本地无线 ADB 可见 `2509FPN0BC`；使用可写临时缓存后服务端 `go test ./...` 通过，Android `./gradlew testDebugUnitTest` 构建成功但无实际单测。
- Certbot 自动续期已配置为远程用户每日 `03:17` 执行；续期成功时自动 reload Nginx。证书当前有效至 `2026-10-08`，`renew --dry-run` 已通过。

## 本机缺口

| 项目 | 当前状态 | 实现前要求 |
| --- | --- | --- |
| JDK | Temurin JDK `17.0.19` 已安装；默认 shell 仍指向 Oracle JDK 20 | 在仓库中执行 `source scripts/android-env.sh`，使 Android/Gradle 构建固定到 JDK 17。 |
| Android SDK | 已安装，但尚未加入全局 shell `PATH` | 在仓库中执行 `source scripts/android-env.sh`；创建 Android 工程时写入同一 SDK 路径。 |
| Gradle | 未发现 | 不单独安装；创建 Android 工程时使用 Gradle Wrapper。 |
| Android 真机 | 已完成一台 HyperOS 真机的无线 ADB 配对 | 首个基线设备已可用；后续仍应增加至少一台非 HyperOS 或不同 Android 版本设备。 |

Android SDK 与 JDK 17 均已准备完成。默认 shell 的 JDK 20 不应用于 Android 构建。

在 Codex/沙箱环境中验证时，不要使用默认的 `$HOME` 缓存路径；使用 `/private/tmp` 下的可写缓存：

```sh
mkdir -p /private/tmp/electricwave-gomodcache \
  /private/tmp/electricwave-gocache \
  /private/tmp/electricwave-gopath
cd server
GOMODCACHE=/private/tmp/electricwave-gomodcache \
GOCACHE=/private/tmp/electricwave-gocache \
GOPATH=/private/tmp/electricwave-gopath \
go test ./...

cd ..
mkdir -p /private/tmp/electricwave-gradle
source scripts/android-env.sh
cd android
GRADLE_USER_HOME=/private/tmp/electricwave-gradle ./gradlew testDebugUnitTest
```

Android 构建可能尝试写 `/Users/makia/.android/analytics.settings` 或 Kotlin daemon marker；在沙箱中会降级或警告。只要最终 `BUILD SUCCESSFUL`，可作为编译链路通过；但 `testDebugUnitTest NO-SOURCE` 不能当作 Android 行为测试通过。

## 后续条件

### 现在必须提供或操作

1. 通知服务对外地址已固定为 `https://notice.makia98.com`。在应用实现后，将它写入 `PUBLIC_BASE_URL` 与 Android 生产 profile 的 `server_endpoint`。
2. VPS 的 Nginx 已具备 TLS 与 ACME 续期能力；应用容器部署前保持 HTTPS 根路径 `404`，避免默认站点内容泄露到通知域名。

### 暂不阻塞 MVP

- 小米推送与 FCM：均可留空。首版使用自建 HTTP SSE，后续再作为可选 provider 接入。
- HyperOS 焦点通知/小米超级岛：需要小米的专项准入、场景审核、签名和设备白名单。普通 webhook 的标准 Android 通知不依赖它。
- 上架、账号体系、多设备和管理后台：均不在 MVP 前置范围内。

## 密钥与数据规则

- 真实 `.env`、小米密钥、FCM service account、webhook token、receiver identity token 和 Android 签名文件只保留在本机或受控 secret manager。
- 服务端落库时仅保存 webhook token 与 receiver identity token 的 hash；手机端使用 Android Keystore 保护 receiver identity token。
- 不要把 token 放进 URL、提交记录、截图或应用日志。

未来服务端的最小配置项为：`HTTP_ADDR`、`PUBLIC_BASE_URL`、`DELIVERY_PROVIDER=self_hosted_sse`、`SSE_HEARTBEAT_INTERVAL_SECONDS`、`BOOTSTRAP_WEBHOOK_ACCESS_TOKEN`、`BOOTSTRAP_RECEIVER_ID`、`BOOTSTRAP_RECEIVER_IDENTITY_TOKEN`。这些值仅在进入实现阶段时写入未提交的本地 `.env`。

## 推荐推进顺序

1. 我检查远程主机的 Docker、CPU 架构、端口与现有反向代理；随后确认实际 HTTPS 入口。
2. 在仓库中执行 `source scripts/android-env.sh`；我会在工程内固定同一 SDK 路径与 Gradle Wrapper。
3. 我实现 receiver 注册、自建 HTTP SSE 下行、标准通知与 webhook 服务端，并在真机上执行验收矩阵。
4. 需要提高后台送达率时，再决定是否申请小米推送或配置 FCM。

## 远程部署端口预设

- 应用容器：容器内 `:8788`，Docker 仅发布为宿主机 `127.0.0.1:8788`，仅由 Nginx 访问；不要在 VPS 防火墙或安全组开放 `8788/tcp`。
- 对外 HTTPS：`443/tcp`，必须开放，供 webhook 与 Android SSE 客户端使用。
- HTTP：`80/tcp`，必须开放，供 Let's Encrypt HTTP-01 证书签发、自动续期和 HTTP 跳转 HTTPS 使用。
- SSH：`5022/tcp`，保持现有管理入口。

SSE 复用 `443/tcp`，不需要单独开放 WebSocket、SSE 或通知端口。Mac 本机构建镜像时必须指定 `linux/amd64`，以适配远程 `x86_64` 主机。
