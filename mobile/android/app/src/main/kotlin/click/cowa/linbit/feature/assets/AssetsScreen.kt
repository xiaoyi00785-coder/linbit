package click.cowa.linbit.feature.assets

import click.cowa.linbit.feature.more.bonusAmount

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.systemBars
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.R
import click.cowa.linbit.core.storage.DisplayPrefs
import click.cowa.linbit.core.storage.QuoteDisplay
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import java.math.BigDecimal
import java.math.RoundingMode
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.assets.AssetsNav
import click.cowa.linbit.data.assets.AssetsRoute
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum

private val Tabs = listOf("总览", "资金", "合约", "现货", "理财", "策略", "模拟", "TradFi")

@Composable
fun AssetsScreen(
    bottomInset: Dp,
    onLogin: () -> Unit,
    assetsNav: AssetsNav,
    onOpenFutures: () -> Unit = {},
    onOpenSpot: () -> Unit = {},
    onOpenProfile: () -> Unit = {},
    /** Production earn/strategy/TradFi hosts; no placeholder success or duplicate balances. */
    onOpenAssetFeature: (String) -> Unit = {},
    onOpenSpotHistory: () -> Unit = {},
    onOpenFuturesHistory: () -> Unit = {},
    vm: AssetsViewModel = hiltViewModel(),
) {
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    val s by vm.state.collectAsStateWithLifecycle()
    var showWithdraw by remember { mutableStateOf(false) }
    var showFilter by remember { mutableStateOf(false) }
    var showSearch by remember { mutableStateOf(false) }
    var showValuation by remember { mutableStateOf(false) }
    var showHistory by remember { mutableStateOf(false) }
    var showEarnOrders by remember { mutableStateOf(false) }
    var showEarnRecords by remember { mutableStateOf(false) }
    var detail by remember { mutableStateOf<String?>(null) }
    detail?.let { a -> CoinDetailPage(vm, a, onBack = { detail = null }, onTrade = { detail = null; onOpenSpot() }) }
    val p = LB.p

    LaunchedEffect(sessionVersion) { vm.load() }

    Column(Modifier.fillMaxSize().background(p.bg)) {
        PrimaryTabs(Tabs, s.tab, onSelect = { i ->
            if (i >= 6) {
                // The old guest guard below suppressed every tab action. These
                // destinations have real production login handling.
                if (loggedIn) onOpenAssetFeature(listOf("paper", "tradfi")[i - 6]) else onLogin()
            } else vm.setTab(i)
        }, Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH), scrollable = true)
        Box(Modifier.fillMaxWidth().height(1.dp).background(p.divider))

        if (!loggedIn) {
            Column(Modifier.fillMaxSize().padding(bottom = bottomInset), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                EmptyState("登录后查看资产")
                Spacer(Modifier.height(12.dp))
                LbButton("登录 / 注册", Modifier.width(200.dp), onClick = onLogin)
            }
            return
        }

        if (s.loading && s.spot.isEmpty() && s.funding.isEmpty()) {
            Box(Modifier.fillMaxSize().padding(bottom = bottomInset), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = p.brand, strokeWidth = 2.dp, modifier = Modifier.size(28.dp))
            }
            return
        }
        if (s.error != null && s.spot.isEmpty()) {
            Column(Modifier.fillMaxSize().padding(bottom = bottomInset), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                Text(s.error ?: "", style = lb(FontSize.body), color = p.textSecondary)
                Spacer(Modifier.height(12.dp))
                LbButton("重试", Modifier.width(140.dp), onClick = { vm.load() })
            }
            return
        }

        val total = vm.totalUsdt()
        val emptyOverview = s.tab == 0 && total.signum() == 0

        Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(bottom = bottomInset + 16.dp)) {
            if (s.tab == 4) {
                EarnTab(s, onOpen = { onOpenAssetFeature("earn") }, onTransfer = { assetsNav.openTransfer() }, onToggle = vm::toggleHide, onHistory = { showHistory = true }, onOrders = { showEarnOrders = true })
            } else if (s.tab == 5) {
                BotsTab(s, onOpen = { onOpenAssetFeature("strategy") }, onTransfer = { assetsNav.openTransfer() }, onToggle = vm::toggleHide)
            } else if (emptyOverview) {
                OverviewEmpty(onDeposit = { assetsNav.openDeposit(switchTab = false) }, onFutures = onOpenFutures)
            } else {
                Header(s.hideAmount, total, s.cnyRate, s.tab, vm, onToggle = vm::toggleHide, onValuation = { showValuation = true }, onHistory = { showHistory = true })
                Actions(
                    tab = s.tab,
                    onDeposit = { assetsNav.openDeposit() },
                    onWithdraw = { showWithdraw = true },
                    onTransfer = { assetsNav.openTransfer() },
                    onTrade = { if (s.tab == 2) onOpenFutures() else onOpenSpot() },
                    onPnl = { onOpenAssetFeature("pnl") },
                    onQuickBuy = { onOpenAssetFeature("quickbuy") },
                )
                if (s.tab == 3) ConvertEntry { onOpenAssetFeature("convert") }
                if (s.tab == 2) FuturesCard(vm, s) else AssetList(
                    vm, s, showSearch,
                    onSearch = { showSearch = it },
                    onFilter = { showFilter = true },
                    onOpenAsset = { detail = it },
                )
            }
        }
    }

    if (showWithdraw) {
        WithdrawMethodSheet(
            onClose = { showWithdraw = false },
            onChain = { showWithdraw = false; assetsNav.openWithdraw() },
            onInternal = { showWithdraw = false; assetsNav.openWithdraw() /* 内部转账同页切换 */ ; assetsNav.open(AssetsRoute.Withdraw(null)) },
            onRecords = { showWithdraw = false; assetsNav.open(AssetsRoute.WithdrawRecords) },
            onAddressBook = { showWithdraw = false; assetsNav.openAddressBook() },
        )
    }
    if (showFilter) {
        LbSheet("资产筛选", onClose = { showFilter = false }) {
            Row(
                Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 28.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("隐藏小额资产（< 1 USDT）", Modifier.weight(1f), style = lb(FontSize.body), color = p.textPrimary)
                val on = s.hideSmall
                Box(
                    Modifier
                        .size(48.dp, 28.dp)
                        .background(if (on) p.brand else p.fillStrong, RoundedCornerShape(999.dp))
                        .pressable(scale = 1f) { vm.setHideSmall(!on) }
                        .padding(2.dp),
                    contentAlignment = if (on) Alignment.CenterEnd else Alignment.CenterStart,
                ) {
                    Box(Modifier.size(24.dp).background(p.bg, RoundedCornerShape(999.dp)))
                }
            }
        }
    }

    val ctx = LocalContext.current
    if (showValuation) {
        LbSheet("估值单位", { showValuation = false }, showClose = true) {
            Column(Modifier.padding(bottom = 20.dp)) {
                listOf("USDT", "BTC", "ETH").forEach { unit ->
                    Row(
                        Modifier.fillMaxWidth().pressable(scale = 1f) {
                            DisplayPrefs.setValuationAsset(ctx, unit); showValuation = false
                        }.padding(horizontal = Spacing.pageH, vertical = 14.dp).testTag("assets.valuation.$unit"),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        CoinIcon(unit, 22.dp)
                        Text(unit, Modifier.padding(start = 10.dp).weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                        if (DisplayPrefs.valuationAsset == unit) Text("✓", style = lb(FontSize.body, FontWeight.Bold), color = p.brand)
                    }
                }
                Text(
                    "BTC/ETH 用现货 ticker 折算；非法币。法币≈见偏好「计价货币」。",
                    Modifier.padding(horizontal = Spacing.pageH).padding(top = 4.dp),
                    style = lb(FontSize.small),
                    color = p.textTertiary,
                )
            }
        }
    }
    if (showHistory) {
        LbSheet("历史记录", { showHistory = false }, showClose = true) {
            Column(
                Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp).testTag("assets.history.sheet"),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                AssetsHistoryCatalog.optionsForTab(s.tab).forEach { opt ->
                    HistoryOption(opt.title, opt.desc) {
                        showHistory = false
                        when (opt.action) {
                            AssetsHistoryOption.Action.DEPOSIT_RECORDS -> assetsNav.open(AssetsRoute.DepositRecords)
                            AssetsHistoryOption.Action.WITHDRAW_RECORDS -> assetsNav.open(AssetsRoute.WithdrawRecords)
                            AssetsHistoryOption.Action.SPOT_HISTORY -> onOpenSpotHistory()
                            AssetsHistoryOption.Action.FUTURES_HISTORY -> onOpenFuturesHistory()
                            AssetsHistoryOption.Action.EARN_ORDERS -> showEarnOrders = true
                            AssetsHistoryOption.Action.EARN_RECORDS -> showEarnRecords = true
                        }
                    }
                }
            }
        }
    }
    if (showEarnOrders) {
        EarnHistorySheet(
            title = "理财订单",
            empty = "暂无理财订单",
            onClose = { showEarnOrders = false },
            kind = EarnHistoryKind.ORDERS,
        )
    }
    if (showEarnRecords) {
        EarnHistorySheet(
            title = "理财流水",
            empty = "暂无理财流水",
            onClose = { showEarnRecords = false },
            kind = EarnHistoryKind.RECORDS,
        )
    }

}


