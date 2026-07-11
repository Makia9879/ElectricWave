package com.makia98.electricwave.remote

import com.makia98.electricwave.data.Profile
import com.makia98.electricwave.util.Logx
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.net.URLEncoder

/**
 * Calls `POST {endpoint}/api/v1/receivers/{receiver_id}/test` to verify the
 * profile/SSE/notification chain end-to-end. Uses the identity token in the
 * Authorization header only; never logs it.
 */
class TestNotifier(private val client: OkHttpClient) {

    suspend fun send(profile: Profile): String = withContext(Dispatchers.IO) {
        val base = profile.serverEndpoint.trimEnd('/', ' ')
        if (base.isEmpty() || profile.receiverId.isBlank() || profile.identityToken.isBlank()) {
            return@withContext "配置不完整"
        }
        val rid = URLEncoder.encode(profile.receiverId, "UTF-8")
        val url = "$base/api/v1/receivers/$rid/test"
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer ${profile.identityToken}")
            .header("Content-Type", "application/json; charset=utf-8")
            .post("{}".toRequestBody(null))
            .build()
        try {
            client.newCall(request).execute().use { resp ->
                val code = resp.code
                when (code) {
                    200, 201 -> "已发送测试通知 ($code)"
                    503 -> "服务不可用：接收端可能未在线 (503)"
                    401, 403 -> "鉴权失败 ($code)"
                    404 -> "接收端不存在 (404)"
                    in 500..599 -> "服务器错误 ($code)"
                    else -> "HTTP $code"
                }
            }
        } catch (e: IOException) {
            Logx.w("Test notify failed: ${e.javaClass.simpleName}")
            "请求失败：${e.javaClass.simpleName}"
        } catch (e: Throwable) {
            Logx.w("Test notify error", e)
            "请求失败：${e.javaClass.simpleName}"
        }
    }
}
