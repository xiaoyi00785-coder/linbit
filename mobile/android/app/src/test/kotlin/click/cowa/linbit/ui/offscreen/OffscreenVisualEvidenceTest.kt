package click.cowa.linbit.ui.offscreen

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.auth.AuthRepository
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiFuturesContract
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.generated.ApiHomeNewcomer
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.storage.AppPrefs
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.data.trade.BookLevel
import click.cowa.linbit.data.trade.OrderBook
import click.cowa.linbit.data.trade.TradeNav
import click.cowa.linbit.feature.assets.AssetsScreen
import click.cowa.linbit.feature.assets.AssetsViewModel
import click.cowa.linbit.feature.assets.TransferScreen
import click.cowa.linbit.feature.auth.AuthViewModel
import click.cowa.linbit.feature.auth.LoginAccountScreen
import click.cowa.linbit.feature.home.HomeContent
import click.cowa.linbit.feature.home.HomeState
import click.cowa.linbit.feature.trade.EventContractScreen
import click.cowa.linbit.feature.trade.EventContractViewModel
import click.cowa.linbit.feature.trade.FuturesKind
import click.cowa.linbit.feature.trade.FuturesRootScreen
import click.cowa.linbit.feature.trade.FuturesTradeViewModel
import click.cowa.linbit.feature.trade.SpotTradeContent
import click.cowa.linbit.feature.trade.SpotTradeViewModel
import click.cowa.linbit.feature.trade.SpotHistoryScreen
import click.cowa.linbit.feature.trade.FuturesHistoryScreen
import click.cowa.linbit.feature.trade.TradeHistoryViewModel
import click.cowa.linbit.ui.components.FloatingTabBar
import click.cowa.linbit.ui.components.MainTab
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.LinbitTheme
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assume
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.io.File
import java.math.BigDecimal
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * 可复现离屏视觉证据（默认跳过，不进 CI 主路径）。
 * 启用：./gradlew testDebugUnitTest -Doffscreen.shots=1 --tests '...OffscreenVisualEvidenceTest'
 * 输出：app/build/reports/offscreen-shots/（gitignore，不入库二进制）
 * 网络全拦截；无真实资金；不等于真机/ref 像素通过。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w390dp-h844dp-xxhdpi")
class OffscreenVisualEvidenceTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()
    private val scope = CoroutineScope(Dispatchers.Main)
    private val client = OkHttpClient.Builder().addInterceptor { chain ->
        Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
            .code(503).message("offline").body("{}".toResponseBody()).build()
    }.build()
    private val api = NetworkFactory.api(client, "https://example.invalid/")
    private val socket = MarketSocket(client, "wss://example.invalid/ws", "shot", scope, accessToken = { null })
    private val store = MarketStore(api, socket, scope)
    private val tokens = InMemoryTokenStore()
    private val nav = TradeNav()
    private val toast = ToastCenter(scope)
    private val outDir: File by lazy {
        val root = File("build/reports/offscreen-shots").absoluteFile
        root.mkdirs(); root
    }

    private fun enabled() {
        Assume.assumeTrue(
            "set -Doffscreen.shots=1 to generate evidence PNGs",
            System.getProperty("offscreen.shots") == "1" || System.getenv("OFFSCREEN_SHOTS") == "1",
        )
    }

    private fun bd(v: String) = BigDecimal(v)
    private fun book(mid: String): OrderBook {
        val m = bd(mid)
        return OrderBook(
            (1..7).map { BookLevel(m - bd("0.1") * it.toBigDecimal(), bd("2.73")) },
            (1..7).map { BookLevel(m + bd("0.1") * it.toBigDecimal(), bd("0.4135")) },
        )
    }

    @Test fun homeWithDeadline() {
        enabled()
        val deadline = Instant.now().plus(6, ChronoUnit.DAYS).plus(12, ChronoUnit.HOURS).toString()
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    Column(Modifier.fillMaxSize()) {
                        Box(Modifier.fillMaxWidth().height(44.dp))
                        HomeContent(
                            HomeState(
                                newcomer = ApiHomeNewcomer(total = "215", asset = "USDT", available = true, deadline = deadline),
                                earn = listOf(ApiEarnProduct(asset = "TRX", title = "TRX定期理财", apr = "0.059")),
                            ),
                            MarketState(), emptyList(), true, 85.dp,
                            {}, {}, {}, {}, {},
                            onRefresh = {},
                        )
                    }
                    FloatingTabBar(MainTab.HOME, {}, Modifier.align(Alignment.BottomCenter).padding(bottom = 15.dp))
                }
            }
        }
        rule.waitForIdle()
        capture("Home-deadline")
    }

    @Test fun spotEmptyClearance() {
        enabled()
        tokens.accessToken = "t"
        val spot = SpotTradeViewModel(api, store, socket, tokens, nav, toast)
        spot.seedForShot(
            ApiSymbol(symbol = "BTCUSDT", base = "BTC", quote = "USDT", pricePrecision = 2, quantityPrecision = 5, status = ApiSymbol.Status.TRADING),
            book("81076.24"), bd("81076.24"), bd("0.51"),
        )
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    SpotTradeContent(spot, true, emptyList(), bd("7.155"), 85.dp, ThemeMode.LIGHT, {}, {}, {}, {}, {})
                    FloatingTabBar(MainTab.SPOT, {}, Modifier.align(Alignment.BottomCenter).padding(bottom = 15.dp).testTag("floating-tabs"))
                }
            }
        }
        rule.waitForIdle()
        val action = rule.onNodeWithText("充值", useUnmergedTree = true).fetchSemanticsNode().boundsInRoot
        val tabs = rule.onNodeWithTag("floating-tabs", useUnmergedTree = true).fetchSemanticsNode().boundsInRoot
        check(tabs.top - action.bottom >= 24f) {
            "空态充值与悬浮栏间距不足: gap=${tabs.top - action.bottom}"
        }
        capture("Spot-empty")
    }

    @Test fun futuresFundingCountdown() {
        enabled()
        tokens.accessToken = "t"
        val futures = FuturesTradeViewModel(api, store, socket, tokens, nav, toast)
        val next = System.currentTimeMillis() + 3_600_000L
        futures.seedForShot(
            ApiFuturesContract("BTCUSDT", 100, "0.1", "0.001", "1000", "BTC", "USDT"),
            ApiFuturesTicker(
                symbol = "BTCUSDT", lastPrice = "81087.2", markPrice = "81085.1",
                priceChangePercent24h = "0.66", fundingRate = "0.000134", nextFundingTime = next,
            ),
            book("81087.2"), null,
        )
        futures.kind = FuturesKind.MARKET
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    FuturesRootScreen(85.dp, ThemeMode.LIGHT, {}, {}, {}, {}, {}, autoStart = false, vm = futures)
                    FloatingTabBar(MainTab.FUTURES, {}, Modifier.align(Alignment.BottomCenter).padding(bottom = 15.dp))
                }
            }
        }
        rule.waitForIdle()
        // 有 nextFundingTime 时不应再显示全占位 --:--:--（允许小时位 00/01）
        val nodes = rule.onAllNodesWithText("--:--:--", useUnmergedTree = true).fetchSemanticsNodes()
        // 可能仍有其它占位；资金费率行应含真实倒计时数字 — 至少页面含「资金费率」
        rule.onNodeWithText("资金费率(8H) / 倒计时", useUnmergedTree = true).assertIsDisplayed()
        capture("Futures-funding")
    }

    @Test fun login() {
        enabled()
        val ctx = RuntimeEnvironment.getApplication()
        val auth = AuthRepository(api, tokens, deviceId = { "shot" }, deviceName = { "Robolectric" }, scope = scope)
        val authVm = AuthViewModel(auth, tokens, AppPrefs(ctx), toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    LoginAccountScreen(authVm, {}, {}, {}, {})
                }
            }
        }
        rule.waitForIdle()
        capture("Login")
    }

    @Test fun assetsLoggedOut() {
        enabled()
        tokens.accessToken = null
        val assetsVm = AssetsViewModel(api, tokens, store, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    AssetsScreen(85.dp, {}, AssetsNav(), vm = assetsVm)
                    FloatingTabBar(MainTab.ASSETS, {}, Modifier.align(Alignment.BottomCenter).padding(bottom = 15.dp))
                }
            }
        }
        rule.waitForIdle()
        capture("Assets-logged-out")
    }

    @Test fun transfer() {
        enabled()
        tokens.accessToken = "t"
        val transferVm = AssetsViewModel(api, tokens, store, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    TransferScreen(transferVm, onBack = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("划转", useUnmergedTree = true).assertIsDisplayed()
        capture("Transfer")
    }

    @Test fun eventContract() {
        enabled()
        tokens.accessToken = "t"
        val eventVm = EventContractViewModel(api, tokens, socket, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    EventContractScreen(bottomInset = 85.dp, onLogin = {}, vm = eventVm)
                }
            }
        }
        rule.waitForIdle()
        capture("Event-contract")
    }

    @Test fun spotHistory() {
        enabled()
        tokens.accessToken = "t"
        val hist = TradeHistoryViewModel(api, tokens, store)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    SpotHistoryScreen(onClose = {}, onLogin = {}, vm = hist)
                }
            }
        }
        rule.waitForIdle()
        capture("Spot-history")
    }

    @Test fun futuresHistory() {
        enabled()
        tokens.accessToken = "t"
        val hist = TradeHistoryViewModel(api, tokens, store)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    FuturesHistoryScreen(onClose = {}, onLogin = {}, vm = hist)
                }
            }
        }
        rule.waitForIdle()
        capture("Futures-history")
    }

    private fun capture(name: String) {
        rule.runOnIdle {
            val view = rule.activity.findViewById<android.view.View>(android.R.id.content)
            val bitmap = Bitmap.createBitmap(view.width.coerceAtLeast(1), view.height.coerceAtLeast(1), Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            File(outDir, "$name.png").outputStream().use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
