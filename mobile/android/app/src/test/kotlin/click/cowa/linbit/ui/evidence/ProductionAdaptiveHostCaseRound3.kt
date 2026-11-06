package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import click.cowa.linbit.feature.assets.AssetFeatureHost
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Narrow/wide/dark/large-font representatives with measured app or true Dialog rectangle. */
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
abstract class ProductionAdaptiveRound3Case {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    abstract val width: Int; abstract val dark: Boolean; abstract val font: Float; abstract val label: String
    private var dialog: android.view.Window? = null
    @Before fun setup() {
        kotlinx.coroutines.Dispatchers.setMain(kotlinx.coroutines.test.UnconfinedTestDispatcher())
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        rule.activity.resources.configuration.let { check(it.screenWidthDp == width && it.fontScale == font && ((it.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) == if (dark) android.content.res.Configuration.UI_MODE_NIGHT_YES else android.content.res.Configuration.UI_MODE_NIGHT_NO)) { "Requested $width/$font/$dark mismatch $it" } }
        System.setProperty("production.evidence.qualifier", "w=$width;h=${if(width==412)915 else 844};xxhdpi;densityDpi=480;night=${if(dark)"yes" else "no"};fontScale=$font")
    }
    @After fun clear() { System.clearProperty("production.evidence.qualifier"); kotlinx.coroutines.Dispatchers.resetMain() }
    private fun wait(text: String) { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
    @Test fun earnNormalAndReview() { MockEvidenceApi().use { api ->
        earnRoutes(api); rule.setContent { LinbitTheme(dark) { AssetFeatureHost("earn", {}, {}, {_,_ ->}, evidenceVm(api), onDialogWindow = { dialog = it }) } }
        wait("我的持仓"); ProductionEvidenceHarness(rule).app("asset-earn-normal-$label")
        rule.onNodeWithText("申购", useUnmergedTree = true).performScrollTo().performClick(); rule.onNode(hasSetTextAction() and hasAnyAncestor(isDialog())).performTextInput("100")
        rule.onNodeWithText("下一步 · 申购复核", substring = true, useUnmergedTree = true).performClick(); wait("申购最终复核")
        rule.waitUntil(5_000) { dialog != null }; ProductionEvidenceHarness(rule).dialogWindow("asset-earn-subscribe-review-$label", dialog); api.assertNoWrites()
    } }
    @Test fun strategyStopDecision() { MockEvidenceApi().use { api ->
        strategyRoutes(api); val vm = evidenceVm(api); rule.setContent { LinbitTheme(dark) { AssetFeatureHost("strategy", {}, {}, {_,_ ->}, vm, onDialogWindow = { dialog = it }) } }
        wait("我的策略")
        try { rule.waitUntil(10_000) { vm.myGrids.isNotEmpty() } }
        catch (e: Throwable) { throw AssertionError("Strategy load failed: error=${vm.error}; loading=${vm.loading}; requests=${api.dumpRequests()}", e) }
        rule.onNodeWithText("停止", useUnmergedTree = true).performScrollTo().performClick(); wait("将保留底仓，仅撤单")
        rule.onNodeWithText("将保留底仓，仅撤单").performScrollTo(); rule.waitUntil(5_000) { dialog != null }
        ProductionEvidenceHarness(rule).dialogWindow("asset-grid-stop-review-$label", dialog); api.assertNoWrites()
    } }
    @Test fun non2faBoundFundPassword() { MockEvidenceApi().use { api ->
        api.route("GET", "/account/security", """{"twoFactor":{"enabled":false},"email":"audit@example.invalid","fundPasswordSet":true}""")
        val f=ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); rule.setContent { LinbitTheme(dark) { SecurityHost({}, f.security(), f.securityFeatures(), onDialogWindow = { dialog = it }) } }
        wait("资金密码"); rule.onNodeWithText("资金密码", useUnmergedTree = true).performClick(); wait("修改资金密码")
        rule.onNodeWithTag("security.fund.old").performTextReplacement("000000"); rule.onNodeWithTag("security.fund.value").performTextReplacement("000001"); rule.onNodeWithTag("security.fund.confirm").performTextReplacement("000001")
        rule.onNodeWithText("核对并继续").performClick(); wait("安全验证码"); rule.waitUntil(5_000) { dialog != null }
        ProductionEvidenceHarness(rule).dialogWindow("security-bound-non2fa-$label", dialog); api.assertNoWrites()
    } }
}
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34], application=Application::class, qualifiers="zh-rCN-w320dp-h844dp-night-xxhdpi")
class ProductionAdaptive320DarkRound3 : ProductionAdaptiveRound3Case() { override val width=320; override val dark=true; override val font=1f; override val label="320-dark" }
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34], application=Application::class, qualifiers="zh-rCN-w360dp-h844dp-xxhdpi", fontScale=1.3f)
class ProductionAdaptive360Font130Round3 : ProductionAdaptiveRound3Case() { override val width=360; override val dark=false; override val font=1.3f; override val label="360-font130" }
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk=[34], application=Application::class, qualifiers="zh-rCN-w412dp-h915dp-night-xxhdpi")
class ProductionAdaptive412DarkRound3 : ProductionAdaptiveRound3Case() { override val width=412; override val dark=true; override val font=1f; override val label="412-dark" }
