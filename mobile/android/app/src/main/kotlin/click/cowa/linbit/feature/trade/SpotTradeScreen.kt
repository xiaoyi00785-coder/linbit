package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiConditionalOrder
import click.cowa.linbit.core.generated.ApiOrder
import click.cowa.linbit.core.generated.ApiOrderSide
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.CheckRow
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.DashedText
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
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LayoutAdapt
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal

/** 现货 Tab（对照图 ref/28、ref/29、ref/19）：一级 Tab → 交易对头 → 下单表单 + 盘口 → 委托 / 条件单。 */
@Composable
fun SpotTradeScreen(
    bottomInset: androidx.compose.ui.unit.Dp,
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogin: () -> Unit,
    onDeposit: () -> Unit,
    onTransfer: () -> Unit,
    onHistory: () -> Unit,
    modifier: Modifier = Modifier,
    autoStart: Boolean = true,
    /** Preserve the production Host origin (Assets -> Spot earn/strategy); not a fixture flag. */
    initialTop: Int = 0,
    vm: SpotTradeViewModel = hiltViewModel(),
) {
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val market by vm.market.collectAsStateWithLifecycle()
    val nav by vm.symbolFlow.collectAsStateWithLifecycle()
    DisposableEffect(nav) { if (autoStart) vm.start(); onDispose { vm.stop() } }
    SpotTradeContent(vm, loggedIn, market.spot, market.cnyRate, bottomInset, themeMode, onTheme, onLogin, onDeposit, onTransfer, onHistory, modifier, initialTop)
}

