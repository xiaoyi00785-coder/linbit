package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiFuturesAlgoOrder
import click.cowa.linbit.core.generated.ApiFuturesOrder
import click.cowa.linbit.core.generated.ApiFuturesPosition
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.trade.BookLevel
import click.cowa.linbit.data.trade.OrderBook
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
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.SelectBox
import click.cowa.linbit.ui.components.SheetOptionRow
import click.cowa.linbit.ui.components.ThemedIcon
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.components.UpArrow
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal

/** 合约 Tab 根（对照图 ref/11）：合约 / 事件合约 / TradFi / 模拟交易。 */
@Composable
fun FuturesRootScreen(
    bottomInset: Dp,
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogin: () -> Unit,
    onDeposit: () -> Unit,
    onTransfer: () -> Unit,
    onHistory: () -> Unit,
    modifier: Modifier = Modifier,
    initialTop: Int = 0,
    autoStart: Boolean = true,
    vm: FuturesTradeViewModel = hiltViewModel(),
) {
    val p = LB.p
    var top by remember { mutableStateOf(initialTop) }
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val market by vm.market.collectAsStateWithLifecycle()
    val nav by vm.symbolFlow.collectAsStateWithLifecycle()
    DisposableEffect(nav, top) { if (top == 0 && autoStart) vm.start(); onDispose { vm.stop() } }
    Column(modifier.fillMaxSize().background(p.bg)) {
        Row(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            PrimaryTabs(listOf("合约", "事件合约", "TradFi", "模拟交易"), top, { top = it }, size = 16.sp, spacing = 16.dp)
        }
        HorizontalDivider(color = p.divider)
        when (top) {
            0 -> FuturesTradeContent(vm, loggedIn, market.futures, bottomInset, themeMode, onTheme, onLogin, onDeposit, onTransfer, onHistory, Modifier.weight(1f))
            1 -> ComingSoon("事件合约", "猜涨跌，到期按指数价结算，敬请期待", bottomInset)
            2 -> ComingSoon("TradFi", "股票、贵金属等传统金融资产交易即将上线", bottomInset)
            else -> ComingSoon("模拟交易", "使用模拟资金体验合约交易，即将上线", bottomInset)
        }
    }
}

@Composable
private fun ComingSoon(title: String, note: String, bottomInset: Dp) {
    val p = LB.p
    Column(Modifier.fillMaxSize().padding(bottom = bottomInset), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        EmptyState(title) { Text(note, style = lb(FontSize.small), color = p.textSecondary, textAlign = TextAlign.Center) }
    }
}

