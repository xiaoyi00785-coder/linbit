package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*

/** Actual AppRoot navigation, not a direct Host or an overwritten feature callback. */
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionAppRootEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    @Before fun main() { Dispatchers.setMain(UnconfinedTestDispatcher()); Assume.assumeTrue(System.getProperty("production.evidence") == "1") }
    @After fun reset() { Dispatchers.resetMain() }
    private fun awaitText(text: String) {
        try { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
        catch (e: Throwable) { throw AssertionError("Wait=$text requests=${api.dumpRequests()} tree=" + rule.onRoot(useUnmergedTree = true).printToString(), e) }
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
    private fun render() {
        base()
        val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
        val route = click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi())
        rule.setContent {
            androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket()) {
                click.cowa.linbit.app.AppRoot(f.root(), f.badge(), route, showSplash = false)
            }
        }
        awaitText("首页"); rule.onNodeWithText("资产", useUnmergedTree = true).performClick(); awaitText("完成充值，开启您的 Linbit 之旅!")
    }
    @Test fun homeAssetsTabEarnClickPath() {
        MockEvidenceApi().use { api -> this.api = api
            render(); rule.onNodeWithText("理财", useUnmergedTree = true).performClick(); awaitText("暂无理财产品")
            harness.app("approot-assets-earn", 1170, 2364); api.assertNoWrites()
            Assert.assertTrue(api.requests.any { it.startsWith("GET /earn/products") })
        }
    }
    @Test fun homeAssetsTabStrategyClickPathAndStableEmpty() {
        MockEvidenceApi().use { api -> this.api = api
            strategyRoutes(api, false); render(); rule.onNodeWithText("策略", useUnmergedTree = true).performClick(); awaitText("暂无公开策略")
            harness.app("approot-assets-strategy-empty", 1170, 2364); api.assertNoWrites()
            Assert.assertTrue(api.requests.contains("GET /strategy/grid/square?page=1&pageSize=20"))
        }
    }
    @Test fun homeAssetsTabTradFiClickPath() {
        MockEvidenceApi().use { api -> this.api = api
            api.route("GET", "/tradfi/assets", """{"code":"EVIDENCE_UNAVAILABLE","message":"测试：TradFi 不可用"}""", 503)
            render(); rule.onNodeWithText("TradFi", useUnmergedTree = true).performClick(); awaitText("测试：TradFi 不可用")
            harness.app("approot-assets-tradfi-error", 1170, 2364); api.assertNoWrites()
        }
    }
}