@Composable
fun SpotTradeContent(
    vm: SpotTradeViewModel,
    loggedIn: Boolean,
    marketRows: List<click.cowa.linbit.data.MarketRow>,
    cnyRate: BigDecimal?,
    bottomInset: androidx.compose.ui.unit.Dp,
    themeMode: ThemeMode,
    onTheme: (ThemeMode) -> Unit,
    onLogin: () -> Unit,
    onDeposit: () -> Unit,
    onTransfer: () -> Unit,
    onHistory: () -> Unit,
    modifier: Modifier = Modifier,
    initialTop: Int = 0,
) {
    val p = LB.p
    var sheet by remember { mutableStateOf<String?>(null) }
    var skipConfirm by remember { mutableStateOf(false) }
    var ordersTab by remember { mutableStateOf(0) }
    var topTab by rememberSaveable { mutableIntStateOf(initialTop.coerceIn(0, 3)) }
    var chartOpen by remember { mutableStateOf(false) }
    val step = vm.bookPrecision ?: BigDecimal.ONE.movePointLeft(vm.pricePrec)
    val grouped = remember(vm.book, step) { vm.book.grouped(step) }
    val pageH = LayoutAdapt.pageH(LocalConfiguration.current.screenWidthDp)

    Column(modifier.fillMaxSize().background(p.bg)) {
        Row(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = pageH), verticalAlignment = Alignment.CenterVertically) {
            PrimaryTabs(listOf("现货", "策略", "理财", "闪兑"), topTab, { topTab = it }, size = 16.sp)
        }
        HorizontalDivider(color = p.divider)
        when (topTab) {
            1 -> StrategySubpage(bottomInset, onLogin)
            2 -> EarnSubpage(bottomInset, onLogin)
            3 -> ConvertSubpage(bottomInset, onLogin)
            else -> Column(Modifier.weight(1f).imePadding().verticalScroll(rememberScrollState()).padding(bottom = bottomInset)) {
            // 交易对头
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp), verticalAlignment = Alignment.Top) {
                Column(Modifier.pressable(scale = 1f) { sheet = "pairs" }) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text("${vm.base}/${vm.quote}", style = lb(18.sp, FontWeight.SemiBold), color = p.textPrimary)
                        DownTriangle(Modifier.padding(start = 2.dp), p.textSecondary, 14.dp)
                        Box(Modifier.padding(start = 2.dp).size(5.dp).clip(CircleShape).background(p.up))
                    }
                    Text(Fmt.percent(vm.changePct), style = lbNum(FontSize.small), color = if (vm.changePct.signum() < 0) p.down else p.up)
                }
                Box(Modifier.weight(1f))
                Row(Modifier.padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                    LbIcon(R.drawable.ic_sun, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { sheet = "theme" })
                    LbIcon(R.drawable.ic_candles, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { chartOpen = !chartOpen })
                    LbIcon(R.drawable.ic_more, 20.dp, p.textPrimary, Modifier.pressable(scale = 1f) { vm.toast.show("更多功能即将开放") })
                }
            }
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                // 左：表单
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    PillSegment(listOf("买入", "卖出"), if (vm.isBuy) 0 else 1, { vm.isBuy = it == 0; vm.applyPercent(0f) })
                    SelectBox(vm.kind.title) { sheet = "kind" }
                    if (vm.isPlan) {
                        NumberBox("触发价", vm.quote, vm.triggerText, { vm.triggerText = it; vm.qtyEdited() })
                        PillSegment(listOf("限价", "市价"), if (vm.planMarket) 1 else 0, { vm.planMarket = it == 1; vm.qtyEdited() }, height = 28.dp, activeColors = listOf(p.fillStrong, p.fillStrong), activeText = p.textPrimary)
                    }
                    if (vm.marketLike) {
                        Box(Modifier.fillMaxWidth().height(38.dp).clip(RoundedCornerShape(Radius.md)).background(p.fill), contentAlignment = Alignment.Center) {
                            Text(if (vm.isPlan) "触发后以市价成交" else "以当前最优价格成交", style = lb(FontSize.label), color = p.textSecondary)
                        }
                        if (vm.isBuy) PillSegment(listOf("按金额", "按数量"), if (vm.marketBuyByAmount) 0 else 1, { vm.marketBuyByAmount = it == 0 }, height = 28.dp, activeColors = listOf(p.fillStrong, p.fillStrong), activeText = p.textPrimary)
                    } else {
                        NumberBox(if (vm.isPlan) "委托价格" else "价格", vm.quote, vm.priceText, { vm.priceText = it; vm.qtyEdited() })
                    }
                    if (vm.byAmount) {
                        NumberBox("交易额", vm.quote, vm.amountText, { vm.amountText = it })
                        PercentSlider(vm.percent, { vm.applyPercent(it) }, Modifier.padding(horizontal = 2.dp), tint = p.textPrimary)
                    } else {
                        NumberBox("数量", vm.base, vm.qtyText, { vm.qtyText = it; vm.qtyEdited() })
                        PercentSlider(vm.percent, { vm.applyPercent(it) }, Modifier.padding(horizontal = 2.dp), tint = p.textPrimary)
                        NumberBox("交易额", vm.quote, vm.amountText, { vm.amountText = it; vm.amountEdited() })
                    }
                    if (!vm.isPlan) CheckRow(vm.tpsl, "止盈/止损", dashed = true) { vm.tpsl = it }
                    if (vm.tpsl && !vm.isPlan) {
                        NumberBox("止盈触发价", vm.quote, vm.tpText, { vm.tpText = it })
                        NumberBox("止损触发价", vm.quote, vm.slText, { vm.slText = it })
                    }
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        KvRow("可用", "${Fmt.amount(if (vm.isBuy) vm.availQuote else vm.availBase, 2).trimZeros()} ${if (vm.isBuy) vm.quote else vm.base}", trailing = {
                            ThemedIcon(R.drawable.ic_mc_plus_square_light, R.drawable.ic_mc_plus_square_dark, 14.dp, Modifier.padding(start = 4.dp).pressable(scale = 1f, onClick = onTransfer))
                        })
                        KvRow(
                            if (vm.isBuy) "可买" else "可卖",
                            if (vm.isBuy) "${Fmt.amount(vm.maxQty, vm.qtyPrec)} ${vm.base}" else "${Fmt.amount(vm.availBase.multiply(vm.book.bestBid ?: vm.last), 2)} ${vm.quote}",
                        )
                        Box(Modifier.pressable(scale = 1f) { sheet = "fee" }) {
                            KvRow("预估手续费", Fmt.amount(vm.fee, 2))
                        }
                    }
                    LbButton(
                        if (loggedIn) "${if (vm.isBuy) "买入" else "卖出"} ${vm.base}" else "登录 / 注册",
                        kind = if (loggedIn) (if (vm.isBuy) ButtonKind.BUY else ButtonKind.SELL) else ButtonKind.PRIMARY,
                        height = 40.dp, loading = vm.placing,
                    ) {
                        if (!loggedIn) { onLogin(); return@LbButton }
                        vm.validation?.let { vm.toast.show(it, click.cowa.linbit.ui.components.ToastCenter.Kind.ERROR); return@LbButton }
                        if (skipConfirm) vm.place() else sheet = "confirm"
                    }
                }
                // 右：盘口
                OrderBookView(
                    book = grouped, rows = 5, rowHeight = 23.dp, pricePrec = vm.pricePrec, quote = vm.quote,
                    mid = {
                        SpotMid(vm.last, vm.pricePrec, vm.changePct.signum() < 0, cnyRate?.let { "¥ " + Fmt.price(vm.last.multiply(it), 2) }) { vm.pickBookPrice(vm.last) }
                    },
                    mode = vm.bookMode, precisionText = step.stripTrailingZeros().toPlainString(),
                    onPick = vm::pickBookPrice, onMode = { sheet = "bookMode" }, onPrecision = { sheet = "precision" },
                    modifier = Modifier.width(138.dp),
                )
            }
            if (chartOpen) {
                Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    TradeChartPanel(symbol = vm.symbol, pricePrec = vm.pricePrec, showTrades = false, loadCandles = { iv -> vm.loadKlines(iv) })
                }
            }
            // 委托区
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 20.dp).height(40.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                Row(Modifier.pressable(scale = 1f) { if (ordersTab == 0 || ordersTab == 2) sheet = "orderKind" else ordersTab = 0 }, verticalAlignment = Alignment.CenterVertically) {
                    Text(if (ordersTab == 2) "条件单(${vm.conditional.size})" else "委托(${vm.openOrders.size})", style = lb(FontSize.label, if (ordersTab != 1) FontWeight.Medium else FontWeight.Normal), color = if (ordersTab != 1) p.textPrimary else p.textSecondary)
                    DownTriangle(tint = if (ordersTab == 0 || ordersTab == 2) p.textPrimary else p.textSecondary, size = 14.dp)
                }
                Text("交易机器人(0)", Modifier.pressable(scale = 1f) { ordersTab = 1 }, style = lb(FontSize.label, if (ordersTab == 1) FontWeight.Medium else FontWeight.Normal), color = if (ordersTab == 1) p.textPrimary else p.textSecondary)
                Box(Modifier.weight(1f))
                ThemedIcon(R.drawable.ic_duo_clock_fill_light, R.drawable.ic_duo_clock_fill_dark, 20.dp, Modifier.pressable(scale = 1f, onClick = onHistory))
            }
            HorizontalDivider(color = p.divider)
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                CheckRow(vm.onlyCurrent, "只看当前") { vm.onlyCurrent = it }
                Box(Modifier.weight(1f))
                if (ordersTab == 0) ChipButton("全部撤销", Modifier.height(28.dp)) { vm.cancelAll() }
            }
            when (ordersTab) {
                0 -> if (vm.shownOrders.isEmpty()) NoOrders(loggedIn, onDeposit, onTransfer, onLogin) else vm.shownOrders.forEach { OrderRow(it, vm.pricePrec) { vm.cancel(it) } }
                2 -> if (vm.shownConditional.isEmpty()) EmptyState("暂无条件单") else vm.shownConditional.forEach { CondRow(it) { vm.cancelConditional(it) } }
                else -> EmptyState("暂无交易机器人")
            }
        }
        } // end when topTab else
    }

    if (topTab == 0) when (sheet) {
        "orderKind" -> LbSheet(null, { sheet = null }) {
            Column(Modifier.padding(bottom = 20.dp)) {
                listOf(0 to "当前委托", 2 to "条件单").forEach { (i, t) -> SheetOptionRow(t, ordersTab == i) { ordersTab = i; sheet = null } }
            }
        }
        "kind" -> LbSheet("委托类型", { sheet = null }) {
            Column(Modifier.padding(bottom = 20.dp)) {
                SpotOrderKind.entries.forEach { k ->
                    SheetOptionRow(k.title, vm.kind == k, subtitle = k.desc) { vm.kind = k; vm.qtyEdited(); sheet = null }
                }
            }
        }
        "pairs" -> PairPickerSheet(marketRows, vm.symbol, true, { vm.switchSymbol(it) }, { sheet = null })
        "bookMode" -> BookModeSheet(vm.bookMode, { vm.bookMode = it }) { sheet = null }
        "precision" -> PrecisionSheet(vm.pricePrec, vm.bookPrecision, { vm.bookPrecision = it }, { sheet = null })
        "theme" -> ThemeModeSheet(themeMode, onTheme, { sheet = null })
        "fee" -> LbSheet("手续费率", { sheet = null }) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                ConfirmRow("Maker 费率", pct(vm.makerRate ?: SpotTradeViewModel.FALLBACK_FEE))
                ConfirmRow("Taker 费率", pct(vm.takerRate ?: SpotTradeViewModel.FALLBACK_FEE))
                ConfirmRow("本次预估手续费", "${Fmt.amount(vm.fee, 4)} ${vm.quote}")
            }
        }
        "confirm" -> ConfirmSheet(
            title = "${if (vm.isBuy) "买入" else "卖出"} ${vm.base}/${vm.quote}",
            rows = buildList {
                add(Triple("委托类型", vm.kind.title, null))
                add(Triple("方向", if (vm.isBuy) "买入" else "卖出", if (vm.isBuy) p.up else p.down))
                if (vm.isPlan) add(Triple("触发价", "${vm.triggerText} ${vm.quote}", null))
                add(Triple("委托价格", if (vm.marketLike) "市价" else "${vm.priceText} ${vm.quote}", null))
                if (vm.byAmount) add(Triple("下单方式", "按金额", null)) else add(Triple("委托数量", "${vm.fmt(Dec.parse(vm.qtyText), vm.qtyPrec)} ${vm.base}", null))
                add(Triple(if (vm.marketLike && !vm.byAmount) "预估交易额" else "交易额", "${Fmt.amount(vm.amount, 2)} ${vm.quote}", null))
                add(Triple("手续费率", pct(vm.feeRate), null))
                add(Triple("预估手续费", "${Fmt.amount(vm.fee, 4)} ${vm.quote}", null))
            },
            note = when {
                vm.isPlan -> "最新价到达触发价后按${if (vm.planMarket) "市价" else "委托价格"}下单，触发前将冻结相应资金"
                vm.kind == SpotOrderKind.MARKET -> "市价单将以盘口最优价格立即成交，实际成交价可能与预估存在偏差"
                else -> null
            },
            buyColor = vm.isBuy, confirmText = "确认${if (vm.isBuy) "买入" else "卖出"}", placing = vm.placing,
            skip = skipConfirm, onSkip = { skipConfirm = it },
            onConfirm = { sheet = null; vm.place() }, onClose = { sheet = null },
        )
    }
}

