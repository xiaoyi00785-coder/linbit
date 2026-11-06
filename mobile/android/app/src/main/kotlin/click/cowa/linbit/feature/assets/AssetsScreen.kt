package click.cowa.linbit.feature.assets

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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

private val Tabs = listOf("总览", "资金", "合约", "现货", "理财", "策略", "TradFi")

@Composable
fun AssetsScreen(
    bottomInset: Dp,
    onLogin: () -> Unit,
    assetsNav: AssetsNav,
    onOpenFutures: () -> Unit = {},
    onOpenSpot: () -> Unit = {},
    onOpenProfile: () -> Unit = {},
    vm: AssetsViewModel = hiltViewModel(),
) {
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    val s by vm.state.collectAsStateWithLifecycle()
    var showWithdraw by remember { mutableStateOf(false) }
    var showFilter by remember { mutableStateOf(false) }
    var showSearch by remember { mutableStateOf(false) }
    val p = LB.p

    LaunchedEffect(sessionVersion) { vm.load() }

    Column(Modifier.fillMaxSize().background(p.bg)) {
        Box(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
            PrimaryTabs(Tabs, s.tab, onSelect = { i ->
                if (i >= 4) vm.toast.show("${Tabs[i]}即将开放") else vm.setTab(i)
            }, Modifier.padding(horizontal = Spacing.pageH), scrollable = true)
        }
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
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 12.dp), horizontalArrangement = Arrangement.End) {
                LbIcon(R.drawable.ic_avatar, 22.dp, p.textPrimary, Modifier.pressable(scale = 1f, onClick = onOpenProfile))
            }
            if (emptyOverview) {
                OverviewEmpty(onDeposit = { assetsNav.openDeposit(switchTab = false) }, onFutures = onOpenFutures)
            } else {
                Header(s.hideAmount, total, s.cnyRate, s.tab, vm, onToggle = vm::toggleHide)
                Actions(
                    tab = s.tab,
                    onDeposit = { assetsNav.openDeposit() },
                    onWithdraw = { showWithdraw = true },
                    onTransfer = { assetsNav.openTransfer() },
                    onTrade = { if (s.tab == 2) onOpenFutures() else onOpenSpot() },
                    onSoon = { vm.toast.show(it) },
                )
                if (s.tab == 2) FuturesCard(vm, s) else AssetList(
                    vm, s, showSearch,
                    onSearch = { showSearch = it },
                    onFilter = { showFilter = true },
                    onOpenAsset = { assetsNav.openDeposit(it) },
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
}

@Composable
private fun OverviewEmpty(onDeposit: () -> Unit, onFutures: () -> Unit) {
    val p = LB.p
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 12.dp)) {
        Box(Modifier.fillMaxWidth().height(180.dp), contentAlignment = Alignment.Center) {
            Box(Modifier.size(150.dp, 120.dp).background(p.fill, RoundedCornerShape(16.dp)))
            Text("Linbit", style = lb(18.sp, FontWeight.Bold), color = p.textTertiary)
        }
        Text("完成充值，开启您的 Linbit 之旅!", style = lb(24.sp, FontWeight.Bold), color = p.textPrimary)
        Spacer(Modifier.height(12.dp))
        LbButton("充值", onClick = onDeposit)
        Spacer(Modifier.height(16.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("您的账户尚无可用资产", style = lb(11.sp), color = p.textSecondary)
                Text("充值任意金额，参与合约交易", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                Text("立即查看 ›", Modifier.pressable(scale = 1f, onClick = onFutures).padding(top = 4.dp), style = lb(FontSize.small, FontWeight.Medium), color = p.textPrimary)
            }
        }
        Spacer(Modifier.height(16.dp))
        Box(Modifier.fillMaxWidth().height(1.dp).background(p.divider))
        Spacer(Modifier.height(12.dp))
        Text("责任与守护", style = lb(FontSize.title, FontWeight.Bold), color = p.textPrimary)
        Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("资产安全，我们的第一责任", style = lb(FontSize.label), color = p.textPrimary)
                Text("查看更多", style = lb(FontSize.small), color = p.brand)
            }
        }
    }
}

