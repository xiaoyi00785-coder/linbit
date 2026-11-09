package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.paper.PaperBalance
import click.cowa.linbit.data.paper.PaperBalances
import click.cowa.linbit.data.paper.validPositiveDecimal
import click.cowa.linbit.feature.assets.PaperAccountScreen
import click.cowa.linbit.feature.assets.PaperAccountViewModel
import click.cowa.linbit.feature.assets.PaperHistoryBody
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.CheckRow
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.DownTriangle
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.KvRow
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.NumberBox
import click.cowa.linbit.ui.components.PercentSlider
import click.cowa.linbit.ui.components.PillSegment
import click.cowa.linbit.ui.components.SelectBox
import click.cowa.linbit.ui.components.ThemedIcon
import click.cowa.linbit.ui.components.UpArrow
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LayoutAdapt
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal
import java.math.RoundingMode

/**
 * 模拟交易页（对照 EasiCoin「Demo Trading」未登录态，Ramus 实拍 2026-11-09）。
 *
 * 行情 / 盘口 / 资金费率 / K 线全部来自真实合约行情接口（FuturesTradeViewModel，只读）。
 * 下单只走独立 /paper/\* 契约（现货、市价、INDEX 全成或全拒），绝不调用真实合约下单。
 * 契约没有的能力（杠杆 / 保证金模式 / 限价 / 计划 / 止盈止损 / 跟单 / USDX 模拟合约）不伪造，
 * 见 mobile/android/docs/batch12-demo-trading-gaps.md。
 */
object DemoTradeLogic {
    enum class AmountUnit { QUANTITY, COST, VALUE }

    /** PaperAccount.orderTypes 契约 enum 目前只有 market；未登录 / 未加载时按契约默认值。 */
    fun supportedOrderTypes(orderTypes: List<String>?): Set<String> =
        (orderTypes?.takeIf { it.isNotEmpty() } ?: listOf("market")).map { it.lowercase() }.toSet()

    fun kindKey(kind: FuturesKind): String = when (kind) {
        FuturesKind.LIMIT -> "limit"
        FuturesKind.MARKET -> "market"
        FuturesKind.PLAN -> "trigger"
        FuturesKind.POST_ONLY -> "post_only"
    }

    /** 仅「数量」「成本」能映射到 /paper/orders（quantity / quoteOrderQty）；「价值」依赖杠杆，契约没有。 */
    fun unitSupported(unit: AmountUnit): Boolean = unit != AmountUnit.VALUE

    fun free(balances: PaperBalances?, asset: String): BigDecimal =
        balances?.items?.firstOrNull { it.asset.equals(asset, ignoreCase = true) }?.free?.toBigDecimalOrNull() ?: BigDecimal.ZERO

    /** 模拟持仓：除计价币外、合计大于零的虚拟余额；onlyBase 非空时只看当前交易对的基础币。 */
    fun holdings(balances: PaperBalances?, quote: String, onlyBase: String?): List<PaperBalance> =
        balances?.items.orEmpty().filter {
            !it.asset.equals(quote, ignoreCase = true) && (it.total.toBigDecimalOrNull()?.signum() ?: 0) > 0 &&
                (onlyBase == null || it.asset.equals(onlyBase, ignoreCase = true))
        }

    /** 可买：成本单位 = USDT 可用；数量单位 = 可用 / 最新价（向下取整到数量精度）。 */
    fun maxBuy(quoteFree: BigDecimal, price: BigDecimal, unit: AmountUnit, qtyPrec: Int): BigDecimal =
        if (unit == AmountUnit.QUANTITY) {
            if (price.signum() > 0) quoteFree.divide(price, qtyPrec.coerceAtLeast(0), RoundingMode.DOWN) else BigDecimal.ZERO
        } else quoteFree

    /** 滑杆比例换算：开仓按可买，平仓按基础币可用；结果去尾零，0 返回空串。 */
    fun percentAmount(isOpen: Boolean, unit: AmountUnit, percent: Float, quoteFree: BigDecimal, baseFree: BigDecimal, price: BigDecimal, qtyPrec: Int): String {
        val pct = BigDecimal(percent.coerceIn(0f, 1f).toDouble()).setScale(4, RoundingMode.HALF_UP)
        val raw = if (isOpen) maxBuy(quoteFree, price, unit, qtyPrec).multiply(pct) else baseFree.multiply(pct)
        val scale = if (isOpen && unit == AmountUnit.COST) 2 else qtyPrec.coerceAtLeast(0)
        val v = raw.setScale(scale, RoundingMode.DOWN)
        return if (v.signum() <= 0) "" else v.stripTrailingZeros().toPlainString()
    }

