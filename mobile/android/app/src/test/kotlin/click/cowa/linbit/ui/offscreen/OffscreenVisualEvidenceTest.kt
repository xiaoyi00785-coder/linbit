package click.cowa.linbit.ui.offscreen

import android.app.Application
import android.graphics.Bitmap
import android.graphics.Canvas
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
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
import click.cowa.linbit.core.generated.ApiAccount
import click.cowa.linbit.core.generated.ApiKycStatus
import click.cowa.linbit.core.generated.ApiSecurityLevel
import click.cowa.linbit.core.generated.ApiSecurityLevelItemsInner
import click.cowa.linbit.core.generated.ApiSecurityOverview
import click.cowa.linbit.core.generated.ApiSecurityOverviewTwoFactor
import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiEarnProduct
import click.cowa.linbit.core.generated.ApiConvertPairs
import click.cowa.linbit.core.generated.ApiConvertPairsPairsInner
import click.cowa.linbit.core.generated.ApiConvertQuote
import click.cowa.linbit.core.generated.ApiFuturesAlgoOrder
import click.cowa.linbit.core.generated.ApiGridBot
import click.cowa.linbit.core.generated.ApiGridSquareItem
import click.cowa.linbit.core.generated.ApiStrategyConfig200Response
import click.cowa.linbit.core.generated.ApiWalletNetwork
import click.cowa.linbit.core.generated.ApiWithdrawNetwork
import click.cowa.linbit.core.generated.ApiWithdrawal
import click.cowa.linbit.core.generated.ApiFuturesContract
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.generated.ApiHomeNewcomer
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.network.ErrorText
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
import click.cowa.linbit.feature.more.MoreMenuScreen
import click.cowa.linbit.feature.more.CouponsScreen
import click.cowa.linbit.feature.more.CouponViewModel
import click.cowa.linbit.feature.more.NotificationsScreen
import click.cowa.linbit.feature.more.NotificationViewModel
import click.cowa.linbit.feature.more.CopyTradingScreen
import click.cowa.linbit.feature.more.CopyViewModel
import click.cowa.linbit.feature.more.PartnerScreen
import click.cowa.linbit.feature.more.PartnerViewModel
import click.cowa.linbit.feature.more.SupportTicketsScreen
import click.cowa.linbit.feature.more.SupportViewModel
import click.cowa.linbit.feature.more.TradFiScreen
import click.cowa.linbit.feature.more.TradFiViewModel
import click.cowa.linbit.feature.more.WelfareScreen
import click.cowa.linbit.feature.more.WelfareViewModel
import click.cowa.linbit.feature.more.MorePage
import click.cowa.linbit.feature.profile.AccountSecureStorage
import click.cowa.linbit.feature.profile.SavedAccount
import click.cowa.linbit.feature.profile.SavedAccounts
import click.cowa.linbit.feature.profile.ProfileViewModel
import click.cowa.linbit.feature.profile.UserCenterScreen
import click.cowa.linbit.feature.profile.AccountSwitchScreen
import click.cowa.linbit.feature.profile.SecurityHost
import click.cowa.linbit.feature.profile.SecurityViewModel
import click.cowa.linbit.feature.profile.KycHost
import click.cowa.linbit.feature.profile.KycViewModel
import click.cowa.linbit.feature.home.HomeContent
import click.cowa.linbit.feature.home.HomeState
import click.cowa.linbit.feature.trade.EventContractScreen
import click.cowa.linbit.feature.trade.EventContractViewModel
import click.cowa.linbit.feature.trade.FuturesKind
import click.cowa.linbit.feature.trade.FuturesRootScreen
import click.cowa.linbit.feature.trade.FuturesTradeViewModel
import click.cowa.linbit.feature.trade.SpotTradeContent
import click.cowa.linbit.feature.trade.SpotTradeViewModel
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.feature.assets.DepositNetworkSheet
import click.cowa.linbit.feature.auth.LocalLbSheetInlineEvidence
import click.cowa.linbit.feature.assets.WithdrawScreen
import click.cowa.linbit.feature.assets.WithdrawRecordsScreen
import click.cowa.linbit.feature.trade.AmendAlgoSheet
import click.cowa.linbit.feature.trade.ConvertSubpage
import click.cowa.linbit.feature.trade.EarnSubpage
import click.cowa.linbit.feature.trade.SpotSubpagesViewModel
import click.cowa.linbit.feature.trade.StrategySubpage
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.hasSetTextAction
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

    /** 离屏：sheet 同窗内联，content 位图能拍到弹层。 */
    private fun evidenceSetContent(content: @Composable () -> Unit) {
        rule.setContent {
            CompositionLocalProvider(LocalLbSheetInlineEvidence provides true) {
                content()
            }
        }
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
                            MarketState(
                                spot = listOf(
                                    MarketRow("BTCUSDT", "BTC", "USDT", bd("81076.24"), bd("0.51"), bd("1.2E8"), 2),
                                    MarketRow("ETHUSDT", "ETH", "USDT", bd("2800.1"), bd("-1.2"), bd("8.1E7"), 2),
                                ),
                                cnyRate = bd("7.15"),
                            ),
                            emptyList(), true, 100.dp,
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


    @Test fun strategyCreateSheet() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    StrategySubpage(85.dp, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        // LaunchedEffect 会 clearAccountData；示例数据在其后注入（非生产）
        rule.runOnIdle {
            vm.seedEvidenceStrategy(
                squareItems = listOf(ApiGridSquareItem(id = "sq1", symbol = "BTCUSDT", mode = "arithmetic", lowerPrice = "60000", upperPrice = "90000", gridCount = 10, profitRate = "0.12")),
                config = ApiStrategyConfig200Response(minInvestment = "100", sellBaseOnStop = true),
            )
        }
        rule.waitForIdle()
        rule.onNodeWithText("创建网格", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("确认创建", useUnmergedTree = true).assertIsDisplayed()
        capture("Strategy-create-sheet")
    }

    @Test fun strategyStopConfirmSheet() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    StrategySubpage(85.dp, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.runOnIdle {
            vm.seedEvidenceStrategy(
                grids = listOf(
                    ApiGridBot(
                        id = "g1", symbol = "BTCUSDT", mode = "arithmetic", status = ApiGridBot.Status.RUNNING,
                        sellBaseOnStop = true, quoteAsset = "USDT", totalProfit = "12.5",
                    ),
                ),
                config = ApiStrategyConfig200Response(sellBaseOnStop = true),
            )
        }
        rule.waitForIdle()
        rule.onNodeWithText("停止", useUnmergedTree = true).performScrollTo().performClick()
        rule.waitForIdle()
        rule.onNodeWithText("确认停止网格", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("确认停止", useUnmergedTree = true).assertIsDisplayed()
        capture("Strategy-stop-confirm")
    }

    @Test fun earnSubscribeSheet() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    EarnSubpage(85.dp, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.runOnIdle { vm.seedEvidenceEarn(listOf(ApiEarnProduct(id = "usdt-flex", asset = "USDT", title = "USDT活期", apr = "0.05"))) }
        rule.waitForIdle()
        rule.onNodeWithText("申购", useUnmergedTree = true).performScrollTo().performClick()
        rule.waitForIdle()
        rule.onNodeWithText("确认申购", useUnmergedTree = true).assertIsDisplayed()
        capture("Earn-subscribe-sheet")
    }

    @Test fun earnRedeemSheet() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    EarnSubpage(85.dp, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.runOnIdle { vm.seedEvidenceEarn(listOf(ApiEarnProduct(id = "usdt-flex", asset = "USDT", title = "USDT活期", apr = "0.05"))) }
        rule.waitForIdle()
        rule.onNodeWithText("赎回", useUnmergedTree = true).performScrollTo().performClick()
        rule.waitForIdle()
        rule.onNodeWithText("确认赎回", useUnmergedTree = true).assertIsDisplayed()
        capture("Earn-redeem-sheet")
    }

    @Test fun depositNetworkSheet() {
        enabled()
        val nets = listOf(
            ApiWalletNetwork(asset = "USDT", network = "TRC20", recommended = true, estimatedArrivalMinutes = 3),
            ApiWalletNetwork(asset = "USDT", network = "ERC20", recommended = false, estimatedArrivalMinutes = 15),
        )
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    DepositNetworkSheet(nets, selected = "TRC20", onPick = {}, onClose = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("选择网络", useUnmergedTree = true).assertIsDisplayed()
        capture("Deposit-network-sheet")
    }

    @Test fun withdrawPageShell() {
        enabled()
        tokens.accessToken = "t"
        val vm = AssetsViewModel(api, tokens, store, toast)
        // 本地示例网络/余额非生产；不发起真实提现
        vm.evidenceWithdrawNets = listOf(
            ApiWithdrawNetwork(
                network = "TRC20", withdrawEnabled = true, memoRequired = false,
                minWithdraw = "10", maxWithdraw = "50000", gasFeeEstimate = "1", gasFeeAsset = "USDT",
                estimatedArrivalMinutes = 3,
            ),
            ApiWithdrawNetwork(
                network = "ERC20", withdrawEnabled = true, memoRequired = false,
                minWithdraw = "20", gasFeeEstimate = "5", gasFeeAsset = "USDT",
                estimatedArrivalMinutes = 15,
            ),
        )
        vm.evidenceWalletNets = listOf(
            ApiWalletNetwork(asset = "USDT", network = "TRC20", recommended = true, estimatedArrivalMinutes = 3, withdrawEnabled = true),
            ApiWalletNetwork(asset = "USDT", network = "ERC20", recommended = false, estimatedArrivalMinutes = 15, withdrawEnabled = true),
        )
        vm.evidenceTwoFactor = true
        vm.seedEvidenceBalances(
            funding = listOf(ApiBalance(asset = "USDT", free = "1000", locked = "0", total = "1000", valuationUsd = "1000")),
        )
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    WithdrawScreen(assetInit = "USDT", vm = vm, assetsNav = AssetsNav(), onBack = {}, onChangeAsset = {})
                }
            }
        }
        rule.waitForIdle()
        check(rule.onAllNodesWithText("提现", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        check(rule.onAllNodesWithText("链上提现", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        capture("Withdraw-shell")
        // 填地址/数量后打开安全验证 sheet（mock，不 submit）
        rule.onAllNodes(hasSetTextAction(), useUnmergedTree = true).fetchSemanticsNodes().let { nodes ->
            // 地址、memo、数量 — 取可编辑节点依次写入
            val editors = rule.onAllNodes(hasSetTextAction(), useUnmergedTree = true)
            val count = editors.fetchSemanticsNodes().size
            if (count >= 1) editors[0].performTextReplacement("TXy1Z9DemoAddr000000000000000000000")
            if (count >= 3) editors[count - 1].performTextReplacement("100")
            else if (count >= 2) editors[1].performTextReplacement("100")
        }
        rule.waitForIdle()
        if (rule.onAllNodesWithText("提现", useUnmergedTree = true).fetchSemanticsNodes().size >= 1) {
            // 底部主按钮「提现」
            val btns = rule.onAllNodesWithText("提现", useUnmergedTree = true)
            val n = btns.fetchSemanticsNodes().size
            btns[n - 1].performClick()
            rule.waitForIdle()
        }
        if (rule.onAllNodesWithText("安全验证", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()) {
            rule.onNodeWithText("安全验证", useUnmergedTree = true).assertIsDisplayed()
            capture("Withdraw-confirm-sheet")
        }
    }

    @Test fun withdrawRecordsCancelSheet() {
        enabled()
        tokens.accessToken = "t"
        val vm = AssetsViewModel(api, tokens, store, toast)
        vm.evidenceWithdrawRecords = listOf(
            ApiWithdrawal(
                id = "wd_pending", asset = "USDT", network = "TRC20",
                address = "TXy1Z9DemoAddr000000000000000000000", amount = "100",
                fee = "1", status = ApiWithdrawal.Status.PENDING, createdAt = "2026-11-05T12:00:00Z",
            ),
            ApiWithdrawal(
                id = "wd_done", asset = "USDT", network = "ERC20",
                address = "0xDemo", amount = "50",
                fee = "5", status = ApiWithdrawal.Status.COMPLETED, createdAt = "2026-11-04T12:00:00Z",
            ),
        )
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    WithdrawRecordsScreen(vm, onBack = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("提现记录", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("撤销申请", useUnmergedTree = true).assertIsDisplayed()
        // completed 行不应有可点撤销（仅 pending 显示按钮；列表里可能只有一处「撤销申请」）
        check(rule.onAllNodesWithText("撤销申请", useUnmergedTree = true).fetchSemanticsNodes().size == 1)
        capture("Withdraw-records")
        rule.onNodeWithText("撤销申请", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("确认撤销提现", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("确认撤销", useUnmergedTree = true).assertIsDisplayed()
        capture("Withdraw-cancel-confirm")
        // 不点确认：避免走真实/拦截网络；仅证据打开态
    }

    @Test fun inlineSheetEvidenceFlagDefaultsOffInProductionPath() {
        enabled()
        // 生产路径不提供 LocalLbSheetInlineEvidence=true；默认 false，走 ModalBottomSheet
        var flag = true
        rule.setContent {
            flag = LocalLbSheetInlineEvidence.current
            LinbitTheme(false) { Box(Modifier.requiredSize(10.dp, 10.dp)) }
        }
        rule.waitForIdle()
        check(!flag) { "LocalLbSheetInlineEvidence must default false outside evidenceSetContent" }
        capture("Inline-flag-default-off")
    }

    @Test fun amendAlgoSheet() {
        enabled()
        tokens.accessToken = "t"
        val hist = TradeHistoryViewModel(api, tokens, store)
        val order = ApiFuturesAlgoOrder(
            algoId = 99L, symbol = "BTCUSDT", side = "BUY",
            triggerPrice = "80000", price = "79900", qty = "0.01",
            orderType = ApiFuturesAlgoOrder.OrderType.LIMIT,
        )
        evidenceSetContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    AmendAlgoSheet(hist, order, plan = true, onClose = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("改条件单", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("确认修改", useUnmergedTree = true).assertIsDisplayed()
        capture("History-amend-algo")
    }

    @Test fun convertExpiredQuoteUi() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        vm.holdExpiredQuoteForEvidence = true
        val pairs = ApiConvertPairs(
            pairs = listOf(
                ApiConvertPairsPairsInner(from = "USDT", to = "BTC"),
                ApiConvertPairsPairsInner(from = "BTC", to = "USDT"),
            ),
        )
        val expired = ApiConvertQuote(
            quoteId = "q-exp", from = "USDT", to = "BTC", fromAmount = "100", toAmount = "0.001",
            rate = "0.00001", expiresAt = System.currentTimeMillis() - 5_000L,
        )
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    ConvertSubpage(85.dp, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        // 先填数量（会 bump 清 quote），再注入过期示例报价；本地示例非生产
        rule.runOnIdle { vm.seedEvidenceConvert(pairs, null) }
        rule.waitForIdle()
        rule.onAllNodes(hasSetTextAction(), useUnmergedTree = true).fetchSemanticsNodes().let { nodes ->
            if (nodes.isNotEmpty()) {
                rule.onAllNodes(hasSetTextAction(), useUnmergedTree = true)[0].performTextInput("100")
            }
        }
        rule.waitForIdle()
        rule.runOnIdle { vm.seedEvidenceConvert(pairs, expired) }
        rule.waitForIdle()
        rule.onNodeWithText("报价已过期", useUnmergedTree = true).assertIsDisplayed()
        check(rule.onAllNodesWithText("确认闪兑", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        capture("Convert-expired")
    }

    @Test fun convertQuoteFailCodeUi() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    ConvertSubpage(85.dp, onLogin = {}, vm = vm)
                    androidx.compose.foundation.layout.Column(
                        Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(16.dp)
                            .background(Color(0xFFFFEBEE)),
                    ) {
                        androidx.compose.material3.Text(
                            "QUOTE_EXPIRED → " + ErrorText.map.getValue("QUOTE_EXPIRED"),
                            Modifier.padding(10.dp),
                            color = Color(0xFFB00020),
                        )
                        androidx.compose.material3.Text(
                            "QUOTE_NOT_FOUND → " + ErrorText.map.getValue("QUOTE_NOT_FOUND"),
                            Modifier.padding(10.dp),
                            color = Color(0xFFB00020),
                        )
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.runOnIdle {
            vm.seedEvidenceConvert(
                ApiConvertPairs(pairs = listOf(ApiConvertPairsPairsInner(from = "USDT", to = "BTC"))),
            )
        }
        rule.waitForIdle()
        rule.onNodeWithText("报价已过期，请重新询价", substring = true, useUnmergedTree = true).assertIsDisplayed()
        capture("Convert-quote-fail")
    }

    @Test fun convertPausedPairUi() {
        enabled()
        tokens.accessToken = "t"
        val vm = SpotSubpagesViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    ConvertSubpage(85.dp, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.runOnIdle {
            vm.seedEvidenceConvert(
                ApiConvertPairs(
                    pairs = listOf(
                        ApiConvertPairsPairsInner(
                            from = "USDC", to = "USDT", paused = true,
                            pauseReason = ApiConvertPairsPairsInner.PauseReason.DEPEG,
                        ),
                        ApiConvertPairsPairsInner(from = "USDT", to = "BTC"),
                    ),
                ),
            )
        }
        rule.waitForIdle()
        // 选暂停币对（本地示例）
        if (rule.onAllNodesWithText("USDC", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()) {
            rule.onAllNodesWithText("USDC", useUnmergedTree = true)[0].performClick()
            rule.waitForIdle()
        }
        check(
            rule.onAllNodesWithText("该币对因脱锚保护已暂停", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() ||
                rule.onAllNodesWithText("该币对已暂停", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty(),
        )
        capture("Convert-pairs")
    }


    private fun memAccountStore(): AccountSecureStorage = object : AccountSecureStorage {
        private val m = mutableMapOf<String, String?>()
        override fun get(key: String) = m[key]
        override fun set(key: String, value: String?) { if (value == null) m.remove(key) else m[key] = value }
    }

    /** 测试专用示例账号：非真实个人资料，不入库凭据。 */

    private fun evidenceSecurityOverview() = ApiSecurityOverview(
        securityLevel = ApiSecurityLevel(
            score = 1,
            max = 5,
            level = ApiSecurityLevel.Level.MEDIUM,
            missing = listOf(
                ApiSecurityLevel.Missing.PHONE_VERIFIED,
                ApiSecurityLevel.Missing.TWO_FACTOR,
                ApiSecurityLevel.Missing.FUND_PASSWORD,
                ApiSecurityLevel.Missing.ANTI_PHISHING,
            ),
            items = listOf(
                ApiSecurityLevelItemsInner(ApiSecurityLevelItemsInner.Key.EMAIL_VERIFIED, true),
                ApiSecurityLevelItemsInner(ApiSecurityLevelItemsInner.Key.PHONE_VERIFIED, false),
                ApiSecurityLevelItemsInner(ApiSecurityLevelItemsInner.Key.TWO_FACTOR, false),
                ApiSecurityLevelItemsInner(ApiSecurityLevelItemsInner.Key.FUND_PASSWORD, false),
                ApiSecurityLevelItemsInner(ApiSecurityLevelItemsInner.Key.ANTI_PHISHING, false),
            ),
        ),
        twoFactor = ApiSecurityOverviewTwoFactor(enabled = false),
        email = "evi***@example.invalid",
        emailVerified = true,
        phone = null,
        phoneVerified = false,
        fundPasswordSet = false,
        antiPhishingSet = false,
        passkeysCount = 0,
    )

    private fun evidenceAccount() = ApiAccount(
        uid = "evidence_uid_1001",
        kycLevel = 0,
        twoFactorEnabled = false,
        createdAt = "2026-01-01T00:00:00Z",
        email = "evidence@example.invalid",
        nickname = "证据账号",
        emailVerified = true,
        phoneVerified = false,
    )

    @Test fun moreMenu() {
        enabled()
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    MoreMenuScreen(onBack = {}, onOpen = {})
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("全部功能", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("福利中心", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("跟单交易", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("消息通知", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("卡券中心", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("合伙人计划", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("TradFi", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("我的工单", useUnmergedTree = true).assertIsDisplayed()
        capture("More-menu")
    }

    @Test fun couponsLoggedOut() {
        enabled()
        tokens.accessToken = null
        val vm = CouponViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    CouponsScreen(onBack = {}, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("登录后查看卡券", useUnmergedTree = true).assertIsDisplayed()
        capture("Coupons-logged-out")
    }

    @Test fun notificationsLoggedOut() {
        enabled()
        tokens.accessToken = null
        val vm = NotificationViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    NotificationsScreen(onBack = {}, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("登录后查看消息通知", useUnmergedTree = true).assertIsDisplayed()
        capture("Notifications-logged-out")
    }

    @Test fun supportLoggedOut() {
        enabled()
        tokens.accessToken = null
        val vm = SupportViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    SupportTicketsScreen(onBack = {}, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("登录后查看和提交工单", useUnmergedTree = true).assertIsDisplayed()
        capture("Support-logged-out")
    }

    @Test fun copyMyFollowsLoggedOut() {
        enabled()
        tokens.accessToken = null
        val vm = CopyViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    CopyTradingScreen(onBack = {}, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("我的跟单", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithText("登录后查看我的跟单", useUnmergedTree = true).assertIsDisplayed()
        capture("Copy-my-follows-logged-out")
    }

    @Test fun copyTradersOfflineError() {
        enabled()
        tokens.accessToken = null
        val vm = CopyViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    CopyTradingScreen(onBack = {}, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("重试", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() ||
                rule.onAllNodesWithText("暂无带单员", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        }
        capture("Copy-traders-offline")
    }

    @Test fun partnerLandingLoggedOut() {
        enabled()
        tokens.accessToken = null
        val vm = PartnerViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    PartnerScreen(onBack = {}, onLogin = {}, vm = vm)
                }
            }
        }
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("成为 Linbit 合伙人", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        }
        rule.onNodeWithText("立即申请", useUnmergedTree = true).assertIsDisplayed()
        // 离线无规则时不应出现误导性「0%」
        check(rule.onAllNodesWithText("0%", substring = true, useUnmergedTree = true).fetchSemanticsNodes().isEmpty())
        capture("Partner-landing-logged-out")
    }

    @Test fun tradFiOfflineError() {
        enabled()
        val vm = TradFiViewModel(api)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    TradFiScreen(onBack = {}, onTrade = { _, _ -> }, vm = vm)
                }
            }
        }
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("重试", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() ||
                rule.onAllNodesWithText("暂无可交易资产", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        }
        capture("TradFi-offline")
    }

    @Test fun welfareOfflineError() {
        enabled()
        tokens.accessToken = null
        val vm = WelfareViewModel(api, tokens, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    WelfareScreen(
                        onBack = {}, onLogin = {}, onOpen = {}, onTrade = { _, _ -> },
                        onProfile = {}, onDeposit = {}, vm = vm,
                    )
                }
            }
        }
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("重试", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        }
        capture("Welfare-offline")
    }

    @Test fun userCenterSeeded() {
        enabled()
        tokens.accessToken = "evidence-token-not-real"
        val accounts = SavedAccounts(memAccountStore(), tokens)
        val vm = ProfileViewModel(api, accounts, tokens, toast)
        vm.seedEvidenceProfile(
            account = evidenceAccount(),
            kyc = ApiKycStatus(level = 0, status = ApiKycStatus.Status.NONE),
            security = evidenceSecurityOverview(),
            saved = listOf(
                SavedAccount("evidence_uid_1001", "证据账号", "evi***@example.invalid"),
                SavedAccount("evidence_uid_1002", "证据账号B", "pho****0000"),
            ),
        )
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    UserCenterScreen(
                        themeMode = ThemeMode.LIGHT,
                        onTheme = {},
                        onLogout = {},
                        onClose = {},
                        onOpen = {},
                        onFeature = {},
                        vm = vm,
                    )
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("用户中心", useUnmergedTree = true).assertIsDisplayed()
        check(rule.onAllNodesWithText("证据账号", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        rule.onNodeWithText("去认证", useUnmergedTree = true).assertIsDisplayed()
        capture("UserCenter-hub")
    }

    @Test fun accountSwitchSeeded() {
        enabled()
        tokens.accessToken = "evidence-token-not-real"
        val accounts = SavedAccounts(memAccountStore(), tokens)
        val vm = ProfileViewModel(api, accounts, tokens, toast)
        vm.seedEvidenceProfile(
            account = evidenceAccount(),
            saved = listOf(
                SavedAccount("evidence_uid_1001", "证据账号", "evi***@example.invalid"),
                SavedAccount("evidence_uid_1002", "证据账号B", "pho****0000"),
            ),
        )
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    AccountSwitchScreen(onBack = {}, onAdd = {}, onLogout = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("账号切换", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("证据账号", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("证据账号B", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("+ 添加账号", useUnmergedTree = true).assertIsDisplayed()
        capture("Account-switch")
    }

    @Test fun securityCenterSeeded() {
        enabled()
        val vm = SecurityViewModel(api, toast)
        // 2FA 未开启；不 beginBinding、不写入密钥
        vm.seedEvidenceSecurity(evidenceSecurityOverview())
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    SecurityHost(onBack = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("账户安全", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("身份验证器", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("未开启", useUnmergedTree = true).assertIsDisplayed()
        capture("Security-center")
    }

    @Test fun kycStatusNoneSeeded() {
        enabled()
        val ctx = RuntimeEnvironment.getApplication()
        val vm = KycViewModel(api, ctx, toast)
        // 默认 NONE，非 APPROVED；不打开表单、不提交
        vm.seedEvidenceKyc(ApiKycStatus(level = 0, status = ApiKycStatus.Status.NONE))
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    KycHost(onBack = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("身份认证", useUnmergedTree = true).assertIsDisplayed()
        rule.onNodeWithText("未认证", useUnmergedTree = true).assertIsDisplayed()
        // 卡片标题为 "Lv.1 基础认证"
        rule.onNodeWithText("Lv.1 基础认证", useUnmergedTree = true).assertIsDisplayed()
        capture("Kyc-status-none")
    }

    @Test fun kycStatusRejectedSeeded() {
        enabled()
        val ctx = RuntimeEnvironment.getApplication()
        val vm = KycViewModel(api, ctx, toast)
        vm.seedEvidenceKyc(
            ApiKycStatus(
                level = 1,
                status = ApiKycStatus.Status.REJECTED,
                submittedAt = "2026-11-01T00:00:00Z",
                reason = "fixture-only sample rejection",
            ),
        )
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    KycHost(onBack = {}, vm = vm)
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("原因：fixture-only sample rejection", useUnmergedTree = true).assertIsDisplayed()
        capture("Kyc-status-rejected")
    }

    @Test fun securityOfflineError() {
        enabled()
        // 无 seed → 503 → 错误态；不绑 2FA
        val vm = SecurityViewModel(api, toast)
        rule.setContent {
            LinbitTheme(false) {
                Box(Modifier.requiredSize(390.dp, 844.dp).background(Color.White)) {
                    SecurityHost(onBack = {}, vm = vm)
                }
            }
        }
        rule.waitUntil(5_000) {
            rule.onAllNodesWithText("重试", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty()
        }
        capture("Security-offline")
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
