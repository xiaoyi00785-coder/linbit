package click.cowa.linbit.feature.trade

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.generated.ApiAppFeatures
import click.cowa.linbit.core.generated.ApiFuturesContract
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.data.trade.BookLevel
import click.cowa.linbit.data.trade.OrderBook
import click.cowa.linbit.data.trade.TradeNav
import click.cowa.linbit.feature.assets.PaperAccountViewModel
import click.cowa.linbit.feature.auth.LocalLbSheetInlineEvidence
import click.cowa.linbit.ui.components.FloatingTabBar
import click.cowa.linbit.ui.components.MainTab
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File
import java.math.BigDecimal

/**
 * 模拟交易页（对照 EasiCoin Demo Trading 未登录态）。网络全拦截：行情为固定夹具，虚拟账户为内存 API。
 * -PoffscreenShots=1 时额外输出离屏渲染图到 build/reports/demo-shots/（不入库）。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h866dp-xxhdpi")
class DemoTradingScreenTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(Dispatchers.Main)
    private val klines = (0 until 60).joinToString(",", "{\"klines\":[", "]}") { i ->
        val base = 74500 + ((i * 37) % 23 - 11) * 40 - (if (i in 18..24) 900 else 0)
        "{\"openTime\":${1_762_000_000_000L + i * 900_000L},\"open\":\"$base\",\"high\":\"${base + 120}\",\"low\":\"${base - 140}\",\"close\":\"${base + ((i % 3) - 1) * 60}\",\"volume\":\"12\"}"
    }
    private val client = OkHttpClient.Builder().addInterceptor { chain ->
        val path = chain.request().url.encodedPath
        val (code, body) = if (path.endsWith("/futures/market/klines")) 200 to klines else 503 to "{}"
        Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(code).message("fixture").body(body.toResponseBody()).build()
    }.build()
    private val api = NetworkFactory.api(client, "https://example.invalid/")
    private val socket = MarketSocket(client, "wss://example.invalid/ws", "shot", scope, accessToken = { null })
    private val store = MarketStore(api, socket, scope)
    private val tokens = InMemoryTokenStore()
    private val nav = TradeNav()
    private val toast = ToastCenter(scope)
    private var logins = 0

    private class MemoryPaperApi : PaperTradingApi {
        val orderRequests = mutableListOf<PaperOrderRequest>()
        val orderSymbols = mutableListOf<String?>()
        val account = PaperAccount("pa_demo", "paper", "spot", 2, "USDT", "10000", 1, 0, null, listOf("market"), "INDEX", "none", "none", "2026-11-09T00:00:00Z", "2026-11-09T00:00:00Z")
        override suspend fun account() = retrofit2.Response.success(account)
        override suspend fun balances(page: Int, pageSize: Int) = retrofit2.Response.success(
            PaperBalances(page, pageSize, 2, listOf(PaperBalance("USDT", "9900", "0", "9900"), PaperBalance("BTC", "0.00125", "0", "0.00125", "93.10")), "paper", "paper", 2),
        )
        override suspend fun reset(key: String, request: PaperResetRequest): retrofit2.Response<PaperReset> = error("not used")
        override suspend fun placeOrder(request: PaperOrderRequest): retrofit2.Response<PaperOrder> {
            orderRequests += request
            return retrofit2.Response.success(
                PaperOrder("o1", request.clientOrderId, "paper", "pa_demo", 2, request.symbol, request.side, "market", "74480", "0.00134", request.quoteOrderQty.ifBlank { null }, "0.00134", "filled", "INDEX", "74480", "2026-11-09T06:00:00Z", "0", "USDT", "2026-11-09T06:00:00Z", "2026-11-09T06:00:00Z"),
            )
        }
        override suspend fun orders(symbol: String?, status: String?, generation: Long?, page: Int, pageSize: Int): retrofit2.Response<PaperOrders> {
            orderSymbols += symbol
            return retrofit2.Response.success(PaperOrders(page, pageSize, 0, emptyList(), 2))
        }
        override suspend fun order(id: String): retrofit2.Response<PaperOrder> = error("not used")
        override suspend fun trades(symbol: String?, generation: Long?, page: Int, pageSize: Int) = retrofit2.Response.success(PaperTrades(page, pageSize, 0, emptyList(), 2))
        override suspend fun alerts(symbol: String?, status: String?, page: Int, pageSize: Int): retrofit2.Response<PriceAlerts> = error("not used")
        override suspend fun createAlert(request: CreatePriceAlertRequest): retrofit2.Response<PriceAlert> = error("not used")
        override suspend fun updateAlert(id: String, request: UpdatePriceAlertRequest): retrofit2.Response<PriceAlert> = error("not used")
        override suspend fun deleteAlert(id: String, revision: Int?): retrofit2.Response<Unit> = error("not used")
    }
    private val paperApi = MemoryPaperApi()
    private fun paperVm() = PaperAccountViewModel(
        PaperTradingClient(paperApi, PaperFeatureGate { retrofit2.Response.success(ApiAppFeatures(features = mapOf("paperTrading" to true))) }),
        tokens, toast,
    )

    private fun bd(v: String) = BigDecimal(v)
    private fun futures(): FuturesTradeViewModel {
        val vm = FuturesTradeViewModel(api, store, socket, tokens, nav, toast)
        val m = bd("74451.4")
        val qty = listOf("3.72", "3.72", "18.61", "7.44", "3.72", "59.41", "3.72").map(::bd)
        vm.seedForShot(
            ApiFuturesContract("BTCUSDT", 100, "0.1", "0.0001", "1000", "BTC", "USDT"),
            ApiFuturesTicker(symbol = "BTCUSDT", lastPrice = "74451.4", markPrice = "74472.9", priceChangePercent24h = "-1.76", fundingRate = "0.000106", nextFundingTime = System.currentTimeMillis() + 7_336_000L),
            OrderBook(
                (1..7).map { BookLevel(m - bd("0.4") * it.toBigDecimal(), qty[it - 1].divide(bd("74.45"), 6, java.math.RoundingMode.HALF_UP)) },
                (1..7).map { BookLevel(m + bd("0.7") * it.toBigDecimal(), qty[7 - it].divide(bd("74.45"), 6, java.math.RoundingMode.HALF_UP)) },
            ),
            null,
        )
        return vm
    }
    private val rows = listOf(
        MarketRow("BTCUSDT", "BTC", "USDT", bd("74489.5"), bd("-1.69"), bd("3.2E8"), 1),
        MarketRow("ETHUSDT", "ETH", "USDT", bd("2128.37"), bd("-4.69"), bd("2.3E8"), 2),
        MarketRow("BNBUSDT", "BNB", "USDT", bd("684.87"), bd("-1.76"), bd("1.7E7"), 2),
        MarketRow("ARBUSDT", "ARB", "USDT", bd("0.1726"), bd("2.74"), bd("1.1E7"), 4),
        MarketRow("XRPUSDT", "XRP", "USDT", bd("1.0934"), bd("-5.98"), bd("1.4E7"), 4),
        MarketRow("SOLUSDT", "SOL", "USDT", bd("86.486"), bd("-2.95"), bd("9.8E7"), 3),
    )

    private fun host(content: @Composable () -> Unit, inline: Boolean = true, tabBar: Boolean = true) {
        rule.setContent {
            CompositionLocalProvider(LocalLbSheetInlineEvidence provides inline) {
                LinbitTheme(false) {
                    Box(Modifier.fillMaxSize().background(Color.White)) {
                        content()
                        if (tabBar) FloatingTabBar(MainTab.FUTURES, {}, Modifier.align(Alignment.BottomCenter).padding(bottom = 15.dp))
                    }
                }
            }
        }
        rule.waitForIdle()
    }
    private fun root(vm: FuturesTradeViewModel, paper: PaperAccountViewModel) = host({
        FuturesRootScreen(85.dp, ThemeMode.LIGHT, {}, onLogin = { logins++ }, onDeposit = {}, onTransfer = {}, onHistory = {}, initialTop = 3, autoStart = false, vm = vm, paperVm = paper)
    })
    private fun demo(vm: FuturesTradeViewModel, paper: PaperAccountViewModel, sheet: String? = null, chart: Boolean = false, tab: Int = 0, loggedIn: Boolean = false) = host({
        androidx.compose.foundation.layout.Column(Modifier.fillMaxSize()) {
            DemoTradingHeader({}, Modifier.padding(horizontal = 16.dp))
            DemoTradingContent(vm, paper, loggedIn, true, rows, if (sheet == null) 85.dp else 0.dp, ThemeMode.LIGHT, {}, { logins++ }, modifier = Modifier.weight(1f), initialSheet = sheet, initialChartOpen = chart, initialBottomTab = tab)
        }
    }, tabBar = sheet == null)

    private fun capture(name: String) {
        if (System.getProperty("offscreen.shots") != "1") return
        val out = File("build/reports/demo-shots").absoluteFile.also { it.mkdirs() }
        rule.runOnIdle {
            val view = rule.activity.findViewById<android.view.View>(android.R.id.content)
            val bitmap = Bitmap.createBitmap(view.width.coerceAtLeast(1), view.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            File(out, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test fun loggedOutDemoShowsDemoHeaderAndRoutesActionsToLogin() {
        root(futures(), paperVm())
        rule.onNodeWithText("返回实盘").assertIsDisplayed()
        assertTrue(rule.onAllNodesWithText("事件合约").fetchSemanticsNodes().isEmpty())
        rule.onNodeWithText("资金费率(8H) / 倒计时").assertIsDisplayed()
        capture("demo-main")
        rule.onNodeWithTag("demo.login.long").performClick()
        rule.onNodeWithTag("demo.login.short").performClick()
        rule.onNodeWithTag("demo.funds").performClick()
        rule.onNodeWithText("全仓 / 合仓").performClick()
        rule.onNodeWithContentDescription("模拟交易历史").performClick()
        rule.waitForIdle()
        assertEquals(5, logins)
        assertTrue("未登录不得下虚拟单", paperApi.orderRequests.isEmpty())
    }

    @Test fun goLiveShowsWelcomeTransitionThenLiveTabs() {
        root(futures(), paperVm())
        rule.mainClock.autoAdvance = false
        rule.onNodeWithTag("demo.goLive").performClick()
        rule.mainClock.advanceTimeByFrame()
        rule.onNodeWithText("实盘交易").assertIsDisplayed()
        capture("demo-go-live")
        rule.mainClock.advanceTimeBy(1200)
        rule.mainClock.autoAdvance = true
        rule.waitForIdle()
        assertTrue(rule.onAllNodesWithText("欢迎来到").fetchSemanticsNodes().isEmpty())
        rule.onNodeWithText("事件合约").assertIsDisplayed()
    }

    @Test fun orderTypeSheetOnlyAllowsContractMarketOrders() {
        demo(futures(), paperVm(), sheet = "kind")
        assertEquals(3, rule.onAllNodesWithText("模拟盘暂不支持").fetchSemanticsNodes().size)
        capture("demo-order-type")
        rule.onNodeWithText("限价单").performClick()
        rule.waitForIdle()
        // 不支持的类型不可选：弹层仍在，表单仍为市价单
        rule.onNodeWithText("委托类型").assertIsDisplayed()
        assertEquals(2, rule.onAllNodesWithText("市价单").fetchSemanticsNodes().size)
    }

    @Test fun toolsSheetHasNoNestedDemoEntry() {
        demo(futures(), paperVm(), sheet = "tools")
        rule.onNodeWithText("计算器").assertIsDisplayed()
        rule.onNodeWithText("历史资金费率").assertIsDisplayed()
        assertTrue(rule.onAllNodesWithText("模拟交易", useUnmergedTree = true).fetchSemanticsNodes().size == 1) // 仅顶栏标题
        capture("demo-tools")
    }

    @Test fun pairSheetListsRealFuturesRows() { demo(futures(), paperVm(), sheet = "pairs"); rule.onNodeWithText("SOLUSDT").assertIsDisplayed(); capture("demo-pairs") }
    @Test fun themeSheet() { demo(futures(), paperVm(), sheet = "theme"); rule.onNodeWithText("主题模式").assertIsDisplayed(); capture("demo-theme") }
    @Test fun precisionSheet() { demo(futures(), paperVm(), sheet = "precision"); rule.onNodeWithText("价格精度").assertIsDisplayed(); capture("demo-precision") }
    @Test fun bookModeSheet() { demo(futures(), paperVm(), sheet = "bookMode"); rule.onNodeWithText("默认").assertIsDisplayed(); capture("demo-bookMode") }

    @Test fun unitSheetDisablesValueUnit() {
        demo(futures(), paperVm(), sheet = "unit")
        rule.onNodeWithText("数量-BTC").assertIsDisplayed()
        rule.onNodeWithText("成本-USDT").assertIsDisplayed()
        capture("demo-unit")
        rule.onNodeWithText("价值-USDT").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("合约单位设置").assertIsDisplayed()
    }

    @Test fun chartExpandedRendersRealKlineEndpoint() {
        demo(futures(), paperVm(), chart = true)
        rule.waitUntil(5_000) { rule.onAllNodesWithText("BTCUSDT 图表").fetchSemanticsNodes().isNotEmpty() }
        rule.mainClock.advanceTimeBy(500); rule.waitForIdle()
        capture("demo-chart")
    }

    @Test fun ordersTabLoggedOut() {
        demo(futures(), paperVm(), tab = 1)
        rule.onNodeWithText("登录后查看模拟委托").assertExists()
        capture("demo-orders-tab")
    }

    @Test fun loggedInMarketBuySubmitsOnlyToPaperContractWithCurrentSymbol() {
        tokens.accessToken = "t"
        val vm = futures(); val paper = paperVm()
        rule.waitUntil(5_000) { paper.balances != null }
        demo(vm, paper, loggedIn = true)
        rule.waitUntil(5_000) { rule.onAllNodesWithText("模拟买入").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("仓位 (1)").assertIsDisplayed()
        rule.onNodeWithTag("demo.amount").performTextInput("100")
        rule.onNodeWithTag("demo.submit").performClick()
        rule.waitForIdle()
        rule.onNodeWithText("确认模拟买入").assertIsDisplayed()
        rule.onNodeWithTag("demo.confirm").performClick()
        rule.waitUntil(5_000) { paperApi.orderRequests.isNotEmpty() && paper.lastOrder != null }
        val req = paperApi.orderRequests.single()
        assertEquals("BTCUSDT", req.symbol); assertEquals("buy", req.side); assertEquals("market", req.type)
        assertEquals("100", req.quoteOrderQty); assertEquals("", req.quantity)
        // 历史分页默认不限交易对（只看当前关闭）
        rule.onNodeWithContentDescription("模拟交易历史").performClick()
        rule.waitUntil(5_000) { paperApi.orderSymbols.isNotEmpty() }
        assertEquals(null, paperApi.orderSymbols.last())
    }
}
