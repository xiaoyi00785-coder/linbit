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
import androidx.compose.ui.graphics.asAndroidBitmap
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
    /** 主窗口 + 所有弹窗窗口（Dialog 是独立窗口，decorView 抓不到）按底部对齐叠加。 */
    private fun captureWithDialogs(name: String) {
        rule.waitForIdle()
        lateinit var base: Bitmap
        rule.runOnIdle {
            val main = rule.activity.window.decorView
            base = Bitmap.createBitmap(main.width, main.height, Bitmap.Config.ARGB_8888)
            val c = Canvas(base); main.draw(c)
            org.robolectric.shadows.ShadowDialog.getShownDialogs().filter { it.isShowing }.forEach { d ->
                val v = d.window!!.decorView
                c.drawColor(android.graphics.Color.argb(0x80, 0, 0, 0))
                c.save(); c.translate((base.width - v.width) / 2f, (base.height - v.height).toFloat()); v.draw(c); c.restore()
            }
        }
        val out = File(System.getProperty("production.evidence.dir") ?: "build/reports/batch4").apply { mkdirs() }
        File(out, "mock-$name.png").outputStream().use { base.compress(Bitmap.CompressFormat.PNG, 100, it) }
        File(out, "mock-$name.txt").writeText("Robolectric native-rendered production component (main + dialog windows composited); loopback fixtures; not device screenshot; no financial writes.\n" + api.dumpRequests())
    }
    /** 游客点「我的」：参考 040_profile_login_gate = 创建帐户页 */
    @Test fun profileGuestGate() {
        val vm = fixtures.auth()
        render { click.cowa.linbit.feature.auth.RegisterScreen(vm, {}, {}, {}, {}) }
        capture("b4-profile-guest")
    }

    /** 游客点登录页右上角客服：未登录不开放在线客服，如实提示先登录提交工单（LbDialog 底部弹层）。 */
    @Test fun guestSupport() {
        val vm = fixtures.auth()
        render { click.cowa.linbit.feature.auth.AuthFlow(false, {}, vm, gs = null) }
        rule.onNodeWithTag("auth.support").performClick(); rule.waitForIdle()
        rule.onNodeWithText("在线客服").assertExists()
        captureWithDialogs("b4-guest-support")
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
