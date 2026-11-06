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

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w320dp-h844dp-night-xxhdpi", fontScale = 1.3f)
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPriceAlert320EvidenceTest : ProductionPriceAlertScenario("alert-320-font130", "w=320;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-night-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPriceAlert360DarkEvidenceTest : ProductionPriceAlertScenario("alert-360-dark", "w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

@OptIn(ExperimentalCoroutinesApi::class)
abstract class ProductionPriceAlertScenario(val label: String, val qualifier: String) {
    private val dispatcher = StandardTestDispatcher()
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private lateinit var vm: click.cowa.linbit.feature.alerts.PriceAlertViewModel
    private var window: android.view.Window? = null
    @Before fun main() { Dispatchers.setMain(dispatcher); Assume.assumeTrue(System.getProperty("production.evidence") == "1"); System.setProperty("production.evidence.qualifier", qualifier) }
    @After fun reset() { Dispatchers.resetMain() }
    private fun awaitText(text: String, scroll: Boolean = false) {
        try {
            rule.waitUntil(12_000) {
                dispatcher.scheduler.advanceUntilIdle(); rule.waitForIdle()
                val nodes = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes()
                if (nodes.isNotEmpty() && scroll) rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true)[0].performScrollTo()
                nodes.isNotEmpty()
            }
        } catch (e: Throwable) { throw AssertionError("await=$text gate=${vm.featureState.value} page=${vm.page}/${vm.total} loading=${vm.loading} failure=${vm.failure} items=${vm.items.size} requests=${api.dumpRequests()}", e) }
    }
    private fun base() {
        repeat(6) {
            api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"page":1,"pageSize":200,"total":0,"items":[]}""")
            api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"page":1,"pageSize":200,"total":0,"items":[]}""")
        }
        repeat(2) {
            api.route("GET", "/market/symbols", "[]"); api.route("GET", "/market/ticker", "[]")
            api.route("GET", "/futures/market/ticker", """{"tickers":[]}"""); api.route("GET", "/market/categories", "[]")
            api.route("GET", "/market/fiat-rates?base=USDT", """{"base":"USDT","rates":{}}""")
        }
        repeat(3) { api.route("GET", "/assets/overview", """{"totalValueUsdt":"0"}"""); api.route("GET", "/home/newcomer", "{}"); api.route("GET", "/notifications/unread-count", """{"unread":0}"""); earnRoutes(api, false) }
    }
    private fun alert(id: String, threshold: String = "80000.00") = """{"alertId":"$id","clientAlertId":"ca_$id","market":"spot","symbol":"BTCUSDT","condition":"gte","threshold":"$threshold","priceSource":"INDEX","triggerMode":"once","channels":["in_app"],"enabled":true,"status":"active","revision":1,"limit":{"activePerUser":50},"lastObservedPrice":"80200.01","lastEvaluatedAt":"2026-01-01T00:00:00Z","triggeredPrice":null,"triggeredAt":null,"notificationId":null,"deliveryStatus":"none","cooldownUntil":null,"note":"mock read only","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
    private fun body(ids: List<String>, page: Int) = """{"page":$page,"pageSize":2,"total":3,"items":[${ids.joinToString { alert(it) }}]}"""
    @Test fun actualMoreListPaginationDetailAndZeroWrites() {
        MockEvidenceApi().use { server -> api = server
            base(); api.defaultFeatureFlags = """{"features":{"paperTrading":false,"priceAlerts":true}}"""
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            val rootVm = f.root()
            runBlocking { rootVm.setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            val gate = click.cowa.linbit.di.AppModule.priceAlertFeatureGate(f.retrofit)
            vm = click.cowa.linbit.feature.alerts.PriceAlertViewModel(click.cowa.linbit.di.AppModule.priceAlertClient(NetworkFactoryPaper(), gate), f.tokens)
            api.route("GET", "/price-alerts?page=1&pageSize=2", body(listOf("mock_alert_1", "mock_alert_2"), 1))
            api.route("GET", "/price-alerts?page=2&pageSize=2", """{"code":"RATE_LIMITED","message":"mock retry","requestId":"mock-id","retryAfterSec":7}""", 429, retryAfter = 7)
            api.route("GET", "/price-alerts?page=2&pageSize=2", body(listOf("mock_alert_3"), 2))
            rule.setContent {
                androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket()) {
                    click.cowa.linbit.app.AppRoot(rootVm, f.badge(), click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi(), priceAlerts = vm), showSplash = false, onDialogWindow = { window = it })
                }
            }
            awaitText("首页"); rule.onNodeWithText("更多", useUnmergedTree = true).performClick(); awaitText("全部功能")
            rule.onNodeWithText("价格预警", useUnmergedTree = true).performClick()
            awaitText("价格预警 · 只读"); awaitText("BTCUSDT", scroll = true)
            harness.app("approot-$label-first")
            rule.onNodeWithTag("alert-next", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("7 秒后可重试", scroll = true)
            Assert.assertEquals(7, vm.retryAfter); Assert.assertEquals(2, vm.items.size)
            rule.onNodeWithTag("alert-retry", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("已到末页", scroll = true); awaitText("3/3", scroll = true)
            harness.app("approot-$label-last")
            rule.onNodeWithTag("alert-detail-2", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("价格预警详情 · 只读"); awaitText("mock_alert_3")
            rule.waitUntil(5_000) { window != null }
            harness.dialogWindow("approot-$label-detail", window)
            Assert.assertEquals(3, vm.items.size); Assert.assertEquals(50, vm.items[0].limit.activePerUser)
            Assert.assertTrue(vm.featureState.value.enabled)
            Assert.assertTrue(api.requests.all { !it.startsWith("POST") && !it.startsWith("PATCH") && !it.startsWith("DELETE") })
            api.assertNoWrites()
        }
    }
    private fun NetworkFactoryPaper() = click.cowa.linbit.core.network.NetworkFactory.paperApi(okhttp3.OkHttpClient(), api.baseUrl)
}
