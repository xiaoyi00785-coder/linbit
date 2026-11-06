package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Each case uses the actual Robolectric initialization configuration, never a claimed dp size. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
open class ProductionAdaptiveHostCase {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    open val width = 390; open val height = 844; open val dpi = 480; open val dark = false; open val font = 1.0f; open val label = "390-light"
    @Before fun configure() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        rule.activity.resources.configuration.let { check(it.screenWidthDp == width && it.fontScale == font && it.densityDpi == dpi) { "Requested qualifier $width/$dpi/$font != ${it.screenWidthDp}/${it.densityDpi}/${it.fontScale}" } }
        System.setProperty("production.evidence.qualifier", "w=$width;h=$height;xxhdpi;densityDpi=$dpi;night=${if (dark) "yes" else "no"};fontScale=$font")
    }
    @After fun reset() { System.clearProperty("production.evidence.qualifier") }
    private fun wait(t: String) { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(t, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
    @Test fun earnHostRealReadOnlyStateMeasured() {
        MockEvidenceApi().use { api ->
            api.route("GET", "/earn/products", """{"items":[]}""")
            api.route("GET", "/earn/summary", """{"totalValueUsdt":"0","totalInterestUsdt":"0"}""")
            api.route("GET", "/earn/positions", """{"items":[]}""")
            api.route("GET", "/earn/orders", """{"items":[]}""")
            api.route("GET", "/earn/records", """{"items":[]}""")
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            rule.setContent { LinbitTheme(dark) { click.cowa.linbit.feature.assets.AssetFeatureHost("earn", {}, {}, { _, _ -> }, SpotSubpagesViewModel(f.retrofit, f.tokens, f.toast)) } }
            wait("暂无理财产品"); ProductionEvidenceHarness(rule).app("asset-earn-$label"); api.assertNoWrites()
        }
    }
    @Test fun emailReviewRealFormMeasured() {
        var dialog: android.view.Window? = null
        MockEvidenceApi().use { api ->
            api.route("GET", "/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"email":"audit@example.invalid"}""")
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            rule.setContent { LinbitTheme(dark) { SecurityHost({}, f.security(), f.securityFeatures(), onDialogWindow = { dialog = it }) } }
            wait("邮箱"); rule.onNodeWithText("邮箱").performClick(); rule.onNodeWithTag("security.contact.value").performTextReplacement("next@example.invalid"); wait("下一步将核对新旧验证码与身份验证器信息。")
            rule.onNodeWithText("核对并继续").performClick(); wait("确认变更")
            rule.onNodeWithText("确认变更").assertIsNotEnabled()
            rule.waitUntil(10_000) { dialog != null }
            ProductionEvidenceHarness(rule).dialogWindow("security-email-review-$label", dialog); api.assertNoWrites()
        }
    }
    @Test fun emailSecurityHostRealFormMeasured() {
        MockEvidenceApi().use { api ->
            api.route("GET", "/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"email":"audit@example.invalid","fundPasswordSet":false}""")
            val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
            rule.setContent { LinbitTheme(dark) { SecurityHost({}, f.security(), f.securityFeatures()) } }
            wait("邮箱"); rule.onNodeWithText("邮箱").performClick(); rule.onNodeWithTag("security.contact.value").performTextReplacement("next@example.invalid"); wait("下一步将核对新旧验证码与身份验证器信息。")
            ProductionEvidenceHarness(rule).app("security-email-$label"); api.assertNoWrites()
        }
    }
}

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w320dp-h844dp-xxhdpi", fontScale = 1.3f)
class ProductionAdaptive320LargeTest : ProductionAdaptiveHostCase() { override val width = 320; override val height = 844; override val dpi = 480; override val dark = false; override val font = 1.3f; override val label = "320-light-font130" }

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-night-xxhdpi")
class ProductionAdaptive360DarkTest : ProductionAdaptiveHostCase() { override val width = 360; override val height = 844; override val dpi = 480; override val dark = true; override val font = 1.0f; override val label = "360-dark" }

@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w412dp-h915dp-xxhdpi")
class ProductionAdaptive412LightTest : ProductionAdaptiveHostCase() { override val width = 412; override val height = 915; override val dpi = 480; override val dark = false; override val font = 1.0f; override val label = "412-light" }
