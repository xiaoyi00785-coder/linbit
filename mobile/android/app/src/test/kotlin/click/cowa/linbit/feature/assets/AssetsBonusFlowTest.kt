package click.cowa.linbit.feature.assets

import android.app.Application
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiFuturesAccount
import click.cowa.linbit.core.generated.ApiFuturesBonusAvailable
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.feature.more.bonusAmount
import click.cowa.linbit.ui.components.ToastCenter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withTimeout
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.math.BigDecimal
import java.util.concurrent.atomic.AtomicReference

/** 纯回环：独立 /futures/bonus 串接资产卡；切账号清空；不计入余额；不发真实外网请求。 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], application = Application::class)
@OptIn(ExperimentalCoroutinesApi::class)
class AssetsBonusFlowTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private lateinit var server: MockWebServer
    private lateinit var tokens: InMemoryTokenStore
    private lateinit var scope: CoroutineScope
    private lateinit var vm: AssetsViewModel
    private val bonusBody = AtomicReference("""{"available":{"trial":null,"deduction":null},"items":[]}""")
    private val futuresBody = AtomicReference("""{"marginBalance":"100.00","availableBalance":"80.00","walletBalance":"100.00","crossPositionMargin":"0","isolatedMargin":"0","crossUnrealizedPnl":"0","isolatedUnrealizedPnl":"0","orderMargin":"0"}""")

    private fun json(body: String) =
        MockResponse().setHeader("Content-Type", "application/json").setBody(body)

    @Before fun setup() {
        Dispatchers.setMain(dispatcher)
        tokens = InMemoryTokenStore()
        server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path.orEmpty()
                return when {
                    path.startsWith("/assets/balances") -> json(
                        """{"items":[{"asset":"USDT","free":"50.00","locked":"0","total":"50.00"}],"page":1,"pageSize":200,"total":1}"""
                    )
                    path.startsWith("/futures/account") -> json(futuresBody.get())
                    path.startsWith("/futures/bonus") -> json(bonusBody.get())
                    path.startsWith("/wallet/networks") -> json("""{"networks":[]}""")
                    else -> MockResponse().setResponseCode(404).setHeader("Content-Type", "application/json")
                        .setBody("""{"message":"not mocked $path"}""")
                }
            }
        }
        server.start()
        val http = OkHttpClient()
        val base = server.url("/").newBuilder().host("127.0.0.1").build().toString()
        val api = NetworkFactory.api(http, base)
        scope = CoroutineScope(Dispatchers.Main)
        vm = AssetsViewModel(api, tokens, MarketStore(api, MarketSocket(http, "ws://127.0.0.1/", "fake", scope, accessToken = { null }), scope), ToastCenter(scope))
    }

    @After fun tearDown() {
        vm.viewModelScope.cancel()
        scope.cancel(); server.shutdown(); Dispatchers.resetMain()
    }

    private fun awaitLoaded() = runBlocking {
        withTimeout(5_000) {
            while (vm.state.value.loading) delay(10)
        }
    }

    @Test fun loadWiresIndependentBonusAndExcludesFromTotal() = runBlocking {
        bonusBody.set("""{"available":{"trial":"12.50","deduction":null},"items":[]}""")
        futuresBody.set("""{"marginBalance":"100.00","availableBalance":"80.00","walletBalance":"100.00","crossPositionMargin":"0","isolatedMargin":"0","crossUnrealizedPnl":"0","isolatedUnrealizedPnl":"0","orderMargin":"0"}""")
        tokens.accessToken = "INVALID_LOCAL_ONLY"
        awaitLoaded()
        vm.load()
        awaitLoaded()
        assertEquals("12.50", vm.state.value.bonus?.trial)
        assertNull(vm.state.value.bonus?.deduction)
        assertEquals("--", bonusAmount(vm.state.value.bonus?.deduction))
        assertEquals("12.5", bonusAmount(vm.state.value.bonus?.trial))
        vm.setTab(2)
        assertEquals(0, vm.totalUsdt().compareTo(BigDecimal("100.00")))
        assertEquals(0, Dec.parse(vm.state.value.futures?.marginBalance).compareTo(BigDecimal("100.00")))
    }

    @Test fun emptyBonusAndLogoutClearsWithoutKeepingStaleTrial() = runBlocking {
        bonusBody.set("""{"available":{"trial":null,"deduction":""},"items":[]}""")
        tokens.accessToken = "INVALID_LOCAL_ONLY"
        awaitLoaded()
        vm.load()
        awaitLoaded()
        assertEquals("--", bonusAmount(vm.state.value.bonus?.trial))
        assertEquals("--", bonusAmount(vm.state.value.bonus?.deduction))
        tokens.clear()
        withTimeout(5_000) { while (vm.state.value.bonus != null || vm.state.value.futures != null) delay(10) }
        assertNull(vm.state.value.bonus)
        assertNull(vm.state.value.futures)
        assertTrue(vm.state.value.spot.isEmpty())
    }

    @Test fun seedEvidenceFuturesDoesNotAddBonusIntoBalance() {
        vm.seedEvidenceFutures(
            futures = ApiFuturesAccount(marginBalance = "200", availableBalance = "150"),
            bonus = ApiFuturesBonusAvailable(trial = "3.00", deduction = "1.00"),
        )
        assertEquals("3.00", vm.state.value.bonus?.trial)
        assertEquals("1.00", vm.state.value.bonus?.deduction)
        assertEquals(2, vm.state.value.tab)
        assertEquals(0, vm.totalUsdt().compareTo(BigDecimal("200")))
    }
}
