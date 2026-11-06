package click.cowa.linbit.feature.profile

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.feature.auth.PasskeyLoginFailure
import click.cowa.linbit.feature.profile.SecurityFeatureViewModel
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.Assert.assertEquals
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** Production SecurityHost pixels with loopback data; NEVER a system Credential Manager window. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = android.app.Application::class, qualifiers = "zh-rCN-w320dp-h720dp-xhdpi")
class PasskeyRegistrationVisualTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val tokens = InMemoryTokenStore()
    private var server: click.cowa.linbit.ui.evidence.MockEvidenceApi? = null
    private lateinit var vm: SecurityViewModel
    private fun width(value: Int, font: Float = 1f) = rule.runOnUiThread {
        val res = rule.activity.resources; val cfg = android.content.res.Configuration(res.configuration)
        cfg.screenWidthDp = value; cfg.fontScale = font
        @Suppress("DEPRECATION") res.updateConfiguration(cfg, res.displayMetrics)
    }
    @Before fun setup() { Dispatchers.setMain(UnconfinedTestDispatcher()); tokens.accessToken = "INVALID_LAYOUT_ONLY" }
    @Test fun narrowLargeFontExistingListAndRealEntry() {
        width(320, 1.35f)
        render(false, list = true)
        rule.onNodeWithText("我已有的 Passkey").performScrollTo().assertIsDisplayed()
        rule.onNodeWithTag("security.passkey.register").assertIsDisplayed().assertIsEnabled()
        capture("passkey-register-320-large")
    }
    @Test fun darkNoTotpFailureRetainsCodeAndCanRetry() {
        width(360)
        render(true, list = false, fail = true)
        rule.onNodeWithTag("security.passkey.code").performScrollTo().performTextReplacement("654321")
        rule.onNodeWithTag("security.passkey.register").performClick()
        rule.waitUntil(5_000) { vm.state.value.passkeysError != null }
        rule.onNodeWithText(PasskeyLoginFailure.Unsupported.message!!, substring = true).performScrollTo().assertIsDisplayed()
        rule.onNodeWithTag("security.passkey.register").assertIsEnabled()
        assertEquals("654321", vm.state.value.passkeyCode)
        assertEquals(0, server!!.requests.count { it.contains("/finish") })
        capture("passkey-register-360-dark")
    }
    private fun render(dark: Boolean, list: Boolean, fail: Boolean = false) {
        val web = click.cowa.linbit.ui.evidence.MockEvidenceApi().also { server = it }
        web.route("GET", "/account/security", """{"twoFactor":{"enabled":false},"passkeysCount":1}""")
        web.route("GET", "/account/passkeys", if (list) """{"enabled":true,"items":[{"id":"loopback","name":"我已有的 Passkey","createdAt":"2026-11-06T00:00:00Z"}]}""" else """{"enabled":true,"items":[]}""")
        if (fail) web.route("POST", "/account/passkeys/register/begin", """{"sessionId":"INVALID_LOOPBACK","publicKey":{"challenge":"YQ"}}""")
        val api = NetworkFactory.api(okhttp3.OkHttpClient(), web.baseUrl)
        val bridge = PasskeyRegistrationBridge { _, _ -> throw PasskeyLoginFailure.Unsupported }
        val scope = CoroutineScope(Dispatchers.Main)
        vm = SecurityViewModel(api, ToastCenter(scope), tokens, PasskeyRegistrationService(api, bridge))
        rule.setContent { LinbitTheme(dark) { SecurityHost(onBack = {}, vm = vm, featureVm = SecurityFeatureViewModel(api, ToastCenter(scope))) } }
        rule.waitUntil(8_000) { rule.onAllNodesWithText("通行密钥").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("通行密钥", substring = false).performClick()
        rule.waitUntil(8_000) { rule.onAllNodesWithText("使用设备屏幕锁或支持的凭据提供程序添加通行密钥。添加时需要服务端安全验证。", substring = false).fetchSemanticsNodes().isNotEmpty() }
        rule.waitUntil(5_000) { vm.state.value.passkeysEnabled && rule.onAllNodesWithTag("security.passkey.register").fetchSemanticsNodes().isNotEmpty() }
    }
    private fun capture(name: String) = rule.runOnIdle {
        val view = rule.activity.findViewById<android.view.View>(android.R.id.content)
        val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888); view.draw(Canvas(bitmap))
        File("build/reports/passkey-shots", "$name.png").also { it.parentFile.mkdirs() }.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }
    @After fun close() { server?.close(); Dispatchers.resetMain() }
}