@Composable
private fun HistoryOption(title: String, desc: String, onClick: () -> Unit) {
    val p = LB.p
    Column(
        Modifier.fillMaxWidth().border(1.dp, p.border, RoundedCornerShape(Radius.md))
            .pressable(scale = 1f, onClick = onClick).padding(14.dp),
    ) {
        Text(title, style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
        Text(desc, Modifier.padding(top = 4.dp), style = lb(FontSize.small), color = p.textSecondary)
    }
}

@Composable
private fun OverviewEmpty(onDeposit: () -> Unit, onFutures: () -> Unit) {
    val p = LB.p
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp)) {
        // The captured reference overview has open whitespace above its deposit call to action.
        Spacer(Modifier.height(270.dp))
        Text("完成充值，开启您的 Linbit 之旅!", style = lb(24.sp, FontWeight.Bold), color = p.textPrimary)
        Spacer(Modifier.height(12.dp))
        LbButton("充值", onClick = onDeposit)
        Spacer(Modifier.height(16.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            click.cowa.linbit.ui.components.LbImageIcon(R.drawable.ic_ref_asset_gift, 76.dp)
            Spacer(Modifier.width(16.dp))
            Column(Modifier.weight(1f)) {
                Text("您的账户尚无可用资产", style = lb(11.sp), color = p.textSecondary)
                Text("充值任意金额，参与合约交易", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                Text("立即查看 ›", Modifier.pressable(scale = 1f, onClick = onFutures).padding(top = 4.dp), style = lb(FontSize.small, FontWeight.Medium), color = p.textPrimary)
            }
        }
        Spacer(Modifier.height(16.dp))
        Box(Modifier.fillMaxWidth().height(1.dp).background(p.divider))
        Spacer(Modifier.height(12.dp))
        Text("责任与保障", style = lb(FontSize.title, FontWeight.Bold), color = p.textPrimary)
        Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("资产安全是我们的首要任务", style = lb(FontSize.label), color = p.textPrimary)
                Text("了解更多", Modifier.padding(top = 6.dp), style = lb(FontSize.small), color = p.brand)
            }
            LbIcon(R.drawable.ic_shield, 40.dp, p.textSecondary)
        }
        Row(Modifier.padding(top = 18.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("帮助中心与 7×24 小时客服", style = lb(FontSize.label), color = p.textPrimary)
                Text("立即前往", Modifier.padding(top = 6.dp), style = lb(FontSize.small), color = p.brand)
            }
            LbIcon(R.drawable.ic_headset, 40.dp, p.textSecondary)
        }
    }
}