    data class Draft(val buy: Boolean, val amount: String, val byQuote: Boolean)

    /** 开仓 = 虚拟市价买入（成本 → quoteOrderQty，数量 → quantity）；平仓 = 虚拟市价卖出（只能按数量）。 */
    fun draft(isOpen: Boolean, unit: AmountUnit, text: String): Draft? {
        val amount = text.trim()
        if (!validPositiveDecimal(amount) || !unitSupported(unit)) return null
        return Draft(buy = isOpen, amount = amount, byQuote = isOpen && unit == AmountUnit.COST)
    }

    fun effectiveUnit(isOpen: Boolean, unit: AmountUnit): AmountUnit = if (isOpen) unit else AmountUnit.QUANTITY
}

/** 「模拟交易」顶栏：左标题，右黑底「返回实盘」（对照参考「Demo Trading / Go back」）。 */
@Composable
fun DemoTradingHeader(onGoLive: () -> Unit, modifier: Modifier = Modifier) {
    val p = LB.p
    Row(modifier.fillMaxWidth().height(44.dp), verticalAlignment = Alignment.CenterVertically) {
        Text("模拟交易", Modifier.weight(1f), style = lb(15.sp, FontWeight.Medium), color = p.textPrimary)
        Box(
            Modifier.clip(RoundedCornerShape(6.dp)).background(p.textPrimary).pressable(scale = 1f, onClick = onGoLive)
                .padding(horizontal = 16.dp, vertical = 6.dp).testTag("demo.goLive"),
            contentAlignment = Alignment.Center,
        ) { Text("返回实盘", style = lb(FontSize.body, FontWeight.Medium), color = p.bg) }
    }
}

