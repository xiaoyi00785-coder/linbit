package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import click.cowa.linbit.feature.trade.ConvertSubpage
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import androidx.compose.ui.unit.dp
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.junit.Assume
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionConvertConfirmationTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    private var dialog: android.view.Window? = null
    @Before fun setup() {
        Assume.assumeTrue(System.getProperty("offscreen.shots") == "1" && System.getProperty("production.evidence") == "1")
        Assume.assumeTrue(System.getProperty("production.evidence.dir") != null)
        api = MockEvidenceApi(); fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        fixtures.tokens.accessToken = "INVALID_LAYOUT_TOKEN"
    }
    @org.junit.After fun close() { if (::api.isInitialized) api.close() }

    @Test fun finalReviewUsesCurrentQuoteAndCannotAcceptInEvidence() {
        val expiry = System.currentTimeMillis() + 120_000
        api.route("GET", "/convert/pairs", """{"pairs":[{"from":"USDT","to":"BTC","paused":false,"route":"direct"}]}""")
        api.route("POST", "/convert/quote", """{"quoteId":"INVALID-QUOTE-VISUAL-ONLY","from":"USDT","to":"BTC","fromAmount":"1","toAmount":"0.00001","rate":"0.00001","expiresAt":$expiry,"ttlSeconds":120}""")
        rule.setContent {
            LinbitTheme(darkTheme = false) {
                ConvertSubpage(bottomInset = 0.dp, onLogin = {}, vm = SpotSubpagesViewModel(fixtures.retrofit, fixtures.tokens, fixtures.toast), onDialogWindow = { dialog = it })
            }
        }
        rule.waitUntil(timeoutMillis = 8_000) { rule.onAllNodesWithText("兑出 USDT", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("convert.amount").performTextReplacement("1")
        rule.onNodeWithText("获取报价").performClick()
        rule.waitUntil(timeoutMillis = 8_000) { rule.onAllNodesWithText("确认闪兑", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("确认闪兑").performClick(); rule.waitForIdle()
        rule.onNodeWithText("确认兑换").assertExists()
        org.junit.Assert.assertFalse(api.requests.any { it.startsWith("POST /convert/accept") })
        if (dialog == null) {
            Assume.assumeNoException("Robolectric cannot expose production DialogWindowProvider; independent window capture pending device", AssertionError("REAL_WINDOW_UNAVAILABLE"))
        }
        harness.dialogWindow("convert-confirm", dialog)
    }
}