@Composable
fun FuturesTradeContent(
    vm: FuturesTradeViewModel,
    loggedIn: Boolean,
    marketRows: List<click.cowa.linbit.data.MarketRow>,
    bottomInset: Dp,
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogin: () -> Unit,
    onDeposit: () -> Unit,
    onTransfer: () -> Unit,
    onHistory: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val p = LB.p
    var sheet by remember { mutableStateOf<String?>(null) }
    var confirmLong by remember { mutableStateOf(true) }
    var skipConfirm by remember { mutableStateOf(false) }
    var bottomTab by remember { mutableStateOf(0) }
    var orderFilter by remember { mutableStateOf(0) }
    var couponTab by remember { mutableStateOf(0) }
    var closeTarget by remember { mutableStateOf<ApiFuturesPosition?>(null) }
    var cancelTarget by remember { mutableStateOf<ApiFuturesOrder?>(null) }
    var cancelAlgoTarget by remember { mutableStateOf<ApiFuturesAlgoOrder?>(null) }
    var closeAllAsk by remember { mutableStateOf(false) }

    val step = vm.bookPrecision ?: BigDecimal.ONE.movePointLeft(vm.pricePrec)
    val shownBook = remember(vm.book, step, vm.byQty) {
        val g = vm.book.grouped(step)
        g
    }
    val bookView = if (vm.byQty) shownBook else OrderBook(
        shownBook.bids.map { BookLevel(it.price, it.qty.multiply(it.price)) },
        shownBook.asks.map { BookLevel(it.price, it.qty.multiply(it.price)) },
    )

    Column(modifier.fillMaxSize().background(p.bg)) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            // 交易对头：BTCUSDT ▾ •  /  永续 +0.66%
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 8.dp), verticalAlignment = Alignment.Top) {
                Column(Modifier.pressable(scale = 1f) { sheet = "pairs" }) {
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
                    LbIcon(R.drawable.ic_candles, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { vm.chartOpen = !vm.chartOpen })
                    LbIcon(R.drawable.ic_more, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { sheet = "tools" })
                }
            }
            // 全仓 / 合仓 · 100x · 100x · 资金费率
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 8.dp), verticalAlignment = Alignment.Top) {
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    ChipButton("${if (vm.cross) "全仓" else "逐仓"} / ${if (vm.hedge) "分仓" else "合仓"}", Modifier.height(24.dp)) { sheet = "mode" }
                    ChipButton("${vm.leverage}x", Modifier.height(24.dp), color = p.up, bg = p.upTint) { sheet = "leverage" }
                    ChipButton("${vm.leverage}x", Modifier.height(24.dp), color = p.down, bg = p.downTint) { sheet = "leverage" }
                }
            }
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                // 左：表单
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    PillSegment(
                        listOf("开仓", "平仓"), if (vm.isOpen) 0 else 1, { vm.isOpen = it == 0; vm.applyPercent(0f) },
                        activeColors = listOf(p.up, p.up), activeText = Color.White,
                    )
                    SelectBox(vm.kind.title, height = 34.dp) { sheet = "kind" }
                    if (vm.isPlan) {
                        NumberBox("触发价", vm.quote, vm.triggerText, { vm.triggerText = it })
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text("最新价格触发", style = lb(FontSize.small), color = p.textSecondary)
                            DownTriangle(tint = p.textSecondary, size = 12.dp)
                            Box(Modifier.weight(1f))
                            CheckRow(vm.planMarket, "市价") { vm.planMarket = it }
                        }
                    }
                    if (!vm.marketLike) NumberBox("价格", vm.quote, vm.priceText, { vm.priceText = it })
                    if (vm.isOpen) {
                        if (vm.byQty) NumberBox("数量", vm.base, vm.qtyText, { vm.qtyText = it; vm.percent = 0f }, onUnitClick = { vm.byQty = false; vm.percent = 0f })
                        else NumberBox("成本", vm.quote, vm.costText, { vm.costText = it; vm.percent = 0f }, onUnitClick = { vm.byQty = true; vm.percent = 0f })
                    } else {
                        Row(Modifier.fillMaxWidth().height(38.dp).clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(horizontal = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text("数量", Modifier.weight(1f), style = lb(FontSize.small), color = p.textSecondary)
                            Text("${Fmt.amount(vm.qty, vm.qtyPrec)} ${vm.base}", style = lbNum(FontSize.label), color = p.textPrimary)
                        }
                    }
                    PercentSlider(vm.percent, { vm.applyPercent(it) }, Modifier.padding(horizontal = 2.dp, vertical = if (vm.marketLike) 12.dp else 0.dp), tint = p.textPrimary)
                    if (vm.isOpen) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            CheckRow(vm.tpsl, "止盈/止损", dashed = false) { vm.tpsl = it }
                            Box(Modifier.weight(1f))
                            if (vm.tpsl) Text("高级设置 ›", Modifier.pressable(scale = 1f) { vm.toast.show("高级止盈止损即将开放") }, style = lb(FontSize.small), color = p.textSecondary)
                        }
                        if (vm.tpsl) {
                            NumberBox("止盈", vm.quote, vm.tpText, { vm.tpText = it })
                            NumberBox("止损", vm.quote, vm.slText, { vm.slText = it })
                        }
                    }
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        KvRow("可用", if (loggedIn && vm.account != null) "${Fmt.amount(vm.available, 2)} ${vm.quote}" else "-- ${vm.quote}", trailing = {
                            ThemedIcon(R.drawable.ic_mc_plus_square_light, R.drawable.ic_mc_plus_square_dark, 14.dp, Modifier.padding(start = 4.dp).pressable(scale = 1f, onClick = onTransfer))
                        })
                        KvRow("可买", "${Fmt.amount(if (vm.isOpen) vm.maxOpenQty else BigDecimal.ZERO, vm.qtyPrec.coerceAtMost(2))} ${if (vm.byQty) vm.base else vm.quote}")
                        KvRow("可卖", "${Fmt.amount(BigDecimal.ZERO, 2)} ${if (vm.byQty) vm.base else vm.quote}")
                    }
                    SideButton(vm, loggedIn, long = true) {
                        if (!loggedIn) onLogin() else vm.validation?.let { vm.toast.show(it, ToastCenter.Kind.ERROR) } ?: run { confirmLong = true; if (skipConfirm) vm.place(true) else sheet = "confirm" }
                    }
                    SideButton(vm, loggedIn, long = false) {
                        if (!loggedIn) onLogin() else vm.validation?.let { vm.toast.show(it, ToastCenter.Kind.ERROR) } ?: run { confirmLong = false; if (skipConfirm) vm.place(false) else sheet = "confirm" }
                    }
                }
                // 右：资金费率 + 盘口
                Column(Modifier.width(138.dp)) {
                    Column(Modifier.fillMaxWidth().height(40.dp), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.Center) {
                        Text("资金费率(8H) / 倒计时", style = lb(10.sp), color = p.textTertiary, maxLines = 1)
                        Text("${Fmt.amount(vm.fundingRate, 4)}% / ${vm.fundingCountdown}", style = lbNum(FontSize.small), color = p.textPrimary, maxLines = 1)
                    }
                    OrderBookView(
                        book = bookView, rows = 7, rowHeight = 18.dp, pricePrec = vm.pricePrec, quote = vm.quote, baseUnit = if (vm.byQty) vm.base else vm.quote,
                        mid = { FuturesMid(vm.last, vm.mark, vm.pricePrec, vm.changePct.signum() < 0) { vm.priceText = vm.fmt(vm.last, vm.pricePrec) } },
                        mode = vm.bookMode, precisionText = step.stripTrailingZeros().toPlainString(),
                        onPick = { if (vm.kind != FuturesKind.MARKET) vm.priceText = vm.fmt(it, vm.pricePrec) },
                        onMode = { sheet = "bookMode" }, onPrecision = { sheet = "precision" },
                    )
                }
            }
            // 仓位 / 委托 / 跟单 / 卡券
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 20.dp).height(40.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                listOf("仓位 (${vm.positions.size})", "委托 (${vm.openOrders.size + vm.planAlgos.size + vm.tpslAlgos.size})", "跟单 (0)", "卡券 (${vm.bonus.size})").forEachIndexed { i, t ->
                    Text(t, Modifier.pressable(scale = 1f) { bottomTab = i }, style = lb(FontSize.label, if (bottomTab == i) FontWeight.Medium else FontWeight.Normal), color = if (bottomTab == i) p.textPrimary else p.textSecondary)
                }
                Box(Modifier.weight(1f))
                ThemedIcon(R.drawable.ic_duo_clock_fill_light, R.drawable.ic_duo_clock_fill_dark, 20.dp, Modifier.pressable(scale = 1f, onClick = onHistory))
            }
            HorizontalDivider(color = p.divider)
            if (bottomTab < 2) Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                CheckRow(vm.onlyCurrent, "只看当前") { vm.onlyCurrent = it }
                Box(Modifier.weight(1f))
                if (bottomTab == 0) ChipButton("一键平仓", Modifier.height(28.dp).width(80.dp), color = p.textPrimary) { if (vm.shownPositions.isNotEmpty()) closeAllAsk = true }
                else ChipButton("全部撤销", Modifier.height(28.dp).width(80.dp), color = p.textPrimary) { if (vm.shownOrders.isNotEmpty()) vm.cancelAll() }
            }
            when (bottomTab) {
                0 -> if (!loggedIn) EmptyState("登录后查看仓位") { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) } else {
                    LaunchedEffect(Unit) { vm.loadTodayPnl() }
                    TodayPnlCard(vm)
                    if (vm.shownPositions.isEmpty()) EmptyState("可用余额：${Fmt.amount(vm.available, 2)} ${vm.quote}\n划转资金到合约账户开始交易，或尝试0风险模拟交易") {
                        Row(Modifier.padding(horizontal = Spacing.pageH), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            ChipButton("充值", Modifier.weight(1f).height(36.dp), onClick = onDeposit)
                            ChipButton("模拟盘", Modifier.weight(1f).height(36.dp)) { vm.toast.show("模拟交易即将上线") }
                            ChipButton("跟单交易", Modifier.weight(1f).height(36.dp)) { vm.toast.show("跟单交易即将开放") }
                        }
                    } else vm.shownPositions.forEach { pos ->
                        PositionCard(pos, vm.livePnl(pos), { vm.toast.show("止盈止损设置即将开放") }, { vm.toast.show("保证金调整即将开放") }, { closeTarget = pos })
                        Box(Modifier.fillMaxWidth().height(8.dp).background(p.fill))
                    }
                }
                1 -> {
                    Row(Modifier.padding(horizontal = Spacing.pageH).padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        listOf("普通委托", "止盈止损", "计划委托").forEachIndexed { i, t ->
                            Text(
                                t, Modifier.clip(RoundedCornerShape(Radius.xs)).background(if (orderFilter == i) p.fill else Color.Transparent).pressable(scale = 1f) { orderFilter = i }
                                    .padding(horizontal = 10.dp, vertical = 4.dp),
                                style = lb(FontSize.small, if (orderFilter == i) FontWeight.Medium else FontWeight.Normal), color = if (orderFilter == i) p.textPrimary else p.textSecondary,
                            )
                        }
                    }
                    if (!loggedIn) EmptyState("登录后查看委托") { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) } else when (orderFilter) {
                        0 -> {
                            val l = vm.shownOrders.filter { it.source != "tpsl" }
                            if (l.isEmpty()) EmptyState("暂无委托") else l.forEach { o -> FuturesOrderRow(o) { cancelTarget = o }; HorizontalDivider(Modifier.padding(horizontal = Spacing.pageH), color = p.divider) }
                        }
                        1 -> if (vm.shownTpsl.isEmpty()) EmptyState("暂无止盈止损") else vm.shownTpsl.forEach { a -> AlgoOrderRow(a) { cancelAlgoTarget = a }; HorizontalDivider(Modifier.padding(horizontal = Spacing.pageH), color = p.divider) }
                        else -> if (vm.shownPlans.isEmpty()) EmptyState("暂无计划委托") else vm.shownPlans.forEach { a -> AlgoOrderRow(a) { cancelAlgoTarget = a }; HorizontalDivider(Modifier.padding(horizontal = Spacing.pageH), color = p.divider) }
                    }
                }
                2 -> EmptyState("暂无跟单") { ChipButton("跟单交易", Modifier.width(120.dp).height(36.dp)) { vm.toast.show("跟单交易即将开放") } }
                else -> Coupons(vm, couponTab) { couponTab = it }
            }
            Box(Modifier.height(24.dp))
        }
        // 底部 K 线条（对照图 ref/11 底部「BTCUSDT 图表 ▴」）
        HorizontalDivider(color = p.divider)
        Row(
            Modifier.fillMaxWidth().height(40.dp).pressable(scale = 1f) { vm.chartOpen = !vm.chartOpen }.padding(horizontal = Spacing.pageH),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("${vm.symbol} 图表", Modifier.weight(1f), style = lb(FontSize.label), color = p.textPrimary)
            if (vm.chartOpen) DownTriangle(tint = p.textSecondary, size = 14.dp) else UpArrow(tint = p.textSecondary)
        }
        if (vm.chartOpen) Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            KvRow("标记价格", Fmt.priceAuto(vm.mark))
            KvRow("指数价格", Fmt.priceAuto(Dec.parse(vm.ticker?.indexPrice)))
            KvRow("24h 成交额", Fmt.volumeCN(Dec.parse(vm.ticker?.turnover24h)))
            Text("K 线图表即将开放", style = lb(FontSize.small), color = p.textTertiary)
        }
        Box(Modifier.height(bottomInset))
    }

    when (sheet) {
        "kind" -> LbSheet("委托类型", { sheet = null }) {
            Column(Modifier.padding(bottom = 20.dp)) {
                FuturesKind.entries.forEach { k -> SheetOptionRow(k.title, vm.kind == k) { vm.kind = k; sheet = null } }
            }
        }
        "bookMode" -> BookModeSheet(vm.bookMode, { vm.bookMode = it }) { sheet = null }
        "pairs" -> PairPickerSheet(marketRows, vm.symbol, false, { vm.switchSymbol(it) }, { sheet = null })
        "precision" -> PrecisionSheet(vm.pricePrec, vm.bookPrecision, { vm.bookPrecision = it }, { sheet = null })
        "theme" -> ThemeModeSheet(themeMode, onTheme, { sheet = null })
        "tools" -> ToolsSheet(vm.toast::show) { sheet = null }
        "mode" -> MarginModeSheet(vm) { sheet = null }
        "leverage" -> LeverageSheet(vm) { sheet = null }
        "confirm" -> {
            val long = confirmLong
            val dir = if (vm.isOpen) (if (long) "开多" else "开空") else (if (long) "平空" else "平多")
            ConfirmSheet(
                title = "$dir ${vm.symbol}",
                rows = buildList {
                    add(Triple("委托类型", vm.kind.title, null))
                    add(Triple("保证金模式", "${if (vm.cross) "全仓" else "逐仓"} ${vm.leverage}x · ${if (vm.hedge) "分仓" else "合仓"}", null))
                    if (vm.isPlan) add(Triple("触发价", "${vm.triggerText} ${vm.quote}", null))
                    add(Triple("价格", if (vm.marketLike) "市价" else "${vm.priceText} ${vm.quote}", null))
                    add(Triple("数量", "${Fmt.amount(vm.qty, vm.qtyPrec)} ${vm.base}", null))
                    add(Triple("预估手续费", "${Fmt.amount(vm.qty.multiply(vm.orderPrice).multiply(Dec.parseOrNull(vm.contract?.takerFeeRate) ?: BigDecimal("0.0005")), 4)} ${vm.quote}", null))
                },
                note = if (vm.marketLike) "市价单将以盘口最优价格立即成交，实际成交价可能与预估存在偏差" else null,
                buyColor = long, confirmText = "确认$dir", placing = vm.placing, skip = skipConfirm, onSkip = { skipConfirm = it },
                onConfirm = { sheet = null; vm.place(long) }, onClose = { sheet = null },
            )
        }
    }
    closeTarget?.let { pos ->
        ConfirmDialog("市价平仓", "确定以市价平掉 ${pos.symbol ?: ""} ${if (pos.side == ApiFuturesPosition.Side.SHORT) "空" else "多"}仓 ${pos.qty ?: ""} 吗？", "平仓", { closeTarget = null }) { vm.close(pos); closeTarget = null }
    }
    cancelTarget?.let { o -> ConfirmDialog("撤销委托", "确定撤销该笔委托吗？", "撤销", { cancelTarget = null }) { vm.cancel(o); cancelTarget = null } }
    cancelAlgoTarget?.let { a -> ConfirmDialog("撤销条件委托", "确定撤销该笔条件委托吗？", "撤销", { cancelAlgoTarget = null }) { vm.cancelAlgo(a); cancelAlgoTarget = null } }
    if (closeAllAsk) ConfirmDialog("一键平仓", "将以市价平掉${if (vm.onlyCurrent) "当前合约" else "全部"}的 ${vm.shownPositions.size} 个仓位，确定吗？", "全部平仓", { closeAllAsk = false }) { vm.closeAll(); closeAllAsk = false }
}

