package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
open class ProductionContactEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private var window: android.view.Window? = null
    @Before fun start() {
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
        api = MockEvidenceApi()
        api.route("GET", "/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"email":"old@example.invalid","phone":"+10000000000","fundPasswordSet":false}""")
        val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        rule.setContent { LinbitTheme(false) { SecurityHost({}, f.security(), f.securityFeatures(), onDialogWindow = { window = it }) } }
        wait("邮箱")
    }
    @After fun close() { if (::api.isInitialized) { api.assertNoWrites(); api.close() } }
    private fun wait(t: String) { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(t, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
    private fun contact(phone: Boolean) {
        val tag = if (phone) "phone" else "email"
        rule.onNodeWithText(if (phone) "手机" else "邮箱").performClick()
        rule.onNodeWithTag("security.contact.value").performTextReplacement(if (phone) "10000000000" else "next@example.invalid")
        harness.app("security-$tag-form", 1170, 2364)
        rule.onNodeWithText("核对并继续").performClick()
        wait("新联系方式验证码")
        if (window == null) Assume.assumeTrue("Real $tag contact review semantic gate reached; independent window evidence NOT_RUN", false)
        harness.dialogWindow("security-$tag-confirm", window)
    }
    @Test fun actualEmailProductionHostWithNewAndOldCodeRequiredNoSendOrBind() = contact(false)
    @Test fun actualPhoneProductionHostWithNewAndOldCodeRequiredNoSendOrBind() = contact(true)
    @Test fun fundPasswordProductionHostReachesReviewWithNoBindingMutation() {
        rule.onNodeWithText("资金密码").performClick(); wait("设置资金密码")
        rule.onNodeWithTag("security.fund.value").performTextReplacement("000000")
        rule.onNodeWithTag("security.fund.confirm").performTextReplacement("000000")
        harness.app("security-fund-form", 1170, 2364)
        rule.onNodeWithText("核对并继续").performClick(); wait("确认资金密码")
        if (window == null) Assume.assumeTrue("Fund review semantic gate reached; independent window evidence NOT_RUN", false)
        harness.dialogWindow("security-fund-confirm", window)
    }
}
