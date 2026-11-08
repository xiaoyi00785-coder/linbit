package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.feature.trade.EventChart
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.*
import java.io.File

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class SupertrendEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    @Test fun switchesToComputedSupertrendWithoutAnyWrites() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        MockEvidenceApi().use { api ->
            try {
                val json = (0 until 160).joinToString(",", "[", "]") { i ->
                    val v = 100 + if ((i/25)%2 == 0) i%25 else 25-i%25
                    """{"time":${1700000000L+i*900},"open":"$v","high":"${v+2}","low":"${v-2}","close":"${v+1}","volume":"10"}"""
                }
                api.route("GET", "/market/klines?symbol=BTCUSDT&interval=15m&limit=160", json)
                val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); val vm = f.event()
                rule.activity.actionBar?.hide()
                rule.setContent { LinbitTheme(false) { Column(Modifier.fillMaxSize().background(LB.p.bg)) { EventChart(vm) } } }
                rule.waitUntil(8000) { rule.onAllNodesWithText("MA-5:", substring=true).fetchSemanticsNodes().any { it.config.toString().contains("--").not() } }
                rule.onNodeWithText("Supertrend").performClick()
                rule.onNodeWithText("ST↑:", substring = true).assertIsDisplayed()
                rule.onNodeWithText("ST↓:", substring = true).assertIsDisplayed()
                rule.waitForIdle()
                System.getProperty("production.evidence.dir")?.let { path -> rule.runOnIdle {
                    val v = rule.activity.window.decorView
                    val bitmap = Bitmap.createBitmap(v.width,v.height,Bitmap.Config.ARGB_8888)
                    v.draw(Canvas(bitmap)); File(path).mkdirs()
                    File(path,"mock-event-supertrend.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG,100,it) };bitmap.recycle()
                } }
                Assert.assertEquals(listOf("GET /market/klines?symbol=BTCUSDT&interval=15m&limit=160"), api.requests.toList())
                api.assertNoWrites()
            } finally { Dispatchers.resetMain() }
        }
    }
}
