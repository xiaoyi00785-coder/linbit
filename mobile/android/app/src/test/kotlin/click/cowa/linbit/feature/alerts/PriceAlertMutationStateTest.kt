package click.cowa.linbit.feature.alerts

import click.cowa.linbit.core.generated.ApiAppFeatures
import click.cowa.linbit.core.generated.ApiError
import click.cowa.linbit.core.network.*
import click.cowa.linbit.data.paper.*
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.*
import org.junit.Assert.*
import retrofit2.Response

@org.junit.runner.RunWith(org.robolectric.RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class PriceAlertMutationStateTest {
    private val tokens = InMemoryTokenStore()
    @Before fun main() { Dispatchers.setMain(StandardTestDispatcher()); tokens.accessToken = "FAKE_LOOPBACK_TOKEN" }
    @After fun reset() { tokens.accessToken = null; Dispatchers.resetMain() }
    private fun alert(id: String = "m1", revision: Int = 1, threshold: String = "80000.00", condition: String = "gte") = PriceAlert(id, "ca_$id", "spot", "BTCUSDT", condition, threshold, "INDEX", "once", listOf("in_app"), true, "active", revision, PriceAlertLimit(50), deliveryStatus = "none", note = "mock", createdAt = "2026-01-01T00:00:00Z", updatedAt = "2026-01-01T00:00:00Z")
    private fun list(vararg alerts: PriceAlert, page: Int = 1): PriceAlerts = PriceAlerts(page, if (page == 1) 2 else 50, total = if (page == 1) alerts.size + 1 else alerts.size, items = alerts.toList())
    private class Transport : PaperTradingApi {
        val responses = ArrayDeque<Response<*>>()
        val requests = mutableListOf<Pair<String, Any>>()
        val held = mutableListOf<CompletableDeferred<Response<*>>>()
        val heldReads = mutableListOf<CompletableDeferred<Response<*>>>()
        fun result(value: Any) { responses += Response.success(value) }
        fun error(code: String, status: Int, after: Int? = null) {
            val raw = """{"code":"$code","message":"mock","retryAfterSec":${after ?: 0}}"""
            val base = okhttp3.Response.Builder().protocol(okhttp3.Protocol.HTTP_1_1).request(okhttp3.Request.Builder().url("http://127.0.0.1/mock").build()).code(status).message("mock")
            if (after != null) base.header("Retry-After", after.toString())
            responses += Response.error<Any>(raw.toResponseBody(), base.build())
        }
        @Suppress("UNCHECKED_CAST") private fun <T> next(): Response<T> = responses.removeFirst() as Response<T>
        override suspend fun alerts(symbol: String?, status: String?, page: Int, pageSize: Int): Response<PriceAlerts> { requests += "GET:$page" to pageSize; return if (heldReads.isNotEmpty()) heldReads.removeFirst().await() as Response<PriceAlerts> else next() }
        override suspend fun createAlert(request: CreatePriceAlertRequest): Response<PriceAlert> { requests += "POST" to request; return if (held.isNotEmpty()) held.removeFirst().await() as Response<PriceAlert> else next() }
        override suspend fun updateAlert(id: String, request: UpdatePriceAlertRequest): Response<PriceAlert> { requests += "PATCH:$id" to request; return if (held.isNotEmpty()) held.removeFirst().await() as Response<PriceAlert> else next() }
        override suspend fun deleteAlert(id: String, revision: Int?): Response<Unit> { requests += "DELETE:$id:$revision" to Unit; return if (held.isNotEmpty()) held.removeFirst().await() as Response<Unit> else next() }
        override suspend fun account() = error("PROHIBITED"); override suspend fun reset(key: String, request: PaperResetRequest): Response<PaperReset> = error("PROHIBITED")
        override suspend fun balances(page: Int, pageSize: Int): Response<PaperBalances> = error("PROHIBITED")
        override suspend fun placeOrder(request: PaperOrderRequest): Response<PaperOrder> = error("PROHIBITED")
        override suspend fun orders(symbol: String?, status: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperOrders> = error("PROHIBITED")
        override suspend fun order(id: String): Response<PaperOrder> = error("PROHIBITED")
        override suspend fun trades(symbol: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperTrades> = error("PROHIBITED")
    }
    private var now = 1_000L
    private fun vm(api: Transport): PriceAlertViewModel = PriceAlertViewModel(PriceAlertApiClient(api, PriceAlertFeatureGate { Response.success(ApiAppFeatures(mapOf("priceAlerts" to true))) }), tokens).also { it.clock = { now } }.also { it.refresh() }
    private suspend fun TestScope.open(vm: PriceAlertViewModel, edit: Boolean = false) {
        advanceUntilIdle()
        if (edit) vm.openEdit(vm.items.single { it.alertId == "m1" }) else vm.openCreate()
    }
    @Test fun createEditDeleteReviewCancelBusyAndDecimalContract() = runTest {
        val api = Transport(); api.result(list(alert())); val model = vm(api); open(model)
        model.editDraft { copy(threshold = "0") }; model.saveAlert(); assertTrue(api.requests.none { it.first.startsWith("POST") })
        model.editDraft { copy(threshold = "08.50", note = "user edit", condition = "lte") }
        api.result(alert("new1", threshold = "08.50", condition = "lte")); model.saveAlert(); model.saveAlert(); advanceUntilIdle()
        val create = api.requests.filter { it.first == "POST" }.single().second as CreatePriceAlertRequest
        assertEquals("08.50", create.threshold); assertEquals("lte", create.condition); assertEquals("spot", create.market); assertEquals("INDEX", create.priceSource)
        assertEquals("once", create.triggerMode); assertEquals(listOf("in_app"), create.channels); assertTrue(create.clientAlertId.length <= 64)
        assertEquals("已创建价格预警", model.successNotice)
        model.openEdit(model.items.single { it.alertId == "new1" }); model.editDraft { copy(threshold = "9.25") }; model.cancelAlertDialogs(); assertEquals(1, api.requests.count { !it.first.startsWith("GET") })
        model.openEdit(model.items.single { it.alertId == "new1" }); model.editDraft { copy(threshold = "9.25") }; api.result(alert("new1", revision = 2, threshold = "9.25")); model.saveAlert(); advanceUntilIdle()
        val update = api.requests.filter { it.first == "PATCH:new1" }.single().second as UpdatePriceAlertRequest
        assertEquals(1, update.revision); assertEquals("9.25", update.threshold); assertEquals(listOf("in_app"), update.channels)
        model.askDelete(model.items.single { it.alertId == "new1" }); model.cancelAlertDialogs(); assertEquals(2, api.requests.count { it.first.startsWith("POST") || it.first.startsWith("PATCH") })
        model.askDelete(model.items.single { it.alertId == "new1" }); api.result(Unit); model.confirmDelete(); advanceUntilIdle()
        assertEquals("DELETE:new1:2", api.requests.last().first); assertNull(model.pendingDelete); assertEquals("已删除价格预警", model.successNotice)
    }
    @Test fun updateOmitsUnchangedOptionalFieldsButStillCarriesRevisionAndPositiveDecimal() = runTest {
        val api = Transport(); api.result(list(alert())); vm(api); advanceUntilIdle()
        val before = api.requests.size
        api.result(alert()); api.createAlert(CreatePriceAlertRequest(clientAlertId = "example_symbol", symbol = "btc_usdt", condition = "gte", threshold = "0.00000001"))
        api.result(alert(revision = 8, threshold = "65.2500")); api.updateAlert("m1", UpdatePriceAlertRequest(revision = 7, threshold = "65.2500"))
        val value = org.json.JSONObject(click.cowa.linbit.core.network.NetworkFactory.json.encodeToString(UpdatePriceAlertRequest.serializer(), UpdatePriceAlertRequest(revision = 7)))
        assertEquals(7, value.getInt("revision")); assertFalse(value.has("threshold")); assertFalse(value.has("enabled")); assertFalse(value.has("note")); assertFalse(value.has("channels"))
        assertEquals(2 + before, api.requests.size)
    }
    @Test fun conflictRefreshFailureRetainsDraftWithoutBlockingRetryOrPoppingSuccess() = runTest {
        val api = Transport(); api.result(list(alert())); val model = vm(api); open(model, edit = true)
        model.editDraft { copy(threshold = "55.100", note = "draft") }
        api.error("REVISION_CONFLICT", 409); api.error("SERVICE_UNAVAILABLE", 503)
        model.saveAlert(); advanceUntilIdle()
        assertEquals("55.100", model.editor?.threshold); assertEquals("draft", model.editor?.note); assertEquals(1, model.editor?.revision)
        assertTrue(model.canSave()); assertNull(model.successNotice); assertFalse(model.conflictRefreshing)
        assertTrue(model.mutationError?.message?.contains("保留您的编辑") == true)
        model.saveAlert(); runCurrent(); assertFalse(model.mutationBusy); assertFalse(model.conflictRefreshing)
    }
    @Test fun bothLiteral429ClockBlocksWritesAndReadRetries() = runTest {
        val api = Transport(); api.result(list(alert())); val model = vm(api); open(model)
        model.editDraft { copy(threshold = "12.30") }; api.error("RATE_LIMITED", 429, 7); model.saveAlert(); advanceUntilIdle()
        assertEquals(7, model.retryRemaining); assertEquals(7, model.retryAfter)
        // Main virtual time must not bypass a supplied real wall clock.
        now += 4_000; model.updateRetryRemaining(); model.saveAlert(); assertEquals(1, api.requests.count { !it.first.startsWith("GET") })
        now += 3_000; model.updateRetryRemaining(); assertEquals(0, model.retryRemaining)
        api.result(alert("new2", threshold = "12.30")); model.saveAlert(); advanceUntilIdle()
        assertEquals(2, api.requests.count { !it.first.startsWith("GET") }); assertEquals("12.30", (api.requests.last().second as CreatePriceAlertRequest).threshold)
    }
    @Test fun realConflictReadsSameIdAcrossPagesAndNeverTreatsSessionStaleAsConflict() = runTest {
        val api = Transport(); api.result(list(alert())); val model = vm(api); open(model, edit = true)
        model.editDraft { copy(threshold = "99.75", note = "preserved") }
        api.error("REVISION_CONFLICT", 409)
        api.result(PriceAlerts(1,50,51,List(1) { alert("other") }))
        api.result(PriceAlerts(2,50,51,listOf(alert(revision = 9, threshold = "81.00"))))
        model.saveAlert(); advanceUntilIdle()
        assertEquals(3, api.requests.filter { it.first.startsWith("GET") }.size)
        assertEquals("99.75", model.editor?.threshold); assertEquals("preserved", model.editor?.note); assertEquals(9, model.editor?.revision)
        assertNull(model.successNotice)
        api.error("SESSION_STALE", 401); model.saveAlert(); advanceUntilIdle()
        assertNull(model.editor); assertNull(model.successNotice) // session expires; never considered a revision conflict or refreshed as one
    }
    @Test fun failedFeatureClosesNoFurtherAlertRequest() = runTest {
        val api = Transport(); api.result(list(alert())); val model = vm(api); open(model)
        model.editDraft { copy(threshold = "11.00") }; api.error("FEATURE_DISABLED", 403); model.saveAlert(); advanceUntilIdle()
        assertFalse(model.featureState.value.enabled); assertNull(model.editor); assertNull(model.successNotice)
        model.openCreate(); model.saveAlert(); assertEquals(2, api.requests.size)
    }
    @Test fun generationDropsOldSuccessAndCannotPopOutAfterCancellation() = runTest {
        val api = Transport(); api.result(list(alert())); val model = vm(api); open(model, edit = true)
        model.editDraft { copy(threshold = "8.9", note = "unchanged before switch") }
        val heldSave = CompletableDeferred<Response<*>>(); api.held += heldSave
        model.saveAlert(); model.saveAlert(); runCurrent()
        assertEquals(api.requests.toString(), 1, api.requests.count { it.first.startsWith("PATCH") })
        val oneRequest = api.requests.count { it.first.startsWith("PATCH") }
        model.cancelAlertDialogs(); assertEquals(oneRequest, api.requests.count { it.first.startsWith("PATCH") })
        api.result(list(alert("new-session")))
        tokens.accessToken = "FAKE_NEW_SESSION"; advanceUntilIdle()
        heldSave.complete(Response.success(alert("m1", revision = 2, threshold = "8.9")))
        advanceUntilIdle()
        assertNull(model.editor); assertNull(model.successNotice)
        model.openCreate(); model.cancelAlertDialogs(); assertNull(model.editor)
        assertEquals("PATCH count cannot grow through old response/cancel: " + api.requests, 1, api.requests.count { it.first.startsWith("PATCH") })
    }
}