@Composable
private fun Header(
    hide: Boolean,
    total: BigDecimal,
    cny: BigDecimal?,
    tab: Int,
    vm: AssetsViewModel,
    onToggle: () -> Unit,
    onValuation: () -> Unit,
    onHistory: () -> Unit,
) {
    val p = LB.p
    val s = vm.state.collectAsStateWithLifecycle().value
    val unit = DisplayPrefs.valuationAsset
    val quoteCur = QuoteDisplay.currency
    val quoteRates = QuoteDisplay.rates
    val quoteBase = QuoteDisplay.ratesBase
    val usdPer = QuoteDisplay.usdPerUsdt
    val display = remember(total, unit, s) { valuationAmount(total, unit, vm) }
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("总资产估值", style = lb(FontSize.caption), color = p.textSecondary)
            Spacer(Modifier.width(6.dp))
            LbIcon(if (hide) R.drawable.ic_eye_off else R.drawable.ic_eye, 14.dp, p.textSecondary, Modifier.pressable(scale = 1f, onClick = onToggle).testTag("assets.hide"))
            Spacer(Modifier.weight(1f))
            LbIcon(R.drawable.ic_doc_text, 18.dp, p.textPrimary, Modifier.pressable(scale = 1f, onClick = onHistory).testTag("assets.history"))
        }
        Row(verticalAlignment = Alignment.Bottom) {
            Text(
                if (hide) "****" else display?.let { if (unit == "USDT") Fmt.amount(it, 2) else Fmt.amount(it, 8) } ?: "--",
                style = lbNum(32.sp, FontWeight.Bold),
                color = p.textPrimary,
            )
            Spacer(Modifier.width(6.dp))
            Row(
                Modifier.pressable(scale = 1f, onClick = onValuation).testTag("assets.valuation"),
                verticalAlignment = Alignment.Bottom,
            ) {
                Text(unit, style = lb(FontSize.small), color = p.textPrimary)
                LbIcon(R.drawable.ic_caret_down, 10.dp, p.textPrimary, Modifier.padding(start = 2.dp, bottom = 2.dp))
            }
        }
        if (hide) {
            Text("≈ ****", style = lbNum(FontSize.small), color = p.textSecondary)
        } else {
            @Suppress("UNUSED_VARIABLE")
            val _observe = quoteCur + quoteBase + (usdPer?.toPlainString() ?: "") + quoteRates.size
            val fiat = QuoteDisplay.format(total, 2, cny)
            Text("≈ $fiat", Modifier.testTag("assets.fiat"), style = lbNum(FontSize.small), color = p.textSecondary)
            if (fiat.endsWith("--")) {
                val reason = QuoteDisplay.lastMissingReason ?: "汇率不可用"
                Row(Modifier.padding(top = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(reason, Modifier.weight(1f), style = lb(FontSize.small), color = p.textTertiary)
                    Text("重试", Modifier.pressable(scale = 1f) { vm.reloadQuotes() }.testTag("assets.fiat.retry"), style = lb(FontSize.small, FontWeight.Medium), color = p.brand)
                }
            }
        }
        if (tab == 1 || tab == 2) {
            val label = if (tab == 2) "今日盈亏" else "可用余额"
            val value = if (hide) "****" else if (tab == 2) {
                "${Fmt.amount(click.cowa.linbit.core.util.Dec.parse(s.futures?.crossUnrealizedPnl), 2)} USD"
            } else "${Fmt.amount(if (tab == 1) vm.fundingAvailUsdt() else vm.spotAvailUsdt(), 2)} USDT"
            Row(Modifier.padding(top = 8.dp)) {
                Text(label, style = lb(FontSize.small), color = p.textSecondary)
                Spacer(Modifier.width(8.dp))
                Text(value, style = lbNum(FontSize.small, FontWeight.Medium), color = p.textPrimary)
            }
        }
    }
}

private fun valuationAmount(totalUsdt: BigDecimal, unit: String, vm: AssetsViewModel): BigDecimal? {
    if (unit == "USDT") return totalUsdt
    val px = vm.pairFor(unit)?.last?.takeIf { it.signum() > 0 } ?: return null
    return totalUsdt.divide(px, 8, RoundingMode.HALF_UP)
}

