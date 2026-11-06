package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiError
import kotlinx.serialization.json.Json

/** 解析服务端错误信封；响应体不是合法信封（HTML 网关错误页、空体……）时返回 null，调用方按状态码兜底。 */
object ErrorEnvelope {
    private val json = Json { ignoreUnknownKeys = true; isLenient = true; explicitNulls = false }

    fun parse(body: String?): ApiError? {
        if (body.isNullOrBlank()) return null
        return try {
            json.decodeFromString(ApiError.serializer(), body)
        } catch (_: Exception) {
            null
        }
    }
}
