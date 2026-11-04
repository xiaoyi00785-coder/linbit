package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiAuthTokens
import click.cowa.linbit.core.generated.ApiRefreshTokenRequest
import kotlinx.serialization.json.Json
import okhttp3.Authenticator
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.Route
import java.io.IOException
import java.util.Locale

/** 接口声明里用的头：公开接口不带 Authorization / 退出登录不触发 401 刷新。会在拦截器里被剥掉，不会发给服务端。 */
object ApiHeaders {
    const val NO_AUTH = "X-LB-No-Auth"
    const val NO_REFRESH = "X-LB-No-Refresh"
}

/**
 * 通用请求头 + Bearer 鉴权（对应 iOS APIClient.makeRequest）：
 * Accept、User-Agent、X-Device-Id、Accept-Language；已登录且接口非公开时带 Authorization: Bearer。
 */
class AuthInterceptor(
    private val tokens: TokenStore,
    private val userAgent: String,
    private val deviceId: () -> String,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val orig = chain.request()
        val noAuth = orig.header(ApiHeaders.NO_AUTH) != null
        val b = orig.newBuilder()
            .removeHeader(ApiHeaders.NO_AUTH)
            .header("Accept", "application/json")
            .header("User-Agent", userAgent)
            .header("X-Device-Id", deviceId())
            .header("Accept-Language", Locale.getDefault().toLanguageTag().ifEmpty { "zh-CN" })
        val token = tokens.accessToken
        if (!noAuth && token != null) b.header("Authorization", "Bearer $token")
        return chain.proceed(b.build())
    }
}

/**
 * 401 自动用 refreshToken 刷新一次后重试（对应 iOS APIClient.refresh）。
 * 并发 401 只触发一次刷新（synchronized + 比较令牌）；刷新失败清除令牌并置 sessionExpired。
 */
class TokenAuthenticator(
    private val tokens: TokenStore,
    private val baseUrl: String,
    private val userAgent: String,
    private val bareClient: OkHttpClient,
    private val onSessionExpired: () -> Unit = {},
) : Authenticator {
    private val json = Json { ignoreUnknownKeys = true }
    private val lock = Any()

    override fun authenticate(route: Route?, response: Response): Request? {
        val req = response.request
        if (req.header("Authorization") == null) return null        // 公开接口的 401：不刷新
        if (req.header(ApiHeaders.NO_REFRESH) != null) return null  // 退出登录
        if (responseCount(response) >= 2) return null               // 已重试过
        val used = req.header("Authorization")?.removePrefix("Bearer ")
        synchronized(lock) {
            val current = tokens.accessToken
            // 别的线程已刷新：直接用新令牌重试
            if (current != null && current != used) return req.withToken(current)
            val rt = tokens.refreshToken ?: return null
            // 网络不可达（IOException）时不清除令牌：保持登录态，让调用方按网络错误处理
            val fresh = try { refresh(rt) } catch (_: IOException) { return null }
            if (fresh == null) {
                tokens.clear()
                if (!tokens.loggingOut) {
                    tokens.sessionExpired = true
                    onSessionExpired()
                }
                return null
            }
            tokens.save(fresh)
            return req.withToken(fresh.accessToken)
        }
    }

    @Throws(IOException::class)
    private fun refresh(refreshToken: String): ApiAuthTokens? {
        val body = json.encodeToString(ApiRefreshTokenRequest.serializer(), ApiRefreshTokenRequest(refreshToken))
            .toRequestBody("application/json".toMediaType())
        val req = Request.Builder().url(baseUrl + "auth/refresh").post(body)
            .header("User-Agent", userAgent).header("Accept", "application/json").build()
        return bareClient.newCall(req).execute().use { r ->
            if (r.code != 200) return@use null
            try {
                json.decodeFromString(ApiAuthTokens.serializer(), r.body?.string().orEmpty())
            } catch (_: Exception) {
                null
            }
        }
    }

    private fun Request.withToken(t: String): Request = newBuilder().header("Authorization", "Bearer $t").build()

    private fun responseCount(r: Response): Int {
        var n = 1
        var p = r.priorResponse
        while (p != null) { n++; p = p.priorResponse }
        return n
    }
}