@Composable
private fun FuturesMid(last: BigDecimal, mark: BigDecimal, prec: Int, down: Boolean, onClick: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().pressable(scale = 1f, onClick = onClick).padding(vertical = 6.dp)) {
        Text(if (last.signum() > 0) Fmt.price(last, prec) else "--", style = lbNum(20.sp, FontWeight.Bold), color = if (down) p.down else p.up, maxLines = 1)
        Text(if (mark.signum() > 0) "≈" + Fmt.price(mark, prec) else "--", style = lbNum(12.sp), color = p.textSecondary, maxLines = 1)
    }
}

@Composable
private fun SideButton(vm: FuturesTradeViewModel, loggedIn: Boolean, long: Boolean, onClick: () -> Unit) {
    val p = LB.p
    val label = if (!loggedIn) "登录 / 注册" else if (vm.isOpen) (if (long) "开多" else "开空") else (if (long) "平空" else "平多")
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        LbButton(
            label, kind = if (!loggedIn) ButtonKind.PRIMARY else if (long) ButtonKind.BUY else ButtonKind.SELL,
            height = if (loggedIn) 46.dp else 40.dp, loading = vm.placing, onClick = onClick,
            subtitle = if (!loggedIn) null else if (!vm.isOpen) null
            else "${if (vm.maxOpenQty.signum() > 0) (if (vm.byQty) Fmt.amount(vm.maxOpenQty, vm.qtyPrec) else Fmt.amount(vm.available.multiply(BigDecimal(vm.leverage)), 2)) else "--"} ${if (vm.byQty) vm.base else vm.quote}",
        )
        if (vm.isOpen) {
            KvRow("预估强平价", "${if (vm.orderPrice.signum() > 0 && vm.cost.signum() > 0) Fmt.price(vm.liqPrice(long), vm.pricePrec) else "0"} ${vm.quote}", kDashed = true)
            KvRow("成本", "${Fmt.amount(vm.cost, 2).trimZeros()} ${vm.quote}", kDashed = true)
        }
    }
}

