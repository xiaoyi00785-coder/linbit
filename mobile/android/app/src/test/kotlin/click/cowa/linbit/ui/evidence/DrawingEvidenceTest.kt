package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
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
@Config(sdk=[34],application=Application::class,qualifiers="zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class DrawingEvidenceTest {
    @get:Rule val rule=createAndroidComposeRule<ComponentActivity>()
    private fun capture(name: String, dialogs: Boolean=false) {
        rule.waitForIdle()
        System.getProperty("production.evidence.dir")?.let { path -> rule.runOnIdle {
            val v=rule.activity.window.decorView
            val b=Bitmap.createBitmap(v.width,v.height,Bitmap.Config.ARGB_8888)
            val canvas=Canvas(b);v.draw(canvas)
            if (dialogs) org.robolectric.shadows.ShadowDialog.getShownDialogs().filter { it.isShowing }.forEach { d ->
                val view=d.window!!.decorView
                canvas.drawColor(android.graphics.Color.argb(128,0,0,0));canvas.save()
                canvas.translate((b.width-view.width)/2f,(b.height-view.height).toFloat());view.draw(canvas);canvas.restore()
            }
            File(path).mkdirs();File(path,"mock-$name.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG,100,it) };b.recycle()
        } }
    }
    @Test fun drawZoomSwitchManageAndClearWithoutWrites() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        MockEvidenceApi().use { api -> try {
            val json=(0 until 160).joinToString(",","[","]") { i ->
                val v=100+if((i/25)%2==0)i%25 else 25-i%25
                """{"time":${1700000000L+i*900},"open":"$v","high":"${v+2}","low":"${v-2}","close":"${v+1}","volume":"10"}"""
            }
            repeat(3) { api.route("GET","/market/klines?symbol=BTCUSDT&interval=15m&limit=160",json) }
            api.route("GET","/market/klines?symbol=ETHUSDT&interval=15m&limit=160",json)
            api.route("GET","/market/klines?symbol=BTCUSDT&interval=5m&limit=160",json)
            val f=ProductionEvidenceFixtures(api,CoroutineScope(Dispatchers.Main));val vm=f.event()
            rule.activity.actionBar?.hide()
            rule.setContent { LinbitTheme(false) { Column(Modifier.fillMaxSize().background(LB.p.bg)) { EventChart(vm) } } }
            rule.waitUntil(8000) { rule.onAllNodesWithText("60 根 · 画线 0").fetchSemanticsNodes().isNotEmpty() }
            fun tool(name: String) { rule.onNodeWithTag("event.drawing.tools").performClick();rule.onNodeWithText(name).performClick() }
            fun tap(x: Float,y: Float) { rule.onNodeWithTag("event.drawing.plot").performTouchInput { click(Offset(center.x*2*x,center.y*2*y)) } }
            tool("水平线");tap(0.3f,0.4f)
            rule.onNodeWithText("60 根 · 画线 1").assertIsDisplayed()
            tool("趋势线");tap(0.25f,0.2f)
            rule.onNodeWithText("轻点图表选择趋势线终点").assertIsDisplayed()
            tap(0.75f,0.7f);rule.onNodeWithText("60 根 · 画线 2").assertIsDisplayed()
            capture("event-drawings")
            rule.onNodeWithText("放大").performClick();rule.onNodeWithText("40 根 · 画线 2").assertIsDisplayed()
            capture("event-drawings-zoom")
            rule.runOnIdle { vm.symbol="ETHUSDT" }
            rule.waitUntil(8000) { rule.onAllNodesWithText("60 根 · 画线 0").fetchSemanticsNodes().isNotEmpty() }
            rule.runOnIdle { vm.symbol="BTCUSDT" }
            rule.waitUntil(8000) { rule.onAllNodesWithText("60 根 · 画线 2").fetchSemanticsNodes().isNotEmpty() }
            rule.onNodeWithText("5m").performClick()
            rule.waitUntil(8000) { rule.onAllNodesWithText("60 根 · 画线 0").fetchSemanticsNodes().isNotEmpty() }
            rule.onNodeWithText("15m").performClick()
            rule.waitUntil(8000) { rule.onAllNodesWithText("60 根 · 画线 2").fetchSemanticsNodes().isNotEmpty() }
            rule.onNodeWithTag("event.drawing.tools").performClick();capture("event-drawing-tools",true)
            rule.onNodeWithText("水平线 #1").performClick();rule.onNodeWithText("删除选中").performClick()
            rule.onNodeWithText("管理画线（1）").assertIsDisplayed()
            rule.onNodeWithText("清空本图").performClick();rule.onNodeWithText("清空",useUnmergedTree=true).performClick()
            rule.onNodeWithText("浏览 / 缩放").performClick();rule.onNodeWithText("60 根 · 画线 0").assertIsDisplayed()
            api.assertNoWrites()
        } finally { Dispatchers.resetMain() } }
    }
}