@Composable
private fun Actions(
    tab: Int,
    onDeposit: () -> Unit,
    onWithdraw: () -> Unit,
    onTransfer: () -> Unit,
    onTrade: () -> Unit,
    onPnl: () -> Unit,
    onQuickBuy: () -> Unit,
) {
    val p = LB.p
    val items: List<Pair<String, () -> Unit>> = when (tab) {
        2 -> listOf("去交易" to onTrade, "划转" to onTransfer, "盈亏" to onPnl)
        3 -> listOf("去交易" to onTrade, "充值" to onDeposit, "快捷买币" to onQuickBuy, "划转" to onTransfer)
        else -> listOf("充值" to onDeposit, "快捷买币" to onQuickBuy, "提现" to onWithdraw, "划转" to onTransfer)
    }
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp).padding(top = 20.dp)) {
        items.forEach { (title, act) ->
            Column(
                Modifier.weight(1f).pressable(onClick = act),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Box(
                    Modifier.size(40.dp).background(p.fill, RoundedCornerShape(Radius.lg)).testTag("assets.action.$title"),
                    contentAlignment = Alignment.Center,
                ) {
                    // 矢量线性图标 + tint；禁止 title.take(1) 文字占位（真机曾显示「去/充/快/划」）。
                    LbIcon(AssetsActionIcons.drawable(title), 22.dp, p.textPrimary)
                }
                Spacer(Modifier.height(8.dp))
                Text(title, style = lb(FontSize.small), color = p.textPrimary)
            }
        }
    }
    // 参考 App：快捷按钮下方去掉整宽浅灰分隔块，仅留白。
    AssetsSectionGap()
}

/** 参考 App：快捷按钮下方无整宽浅灰分隔块，仅留白距。 */
@Composable
private fun AssetsSectionGap() {
    Spacer(Modifier.height(16.dp).testTag("assets.section.gap"))
}

