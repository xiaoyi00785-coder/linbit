package click.cowa.linbit.core.network

import click.cowa.linbit.core.generated.ApiAuthTokens
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.generated.ApiLoginChallenge
import click.cowa.linbit.core.generated.ApiTicker
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement

/** 登录结果：直接拿到令牌，或需要二次验证。 */
sealed interface LoginResponse {
    data class Tokens(val tokens: ApiAuthTokens) : LoginResponse
    data class Challenge(val challenge: ApiLoginChallenge) : LoginResponse
}

/**
 * OpenAPI 里三个 oneOf 响应的判别解码（代码生成器对 oneOf 不可用，见 mobile/tools/gen_models_android.sh）：
 *  - POST /auth/login：含 challengeToken → LoginChallenge，否则 AuthTokens
 *  - GET /market/ticker：数组 → 多个；对象 → 单个
 *  - GET /futures/market/ticker：{tickers:[...]} → 多个；否则单个 FuturesTicker
 */
object OneOfResponses {
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; coerceInputValues = true }

    fun login(e: JsonElement): LoginResponse {
        val obj = e as? JsonObject ?: throw IllegalArgumentException("login: expected object")
        return if ("challengeToken" in obj) LoginResponse.Challenge(json.decodeFromJsonElement(obj))
        else LoginResponse.Tokens(json.decodeFromJsonElement(obj))
    }

    fun tickers(e: JsonElement): List<ApiTicker> = when (e) {
        is JsonArray -> json.decodeFromJsonElement(e)
        is JsonObject -> listOf(json.decodeFromJsonElement<ApiTicker>(e))
        else -> emptyList()
    }

    fun futuresTickers(e: JsonElement): List<ApiFuturesTicker> {
        val obj = e as? JsonObject ?: return emptyList()
        val list = obj["tickers"]
        return if (list is JsonArray) json.decodeFromJsonElement(list) else listOf(json.decodeFromJsonElement<ApiFuturesTicker>(obj))
    }
}
