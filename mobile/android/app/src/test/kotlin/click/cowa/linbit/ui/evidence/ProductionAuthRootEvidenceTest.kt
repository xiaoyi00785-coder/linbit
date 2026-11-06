package click.cowa.linbit.ui.evidence

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.test.*
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The actual AppRoot gate and AuthFlow with concrete VMs against an explicitly-local API. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class ProductionAuthRootEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private lateinit var f: ProductionEvidenceFixtures
    private lateinit var root: click.cowa.linbit.app.AppViewModel
    private val tokens get() = f.tokens

    @Before fun setUp() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        Assume.assumeTrue(System.getProperty("production.evidence") == "1")
    }
    @After fun tearDown() { Dispatchers.resetMain() }
    private fun await(text: String) {
        try { rule.waitUntil(8_000) { rule.onAllNodes(hasText(text, substring = true), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() } }
        catch (e: Throwable) { throw AssertionError("await $text requests=${api.dumpRequests()}", e) }
    }
    private fun render(existing: Boolean = false) {
        api.allowAuthRootMocks = true
        if (existing) tokens.accessToken = "FAKE_EXISTING_TOKEN_NEVER_REAL"
        root = f.root()
        val assets = f.assets(seedSession = existing)
        // Exact production root gate. Inject concrete VMs, never start AuthFlow/Home directly.
        rule.setContent {
            CompositionLocalProvider(
                click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.lazyRootHomeMarket(),
                click.cowa.linbit.app.LocalRootProfileFactory provides { f.profile() },
            ) {
                val loggedIn by tokens.loggedIn.collectAsStateWithLifecycle()
                click.cowa.linbit.app.AppRootContent(root, f.badge(), click.cowa.linbit.app.AssetRouteViewModels(assets, f.spot(), f.tradFi()), loggedIn, false, {}, f.auth())
            }
        }
    }
    private fun base() {
        repeat(4) {
            api.route("GET", "/assets/balances?account=spot&pageSize=200", """{"items":[]}""")
            api.route("GET", "/assets/balances?account=funding&pageSize=200", """{"items":[]}""")
            api.route("GET", "/futures/account", "{}")
            api.route("GET", "/assets/overview", """{"totalValueUsdt":"0"}"""); api.route("GET", "/home/newcomer", "{}")
            api.route("GET", "/market/symbols", "[]"); api.route("GET", "/market/ticker", "[]")
            api.route("GET", "/futures/market/ticker", """{"tickers":[]}"""); api.route("GET", "/market/categories", "[]")
            api.route("GET", "/market/fiat-rates?base=USDT", """{"base":"USDT","rates":{}}""")
            api.route("GET", "/notifications/unread-count", """{"unread":0}"""); earnRoutes(api, false)
        }
    }
    private fun assertUnauthenticated() {
        await("立即登录，每日登录领红包")
        rule.onNodeWithText("首页", useUnmergedTree = true).assertDoesNotExist()
        rule.onNodeWithText("用户中心", useUnmergedTree = true).assertDoesNotExist()
        Assert.assertTrue("requests=${api.dumpRequests()}", api.requests.none { it.contains("REJECTED") })
    }

    private fun login() {
        api.route("POST", "/auth/login", """{"accessToken":"FAKE_LOGIN_ACCESS_NEVER_REAL","refreshToken":"FAKE_LOGIN_REFRESH_NEVER_REAL","tokenType":"Bearer","expiresIn":3600}""")
        rule.onNodeWithTag("auth.account", useUnmergedTree = true).performTextReplacement("fake@example.invalid")
        rule.onNodeWithTag("auth.login.next", useUnmergedTree = true).performClick(); await("请输入密码")
        rule.onNodeWithTag("auth.password", useUnmergedTree = true).performTextReplacement("Fake-Local-Password-Not-Real-123")
        rule.onNodeWithText("下一步", useUnmergedTree = true).performClick(); await("首页")
        Assert.assertEquals("FAKE_LOGIN_ACCESS_NEVER_REAL", tokens.accessToken)
    }

    @Test fun coldStartLoginRegisterPasskeyAndBackCannotEnterHome() {
        MockEvidenceApi().use { api = it
            f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); render()
            assertUnauthenticated()
            rule.onNodeWithText("使用通行密钥继续", useUnmergedTree = true).assertIsDisplayed()
            rule.onNodeWithText("创建账户", useUnmergedTree = true).performClick(); await("已有账户，立即登录")
            rule.onNodeWithText("已有账户，立即登录", useUnmergedTree = true).performClick(); assertUnauthenticated()
            rule.activity.onBackPressedDispatcher.onBackPressed(); rule.waitForIdle()
            rule.onNodeWithText("首页", useUnmergedTree = true).assertDoesNotExist()
            Assert.assertTrue(rule.activity.isFinishing); api.assertNoWrites()
        }
    }

    @Test fun loginSuccessAndLogoutReturnToRootAuth() {
        MockEvidenceApi().use { api = it; base()
            f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); render()
            assertUnauthenticated(); login()
            Assert.assertEquals(listOf("POST /auth/login"), api.requests.filter { !it.startsWith("GET ") }.map { it.substringBefore(" [") })
            // First registered content-desc-free avatar is the real Home -> UserCenter entry.
            rule.onAllNodes(androidx.compose.ui.test.hasClickAction(), useUnmergedTree = true)[0].performClick()
            await("退出当前账户")
            rule.onNodeWithText("退出当前账户", useUnmergedTree = true).performScrollTo().performClick()
            api.route("POST", "/auth/logout", """{"loggedOut":true}""")
            rule.onNodeWithText("退出", substring = false, useUnmergedTree = true).performClick()
            assertUnauthenticated()
            rule.activity.onBackPressedDispatcher.onBackPressed(); rule.waitForIdle()
            rule.onNodeWithText("首页", useUnmergedTree = true).assertDoesNotExist()
            Assert.assertEquals(1, api.requests.count { it.startsWith("POST /auth/logout") })
            Assert.assertTrue("requests=${api.dumpRequests()}", api.requests.none { it.contains("REJECTED") })
        }
    }

    @Test fun existingTokenAndSessionClearCannotBypassGate() {
        MockEvidenceApi().use { api = it; base()
            f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); render(existing = true)
            await("首页")
            tokens.clear(); assertUnauthenticated()
            rule.activity.onBackPressedDispatcher.onBackPressed(); rule.waitForIdle()
            rule.onNodeWithText("首页", useUnmergedTree = true).assertDoesNotExist()
            Assert.assertTrue(rule.activity.isFinishing)
        }
    }

    @Test @Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w320dp-h844dp-xxhdpi", fontScale = 1.3f)
    fun coldStart320LargeFont() {
        MockEvidenceApi().use { api = it; f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); render()
            System.setProperty("production.evidence.qualifier", "w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3"); assertUnauthenticated(); harness.app("authsession-coldstart-320-large"); api.assertNoWrites()
        }
    }

    @Test @Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-night-xxhdpi")
    fun coldStart360Dark() {
        MockEvidenceApi().use { api = it; f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main)); render()
            runBlockingTheme(); System.setProperty("production.evidence.qualifier", "w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0"); assertUnauthenticated(); harness.app("authsession-coldstart-360-dark"); api.assertNoWrites()
        }
    }
    private fun runBlockingTheme() {
        root.setTheme(click.cowa.linbit.core.storage.ThemeMode.DARK)
        kotlinx.coroutines.runBlocking { root.themeMode.first { it == click.cowa.linbit.core.storage.ThemeMode.DARK } }
        rule.waitForIdle()
    }
}

