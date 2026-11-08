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
import click.cowa.linbit.feature.more.*
import click.cowa.linbit.feature.assets.*
import click.cowa.linbit.feature.markets.*
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
class Batch4VisualEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        Dispatchers.setMain(UnconfinedTestDispatcher())
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        fixtures.tokens.accessToken = "INVALID_BATCH4_FIXTURE"
    }
    @After fun done() { if (::api.isInitialized) { api.assertNoWrites(); api.close() }; Dispatchers.resetMain() }
    private fun render(content: @Composable () -> Unit) { rule.activity.actionBar?.hide(); rule.setContent { LinbitTheme(false) { Box(Modifier.fillMaxSize().background(LB.p.bg)) { content() } } } }
    private fun capture(name: String) {
        rule.waitForIdle()
        rule.runOnIdle {
            val view = rule.activity.window.decorView
            val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            val out = File(System.getProperty("production.evidence.dir") ?: "build/reports/batch4").apply { mkdirs() }
            File(out, "mock-$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
            File(out, "mock-$name.txt").writeText("Robolectric native-rendered production component; loopback fixtures; not device screenshot; no financial writes.\n" + api.dumpRequests())
            bitmap.recycle()
        }
    }
    @Test fun profileGuideOverlay() {
        rule.activity.getSharedPreferences("profile_guide", android.content.Context.MODE_PRIVATE).edit().clear().commit()
        val vm = fixtures.profile()
        render { ProfileLandingScreen({}, {}, {}, {}, {}, vm) }
        rule.onNodeWithText("偏好设置已移至用户中心。").assertExists()
        capture("b4-profile-guide")
    }
    @Test fun fundingWithHoldings() {
        api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"items":[{"asset":"BTC","free":"0.0123","locked":"0.001","total":"0.0133"},{"asset":"ETH","free":"0.5","locked":"0","total":"0.5"}],"page":1,"pageSize":200,"total":2}""")
        api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"page":1,"pageSize":200,"items":[{"asset":"USDT","free":"1250.5","locked":"20","total":"1270.5"},{"asset":"BTC","free":"0.002","locked":"0","total":"0.002"}],"total":2}""")
        api.route("GET", "/futures/account", """{"asset":"USDT","walletBalance":"300","marginBalance":"312.3","availableBalance":"280","crossUnrealizedPnl":"12.3"}""")
        val vm = fixtures.assets()
        render { AssetsScreen(60.dp, {}, click.cowa.linbit.data.assets.AssetsNav(), vm = vm) }
        rule.waitUntil(5000) { !vm.state.value.loading }
        capture("b4-assets-overview-holdings")
        rule.onNodeWithText("资金").performClick(); rule.waitForIdle()
        capture("b4-assets-funding-holdings")
    }
    @Test fun assetsZeroTabs() {
        api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"items":[],"page":1,"pageSize":200,"total":0}""")
        api.route("GET", "/futures/account", """{"asset":"USDT","walletBalance":"0","marginBalance":"0","availableBalance":"0","crossUnrealizedPnl":"0"}""")
        api.route("GET", "/wallet/networks", """{"networks":[{"asset":"BTC","network":"BTC"},{"asset":"ETH","network":"ETH"},{"asset":"TRX","network":"TRON"},{"asset":"USDT","network":"TRON"}]}""")
        val vm = fixtures.assets()
        render { AssetsScreen(60.dp, {}, click.cowa.linbit.data.assets.AssetsNav(), vm = vm) }
        rule.waitUntil(5000) { !vm.state.value.loading }
        capture("b4-assets-zero-overview")
        listOf("资金" to "funding", "合约" to "futures", "现货" to "spot").forEach { (t, n) ->
            rule.onAllNodesWithText(t).onFirst().performClick(); rule.waitForIdle(); capture("b4-assets-zero-$n")
        }
    }
}
