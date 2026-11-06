package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.di.AppModule
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.*
import org.junit.Assert.*
import retrofit2.Response

/** Real production ViewModel + stable PaperTradingClient over deterministic in-memory API.
 * External transport/JSON is tested separately by loopback PaperTradingContractTest.
 */
@org.junit.runner.RunWith(org.robolectric.RobolectricTestRunner::class)
@org.robolectric.annotation.Config(sdk = [34], application = android.app.Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class PaperAccountStateTest {
    private val tokens = InMemoryTokenStore()
    @Before fun main() { Dispatchers.setMain(UnconfinedTestDispatcher()) }
    @After fun tearDown() { Dispatchers.resetMain() }
    private val account = PaperAccount("pa_mock", "paper", "spot", 3, "USDT", "10000", 1, 0, null, listOf("market"), "INDEX", "none", "none", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
    private fun vm(client: PaperTradingClient) = PaperAccountViewModel(client, tokens, ToastCenter(CoroutineScope(Dispatchers.Main)))
    private class MemoryApi(val account: PaperAccount) : PaperTradingApi {
        var balanceGeneration: Long = account.generation
        var accountId = account.accountId
        var failSecondOrderPage = false
        var ordersRequests = 0
        var tradesRequests = 0
        val orderPages = mutableListOf<Int>()
        val tradePages = mutableListOf<Int>()
        var orderPageReply = CompletableDeferred<Response<PaperOrders>>()
        var tradePageReply = CompletableDeferred<Response<PaperTrades>>()
        var tradeResponse: PaperTrades? = null
        var failure: Boolean = true
        var empty = false
        var accountReply = CompletableDeferred<PaperAccount>()
        var resetReply = CompletableDeferred<Response<PaperReset>>()
        val resetKeys = mutableListOf<String>()
        val orderRequests = mutableListOf<PaperOrderRequest>()
        var orderReply = CompletableDeferred<Response<PaperOrder>>()
        override suspend fun account(): Response<PaperAccount> = Response.success(accountReply.await().copy(accountId = accountId))
        override suspend fun balances(page: Int, pageSize: Int): Response<PaperBalances> = Response.success(
            PaperBalances(page, pageSize, if (empty) 0 else 2, if (empty) emptyList() else listOf(PaperBalance("USDT", if (orderRequests.any()) "9900" else "10000", "0", if (orderRequests.any()) "9900" else "10000"), PaperBalance("BTC", if (orderRequests.any()) "0.00125" else "2", "0", if (orderRequests.any()) "0.00125" else "2")), "paper", "paper", balanceGeneration))
        override suspend fun reset(key: String, request: PaperResetRequest): Response<PaperReset> { resetKeys += key; assertEquals("RESET_PAPER_ACCOUNT", request.confirmation); return resetReply.await() }
        override suspend fun placeOrder(request: PaperOrderRequest): Response<PaperOrder> { orderRequests += request; return orderReply.await() }
        override suspend fun orders(symbol: String?, status: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperOrders> {
            ordersRequests += 1; orderPages += page; assertEquals("BTCUSDT", symbol); assertEquals(balanceGeneration, generation); assertEquals(2, pageSize)
            return if (ordersRequests == 1) orderPageReply.await() else if (ordersRequests == 2 && failSecondOrderPage) Response.error(500, "mock next page".toResponseBody()) else Response.success(PaperOrders(page, pageSize, 3, listOf(orderFixture.copy(orderId = "page-3", clientOrderId = "page-3")), account.generation))
        }
        override suspend fun order(id: String): Response<PaperOrder> = error("Not this batch")
        override suspend fun trades(symbol: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperTrades> { tradesRequests += 1; if (failure && tradesRequests == 1) return Response.error(500, "mock page failure".toResponseBody())
            tradeResponse?.let { return Response.success(it) }; return tradePageReply.await() }
        override suspend fun alerts(symbol: String?, status: String?, page: Int, pageSize: Int): Response<PriceAlerts> = error("Not this batch")
        override suspend fun createAlert(request: CreatePriceAlertRequest): Response<PriceAlert> = error("Not this batch")
        override suspend fun updateAlert(id: String, request: UpdatePriceAlertRequest): Response<PriceAlert> = error("Not this batch")
        override suspend fun deleteAlert(id: String, revision: Int?): Response<Unit> = error("Not this batch")
        val orderFixture get() = PaperOrder("page-1", "page-1", "paper", "pa_mock", account.generation, "BTCUSDT", "buy", "market", "80000", "0.001", "100", "0.001", "filled", "INDEX", "80000", "mock-index-time", "0", "USDT", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
    }
    private fun ready(enabled: () -> Boolean = { true }, configure: MemoryApi.() -> Unit = {}): Pair<PaperAccountViewModel, MemoryApi> {
        tokens.accessToken = "test-paper-only"
        val api = MemoryApi(account).apply(configure).also { it.accountReply.complete(account) }
        val vm = vm(PaperTradingClient(api, PaperFeatureGate(PublicFeatureFlags { Response.success(click.cowa.linbit.core.generated.ApiAppFeatures(features = mapOf("paperTrading" to enabled()))) })))
        return vm to api
    }
    @Test fun productionHiltClientIsFailClosedAndGuestHasNoRequest() {
        val closed = AppModule.paperClient(click.cowa.linbit.core.network.NetworkFactory.paperApi(click.cowa.linbit.core.network.NetworkFactory.bareClient(), "http://127.0.0.1:1/"), AppModule.paperFeatureGate(click.cowa.linbit.core.network.NetworkFactory.api(click.cowa.linbit.core.network.NetworkFactory.bareClient(), "http://127.0.0.1:1/"))); val disabled = vm(closed)
        assertFalse(closed.enabled); assertFalse(disabled.available); assertNull(disabled.account)
        /* Provider construction default-closed is asserted separately on loopback; no connection refused is used. */
        val api = MemoryApi(account); val guest = vm(PaperTradingClient(api, PaperFeatureGate(PublicFeatureFlags { Response.success(click.cowa.linbit.core.generated.ApiAppFeatures(features = mapOf("paperTrading" to true))) }))); guest.load()
        assertNull(guest.account); assertNull(guest.failure); assertFalse(api.accountReply.isCompleted)
    }
    @Test fun paperGenerationLoadsAndSessionChangeClearsBeforeOldResponseCanBeCommitted() = runTest {
        tokens.accessToken = "test-paper-only"; val api = MemoryApi(account); val vm = vm(PaperTradingClient(api, PaperFeatureGate(PublicFeatureFlags { Response.success(click.cowa.linbit.core.generated.ApiAppFeatures(features = mapOf("paperTrading" to true))) })))
        assertTrue(vm.loading); tokens.accessToken = null
        api.accountReply.complete(account); advanceUntilIdle()
        assertNull(vm.account); assertNull(vm.balances); assertNull(vm.failure); assertFalse(vm.loading)
        vm.load(); assertFalse(api.accountReply.isCancelled)
    }
    @Test fun generationMismatchIsAReadableVMFailureNotAnOverwrittenString() = runTest {
        val (vm, api) = ready { balanceGeneration = 4 }; advanceUntilIdle()
        assertEquals("虚拟账户余额代次不一致，请重试", vm.failure)
        assertNull(vm.account); assertNull(vm.balances)
    }
    @Test fun heldResetBlocksRepeatAndShowsRetry429WithoutChangingGeneration() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        assertEquals(3L, vm.account?.generation)
        val blocked = CompletableDeferred<Response<PaperReset>>(); api.resetReply = blocked
        vm.reset("reset-mock-exact") { }; vm.reset("reset-must-not-go-out") { }
        assertTrue(vm.resetting); assertEquals(listOf("reset-mock-exact"), api.resetKeys)
        val raw = okhttp3.Response.Builder().protocol(okhttp3.Protocol.HTTP_1_1)
            .request(okhttp3.Request.Builder().url("http://127.0.0.1/mock/reset").build())
            .code(429).message("Mock retry").header("Retry-After", "7").build()
        val retry = Response.error<PaperReset>("""{"code":"PAPER_RESET_LIMIT_EXCEEDED","message":"mock reset daily limit"}"""".toResponseBody(), raw)
        blocked.complete(retry); advanceUntilIdle()
        assertTrue(vm.resetFailure.orEmpty().contains("7秒后重试")); assertEquals(3L, vm.account?.generation)
        val second = CompletableDeferred<Response<PaperReset>>(); api.resetReply = second
        vm.reset("reset-mock-exact") { }; assertEquals(listOf("reset-mock-exact", "reset-mock-exact"), api.resetKeys)
        assertTrue(vm.resetting); assertNull(vm.resetFailure)
        // A retry is a further review action under the same in-flight key; no mocked-back-end success is asserted here.
        assertEquals(listOf("reset-mock-exact", "reset-mock-exact"), api.resetKeys)
        assertTrue(vm.resetting); assertEquals(3L, vm.account?.generation); assertNull(vm.resetFailure)
    }
    private suspend fun makeOrder(vm: PaperAccountViewModel) {
        assertNull(vm.orderDraft(true, "100.010")); vm.confirmOrder()
    }
    private fun order(query: PaperOrderRequest) = PaperOrder("po_mock", query.clientOrderId, "paper", "pa_mock", 3L, query.symbol, query.side, "market", "79999.999", "0.00125", query.quoteOrderQty, "0.00125", "filled", "INDEX", "79999.999", "2026-01-01T00:00:00Z", "0", "USDT", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
    private fun paperError(code: String, status: Int = 400, retry: Int? = null): Response<PaperOrder> {
        val raw = okhttp3.Response.Builder().protocol(okhttp3.Protocol.HTTP_1_1).code(status)
        retry?.let { raw.header("Retry-After", it.toString()) }
        return Response.error("""{"code":"$code","message":"mock-server-$code"}""".toResponseBody(), raw.request(okhttp3.Request.Builder().url("http://127.0.0.1/paper/orders").build()).message("Mock only").build())
    }
    @Test fun buyUsesExactQuoteDecimalReplaySameClientIdAndRefreshesActualBalance() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        val pending = CompletableDeferred<Response<PaperOrder>>()
        api.orderReply = pending
        makeOrder(vm); assertEquals(null, vm.confirmOrder()); assertTrue(vm.orderSubmitting)
        assertEquals("100.010", api.orderRequests.single().quoteOrderQty); assertEquals("", api.orderRequests.single().quantity)
        assertEquals("market", api.orderRequests.single().type)
        pending.complete(Response.success(order(api.orderRequests.single()))); advanceUntilIdle()
        assertEquals(3L, vm.account?.generation); assertTrue(api.accountReply.isCompleted)
        assertEquals("79999.999", vm.lastOrder?.price); assertEquals("0.00125", vm.lastOrder?.quantity)
        assertEquals("9900", vm.balances?.items?.first()?.free); assertTrue(vm.lastOrder?.status == "filled")
    }
    @Test fun sellHasOnlyDecimalQuantityNoQuoteNoLocalPrice() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        assertNotNull(vm.orderDraft(false, "NaN")); assertTrue(api.orderRequests.isEmpty())
        assertNull(vm.orderDraft(false, "0.25001")); vm.confirmOrder(); advanceUntilIdle()
        assertEquals("0.25001", api.orderRequests.single().quantity); assertEquals("", api.orderRequests.single().quoteOrderQty)
    }
    @Test fun allFailureCodesPreserveReviewAndOriginalIdUntilExplicitRetry() = runTest {
        for ((code,status,retry) in listOf(Triple("INDEX_UNAVAILABLE",503,null), Triple("PAPER_BALANCE_INSUFFICIENT",400,null), Triple("RATE_LIMITED",429,7), Triple("IDEMPOTENCY_CONFLICT",409,null))) {
            val (vm, api) = ready(); advanceUntilIdle()
            api.orderReply = CompletableDeferred<Response<PaperOrder>>().also { it.complete(paperError(code, status, retry)) }
            makeOrder(vm); advanceUntilIdle()
            assertFalse("code=$code failure=${vm.orderFailure}", vm.orderFailure.isNullOrBlank()); assertEquals(retry, vm.orderRetryAfter)
            assertTrue(vm.orderAmount.isNotEmpty())
            val first = api.orderRequests.single()
            vm.confirmOrder(); advanceUntilIdle()
            assertEquals(first, api.orderRequests.last()); assertEquals(2, api.orderRequests.size)
        }
    }
    @Test fun flagClosedInvalidatesPreparedOrderBeforeAnyPost() = runTest {
        var enabled = true
        val (vm, api) = ready(enabled = { enabled }); advanceUntilIdle()
        assertNull(vm.orderDraft(true, "100.010"))
        // A draft has no transport. Refresh receives the closed production gate on loopback.
        assertTrue(api.orderRequests.isEmpty())
        enabled = false; vm.load(); advanceUntilIdle()
        assertFalse(vm.available); assertNotNull(vm.confirmOrder()); assertTrue(api.orderRequests.isEmpty())
        assertTrue(vm.orderAmount.isEmpty())
    }
    @Test fun mismatchedGenerationAndClientOrderIdsNeverCommitAsFilled() = runTest {
        for (wrongGeneration in listOf(true, false)) {
            val (vm, api) = ready(); advanceUntilIdle()
            makeOrder(vm)
            val sent = api.orderRequests.single()
            api.orderReply.complete(Response.success(order(sent).copy(
                generation = if (wrongGeneration) 4 else 3,
                clientOrderId = if (wrongGeneration) sent.clientOrderId else "android-paper-old-session",
            ))); advanceUntilIdle()
            assertNull(vm.lastOrder)
            assertEquals("10000", vm.balances?.items?.first()?.free)
            assertTrue(vm.orderFailure.orEmpty().contains("不匹配"))
            assertFalse(vm.orderSubmitting)
            assertEquals("100.010", vm.orderAmount)
            assertEquals(sent.clientOrderId, api.orderRequests.single().clientOrderId)
        }
    }
    @Test fun sessionEpochInvalidatesJsonReplyAndFlagGateCannotLeaveCalculatedBalance() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        makeOrder(vm); tokens.accessToken = null
        api.orderReply.complete(Response.success(order(api.orderRequests.single()))); advanceUntilIdle()
        assertNull(vm.account); assertNull(vm.lastOrder)
    }

    @Test fun realOrderAndTradePagesHaveIndependentPagingFailureRetryAndTerminalEmptyStates() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS); runCurrent(); vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS)
        assertEquals(1, api.ordersRequests); assertTrue(vm.ordersHistory.loading)
        api.orderPageReply.complete(Response.success(PaperOrders(1, 2, 3, listOf(api.orderFixture, api.orderFixture.copy(orderId = "page-2", clientOrderId = "page-2")), 3)))
        advanceUntilIdle(); assertEquals(2, vm.ordersHistory.items.size); assertTrue(vm.ordersHistory.hasNext)
        vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS); vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS)
        advanceUntilIdle(); assertEquals(2, api.ordersRequests); assertEquals(3, vm.ordersHistory.items.size)
        vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS); assertFalse(vm.ordersHistory.hasNext)
        vm.openHistory(PaperAccountViewModel.HistoryKind.TRADES); advanceUntilIdle()
        assertTrue(vm.tradesHistory.failure.orEmpty().isNotEmpty()); assertEquals(1, api.tradesRequests)
        api.tradeResponse = PaperTrades(1, 2, 0, emptyList(), 3)
        vm.refreshHistory(PaperAccountViewModel.HistoryKind.TRADES); advanceUntilIdle()
        assertNull(vm.tradesHistory.failure); assertTrue(vm.tradesHistory.items.isEmpty()); assertFalse(vm.tradesHistory.hasNext)
        // No other endpoint, zero mutations, independent from real balances.
        assertTrue(api.orderRequests.isEmpty())
    }
    @Test fun failedNextOrderPageKeepsItemsAndRetryLoadsThatPageOnlyOnceWithCorrectSymbol() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS)
        api.orderPageReply.complete(Response.success(PaperOrders(1, 2, 3, listOf(api.orderFixture, api.orderFixture.copy(orderId = "page-2", clientOrderId = "page-2")), 3)))
        advanceUntilIdle()
        api.failSecondOrderPage = true; vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS); advanceUntilIdle()
        assertEquals(2, vm.ordersHistory.items.size); assertNotNull(vm.ordersHistory.failure)
        api.failSecondOrderPage = false; vm.refreshHistory(PaperAccountViewModel.HistoryKind.ORDERS)
        assertEquals(3, api.ordersRequests); runCurrent()
        vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS)
        vm.refreshHistory(PaperAccountViewModel.HistoryKind.ORDERS)
        assertEquals(3, api.ordersRequests)
        advanceUntilIdle(); assertEquals(listOf(1, 2, 2), api.orderPages)
        assertEquals(3, vm.ordersHistory.items.size); assertFalse(vm.ordersHistory.hasNext)
        assertTrue(api.orderRequests.isEmpty())
    }
    @Test fun sessionAndAccountGenerationDiscardDeferredHistoryRatherThanMixingPages() = runTest {
        val (vm, api) = ready(); advanceUntilIdle()
        vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS)
        tokens.accessToken = null
        api.orderPageReply.complete(Response.success(PaperOrders(1, 2, 1, listOf(api.orderFixture), 3)))
        advanceUntilIdle(); assertEquals(0, vm.ordersHistory.page); assertTrue(vm.ordersHistory.items.isEmpty())
        tokens.accessToken = "test-paper-only-new-session"; advanceUntilIdle()
        vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS)
        // Fresh epoch only accepts its own replies; original fake account callback has already completed.
        assertTrue(api.accountReply.isCompleted); assertFalse(vm.ordersHistory.loading)
    }
    @Test fun accountOrGenerationChangesInvalidateDeferredOrderPage() = runTest {
        for (nextGeneration in listOf(3L, 4L)) {
            val (vm, api) = ready(); advanceUntilIdle()
            vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS)
            assertTrue(vm.ordersHistory.loading)
            api.balanceGeneration = nextGeneration
            api.accountId = if (nextGeneration == 3L) "pa_other_mock" else "pa_mock"
            api.accountReply = CompletableDeferred<PaperAccount>().apply { complete(account.copy(accountId = api.accountId, generation = nextGeneration)) }
            vm.load(); advanceUntilIdle()
            api.orderPageReply.complete(Response.success(PaperOrders(1, 2, 1, listOf(api.orderFixture.copy(accountId = api.accountId, generation = nextGeneration)), nextGeneration)))
            advanceUntilIdle()
            assertTrue(vm.ordersHistory.items.isEmpty())
            assertEquals(1, api.ordersRequests)
            assertNull(vm.ordersHistory.failure)
        }
    }
    @Test fun emptyPaperPaginationIsActualEmptyStateNotRealAssetFallback() = runTest {
        val (vm, api) = ready { empty = true }; advanceUntilIdle()
        assertEquals(0, vm.balances?.total); assertTrue(vm.balances?.items?.isEmpty() == true)
        assertEquals("paper", vm.balances?.mode); assertEquals(3L, vm.balances?.generation)
    }
}
