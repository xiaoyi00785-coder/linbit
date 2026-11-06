package click.cowa.linbit.feature.alerts

import click.cowa.linbit.core.generated.ApiAppFeatures
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.core.network.NetworkFactory
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.*
import org.junit.Assert.*
import retrofit2.Response

@org.junit.runner.RunWith(org.robolectric.RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class PriceAlertStateTest {
    private val tokens = InMemoryTokenStore()
    @Before fun main() { Dispatchers.setMain(UnconfinedTestDispatcher()); tokens.accessToken = "fake-only" }
    @After fun reset() { Dispatchers.resetMain(); tokens.accessToken = null }
    private fun alert(id: String, status: String = "active") = """{"alertId":"$id","clientAlertId":"ca_${id}","market":"spot","symbol":"BTCUSDT","condition":"gte","threshold":"80000.00","priceSource":"INDEX","triggerMode":"once","channels":["in_app"],"enabled":true,"status":"$status","revision":1,"limit":{"activePerUser":50},"lastObservedPrice":null,"lastEvaluatedAt":null,"triggeredPrice":null,"triggeredAt":null,"notificationId":null,"deliveryStatus":"none","cooldownUntil":null,"note":"mock only","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
    private fun response(ids: List<String>, page: Int = 1, total: Int = 3) = """{"page":$page,"pageSize":2,"total":$total,"items":[${ids.joinToString { alert(it) }}]}"""
    private class Api : PaperTradingApi {
        val queries = mutableListOf<Triple<String?, String?, Pair<Int, Int>>>()
        val replies = ArrayDeque<Response<PriceAlerts>>()
        val deferred = ArrayDeque<CompletableDeferred<Response<PriceAlerts>>>()
        override suspend fun alerts(symbol: String?, status: String?, page: Int, pageSize: Int): Response<PriceAlerts> {
            queries += Triple(symbol, status, page to pageSize)
            return if (queries.size in 2..3 && deferred.isNotEmpty()) deferred.removeFirst().await() else replies.removeFirst()
        }
        override suspend fun account() = error("Paper unavailable")
        override suspend fun reset(key: String, request: PaperResetRequest): Response<PaperReset> = error("PROHIBITED")
        override suspend fun balances(page: Int, pageSize: Int): Response<PaperBalances> = error("PROHIBITED")
        override suspend fun placeOrder(request: PaperOrderRequest): Response<PaperOrder> = error("PROHIBITED")
        override suspend fun orders(symbol: String?, status: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperOrders> = error("PROHIBITED")
        override suspend fun order(id: String): Response<PaperOrder> = error("PROHIBITED")
        override suspend fun trades(symbol: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperTrades> = error("PROHIBITED")
        override suspend fun createAlert(request: CreatePriceAlertRequest): Response<PriceAlert> = error("PROHIBITED")
        override suspend fun updateAlert(id: String, request: UpdatePriceAlertRequest): Response<PriceAlert> = error("PROHIBITED")
        override suspend fun deleteAlert(id: String, revision: Int?): Response<Unit> = error("PROHIBITED")
    }
    private class GateApi(var flags: Map<String, Boolean>? = mapOf("priceAlerts" to true), var fail: Boolean = false) : PublicFeatureFlags {
        override suspend fun load(): Response<ApiAppFeatures> = if (fail) Response.error(500, "no feature".toResponseBody()) else Response.success(ApiAppFeatures(features = flags))
    }
    private fun vm(api: Api, flags: GateApi): PriceAlertViewModel {
        val gate = PriceAlertFeatureGate(flags)
        val client = click.cowa.linbit.di.AppModule.priceAlertClient(api, gate)
        return PriceAlertViewModel(client, tokens).also { it.refresh() }
    }
    @Test fun trueOpensMissingErrorClosedAndStateFlowEmits() = runTest {
        val api = Api(); val flags = GateApi()
        api.replies += Response.success(NetworkFactory.json.decodeFromString<PriceAlerts>(response(listOf("mock_1"))))
        val vm = vm(api, flags); advanceUntilIdle()
        assertEquals(1, api.queries.size)
        assertEquals(null, api.queries.single().first); assertEquals(1 to 2, api.queries.single().third)
        assertTrue(vm.featureState.value.enabled && vm.featureState.value.checked)
        assertEquals("mock_1", vm.items.single().alertId); assertEquals(50, vm.items.single().limit.activePerUser)
        flags.flags = emptyMap(); vm.refresh(); advanceUntilIdle()
        assertFalse(vm.featureState.value.enabled); assertTrue(vm.featureState.value.checked); assertTrue(vm.items.isEmpty())
        flags.fail = true; vm.refresh(); advanceUntilIdle()
        assertFalse(vm.featureState.value.checked); assertNotNull(vm.featureState.value.failure)
        assertEquals(1, api.queries.size)
        tokens.accessToken = null; advanceUntilIdle(); assertNull(vm.selected); assertFalse(vm.loading)
    }
    @Test fun nextPageControlled429RetryAndTerminalAreReadOnly() = runTest {
        val json = NetworkFactory.json
        val api = Api()
        api.replies.add(Response.success(json.decodeFromString<PriceAlerts>(response(listOf("mock_1", "mock_2")))))
        val error = """{"code":"RATE_LIMITED","message":"mock retry","requestId":"mock-req","retryAfterSec":7}"""
        val next429 = CompletableDeferred<Response<PriceAlerts>>()
        val retryNext = CompletableDeferred<Response<PriceAlerts>>()
        api.deferred += next429; api.deferred += retryNext
        api.replies.add(Response.success(json.decodeFromString<PriceAlerts>(response(listOf("mock_3"), page = 2))))
        var now = System.currentTimeMillis()
        val vm = vm(api, GateApi()).also { it.clock = { now } }; advanceUntilIdle()
        vm.next(); runCurrent(); vm.next()
        assertEquals(2, api.queries.size)
        next429.complete(Response.error(error.toResponseBody(), okhttp3.Response.Builder().protocol(okhttp3.Protocol.HTTP_1_1).request(okhttp3.Request.Builder().url("http://127.0.0.1/price-alerts?page=2").build()).header("Retry-After", "7").code(429).message("MOCK").build()))
        advanceUntilIdle()
        assertEquals(2, api.queries.size); assertEquals("items=${vm.items} page=${vm.page} total=${vm.total} loading=${vm.loading} failure=${vm.failure}", 2, vm.items.size); assertNotNull(vm.failure)
        assertEquals(7, vm.retryAfter)
        now += 7_000; vm.updateRetryRemaining()
        vm.retry(); runCurrent(); vm.retry()
        assertEquals(3, api.queries.size)
        retryNext.complete(Response.success(NetworkFactory.json.decodeFromString<PriceAlerts>(response(listOf("mock_3"), page = 2))))
        advanceUntilIdle()
        assertEquals(3, api.queries.size); assertEquals(listOf(1,2,2), api.queries.map { it.third.first })
        assertEquals(3, vm.items.size); assertNull(vm.failure); vm.next(); assertEquals(3, api.queries.size)
        assertFalse(vm.loading)
    }
    @Test fun emptyFailureSessionInvalidationAndRealDetailSelectionsAreNotSynthetic() = runTest {
        val api = Api(); api.replies += Response.success(NetworkFactory.json.decodeFromString<PriceAlerts>(response(emptyList(), total = 0)))
        val vm = vm(api, GateApi()); advanceUntilIdle()
        assertEquals(0, vm.total); assertTrue(vm.items.isEmpty()); assertEquals(1, vm.page)
        val held = CompletableDeferred<Response<PriceAlerts>>(); api.deferred += held
        vm.retry(); runCurrent(); tokens.accessToken = null
        held.complete(Response.success(NetworkFactory.json.decodeFromString<PriceAlerts>(response(listOf("old_session"), total = 1))))
        advanceUntilIdle(); assertTrue(vm.items.isEmpty()); assertEquals(0, vm.page); assertFalse(vm.loading); assertNull(vm.failure)
        val closedVm = vm(Api(), GateApi(mapOf("priceAlerts" to false))); assertTrue(closedVm.featureState.value.enabled.not())
        closedVm.load(); assertTrue(closedVm.items.isEmpty()) // closed gate is checked before every list request.
    }
}
