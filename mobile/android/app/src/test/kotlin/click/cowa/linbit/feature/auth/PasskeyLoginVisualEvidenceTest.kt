package click.cowa.linbit.feature.auth

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** Host-UI only: the system credential sheet and real RP/device ceremony are NOT tested here. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = android.app.Application::class, qualifiers = "zh-rCN-w320dp-h720dp-xhdpi")
class PasskeyLoginVisualEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(Dispatchers.Main)
    private var api: click.cowa.linbit.ui.evidence.MockEvidenceApi? = null
    private lateinit var vm: AuthViewModel
    private val tokens = InMemoryTokenStore()

    private fun selectPhoneWidth(width: Int, fontScale: Float = 1f) {
        rule.runOnUiThread {
            val resources = rule.activity.resources
            val configuration = android.content.res.Configuration(resources.configuration)
            configuration.screenWidthDp = width
            configuration.fontScale = fontScale
            @Suppress("DEPRECATION") resources.updateConfiguration(configuration, resources.displayMetrics)
        }
    }

    private fun render(error: PasskeyLoginFailure, dark: Boolean) {
        val server = click.cowa.linbit.ui.evidence.MockEvidenceApi().also { api = it }
        server.route("POST", "/auth/passkey/login/begin", JSONObject().put("sessionId", "VISUAL_ONLY_INVALID").put("publicKey", JSONObject().put("challenge", "YQ").put("rpId", "cowa.click")).toString())
        val api = NetworkFactory.api(okhttp3.OkHttpClient(), server.baseUrl)
        val bridge = PasskeyCredentialBridge { _, _ -> throw error }
        vm = AuthViewModel(AuthRepository(api, tokens, { "visual-invalid" }, { "Robolectric" }, scope), tokens,
            AppPrefs(RuntimeEnvironment.getApplication()), ToastCenter(scope), PasskeyLoginService(api, bridge, { "visual-invalid" }, { "Robolectric" }))
        rule.setContent { LinbitTheme(dark) { AuthFlow(startRegister = false, onClose = {}, vm = vm) } }
        rule.waitForIdle()
        rule.onNodeWithTag("auth.passkey.login").performScrollTo().assertIsEnabled().performClick()
        rule.waitUntil(5_000) { !vm.passkeyLoading }
        rule.waitForIdle()
        rule.onNodeWithText(error.message!!, substring = true).performScrollTo().assertIsDisplayed()
        rule.onNodeWithTag("auth.passkey.login").assertIsEnabled()
        assertEquals(1, server.requests.count { it.contains("/begin") })
        assertEquals(0, server.mutationRequests.count { it.path?.contains("/finish") == true })
    }

    private fun capture(name: String) {
        rule.runOnIdle {
            val view = rule.activity.findViewById<android.view.View>(android.R.id.content)
            val bitmap = Bitmap.createBitmap(view.width.coerceAtLeast(1), view.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            File("build/reports/passkey-shots", "$name.png").also { it.parentFile.mkdirs() }
                .outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test fun narrowLargeFontUnsupportedIsRecoverable() {
        selectPhoneWidth(320, 1.35f)
        render(PasskeyLoginFailure.Unsupported, dark = false)
        capture("passkey-login-320-large")
    }

    @Test fun darkNoCredentialIsRecoverable() {
        selectPhoneWidth(360)
        render(PasskeyLoginFailure.NoCredential, dark = true)
        capture("passkey-login-360-dark")
    }

    @org.junit.Before fun installMain() { Dispatchers.setMain(kotlinx.coroutines.test.UnconfinedTestDispatcher()) }
    @After fun shutdown() {
        rule.runOnUiThread { vm.cancelPasskeyLogin() }
        Dispatchers.resetMain(); api?.close()
    }
}
