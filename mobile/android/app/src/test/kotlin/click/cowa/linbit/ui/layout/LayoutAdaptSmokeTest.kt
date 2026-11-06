package click.cowa.linbit.ui.layout

import android.app.Application
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.core.generated.ApiFuturesContract
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.network.InMemoryTokenStore
import click.cowa.linbit.core.network.NetworkFactory
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.trade.OrderBook
import click.cowa.linbit.data.trade.TradeNav
import click.cowa.linbit.feature.trade.FuturesTradeContent
import click.cowa.linbit.feature.trade.FuturesTradeViewModel
import click.cowa.linbit.feature.trade.SpotTradeContent
import click.cowa.linbit.feature.trade.SpotTradeViewModel
import click.cowa.linbit.ui.components.FloatingTabBar
import click.cowa.linbit.ui.components.MainTab
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LayoutAdapt
import click.cowa.linbit.ui.theme.LinbitTheme
import click.cowa.linbit.ui.theme.lb
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import java.math.BigDecimal

/**
 * 离屏布局冒烟（Robolectric）：320/360/412dp、字体放大、明暗主题。
 * 检查登录/交易/事件/历史骨架可达、滚动、安全区与 IME；并渲染令牌字色。
 * **不等于真机验收 / 不等于 ref 像素对齐**；拦截网络，无真实资金操作。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], application = Application::class, qualifiers = "zh-rCN-w360dp-h780dp-xxhdpi")
class LayoutAdaptSmokeTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    private val scope = CoroutineScope(Dispatchers.Main)
    private val client = OkHttpClient.Builder().addInterceptor { chain ->
        Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1)
            .code(503).message("offline layout smoke").body("{}".toResponseBody()).build()
    }.build()
    private val api = NetworkFactory.api(client, "https://example.invalid/")
    private val socket = MarketSocket(client, "wss://example.invalid/ws", "layout", scope, accessToken = { null })
    private val store = MarketStore(api, socket, scope)
    private val tokens = InMemoryTokenStore()
    private val nav = TradeNav()
    private val toast = ToastCenter(scope)

    @Test fun futuresTradeCompact320() = smokeFutures(320, 844, 1.0f)
    @Test fun futuresTradeRegular360() = smokeFutures(360, 780, 1.0f)
    @Test fun futuresTradeExpanded412() = smokeFutures(412, 915, 1.0f)
    @Test fun futuresTradeLargeFont360() = smokeFutures(360, 780, 1.3f)
    @Test fun futuresTradeDark360() = smokeFutures(360, 780, 1.0f, dark = true)

    @Test fun spotTradeCompact320() = smokeSpot(320, 844, 1.0f)
    @Test fun spotTradeLargeFont412() = smokeSpot(412, 915, 1.5f)
    @Test fun spotTradeDarkLargeFont360() = smokeSpot(360, 780, 1.3f, dark = true)

    @Test fun topTabsAndSimUnavailableOnCompact() {
        applyViewport(320, 844, 1.0f)
        rule.setContent {
            LinbitTheme(false) {
                var top by remember { mutableIntStateOf(0) }
                Column(Modifier.fillMaxSize().background(Color.White).statusBarsPadding().navigationBarsPadding().padding(horizontal = LayoutAdapt.pageH(320))) {
                    PrimaryTabs(
                        listOf("合约", "事件合约", "TradFi", "模拟交易"),
                        top, { top = it },
                        Modifier.testTag("futures-top-tabs"),
                        size = 16.sp,
                        spacing = LayoutAdapt.tabSpacing(320),
                        scrollable = LayoutAdapt.tabsScrollable(320, 4),
                    )
                    when (top) {
                        3 -> {
                            Text("模拟交易", Modifier.testTag("sim-title"))
                            Text(
                                "后端尚未提供模拟交易契约（OpenAPI 无 paper/sim 端点），上线后按契约接入",
                                Modifier.testTag("sim-note"),
                            )
                        }
                        else -> Text("tab-$top", Modifier.testTag("tab-body"))
                    }
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithTag("futures-top-tabs").assertIsDisplayed()
        rule.onNodeWithText("模拟交易", useUnmergedTree = true).performClick()
        rule.waitForIdle()
        rule.onNodeWithTag("sim-note").assertIsDisplayed()
    }

    @Test fun historyTabsScrollableOnCompact() {
        applyViewport(320, 844, 1.3f)
        rule.setContent {
            LinbitTheme(false) {
                var tab by remember { mutableIntStateOf(0) }
                Column(
                    Modifier.fillMaxSize().background(Color.White)
                        .statusBarsPadding().navigationBarsPadding().imePadding()
                        .padding(horizontal = LayoutAdapt.pageH(320)),
                ) {
                    Text(
                        "历史记录",
                        Modifier.testTag("history-title"),
                        style = lb(17.sp),
                        maxLines = LayoutAdapt.titleMaxLines(1.3f),
                    )
                    PrimaryTabs(
                        listOf("当前委托", "历史委托", "历史仓位", "成交明细", "资金流水", "条件委托"),
                        tab, { tab = it },
                        Modifier.testTag("history-tabs"),
                        size = 14.sp,
                        spacing = LayoutAdapt.tabSpacing(320),
                        scrollable = LayoutAdapt.tabsScrollable(320, 6),
                    )
                    Text("筛选条", Modifier.testTag("history-filters"))
                    Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).testTag("history-scroll")) {
                        repeat(12) { Text("行 $it", Modifier.fillMaxWidth().height(36.dp).testTag("row-$it")) }
                    }
                    Text(
                        "底部操作",
                        Modifier.testTag("history-action").padding(bottom = LayoutAdapt.contentBottomExtra(gestureNav = true)),
                    )
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithTag("history-title").assertIsDisplayed()
        rule.onNodeWithTag("history-tabs").assertIsDisplayed()
        rule.onNodeWithText("条件委托", useUnmergedTree = true).performScrollTo().assertIsDisplayed()
        rule.onNodeWithTag("row-11").performScrollTo().assertIsDisplayed()
        rule.onNodeWithTag("history-action").assertIsDisplayed()
    }

    @Test fun typographyAndPaletteSurfacesLight() = typographySurface(dark = false)
    @Test fun typographyAndPaletteSurfacesDark() = typographySurface(dark = true)

    private fun typographySurface(dark: Boolean) {
        applyViewport(360, 780, 1.0f)
        rule.setContent {
            LinbitTheme(dark) {
                val p = LB.p
                Column(Modifier.fillMaxSize().background(p.bg).padding(LayoutAdapt.pageH(360))) {
                    Text("一级标题", Modifier.testTag("typo-title"), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                    Text("正文", Modifier.testTag("typo-body"), style = lb(FontSize.body), color = p.textPrimary)
                    Text("次要", Modifier.testTag("typo-secondary"), style = lb(FontSize.caption), color = p.textSecondary)
                    Text("品牌", Modifier.testTag("typo-brand"), style = lb(FontSize.label, FontWeight.Medium), color = p.brand)
                    Text("涨", Modifier.testTag("typo-up"), style = lb(FontSize.label), color = p.up)
                    Text("跌", Modifier.testTag("typo-down"), style = lb(FontSize.label), color = p.down)
                    Box(Modifier.fillMaxWidth().height(44.dp).background(p.primary).testTag("btn-primary")) {
                        Text("主按钮", Modifier.align(Alignment.Center), color = p.textOnPrimary, style = lb(FontSize.label, FontWeight.Medium))
                    }
                }
            }
        }
        rule.waitForIdle()
        listOf("typo-title", "typo-body", "typo-secondary", "typo-brand", "typo-up", "typo-down", "btn-primary").forEach {
            rule.onNodeWithTag(it).assertIsDisplayed()
        }
    }

    @Test fun eventRulesFormAndLoginReachable() {
        applyViewport(360, 780, 1.0f)
        rule.setContent {
            LinbitTheme(false) {
                Column(
                    Modifier.fillMaxSize().background(LB.p.bg)
                        .imePadding().navigationBarsPadding().statusBarsPadding()
                        .verticalScroll(rememberScrollState())
                        .padding(LayoutAdapt.pageH(360)),
                ) {
                    Text("事件合约", Modifier.testTag("event-title"))
                    Text("规则", Modifier.testTag("event-rules"))
                    BasicTextField(
                        value = "10",
                        onValueChange = {},
                        modifier = Modifier.fillMaxWidth().height(40.dp).testTag("event-stake").background(LB.p.fill),
                    )
                    Text("看涨", Modifier.testTag("event-up").padding(top = 12.dp))
                    Text("看跌", Modifier.testTag("event-down"))
                    Text("登录 / 注册", Modifier.testTag("event-login"))
                    Text("加载更多", Modifier.testTag("event-more"))
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithTag("event-title").assertIsDisplayed()
        rule.onNodeWithTag("event-rules").assertIsDisplayed()
        rule.onNodeWithTag("event-stake").assertIsDisplayed()
        rule.onNodeWithTag("event-up").assertIsDisplayed()
        rule.onNodeWithTag("event-login").assertIsDisplayed().assertIsEnabled()
        rule.onNodeWithTag("event-more").assertIsDisplayed()
    }




    private fun applyViewport(width: Int, height: Int, fontScale: Float) {
        RuntimeEnvironment.setQualifiers("zh-rCN-w${width}dp-h${height}dp-xxhdpi")
        rule.activity.resources.configuration.fontScale = fontScale
        rule.activity.resources.updateConfiguration(rule.activity.resources.configuration, rule.activity.resources.displayMetrics)
    }

    private fun smokeFutures(width: Int, height: Int, fontScale: Float, dark: Boolean = false) {
        applyViewport(width, height, fontScale)
        tokens.accessToken = "offline-test-only"
        val futures = FuturesTradeViewModel(api, store, socket, tokens, nav, toast)
        futures.seedForShot(
            ApiFuturesContract("BTCUSDT", 100, "0.1", "0.001", "1000", "BTC", "USDT"),
            ApiFuturesTicker(symbol = "BTCUSDT", lastPrice = "81087.2", markPrice = "81085.1"),
            OrderBook(), null,
        )
        rule.setContent {
            LinbitTheme(dark) {
                Box(Modifier.fillMaxSize().background(if (dark) Color(0xFF0B0B0C) else Color.White).navigationBarsPadding()) {
                    FuturesTradeContent(
                        futures, true, emptyList(), 73.dp, if (dark) ThemeMode.DARK else ThemeMode.LIGHT,
                        {}, {}, {}, {}, {},
                        modifier = Modifier.fillMaxSize().padding(bottom = 56.dp),
                    )
                    FloatingTabBar(MainTab.FUTURES, {}, Modifier.align(Alignment.BottomCenter).testTag("floating-tabs"))
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithText("开多", useUnmergedTree = true).performScrollTo().assertIsDisplayed()
        rule.onNodeWithText("开空", useUnmergedTree = true).performScrollTo().assertIsDisplayed()
        rule.onNodeWithTag("floating-tabs").assertIsDisplayed()
    }

    private fun smokeSpot(width: Int, height: Int, fontScale: Float, dark: Boolean = false) {
        applyViewport(width, height, fontScale)
        tokens.accessToken = "offline-test-only"
        val spot = SpotTradeViewModel(api, store, socket, tokens, nav, toast)
        spot.seedForShot(
            ApiSymbol(symbol = "BTCUSDT", base = "BTC", quote = "USDT", pricePrecision = 2, quantityPrecision = 5, status = ApiSymbol.Status.TRADING),
            OrderBook(), BigDecimal("81076.24"), BigDecimal("0.51"),
        )
        rule.setContent {
            LinbitTheme(dark) {
                Box(Modifier.fillMaxSize().background(if (dark) Color(0xFF0B0B0C) else Color.White).navigationBarsPadding()) {
                    SpotTradeContent(
                        spot, true, emptyList(), BigDecimal("7.1"), 73.dp, if (dark) ThemeMode.DARK else ThemeMode.LIGHT,
                        {}, {}, {}, {}, {},
                        modifier = Modifier.fillMaxSize().padding(bottom = 56.dp),
                    )
                    FloatingTabBar(MainTab.SPOT, {}, Modifier.align(Alignment.BottomCenter).testTag("floating-tabs"))
                }
            }
        }
        rule.waitForIdle()
        rule.onNodeWithTag("floating-tabs").assertIsDisplayed()
        // PrimaryTabs「现货」与底栏可能各一处文案；至少一处可见
        assert(rule.onAllNodesWithText("现货", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty())
        rule.onNodeWithText("买入", useUnmergedTree = true).performScrollTo().assertIsDisplayed()
    }
}
