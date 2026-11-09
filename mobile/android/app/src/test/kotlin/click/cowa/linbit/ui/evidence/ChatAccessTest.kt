package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import click.cowa.linbit.feature.markets.*
import click.cowa.linbit.core.generated.ApiChatRoom
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import kotlinx.coroutines.*
import kotlinx.coroutines.test.*
import org.junit.*
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.*
import java.io.File

/** Only loopback fixtures. Never touches a device session or sends a message. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class ChatAccessTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private lateinit var api: MockEvidenceApi
    private lateinit var f: ProductionEvidenceFixtures
    @Before fun start() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        api = MockEvidenceApi(); f = ProductionEvidenceFixtures(api, CoroutineScope(Dispatchers.Main))
    }
    private var localAuthScenario = false
    @After fun stop() {
        if (localAuthScenario) Assert.assertEquals(listOf("/auth/login"), api.mutationRequests.map { it.path })
        else api.assertNoWrites()
        api.close(); Dispatchers.resetMain()
    }
    private val room = ApiChatRoom("general", ApiChatRoom.Type.GENERAL, "测试群组（仅本地）", 0)

    @Test fun guestDirectActionsNeverLoadOrSend() {
        val vm = f.chat()
        vm.loadRooms(); vm.open(room); vm.older(); vm.send("LOCAL TEST ONLY") { error("Guest cannot send") }
        Assert.assertNull(vm.rooms); Assert.assertNull(vm.room); Assert.assertTrue(vm.messages.isEmpty())
        Assert.assertTrue(api.requests.isEmpty())
    }

    @Test fun directTabAlsoChecksRealSession() {
        val vm = f.chat()
        var login = false
        rule.setContent { LinbitTheme(false) { ChatRoomTab(true, { login = true }, vm = vm) } }
        rule.onNodeWithText("登录后查看聊天记录").assertIsDisplayed()
        rule.onNodeWithText("立即登录").performClick()
        Assert.assertTrue(login); Assert.assertTrue(api.requests.isEmpty())
    }

    @Test fun guestMarketGateEvidence() {
        var login = false
        rule.activity.actionBar?.hide()
        rule.setContent { LinbitTheme(false) {
            Box(Modifier.fillMaxSize().background(LB.p.bg)) {
                MarketsContent(click.cowa.linbit.data.MarketState(), emptyList(), 84.dp, {}, {},
                    modifier = Modifier.padding(top = 24.dp), initialTop = 2, loggedIn = false, onLogin = { login = true })
                FloatingTabBar(MainTab.MARKETS, {}, Modifier.align(Alignment.BottomCenter))
            }
        } }
        rule.onNodeWithText("登录后查看聊天记录").assertIsDisplayed()
        rule.onNodeWithText("搜索群组名称").assertDoesNotExist()
        rule.waitForIdle()
        System.getProperty("production.evidence.dir")?.let { path -> rule.runOnIdle {
            val v = rule.activity.window.decorView
            val b = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
            v.draw(Canvas(b)); File(path).mkdirs()
            File(path, "mock-chat-login-gate.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG, 100, it) }; b.recycle()
        } }
        rule.onNodeWithText("立即登录").performClick()
        Assert.assertTrue(login); Assert.assertTrue(api.requests.isEmpty())
    }

    private fun captureChat(name: String) {
        rule.waitForIdle()
        System.getProperty("production.evidence.dir")?.let { path -> rule.runOnIdle {
            val v = rule.activity.window.decorView
            val b = Bitmap.createBitmap(v.width, v.height, Bitmap.Config.ARGB_8888)
            v.draw(Canvas(b)); File(path).mkdirs()
            File(path, "mock-$name.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG, 100, it) }; b.recycle()
        } }
    }

    @Test fun authenticatedListAndRoomEvidenceNoDuplicateHeaderOrDraftLeak() {
        f.tokens.accessToken = "INVALID_LOCAL_LAYOUT_ONLY"
        api.route("GET", "/chat/rooms", """{"rooms":[{"id":"general","type":"general","name":"本地测试群组 A","online":0},{"id":"spot.BTCUSDT","type":"spot","name":"本地测试群组 B","online":0,"symbol":"BTCUSDT"}]}""")
        api.route("GET", "/chat/rooms/general/messages?limit=50", """{"items":[{"id":1,"room":"general","sender":"本地测试用户","text":"仅本地测试消息，用于验证阅读布局。","createdAt":"2026-01-01T00:00:00Z"}],"hasMore":false}""")
        api.route("GET", "/chat/rooms/spot.BTCUSDT/messages?limit=50", """{"items":[],"hasMore":false}""")
        val vm = f.chat()
        var overlay by mutableStateOf(false)
        rule.activity.actionBar?.hide()
        rule.setContent { CompositionLocalProvider(LocalChatViewModelFactory provides { vm }) { LinbitTheme(false) {
            Box(Modifier.fillMaxSize().background(LB.p.bg)) {
                MarketsContent(click.cowa.linbit.data.MarketState(), emptyList(), 84.dp, {}, {},
                    modifier = Modifier.padding(top = 24.dp), initialTop = 2, loggedIn = true, onChatOverlay = { overlay = it })
                if (!overlay) FloatingTabBar(MainTab.MARKETS, {}, Modifier.align(Alignment.BottomCenter))
            }
        } } }
        rule.waitUntil(8000) { vm.rooms != null }
        captureChat("chat-auth-list")
        rule.onNodeWithText("本地测试群组 A").performClick()
        rule.waitUntil(8000) { !vm.messagesLoading }
        rule.onNodeWithText("仅本地测试消息，用于验证阅读布局。").assertIsDisplayed()
        rule.onNodeWithText("自选").assertDoesNotExist()
        rule.onNodeWithText("市场").assertDoesNotExist()
        rule.onNodeWithText("行情").assertDoesNotExist()
        rule.onNodeWithText("发送").assertIsNotEnabled()
        captureChat("chat-auth-room")
        rule.onNode(hasSetTextAction()).performTextReplacement("不发送的本地草稿")
        rule.activity.onBackPressedDispatcher.onBackPressed(); rule.waitForIdle()
        rule.onNodeWithText("本地测试群组 B").performClick()
        rule.waitUntil(8000) { !vm.messagesLoading }
        rule.onNodeWithText("不发送的本地草稿").assertDoesNotExist()
        rule.onNodeWithText("说点什么…").assertIsDisplayed()
        Assert.assertTrue(api.mutationRequests.isEmpty())
    }

    @Test fun authenticatedReadAndSendUseOnlyInProcessFake() {
        f.tokens.accessToken = "INVALID_IN_PROCESS_SESSION"
        var posts = 0
        var completed = false
        val history = click.cowa.linbit.core.generated.ApiChatMessage(1L, "general", "本地测试", "仅内存历史", "2026-01-01T00:00:00Z")
        val fake = java.lang.reflect.Proxy.newProxyInstance(
            click.cowa.linbit.core.network.LinbitApi::class.java.classLoader,
            arrayOf(click.cowa.linbit.core.network.LinbitApi::class.java),
        ) { _, method, args ->
            when (method.name) {
                "chatMessages" -> click.cowa.linbit.core.generated.ApiListChatMessages200Response(listOf(history), false)
                "postChatMessage" -> {
                    posts++
                    val request = args!![1] as click.cowa.linbit.core.generated.ApiPostChatMessageRequest
                    Assert.assertEquals("仅内存发送测试", request.text)
                    java.util.UUID.fromString(request.clientId)
                    history.copy(id = 2L, text = request.text, mine = true)
                }
                else -> error("Unexpected local API call: ${method.name}")
            }
        } as click.cowa.linbit.core.network.LinbitApi
        val vm = f.chat(fake)
        vm.open(room)
        Assert.assertEquals(listOf(history), vm.messages)
        vm.send("  仅内存发送测试  ") { completed = true }
        Assert.assertTrue(completed); Assert.assertEquals(1, posts)
        Assert.assertEquals(2, vm.messages.size); Assert.assertTrue(vm.isMine(vm.messages.last()))
        vm.send("   ") { error("Blank message must not send") }
        Assert.assertEquals(1, posts)
        f.tokens.clear()
        vm.send("仅内存拒绝测试") { error("Expired session must not send") }
        Assert.assertEquals(1, posts); Assert.assertTrue(vm.messages.isEmpty())
        Assert.assertTrue(api.requests.isEmpty())
    }

    @Test fun marketTabLoginReturnsToChatAcrossRootSessionKey() {
        localAuthScenario = true
        repeat(6) {
            listOf("/home/newcomer", "/growth/config", "/assets/overview", "/notifications/unread-count", "/market/usd-rate", "/tradfi/assets",
                "/home/placements?slots=popup%2Ccarousel%2Csearch_hint&platform=app&locale=zh-CN&appVersion=0.3.0",
                "/academy/articles?locale=zh-CN&limit=5", "/market/fiat-rates?base=USD").forEach { path -> api.route("GET", path, "{}") }
            listOf("/market/symbols", "/market/ticker", "/market/categories").forEach { path -> api.route("GET", path, "[]") }
            api.route("GET", "/futures/market/ticker", """{"tickers":[]}""")
            api.route("GET", "/earn/products", """{"items":[]}""")
        }
        val root = f.root(); val badge = f.badge()
        val routes = click.cowa.linbit.app.AssetRouteViewModels(f.assets(false), f.spot(), f.tradFi())
        val auth = f.auth(); val chat = f.chat(); val profile = f.profile(); val homeMarkets = f.lazyRootHomeMarket()
        api.allowAuthRootMocks = true
        api.route("POST", "/auth/login", """{"accessToken":"INVALID_LOCAL_CHAT_LOGIN","refreshToken":"INVALID_LOCAL_CHAT_REFRESH","tokenType":"Bearer","expiresIn":3600}""")
        api.route("GET", "/chat/rooms", """{"rooms":[]}""")
        rule.setContent {
            CompositionLocalProvider(
                click.cowa.linbit.app.LocalRootHomeMarketFactory provides homeMarkets,
                LocalChatViewModelFactory provides { chat },
                click.cowa.linbit.app.LocalRootProfileFactory provides { profile },
            ) {
                val loggedIn by f.tokens.loggedIn.collectAsState()
                click.cowa.linbit.app.AppRootContent(root, badge, routes, loggedIn, false, {}, auth)
            }
        }
        rule.onNodeWithText("行情").performClick()
        rule.onAllNodesWithText("聊天室").onFirst().performClick()
        rule.onNodeWithText("登录后查看聊天记录").assertIsDisplayed()
        // Snapshot reads: MockWebServer may still append on dispatcher threads.
        Assert.assertTrue(api.requests.toList().none { it.contains("/chat/") })
        rule.onNodeWithText("立即登录").performClick()
        rule.onNodeWithTag("auth.account", useUnmergedTree = true).performTextReplacement("chat@example.invalid")
        rule.onNodeWithTag("auth.login.next", useUnmergedTree = true).performClick()
        rule.onNodeWithTag("auth.password", useUnmergedTree = true).performTextReplacement("Local-Test-Only-Password123")
        rule.onNodeWithText("下一步", useUnmergedTree = true).performClick()
        rule.waitUntil(8000) { api.requests.toList().any { it == "GET /chat/rooms" } }
        rule.onNodeWithText("搜索群组名称").assertIsDisplayed()
        rule.onNodeWithText("登录后查看聊天记录").assertDoesNotExist()
        Assert.assertEquals(1, api.requests.toList().count { it == "GET /chat/rooms" })
    }

    @Test fun sessionLossClearsPrivateRoomsAndMessages() {
        f.tokens.accessToken = "INVALID_LOCAL_TEST_ONLY"
        api.route("GET", "/chat/rooms", """{"rooms":[{"id":"general","type":"general","name":"测试群组（仅本地）","online":0}]}""")
        val vm = f.chat()
        rule.setContent { LinbitTheme(false) { ChatRoomTab(true, {}, vm = vm) } }
        rule.waitUntil(8000) { vm.rooms != null }
        rule.onNodeWithText("测试群组（仅本地）").assertIsDisplayed()
        rule.runOnIdle { f.tokens.clear() }
        rule.onNodeWithText("登录后查看聊天记录").assertIsDisplayed()
        Assert.assertNull(vm.rooms); Assert.assertNull(vm.room); Assert.assertTrue(vm.messages.isEmpty())
        Assert.assertEquals(listOf("GET /chat/rooms"), api.requests.toList())
    }
}
