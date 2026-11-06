package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import androidx.compose.ui.unit.dp
import click.cowa.linbit.feature.assets.AssetFeatureHost
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Production host teardown/no-write route proof for the asset feature selector. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class ProductionFeatureEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        api = MockEvidenceApi()
        api.route("GET", "/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"fundPasswordSet":false}""")
    }
    @After fun done() { if (::api.isInitialized) { api.assertNoWrites(); java.io.File(harness.out, "mock-routes-$feature.txt").writeText(api.dumpRequests()); api.close() }; Dispatchers.resetMain() }
    private var feature = "unset"
    private fun wait(t: String, contains: Boolean = false) { try { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(t, substring = contains, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } } catch (e: Throwable) { throw AssertionError("wait $t; routes=${api.requests}; tree=${rule.onRoot(useUnmergedTree = true).printToString()}", e) } }

    @Test fun earnExistingProductEmptyStateAndAssetRoute() {
        feature = "earn"
        api.route("GET", "/earn/products", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/earn/summary", """{"totalValueUsdt":"0","totalInterestUsdt":"0"}""")
        api.route("GET", "/earn/positions", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/earn/orders", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/earn/records", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        renderFeatureHost("earn")
        wait("暂无理财产品"); harness.app("asset-earn-empty", 1170, 2364)
    }

    @Test fun strategyExistingGridEmptyStateAndAssetRoute() {
        feature = "strategy"
        api.route("GET", "/strategy/config", """{"sellBaseOnStop":true}""")
        api.route("GET", "/strategy/grid/square?page=1&pageSize=20", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/strategy/grid", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        renderFeatureHost("strategy")
        wait("暂无公开策略"); harness.app("asset-strategy-empty", 1170, 2364)
    }

    @Test fun tradFiExplicitErrorAndAssetRouteNoTrading() {
        feature = "tradfi"
        api.route("GET", "/tradfi/assets", """{"code":"TEST_TRADFI_UNAVAILABLE","message":"测试：TradFi 不可用"}""", 503)
        renderFeatureHost("tradfi")
        wait("测试：TradFi 不可用"); harness.app("asset-tradfi-error", 1170, 2364)
    }


    @Test fun actualAssetTabClickReachesSharedProductionHost() {
        feature = "click-through"
        api.route("GET", "/earn/products", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/earn/summary", """{"totalValueUsdt":"0","totalInterestUsdt":"0"}""")
        api.route("GET", "/earn/positions", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/earn/orders", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/earn/records", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/futures/account", "{}")
        Dispatchers.setMain(kotlinx.coroutines.test.UnconfinedTestDispatcher())
        val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        val assetVm = f.assets()
        val selected = androidx.compose.runtime.mutableStateOf<String?>(null)
        rule.setContent { LinbitTheme(false) {
            val feature = selected.value
            if (feature == null) click.cowa.linbit.feature.assets.AssetsScreen(0.dp, {}, click.cowa.linbit.data.assets.AssetsNav(), vm = assetVm, onOpenAssetFeature = { selected.value = it })
            else AssetFeatureHost(feature, { selected.value = null }, {}, { _, _ -> }, SpotSubpagesViewModel(f.retrofit, f.tokens, f.toast))
        } }
        wait("完成充值，开启您的 Linbit 之旅!")
        rule.onNodeWithText("理财").performClick()
        Assert.assertEquals("earn", selected.value); wait("暂无理财产品")
        harness.app("asset-click-to-earn-empty", 1170, 2364)
    }
    private fun renderFeatureHost(kind: String) {
        val fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        val vm = SpotSubpagesViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)
        val tradFi = click.cowa.linbit.feature.more.TradFiViewModel(fixtures.retrofit)
        rule.setContent { LinbitTheme(false) {
            AssetFeatureHost(kind, onBack = {}, onLogin = {}, onTrade = { _, _ -> }, spotVm = vm, tradFiVm = tradFi)
        } }
    }
}
