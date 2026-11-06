package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Insets as AndroidInsets
import android.view.WindowInsets
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.hasText
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.unit.dp
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

/** Production Root overlay with explicitly injected system insets; not a hardware cutout emulator. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-notnight-xxhdpi", fontScale = 1.0f)
class ProductionMoreInsetsEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val harness by lazy { ProductionEvidenceHarness(rule) }
    private lateinit var api: MockEvidenceApi
    private lateinit var f: ProductionEvidenceFixtures
    private lateinit var root: click.cowa.linbit.app.AppViewModel

    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    @Before fun mainDispatcher() { Dispatchers.setMain(UnconfinedTestDispatcher()); Assume.assumeTrue(System.getProperty("production.evidence") == "1") }
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    @After fun close() { Dispatchers.resetMain() }
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
    private fun await(text: String) {
        try { rule.waitUntil(10_000) { rule.onAllNodes(androidx.compose.ui.test.hasText(text, substring = true), useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() } }
        catch (e: Throwable) { throw AssertionError("await=$text requests=${api.dumpRequests()}", e) }
    }
    private fun forceInsets(topPx: Int, bottomPx: Int) {
        val content = rule.runOnUiThread { rule.activity.findViewById<android.view.View>(android.R.id.content) }
        val frame = WindowInsets.Builder()
            .setInsets(WindowInsets.Type.statusBars(), AndroidInsets.of(0, topPx, 0, 0))
            .setInsets(WindowInsets.Type.displayCutout(), AndroidInsets.of(0, topPx, 0, 0))
            .setInsets(WindowInsets.Type.navigationBars(), AndroidInsets.of(0, 0, 0, bottomPx))
            .setVisible(WindowInsets.Type.statusBars() or WindowInsets.Type.navigationBars(), true).build()
        var observed = false
        val listener = androidx.core.view.OnApplyWindowInsetsListener { v, insets ->
            val top = insets.getInsets(androidx.core.view.WindowInsetsCompat.Type.statusBars() or androidx.core.view.WindowInsetsCompat.Type.displayCutout()).top
            val bottom = insets.getInsets(androidx.core.view.WindowInsetsCompat.Type.navigationBars()).bottom
            if (top == topPx && bottom == bottomPx) observed = true
            v.onApplyWindowInsets(insets.toWindowInsets())
            insets
        }
        val received = rule.runOnUiThread {
            androidx.core.view.ViewCompat.setOnApplyWindowInsetsListener(content, listener)
            androidx.core.view.ViewCompat.dispatchApplyWindowInsets(content, WindowInsetsCompat.toWindowInsetsCompat(frame))
            observed
        }
        rule.waitForIdle()
        Assert.assertTrue("Real dispatch must be observed with top=$topPx bottom=$bottomPx", received)
    }
    private fun assertSafe(topPx: Int, bottomPx: Int) {
        val density = rule.activity.resources.displayMetrics.density
        val top = topPx.dp / density
        val bottom = bottomPx.dp / density
        val title = rule.onNodeWithTag("more.title", useUnmergedTree = true).getBoundsInRoot()
        val back = rule.onNodeWithTag("more.back", useUnmergedTree = true).getBoundsInRoot()
        val list = rule.onNodeWithTag("more.list", useUnmergedTree = true).getBoundsInRoot()
        val rootHeight = rule.runOnIdle { rule.activity.findViewById<android.view.View>(android.R.id.content).height }.dp / density
        println("MORE_BOUNDS title=$title back=$back list=$list topPx=$topPx bottomPx=$bottomPx rootHeight=$rootHeight")
        Assert.assertTrue("title below unsafe top: $title", title.top >= top)
        Assert.assertTrue("back below unsafe top: $back", back.top >= top)
        Assert.assertTrue("back hit target is 48dp: $back", (back.bottom - back.top) >= 48.dp)
        Assert.assertTrue("list not under title: list=$list title=$title", list.top >= title.bottom)
        Assert.assertTrue("list bottom respects root nav: $list bottom=$bottom root=$rootHeight", list.bottom <= rootHeight - bottom)
        Assert.assertTrue("final list has visible area", list.bottom - list.top > 0.dp)
    }
    private fun case(label: String, width: CaseStyle, topPx: Int, bottomPx: Int, runBody: () -> Unit) = MockEvidenceApi().use { api = it; base()
        f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
        f.tokens.accessToken = "FAKE_MORE_INSETS_NEVER_REAL"
        val assets = f.assets()
        root = f.root()
        rule.setContent {
            CompositionLocalProvider(click.cowa.linbit.app.LocalRootHomeMarketFactory provides f.rootHomeMarket(), click.cowa.linbit.app.LocalRootProfileFactory provides { f.profile() }) {
                val loggedIn by f.tokens.loggedIn.collectAsStateWithLifecycle()
                Box { click.cowa.linbit.app.AppRootContent(root, f.badge(), click.cowa.linbit.app.AssetRouteViewModels(assets, f.spot(), f.tradFi()), loggedIn, false, {}, f.auth()) }
            }
        }
        await("首页"); forceInsets(topPx, bottomPx)
        if (width.dark) { root.setTheme(click.cowa.linbit.core.storage.ThemeMode.DARK); kotlinx.coroutines.runBlocking { root.themeMode.first { it == click.cowa.linbit.core.storage.ThemeMode.DARK } } }
        await("更多"); rule.onNodeWithText("更多", useUnmergedTree = true).performClick(); await("全部功能")
        assertSafe(topPx, bottomPx)
        val titleBounds = rule.onNodeWithTag("more.title", useUnmergedTree = true).getBoundsInRoot()
        val backBounds = rule.onNodeWithTag("more.back", useUnmergedTree = true).getBoundsInRoot()
        val listBounds = rule.onNodeWithTag("more.list", useUnmergedTree = true).getBoundsInRoot()
        java.io.File(System.getProperty("production.evidence.dir"), "more-insets-$label-layout.json").writeText("""{
          "productionEntry": "AppRootContent -> AuthenticatedAppContent -> Home -> moreMenu",
          "realRootClicks": true,
          "insetsDispatchPayloadPx": {"statusBarsAndDisplayCutoutTop": $topPx, "navigationBarsBottom": $bottomPx},
          "composeTitleBoundsDp": "$titleBounds", "composeBackBoundsDp": "$backBounds", "composeListBoundsDp": "$listBounds",
          "captureLimitation": "App content only. Robolectric rootWindowInsets getter remains zero/stale, so this file records the actually dispatched nonzero payload and Compose consumed bounds, not an actual system status bar or a hardware cutout. NOT_A_DEVICE_TEST."
        }""")
        harness.app("more-insets-$label")
        rule.onNodeWithTag("more.list").performScrollToNode(hasText("我的工单"))
        val last = rule.onNodeWithText("我的工单", useUnmergedTree = true).getBoundsInRoot()
        val navBottom = bottomPx.dp / rule.activity.resources.displayMetrics.density
        val rootHeight = with(rule.activity) { findViewById<android.view.View>(android.R.id.content).height }.dp / rule.activity.resources.displayMetrics.density
        Assert.assertTrue("Last row before scrolling beneath nav: $last root=$rootHeight nav=$navBottom", last.bottom <= rootHeight - navBottom)
        Assert.assertTrue("Scroll state consumed end", last.bottom > 0.dp)
        rule.onNodeWithTag("more.back").performClick(); rule.waitForIdle()
        rule.onNodeWithTag("more.back").assertDoesNotExist()
        rule.onNodeWithText("更多", useUnmergedTree = true).assertExists()
        Assert.assertTrue("Must remain the authenticated page", f.tokens.loggedIn.value)
        api.assertNoWrites(); Assert.assertFalse(api.requests.any { it.contains("REJECTED") })
        runBody()
    }
    private data class CaseStyle(val dark: Boolean)
    @Test @Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w320dp-h844dp-notnight-xxhdpi", fontScale = 1.3f)
    fun more320LargeFontTopStatusBottomNavAndBack() {
        System.setProperty("production.evidence.qualifier", "w=320;h=844;xxhdpi;densityDpi=480;night=no;fontScale=1.3")
        case("320-large", CaseStyle(false), 96, 64) {}
    }
    @Test @Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h844dp-night-xxhdpi", fontScale = 1.0f)
    fun more360DarkTopNotchAndGestureBottom() {
        System.setProperty("production.evidence.qualifier", "w=360;h=844;xxhdpi;densityDpi=480;night=yes;fontScale=1.0")
        case("360-dark", CaseStyle(true), 96, 48) {
            rule.activity.onBackPressedDispatcher.onBackPressed(); rule.waitForIdle()
        }
    }
}