internal fun pct(r: BigDecimal): String = r.multiply(BigDecimal(100)).stripTrailingZeros().toPlainString() + "%"

/** 「0.00」→「0」，「12.50」→「12.5」（可用余额样式，对照图「0 USDT」）。 */
internal fun String.trimZeros(): String =
    if (contains('.')) trimEnd('0').trimEnd('.').ifEmpty { "0" } else this

@Composable
private fun NoOrders(loggedIn: Boolean, onDeposit: () -> Unit, onTransfer: () -> Unit, onLogin: () -> Unit) {
    val p = LB.p
    // 空态操作与悬浮 Tab 至少保留约 24dp 视觉间距（bottomInset 由 AppRoot 注入）。
    Column(Modifier.fillMaxWidth().padding(bottom = 24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        EmptyState(if (loggedIn) "划转资金，开始现货交易" else "登录后查看委托", iconSize = 56.dp, verticalPadding = 8.dp) {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (loggedIn) {
                    ChipButton("充值", Modifier.width(90.dp).height(32.dp), onClick = onDeposit)
                    ChipButton("划转", Modifier.width(90.dp).height(32.dp), onClick = onTransfer)
                } else ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin)
            }
        }
    }
}

@Composable
private fun OrderRow(o: ApiOrder, prec: Int, onCancel: () -> Unit) {
    val p = LB.p
    val buy = o.side == click.cowa.linbit.core.generated.ApiOrderSide.BUY
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(o.symbol.replace("USDT", "/USDT"), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (buy) "买入" else "卖出"}", style = lb(FontSize.small), color = if (buy) p.up else p.down)
            Box(Modifier.weight(1f))
            ChipButton("撤单", onClick = onCancel)
        }
        KvRow("价格", o.price ?: "市价")
        KvRow("数量（已成交）", "${o.quantity}（${o.filledQuantity}）")
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun CondRow(o: ApiConditionalOrder, onCancel: () -> Unit) {
    val p = LB.p
    val buy = o.side == "buy"
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text((o.symbol ?: "").replace("USDT", "/USDT"), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (buy) "买入" else "卖出"}", style = lb(FontSize.small), color = if (buy) p.up else p.down)
            Box(Modifier.weight(1f))
            ChipButton("撤销", onClick = onCancel)
        }
        KvRow("触发价", o.triggerPrice ?: "--")
        KvRow("委托价", o.price ?: "市价")
        KvRow("数量", o.quantity ?: o.quoteOrderQty ?: "--")
    }
    HorizontalDivider(color = p.divider)
}

