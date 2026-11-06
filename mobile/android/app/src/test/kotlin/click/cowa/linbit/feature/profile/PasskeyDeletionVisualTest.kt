package click.cowa.linbit.feature.profile

import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** Real production AlertDialog callback captured separately from host pixels; not a native credential dialog. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = android.app.Application::class, qualifiers = "zh-rCN-w320dp-h720dp-xhdpi")
class PasskeyDeletionVisualTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val tokens = InMemoryTokenStore()
    private lateinit var server: click.cowa.linbit.ui.evidence.MockEvidenceApi
    private lateinit var vm: SecurityViewModel
    private var dialog: android.view.Window? = null
    @Before fun setup() { Dispatchers.setMain(UnconfinedTestDispatcher()); tokens.accessToken = "INVALID_LAYOUT_ONLY" }
    private fun render(width: Int, font: Float, dark: Boolean, name: String) {
        rule.runOnUiThread { val res = rule.activity.resources; val cfg = android.content.res.Configuration(res.configuration)
            cfg.screenWidthDp = width; cfg.fontScale = font
            @Suppress("DEPRECATION") res.updateConfiguration(cfg, res.displayMetrics) }
        server = click.cowa.linbit.ui.evidence.MockEvidenceApi()
        server.route("GET", "/account/passkeys", """{"enabled":true,"items":[{"id":"review-loopback","name":"演示 Android","createdAt":"2026-11-06T00:00:00Z"}]}""")
        val api = NetworkFactory.api(okhttp3.OkHttpClient(), server.baseUrl)
        vm = SecurityViewModel(api, ToastCenter(CoroutineScope(Dispatchers.Main)), tokens, PasskeyRegistrationService(api) { _, _ -> error("Delete has no native credential creation") })
        rule.setContent { LinbitTheme(dark) { click.cowa.linbit.feature.profile.SecurityHost(onBack = {}, vm = vm, featureVm = click.cowa.linbit.feature.profile.SecurityFeatureViewModel(api, ToastCenter(CoroutineScope(Dispatchers.Main))), onDialogWindow = { dialog = it }) } }
        vm.seedEvidenceSecurity(click.cowa.linbit.core.generated.ApiSecurityOverview())
        rule.onNodeWithText("通行密钥").performClick()
        rule.waitUntil(5_000) { rule.onAllNodesWithTag("security.passkey.delete.review-loopback").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithTag("security.passkey.delete.review-loopback").performClick()
        rule.onNodeWithText("删除通行密钥？").assertExists()
        rule.onNodeWithText("删除后「演示 Android」不能再用于登录此账户。该操作不会改变密码或其他登录方式。请确认后再删除。").assertExists()
        rule.onNodeWithText("本步骤是前端最终复核；当前服务端 DELETE 契约不提供二次验证码校验。").assertExists()
        captureDialog(name)
        if (name.contains("dark")) captureHost("passkey-delete-host-360-dark")
        rule.onNodeWithTag("security.passkey.delete.cancel").performClick()
        rule.onAllNodesWithText("删除通行密钥？").assertCountEquals(0)
        assertEquals(0, server.requests.count { it.startsWith("DELETE") })
    }
    @Test fun narrowLargeFontDeleteReviewAndCancel() = render(320, 1.35f, false, "passkey-delete-dialog-320-large")
    @Test fun darkDeleteReviewAndCancel() = render(360, 1f, true, "passkey-delete-dialog-360-dark")
    private fun captureDialog(name: String) {
        rule.waitUntil(5_000) { dialog?.decorView?.isShown == true && (dialog?.decorView?.width ?: 0) > 0 }
        checkNotNull(dialog) { "Production dialog window required; not replacing with inline content" }
        rule.runOnIdle { val view = dialog!!.decorView; check(view.isShown && view.width > 0)
            val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888); view.draw(Canvas(bitmap))
            File("build/reports/passkey-shots", "$name.png").also { it.parentFile.mkdirs() }.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) } }
    }
    private fun captureHost(name: String) = rule.runOnIdle { val view = rule.activity.findViewById<android.view.View>(android.R.id.content)
        val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888); view.draw(Canvas(bitmap))
        File("build/reports/passkey-shots", "$name.png").also { it.parentFile.mkdirs() }.outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) } }
    @After fun close() { server.shutdown(); Dispatchers.resetMain() }
}
