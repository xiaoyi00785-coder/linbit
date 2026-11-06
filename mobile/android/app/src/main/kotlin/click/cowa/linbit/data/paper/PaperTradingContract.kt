package click.cowa.linbit.data.paper

import click.cowa.linbit.core.generated.ApiError
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.ErrorEnvelope
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.serialization.Serializable
import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.json.JsonObject
import retrofit2.Response
import retrofit2.http.Body
import retrofit2.http.DELETE
import retrofit2.http.GET
import retrofit2.http.Header
import retrofit2.http.PATCH
import retrofit2.http.POST
import retrofit2.http.Path
import retrofit2.http.Query

/**
 * Production Hilt binds this separate service with the public /app/features paperTrading gate.
 * Disabled, missing or failed feature reads deny every Paper operation.
 * Positive-path tests may instantiate only with an isolated mock or in-memory API.
 * Never blend this client with LinbitApi, real balances, or the real spot-order repository.
 */
interface PaperTradingApi {
    @GET("paper/account") suspend fun account(): Response<PaperAccount>
    @POST("paper/account/reset") suspend fun reset(@Header("Idempotency-Key") key: String, @Body request: PaperResetRequest): Response<PaperReset>
    @GET("paper/balances") suspend fun balances(@Query("page") page: Int, @Query("pageSize") pageSize: Int): Response<PaperBalances>
    @POST("paper/orders") suspend fun placeOrder(@Body request: PaperOrderRequest): Response<PaperOrder>
    @GET("paper/orders") suspend fun orders(@Query("symbol") symbol: String? = null, @Query("status") status: String? = null, @Query("generation") generation: Long? = null, @Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): Response<PaperOrders>
    @GET("paper/orders/{orderId}") suspend fun order(@Path("orderId") id: String): Response<PaperOrder>
    @GET("paper/trades") suspend fun trades(@Query("symbol") symbol: String? = null, @Query("generation") generation: Long? = null, @Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): Response<PaperTrades>
    @GET("price-alerts") suspend fun alerts(@Query("symbol") symbol: String? = null, @Query("status") status: String? = null, @Query("page") page: Int = 1, @Query("pageSize") pageSize: Int = 20): Response<PriceAlerts>
    @POST("price-alerts") suspend fun createAlert(@Body request: CreatePriceAlertRequest): Response<PriceAlert>
    @PATCH("price-alerts/{alertId}") suspend fun updateAlert(@Path("alertId") id: String, @Body request: UpdatePriceAlertRequest): Response<PriceAlert>
    @DELETE("price-alerts/{alertId}") suspend fun deleteAlert(@Path("alertId") id: String, @Query("revision") revision: Int? = null): Response<Unit>
}

class PaperTradingClient(private val api: PaperTradingApi, private val gate: PaperFeatureGate) {
    val enabled: Boolean get() = gate.enabled
    val featureState: kotlinx.coroutines.flow.StateFlow<PaperFeatureSnapshot> get() = gate.snapshot
    val featureFailure: String? get() = gate.snapshot.value.failure
    suspend fun refreshFeature(): Boolean = gate.refresh()
    private fun requireEnabled() = require(enabled) { "Paper trading is disabled or unset" }
    suspend fun account(): PaperAccount {
        requireEnabled()
        return unwrapApiResponse(api.account()).also { PaperContractValidation.requireAccount(it) }
    }
    suspend fun balances(page: Int = 1, pageSize: Int = 20): PaperBalances {
        requireEnabled(); requirePage(page, pageSize)
        return unwrapApiResponse(api.balances(page, pageSize)).also { PaperContractValidation.requireBalances(it) }
    }
    suspend fun reset(idempotencyKey: String): PaperReset {
        requireEnabled(); require(idempotencyKey.length in 1..128) { "Reset requires a bounded Idempotency-Key" }
        return unwrapApiResponse(api.reset(idempotencyKey, PaperResetRequest())).also { require(it.mode == "paper" && it.generation > 0) { "Response is not isolated paper reset" } }
    }
    suspend fun placeOrder(request: PaperOrderRequest): PaperOrder {
        requireEnabled()
        PaperContractValidation.requireOrderRequest(request)
        return unwrapApiResponse(api.placeOrder(request)).also { PaperContractValidation.requirePaperOrder(it) }
    }
    suspend fun order(id: String): PaperOrder {
        requireEnabled(); require(id.isNotBlank())
        return unwrapApiResponse(api.order(id)).also { PaperContractValidation.requirePaperOrder(it) }
    }
    suspend fun orders(symbol: String? = null, status: String? = null, generation: Long? = null, page: Int = 1, pageSize: Int = 20): PaperOrders {
        requireEnabled(); requirePage(page, pageSize); require(status in setOf(null, "filled"))
        return unwrapPage(api.orders(symbol = symbol, status = status, generation = generation?.also { require(it >= 1) }, page = page, pageSize = pageSize)) { orders ->
            require(orders.generation >= 0); orders.items.forEach(PaperContractValidation::requirePaperOrder); orders.items.forEach { require(it.generation == orders.generation) }
        }
    }
    suspend fun trades(symbol: String? = null, generation: Long? = null, page: Int = 1, pageSize: Int = 20): PaperTrades {
        requireEnabled(); requirePage(page, pageSize)
        return unwrapPage(api.trades(symbol = symbol, generation = generation?.also { require(it >= 1) }, page = page, pageSize = pageSize)) { trades ->
            require(trades.generation >= 0); trades.items.forEach { require(it.mode == "paper" && it.priceSource == "INDEX") }
        }
    }
    suspend fun alerts(symbol: String? = null, status: String? = null, page: Int = 1, pageSize: Int = 20): PriceAlerts {
        requirePage(page, pageSize); require(status in setOf(null, "active", "triggered", "disabled"))
        return unwrapPage(api.alerts(symbol = symbol, status = status, page = page, pageSize = pageSize)) { alerts -> alerts.items.forEach(PaperContractValidation::requireAlert) }
    }
    suspend fun createAlert(request: CreatePriceAlertRequest): PriceAlert {
        PaperContractValidation.requireNewAlert(request)
        return unwrapApiResponse(api.createAlert(request)).also(PaperContractValidation::requireAlert)
    }
    suspend fun updateAlert(id: String, request: UpdatePriceAlertRequest): PriceAlert {
        require(id.isNotBlank() && request.revision >= 1 && request.condition in setOf(null, "gte", "lte") && (request.threshold == null || validPositiveDecimal(request.threshold)) && (request.note?.length ?: 0) <= 120 && request.channels in setOf(null, listOf("in_app")))
        return unwrapApiResponse(api.updateAlert(id, request)).also(PaperContractValidation::requireAlert)
    }
    suspend fun deleteAlert(id: String, revision: Int? = null) {
        require(id.isNotBlank() && (revision == null || revision >= 1))
        unwrapApiResponse(api.deleteAlert(id, revision))
    }
}


