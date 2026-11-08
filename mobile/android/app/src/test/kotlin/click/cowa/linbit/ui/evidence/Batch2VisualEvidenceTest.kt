package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.feature.assets.*
import click.cowa.linbit.feature.profile.*
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** Native rendered production components with explicit loopback data. NOT real account or device screenshots. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class Batch2VisualEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        Dispatchers.setMain(UnconfinedTestDispatcher())
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        fixtures.tokens.accessToken = "INVALID_BATCH2_FIXTURE"
    }
    @After fun done() { if (::api.isInitialized) { api.assertNoWrites(); api.close() }; Dispatchers.resetMain() }
    private fun render(content: @Composable () -> Unit) { rule.activity.actionBar?.hide(); rule.setContent { LinbitTheme(false) { Box(Modifier.fillMaxSize().background(LB.p.bg)) { content() } } } }
    private fun capture(name: String) {
        rule.waitForIdle()
        rule.runOnIdle {
            val view = rule.activity.window.decorView
            val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            val out = File(System.getProperty("production.evidence.dir") ?: "build/reports/batch2").apply { mkdirs() }
            File(out, "mock-$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            File(out, "mock-$name.txt").writeText("Robolectric native-rendered production component; loopback fixtures; not device screenshot; no financial writes.\n" + api.dumpRequests())
            bitmap.recycle()
        }
    }
    @Test fun profileLanding() {
        val vm = fixtures.profile()
        render { ProfileLandingScreen({}, {}, {}, {}, {}, vm) }
        rule.onNodeWithText("推荐").assertExists()
        capture("profile")
    }
    @Test fun preferences() {
        api.route("GET", "/account/preferences", """{"quoteCurrency":"USD","defaultQuoteCurrency":"USD","supportedQuoteCurrencies":["USD","CNY"]}""")
        val vm = AccountPreferencesViewModel(fixtures.retrofit, fixtures.tokens)
        api.route("GET", "/assets/deposit-account", """{"account":"funding","source":"platform","platformDefault":"funding","supported":["funding","spot"]}""")
        val dvm = DepositTargetViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)
        val profile = fixtures.profile()
        render { androidx.compose.runtime.CompositionLocalProvider(LocalAccountPreferencesFactory provides { vm }, LocalDepositTargetFactory provides { dvm }) { UserCenterScreen(ThemeMode.LIGHT, {}, {}, {}, {}, {}, profile) } }
        rule.onNodeWithText("偏好设置").performClick()
        rule.waitUntil(5000) { vm.preferences != null }
        rule.onNodeWithText("颜色偏好").assertExists()
        capture("preferences")
    }
    @Test fun fundingAssets() {
        api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"page":1,"pageSize":200,"items":[{"asset":"BTC","free":"0","locked":"0","total":"0"},{"asset":"USDT","free":"0","locked":"0","total":"0"}],"total":2}""")
        api.route("GET", "/futures/account", "{}")
        val vm = fixtures.assets()
        render { AssetsScreen(60.dp, {}, click.cowa.linbit.data.assets.AssetsNav(), vm = vm) }
        rule.waitUntil(5000) { !vm.state.value.loading }
        rule.onNodeWithText("资金").performClick()
        rule.waitUntil(5000) { rule.onAllNodesWithText("快捷买币").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("快捷买币").assertExists()
        capture("funding-assets")
    }
    @Test fun dailyPnl() {
        api.route("GET", "/futures/pnl/today", """{"asset":"USDT","since":1794096000000,"insuranceCover":"0","pnl":"0","realizedPnl":"0","funding":"0","fee":"0","liquidationFee":"0"}""")
        val vm = PnlAnalysisViewModel(fixtures.retrofit, fixtures.tokens)
        render { PnlAnalysisScreen({}, vm, PnlHistoryViewModel(fixtures.retrofit, fixtures.tokens)) }
        rule.waitUntil(5000) { vm.data != null }
        rule.onNodeWithText("已实现盈亏").assertExists()
        capture("pnl")
    }
    @Test fun fiatUnavailableWithoutFabricatedChannel() {
        api.route("GET", "/fiat/buy", """{"configured":false,"available":false,"reason":"unconfigured"}""")
        val fvm = FiatBuyViewModel(fixtures.retrofit)
        render { QuickBuyScreen({}, fvm) }
        rule.waitUntil(5000) { fvm.status != null }
        rule.onNodeWithText("暂无可用的支付渠道").assertExists()
        capture("quickbuy-unavailable")
    }
}