/** 切换实盘 / 模拟时的过渡页（对照参考 Go back 后「Welcome to Live Trading」）。 */
@Composable
fun DemoWelcomeOverlay(target: String, modifier: Modifier = Modifier) {
    val p = LB.p
    Box(modifier.fillMaxSize().background(p.bg).testTag("demo.welcome"), contentAlignment = Alignment.CenterStart) {
        Column(Modifier.padding(horizontal = 28.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text("欢迎来到", style = lb(16.sp), color = p.textPrimary)
            Text(target, style = lb(40.sp, FontWeight.Bold), color = p.brand)
        }
    }
}

@Composable
fun DemoTradingContent(
    vm: FuturesTradeViewModel,
    paperVm: PaperAccountViewModel,
    loggedIn: Boolean,
    featureEnabled: Boolean,
    marketRows: List<click.cowa.linbit.data.MarketRow>,
    bottomInset: Dp,
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogin: () -> Unit,
    modifier: Modifier = Modifier,
    onPriceAlerts: () -> Unit = {},
    onFavorite: (String) -> Unit = {},
    /** 离屏证据专用：直接打开某个弹层。 */
    initialSheet: String? = null,
    initialChartOpen: Boolean = false,
    initialBottomTab: Int = 0,
) {
    val p = LB.p
    var sheet by remember { mutableStateOf(initialSheet) }
    var isOpen by rememberSaveable { mutableStateOf(true) }
    var kind by remember { mutableStateOf(FuturesKind.MARKET) }
    var unitPref by rememberSaveable { mutableStateOf(DemoTradeLogic.AmountUnit.COST) }
    var amount by rememberSaveable { mutableStateOf("") }
    var percent by remember { mutableStateOf(0f) }
    var bottomTab by remember { mutableIntStateOf(initialBottomTab) }
    var onlyCurrent by remember { mutableStateOf(false) }
    var bookMode by remember { mutableIntStateOf(0) }
    var bookPrecision by remember { mutableStateOf<BigDecimal?>(null) }
    var chartOpen by remember { mutableStateOf(initialChartOpen) }
    var review by remember { mutableStateOf(false) }
    var inputFailure by remember { mutableStateOf<String?>(null) }
    var skipConfirm by remember { mutableStateOf(false) }
    var showCalculator by remember { mutableStateOf(false) }

    val unit = DemoTradeLogic.effectiveUnit(isOpen, unitPref)
    val base = vm.base
    val quote = vm.quote
    val account = paperVm.account
    val balances = paperVm.balances
    val ready = loggedIn && featureEnabled && account != null && balances != null
    val quoteFree = DemoTradeLogic.free(balances, quote)
    val baseFree = DemoTradeLogic.free(balances, base)
    val price = vm.last
    val supported = DemoTradeLogic.supportedOrderTypes(account?.orderTypes)
    val holdings = DemoTradeLogic.holdings(balances, quote, if (onlyCurrent) base else null)

    LaunchedEffect(vm.symbol, onlyCurrent) { paperVm.setHistoryFilter(if (onlyCurrent) vm.symbol else null) }
    LaunchedEffect(paperVm.lastOrder?.orderId) { if (paperVm.lastOrder != null) { amount = ""; percent = 0f; review = false } }

    val step = bookPrecision ?: BigDecimal.ONE.movePointLeft(vm.pricePrec)
    val shownBook = remember(vm.book, step) { vm.book.grouped(step) }
    val bookView = if (unit == DemoTradeLogic.AmountUnit.QUANTITY) shownBook else click.cowa.linbit.data.trade.OrderBook(
        shownBook.bids.map { click.cowa.linbit.data.trade.BookLevel(it.price, it.qty.multiply(it.price)) },
        shownBook.asks.map { click.cowa.linbit.data.trade.BookLevel(it.price, it.qty.multiply(it.price)) },
    )
    val pageH = LayoutAdapt.pageH(LocalConfiguration.current.screenWidthDp)
    val gated: (() -> Unit) -> () -> Unit = { action -> { if (loggedIn) action() else onLogin() } }

    Box(modifier.fillMaxSize().background(p.bg)) {
        Column(Modifier.fillMaxSize()) {
            Column(Modifier.weight(1f).imePadding().verticalScroll(rememberScrollState())) {
                // 交易对头：BTCUSDT ▾ • / 永续 ±x%；右侧 主题 / K 线 / 更多
                Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp), verticalAlignment = Alignment.Top) {
                    Column(Modifier.pressable(scale = 1f) { sheet = "pairs" }.testTag("demo.pair")) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(vm.symbol, style = lb(18.sp, FontWeight.SemiBold), color = p.textPrimary)
                            DownTriangle(Modifier.padding(start = 2.dp), p.textSecondary, 14.dp)
                            Box(Modifier.padding(start = 2.dp).size(5.dp).clip(CircleShape).background(p.up))
                        }
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text("永续", style = lb(FontSize.small), color = p.textSecondary)
                            Text(" " + Fmt.percent(vm.changePct), style = lbNum(FontSize.small), color = if (vm.changePct.signum() < 0) p.down else p.up)
                        }
                    }
                    Box(Modifier.weight(1f))
                    Row(Modifier.padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                        LbIcon(R.drawable.ic_sun, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { sheet = "theme" })
                        LbIcon(R.drawable.ic_candles, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { chartOpen = !chartOpen })
                        LbIcon(R.drawable.ic_more, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { sheet = "tools" })
                    }
                }
                // 保证金模式 / 杠杆（未登录 -- 跳登录）；登录后显示独立虚拟账户
                Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp), verticalAlignment = Alignment.Top) {
                    Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                        if (!loggedIn) {
                            ChipButton("全仓 / 合仓", Modifier.height(24.dp), onClick = onLogin)
                            ChipButton("--", Modifier.height(24.dp).width(44.dp), color = p.up, bg = p.upTint, onClick = onLogin)
                            ChipButton("--", Modifier.height(24.dp).width(44.dp), color = p.down, bg = p.downTint, onClick = onLogin)
                        } else {
                            ChipButton(
                                if (!featureEnabled) "模拟服务未启用" else account?.let { "虚拟账户 · 第${it.generation}代" } ?: "虚拟账户加载中",
                                Modifier.height(24.dp),
                            ) { sheet = "account" }
                        }
                    }
                    Column(Modifier.width(138.dp).height(24.dp), horizontalAlignment = Alignment.End) {
                        Text("资金费率(8H) / 倒计时", style = lb(10.sp).copy(lineHeight = 12.sp), color = p.textTertiary, maxLines = 1)
                        Text("${Fmt.amount(vm.fundingRate, 4)}% / ${vm.fundingCountdown.also { vm.nowMs }}", style = lbNum(10.sp).copy(lineHeight = 12.sp), color = p.textPrimary, maxLines = 1)
                    }
                }
                Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(7.dp)) {
                        PillSegment(
                            listOf("开仓", "平仓"), if (isOpen) 0 else 1, { isOpen = it == 0; amount = ""; percent = 0f; inputFailure = null },
                            activeColors = listOf(p.up, p.up), activeText = androidx.compose.ui.graphics.Color.White,
                        )
                        SelectBox(kind.title, height = 28.dp) { sheet = "kind" }
                        val unitLabel = when (unit) { DemoTradeLogic.AmountUnit.QUANTITY -> "数量"; DemoTradeLogic.AmountUnit.COST -> "成本"; DemoTradeLogic.AmountUnit.VALUE -> "价值" }
                        NumberBox(
                            unitLabel, if (unit == DemoTradeLogic.AmountUnit.QUANTITY) base else quote, amount,
                            { amount = it; percent = 0f; inputFailure = null },
                            readOnly = paperVm.orderSubmitting,
                            onUnitClick = if (isOpen) ({ sheet = "unit" }) else null,
                            testTag = "demo.amount",
                        )
                        PercentSlider(percent, {
                            percent = it
                            if (ready) amount = DemoTradeLogic.percentAmount(isOpen, unit, it, quoteFree, baseFree, price, vm.qtyPrec)
                        }, Modifier.padding(horizontal = 2.dp, vertical = 12.dp), tint = p.textPrimary)
                        // 参考未登录态有 TP/SL 勾选；模拟契约无止盈止损，登录后不展示。
                        if (!loggedIn && isOpen) CheckRow(false, "止盈/止损", dashed = false) { onLogin() }
                        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            KvRow("可用", if (ready) "${Fmt.amount(quoteFree, 2)} $quote" else "-- $quote", trailing = {
                                Row(Modifier.padding(start = 4.dp).pressable(scale = 1f, onClick = gated { sheet = "account" }).testTag("demo.funds"), verticalAlignment = Alignment.CenterVertically) {
                                    ThemedIcon(R.drawable.ic_mc_plus_square_light, R.drawable.ic_mc_plus_square_dark, 14.dp)
                                    Text(" 资金", style = lb(FontSize.small), color = p.textPrimary)
                                }
                            })
                            val buyUnit = if (unit == DemoTradeLogic.AmountUnit.QUANTITY) base else quote
                            KvRow("可买", if (ready) "${Fmt.amount(DemoTradeLogic.maxBuy(quoteFree, price, unit, vm.qtyPrec), if (unit == DemoTradeLogic.AmountUnit.QUANTITY) vm.qtyPrec else 2)} $buyUnit" else "-- $buyUnit")
                            KvRow("可卖", if (ready) "${Fmt.amount(baseFree, vm.qtyPrec.coerceAtLeast(2))} $base" else "-- $base")
                        }
                        when {
                            !loggedIn -> {
                                DemoLoginButton(true, quote, onLogin)
                                DemoLoginButton(false, quote, onLogin)
                            }
                            !featureEnabled -> {
                                LbButton("模拟服务未启用", enabled = false, height = 40.dp) {}
                                Text(paperVm.featureState.value.failure ?: "服务端未开启模拟交易（/app/features paperTrading=false），不会用真实账户代替。", style = lb(FontSize.small), color = p.textSecondary)
                            }
                            else -> {
                                val buy = isOpen
                                LbButton(
                                    if (buy) "模拟买入" else "模拟卖出", kind = if (buy) ButtonKind.BUY else ButtonKind.SELL, height = 40.dp,
                                    enabled = ready && !paperVm.orderSubmitting, loading = paperVm.orderSubmitting, testTag = "demo.submit",
                                ) {
                                    val d = DemoTradeLogic.draft(isOpen, unit, amount)
                                    inputFailure = if (d == null) "请输入大于零的${if (unit == DemoTradeLogic.AmountUnit.QUANTITY) "数量" else "成本"}"
                                    else paperVm.orderDraft(d.buy, d.amount, vm.symbol, d.byQuote)
                                    if (inputFailure == null) { if (skipConfirm) inputFailure = paperVm.confirmOrder() else review = true }
                                }
                                KvRow(if (unit == DemoTradeLogic.AmountUnit.QUANTITY) "数量" else "成本", "${amount.ifBlank { "0" }} ${if (unit == DemoTradeLogic.AmountUnit.QUANTITY) base else quote}", kDashed = true)
                                Text("虚拟市价单按服务端外部指数（INDEX）全部成交或拒绝，不影响真实资产。", style = lb(11.sp), color = p.textTertiary)
                                (inputFailure ?: paperVm.orderFailure?.takeIf { !review })?.let { Text(it, style = lb(FontSize.small), color = p.down) }
                                if (paperVm.failure != null && account == null) Text("虚拟账户加载失败：${paperVm.failure}", Modifier.pressable(scale = 1f) { paperVm.load() }, style = lb(FontSize.small), color = p.down)
                            }
                        }
                    }
                    Column(Modifier.width(138.dp).padding(top = 8.dp)) {
                        OrderBookView(
                            book = bookView, rows = 7, rowHeight = 18.dp, pricePrec = vm.pricePrec, quote = quote,
                            baseUnit = if (unit == DemoTradeLogic.AmountUnit.QUANTITY) base else quote,
                            mid = { FuturesMid(vm.last, vm.mark, vm.pricePrec, vm.changePct.signum() < 0) {} },
                            mode = bookMode, precisionText = step.stripTrailingZeros().toPlainString(),
                            onPick = {}, onMode = { sheet = "bookMode" }, onPrecision = { sheet = "precision" },
                        )
                    }
                }
                // 仓位 / 委托 + 历史
                Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 18.dp).height(40.dp), verticalAlignment = Alignment.CenterVertically) {
                    Row(Modifier.weight(1f).horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(18.dp), verticalAlignment = Alignment.CenterVertically) {
                        listOf("仓位 (${if (ready) holdings.size else 0})", "委托 (0)").forEachIndexed { i, t ->
                            Text(t, Modifier.pressable(scale = 1f) { bottomTab = i }, style = lb(FontSize.label, if (bottomTab == i) FontWeight.Medium else FontWeight.Normal), color = if (bottomTab == i) p.textPrimary else p.textSecondary)
                        }
                    }
                    Spacer(Modifier.width(18.dp))
                    Box(
                        Modifier.size(48.dp).pressable(scale = 1f, onClick = gated { paperVm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS); sheet = "history" })
                            .semantics { contentDescription = "模拟交易历史" },
                        contentAlignment = Alignment.Center,
                    ) { ThemedIcon(R.drawable.ic_duo_clock_fill_light, R.drawable.ic_duo_clock_fill_dark, 20.dp) }
                }
                HorizontalDivider(color = p.divider)
                Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    CheckRow(onlyCurrent, "只看当前") { onlyCurrent = it }
                    Box(Modifier.weight(1f))
                    // 参考未登录态有「Close All」；模拟现货无仓位平仓契约，登录后不展示
                    if (!loggedIn) ChipButton(if (bottomTab == 0) "一键平仓" else "全部撤销", Modifier.height(28.dp).width(80.dp), color = p.textPrimary, onClick = onLogin)
                }
                when {
                    !loggedIn -> EmptyState(if (bottomTab == 0) "登录后查看模拟仓位" else "登录后查看模拟委托") {
                        ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin)
                    }
                    bottomTab == 0 && !ready -> EmptyState(if (!featureEnabled) "模拟服务未启用" else paperVm.failure ?: "虚拟账户加载中")
                    bottomTab == 0 && holdings.isEmpty() -> EmptyState("暂无模拟持仓")
                    bottomTab == 0 -> holdings.forEach { h -> DemoHoldingRow(h) }
                    else -> EmptyState("暂无当前委托\n模拟单为市价，全部成交或拒绝，不会挂单") {
                        ChipButton("订单 / 成交记录", Modifier.width(140.dp).height(36.dp)) { paperVm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS); sheet = "history" }
                    }
                }
                Box(Modifier.height(24.dp))
            }
            // 底部 K 线条（参考「BTCUSDX Chart ▴」）
            HorizontalDivider(color = p.divider)
            Row(
                Modifier.fillMaxWidth().height(40.dp).pressable(scale = 1f) { chartOpen = !chartOpen }.padding(horizontal = Spacing.pageH).testTag("demo.chartBar"),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("${vm.symbol} 图表", Modifier.weight(1f), style = lb(FontSize.label), color = p.textPrimary)
                if (chartOpen) DownTriangle(tint = p.textSecondary, size = 14.dp) else UpArrow(tint = p.textSecondary)
            }
            if (chartOpen) Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 8.dp)) {
                TradeChartPanel(
                    symbol = vm.symbol, pricePrec = vm.pricePrec, showTrades = false,
                    loadCandles = { iv -> vm.loadKlines(iv) },
                    loadTrades = { vm.loadMarketTrades() },
                )
            }
            Box(Modifier.height(bottomInset))
        }
        if (sheet == "account") Box(Modifier.fillMaxSize().background(p.bg).padding(bottom = bottomInset)) {
            PaperAccountScreen(onBack = { sheet = null }, onLogin = onLogin, vm = paperVm)
        }

    when (sheet) {
        "kind" -> LbSheet("委托类型", { sheet = null }) {
            Column(Modifier.padding(bottom = 20.dp)) {
                FuturesKind.entries.forEach { k ->
                    val ok = DemoTradeLogic.kindKey(k) in supported
                    DemoOptionRow(
                        when (k) { FuturesKind.LIMIT -> R.drawable.ic_ot_limit; FuturesKind.MARKET -> R.drawable.ic_ot_market; FuturesKind.PLAN -> R.drawable.ic_ot_trigger; FuturesKind.POST_ONLY -> R.drawable.ic_ot_postonly },
                        k.title, if (ok) null else "模拟盘暂不支持", kind == k, ok,
                    ) { kind = k; sheet = null }
                }
            }
        }
        "unit" -> DemoUnitSheet(base, quote, unitPref, { unitPref = it; amount = ""; percent = 0f }) { sheet = null }
        "bookMode" -> BookModeSheet(bookMode, { bookMode = it }) { sheet = null }
        "pairs" -> PairPickerSheet(marketRows, vm.symbol, false, { vm.switchSymbol(it); amount = ""; percent = 0f }, { sheet = null })
        "precision" -> PrecisionSheet(vm.pricePrec, bookPrecision, { bookPrecision = it }, { sheet = null })
        "theme" -> ThemeModeSheet(themeMode, onTheme, { sheet = null })
        "tools" -> ToolsSheet(vm.toast::show, { sheet = null }, exclude = setOf("模拟交易")) { tool ->
            when (tool) {
                "计算器" -> showCalculator = true
                "价格预警" -> { sheet = null; onPriceAlerts() }
                "添加自选" -> { onFavorite(vm.symbol); vm.toast.show("已添加到自选") }
                else -> sheet = tool
            }
        }
        "history" -> LbSheet("虚拟订单 / 成交记录", { sheet = null }) { PaperHistoryBody(paperVm) }
    }
    if (sheet == "交易偏好") TradePreferencesSheet(themeMode, onTheme, skipConfirm, { skipConfirm = it }) { sheet = null }
    if (sheet in listOf("仓位档位介绍", "历史资金费率", "历史标记价格", "风险准备金", "新手教学")) TradeInfoSheet(sheet!!, vm) { sheet = null }
    if (showCalculator) CalculatorSheet(vm) { showCalculator = false }
    if (review && paperVm.orderAmount.isNotEmpty()) {
        val buy = paperVm.orderBuy
        LbSheet("模拟${if (buy) "买入" else "卖出"} ${paperVm.orderSymbol}", { if (!paperVm.orderSubmitting) { paperVm.cancelOrderReview(); review = false } }, showClose = !paperVm.orderSubmitting) {
            Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("这是模拟交易，不使用、划扣或变动真实资金。", style = lb(FontSize.body, FontWeight.SemiBold), color = p.down)
                KvRow("虚拟账户", "${account?.accountId.orEmpty()} · 第${account?.generation ?: 0}代")
                KvRow("交易对 / 方式", "${paperVm.orderSymbol} · 市价${if (buy) "买入" else "卖出"}")
                KvRow(if (paperVm.orderByQuote) "成本" else "数量", "${paperVm.orderAmount} ${if (paperVm.orderByQuote) quote else base}")
                KvRow("价格来源", "外部指数（INDEX）")
                paperVm.orderFailure?.let { Text("操作未完成：$it", style = lb(FontSize.body), color = p.down) }
                paperVm.orderRetryAfter?.let { Text("请在${it}秒后重试", style = lb(FontSize.body), color = p.down) }
                LbButton(
                    if (paperVm.orderSubmitting) "提交中…" else "确认模拟${if (buy) "买入" else "卖出"}", kind = if (buy) ButtonKind.BUY else ButtonKind.SELL,
                    enabled = !paperVm.orderSubmitting, testTag = "demo.confirm",
                ) {
                    val failure = paperVm.confirmOrder()
                    if (failure != null) { inputFailure = failure; review = false }
                }
                LbButton("取消", kind = ButtonKind.OUTLINE, enabled = !paperVm.orderSubmitting) { paperVm.cancelOrderReview(); review = false }
            }
        }
    }
    }
}

