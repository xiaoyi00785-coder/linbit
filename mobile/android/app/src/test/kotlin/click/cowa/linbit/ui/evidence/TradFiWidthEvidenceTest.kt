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

/** TradFi 多屏宽适配：同一组后端快照数据（resources/evidence/tradfi-assets.json，取自线上 /tradfi/assets 公共接口）在不同屏宽下离屏渲染。 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@OptIn(ExperimentalCoroutinesApi::class)
class TradFiWidthEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    private val scope = CoroutineScope(kotlinx.coroutines.SupervisorJob() + Dispatchers.Main + kotlinx.coroutines.CoroutineExceptionHandler { _, _ -> })
    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        Dispatchers.setMain(UnconfinedTestDispatcher())
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, scope)
    }
    @After fun done() { scope.coroutineContext[kotlinx.coroutines.Job]?.cancel(); if (::api.isInitialized) { api.assertNoWrites(); api.close() }; Dispatchers.resetMain() }

    /** 单个 Activity 内切换内容宽度（320/360/411/600/840dp），逐一截图并按宽度裁剪。 */
    @Test @Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w840dp-h900dp-xxhdpi")
    fun widths() {
        val body = javaClass.classLoader!!.getResource("evidence/tradfi-assets.json")!!.readText()
        api.route("GET", "/tradfi/assets", body)
        val vm = fixtures.tradFi()
        val width = androidx.compose.runtime.mutableIntStateOf(320)
        rule.activity.actionBar?.hide()
        rule.setContent { LinbitTheme(false) { Box(Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Color(0xFFDDDDDD))) {
            Box(Modifier.width(width.intValue.dp).fillMaxHeight().background(LB.p.bg)) { click.cowa.linbit.feature.more.TradFiScreen({}, { _, _ -> }, vm) }
        } } }
        rule.waitUntil(5_000) { vm.state.value.data != null || vm.state.value.error != null }
        val out = File(System.getProperty("production.evidence.dir") ?: "build/reports/batch4").apply { mkdirs() }
        for (w in listOf(320, 360, 411, 600, 840)) {
            width.intValue = w
            rule.waitForIdle()
            rule.runOnIdle {
                val view = rule.activity.window.decorView
                val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)
                view.draw(Canvas(bitmap))
                val px = minOf(view.width, (w * view.resources.displayMetrics.density).toInt())
                val crop = Bitmap.createBitmap(bitmap, 0, 0, px, view.height)
                File(out, "tradfi-w$w.png").outputStream().use { crop.compress(Bitmap.CompressFormat.PNG, 100, it) }
                crop.recycle(); bitmap.recycle()
            }
        }
    }
}
