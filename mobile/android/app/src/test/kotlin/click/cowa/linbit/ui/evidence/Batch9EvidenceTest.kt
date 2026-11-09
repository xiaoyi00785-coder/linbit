package click.cowa.linbit.ui.evidence

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.*
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.feature.assets.AssetsScreen
import click.cowa.linbit.feature.assets.AssetsViewModel
import click.cowa.linbit.feature.more.NotificationViewModel
import click.cowa.linbit.feature.more.NotificationsScreen
import click.cowa.linbit.feature.more.supportTicketId
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.mockito.kotlin.any
import org.mockito.kotlin.anyOrNull
import org.mockito.kotlin.doReturn
import org.mockito.kotlin.mock
import org.mockito.kotlin.never
import org.mockito.kotlin.verify
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File

/** 离屏夹具/非生产。不真实发送通知或创建工单。 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
@OptIn(ExperimentalCoroutinesApi::class)
class Batch9EvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Before fun start() {
        Dispatchers.setMain(UnconfinedTestDispatcher())
        rule.activity.actionBar?.hide()
    }

    @After fun stop() { Dispatchers.resetMain() }

    private fun capture(name: String) {
        rule.waitForIdle()
        System.getProperty("production.evidence.dir")?.let { path ->
            rule.runOnIdle {
                val v = rule.activity.window.decorView
                val b = Bitmap.createBitmap(v.width.coerceAtLeast(1), v.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
                Canvas(b).also { v.draw(it) }
                File(path).mkdirs()
                File(path, "mock-b9-$name.png").outputStream().use { b.compress(Bitmap.CompressFormat.PNG, 100, it) }
                b.recycle()
            }
        }
    }

    @Test fun notificationsSupportLinkShowsOpenTicketWithoutMarkingOnTap() {
        val linked = ApiNotification(
            id = "n-local-1",
            kind = ApiNotification.Kind.SYSTEM,
            title = "本地测试：工单有新回复",
            body = "本地夹具正文，不得从正文推断工单号。",
            read = false,
            time = 1_700_000_000_000L,
            link = ApiNotificationLink(ApiNotificationLink.Type.SUPPORT_TICKET, "1001"),
        )
        val plain = ApiNotification(
            id = "n-local-2",
            kind = ApiNotification.Kind.SYSTEM,
            title = "本地测试：系统公告",
            body = "旧通知无 link，只能展开，不能猜工单。",
            read = false,
            time = 1_700_000_100_000L,
        )
        assertEquals("1001", supportTicketId(linked))
        var opened: Pair<String, String>? = null
        val tokens = InMemoryTokenStore().apply { accessToken = "INVALID_LOCAL_ONLY" }
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
        val api = mock<LinbitApi> {
            onBlocking { notifications(anyOrNull(), any(), any()) } doReturn ApiNotificationList(
                items = listOf(linked, plain), total = 2, unread = 2,
            )
            onBlocking { readNotifications(any()) } doReturn ApiUpdatedCount(updated = 0)
        }
        val vm = NotificationViewModel(api, tokens, ToastCenter(scope))
        try {
            rule.setContent {
                LinbitTheme(false) {
                    NotificationsScreen(
                        onBack = {},
                        onLogin = {},
                        onOpenSupportTicket = { t, n -> opened = t to n },
                        vm = vm,
                    )
                }
            }
            rule.waitForIdle()
            rule.onNodeWithText("本地测试：工单有新回复").assertIsDisplayed()
            rule.onNodeWithText("查看工单").assertIsDisplayed()
            rule.onNodeWithText("本地测试：系统公告").assertIsDisplayed()
            capture("notifications-support-link")
            rule.onNodeWithText("查看工单").performClick()
            rule.waitForIdle()
            assertEquals("1001" to "n-local-1", opened)
            runBlocking { verify(api, never()).readNotifications(any()) }
        } finally {
            vm.viewModelScope.cancel()
            scope.cancel()
        }
    }

    @Test fun futuresAssetsCardShowsIndependentTrialAndDeductionDash() {
        val tokens = InMemoryTokenStore().apply { accessToken = "INVALID_LOCAL_ONLY" }
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
        val server = MockWebServer().also { it.start() }
        try {
            val bal = """{"items":[{"asset":"USDT","free":"50.00","locked":"0","total":"50.00"}],"page":1,"pageSize":200,"total":1}"""
            val futures = """{"marginBalance":"1000.00","availableBalance":"800.00","walletBalance":"1000.00","crossPositionMargin":"100","isolatedMargin":"0","crossUnrealizedPnl":"1.25","isolatedUnrealizedPnl":"0","orderMargin":"10"}"""
            val bonus = """{"available":{"trial":"12.34567890","deduction":null},"items":[]}"""
            // init silent load + composition LaunchedEffect load
            repeat(2) {
                server.enqueue(MockResponse().setBody(bal))
                server.enqueue(MockResponse().setBody(bal))
                server.enqueue(MockResponse().setBody(futures))
                server.enqueue(MockResponse().setBody(bonus))
                server.enqueue(MockResponse().setBody("""{"networks":[]}"""))
            }
            val http = OkHttpClient()
            val api = NetworkFactory.api(http, server.url("/").newBuilder().host("127.0.0.1").build().toString())
            val vm = AssetsViewModel(
                api, tokens,
                MarketStore(api, MarketSocket(http, "ws://127.0.0.1/", "fake", scope, accessToken = { null }), scope),
                ToastCenter(scope),
            )
            vm.seedEvidenceFutures(
                futures = ApiFuturesAccount(
                    marginBalance = "1000.00", availableBalance = "800.00", walletBalance = "1000.00",
                    crossPositionMargin = "100", isolatedMargin = "0",
                    crossUnrealizedPnl = "1.25", isolatedUnrealizedPnl = "0", orderMargin = "10",
                ),
                bonus = ApiFuturesBonusAvailable(trial = "12.34567890", deduction = null),
            )
            rule.setContent {
                LinbitTheme(false) {
                    AssetsScreen(bottomInset = 0.dp, onLogin = {}, assetsNav = AssetsNav(), vm = vm)
                }
            }
            rule.waitForIdle()
            // 再 seed 一次，覆盖 composition 触发的 load 竞态，仍用同一本地夹具
            rule.runOnIdle {
                vm.seedEvidenceFutures(
                    futures = ApiFuturesAccount(
                        marginBalance = "1000.00", availableBalance = "800.00", walletBalance = "1000.00",
                        crossPositionMargin = "100", isolatedMargin = "0",
                        crossUnrealizedPnl = "1.25", isolatedUnrealizedPnl = "0", orderMargin = "10",
                    ),
                    bonus = ApiFuturesBonusAvailable(trial = "12.34567890", deduction = null),
                )
            }
            rule.waitForIdle()
            rule.onNodeWithText("体验金").assertIsDisplayed()
            rule.onNodeWithText("抵扣金").assertIsDisplayed()
            rule.onNodeWithText("12.3456789").assertIsDisplayed()
            assertTrue(rule.onAllNodesWithText("--").fetchSemanticsNodes().isNotEmpty())
            capture("assets-futures-bonus")
            vm.viewModelScope.cancel()
        } finally {
            scope.cancel()
            server.shutdown()
        }
    }
}
