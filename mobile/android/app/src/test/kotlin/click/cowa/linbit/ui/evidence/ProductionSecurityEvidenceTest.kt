package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.junit.Assume
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionSecurityEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private var dialog: android.view.Window? = null
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    @org.junit.Before fun setup() {
        Assume.assumeTrue(System.getProperty("offscreen.shots") == "1" && System.getProperty("production.evidence") == "1")
        Assume.assumeTrue(System.getProperty("production.evidence.dir") != null)
        api = MockEvidenceApi()
        fixtures = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        api.route("GET", "/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"antiPhishingSet":false}""")
        rule.setContent { LinbitTheme(darkTheme = false) { SecurityHost(onBack = {}, vm = fixtures.security(), featureVm = fixtures.securityFeatures(), onDialogWindow = { dialog = it }) } }
        waitText("登录密码")
    }
    @org.junit.After fun close() {
        if (::api.isInitialized) {
            assert(api.requests.none { it.split(" ").first() != "GET" })
            api.close()
        }
    }
    private fun waitText(text: String) = rule.waitUntil(timeoutMillis = 8_000) { rule.onAllNodesWithText(text, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }

    @Test fun passwordFormReachesReviewWithoutRealCredentialOrSubmit() {
        rule.onNodeWithText("登录密码").performClick()
        rule.onNodeWithTag("security.password.old").performTextReplacement("INVALID_OLD_TEST_DATA1")
        rule.onNodeWithTag("security.password.new").performTextReplacement("INVALID_NEW_TEST_DATA2")
        rule.onNodeWithTag("security.password.confirm").performTextReplacement("INVALID_NEW_TEST_DATA2")
        harness.app("security-password", 1170, 2364)
        rule.onNodeWithText("下一步").performClick()
        rule.onNodeWithText("修改登录密码会注销其他设备会话。请核对后完成安全验证。", substring = true).assertExists()
        if (dialog == null) Assume.assumeTrue("Production password review present; independent pixels require ProductionDialogPixelCopyTest/device", false)
        harness.dialogWindow("security-password-confirm", dialog)
    }

    @Test fun antiPhishingFormReachesReviewWithoutClaimingBindingSuccess() {
        rule.onNodeWithText("防钓鱼码").performClick()
        rule.onNodeWithTag("security.antiphishing.value").performTextReplacement("INVALIDTESTONLY")
        harness.app("security-antiphishing", 1170, 2364)
        rule.onNodeWithText("核对并设置").performClick()
        rule.onNodeWithText("确认防钓鱼码").assertExists()
        if (dialog == null) Assume.assumeTrue("Production anti-phishing review present; independent pixels require device", false)
        harness.dialogWindow("security-antiphishing-confirm", dialog)
    }
}