@Composable
private fun Coupons(vm: FuturesTradeViewModel, tab: Int, onTab: (Int) -> Unit) {
    val p = LB.p
    val items = vm.bonus.filter { if (tab == 0) (it.kind ?: "trial") != "deduction" else it.kind == "deduction" }
    Row(Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        listOf("体验金", "抵扣金").forEachIndexed { i, t ->
            Text(
                t, Modifier.clip(RoundedCornerShape(Radius.xs)).background(if (tab == i) p.fill else Color.Transparent).pressable(scale = 1f) { onTab(i) }.padding(horizontal = 10.dp, vertical = 4.dp),
                style = lb(FontSize.small, if (tab == i) FontWeight.Medium else FontWeight.Normal), color = if (tab == i) p.textPrimary else p.textSecondary,
            )
        }
    }
    if (items.isEmpty()) EmptyState("暂无卡券") { ChipButton("前往卡券中心", Modifier.width(140.dp).height(36.dp)) { vm.toast.show("卡券中心即将开放") } }
    else items.forEach { b ->
        Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp).border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text("${b.remaining ?: b.amount ?: "0"} USDT", style = lbNum(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                Text(b.source ?: "合约体验金", style = lb(FontSize.small), color = p.textSecondary)
            }
            Text(if (b.status == "active") "可使用" else (b.status ?: ""), style = lb(FontSize.small), color = p.textSecondary)
        }
    }
}

