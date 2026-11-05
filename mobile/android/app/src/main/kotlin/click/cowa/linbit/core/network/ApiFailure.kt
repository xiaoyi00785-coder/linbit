package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiError

/**
 * 网络层统一错误（对应 iOS APIFailure）。
 * 服务端错误信封：{ code, message, requestId?, details? }（OpenAPI Error），见 [ErrorEnvelope]。
 */
sealed class ApiFailure(message: String?, cause: Throwable? = null) : Exception(message, cause) {
    /** retryAfter：响应头 Retry-After（秒），429 RATE_LIMITED 时用于倒计时 */
    class Server(val status: Int, val error: ApiError?, val retryAfter: Int? = null) :
        ApiFailure(error?.message ?: "HTTP $status")

    class Decoding(cause: Throwable) : ApiFailure("数据解析失败：${cause.message}", cause)
    class Transport(cause: Throwable) : ApiFailure(cause.message, cause)

    val code: String? get() = (this as? Server)?.error?.code
    val httpStatus: Int? get() = (this as? Server)?.status
    val retryAfterSeconds: Int? get() = (this as? Server)?.retryAfter

    /** 面向用户的中文文案：服务端 message 优先（已知错误码用统一文案），否则按类型兜底。 */
    fun userMessage(sessionExpired: Boolean = false): String = when (this) {
        is Server -> when {
            status == 401 || error?.code == "AUTH_UNAUTHORIZED" ->
                if (sessionExpired) "登录已过期，请重新登录" else ErrorText.LOGIN_REQUIRED
            error != null -> ErrorText.map[error.code] ?: error.message
            status >= 500 -> "服务繁忙，请稍后再试"
            else -> "请求失败（$status）"
        }
        is Transport -> "网络连接失败，请检查网络"
        is Decoding -> "数据异常，请稍后再试"
    }
}

/** 任意异常 → 用户文案 */
fun Throwable.lbMessage(sessionExpired: Boolean = false): String =
    (this as? ApiFailure)?.userMessage(sessionExpired) ?: (message ?: "未知错误")
