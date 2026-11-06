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
class ProductionPriceAlert320EvidenceTest : ProductionPriceAlertMutationScenario("alert-320-font130", "w=320;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.3")
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-night-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ProductionPriceAlert360DarkEvidenceTest : ProductionPriceAlertMutationScenario("alert-360-dark", "w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")

@OptIn(ExperimentalCoroutinesApi::class)
abstract class ProductionPriceAlertScenario(val label: String, val qualifier: String) {
    protected val dispatcher = StandardTestDispatcher()
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    internal val harness by lazy { ProductionEvidenceHarness(rule) }
    internal lateinit var api: MockEvidenceApi
    internal lateinit var vm: click.cowa.linbit.feature.alerts.PriceAlertViewModel
    internal var window: android.view.Window? = null
    @Before fun main() { Dispatchers.setMain(dispatcher); Assume.assumeTrue(System.getProperty("production.evidence") == "1"); System.setProperty("production.evidence.qualifier", qualifier) }
    @After fun reset() { Dispatchers.resetMain() }
    protected fun awaitText(text: String, scroll: Boolean = false) {
        try {
            rule.waitUntil(12_000) {
                dispatcher.scheduler.advanceUntilIdle(); rule.waitForIdle()
                val nodes = rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes()
                if (nodes.isNotEmpty() && scroll) rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true)[0].performScrollTo()
                nodes.isNotEmpty()
            }
        } catch (e: Throwable) { throw AssertionError("await=$text gate=${vm.featureState.value} page=${vm.page}/${vm.total} loading=${vm.loading} failure=${vm.failure} items=${vm.items.size} editor=${vm.editor} mutationError=${vm.mutationError} requests=${api.dumpRequests()}", e) }
    }
    protected fun base() {
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
    protected fun alert(id: String, threshold: String = "80000.00", revision: Int = 1) = """{"alertId":"$id","clientAlertId":"ca_$id","market":"spot","symbol":"BTCUSDT","condition":"gte","threshold":"$threshold","priceSource":"INDEX","triggerMode":"once","channels":["in_app"],"enabled":true,"status":"active","revision":$revision,"limit":{"activePerUser":50},"lastObservedPrice":"80200.01","lastEvaluatedAt":"2026-01-01T00:00:00Z","triggeredPrice":null,"triggeredAt":null,"notificationId":null,"deliveryStatus":"none","cooldownUntil":null,"note":"mock read only","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}"""
    private fun body(ids: List<String>, page: Int) = """{"page":$page,"pageSize":2,"total":3,"items":[${ids.joinToString { alert(it) }}]}"""
    @Test fun actualMoreListPaginationDetailAndZeroWrites() {
        MockEvidenceApi().use { server -> api = server
            base(); api.defaultFeatureFlags = """{"features":{"paperTrading":false,"priceAlerts":true}}"""
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            val rootVm = f.root()
            runBlocking { rootVm.setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            val gate = click.cowa.linbit.di.AppModule.priceAlertFeatureGate(f.retrofit)
            var now = 1_000L
            vm = click.cowa.linbit.feature.alerts.PriceAlertViewModel(click.cowa.linbit.di.AppModule.priceAlertClient(NetworkFactoryPaper(), gate), f.tokens).also { it.clock = { now } }
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
            awaitText("价格预警"); awaitText("BTCUSDT", scroll = true)
            harness.app("approot-$label-first")
            rule.onNodeWithTag("alert-next", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("7 秒后可重试", scroll = true)
            Assert.assertEquals(7, vm.retryAfter); Assert.assertEquals(2, vm.items.size)
            rule.onNodeWithTag("alert-retry", useUnmergedTree = true).assertIsNotEnabled()
            now += 7_000; vm.updateRetryRemaining()
            rule.onNodeWithTag("alert-retry", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("已到末页", scroll = true); awaitText("3/3", scroll = true)
            harness.app("approot-$label-last")
            rule.onNodeWithTag("alert-detail-2", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("价格预警详情"); awaitText("mock_alert_3")
            rule.waitUntil(5_000) { window != null }
            harness.dialogWindow("approot-$label-detail", window)
            Assert.assertEquals(3, vm.items.size); Assert.assertEquals(50, vm.items[0].limit.activePerUser)
            Assert.assertTrue(vm.featureState.value.enabled)
            Assert.assertTrue(api.requests.all { !it.startsWith("POST") && !it.startsWith("PATCH") && !it.startsWith("DELETE") })
            api.assertNoWrites()
        }
    }
    protected fun NetworkFactoryPaper() = click.cowa.linbit.core.network.NetworkFactory.paperApi(okhttp3.OkHttpClient(), api.baseUrl)
}

@OptIn(ExperimentalCoroutinesApi::class)
abstract class ProductionPriceAlertMutationScenario(label: String, qualifier: String) : ProductionPriceAlertScenario(label, qualifier) {
    @Test fun appRootRevisionConflictRefreshesSameIdKeepingUserEdit() {
        MockEvidenceApi().use { server -> api = server
            base(); api.defaultFeatureFlags = """{"features":{"priceAlerts":true}}"""
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            runBlocking { f.root().setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            vm = click.cowa.linbit.feature.alerts.PriceAlertViewModel(click.cowa.linbit.di.AppModule.priceAlertClient(NetworkFactoryPaper(), click.cowa.linbit.di.AppModule.priceAlertFeatureGate(f.retrofit)), f.tokens)
            api.route("GET", "/price-alerts?page=1&pageSize=2", """{"page":1,"pageSize":2,"total":1,"items":[${alert("same_mock")}]}""")
            api.route("GET", "/price-alerts?page=1&pageSize=2", """{"page":1,"pageSize":2,"total":1,"items":[${alert("same_mock")}]}""")
            api.route("PATCH", "/price-alerts/same_mock", """{"code":"REVISION_CONFLICT","message":"mock revision"}""", 409)
            api.route("GET", "/price-alerts?page=1&pageSize=50", """{"page":1,"pageSize":50,"total":1,"items":[${alert("same_mock", revision = 5, threshold = "77777.77")}]}""")
            api.route("PATCH", "/price-alerts/same_mock", alert("same_mock", revision = 6, threshold = "81200.55"))
            rule.setContent {
                androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket()) {
                    click.cowa.linbit.app.AppRoot(f.root(), f.badge(), click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi(), priceAlerts = vm), showSplash = false, onDialogWindow = { window = it })
                }
            }
            awaitText("首页"); rule.onNodeWithText("更多", useUnmergedTree = true).performClick(); awaitText("全部功能")
            rule.onNodeWithText("价格预警", useUnmergedTree = true).performClick(); awaitText("BTCUSDT")
            rule.onNodeWithTag("alert-edit-0", useUnmergedTree = true).performScrollTo().performClick(); awaitText("编辑价格预警")
            rule.onNodeWithTag("alert-field-threshold", useUnmergedTree = true).performScrollTo().performTextReplacement("81200.55")
            rule.onNodeWithTag("alert-field-note", useUnmergedTree = true).performScrollTo().performTextReplacement("用户未保存备注")
            rule.onNodeWithTag("alert-save", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("已刷新同一预警至 revision 5", scroll = true)
            Assert.assertEquals("81200.55", vm.editor?.threshold); Assert.assertEquals("用户未保存备注", vm.editor?.note); Assert.assertEquals(5, vm.editor?.revision)
            rule.onNodeWithTag("alert-save", useUnmergedTree = true).performScrollTo().performClick(); awaitText("已保存价格预警", scroll = true)
            val edited = api.mutationRequests.filter { it.method == "PATCH" }; Assert.assertEquals(2, edited.size)
            Assert.assertEquals(5, org.json.JSONObject(edited.last().body.clone().readUtf8()).getInt("revision"))
            Assert.assertEquals("用户未保存备注", org.json.JSONObject(edited.last().body.clone().readUtf8()).getString("note"))
        }
    }
    @Test fun actualDeletion429LocksDialogUntilLiteralRetryAfterExpiry() {
        MockEvidenceApi().use { server -> api = server
            base(); api.defaultFeatureFlags = """{"features":{"priceAlerts":true}}"""
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            runBlocking { f.root().setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            var now = 1_000L
            vm = click.cowa.linbit.feature.alerts.PriceAlertViewModel(click.cowa.linbit.di.AppModule.priceAlertClient(NetworkFactoryPaper(), click.cowa.linbit.di.AppModule.priceAlertFeatureGate(f.retrofit)), f.tokens).also { it.clock = { now } }
            api.route("GET", "/price-alerts?page=1&pageSize=2", """{"page":1,"pageSize":2,"total":1,"items":[${alert("rate_mock")}]}""")
            api.route("GET", "/price-alerts?page=1&pageSize=2", """{"page":1,"pageSize":2,"total":1,"items":[${alert("rate_mock")}]}""")
            api.route("DELETE", "/price-alerts/rate_mock?revision=1", """{"code":"RATE_LIMITED","message":"mock retry","retryAfterSec":7}""", 429, retryAfter = 7)
            rule.setContent {
                androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket()) {
                    click.cowa.linbit.app.AppRoot(f.root(), f.badge(), click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi(), priceAlerts = vm), showSplash = false, onDialogWindow = { window = it })
                }
            }
            awaitText("首页"); rule.onNodeWithText("更多", useUnmergedTree = true).performClick(); awaitText("全部功能")
            rule.onNodeWithText("价格预警", useUnmergedTree = true).performClick(); awaitText("BTCUSDT")
            rule.onNodeWithTag("alert-delete-0", useUnmergedTree = true).performScrollTo().performClick(); awaitText("独立确认删除")
            rule.onNodeWithTag("alert-delete-confirm", useUnmergedTree = true).performClick(); awaitText("Retry-After", scroll = true)
            rule.onNodeWithTag("alert-delete-confirm", useUnmergedTree = true).assertIsNotEnabled()
            Assert.assertEquals(7, vm.mutationError?.retryAfter); Assert.assertEquals(1, api.mutationRequests.count { it.method == "DELETE" })
            rule.onNodeWithTag("alert-delete-confirm", useUnmergedTree = true).performScrollTo()
            rule.waitUntil(5_000) { window != null }; harness.dialogWindow("approot-$label-delete-rate-limited", window)
            now += 7_000; vm.updateRetryRemaining()
            api.route("DELETE", "/price-alerts/rate_mock?revision=1", "", 204)
            rule.onNodeWithTag("alert-delete-confirm", useUnmergedTree = true).performScrollTo().assertIsEnabled().performClick()
            awaitText("已删除价格预警", scroll = true)
            Assert.assertEquals(2, api.mutationRequests.count { it.method == "DELETE" }); Assert.assertTrue(vm.featureState.value.enabled)
        }
    }
    @Test fun appRootCreateReviewEditAndIndependentDeleteDialog() {
        MockEvidenceApi().use { server -> api = server
            base(); api.defaultFeatureFlags = """{"features":{"paperTrading":false,"priceAlerts":true}}"""
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); f.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
            runBlocking { f.root().setTheme(if (label.endsWith("dark")) click.cowa.linbit.core.storage.ThemeMode.DARK else click.cowa.linbit.core.storage.ThemeMode.LIGHT) }
            val gate = click.cowa.linbit.di.AppModule.priceAlertFeatureGate(f.retrofit)
            vm = click.cowa.linbit.feature.alerts.PriceAlertViewModel(click.cowa.linbit.di.AppModule.priceAlertClient(NetworkFactoryPaper(), gate), f.tokens)
            api.route("GET", "/price-alerts?page=1&pageSize=2", """{"page":1,"pageSize":2,"total":1,"items":[${alert("live_mock")}]}""")
            api.route("POST", "/price-alerts", alert("new_mock"), 201)
            api.route("PATCH", "/price-alerts/new_mock", alert("new_mock", threshold = "90000.00", revision = 2))
            api.route("DELETE", "/price-alerts/new_mock?revision=2", "", 204)
            rule.setContent {
                androidx.compose.runtime.CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket()) {
                    click.cowa.linbit.app.AppRoot(f.root(), f.badge(), click.cowa.linbit.app.AssetRouteViewModels(f.assets(), f.spot(), f.tradFi(), priceAlerts = vm), showSplash = false, onDialogWindow = { window = it })
                }
            }
            awaitText("首页"); rule.onNodeWithText("更多", useUnmergedTree = true).performClick(); awaitText("全部功能")
            rule.onNodeWithText("价格预警", useUnmergedTree = true).performClick(); awaitText("BTCUSDT")
            assert(vm.items.size == 1)
            rule.onNodeWithTag("alert-create", useUnmergedTree = true).performScrollTo().performClick()
            awaitText("创建价格预警"); rule.waitUntil(5_000) { window != null }; harness.dialogWindow("approot-$label-create", window)
            rule.onNodeWithTag("alert-cancel", useUnmergedTree = true).performClick()
            api.assertNoWrites()
            rule.onNodeWithTag("alert-create", useUnmergedTree = true).performScrollTo().performClick()
            rule.onNodeWithTag("alert-field-threshold", useUnmergedTree = true).performScrollTo().performTextInput("80000.00")
            api.mutationRequests.clear()
            rule.onNodeWithTag("alert-save", useUnmergedTree = true).performScrollTo().assertIsEnabled()
            harness.dialogWindow("approot-$label-create-review", window)
            rule.onNodeWithTag("alert-save", useUnmergedTree = true).performScrollTo().performClick(); rule.waitForIdle()
            awaitText("已创建价格预警", scroll = true); Assert.assertTrue(vm.items.any { it.alertId == "new_mock" })
            val create = api.mutationRequests.single(); assert(create.method == "POST")
            val createJson = org.json.JSONObject(create.body.clone().readUtf8())
            assert(createJson.getString("threshold") == "80000.00"); assert(createJson.getString("market") == "spot")
            assert(createJson.getString("priceSource") == "INDEX"); assert(createJson.getString("triggerMode") == "once")
            assert(createJson.getJSONArray("channels").getString(0) == "in_app")
            rule.onNodeWithTag("alert-edit-0", useUnmergedTree = true).performScrollTo().performClick()
            harness.app("approot-$label-before-edit")
            awaitText("编辑价格预警")
            rule.onNodeWithTag("alert-field-threshold", useUnmergedTree = true).performScrollTo().performTextReplacement("90000.00")
            rule.onNodeWithTag("alert-save", useUnmergedTree = true).performScrollTo().performClick(); rule.waitForIdle(); awaitText("已保存价格预警", scroll = true)
            val patch = api.mutationRequests.last(); assert(patch.method == "PATCH")
            assert(org.json.JSONObject(patch.body.clone().readUtf8()).getInt("revision") == 1)
            assert(org.json.JSONObject(patch.body.clone().readUtf8()).getString("threshold") == "90000.00")
            rule.onNodeWithTag("alert-delete-0", useUnmergedTree = true).performScrollTo().performClick(); awaitText("独立确认删除")
            rule.waitUntil(5_000) { window != null }; harness.dialogWindow("approot-$label-delete-confirm", window)
            rule.onNodeWithTag("alert-delete-confirm", useUnmergedTree = true).performClick(); awaitText("已删除价格预警", scroll = true)
            val delete = api.mutationRequests.last(); Assert.assertEquals("DELETE", delete.method); Assert.assertEquals("/price-alerts/new_mock?revision=2", delete.path)
            Assert.assertTrue(vm.items.none { it.alertId == "new_mock" }); Assert.assertEquals(1, vm.total)
            Assert.assertEquals(1, api.mutationRequests.count { it.method == "POST" }); Assert.assertEquals(1, api.mutationRequests.count { it.method == "PATCH" })

        }
    }
}