@Composable
private fun TodayPnlCard(vm: FuturesTradeViewModel) {
    val p = LB.p
    fun signed(d: BigDecimal) = (if (d.signum() > 0) "+" else "") + Fmt.amount(d, 4)
    fun col(d: BigDecimal) = if (d.signum() > 0) p.up else if (d.signum() < 0) p.down else p.textPrimary
    val unreal = vm.positions.fold(BigDecimal.ZERO) { a, pos -> a + vm.livePnl(pos).first }
    Column(
        Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp).clip(RoundedCornerShape(Radius.md)).background(p.bgElevated)
            .border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("今日盈亏(USDT)", style = lb(FontSize.small), color = p.textTertiary)
            Text(" UTC 00:00 起", style = lb(FontSize.small), color = p.textTertiary)
            Box(Modifier.weight(1f))
            if (vm.positions.isNotEmpty()) {
                Text("未实现 ", style = lb(FontSize.small), color = p.textTertiary)
                Text(signed(unreal), style = lbNum(FontSize.small, FontWeight.Medium), color = col(unreal))
            }
        }
        val v = vm.todayPnl
        when {
            v != null -> {
                val total = Dec.parse(v.pnl); val realized = Dec.parse(v.realizedPnl); val funding = Dec.parse(v.funding)
                val fee = Dec.parse(v.fee) + Dec.parse(v.liquidationFee)
                if (total.signum() == 0 && realized.signum() == 0 && funding.signum() == 0 && fee.signum() == 0) {
                    Text("今日暂无已实现盈亏、资金费或手续费记录", style = lb(FontSize.small), color = p.textSecondary)
                } else {
                    Text(signed(total), style = lbNum(FontSize.title, FontWeight.SemiBold), color = col(total))
                    Row {
                        KVCell("已实现盈亏", signed(realized), Modifier.weight(1f), vColor = col(realized))
                        KVCell("资金费", signed(funding), Modifier.weight(1f), TextAlign.Center, col(funding))
                        KVCell("手续费", signed(fee), Modifier.weight(1f), TextAlign.End, col(fee))
                    }
                }
            }
            vm.todayPnlError != null -> Row(verticalAlignment = Alignment.CenterVertically) {
                Text("今日盈亏加载失败：${vm.todayPnlError}", Modifier.weight(1f), style = lb(FontSize.small), color = p.down, maxLines = 2)
                ChipButton("重试", Modifier.height(24.dp)) { vm.loadTodayPnl() }
            }
            else -> Box(Modifier.fillMaxWidth().height(30.dp).clip(RoundedCornerShape(3.dp)).background(p.fill))
        }
    }
}

