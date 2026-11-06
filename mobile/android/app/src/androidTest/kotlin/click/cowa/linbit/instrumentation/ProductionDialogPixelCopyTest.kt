package click.cowa.linbit.instrumentation

import android.graphics.Bitmap
import android.view.PixelCopy
import android.view.Window
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.unit.dp
import androidx.test.platform.app.InstrumentationRegistry
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.data.assets.AssetsRoute
import click.cowa.linbit.feature.assets.AssetsHost
import click.cowa.linbit.feature.assets.AssetsViewModel
import click.cowa.linbit.feature.auth.AuthFlow
import click.cowa.linbit.feature.auth.AuthViewModel
import click.cowa.linbit.feature.profile.SecurityFeatureViewModel
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.feature.profile.SecurityViewModel
import click.cowa.linbit.feature.trade.ConvertSubpage
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test

/** NOT RUN until a device is supplied. Every capture is a real ModalBottomSheet Window via PixelCopy. */
class ProductionDialogPixelCopyTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(Dispatchers.Main)
    private lateinit var server: MockWebServer
    private val responses = linkedMapOf<String, String>()
    private val requests = mutableListOf<String>()

    @Before fun startLoopback() {
        server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val key = "${request.method} ${request.path}"
                val path = request.path?.substringBefore('?').orEmpty()
                assertFalse("No financial/2FA/KYC final mutation", path in setOf("/wallet/withdraw", "/wallet/internal/transfer", "/convert/accept", "/account/password/change", "/account/anti-phishing", "/2fa/verify", "/2fa/enable", "/2fa/disable", "/account/email", "/account/phone", "/account/fund-password", "/account/code/send", "/account/contact/code", "/earn/products/invalid/subscribe", "/earn/products/invalid/redeem", "/strategy/grid"))
                requests += key
                return MockResponse().setHeader("Content-Type", "application/json")
                    .also { if (responses.remove(key)?.let(it::setBody) == null) throw AssertionError("Unmapped route: $key") }
            }
        }
        server.start()
    }
    @After fun shutdown() { server.shutdown(); scope.coroutineContext[kotlinx.coroutines.Job]?.children?.forEach { it.cancel() } }

    private fun get(path: String, body: String) { responses["GET $path"] = body }
    private fun post(path: String, body: String) { responses["POST $path"] = body }
    private fun createApi() = NetworkFactory.api(OkHttpClient(), server.url("/").newBuilder().host("127.0.0.1").build().toString())
    private val toast by lazy { ToastCenter(scope) }
    private val api: click.cowa.linbit.core.network.LinbitApi by lazy { createApi() }

    @Test fun loginHelpRealDialogWindow() {
        post("/auth/login", """{"challengeToken":"INVALID_VISUAL_TOKEN","methods":["email"],"expiresIn":120}""")
        val ctx = InstrumentationRegistry.getInstrumentation().targetContext
        val tokens = InMemoryTokenStore()
        val vm = AuthViewModel(AuthRepository(api, tokens, { "evidence-invalid-device" }, { "instrumentation layout" }, scope), tokens, AppPrefs(ctx), toast)
        var window: Window? = null
        rule.setContent { LinbitTheme(false) { AuthFlow(false, {}, vm, onDialogWindow = { window = it }) } }
        rule.onNodeWithTag("auth.account").performTextReplacement("audit@example.invalid")
        rule.onNodeWithTag("auth.login.next").performClick()
        rule.onNodeWithTag("auth.password").performTextReplacement("INVALID_TEST1")
        rule.onNodeWithText("下一步").performClick()
        waitText("收不到验证码?"); rule.onNodeWithText("收不到验证码?").performClick()
        waitText("验证码 10 分钟内有效，60 秒后可重新发送", substring = true)
        capture("auth-login-help-dialog", window)
        check(tokens.accessToken == null)
    }

    @Test fun changePasswordReviewRealDialogWindow() = securityReview("password")
    @Test fun antiPhishingReviewRealDialogWindow() = securityReview("anti-phishing")
    @Test fun emailReviewRealDialogWindow() = contactReview(false)
    @Test fun phoneReviewRealDialogWindow() = contactReview(true)
    @Test fun fundPasswordReviewRealDialogWindow() = fundReview()

    private fun contactReview(phone: Boolean) {
        get("/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"email":"old@example.invalid","phone":"10000000000"}""")
        val vm = SecurityViewModel(api, toast); val feature = SecurityFeatureViewModel(api, toast); var window: Window? = null
        rule.setContent { LinbitTheme(false) { SecurityHost({}, vm, feature, onDialogWindow = { window = it }) } }
        waitText(if (phone) "手机" else "邮箱"); rule.onNodeWithText(if (phone) "手机" else "邮箱").performClick()
        rule.onNodeWithTag("security.contact.value").performTextReplacement(if (phone) "10000000000" else "next@example.invalid")
        rule.onNodeWithText("核对并继续").performClick(); waitText("新联系方式验证码")
        capture("security-${if (phone) "phone" else "email"}-confirm", window)
        assertFalse(requests.any { it != "GET /account/security" })
    }

    private fun fundReview() {
        get("/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"fundPasswordSet":false}""")
        val vm = SecurityViewModel(api, toast); val feature = SecurityFeatureViewModel(api, toast); var window: Window? = null
        rule.setContent { LinbitTheme(false) { SecurityHost({}, vm, feature, onDialogWindow = { window = it }) } }
        waitText("资金密码"); rule.onNodeWithText("资金密码").performClick()
        rule.onNodeWithTag("security.fund.value").performTextReplacement("000000")
        rule.onNodeWithTag("security.fund.confirm").performTextReplacement("000000")
        rule.onNodeWithText("核对并继续").performClick(); waitText("确认资金密码")
        capture("security-fund-confirm", window)
        assertFalse(requests.any { it != "GET /account/security" })
    }

    private fun securityReview(kind: String) {
        get("/account/security", """{"twoFactor":{"enabled":true,"method":"totp"},"antiPhishingSet":false}""")
        var window: Window? = null
        val vm = SecurityViewModel(api, toast)
        val feature = SecurityFeatureViewModel(api, toast)
        rule.setContent { LinbitTheme(false) { SecurityHost({}, vm, feature, onDialogWindow = { window = it }) } }
        if (kind == "password") {
            waitText("登录密码"); rule.onNodeWithText("登录密码").performClick()
            rule.onNodeWithTag("security.password.old").performTextReplacement("INVALID_OLD_TEST1")
            rule.onNodeWithTag("security.password.new").performTextReplacement("INVALID_NEW_TEST2")
            rule.onNodeWithTag("security.password.confirm").performTextReplacement("INVALID_NEW_TEST2")
            rule.onNodeWithText("下一步").performClick(); waitText("修改登录密码会注销其他设备会话。请核对后完成安全验证。", substring = true)
        } else {
            waitText("防钓鱼码"); rule.onNodeWithText("防钓鱼码").performClick()
            rule.onNodeWithTag("security.antiphishing.value").performTextReplacement("INVALIDTESTONLY")
            rule.onNodeWithText("核对并设置").performClick(); waitText("保存后请勿向他人泄露动态验证码", substring = true)
        }
        capture("security-$kind-confirm", window)
        assertFalse(requests.any { it != "GET /account/security" })
    }

    @Test fun convertFinalReviewRealDialogWindow() {
        get("/convert/pairs", """{"pairs":[{"from":"USDT","to":"BTC","paused":false,"route":"direct"}]}""")
        post("/convert/quote", """{"quoteId":"INVALID-QUOTE-VISUAL-ONLY","from":"USDT","to":"BTC","fromAmount":"1","toAmount":"0.00001","rate":"0.00001","expiresAt":${System.currentTimeMillis() + 120_000},"ttlSeconds":120}""")
        val tokens = InMemoryTokenStore().also { it.accessToken = "INVALID_LAYOUT_TOKEN" }
        var window: Window? = null
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        rule.setContent { LinbitTheme(false) { ConvertSubpage(0.dp, {}, vm, onDialogWindow = { window = it }) } }
        waitText("兑出 USDT"); rule.onNodeWithTag("convert.amount").performTextReplacement("1")
        rule.onNodeWithText("获取报价").performClick(); waitText("预计到账")
        rule.onNodeWithText("确认闪兑").performClick(); waitText("兑换率")
        capture("convert-confirm", window)
        assertFalse(requests.any { it.startsWith("POST /convert/accept") })
    }

    @Test fun withdrawFinalReviewRealDialogWindow() {
        get("/wallet/withdraw/networks?asset=USDT", """{"asset":"USDT","networks":[{"network":"TRC20","withdrawEnabled":true,"memoRequired":false,"minWithdraw":"1","maxWithdraw":"200","gasFeeEstimate":"0.1","gasFeeAsset":"USDT","estimatedArrivalMinutes":3}]}""")
        get("/wallet/networks?asset=USDT", """{"networks":[{"asset":"USDT","network":"TRC20","depositEnabled":true,"withdrawEnabled":true,"minWithdraw":"1","maxWithdraw":"200","withdrawFee":"0.1","withdrawFeeAsset":"USDT","estimatedArrivalMinutes":3}]}""")
        val balance = """{"items":[{"asset":"USDT","free":"100.00","locked":"0.00","total":"100.00"}],"page":1,"pageSize":200,"total":1}"""
        get("/assets/balances?account=spot&pageSize=200", balance)
        get("/assets/balances?account=funding&pageSize=200", balance)
        get("/futures/account", "{}"); get("/account/security", """{"twoFactor":{"enabled":true,"method":"totp"}}""")
        val tokens = InMemoryTokenStore().also { it.accessToken = "INVALID_LAYOUT_TOKEN" }
        val http = OkHttpClient()
        val socket = MarketSocket(http, "ws://127.0.0.1/", "evidence", scope, accessToken = { null })
        val vm = AssetsViewModel(api, tokens, MarketStore(api, socket, scope), toast)
        val nav = AssetsNav().also { it.open(AssetsRoute.Withdraw("USDT")) }
        var window: Window? = null
        rule.setContent { LinbitTheme(false) { AssetsHost(nav, vm, onDialogWindow = { window = it }) } }
        waitText("Tron (TRC20)"); waitText("100", substring = true)
        rule.onNodeWithTag("withdraw.address").performTextReplacement("INVALID-ONCHAIN-ADDRESS-NOT-FOR-TRANSFER")
        rule.onNodeWithTag("withdraw.amount").performTextReplacement("1")
        rule.onNodeWithTag("withdraw.review").performClick(); waitText("谷歌验证器动态码")
        capture("withdraw-before-confirm", window)
        assertFalse(requests.any { it.startsWith("POST /wallet/withdraw") || it.startsWith("POST /account/code/send") })
    }

    private fun waitText(text: String, substring: Boolean = false) {
        rule.waitUntil(10_000) { runCatching { rule.onAllNodesWithText(text, substring = substring, useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }.getOrDefault(false) }
    }

    private fun capture(name: String, supplied: Window?) {
        val window = requireNotNull(supplied) { "No independent production Dialog Window" }
        val dialog = window.decorView
        assertTrue("Cannot substitute Activity", window !== rule.activity.window && dialog.isShown && dialog.width > 0 && dialog.height > 0)
        val bitmap = Bitmap.createBitmap(dialog.width, dialog.height, Bitmap.Config.ARGB_8888)
        val done = CountDownLatch(1)
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            PixelCopy.request(window, bitmap, { result ->
                assertTrue("PixelCopy result=$result", result == PixelCopy.SUCCESS)
                val dir = File(InstrumentationRegistry.getInstrumentation().targetContext.getExternalFilesDir(android.os.Environment.DIRECTORY_PICTURES), "production-evidence").apply { mkdirs() }
                File(dir, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
                File(dir, "$name.json").writeText(org.json.JSONObject().put("windowCapture", "PixelCopy").put("width", bitmap.width).put("height", bitmap.height).put("requests", requests).put("deviceVisualVerdict", "PENDING_HUMAN_READ").toString(2))
                done.countDown()
            }, android.os.Handler(android.os.Looper.getMainLooper()))
        }
        assertTrue("PixelCopy timeout", done.await(10, TimeUnit.SECONDS))
        InstrumentationRegistry.getInstrumentation().waitForIdleSync()
    }
}
