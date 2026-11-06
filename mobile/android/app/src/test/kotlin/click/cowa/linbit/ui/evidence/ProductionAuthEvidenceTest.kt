package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import click.cowa.linbit.feature.auth.AuthFlow
import click.cowa.linbit.feature.auth.AuthViewModel
import click.cowa.linbit.feature.auth.LocalLbSheetInlineEvidence
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assume
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Production navigation only, no account creation/reset or successful login/2FA binding. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionAuthEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private val scope = CoroutineScope(Dispatchers.Main)
    private lateinit var api: MockEvidenceApi
    private lateinit var fixtures: ProductionEvidenceFixtures
    private var dialog: android.view.Window? = null

    @Before fun setup() {
        Assume.assumeTrue(System.getProperty("offscreen.shots") == "1" && System.getProperty("production.evidence") == "1")
        Assume.assumeTrue(System.getProperty("production.evidence.dir") != null)
        api = MockEvidenceApi(); fixtures = ProductionEvidenceFixtures(api, scope)
    }
    @org.junit.After fun close() { if (::api.isInitialized) api.close() }

    private fun waitText(text: String, vm: AuthViewModel) {
        try { rule.waitUntil(timeoutMillis = 10_000) {
            runCatching { rule.onAllNodesWithText(text, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false)
        } } catch (e: Throwable) {
            throw AssertionError("wait $text: account=${vm.raw} loading=${vm.loading} error=${vm.accountError} passwordError=${vm.passwordError} challenge=${vm.challenge?.challengeToken} routes=${api.requests}", e)
        }
    }

    private fun launch(vm: AuthViewModel, startRegister: Boolean = false) {
        rule.setContent {
            CompositionLocalProvider(LocalLbSheetInlineEvidence provides false) {
                LinbitTheme(darkTheme = false) {
                    AuthFlow(startRegister, onClose = {}, vm = vm, onDialogWindow = { dialog = it })
                }
            }
        }
    }

    private fun loginEntry(vm: AuthViewModel) {
        launch(vm)
        harness.app("auth-step1-login", 1170, 2364)
        rule.onNodeWithTag("auth.account").performTextReplacement("audit@example.invalid")
        rule.onNodeWithTag("auth.login.next").performClick()
        waitText("请输入密码", vm)
        harness.app("auth-step2-password", 1170, 2364)
    }

    @Test fun loginPasswordAndChallengeThroughProductionNavHost() {
        api.route("POST", "/auth/login", """{"challengeToken":"INVALID_VISUAL_TOKEN","methods":["email"],"expiresIn":120}""")
        val vm = fixtures.auth(); loginEntry(vm)
        rule.onNodeWithTag("auth.password").performTextReplacement("INVALID_TEST1")
        rule.onNodeWithText("下一步").performClick()
        waitText("验证邮箱", vm)
        harness.app("auth-step3-login-verify", 1170, 2364)
        assertTrue(api.requests.contains("POST /auth/login"))
        assertNull(fixtures.tokens.accessToken)
        assertFalse(api.requests.contains("POST /auth/login/verify"))
    }

    @Test fun registerVerifyAndSetPasswordWithoutOpeningAccount() {
        api.route("POST", "/auth/code/send", """{"sent":true,"channel":"email","ttlSeconds":60}""")
        val vm = fixtures.auth(); launch(vm, startRegister = true)
        harness.app("auth-step1-register", 1170, 2364)
        rule.onNodeWithTag("auth.account").performTextReplacement("audit@example.invalid")
        if (rule.onAllNodesWithTag("auth.register.agree.unchecked").fetchSemanticsNodes().isNotEmpty()) {
            rule.onNodeWithTag("auth.register.agree.unchecked").performClick()
        }
        rule.onNodeWithTag("auth.register.submit").performClick()
        waitText("验证邮箱", vm); harness.app("auth-step2-register-verify", 1170, 2364)
        rule.onNodeWithTag("auth.otp.input").performTextReplacement("000000")
        waitText("设置密码", vm); harness.app("auth-step3-register-password", 1170, 2364)
        assertFalse(api.requests.contains("POST /auth/register"))
        assertNull(fixtures.tokens.accessToken)
    }

    @Test fun forgotResetVerifyAndFinalFormWithoutResettingPassword() {
        api.route("POST", "/auth/code/send", """{"sent":true,"channel":"email","ttlSeconds":60}""")
        api.route("GET", "/auth/config", "{}")
        api.route("POST", "/auth/password/verify-code", """{"resetToken":"INVALID_RESET_TOKEN","expiresIn":60}""")
        val vm = fixtures.auth(); loginEntry(vm)
        rule.onNodeWithText("忘记密码?").performClick()
        waitText("重置密码", vm); harness.app("auth-step2-forgot", 1170, 2364)
        rule.onNodeWithTag("auth.forgot.next").performClick()
        waitText("验证邮箱", vm); harness.app("auth-step3-forgot-verify", 1170, 2364)
        rule.onNodeWithTag("auth.otp.input").performTextReplacement("000001")
        waitText("确认密码", vm); harness.app("auth-step4-reset-password", 1170, 2364)
        assertTrue(api.requests.contains("POST /auth/password/verify-code"))
        assertFalse(api.requests.contains("POST /auth/password/reset"))
        assertNull(fixtures.tokens.accessToken)
    }

    @Test fun loginHelpIndependentWindowRequiresSupportedEnvironment() {
        api.route("POST", "/auth/login", """{"challengeToken":"INVALID_VISUAL_TOKEN","methods":["email"],"expiresIn":120}""")
        val vm = fixtures.auth(); loginEntry(vm)
        rule.onNodeWithTag("auth.password").performTextReplacement("INVALID_TEST1")
        rule.onNodeWithText("下一步").performClick()
        waitText("收不到验证码?", vm)
        rule.onNodeWithText("收不到验证码?", useUnmergedTree = true).performClick()
        if (dialog == null) {
            Assume.assumeNoException("No independent production Dialog.Window in this environment; ProductionDialogPixelCopyTest is compiled NOT RUN", AssertionError("REAL_WINDOW_UNAVAILABLE"))
        }
        harness.dialogWindow("auth-login-help-dialog", dialog)
        assertFalse(api.requests.contains("POST /auth/login/verify"))
    }
}