@Composable
fun KVCell(k: String, v: String, modifier: Modifier = Modifier, align: TextAlign = TextAlign.Start, vColor: Color? = null) {
    val p = LB.p
    val h = when (align) { TextAlign.Center -> Alignment.CenterHorizontally; TextAlign.End -> Alignment.End; else -> Alignment.Start }
    Column(modifier, horizontalAlignment = h, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(k, style = lb(FontSize.small), color = p.textTertiary, maxLines = 1)
        Text(v, style = lbNum(FontSize.label, FontWeight.Medium), color = vColor ?: p.textPrimary, maxLines = 1)
    }
}

@Composable
private fun SmallBtn(t: String, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val p = LB.p
    Box(modifier.height(30.dp).clip(RoundedCornerShape(Radius.sm)).background(p.fill).pressable(scale = 1f, onClick = onClick), contentAlignment = Alignment.Center) {
        Text(t, style = lb(FontSize.small, FontWeight.Medium), color = p.textPrimary)
    }
}

@Composable
private fun PositionCard(pos: ApiFuturesPosition, live: Triple<BigDecimal, BigDecimal, BigDecimal>, onTpsl: () -> Unit, onMargin: () -> Unit, onClose: () -> Unit) {
    val p = LB.p
    val long = pos.side != ApiFuturesPosition.Side.SHORT
    val (pnl, roe, mk) = live
    val pc = if (pnl.signum() < 0) p.down else p.up
    val markV = if (mk.signum() > 0) mk else Dec.parse(pos.markPrice)
    fun tag(t: String) = @Composable { Text(t, Modifier.clip(RoundedCornerShape(3.dp)).background(p.fill).padding(horizontal = 4.dp, vertical = 1.dp), style = lb(FontSize.small), color = p.textSecondary) }
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 14.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(if (long) "多" else "空", Modifier.clip(RoundedCornerShape(3.dp)).background(if (long) p.up else p.down).width(20.dp).height(18.dp), style = lb(FontSize.small, FontWeight.SemiBold), color = Color.White, textAlign = TextAlign.Center)
            Text(pos.symbol ?: "", style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            if (pos.positionSide != null && pos.positionSide != ApiFuturesPosition.PositionSide.BOTH) tag("分仓")()
            tag("${if (pos.marginMode == ApiFuturesPosition.MarginMode.ISOLATED) "逐仓" else "全仓"} ${pos.leverage ?: 0}x")()
        }
        Row {
            KVCell("未实现盈亏(USDT)", Fmt.amount(pnl, 2), Modifier.weight(1f), vColor = pc)
            KVCell("收益率", Fmt.percent(roe), Modifier.weight(1f), TextAlign.End, pc)
        }
        Row {
            KVCell("持仓量", pos.qty ?: "--", Modifier.weight(1f))
            KVCell("保证金(USDT)", Fmt.amount(Dec.parse(pos.margin), 2), Modifier.weight(1f), TextAlign.Center)
            KVCell("开仓价格", Fmt.priceAuto(Dec.parse(pos.entryPrice)), Modifier.weight(1f), TextAlign.End)
        }
        Row {
            KVCell("标记价格", Fmt.priceAuto(markV), Modifier.weight(1f))
            val liq = Dec.parse(pos.liquidationPrice)
            KVCell("强平价格", if (liq.signum() > 0) Fmt.priceAuto(liq) else "--", Modifier.weight(1f), TextAlign.Center)
            val tp = pos.tpPrice?.takeIf { it.isNotBlank() }; val sl = pos.slPrice?.takeIf { it.isNotBlank() }
            KVCell("止盈/止损", if (tp == null && sl == null) "--" else "${tp ?: "--"}/${sl ?: "--"}", Modifier.weight(1f), TextAlign.End)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            SmallBtn(if (pos.marginMode == ApiFuturesPosition.MarginMode.ISOLATED) "调整保证金" else "保证金", Modifier.weight(1f), onMargin)
            SmallBtn("止盈止损", Modifier.weight(1f), onTpsl)
            SmallBtn("市价平仓", Modifier.weight(1f), onClose)
        }
    }
}

