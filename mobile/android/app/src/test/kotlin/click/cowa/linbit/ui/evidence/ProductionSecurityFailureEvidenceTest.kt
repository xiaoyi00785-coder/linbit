package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.*
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** A distinct 503-only harness: a failed send must not reach a bind endpoint. */
@RunWith(RobolectricTestRunner::class) @GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionSecurityFailureEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private var dialog: android.view.Window? = null
    private fun await(text: String) { rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(text, substring = true, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) } }
    private fun render(api: MockEvidenceApi, bound: Boolean) {
        api.route("GET", "/account/security", """{"twoFactor":{"enabled":false},"email":"audit@example.invalid","phone":"+10000000000","fundPasswordSet":$bound}""")
        val f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        rule.setContent { LinbitTheme(false) { SecurityHost({}, f.security(), f.securityFeatures(), onDialogWindow = { dialog = it }) } }
        await("邮箱"); rule.onNodeWithText("资金密码", useUnmergedTree = true).performClick()
        await(if (bound) "修改资金密码" else "设置资金密码")
        if (bound) rule.onNodeWithTag("security.fund.old").performTextReplacement("000000")
        rule.onNodeWithTag("security.fund.value").performTextReplacement("000001"); rule.onNodeWithTag("security.fund.confirm").performTextReplacement("000001")
        rule.onNodeWithText("原资金密码", substring = true).let { if (bound) it.assertExists() else it.assertDoesNotExist() }
        ProductionEvidenceHarness(rule).app("security-fund-${if (bound) "bound" else "new"}-non2fa-form", 1170, 2364)
        rule.onNodeWithText("核对并继续", useUnmergedTree = true).performClick(); await("安全验证码"); rule.onNodeWithText("6 位身份验证器动态码", useUnmergedTree = true).assertDoesNotExist()
    }
    @Test fun boundFundPasswordPurposeSecuritySendFailure() { MockEvidenceApi().use { api ->
        render(api, true); rule.waitUntil(5_000) { dialog != null }
        ProductionEvidenceHarness(rule).dialogWindow("security-fund-bound-non2fa-review", dialog)
        api.explicitFailure("POST", "/account/code/send", "测试：验证码发送失败")
        rule.onNodeWithText("发送", useUnmergedTree = true).performClick(); await("测试：验证码发送失败")
        ProductionEvidenceHarness(rule).dialogWindow("security-send-failure-non2fa", dialog)
        rule.onNodeWithText("发送", useUnmergedTree = true).assertIsEnabled(); rule.onNodeWithTag("security.fund.value").performTextReplacement("000001")
        Assert.assertTrue(api.requests.none { it.contains("/account/fund-password") }); api.assertOnlyExpectedFailures()
    } }
}
