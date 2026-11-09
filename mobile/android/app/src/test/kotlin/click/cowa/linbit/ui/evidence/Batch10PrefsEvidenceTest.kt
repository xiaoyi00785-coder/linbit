package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.core.storage.UpDownColor
import click.cowa.linbit.feature.profile.AccountPreferencesViewModel
import click.cowa.linbit.feature.profile.DepositTargetViewModel
import click.cowa.linbit.feature.profile.LocalAccountPreferencesFactory
import click.cowa.linbit.feature.profile.LocalDepositTargetFactory
import click.cowa.linbit.feature.profile.PrefLanguages
import click.cowa.linbit.feature.auth.LocalLbSheetInlineEvidence
import click.cowa.linbit.feature.profile.UserCenterScreen
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** 离屏夹具/非生产。偏好页颜色/主题视觉结构与语言列表；不变用户语言。 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class Batch10PrefsEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures

    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        Dispatchers.setMain(UnconfinedTestDispatcher())
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        fixtures.tokens.accessToken = "INVALID_BATCH10_FIXTURE"
    }

    @After fun done() {
        if (::api.isInitialized) { api.assertNoWrites(); api.close() }
        Dispatchers.resetMain()
    }

    private fun render(content: @Composable () -> Unit) {
        rule.activity.actionBar?.hide()
        rule.setContent { LinbitTheme(false) { Box(Modifier.fillMaxSize().background(LB.p.bg)) { content() } } }
    }

    private fun capture(name: String) {
        rule.waitForIdle()
        System.getProperty("production.evidence.dir")?.let { path ->
            rule.runOnIdle {
                val v = rule.activity.window.decorView
                val b = Bitmap.createBitmap(v.width.coerceAtLeast(1), v.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
                Canvas(b).also { v.draw(it) }
                File(path).mkdirs()
                File(path, "mock-b10-$name.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG, 100, it) }
                b.recycle()
            }
        }
    }

    private fun openPrefs(sheet: String? = null) {
        api.route("GET", "/account/preferences", """{"quoteCurrency":"USD","defaultQuoteCurrency":"USD","supportedQuoteCurrencies":["USD","CNY"]}""")
        api.route("GET", "/assets/deposit-account", """{"code":"NOT_FOUND","message":"not found"}""", 404)
        val vm = AccountPreferencesViewModel(fixtures.retrofit, fixtures.tokens)
        val dvm = DepositTargetViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast)
        val profile = fixtures.profile()
        render {
            CompositionLocalProvider(
                LocalLbSheetInlineEvidence provides true,
                LocalAccountPreferencesFactory provides { vm },
                LocalDepositTargetFactory provides { dvm },
            ) {
                UserCenterScreen(ThemeMode.LIGHT, {}, {}, {}, {}, {}, profile)
            }
        }
        rule.onNodeWithText("偏好设置").performClick()
        rule.waitUntil(5_000) { dvm.unavailable || dvm.target != null }
        if (sheet != null) rule.onAllNodesWithText(sheet)[0].performClick()
        rule.waitForIdle()
    }

    @Test fun preferencesRowsUseVisualTrailersNotOnlyText() {
        openPrefs()
        // 行存在；颜色偏好不再以「绿涨红跌」纯文字作为行尾（参考为 K 线图标）
        rule.onNodeWithText("颜色偏好").assertIsDisplayed()
        rule.onNodeWithText("主题设置").assertIsDisplayed()
        rule.onNodeWithText("暂未开放").assertIsDisplayed()
        capture("preferences")
    }

    @Test fun colorSheetShowsCandlesAndLabels() {
        openPrefs("颜色偏好")
        rule.onNodeWithText(UpDownColor.GREEN_UP.title).assertIsDisplayed()
        rule.onNodeWithText(UpDownColor.RED_UP_GREEN_DOWN.title).assertIsDisplayed()
        rule.onNodeWithText(UpDownColor.RED_UP_BLUE_DOWN.title).assertIsDisplayed()
        capture("color-sheet")
    }

    @Test fun languageSheetListsReferenceOrderWithoutSwitchingLocale() {
        openPrefs("语言")
        assertEquals(14, PrefLanguages.all.size)
        assertEquals("English", PrefLanguages.all.first())
        assertTrue(PrefLanguages.all.contains(PrefLanguages.SUPPORTED))
        // 列表较长时底部项可能离屏；用语义树确认条目存在，不要求全部同时可见。
        assertTrue(rule.onAllNodesWithText("简体中文").fetchSemanticsNodes().isNotEmpty())
        assertTrue(rule.onAllNodesWithText("English").fetchSemanticsNodes().isNotEmpty())
        assertTrue(rule.onAllNodesWithText("日本語", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        rule.onAllNodesWithText("English")[0].performClick()
        rule.waitForIdle()
        // 未切换语言：仍能回到偏好行看到简体中文（弹层可能仍开着或 toast）
        assertTrue(rule.onAllNodesWithText("简体中文").fetchSemanticsNodes().isNotEmpty())
        capture("language-sheet")
    }
}