private fun <T : Any> unwrapPage(response: Response<T>, validate: (T) -> Unit): T {
    val body = unwrapApiResponse(response)
    require(pageIsValid(page(body))) { "Paper pagination/pageSize/total is required and non-negative" }
    return body.also(validate)
}
private fun page(body: Any): Triple<Int, Int, Int> = when (body) {
    is PaperBalances -> Triple(body.page, body.pageSize, body.total)
    is PaperOrders -> Triple(body.page, body.pageSize, body.total)
    is PaperTrades -> Triple(body.page, body.pageSize, body.total)
    is PriceAlerts -> Triple(body.page, body.pageSize, body.total)
    else -> Triple(-1, -1, -1)
}
private fun requirePage(page: Int, pageSize: Int) = require(page >= 1 && pageSize in 1..100)
private fun pageIsValid(page: Triple<Int, Int, Int>) = page.first >= 1 && page.second in 1..100 && page.third >= 0

@Serializable data class PriceAlertRateLimitError(val code: String, val message: String, val requestId: String? = null, val retryAfterSec: Int)
private fun <T> unwrapApiResponse(response: Response<T>): T {
    val body = response.body()
    if (response.isSuccessful && (body != null || response.code() == 204)) return body ?: Unit as T
    val raw = response.errorBody()?.string()
    val alertError = runCatching { NetworkFactory.json.decodeFromString<PriceAlertRateLimitError>(raw.orEmpty()) }.getOrNull()
    if (response.code() == 429 && alertError?.code == "RATE_LIMITED") {
        require(alertError.retryAfterSec >= 1) { "RATE_LIMITED must have retryAfterSec >= 1" }
        val retryAfterHeader = response.headers()["Retry-After"]?.toIntOrNull()
        require(alertError.retryAfterSec == retryAfterHeader) { "Alert 429 code/header/body must match" }
        throw ApiFailure.Server(429, ApiError(alertError.code, alertError.message, alertError.requestId), retryAfterHeader!!)
    }
    throw ApiFailure.Server(response.code(), ErrorEnvelope.parse(raw), response.headers()["Retry-After"]?.toIntOrNull())
}

fun validPositiveDecimal(value: String): Boolean = value.matches(Regex("[0-9]+(?:\\.[0-9]+)?")) && value.toBigDecimalOrNull()?.signum() == 1

