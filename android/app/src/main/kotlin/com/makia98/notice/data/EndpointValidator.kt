package com.makia98.notice.data

/**
 * Validates a server endpoint against the production/dev rules:
 *
 *  - Production (devMode off): only `https://` is accepted.
 *  - Dev mode (off by default): `http://` is accepted, but only to local or
 *    private (RFC1918) hosts.
 *
 * This is an application-layer gate; the platform network-security-config
 * additionally rejects cleartext traffic to public hosts.
 */
object EndpointValidator {

    data class Result(val ok: Boolean, val error: String? = null)

    fun validate(endpoint: String, devMode: Boolean): Result {
        val e = endpoint.trim()
        if (e.isEmpty()) return Result(false, "服务器地址为空")
        val lower = e.lowercase()
        val isHttps = lower.startsWith("https://")
        val isHttp = lower.startsWith("http://")
        if (!isHttps && !isHttp) {
            return Result(false, "地址必须以 http:// 或 https:// 开头")
        }
        if (isHttp && !devMode) {
            return Result(false, "生产模式只允许 https://（可在设置中开启开发模式）")
        }
        if (isHttp) {
            val host = hostOf(e)
            if (!isPrivateOrLocal(host)) {
                return Result(false, "开发模式仅允许本地/内网地址（localhost、127.0.0.1、10.x、192.168.x、172.16-31.x）")
            }
        }
        // trailing-path sanity: must contain a host segment.
        val host = hostOf(e)
        if (host.isEmpty()) return Result(false, "地址缺少主机名")
        return Result(true)
    }

    private fun hostOf(url: String): String {
        val noScheme = url.substringAfter("://", url)
        return noScheme.substringBefore('/').substringBefore(':')
    }

    private fun isPrivateOrLocal(host: String): Boolean {
        if (host == "localhost") return true
        if (host == "127.0.0.1") return true
        if (host.startsWith("10.")) return true
        if (host.startsWith("192.168.")) return true
        if (host.startsWith("172.")) {
            val parts = host.split(".")
            if (parts.size >= 2) {
                val second = parts[1].toIntOrNull() ?: return false
                return second in 16..31
            }
        }
        return false
    }
}
