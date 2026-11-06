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

/** Real HOME -> ASSETS -> Paper entry and production reset review. Only its adapter is loopback. */
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
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private lateinit var pendingWindow: PendingWindow
    class PendingWindow { var window: android.view.Window? = null }
    @Before fun main() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        System.setProperty("production.evidence.qualifier", qualifier)
    }
    @After fun reset() { Dispatchers.resetMain() }
    private fun awaitText(text: String) {
        try { rule.waitUntil(15_000) { runCatching { rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
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
    @Test fun appRootPaperBalancesResetFailureAndAccessibility() {
        MockEvidenceApi().use { server -> api = server
            base()
            api.route("GET", "/paper/account", """{"accountId":"pa_mock","mode":"paper","market":"spot","generation":3,"quoteAsset":"USDT","initialBalance":"10000.00000000","resetLimitPerUtcDay":1,"resetsUsedToday":0,"nextResetAt":null,"orderTypes":["market"],"priceSource":"INDEX","feeModel":"none","slippageModel":"none","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}""")
            api.route("GET", "/paper/balances?page=1&pageSize=20", """{"page":1,"pageSize":20,"total":1,"items":[{"asset":"USDT","free":"10000","locked":"0","total":"10000","valuationUsd":"10000","priceUsd":"1"}],"account":"paper","mode":"paper","generation":3}""")
            api.route("GET", "/paper/account", """{"accountId":"pa_mock","mode":"paper","market":"spot","generation":3,"quoteAsset":"USDT","initialBalance":"10000.00000000","resetLimitPerUtcDay":1,"resetsUsedToday":0,"nextResetAt":null,"orderTypes":["market"],"priceSource":"INDEX","feeModel":"none","slippageModel":"none","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}""")
            api.route("GET", "/paper/balances?page=1&pageSize=20", """{"page":1,"pageSize":20,"total":1,"items":[{"asset":"USDT","free":"10000","locked":"0","total":"10000","valuationUsd":"10000","priceUsd":"1"}],"account":"paper","mode":"paper","generation":3}""")
            api.explicitFailure("POST", "/paper/account/reset", "测试：虚拟重置未执行，稍后重试")
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            // A second Paper init verifies AppRoot separately installed test loopback; no real-money paper call exists.
            val closed = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.IO)).paperClient(false)
            Assert.assertFalse(closed.enabled)
            f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            pendingWindow = PendingWindow()
            val rootVm = f.root()
            runBlocking { rootVm.setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            val route = click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi(), f.paper())
            rule.setContent {
                androidx.compose.runtime.CompositionLocalProvider(
                    click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket(),
                ) { click.cowa.linbit.app.AppRoot(rootVm, f.badge(), route, showSplash = false, onDialogWindow = { pendingWindow.window = it }) }
            }
            awaitText("首页"); rule.onNodeWithText("资产", useUnmergedTree = true).performClick(); awaitText("完成充值，开启您的 Linbit 之旅!")
            rule.onNodeWithText("模拟", useUnmergedTree = false).performScrollTo().performClick(); awaitText("pa_mock")
            assert(api.requests.count { it.startsWith("GET /paper/account") } == 1)
            rule.onNodeWithText("重置虚拟账户", useUnmergedTree = true).performScrollTo().performClick(); awaitText("确认语句：RESET_PAPER_ACCOUNT")
            rule.onNodeWithText("确认重置模拟账户", useUnmergedTree = true).performClick()
            // The actual VM's in-flight gate has its own deterministic unit test. Here each real click may submit only once.
            awaitText("操作未完成：测试：虚拟重置未执行，稍后重试")
            Assert.assertEquals(1, api.mutationRequests.size)
            harness.app("approot-$label", rule.activity.resources.displayMetrics.let { if (label.startsWith("paper-320")) 960 else 1080 }, 2364)
            val preview = harness.dialogWindow("approot-$label-reset-failure", pendingWindow.window)
            assert(preview.window)
            api.assertOnlyExpectedFailures()
            val ids = api.mutationRequests.map { it.getHeader("Idempotency-Key") }
            Assert.assertEquals(1, ids.size); Assert.assertNotNull(ids.single())
            Assert.assertTrue(api.lastMutationBody().contains("RESET_PAPER_ACCOUNT"))
            Assert.assertTrue(api.requests.all { !it.contains("/spot/order") && !it.contains("/orders") })
            println("PAPER_DIALOG=$preview")
        }
    }
}