object PaperContractValidation {
    private val id = Regex("^[A-Za-z0-9_.:-]{1,64}$")
    fun requireAccount(account: PaperAccount) = require(account.mode == "paper" && account.market == "spot" && account.quoteAsset == "USDT" && account.generation >= 1) { "Paper spot account/generation mismatch" }
    fun requireBalances(value: PaperBalances) = require(value.account == "paper" && value.mode == "paper" && value.generation >= 0 && value.items.all { it.locked == "0" }) { "Paper account cannot be mixed with real balances" }
    fun requireOrderRequest(request: PaperOrderRequest) {
        require(id.matches(request.clientOrderId) && request.type == "market" && request.symbol.isNotBlank() && request.side in setOf("buy", "sell"))
        require(request.quoteOrderQty.isBlank() || request.side == "buy")
        require(if (request.side == "buy") request.quantity.isNotBlank().xor(request.quoteOrderQty.isNotBlank()) else request.quantity.isNotBlank() && request.quoteOrderQty.isBlank()) { "Market buy needs exactly one quantity/quoteOrderQty; sell needs only quantity" }
    }
    fun requirePaperOrder(it: PaperOrder) = require(it.mode == "paper" && it.accountId.isNotBlank() && it.generation >= 1 && it.type == "market" && it.priceSource == "INDEX" && it.fee == "0" && it.marketObservedAt.isNotBlank()) { "Invalid simulated market-only INDEX order" }
    fun requireNewAlert(it: CreatePriceAlertRequest) = require(id.matches(it.clientAlertId) && it.market == "spot" && validPositiveDecimal(it.threshold) && it.symbol.isNotBlank() && it.condition in setOf("gte", "lte") && it.priceSource == "INDEX" && it.triggerMode == "once" && it.channels == listOf("in_app") && it.note.length <= 120)
    fun requireAlert(it: PriceAlert) = require(it.status in setOf("active", "triggered", "disabled") && it.condition in setOf("gte", "lte") && validPositiveDecimal(it.threshold.trim()) && it.deliveryStatus in setOf("none", "pending", "delivered_in_app", "failed", "unsupported") && it.limit.activePerUser == 50 && it.market == "spot" && it.revision >= 1 && it.priceSource == "INDEX" && it.triggerMode == "once" && it.channels == listOf("in_app")) { "Price alert must remain isolated from order execution" }
}

@Serializable data class PaperAccount(val accountId: String, val mode: String, val market: String, val generation: Long, val quoteAsset: String, val initialBalance: String, val resetLimitPerUtcDay: Int, val resetsUsedToday: Int, val nextResetAt: String? = null, val orderTypes: List<String>, val priceSource: String, val feeModel: String, val slippageModel: String, val createdAt: String, val updatedAt: String)
@Serializable data class PaperResetRequest(@EncodeDefault val confirmation: String = "RESET_PAPER_ACCOUNT")
@Serializable data class PaperBalance(val asset: String, val free: String, val locked: String, val total: String, val valuationUsd: String? = null, val priceUsd: String? = null)
@Serializable data class PaperReset(val accountId: String, val mode: String, val generation: Long, val balances: List<PaperBalance>, val resetAt: String)
@Serializable data class PaperBalances(val page: Int, val pageSize: Int, val total: Int, val items: List<PaperBalance>, val account: String, val mode: String, val generation: Long)
@Serializable data class PaperOrderRequest(val symbol: String, val side: String, @EncodeDefault val type: String = "market", val quantity: String = "", val quoteOrderQty: String = "", val clientOrderId: String)
@Serializable data class PaperOrder(val orderId: String, val clientOrderId: String, val mode: String, val accountId: String, val generation: Long, val symbol: String, val side: String, val type: String, val price: String? = null, val quantity: String, val quoteOrderQty: String? = null, val filledQuantity: String, val status: String, val priceSource: String, val referencePrice: String, val marketObservedAt: String, val fee: String, val feeAsset: String, val createdAt: String, val executedAt: String, val trades: List<PaperTrade>? = null)
@Serializable data class PaperTrade(val tradeId: String, val orderId: String, val mode: String, val symbol: String, val side: String, val price: String, val quantity: String, val fee: String, val feeAsset: String, val role: String, val priceSource: String, val marketObservedAt: String, val createdAt: String)
@Serializable data class PaperOrders(val page: Int, val pageSize: Int, val total: Int, val items: List<PaperOrder>, val generation: Long)
@Serializable data class PaperTrades(val page: Int, val pageSize: Int, val total: Int, val items: List<PaperTrade>, val generation: Long)
@Serializable data class CreatePriceAlertRequest(val clientAlertId: String, @EncodeDefault val market: String = "spot", val symbol: String, val condition: String, val threshold: String, @EncodeDefault val priceSource: String = "INDEX", @EncodeDefault val triggerMode: String = "once", @EncodeDefault val channels: List<String> = listOf("in_app"), val enabled: Boolean = true, val note: String = "")
@Serializable data class UpdatePriceAlertRequest(val revision: Int, val threshold: String? = null, val condition: String? = null, val enabled: Boolean? = null, val channels: List<String>? = null, val note: String? = null)
@Serializable data class PriceAlertLimit(val activePerUser: Int)
@Serializable data class PriceAlert(val alertId: String, val clientAlertId: String, val market: String, val symbol: String, val condition: String, val threshold: String, val priceSource: String, val triggerMode: String, val channels: List<String>, val enabled: Boolean, val status: String, val revision: Int, val limit: PriceAlertLimit, val lastObservedPrice: String? = null, val lastEvaluatedAt: String? = null, val triggeredPrice: String? = null, val triggeredAt: String? = null, val notificationId: String? = null, val deliveryStatus: String, val cooldownUntil: String? = null, val note: String? = null, val createdAt: String, val updatedAt: String)
@Serializable data class PriceAlerts(val page: Int, val pageSize: Int, val total: Int, val items: List<PriceAlert>)