@Composable
private fun AssetList(
    vm: AssetsViewModel,
    s: AssetsUiState,
    showSearch: Boolean,
    onSearch: (Boolean) -> Unit,
    onFilter: () -> Unit,
    onOpenAsset: (String) -> Unit,
) {
    val p = LB.p
    val rows = vm.rows()
    Column {
        Row(
            Modifier.fillMaxWidth().height(56.dp).padding(horizontal = Spacing.pageH).padding(top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (showSearch) {
                LbTextField(s.query, vm::setQuery, "搜索币种", Modifier.weight(1f), height = 34.dp)
                Spacer(Modifier.width(8.dp))
                Text("取消", Modifier.pressable(scale = 1f) { vm.setQuery(""); onSearch(false) }, style = lb(FontSize.label), color = p.textPrimary)
            } else {
                Text("资产", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                Spacer(Modifier.weight(1f))
                LbIcon(R.drawable.ic_search, 18.dp, p.textSecondary, Modifier.pressable(scale = 1f) { onSearch(true) })
                Spacer(Modifier.width(14.dp))
                LbIcon(R.drawable.ic_sort, 18.dp, p.textSecondary, Modifier.pressable(scale = 1f, onClick = onFilter))
            }
        }
        val all = rows + vm.zeroRows()
        if (all.isEmpty()) EmptyState(if (s.query.isEmpty()) "暂无资产" else "暂无相关币种", Modifier.padding(vertical = 40.dp))
        all.forEach { b ->
            Row(
                Modifier.fillMaxWidth().height(60.dp).pressable(scale = 1f) { onOpenAsset(b.asset) }.padding(horizontal = Spacing.pageH),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CoinIcon(b.asset)
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(b.asset, style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    val sub = if (click.cowa.linbit.core.util.Dec.parse(b.locked).signum() > 0) "冻结 ${b.locked}" else b.asset
                    Text(sub, style = lb(FontSize.small), color = p.textSecondary)
                }
                Column(horizontalAlignment = Alignment.End) {
                    Text(if (s.hideAmount) "****" else balanceText(b.total), style = lbNum(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    val fiat = if (s.hideAmount) "≈ ****" else "≈ " + click.cowa.linbit.core.storage.QuoteDisplay.format(vm.usd(b), 2, s.cnyRate)
                    if (fiat != null) Text(fiat, style = lbNum(FontSize.small), color = p.textSecondary)
                }
            }
        }
    }
}

@Composable
private fun FuturesCard(vm: AssetsViewModel, s: AssetsUiState) {
    val p = LB.p
    val f = s.futures
    // 参考：账户权益 / 未实现盈亏 / 可用余额 · 体验金 / 抵扣金 / 仓位保证金 · 委托成本；每格下方 ≈ 法币
    val dec = click.cowa.linbit.core.util.Dec::parse
    val posMargin = f?.let { (dec(it.crossPositionMargin) + dec(it.isolatedMargin)).toPlainString() }
    val upnl = f?.let { (dec(it.crossUnrealizedPnl) + dec(it.isolatedUnrealizedPnl)).toPlainString() }
    val cells = listOf(
        "账户权益(USDT)" to f?.marginBalance,
        "未实现盈亏" to upnl,
        "可用余额" to f?.availableBalance,
        "体验金" to s.bonus?.trial,
        "抵扣金" to s.bonus?.deduction,
        "仓位保证金" to posMargin,
        "委托成本" to f?.orderMargin,
    )
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp)) {
        Text("持有仓位", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Row(Modifier.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            CoinIcon("USDT", 20.dp)
            Spacer(Modifier.width(6.dp))
            Text("USDT", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
        }
        cells.chunked(3).forEach { row ->
            Row(Modifier.fillMaxWidth().padding(bottom = 14.dp)) {
                row.forEachIndexed { i, (k, v) ->
                    val align = when (i) { 0 -> Alignment.Start; 1 -> Alignment.CenterHorizontally; else -> Alignment.End }
                    Column(Modifier.weight(1f), horizontalAlignment = align) {
                        Text(k, style = lb(11.sp), color = p.textSecondary, maxLines = 1)
                        val bonusCell = k == "体验金" || k == "抵扣金"
                        val text = if (s.hideAmount) "****" else if (bonusCell) bonusAmount(v) else v?.let { Fmt.amount(dec(it), 2) } ?: "--"
                        Text(text, style = lbNum(FontSize.label), color = p.textPrimary)
                        val approx = when {
                            s.hideAmount -> "≈ ****"
                            bonusCell && text == "--" -> "≈ --"
                            bonusCell -> "≈ " + click.cowa.linbit.core.storage.QuoteDisplay.format(dec(v), 2, s.cnyRate)
                            else -> "≈ " + (v?.let { click.cowa.linbit.core.storage.QuoteDisplay.format(dec(it), 2, s.cnyRate) } ?: "--")
                        }
                        Text(approx, style = lbNum(10.sp), color = p.textSecondary)
                    }
                }
                repeat(3 - row.size) { Spacer(Modifier.weight(1f)) }
            }
        }
    }
}

@Composable
private fun WithdrawMethodSheet(
    onClose: () -> Unit,
    onChain: () -> Unit,
    onInternal: () -> Unit,
    onRecords: () -> Unit,
    onAddressBook: () -> Unit,
) {
    val p = LB.p
    LbSheet("提现", onClose = onClose) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            MethodCard("链上提币", "往钱包或交易所转出数字货币", null, onChain)
            MethodCard("内部转账", "立即向其他 Linbit 用户转出数字货币", "0 手续费", onInternal)
            Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.SpaceEvenly) {
                Text("提现记录", Modifier.pressable(scale = 1f, onClick = onRecords), style = lb(FontSize.small), color = p.textSecondary)
                Text("地址簿", Modifier.pressable(scale = 1f, onClick = onAddressBook), style = lb(FontSize.small), color = p.textSecondary)
            }
        }
    }
}

@Composable
private fun MethodCard(title: String, sub: String, badge: String?, onClick: () -> Unit) {
    val p = LB.p
    Box(
        Modifier
            .fillMaxWidth()
            .border(1.dp, p.border, RoundedCornerShape(Radius.xl))
            .pressable(onClick = onClick)
            .padding(16.dp),
    ) {
        Column {
            Text(title, style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
            Spacer(Modifier.height(3.dp))
            Text(sub, style = lb(FontSize.small), color = p.textSecondary)
        }
        if (badge != null) {
            Text(
                badge,
                Modifier.align(Alignment.TopEnd).border(1.dp, p.brand, RoundedCornerShape(3.dp)).padding(horizontal = 5.dp, vertical = 2.dp),
                style = lb(10.sp, FontWeight.Medium),
                color = p.brand,
            )
        }
    }
}


/** 现货账户「现货闪兑 0 手续费 ›」入口卡（参考 spot 账户页）。 */
@Composable
private fun ConvertEntry(onClick: () -> Unit) {
    val p = LB.p
    Row(Modifier.padding(horizontal = Spacing.pageH).padding(top = 16.dp).fillMaxWidth().height(44.dp)
        .border(0.5.dp, p.border, RoundedCornerShape(Radius.md)).pressable(scale = 1f, onClick = onClick).padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically) {
        LbIcon(R.drawable.ic_cv_inverse, 16.dp, p.textPrimary)
        Text("现货闪兑", Modifier.padding(start = 8.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
        Text("0 手续费", Modifier.padding(start = 8.dp).background(p.brand, RoundedCornerShape(4.dp)).padding(horizontal = 5.dp, vertical = 1.dp), style = lb(10.sp, FontWeight.Medium), color = androidx.compose.ui.graphics.Color.Black)
        Spacer(Modifier.weight(1f))
        LbIcon(R.drawable.ic_chevron_right, 14.dp, p.textSecondary)
    }
}


/** 参考资产列表：余额去掉多余尾零，最少 2 位、最多 8 位小数（0 显示 0.00）。 */
internal fun balanceText(raw: String?): String {
    val d = click.cowa.linbit.core.util.Dec.parse(raw).setScale(8, java.math.RoundingMode.DOWN).stripTrailingZeros()
    val scale = d.scale().coerceIn(2, 8)
    return Fmt.amount(d, scale)
}


// ── 理财 / 策略 账户标签（参考 Earn / Bots 账户页，嵌在资产页内）──
@Composable
private fun AccountHead(label: String, total: java.math.BigDecimal?, hide: Boolean, cny: java.math.BigDecimal?, onToggle: () -> Unit, onHistory: (() -> Unit)? = null) {
    val p = LB.p
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, style = lb(FontSize.caption), color = p.textSecondary)
            Spacer(Modifier.width(6.dp))
            LbIcon(if (hide) R.drawable.ic_eye_off else R.drawable.ic_eye, 14.dp, p.textSecondary, Modifier.pressable(scale = 1f, onClick = onToggle))
            if (onHistory != null) {
                Spacer(Modifier.weight(1f))
                LbIcon(R.drawable.ic_doc_text, 18.dp, p.textPrimary, Modifier.pressable(scale = 1f, onClick = onHistory).testTag("assets.history"))
            }
        }
        Row(verticalAlignment = Alignment.Bottom) {
            Text(if (hide) "****" else total?.let { Fmt.amount(it, 2) } ?: "--", style = lbNum(32.sp, FontWeight.Bold), color = p.textPrimary)
            Spacer(Modifier.width(6.dp))
            Text("USDT", style = lb(FontSize.small), color = p.textPrimary)
            LbIcon(R.drawable.ic_caret_down, 10.dp, p.textPrimary, Modifier.padding(start = 2.dp, bottom = 9.dp))
        }
        Text(if (hide) "≈ ****" else "≈ " + (total?.let { click.cowa.linbit.core.storage.QuoteDisplay.format(it, 2, cny) } ?: "--"), style = lbNum(FontSize.small), color = p.textSecondary)
    }
}

@Composable
private fun KvTriple(items: List<Pair<String, String>>) {
    val p = LB.p
    Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 14.dp)) {
        items.forEachIndexed { i, (k, v) ->
            Column(Modifier.weight(1f), horizontalAlignment = if (i == 0) Alignment.Start else if (i == items.lastIndex && items.size == 3) Alignment.End else Alignment.Start) {
                Text(k, style = lb(11.sp), color = p.textSecondary, maxLines = 1)
                Text(v, style = lbNum(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            }
        }
    }
}

@Composable
private fun ActionRow(items: List<Triple<String, Int, () -> Unit>>) {
    val p = LB.p
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp).padding(top = 20.dp)) {
        items.forEach { (t, icon, act) ->
            Column(Modifier.weight(1f).pressable(onClick = act), horizontalAlignment = Alignment.CenterHorizontally) {
                Box(Modifier.size(40.dp).background(p.fill, RoundedCornerShape(Radius.lg)), contentAlignment = Alignment.Center) { LbIcon(icon, 22.dp, p.textPrimary) }
                Spacer(Modifier.height(8.dp))
                Text(t, style = lb(FontSize.small), color = p.textPrimary)
            }
        }
    }
}

