package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Real HOME -> ASSETS -> Paper buy/sell confirmation, service fill and balances. Loopback only. */
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w320dp-h844dp-night-xxhdpi", fontScale = 1.3f)
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPaperAccount320EvidenceTest : ProductionPaperAccountScenario("paper-320-font130", "w=320;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.3")

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-night-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPaperAccount360DarkEvidenceTest : ProductionPaperAccountScenario("paper-360-dark", "w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

@OptIn(ExperimentalCoroutinesApi::class)
abstract class ProductionPaperAccountScenario(private val label: String, private val qualifier: String) {
    private val dispatcher = StandardTestDispatcher()
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private lateinit var pendingWindow: PendingWindow
    private lateinit var paperVm: click.cowa.linbit.feature.assets.PaperAccountViewModel
    class PendingWindow { var window: android.view.Window? = null }
    @Before fun main() {
        Dispatchers.setMain(dispatcher)
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        System.setProperty("production.evidence.qualifier", qualifier)
    }
    private fun awaitText(text: String, scroll: Boolean = false) {
        try { rule.waitUntil(15_000) { runCatching {
            dispatcher.scheduler.advanceUntilIdle(); rule.waitForIdle()
            val nodes = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes()
            nodes.isNotEmpty() && if (scroll) {
                rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true)[0].performScrollTo()
                true
            } else true
        }.getOrDefault(false) } }
        catch (e: Throwable) { throw AssertionError("Wait=$text enabled=${paperVm.available} loading=${paperVm.loading} failure=${paperVm.failure} account=${paperVm.account} balances=${paperVm.balances} last=${paperVm.lastOrder} requests=${api.dumpRequests()} tree=" + rule.onRoot(useUnmergedTree = true).printToString(), e) }
    }
    private fun awaitBalance(mutations: Int, orderId: String, asset: String, amount: String) {
        try { rule.waitUntil(15_000) {
            dispatcher.scheduler.advanceUntilIdle(); rule.waitForIdle()
            api.mutationRequests.size == mutations && paperVm.lastOrder?.orderId == orderId &&
                paperVm.lastOrder?.clientOrderId == api.mutationRequests.last().body.clone().readUtf8().let { org.json.JSONObject(it).getString("clientOrderId") } &&
                !paperVm.loading && paperVm.available && paperVm.balances?.items?.any { it.asset == asset && it.free == amount } == true
        } } catch (e: Throwable) {
            throw AssertionError("Balance=$asset:$amount writes=${mutations}; actual=${api.mutationRequests.size}; enabled=${paperVm.available}; loading=${paperVm.loading}; failure=${paperVm.failure}; generation=${paperVm.account?.generation}; balances=${paperVm.balances}; order=${paperVm.lastOrder}; requests=${api.dumpRequests()}", e)
        }
        rule.waitForIdle()
        rule.onAllNodesWithText(amount, useUnmergedTree = true)[0].performScrollTo().assertIsDisplayed()
    }
    private fun base() {
        repeat(6) {
            api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"page":1,"pageSize":200,"total":0,"items":[]}""")
            api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"page":1,"pageSize":200,"total":0,"items":[]}""")
            api.route("GET", "/futures/account", "{}")
        }
        repeat(2) {
            api.route("GET", "/market/symbols", "[]"); api.route("GET", "/market/ticker", "[]")
            api.route("GET", "/futures/market/ticker", """{"tickers":[]}"""); api.route("GET", "/market/categories", "[]")
            api.route("GET", "/market/fiat-rates?base=USDT", """{"base":"USDT","rates":{}}""")
        }
        repeat(3) {
            api.route("GET", "/assets/overview", """{"totalValueUsdt":"0"}"""); api.route("GET", "/home/newcomer", "{}")
            api.route("GET", "/notifications/unread-count", """{"unread":0}"""); earnRoutes(api, false)
        }
    }
    @After fun tearDown() { Dispatchers.resetMain() }
    @Test fun appRootPaperMarketBuyReviewAndSellDecimalReview() {
        MockEvidenceApi().use { server -> api = server
            base()
            api.defaultFeatureFlags = """{"features":{"paperTrading":true,"priceAlerts":false}}"""
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            val paperClient = f.paperClient(true)
            f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            repeat(4) { api.route("GET", "/paper/account", ACCOUNT) }
            api.route("GET", "/paper/balances?page=1&pageSize=20", BALANCES)
            api.route("GET", "/paper/balances?page=1&pageSize=20", """{"page":1,"pageSize":20,"total":2,"items":[{"asset":"USDT","free":"8900","locked":"0","total":"8900"},{"asset":"BTC","free":"1.00125","locked":"0","total":"1.00125"}],"account":"paper","mode":"paper","generation":3}""")
            api.route("POST", "/paper/orders", """{"orderId":"po_mock","clientOrderId":"MOCK_CLIENT_ID","accountId":"pa_mock","mode":"paper","generation":3,"symbol":"BTCUSDT","side":"buy","type":"market","price":"80000.01","quantity":"0.00125","quoteOrderQty":"100.010","filledQuantity":"0.00125","status":"filled","priceSource":"INDEX","referencePrice":"80000.01","marketObservedAt":"2026-01-01T00:00:00Z","fee":"0","feeAsset":"USDT","createdAt":"2026-01-01T00:00:00Z","executedAt":"2026-01-01T00:00:00Z"}""")
            api.route("POST", "/paper/orders", """{"orderId":"po_sell_mock","clientOrderId":"MOCK_CLIENT_ID","accountId":"pa_mock","mode":"paper","generation":3,"symbol":"BTCUSDT","side":"sell","type":"market","price":"80000.02","quantity":"0.25001","quoteOrderQty":null,"filledQuantity":"0.25001","status":"filled","priceSource":"INDEX","referencePrice":"80000.02","marketObservedAt":"2026-01-01T00:00:00Z","fee":"0","feeAsset":"USDT","createdAt":"2026-01-01T00:00:00Z","executedAt":"2026-01-01T00:00:00Z"}""")
            api.route("GET", "/paper/balances?page=1&pageSize=20", """{"page":1,"pageSize":20,"total":2,"items":[{"asset":"USDT","free":"20900.01","locked":"0","total":"20900.01"},{"asset":"BTC","free":"1.00125","locked":"0","total":"1.00125"}],"account":"paper","mode":"paper","generation":3}""")
            paperVm = f.paper(paperClient)
            val rootVm = f.root()
            runBlocking { rootVm.setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            pendingWindow = PendingWindow()
            rule.setContent {
                androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket()) {
                    click.cowa.linbit.app.AppRoot(rootVm, f.badge(), click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi(), paperVm), showSplash = false, onDialogWindow = { pendingWindow.window = it })
                }
            }
            awaitText("首页"); rule.onNodeWithText("资产", useUnmergedTree = true).performClick(); awaitText("完成充值，开启您的 Linbit 之旅!")
            rule.onNodeWithText("模拟", useUnmergedTree = false).performScrollTo().performClick(); awaitText("pa_mock")
            rule.onNodeWithTag("paper-order-amount").performScrollTo().performTextInput("100.010")
            rule.onNodeWithTag("paper-order-review", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("确认虚拟买入")
            rule.onNodeWithText("100.010 USDT", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("新鲜外部指数（INDEX）", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("这是模拟交易，不使用、划扣或变动真实资金。", useUnmergedTree = true).assertIsDisplayed()
            harness.app("approot-$label-buy-review", if (label.startsWith("paper-320")) 960 else 1080, 2364)
            harness.dialogWindow("approot-$label-buy-confirm", pendingWindow.window)
            Assert.assertEquals(0, api.mutationRequests.size)
            api.assertNoWrites()
            Assert.assertTrue(api.requests.all { !it.contains("/spot/order") && !it.contains("/order/accept") })
            rule.onNodeWithTag("paper-order-confirm", useUnmergedTree = true).performClick()
            awaitBalance(1, "po_mock", "USDT", "8900")
            Assert.assertEquals(1, api.mutationRequests.size)
            val sent = api.lastMutationBody()
            Assert.assertTrue(sent.contains("\"quoteOrderQty\":\"100.010\"") && sent.contains("\"side\":\"buy\""))
            Assert.assertFalse(org.json.JSONObject(sent).has("price"))
            Assert.assertTrue(api.requests.indexOfLast { it.startsWith("GET /paper/balances") } > api.requests.indexOfFirst { it.startsWith("POST /paper/orders") })
            rule.onNodeWithText("最近笔虚拟买入由服务端确认成交：0.00125 BTC", useUnmergedTree = true).performScrollTo().assertIsDisplayed()
            harness.app("approot-$label-buy-success")
            rule.onNodeWithText("市价卖出", useUnmergedTree = true).performScrollTo().performClick()
            rule.onNodeWithTag("paper-order-amount").performScrollTo().performTextReplacement("0.25001")
            rule.onNodeWithTag("paper-order-review", useUnmergedTree = true).performClick()
            awaitText("确认虚拟卖出"); rule.onNodeWithText("0.25001 BTC", useUnmergedTree = true).assertIsDisplayed()
            harness.dialogWindow("approot-$label-sell-confirm", pendingWindow.window)
            rule.onNodeWithTag("paper-order-confirm", useUnmergedTree = true).performClick()
            awaitBalance(2, "po_sell_mock", "BTC", "1.00125")
            Assert.assertEquals("po_sell_mock", paperVm.lastOrder?.orderId)
            val sellBody = org.json.JSONObject(api.lastMutationBody())
            Assert.assertEquals("sell", sellBody.getString("side"))
            Assert.assertEquals("0.25001", sellBody.getString("quantity"))
            Assert.assertFalse(sellBody.has("quoteOrderQty") || sellBody.has("price"))
            Assert.assertNotEquals(org.json.JSONObject(sent).getString("clientOrderId"), sellBody.getString("clientOrderId"))
            awaitText("最近笔虚拟卖出由服务端确认成交：0.25001 BTC")
            rule.onNodeWithText("最近笔虚拟卖出由服务端确认成交：0.25001 BTC", useUnmergedTree = true).performScrollTo().assertIsDisplayed()
            harness.app("approot-$label-sell-success")
            Assert.assertTrue(api.requests.all { !it.contains("/spot/order") && !it.contains("/order/accept") && !it.contains("/price-alerts") && !it.contains("/paper/account/reset") })
            awaitBalance(2, "po_sell_mock", "BTC", "1.00125")
            Assert.assertEquals(2, api.mutationRequests.size)
            Assert.assertEquals("100.010", org.json.JSONObject(sent).getString("quoteOrderQty"))
            Assert.assertEquals(paperVm.lastOrder?.clientOrderId, org.json.JSONObject(api.lastMutationBody()).getString("clientOrderId"))
            api.assertOnlyDeclaredWrites()
        }
    }

    companion object {
        const val ACCOUNT = """{"accountId":"pa_mock","mode":"paper","market":"spot","generation":3,"quoteAsset":"USDT","initialBalance":"10000.00000000","resetLimitPerUtcDay":1,"resetsUsedToday":0,"nextResetAt":null,"orderTypes":["market"],"priceSource":"INDEX","feeModel":"none","slippageModel":"none","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
        const val BALANCES = """{"page":1,"pageSize":20,"total":2,"items":[{"asset":"USDT","free":"10000","locked":"0","total":"10000"},{"asset":"BTC","free":"2","locked":"0","total":"2"}],"account":"paper","mode":"paper","generation":3}"""
    }
}