@Composable
private fun DemoLoginButton(long: Boolean, quote: String, onLogin: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(3.dp)) {
        LbButton("登录", kind = if (long) ButtonKind.BUY else ButtonKind.SELL, height = 40.dp, testTag = if (long) "demo.login.long" else "demo.login.short", onClick = onLogin)
        KvRow("预估强平价", "-- $quote", kDashed = true)
        KvRow("成本", "-- $quote", kDashed = true)
    }
}

@Composable
private fun DemoHoldingRow(b: PaperBalance) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(b.asset, style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            Text("虚拟持仓", Modifier.clip(RoundedCornerShape(3.dp)).background(p.fill).padding(horizontal = 4.dp, vertical = 1.dp), style = lb(FontSize.small), color = p.textSecondary)
        }
        Row {
            KVCell("可用", b.free, Modifier.weight(1f))
            KVCell("合计", b.total, Modifier.weight(1f), androidx.compose.ui.text.style.TextAlign.Center)
            KVCell("估值(USD)", b.valuationUsd ?: "--", Modifier.weight(1f), androidx.compose.ui.text.style.TextAlign.End)
        }
    }
    HorizontalDivider(Modifier.padding(horizontal = Spacing.pageH), color = p.divider)
}

/** 委托类型行：契约不支持的类型置灰并标注原因，不可选。 */
@Composable
private fun DemoOptionRow(icon: Int, title: String, note: String?, selected: Boolean, enabled: Boolean, onClick: () -> Unit) {
    val p = LB.p
    Row(
        Modifier.fillMaxWidth().alpha(if (enabled) 1f else 0.4f).pressable(enabled = enabled, scale = 1f, onClick = onClick)
            .padding(horizontal = Spacing.pageH, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        LbIcon(icon, 20.dp, p.textPrimary)
        Column(Modifier.weight(1f).padding(start = 14.dp)) {
            Text(title, style = lb(FontSize.body), color = p.textPrimary)
            if (note != null) Text(note, style = lb(FontSize.small), color = p.textTertiary)
        }
        if (selected) LbIcon(R.drawable.ic_check, 18.dp, p.textPrimary)
    }
}

/** 合约单位设置（参考 Futures Unit Settings：数量 / 成本 / 价值 三张说明卡）。 */
@Composable
fun DemoUnitSheet(base: String, quote: String, current: DemoTradeLogic.AmountUnit, onPick: (DemoTradeLogic.AmountUnit) -> Unit, onClose: () -> Unit) {
    val p = LB.p
    LbSheet("合约单位设置", onClose) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 8.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            listOf(
                Triple(DemoTradeLogic.AmountUnit.QUANTITY, "数量-$base", "按标的资产数量下单，单位为 $base"),
                Triple(DemoTradeLogic.AmountUnit.COST, "成本-$quote", "按实际占用的资金下单，即本次虚拟买入花费的 $quote"),
                Triple(DemoTradeLogic.AmountUnit.VALUE, "价值-$quote", "按杠杆后的合约价值下单；模拟账户为现货 1x，暂不支持"),
            ).forEach { (u, title, desc) ->
                val ok = DemoTradeLogic.unitSupported(u)
                Column(
                    Modifier.fillMaxWidth().alpha(if (ok) 1f else 0.45f).clip(RoundedCornerShape(Radius.md))
                        .border(if (u == current) 1.5.dp else 1.dp, if (u == current) p.textPrimary else p.border, RoundedCornerShape(Radius.md))
                        .pressable(enabled = ok, scale = 1f) { onPick(u); onClose() }.padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text(title, style = lb(FontSize.title, FontWeight.Medium), color = p.textPrimary)
                    Text(desc, style = lb(FontSize.small), color = p.textSecondary)
                }
            }
        }
    }
}
