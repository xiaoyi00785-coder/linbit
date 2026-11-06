package click.cowa.linbit.data.paper

import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.NetworkFactory
import com.jakewharton.retrofit2.converter.kotlinx.serialization.asConverterFactory
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.Retrofit

@OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)
class PaperTradingContractTest {
    private fun fixture(block: suspend (client: PaperTradingClient, MockWebServer) -> Unit) = runTest {
        MockWebServer().use { server ->
            server.dispatcher = object : okhttp3.mockwebserver.Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    // This fixture fails closed. No route in this file can reach a real asset endpoint.
                    val key = "${request.method} ${request.path}"
                    if (!request.requestUrl.toString().startsWith(server.url("/").toString())) return MockResponse().setResponseCode(599)
                    if (request.method != "GET" && request.path.orEmpty().substringBefore('?') !in setOf("/paper/account/reset", "/paper/orders", "/price-alerts", "/price-alerts/mock-alert")) return MockResponse().setResponseCode(599)
                    return responses.remove(key) ?: MockResponse().setResponseCode(599).setBody("""{"code":"UNDECLARED_ROUTE","message":"mock fixture rejects the endpoint"}""")
                }
            }
            server.start()
            client = PaperTradingClient(Retrofit.Builder().baseUrl(server.url("/")).client(OkHttpClient()).addConverterFactory(NetworkFactory.json.asConverterFactory("application/json".toMediaType())).build().create(PaperTradingApi::class.java))
            block(client, server)
        }
    }
    private val responses = linkedMapOf<String, MockResponse>()
    private suspend fun <T> invalid(block: suspend () -> T): Throwable = runCatching { block() }.exceptionOrNull() ?: error("Expected explicit validation failure")
    private suspend fun serverFailure(block: suspend () -> Any?): ApiFailure.Server = (invalid(block) as? ApiFailure.Server) ?: error("Expected stable server error")

    private lateinit var client: PaperTradingClient
    private fun route(method: String, path: String, body: String, status: Int = 200, retry: Int? = null) {
        responses["$method $path"] = MockResponse().setResponseCode(status).setHeader("Content-Type", "application/json").apply { if (retry != null) addHeader("Retry-After", retry.toString()) }.setBody(body)
    }
    private val account = """{"accountId":"pa_mock","mode":"paper","market":"spot","generation":3,"quoteAsset":"USDT","initialBalance":"10000.00000000","resetLimitPerUtcDay":1,"resetsUsedToday":0,"nextResetAt":null,"orderTypes":["market"],"priceSource":"INDEX","feeModel":"none","slippageModel":"none","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
    private val balances = """{"page":1,"pageSize":20,"total":1,"items":[{"asset":"USDT","free":"10000","locked":"0","total":"10000","valuationUsd":"10000","priceUsd":"1"}],"account":"paper","mode":"paper","generation":3}"""
    private val order = """{"orderId":"po_mock","clientOrderId":"client-mock-1","mode":"paper","accountId":"pa_mock","generation":3,"symbol":"BTCUSDT","side":"buy","type":"market","price":null,"quantity":".00123","quoteOrderQty":"100.00","filledQuantity":".00123","status":"filled","priceSource":"INDEX","referencePrice":"81000.04","marketObservedAt":"2026-01-01T00:00:00Z","fee":"0","feeAsset":"USDT","createdAt":"2026-01-01T00:00:00Z","executedAt":"2026-01-01T00:00:00Z"}"""
    private val trade = """{"tradeId":"pt_mock","orderId":"po_mock","mode":"paper","symbol":"BTCUSDT","side":"buy","price":"81000.04","quantity":"0.00123","fee":"0","feeAsset":"USDT","role":"taker","priceSource":"INDEX","marketObservedAt":"2026-01-01T00:00:00Z","createdAt":"2026-01-01T00:00:00Z"}"""
    private fun alert(revision: Int = 1) = """{"alertId":"al_mock","clientAlertId":"ca_mock_1","market":"spot","symbol":"BTCUSDT","condition":"gte","threshold":"80000.00","priceSource":"INDEX","triggerMode":"once","channels":["in_app"],"enabled":true,"status":"active","revision":$revision,"limit":{"activePerUser":50},"lastObservedPrice":null,"lastEvaluatedAt":null,"triggeredPrice":null,"triggeredAt":null,"notificationId":null,"deliveryStatus":"none","cooldownUntil":null,"note":"test only","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
    private fun json(body: String) = NetworkFactory.json.parseToJsonElement(body).jsonObject
    private fun newAlert() = CreatePriceAlertRequest(clientAlertId = "ca_mock_1", symbol = "BTCUSDT", condition = "gte", threshold = "80000.00", note = "test only")

    @Test fun virtualAccountReadsMaintainGenerationAndPaginationIndependentlyOfRealAssets() = fixture { client, server ->
        route("GET", "/paper/account", account)
        route("GET", "/paper/balances?page=1&pageSize=20", balances)
        assertEquals("paper", client.account().mode)
        assertEquals(3L, client.balances().generation)
        val request = server.takeRequest(); assertEquals("/paper/account", request.path); assertNull(request.getHeader("Authorization")); assertNull(request.getHeader("Cookie"))
        assertEquals("GET", server.takeRequest().method)
    }
    @Test fun accountResetRequiresIdempotencyAndExactConfirmation() = fixture { client, server ->
        route("POST", "/paper/account/reset", """{"accountId":"pa_mock","mode":"paper","generation":4,"balances":[],"resetAt":"2026-01-01T00:00:00Z"}""")
        assertEquals(4L, client.reset("reset-mock-only").generation)
        val req = server.takeRequest(); assertEquals("reset-mock-only", req.getHeader("Idempotency-Key"))
        invalid { client.reset("") }
        assertEquals("RESET_PAPER_ACCOUNT", json(req.body.clone().readUtf8())["confirmation"]?.jsonPrimitive?.contentOrNull)
        assertTrue(req.path!!.contains("/paper/")); assertTrue(!req.path!!.contains("/wallet/"))
    }
    @Test fun paginationAndStableErrorMappingsAreCheckedBeforeAndAfterHttp() = fixture { client, server ->
        invalid { client.balances(page = 0) }; invalid { client.balances(pageSize = 101) }
        invalid { client.orders(pageSize = 0, generation = 0) }; invalid { client.alerts(pageSize = 101) }
        assertEquals(0, server.requestCount)
        route("POST", "/paper/account/reset", """{"code":"PAPER_RESET_LIMIT_EXCEEDED","message":"每 UTC 日仅可重置一次","requestId":"mock-reset-limit"}""", 429)
        val rate = serverFailure { client.reset("reset-mock-only") }
        assertEquals("PAPER_RESET_LIMIT_EXCEEDED", rate.code); assertEquals(429, rate.status)
    }
    @Test fun virtualMarketOrderUsesStableClientIdAndNeverRealOrderPath() = fixture { client, server ->
        val q = PaperOrderRequest(symbol = "BTCUSDT", side = "buy", quoteOrderQty = "100.00", clientOrderId = "client-mock-1")
        route("POST", "/paper/orders", order, 201)
        assertEquals("paper", client.placeOrder(q).mode)
        val req = server.takeRequest(); assertEquals("/paper/orders", req.path)
        assertEquals(mapOf("symbol" to "BTCUSDT", "side" to "buy", "type" to "market", "clientOrderId" to "client-mock-1", "quoteOrderQty" to "100.00"), json(req.body.clone().readUtf8()).mapValues { it.value.jsonPrimitive.content })
        invalid { client.placeOrder(q.copy(type = "limit")) }
        invalid { client.placeOrder(q.copy(clientOrderId = "spaces are not allowed")) }
        invalid { client.placeOrder(q.copy(side = "sell")) }
        invalid { client.placeOrder(q.copy(quantity = "0.00123")) }
    }
    @Test fun historyAndOrderLookupPreserveMockOnlyExecutionMetadata() = fixture { client, server ->
        route("GET", "/paper/orders?symbol=BTCUSDT&generation=3&page=1&pageSize=20", """{"page":1,"pageSize":20,"total":1,"items":[$order],"generation":3}""")
        route("GET", "/paper/trades?symbol=BTCUSDT&generation=3&page=1&pageSize=20", """{"page":1,"pageSize":20,"total":1,"items":[$trade],"generation":3}""")
        route("GET", "/paper/orders/po_mock", order)
        assertEquals(1, client.orders(symbol = "BTCUSDT", generation = 3).total)
        assertEquals("INDEX", client.trades(symbol = "BTCUSDT", generation = 3).items.single().priceSource)
        assertEquals(3L, client.order("po_mock").generation)
    }
    @Test fun alertsUseOneShotIndexAndInAppOnlyAndRevisionPaths() = fixture { client, server ->
        route("GET", "/price-alerts?symbol=BTCUSDT&status=active&page=1&pageSize=20", """{"page":1,"pageSize":20,"total":1,"items":[${alert()}]}""")
        route("POST", "/price-alerts", alert(), 201)
        route("PATCH", "/price-alerts/mock-alert", alert(2))
        route("DELETE", "/price-alerts/mock-alert?revision=2", "", 204)
        client.alerts(symbol = "BTCUSDT", status = "active")
        client.createAlert(newAlert()); client.updateAlert("mock-alert", UpdatePriceAlertRequest(revision = 1, threshold = "81000")); client.deleteAlert("mock-alert", revision = 2)
        assertEquals("GET", server.takeRequest().method)
        val created = server.takeRequest(); assertEquals("POST", created.method); assertEquals("/price-alerts", created.path)
        val createdJson = json(created.body.clone().readUtf8())
        assertEquals(listOf("spot", "INDEX", "once", "in_app"), listOf(createdJson["market"]?.jsonPrimitive?.content, createdJson["priceSource"]?.jsonPrimitive?.content, createdJson["triggerMode"]?.jsonPrimitive?.content, createdJson["channels"]?.jsonArray?.single()?.jsonPrimitive?.content))
        assertEquals("ca_mock_1", createdJson["clientAlertId"]?.jsonPrimitive?.content)
        val updated = server.takeRequest(); assertEquals("PATCH", updated.method); assertEquals("/price-alerts/mock-alert", updated.path)
        assertEquals("81000", json(updated.body.clone().readUtf8())["threshold"]?.jsonPrimitive?.content)
        assertEquals(1, json(updated.body.clone().readUtf8())["revision"]?.jsonPrimitive?.int)
        val deleted = server.takeRequest(); assertEquals("/price-alerts/mock-alert?revision=2", deleted.path)
    }
    @Test fun eachAlertMutationCanSurfaceRateLimitedWithoutLocalStateChange() = fixture { client, server ->
        listOf("POST", "PATCH", "DELETE").forEach { method ->
            route(method, "/price-alerts" + if (method == "POST") "" else "/mock-alert", priceAlertRateLimit, 429, 17)
        }
        val create = serverFailure { client.createAlert(newAlert()) }
        assertEquals(429, create.status); assertEquals("RATE_LIMITED", create.error?.code); assertEquals(17, create.retryAfter); assertEquals("mock-req-429", create.error?.requestId)
        assertEquals(17, serverFailure { client.updateAlert("mock-alert", UpdatePriceAlertRequest(1, threshold = "81000")) }.retryAfter)
        val delete = serverFailure { client.deleteAlert("mock-alert") }; assertEquals("RATE_LIMITED", delete.code)
    }
    @Test fun alert429MustNotAcceptAnAbsentOrContradictoryRetryInstruction() = fixture { client, _ ->
        route("POST", "/price-alerts", priceAlertRateLimit, 429)
        invalid { client.createAlert(newAlert()) }
        // A separate route keeps the server dispatcher fail-closed for repeated POSTs.
    }
    @Test fun mismatched429HeaderIsRejectedInsteadOfSelectingAnotherRetryDelay() = fixture { client, _ ->
        route("POST", "/price-alerts", priceAlertRateLimit, 429, 7); invalid { client.createAlert(newAlert()) }
    }
    @Test fun staleIndexConflictAndFeatureDisabledAreDistinctFromRateLimit() = fixture { client, _ ->
        route("POST", "/paper/orders", """{"code":"INDEX_UNAVAILABLE","message":"新鲜外部指数不可用","requestId":"mock-index"}""", 503)
        val unavailable = serverFailure { client.placeOrder(PaperOrderRequest("BTCUSDT", "sell", quantity = "1", clientOrderId = "mock-idempotency")) }
        assertEquals("INDEX_UNAVAILABLE", unavailable.code); assertEquals(503, unavailable.status)
        route("POST", "/paper/orders", """{"code":"IDEMPOTENCY_CONFLICT","message":"相同 clientOrderId 对应了不同内容","requestId":"mock-collision"}""", 409)
        val conflict = serverFailure { client.placeOrder(PaperOrderRequest("BTCUSDT", "sell", quantity = "2", clientOrderId = "mock-idempotency")) }
        assertEquals("IDEMPOTENCY_CONFLICT", conflict.code)
        route("GET", "/paper/account", """{"code":"FEATURE_DISABLED","message":"模拟交易未开启"}""", 403)
        assertEquals("FEATURE_DISABLED", serverFailure { client.account() }.code)
    }
    @Test fun rejectRealLedgerDataOrWrongGenerationOnPaperReply() = fixture { client, _ ->
        route("GET", "/paper/balances?page=1&pageSize=20", balances.replace("\"account\":\"paper\"", "\"account\":\"assets\""))
        invalid { client.balances() }
        route("GET", "/paper/account", account.replace("\"generation\":3", "\"generation\":0"))
        invalid { client.account() }
    }
}
private const val priceAlertRateLimit = """{"code":"RATE_LIMITED","message":"价格预警写操作过于频繁，请稍后再试","requestId":"mock-req-429","retryAfterSec":17}"""
