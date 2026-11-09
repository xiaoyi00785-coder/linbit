package click.cowa.linbit.feature.home

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiHomeNewcomer
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.ui.theme.LinbitTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/**
 * 游客首页主视觉动画：节奏（阶段表）、减少动画、非空白绘制、登录/注册按钮可用。
 * -PoffscreenShots=1 时输出离屏帧到 build/reports/home-hero-shots/（不入库）。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class GuestHeroAnimationTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val shots = System.getProperty("offscreen.shots") == "1"
    private val out by lazy { File("build/reports/home-hero-shots").absoluteFile.also { it.mkdirs() } }

    @Test fun timelineMatchesReferenceRhythmAndLoops() {
        assertEquals(11_333L, HERO_LOOP_MS)
        assertEquals(HeroPhase.RISE, heroPhaseAt(0))
        assertEquals(HeroPhase.BURST, heroPhaseAt(2_000))
        assertEquals(HeroPhase.ORBIT, heroPhaseAt(5_500))
        assertEquals(HeroPhase.CLUSTER, heroPhaseAt(9_200))
        assertEquals(HeroPhase.SETTLE, heroPhaseAt(11_000))
        assertEquals(HeroPhase.RISE, heroPhaseAt(HERO_LOOP_MS + 10))
        assertEquals(HeroPhase.CLUSTER, heroPhaseAt(HERO_STATIC_MS))
    }

    @Test fun reduceMotionUsesStaticFrame() {
        val ctx = ApplicationProvider.getApplicationContext<Application>()
        Settings.Global.putFloat(ctx.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
        assertTrue(heroReduceMotion(ctx))
        assertEquals(HERO_STATIC_MS, heroInitialFrame(true, null))
        Settings.Global.putFloat(ctx.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
        assertFalse(heroReduceMotion(ctx))
        assertEquals(0L, heroInitialFrame(false, null))
        assertEquals(5_500L, heroInitialFrame(true, 5_500L))
    }

    @Test fun everyKeyFrameDrawsBrandArtwork() {
        var frame by mutableLongStateOf(0L)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 220.dp).background(Color.White)) {
                    GuestHeroAnimation(Modifier.width(390.dp).aspectRatio(16f / 9f), frameOverrideMs = frame)
                }
            }
        }
        val keys = listOf(400L, 1_900L, 5_600L, 9_200L, 10_800L)
        keys.forEach { ms ->
            frame = ms
            rule.waitForIdle()
            val bmp = snapshot()
            val inked = inkRatio(heroCrop(bmp))
            assertTrue("帧 ${ms}ms 不应空白（非白像素占比=$inked）", inked > 0.02)
            if (shots) save(heroCrop(bmp), "hero-frame-%05d".format(ms))
        }
    }

    @Test fun guestHomeShowsAnimationAboveHeadlineAndLoginWorks() {
        var logins = 0
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 800.dp).background(Color.White)) {
                    HomeContent(
                        HomeState(
                            newcomer = ApiHomeNewcomer(total = "220", asset = "USDT", available = true),
                            earn = listOf(ApiEarnProduct(asset = "ETH", title = "ETH 活期", apr = "0.008")),
                            newcomerTotal = "220",
                        ),
                        MarketState(), emptyList(), false, 0.dp,
                        {}, {}, {}, {}, {},
                        onRefresh = {}, onLogin = { logins++ }, heroFrameMs = 5_600L,
                    )
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithTag(HERO_TAG).assertIsDisplayed()
        val hero = rule.onNodeWithTag(HERO_TAG).fetchSemanticsNode().boundsInRoot
        val title = rule.onNodeWithText("立即注册，最高可获得 220 USDT").fetchSemanticsNode().boundsInRoot
        assertTrue("动画应位于标题上方", hero.bottom <= title.top)
        assertEquals(16f / 9f, hero.width / hero.height, 0.02f)
        if (shots) save(snapshot(), "home-guest-hero")
        rule.onNodeWithText("登录/注册").performClick()
        assertEquals(1, logins)
    }

    /** 仅 -PoffscreenShots=1 且环境变量 HERO_GIF=1 时：按 10fps 导出整轮帧，用于拼 GIF 预览。 */
    @Test fun exportLoopFramesForGif() {
        if (!shots || System.getenv("HERO_GIF") != "1") return
        var frame by mutableLongStateOf(0L)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 220.dp).background(Color.White)) {
                    GuestHeroAnimation(Modifier.fillMaxWidth().aspectRatio(16f / 9f), frameOverrideMs = frame)
                }
            }
        }
        val dir = File(out, "gif").also { it.mkdirs() }
        var i = 0
        var ms = 0L
        while (ms < HERO_LOOP_MS) {
            frame = ms; rule.waitForIdle()
            save(heroCrop(snapshot()), "gif/f%04d".format(i++))
            ms += 100
        }
        assertTrue(dir.listFiles()!!.size > 100)
    }

    private fun snapshot(): Bitmap {
        var bmp: Bitmap? = null
        rule.runOnIdle {
            val view = rule.activity.findViewById<android.view.View>(android.R.id.content)
            bmp = Bitmap.createBitmap(view.width.coerceAtLeast(1), view.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bmp!!))
        }
        return bmp!!
    }

    private fun heroCrop(b: Bitmap): Bitmap {
        val r = rule.onNodeWithTag(HERO_TAG).fetchSemanticsNode().boundsInRoot
        val l = r.left.toInt().coerceIn(0, b.width - 1); val t = r.top.toInt().coerceIn(0, b.height - 1)
        return Bitmap.createBitmap(b, l, t, r.width.toInt().coerceAtMost(b.width - l), r.height.toInt().coerceAtMost(b.height - t))
    }

    private fun inkRatio(b: Bitmap): Double {
        var ink = 0; var n = 0
        for (y in 0 until b.height step 4) for (x in 0 until b.width step 4) {
            val c = b.getPixel(x, y); n++
            val r = (c shr 16) and 0xff; val g = (c shr 8) and 0xff; val bl = c and 0xff
            if (r < 235 || g < 235 || bl < 235) ink++
        }
        return ink.toDouble() / n
    }

    private fun save(b: Bitmap, name: String) {
        File(out, "$name.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
}
