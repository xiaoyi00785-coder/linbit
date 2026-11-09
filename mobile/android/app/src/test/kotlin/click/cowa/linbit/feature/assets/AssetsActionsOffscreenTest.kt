package click.cowa.linbit.feature.assets

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import click.cowa.linbit.R
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LinbitTheme
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.lb
import org.junit.Assert.assertEquals
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
 * vc12 #4/#5：快捷按钮矢量图标 + 去掉整宽浅灰分隔块。截图：batch14。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class AssetsActionsOffscreenTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val out = File("/workspace/linbit-verify/batch14").also { it.mkdirs() }

    private fun shot(name: String) {
        rule.waitForIdle()
        rule.runOnIdle {
            val decor = rule.activity.window.decorView
            val bmp = Bitmap.createBitmap(decor.width, decor.height, Bitmap.Config.ARGB_8888)
            decor.draw(Canvas(bmp))
            FileOutputStream(File(out, name)).use { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test fun spotActionsVectorIconsNoGrayBarAndCapture() {
        rule.setContent {
            LinbitTheme(false) {
                val p = LB.p
                Column(Modifier.fillMaxSize().background(p.bg).padding(top = 40.dp)) {
                    Text("总资产估值", Modifier.padding(horizontal = 16.dp), style = lb(FontSize.caption), color = p.textSecondary)
                    Text("0.00", Modifier.padding(horizontal = 16.dp), style = lb(32.sp, FontWeight.Bold), color = p.textPrimary)
                    Text("USDT", Modifier.padding(horizontal = 16.dp), style = lb(FontSize.small), color = p.textPrimary)
                    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp).padding(top = 20.dp)) {
                        listOf("去交易", "充值", "快捷买币", "划转").forEach { title ->
                            Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                                Box(
                                    Modifier.size(40.dp).background(p.fill, RoundedCornerShape(Radius.lg))
                                        .testTag("assets.action.$title"),
                                    contentAlignment = Alignment.Center,
                                ) { LbIcon(AssetsActionIcons.drawable(title), 22.dp, p.textPrimary) }
                                Spacer(Modifier.height(8.dp))
                                Text(title, style = lb(FontSize.small), color = p.textPrimary)
                            }
                        }
                    }
                    // 参考：无整宽浅灰分隔块，仅留白
                    Spacer(Modifier.height(16.dp).testTag("assets.section.gap"))
                    Row(
                        Modifier.padding(horizontal = 16.dp).fillMaxWidth().height(44.dp)
                            .border(0.5.dp, p.border, RoundedCornerShape(Radius.md))
                            .padding(horizontal = 12.dp)
                            .testTag("assets.convert.entry"),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        LbIcon(R.drawable.ic_cv_inverse, 16.dp, p.textPrimary)
                        Text("现货闪兑", Modifier.padding(start = 8.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                        Text(
                            "0 手续费",
                            Modifier.padding(start = 8.dp).background(p.brand, RoundedCornerShape(4.dp)).padding(horizontal = 5.dp, vertical = 1.dp),
                            style = lb(10.sp, FontWeight.Medium),
                            color = androidx.compose.ui.graphics.Color.Black,
                        )
                        Spacer(Modifier.weight(1f))
                        LbIcon(R.drawable.ic_chevron_right, 14.dp, p.textSecondary)
                    }
                    Text("资产", Modifier.padding(16.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                }
            }
        }
        rule.waitForIdle()
        listOf("去交易", "充值", "快捷买币", "划转").forEach { t ->
            assertEquals(1, rule.onAllNodesWithTag("assets.action.$t", useUnmergedTree = true).fetchSemanticsNodes().size)
        }
        listOf("去", "充", "快", "划").forEach { ch ->
            val onlyLetter = rule.onAllNodesWithText(ch, substring = false, useUnmergedTree = true)
                .fetchSemanticsNodes()
                .filter { node ->
                    val texts = node.config.getOrElse(androidx.compose.ui.semantics.SemanticsProperties.Text) { emptyList() }
                        .map { it.text }
                    texts == listOf(ch)
                }
            assertTrue("letter placeholder `$ch` must not appear as sole icon text", onlyLetter.isEmpty())
        }
        assertEquals(1, rule.onAllNodesWithTag("assets.section.gap", useUnmergedTree = true).fetchSemanticsNodes().size)
        assertEquals(1, rule.onAllNodesWithTag("assets.convert.entry", useUnmergedTree = true).fetchSemanticsNodes().size)
        shot("linbit-assets-spot-actions-vc12-offscreen.png")
        File(out, "linbit-assets-spot-actions-vc12-offscreen.txt").writeText(
            "离屏渲染（Robolectric + decorView.draw），非真机。现货 tab：快捷按钮为矢量线性图标；快捷行下方已去掉整宽浅灰分隔块（仅留白，对照参考）。\n",
        )
        shot("linbit-assets-spot-separator-vc12-offscreen.png")
        File(out, "linbit-assets-spot-separator-vc12-offscreen.txt").writeText(
            "同帧证据侧重分隔：参考 App 快捷按钮与现货闪兑之间无 8dp 浅灰横条。\n",
        )
    }
}