@Composable
private fun FuturesOrderRow(o: ApiFuturesOrder, onCancel: () -> Unit) {
    val p = LB.p
    val buy = o.side == "buy"
    val ps = o.positionSide ?: "BOTH"
    val open = if (ps == "BOTH") o.reduceOnly != true else (ps == "LONG") == buy
    val dir = if (open) (if (buy) "开多" else "开空") else (if (buy) "平空" else "平多")
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(dir, style = lb(FontSize.small, FontWeight.SemiBold), color = if (buy) p.up else p.down)
            Text(o.symbol ?: "", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(if (o.type == "market") "市价" else "限价", style = lb(FontSize.small), color = p.textSecondary)
            if (o.timeInForce == "POST_ONLY" || o.timeInForce == "GTX") Text("只做Maker", style = lb(10.sp), color = p.textSecondary)
            Box(Modifier.weight(1f))
            ChipButton("撤单", Modifier.height(24.dp), onClick = onCancel)
        }
        Row {
            KVCell("委托价格", o.price ?: "市价", Modifier.weight(1f))
            KVCell("数量(${(o.symbol ?: "").removeSuffix("USDT")})", o.qty ?: "--", Modifier.weight(1f), TextAlign.Center)
            KVCell("已成交", o.filledQty ?: "0", Modifier.weight(1f), TextAlign.End)
        }
    }
}