@Composable
private fun Header(hide: Boolean, total: java.math.BigDecimal, cny: java.math.BigDecimal?, tab: Int, vm: AssetsViewModel, onToggle: () -> Unit) {
    val p = LB.p
    val s = vm.state.collectAsStateWithLifecycle().value
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("总资产估值", style = lb(FontSize.caption), color = p.textSecondary)
            Spacer(Modifier.width(6.dp))
            LbIcon(if (hide) R.drawable.ic_eye_off else R.drawable.ic_eye, 14.dp, p.textSecondary, Modifier.pressable(scale = 1f, onClick = onToggle))
            Spacer(Modifier.weight(1f))
            LbIcon(R.drawable.ic_doc_text, 18.dp, p.textPrimary)
        }
        Row(verticalAlignment = Alignment.Bottom) {
            Text(if (hide) "****" else Fmt.amount(total, 2), style = lbNum(32.sp, FontWeight.Bold), color = p.textPrimary)
            Spacer(Modifier.width(6.dp))
            Text("USDT", style = lb(FontSize.small), color = p.textPrimary)
        }
        val fiat = if (hide) "≈ ****" else if (cny != null) "≈ " + Fmt.fiat(total, cny, 2) else null
        if (fiat != null) Text(fiat, style = lbNum(FontSize.small), color = p.textSecondary)
        if (tab == 1 || tab == 2) {
            val label = if (tab == 2) "今日盈亏" else "可用余额"
            val value = if (hide) "****" else if (tab == 2) {
                "${Fmt.amount(click.cowa.linbit.core.util.Dec.parse(s.futures?.crossUnrealizedPnl), 2)} USD"
            } else "${Fmt.amount(vm.spotAvailUsdt(), 2)} USDT"
            Row(Modifier.padding(top = 8.dp)) {
                Text(label, style = lb(FontSize.small), color = p.textSecondary)
                Spacer(Modifier.width(8.dp))
                Text(value, style = lbNum(FontSize.small, FontWeight.Medium), color = p.textPrimary)
            }
        }
    }
}

@Composable
private fun Actions(
    tab: Int,
    onDeposit: () -> Unit,
    onWithdraw: () -> Unit,
    onTransfer: () -> Unit,
    onTrade: () -> Unit,
    onSoon: (String) -> Unit,
) {
    val p = LB.p
    val items: List<Pair<String, () -> Unit>> = when (tab) {
        2 -> listOf("去交易" to onTrade, "划转" to onTransfer, "盈亏" to { onSoon("盈亏分析即将开放") })
        3 -> listOf("去交易" to onTrade, "充值" to onDeposit, "快捷买币" to { onSoon("快捷买币即将开放") }, "划转" to onTransfer)
        else -> listOf("充值" to onDeposit, "快捷买币" to { onSoon("快捷买币即将开放") }, "提现" to onWithdraw, "划转" to onTransfer)
    }
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp).padding(top = 20.dp)) {
        items.forEach { (title, act) ->
            Column(
                Modifier.weight(1f).pressable(onClick = act),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Box(
                    Modifier.size(40.dp).background(p.fill, RoundedCornerShape(Radius.lg)),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(title.take(1), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                }
                Spacer(Modifier.height(8.dp))
                Text(title, style = lb(FontSize.small), color = p.textPrimary)
            }
        }
    }
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
        Box(Modifier.fillMaxWidth().height(8.dp).background(p.fill).padding(top = 20.dp))
        Row(
            Modifier.fillMaxWidth().height(56.dp).padding(horizontal = Spacing.pageH),
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
        if (rows.isEmpty()) EmptyState(if (s.query.isEmpty()) "暂无资产" else "暂无相关币种", Modifier.padding(vertical = 40.dp))
        rows.forEach { b ->
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
                    Text(if (s.hideAmount) "****" else b.total, style = lbNum(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    val fiat = if (s.hideAmount) "≈ ****" else s.cnyRate?.let { "≈ " + Fmt.fiat(vm.usd(b), it, 2) }
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
    val cells = listOf(
        "账户权益(USDT)" to f?.marginBalance,
        "未实现盈亏" to f?.crossUnrealizedPnl,
        "可用余额" to f?.availableBalance,
        "钱包余额" to f?.walletBalance,
        "委托保证金" to f?.orderMargin,
        "全仓保证金" to f?.crossPositionMargin,
        "维持保证金" to f?.crossMaintenanceMargin,
        "逐仓保证金" to f?.isolatedMargin,
        "逐仓未实现盈亏" to f?.isolatedUnrealizedPnl,
    )
    Column(Modifier.padding(horizontal = Spacing.pageH).padding(top = 20.dp)) {
        Box(Modifier.fillMaxWidth().height(8.dp).background(p.fill))
        Spacer(Modifier.height(12.dp))
        Text("持有仓位", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Row(Modifier.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            CoinIcon("USDT", 20.dp)
            Spacer(Modifier.width(6.dp))
            Text("USDT", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
        }
        cells.chunked(3).forEach { row ->
            Row(Modifier.fillMaxWidth().padding(bottom = 14.dp)) {
                row.forEach { (k, v) ->
                    Column(Modifier.weight(1f)) {
                        Text(k, style = lb(11.sp), color = p.textSecondary, maxLines = 1)
                        Text(if (s.hideAmount) "****" else Fmt.amount(click.cowa.linbit.core.util.Dec.parse(v), 2), style = lbNum(FontSize.label), color = p.textPrimary)
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
