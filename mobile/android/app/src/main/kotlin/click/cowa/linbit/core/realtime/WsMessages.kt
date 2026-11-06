package click.cowa.linbit.core.realtime

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.put

/** 服务端 → 客户端 的一帧（docs/api/websocket.md）。 */
sealed interface WsInbound {
    /** 推送数据帧：{ channel, ts, data }。data 保留原始 JSON，由订阅方按频道解码。 */
    data class Frame(val channel: String, val ts: Long?, val data: JsonElement) : WsInbound

    /** 回执 / 事件帧：subscribed、unsubscribed、authed、pong、ping、error。 */
    data class Event(val event: String, val channel: String?, val code: String?, val message: String?, val id: String?) : WsInbound

    /** 无法识别（非 JSON、缺字段）：直接忽略。 */
    data object Ignored : WsInbound
}

/** `ticker:<symbol>` 频道的 data（价格、成交额等均为十进制字符串，绝不转 Double）。 */
@Serializable
data class WsTicker(
    val symbol: String,
    val lastPrice: String,
    val priceChangePercent: String,
    val high: String? = null,
    val low: String? = null,
    val volume: String? = null,
    val quoteVolume: String? = null,
)

/** `futures-ticker:<symbol>` 频道的 data（与 GET /futures/market/ticker 对象一致；含现货兼容别名）。 */
@Serializable
data class WsFuturesTicker(
    val symbol: String,
    val lastPrice: String? = null,
    val markPrice: String? = null,
    val indexPrice: String? = null,
    val priceChangePercent: String? = null,
    val quoteVolume: String? = null,
    val turnover24h: String? = null,
)

object WsMessageParser {
    private val json = Json { ignoreUnknownKeys = true; isLenient = true; explicitNulls = false }

    fun parse(text: String): WsInbound {
        val obj = try {
            json.parseToJsonElement(text) as? JsonObject
        } catch (_: Exception) {
            null
        } ?: return WsInbound.Ignored

        fun str(k: String) = (obj[k] as? JsonPrimitive)?.contentOrNull
        val event = str("event")
        if (event != null) return WsInbound.Event(event, str("channel"), str("code"), str("message"), str("id"))
        val channel = str("channel") ?: return WsInbound.Ignored
        val data = obj["data"] ?: return WsInbound.Ignored
        return WsInbound.Frame(channel, (obj["ts"] as? JsonPrimitive)?.longOrNull, data)
    }

    fun ticker(frame: WsInbound.Frame): WsTicker? = decode(frame, WsTicker.serializer())
    fun futuresTicker(frame: WsInbound.Frame): WsFuturesTicker? = decode(frame, WsFuturesTicker.serializer())

    /** `event:orders` 私有频道：data 为 EventOrder（OpenAPI）。 */
    fun eventOrder(frame: WsInbound.Frame): click.cowa.linbit.core.generated.ApiEventOrder? =
        decode(frame, click.cowa.linbit.core.generated.ApiEventOrder.serializer())

    private fun <T> decode(frame: WsInbound.Frame, ser: kotlinx.serialization.KSerializer<T>): T? = try {
        json.decodeFromJsonElement(ser, frame.data)
    } catch (_: Exception) {
        null
    }

    /** 客户端请求：{"op":"subscribe","id":"android-1","args":["ticker:BTCUSDT"]}（args 可省略，如 ping/pong）。 */
    fun encode(op: String, args: List<String>?, id: String): String = buildJsonObject {
        put("op", op)
        put("id", id)
        if (args != null) put("args", buildJsonArray { args.forEach { add(JsonPrimitive(it)) } })
    }.toString()

}
