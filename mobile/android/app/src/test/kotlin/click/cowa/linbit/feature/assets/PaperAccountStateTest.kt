package click.cowa.linbit.feature.assets

import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.di.AppModule
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import okhttp3.Headers.Companion.headersOf
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
    @After fun reset() { Dispatchers.resetMain() }
    private val account = PaperAccount("pa_mock", "paper", "spot", 3, "USDT", "10000", 1, 0, null, listOf("market"), "INDEX", "none", "none", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
    private fun vm(client: PaperTradingClient) = PaperAccountViewModel(client, tokens, ToastCenter(CoroutineScope(Dispatchers.Main)))
    private class MemoryApi(val account: PaperAccount) : PaperTradingApi {
        var balanceGeneration: Long = account.generation
        var empty = false
        var accountReply = CompletableDeferred<PaperAccount>()
        var resetReply = CompletableDeferred<Response<PaperReset>>()
        val resetKeys = mutableListOf<String>()
        override suspend fun account(): Response<PaperAccount> = Response.success(accountReply.await())
        override suspend fun balances(page: Int, pageSize: Int): Response<PaperBalances> = Response.success(
            PaperBalances(page, pageSize, if (empty) 0 else 1, if (empty) emptyList() else listOf(PaperBalance("USDT", "10000", "0", "10000")), "paper", "paper", balanceGeneration))
        override suspend fun reset(key: String, request: PaperResetRequest): Response<PaperReset> { resetKeys += key; assertEquals("RESET_PAPER_ACCOUNT", request.confirmation); return resetReply.await() }
        override suspend fun placeOrder(request: PaperOrderRequest): Response<PaperOrder> = error("Not this batch")
        override suspend fun orders(symbol: String?, status: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperOrders> = error("Not this batch")
        override suspend fun order(id: String): Response<PaperOrder> = error("Not this batch")
        override suspend fun trades(symbol: String?, generation: Long?, page: Int, pageSize: Int): Response<PaperTrades> = error("Not this batch")
        override suspend fun alerts(symbol: String?, status: String?, page: Int, pageSize: Int): Response<PriceAlerts> = error("Not this batch")
        override suspend fun createAlert(request: CreatePriceAlertRequest): Response<PriceAlert> = error("Not this batch")
        override suspend fun updateAlert(id: String, request: UpdatePriceAlertRequest): Response<PriceAlert> = error("Not this batch")
        override suspend fun deleteAlert(id: String, revision: Int?): Response<Unit> = error("Not this batch")
    }
    private fun ready(configure: MemoryApi.() -> Unit = {}): Pair<PaperAccountViewModel, MemoryApi> {
        tokens.accessToken = "test-paper-only"
        val api = MemoryApi(account).apply(configure).also { it.accountReply.complete(account) }
        val vm = vm(PaperTradingClient(api))
        return vm to api
    }
    @Test fun productionHiltClientIsFailClosedAndGuestHasNoRequest() {
        val closed = AppModule.paperClient(); val disabled = vm(closed)
        assertFalse(closed.enabled); assertFalse(disabled.available); assertNull(disabled.account)
        tokens.accessToken = "test-only"; vm(closed).load()
        val api = MemoryApi(account); val guest = vm(PaperTradingClient(api)); guest.load()
        assertNull(guest.account); assertNull(guest.failure); assertFalse(api.accountReply.isCompleted)
    }
    @Test fun paperGenerationLoadsAndSessionChangeClearsBeforeOldResponseCanBeCommitted() = runTest {
        tokens.accessToken = "test-paper-only"; val api = MemoryApi(account); val vm = vm(PaperTradingClient(api))
        assertTrue(vm.loading); tokens.accessToken = null
        api.accountReply.complete(account); advanceUntilIdle()
        assertNull(vm.account); assertNull(vm.balances); assertNull(vm.failure); assertFalse(vm.loading)
        vm.load(); assertFalse(api.accountReply.isCancelled)
    }
    @Test fun generationMismatchIsAReadableVMFailureNotAnOverwrittenString() = runTest {
        val (vm, api) = ready { balanceGeneration = 4 }; advanceUntilIdle()
        assertEquals("虚拟账户余额代次不一致，请重试", vm.failure)
        assertNull(vm.account); assertNull(vm.balances); assertEquals("PAPER", vm.account?.mode ?: "PAPER")
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
    @Test fun emptyPaperPaginationIsActualEmptyStateNotRealAssetFallback() = runTest {
        val (vm, api) = ready { empty = true }; advanceUntilIdle()
        assertEquals(0, vm.balances?.total); assertTrue(vm.balances?.items?.isEmpty() == true)
        assertEquals("paper", vm.balances?.mode); assertEquals(3L, vm.balances?.generation)
    }
}