@Composable
private fun AlgoOrderRow(a: ApiFuturesAlgoOrder, onCancel: () -> Unit) {
    val p = LB.p
    val buy = a.side == "buy"
    val ps = a.positionSide ?: "BOTH"
    val open = a.algoType == ApiFuturesAlgoOrder.AlgoType.PLAN && (if (ps == "BOTH") a.reduceOnly != true else (ps == "LONG") == buy)
    val dir = if (open) (if (buy) "开多" else "开空") else (if (buy) "平空" else "平多")
    val typeText = when (a.algoType) { ApiFuturesAlgoOrder.AlgoType.PLAN -> "计划委托"; ApiFuturesAlgoOrder.AlgoType.TP -> "止盈"; else -> "止损" }
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(dir, style = lb(FontSize.small, FontWeight.SemiBold), color = if (buy) p.up else p.down)
            Text(a.symbol ?: "", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(typeText, style = lb(FontSize.small), color = p.textSecondary)
            Box(Modifier.weight(1f))
            if (a.status == ApiFuturesAlgoOrder.Status.ACTIVE) ChipButton("撤销", Modifier.height(24.dp), onClick = onCancel)
            else Text(
                when (a.status) { ApiFuturesAlgoOrder.Status.TRIGGERED -> "已触发"; ApiFuturesAlgoOrder.Status.CANCELED -> "已撤销"; ApiFuturesAlgoOrder.Status.FAILED -> "触发失败"; else -> "--" },
                style = lb(FontSize.small), color = p.textSecondary,
            )
        }
        Row {
            KVCell(
                "触发价(${if (a.triggerPriceType == ApiFuturesAlgoOrder.TriggerPriceType.MARK) "标记" else "最新"})",
                "${if (a.triggerDirection == ApiFuturesAlgoOrder.TriggerDirection.DOWN) "≤" else "≥"} ${Fmt.priceAuto(Dec.parse(a.triggerPrice))}", Modifier.weight(1f),
            )
            KVCell("委托价格", if (a.orderType == ApiFuturesAlgoOrder.OrderType.MARKET || a.price.isNullOrBlank()) "市价" else Fmt.priceAuto(Dec.parse(a.price)), Modifier.weight(1f), TextAlign.Center)
            KVCell("数量", a.qty?.takeIf { it.isNotBlank() } ?: "全部仓位", Modifier.weight(1f), TextAlign.End)
        }
    }
}