@Composable
private fun EarnTab(
    s: AssetsUiState,
    onOpen: () -> Unit,
    onTransfer: () -> Unit,
    onToggle: () -> Unit,
    onHistory: () -> Unit,
    onOrders: () -> Unit,
) {
    val p = LB.p
    val vm: click.cowa.linbit.feature.trade.SpotSubpagesViewModel = androidx.hilt.navigation.compose.hiltViewModel()
    LaunchedEffect(Unit) { vm.loadEarn() }
    val sum = vm.earnSummary
    val dec = click.cowa.linbit.core.util.Dec::parse
    val signed = { v: String? -> v?.let { val d = dec(it); (if (d.signum() >= 0) "+" else "") + Fmt.amount(d, 2) + " USDT" } ?: "--" }
    AccountHead("总资产估值", sum?.totalValueUsdt?.let(dec), s.hideAmount, s.cnyRate, onToggle, onHistory = onHistory)
    KvTriple(listOf("累计收益" to (if (s.hideAmount) "****" else signed(sum?.totalInterestUsdt)), "昨日收益" to (if (s.hideAmount) "****" else signed(sum?.recentInterestUsdt))))
    ActionRow(listOf(Triple("总览", R.drawable.ic_asset_trade, onOpen), Triple("划转", R.drawable.ic_asset_transfer, onTransfer), Triple("订单", R.drawable.ic_doc_text, onOrders)))
    AssetsSectionGap()
    val best = vm.earnProducts.maxByOrNull { dec(it.apr) }
    if (best != null) Row(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp).fillMaxWidth().border(0.5.dp, p.border, RoundedCornerShape(Radius.md)).pressable(scale = 1f, onClick = onOpen).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text("活期理财 · 天天赚币", style = lb(FontSize.label, FontWeight.SemiBold), color = p.textPrimary)
            Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                CoinIcon(best.asset ?: "USDT", 16.dp)
                Text(best.title ?: best.asset ?: "", Modifier.padding(start = 6.dp).weight(1f), style = lb(FontSize.small), color = p.textPrimary, maxLines = 1)
                Text(Fmt.amount(dec(best.apr).multiply(java.math.BigDecimal(100)), 2) + "% 最高年化", style = lbNum(FontSize.small), color = p.up)
            }
        }
        LbIcon(R.drawable.ic_chevron_right, 14.dp, p.textSecondary)
    }
    var t by remember { mutableStateOf(0) }
    Row(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp), horizontalArrangement = Arrangement.spacedBy(20.dp)) {
        listOf("按币种", "按产品").forEachIndexed { i, x -> Text(x, Modifier.pressable(scale = 1f) { t = i }, style = lb(FontSize.body, if (t == i) FontWeight.SemiBold else FontWeight.Normal), color = if (t == i) p.textPrimary else p.textSecondary) }
    }
    val pos = vm.earnPositions
    if (pos.isEmpty()) EmptyState("暂无记录", Modifier.padding(vertical = 30.dp))
    else if (t == 0) pos.groupBy { it.asset ?: "--" }.forEach { (a, list) ->
        Row(Modifier.fillMaxWidth().height(56.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            CoinIcon(a); Text(a, Modifier.padding(start = 12.dp).weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(if (s.hideAmount) "****" else balanceText(list.fold(java.math.BigDecimal.ZERO) { x, y -> x + dec(y.amount) }.toPlainString()), style = lbNum(FontSize.body, FontWeight.Medium), color = p.textPrimary)
        }
    } else pos.forEach { x ->
        Row(Modifier.fillMaxWidth().height(56.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            Text(x.title ?: x.asset ?: "--", Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(if (s.hideAmount) "****" else "${balanceText(x.amount)} ${x.asset ?: ""}", style = lbNum(FontSize.body), color = p.textPrimary)
        }
    }
}

@Composable
private fun BotsTab(s: AssetsUiState, onOpen: () -> Unit, onTransfer: () -> Unit, onToggle: () -> Unit) {
    val p = LB.p
    val vm: click.cowa.linbit.feature.trade.SpotSubpagesViewModel = androidx.hilt.navigation.compose.hiltViewModel()
    LaunchedEffect(Unit) { vm.loadStrategy() }
    val dec = click.cowa.linbit.core.util.Dec::parse
    val running = vm.myGrids.filter { it.status?.value?.lowercase() == "running" }
    val equity = vm.myGrids.fold(java.math.BigDecimal.ZERO) { a, g -> a + dec(g.`value`) }
    val upnl = vm.myGrids.fold(java.math.BigDecimal.ZERO) { a, g -> a + dec(g.unrealizedPnl) }
    AccountHead("总资产估值", equity, s.hideAmount, s.cnyRate, onToggle)
    KvTriple(listOf("账户权益(USDT)" to (if (s.hideAmount) "****" else Fmt.amount(equity, 2)), "未实现盈亏(USDT)" to (if (s.hideAmount) "****" else Fmt.amount(upnl, 2)), "保证金率" to "--"))
    ActionRow(listOf(Triple("去交易", R.drawable.ic_asset_trade, onOpen), Triple("划转", R.drawable.ic_asset_transfer, onTransfer)))
    AssetsSectionGap()
    Text("运行中", Modifier.padding(horizontal = Spacing.pageH).padding(top = 16.dp), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
    if (running.isEmpty()) EmptyState("暂无记录", Modifier.padding(vertical = 30.dp))
    running.forEach { g ->
        Row(Modifier.fillMaxWidth().height(56.dp).pressable(scale = 1f, onClick = onOpen).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("${g.symbol ?: "--"} 现货网格", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                Text("投入 ${g.investment ?: "--"} USDT", style = lb(FontSize.small), color = p.textSecondary)
            }
            val tp = dec(g.totalProfit)
            Text(if (s.hideAmount) "****" else (if (tp.signum() >= 0) "+" else "") + Fmt.amount(tp, 2), style = lbNum(FontSize.body, FontWeight.Medium), color = if (tp.signum() < 0) p.down else p.up)
        }
    }
}


/** 币种详情（参考：顶部币种 + 记录图标；账户余额 / ≈ 法币；可用 / 冻结；「交易」币对卡）。 */
@Composable
private fun CoinDetailPage(vm: AssetsViewModel, asset: String, onBack: () -> Unit, onTrade: () -> Unit) {
    val p = LB.p
    val s = vm.state.collectAsStateWithLifecycle().value
    val list = if (s.tab == 1) s.funding else s.spot
    val b = list.firstOrNull { it.asset == asset }
    val dec = click.cowa.linbit.core.util.Dec::parse
    val total = dec(b?.total)
    androidx.compose.ui.window.Dialog(onDismissRequest = onBack, properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        val dv = androidx.compose.ui.platform.LocalView.current
        val lightBars = !LB.p.isDark
        androidx.compose.runtime.SideEffect {
            (dv.parent as? androidx.compose.ui.window.DialogWindowProvider)?.window?.let { w ->
                w.clearFlags(android.view.WindowManager.LayoutParams.FLAG_DIM_BEHIND)
                w.setDimAmount(0f)
                w.statusBarColor = android.graphics.Color.TRANSPARENT
                w.navigationBarColor = android.graphics.Color.TRANSPARENT
                androidx.core.view.WindowCompat.getInsetsController(w, dv).isAppearanceLightStatusBars = lightBars
            }
        }
        Column(Modifier.fillMaxSize().background(p.bg).windowInsetsPadding(androidx.compose.foundation.layout.WindowInsets.systemBars)) {
            Row(Modifier.fillMaxWidth().height(48.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(40.dp).pressable(scale = 1f, onClick = onBack), contentAlignment = Alignment.CenterStart) { LbIcon(R.drawable.ic_arrow_left, 22.dp, p.textPrimary) }
                Row(Modifier.weight(1f), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                    CoinIcon(asset, 22.dp); Text(asset, Modifier.padding(start = 6.dp), style = lb(FontSize.title, FontWeight.Medium), color = p.textPrimary)
                }
                Spacer(Modifier.size(40.dp))
            }
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp)) {
                Text(if (s.tab == 1) "资金账户余额" else "现货账户余额", style = lb(FontSize.caption), color = p.textTertiary)
                Text(if (s.hideAmount) "****" else balanceText(b?.total), Modifier.padding(top = 8.dp), style = lbNum(26.sp, FontWeight.Bold), color = p.textPrimary)
                Text("≈ " + (click.cowa.linbit.core.storage.QuoteDisplay.format((b?.let(vm::usd) ?: java.math.BigDecimal.ZERO), 2, s.cnyRate)), Modifier.padding(top = 4.dp), style = lbNum(FontSize.small), color = p.textTertiary)
                Row(Modifier.padding(top = 24.dp)) {
                    Column(Modifier.weight(1f)) { Text("可用", style = lb(FontSize.caption), color = p.textTertiary); Text(if (s.hideAmount) "****" else balanceText(b?.free), Modifier.padding(top = 4.dp), style = lbNum(FontSize.body), color = p.textPrimary) }
                    Column(Modifier.weight(1f)) { Text("冻结", style = lb(FontSize.caption), color = p.textTertiary); Text(if (s.hideAmount) "****" else balanceText(b?.locked), Modifier.padding(top = 4.dp), style = lbNum(FontSize.body), color = p.textPrimary) }
                }
                Box(Modifier.padding(top = 20.dp).fillMaxWidth().height(1.dp).background(p.border))
                val pair = vm.pairFor(asset)
                if (pair != null) {
                    Text("交易", Modifier.padding(top = 20.dp), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    Column(Modifier.padding(top = 12.dp).fillMaxWidth(0.47f).background(p.fill, RoundedCornerShape(Radius.md)).pressable(scale = 1f, onClick = onTrade).padding(horizontal = 16.dp, vertical = 20.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) { Text("${pair.base}/${pair.quote}", Modifier.weight(1f), style = lb(FontSize.caption), color = p.textSecondary); LbIcon(R.drawable.ic_chevron_right, 12.dp, p.textTertiary) }
                        Row(Modifier.padding(top = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(Fmt.priceAuto(pair.last), style = lbNum(FontSize.title, FontWeight.Medium), color = p.textPrimary)
                            Text(Fmt.percent(pair.changePct), Modifier.padding(start = 8.dp), style = lbNum(FontSize.title), color = if (pair.changePct.signum() < 0) p.down else p.up)
                        }
                    }
                }
            }
        }
    }
}

private enum class EarnHistoryKind { ORDERS, RECORDS }

/** 理财订单 / 理财流水（只读，数据来自 GET /earn/orders 与 /earn/records）。 */
@Composable
private fun EarnHistorySheet(title: String, empty: String, onClose: () -> Unit, kind: EarnHistoryKind) {
    val p = LB.p
    val vm: click.cowa.linbit.feature.trade.SpotSubpagesViewModel = androidx.hilt.navigation.compose.hiltViewModel()
    LaunchedEffect(Unit) { vm.loadEarn() }
    LbSheet(title, onClose, showClose = true) {
        Column(
            Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)
                .testTag(if (kind == EarnHistoryKind.ORDERS) "assets.earn.orders" else "assets.earn.records"),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            when (kind) {
                EarnHistoryKind.ORDERS -> {
                    val items = vm.earnOrders
                    val err = vm.earnAccountError
                    when {
                        err != null && items.isEmpty() -> EmptyState(err, verticalPadding = 40.dp) {
                            click.cowa.linbit.ui.components.ChipButton("重试", Modifier.height(32.dp)) { vm.loadEarn() }
                        }
                        items.isEmpty() -> EmptyState(empty, verticalPadding = 40.dp)
                        else -> items.take(50).forEach { o ->
                            Column(
                                Modifier.fillMaxWidth().border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
                                verticalArrangement = Arrangement.spacedBy(4.dp),
                            ) {
                                Text(
                                    "${o.title ?: o.asset ?: "--"} · ${o.status ?: "--"}",
                                    style = lb(FontSize.body, FontWeight.Medium),
                                    color = p.textPrimary,
                                )
                                Text("金额 ${o.amount ?: "--"} ${o.asset ?: ""}", style = lb(FontSize.small), color = p.textSecondary)
                                Text("利息 ${o.interest ?: "--"}", style = lb(FontSize.small), color = p.textSecondary)
                            }
                        }
                    }
                }
                EarnHistoryKind.RECORDS -> {
                    val items = vm.earnRecords
                    val err = vm.earnAccountError
                    when {
                        err != null && items.isEmpty() -> EmptyState(err, verticalPadding = 40.dp) {
                            click.cowa.linbit.ui.components.ChipButton("重试", Modifier.height(32.dp)) { vm.loadEarn() }
                        }
                        items.isEmpty() -> EmptyState(empty, verticalPadding = 40.dp)
                        else -> items.take(50).forEach { r ->
                            val typeLabel = when (r.type) {
                                click.cowa.linbit.core.generated.ApiEarnRecords200ResponseItemsInner.Type.SUBSCRIBE -> "申购"
                                click.cowa.linbit.core.generated.ApiEarnRecords200ResponseItemsInner.Type.REDEEM -> "赎回"
                                click.cowa.linbit.core.generated.ApiEarnRecords200ResponseItemsInner.Type.INTEREST -> "利息"
                                click.cowa.linbit.core.generated.ApiEarnRecords200ResponseItemsInner.Type.MATURITY -> "到期"
                                click.cowa.linbit.core.generated.ApiEarnRecords200ResponseItemsInner.Type.UNLOCK -> "解锁"
                                null -> "--"
                            }
                            Column(
                                Modifier.fillMaxWidth().border(1.dp, p.border, RoundedCornerShape(Radius.md)).padding(12.dp),
                                verticalArrangement = Arrangement.spacedBy(4.dp),
                            ) {
                                Text("$typeLabel · ${r.asset ?: "--"}", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                                Text("数量 ${r.amount ?: "--"}", style = lb(FontSize.small), color = p.textSecondary)
                            }
                        }
                    }
                }
            }
        }
    }
}
