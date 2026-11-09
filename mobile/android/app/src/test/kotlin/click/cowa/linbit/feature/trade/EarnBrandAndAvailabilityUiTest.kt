package click.cowa.linbit.feature.trade

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import android.view.View
import android.view.Window
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.brand.BrandCopy
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.components.ToastOverlay
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File
import java.io.FileOutputStream

/**
 * 协议品牌与已结束产品 CTA。截图：/workspace/linbit-verify/batch14/（离屏渲染，非真机）。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class EarnBrandAndAvailabilityUiTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(Dispatchers.Main)
    private val outDir = File("/workspace/linbit-verify/batch14").also { it.mkdirs() }
    private var dialogWindow: Window? = null

    private fun vm(productsJson: String): SpotSubpagesViewModel {
        val client = OkHttpClient.Builder().addInterceptor { chain ->
            val path = chain.request().url.encodedPath
            val (code, body) = when {
                path.endsWith("/earn/products") -> 200 to productsJson
                path.contains("/earn/") -> 200 to """{"items":[]}"""
                else -> 404 to """{"code":"NOT_FOUND"}"""
            }
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(code).message("ok")
                .body(body.toResponseBody()).build()
        }.build()
        val tokens = InMemoryTokenStore().also { it.accessToken = "fixture" }
        return SpotSubpagesViewModel(NetworkFactory.api(client, "https://example.invalid/"), tokens, ToastCenter(scope))
    }

    private fun shotDecor(name: String, window: Window? = null) {
        rule.waitForIdle()
        rule.runOnIdle {
            val decor = (window ?: rule.activity.window).decorView
            check(decor.width > 0 && decor.height > 0) { "empty decor ${decor.width}x${decor.height}" }
            val bmp = Bitmap.createBitmap(decor.width, decor.height, Bitmap.Config.ARGB_8888)
            decor.draw(Canvas(bmp))
            FileOutputStream(File(outDir, name)).use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    private fun waitText(t: String) = rule.waitUntil(10_000) {
        rule.onAllNodesWithText(t, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
    }

    @Test fun agreementShowsLinbitNotCompetitorAndCapture() {
        dialogWindow = null
        val products = """{"items":[{"id":"fixed-ETH-14","kind":"fixed","asset":"ETH","title":"ETH-抢购","apr":"0.083","termDays":14,"minAmount":"0.1","status":"online","sold":"0"}]}"""
        val model = vm(products)
        rule.setContent {
            LinbitTheme(false) {
                EarnSubpage(0.dp, onLogin = {}, vm = model, onDialogWindow = { dialogWindow = it })
            }
        }
        waitText("ETH")
        rule.onNodeWithTag("earn.asset.ETH", useUnmergedTree = true).performClick()
        waitText("申购")
        rule.onNodeWithTag("earn.subscribe", useUnmergedTree = true).performClick()
        waitText("Linbit 的理财服务协议")
        assertTrue(
            rule.onAllNodesWithText(BrandCopy.EARN_AGREEMENT, substring = true, useUnmergedTree = true)
                .fetchSemanticsNodes().isNotEmpty(),
        )
        assertFalse(
            rule.onAllNodesWithText("EasiCoin", substring = true, useUnmergedTree = true)
                .fetchSemanticsNodes().isNotEmpty(),
        )
        rule.waitUntil(5_000) { dialogWindow != null && (dialogWindow?.decorView?.width ?: 0) > 0 }
        shotDecor("linbit-earn-agreement-linbit-vc12-offscreen.png", dialogWindow)
        File(outDir, "linbit-earn-agreement-linbit-vc12-offscreen.txt").writeText(
            "离屏渲染（Robolectric GraphicsMode.NATIVE + ModalBottomSheet DialogWindow.decorView.draw），非真机。申购弹层协议为「${BrandCopy.EARN_AGREEMENT}」，不含 EasiCoin。\n",
        )
    }

    @Test fun endedProductCtaShowsEndedToastAndCapture() {
        val products = """{"items":[{"id":"fixed-ETH-ended","kind":"fixed","asset":"ETH","title":"ETH-抢购","apr":"0.083","termDays":14,"minAmount":"0.1","status":"ended","sold":"100","remaining":"0"}]}"""
        val model = vm(products)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.fillMaxSize()) {
                    EarnSubpage(0.dp, onLogin = {}, vm = model)
                    ToastOverlay(model.toast)
                }
            }
        }
        waitText("ETH")
        rule.onNodeWithTag("earn.asset.ETH", useUnmergedTree = true).performClick()
        waitText("已结束")
        rule.onNodeWithTag("earn.subscribe", useUnmergedTree = true).performClick()
        waitText("申购已结束")
        shotDecor("linbit-earn-ended-toast-vc12-offscreen.png")
        File(outDir, "linbit-earn-ended-toast-vc12-offscreen.txt").writeText(
            "离屏渲染（Robolectric GraphicsMode.NATIVE + activity decorView.draw），非真机。产品 status=ended；点击「已结束」显示 toast「申购已结束」，不会提示过去的开始时间。\n",
        )
    }
}
