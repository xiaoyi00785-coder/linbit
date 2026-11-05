package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiFuturesBills200ResponseBillsInner
import click.cowa.linbit.core.generated.ApiFuturesMyTrades200ResponseTradesInner
import click.cowa.linbit.core.generated.ApiFuturesOrder
import click.cowa.linbit.core.generated.ApiFuturesPositionHistory
import click.cowa.linbit.core.generated.ApiOrder
import click.cowa.linbit.core.generated.ApiTrade
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.R
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class TradeHistoryViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
) : ViewModel() {
    val loggedIn get() = tokens.loggedIn

    var spotOpen by mutableStateOf<List<ApiOrder>>(emptyList()); private set
    var spotHistory by mutableStateOf<List<ApiOrder>>(emptyList()); private set
    var spotTrades by mutableStateOf<List<ApiTrade>>(emptyList()); private set
    var futOrders by mutableStateOf<List<ApiFuturesOrder>>(emptyList()); private set
    var futHistory by mutableStateOf<List<ApiFuturesOrder>>(emptyList()); private set
    var futPositions by mutableStateOf<List<ApiFuturesPositionHistory>>(emptyList()); private set
    var futTrades by mutableStateOf<List<ApiFuturesMyTrades200ResponseTradesInner>>(emptyList()); private set
    var futBills by mutableStateOf<List<ApiFuturesBills200ResponseBillsInner>>(emptyList()); private set
    var loading by mutableStateOf(false); private set
    var error by mutableStateOf<String?>(null); private set

    fun loadSpot(tab: Int, symbol: String?) {
        if (!tokens.loggedIn.value) return
        viewModelScope.launch {
            loading = true; error = null
            try {
                when (tab) {
                    0 -> spotOpen = apiCall { api.spotOpenOrders(symbol) }.items
                    1 -> spotHistory = apiCall { api.spotOrderHistory(symbol = symbol) }.items
                    2 -> spotTrades = apiCall { api.spotTrades(symbol = symbol) }.items
                }
            } catch (e: Throwable) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    fun loadFutures(tab: Int, symbol: String?) {
        if (!tokens.loggedIn.value) return
        viewModelScope.launch {
            loading = true; error = null
            try {
                when (tab) {
                    0 -> futOrders = (apiCall { api.futuresOrders(symbol = symbol, status = "open") }.orders ?: emptyList())
                    1 -> futHistory = (apiCall { api.futuresOrders(symbol = symbol) }.orders ?: emptyList()).filter { it.status != "new" && it.status != "partially_filled" }
                    2 -> futPositions = apiCall { api.futuresPositionHistory(symbol = symbol) }.positions ?: emptyList()
                    3 -> futTrades = apiCall { api.futuresMyTrades(symbol = symbol) }.trades ?: emptyList()
                    4 -> futBills = apiCall { api.futuresBills() }.bills ?: emptyList()
                }
            } catch (e: Throwable) { error = e.message ?: "加载失败" }
            finally { loading = false }
        }
    }

    fun cancelSpot(id: String, symbol: String?, onDone: () -> Unit) {
        viewModelScope.launch {
            try { apiCall { api.cancelSpotOrder(id) }; onDone() } catch (_: Throwable) {}
        }
    }

    fun cancelFutures(id: Long, onDone: () -> Unit) {
        viewModelScope.launch {
            try { apiCall { api.cancelFuturesOrder(id) }; onDone() } catch (_: Throwable) {}
        }
    }
}

@Composable
fun SpotHistoryScreen(onClose: () -> Unit, onLogin: () -> Unit, symbol: String? = null, vm: TradeHistoryViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var tab by remember { mutableStateOf(0) }
    LaunchedEffect(tab, loggedIn) { if (loggedIn) vm.loadSpot(tab, symbol) }
    HistoryShell("历史记录", onClose) {
        PrimaryTabs(listOf("当前委托", "历史委托", "成交明细"), tab, { tab = it }, size = FontSize.label)
        HorizontalDivider(Modifier.padding(top = 8.dp), color = p.divider)
        when {
            !loggedIn -> EmptyState("登录后查看历史记录") { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) }
            vm.loading && listForSpot(vm, tab).isEmpty() -> Box(Modifier.fillMaxWidth().height(200.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && listForSpot(vm, tab).isEmpty() -> EmptyState(vm.error ?: "加载失败") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadSpot(tab, symbol) } }
            listForSpot(vm, tab).isEmpty() -> EmptyState("暂无数据")
            tab == 0 -> vm.spotOpen.forEach { o ->
                HistOrderRow(o.symbol, o.side.value, o.price, o.quantity, o.filledQuantity, o.status.value) {
                    vm.cancelSpot(o.orderId, o.symbol) { vm.loadSpot(0, symbol) }
                }
            }
            tab == 1 -> vm.spotHistory.forEach { o -> HistOrderRow(o.symbol, o.side.value, o.price, o.quantity, o.filledQuantity, o.status.value, showCancel = false) }
            else -> vm.spotTrades.forEach { t -> HistTradeRow(t.symbol, t.side.value, t.price, t.quantity, t.createdAt) }
        }
    }
}

@Composable
fun FuturesHistoryScreen(onClose: () -> Unit, onLogin: () -> Unit, symbol: String? = null, vm: TradeHistoryViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    var tab by remember { mutableStateOf(0) }
    val tabs = listOf("当前委托", "历史委托", "历史仓位", "成交明细", "资金流水")
    LaunchedEffect(tab, loggedIn) { if (loggedIn) vm.loadFutures(tab, symbol) }
    HistoryShell("历史记录", onClose) {
        Row(
            Modifier.fillMaxWidth().padding(bottom = 4.dp).horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            tabs.forEachIndexed { i, label ->
                Column(Modifier.pressable(scale = 1f) { tab = i }, horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(label, style = lb(FontSize.label, if (tab == i) FontWeight.SemiBold else FontWeight.Normal), color = if (tab == i) p.textPrimary else p.textSecondary)
                    Box(Modifier.padding(top = 6.dp).width(16.dp).height(2.dp).background(if (tab == i) p.textPrimary else androidx.compose.ui.graphics.Color.Transparent))
                }
            }
        }
        HorizontalDivider(color = p.divider)
        when {
            !loggedIn -> EmptyState("登录后查看历史记录") { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) }
            vm.loading && listForFut(vm, tab).isEmpty() -> Box(Modifier.fillMaxWidth().height(200.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && listForFut(vm, tab).isEmpty() -> EmptyState(vm.error ?: "加载失败") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadFutures(tab, symbol) } }
            listForFut(vm, tab).isEmpty() -> EmptyState("暂无数据")
            tab == 0 -> vm.futOrders.forEach { o ->
                HistOrderRow(o.symbol ?: "", o.side ?: "", o.price, o.qty, o.filledQty, o.status, showCancel = true) {
                    o.orderId?.let { id -> vm.cancelFutures(id) { vm.loadFutures(0, symbol) } }
                }
            }
            tab == 1 -> vm.futHistory.forEach { o -> HistOrderRow(o.symbol ?: "", o.side ?: "", o.price, o.qty, o.filledQty, o.status, showCancel = false) }
            tab == 2 -> vm.futPositions.forEach { PosHistRow(it) }
            tab == 3 -> vm.futTrades.forEach { t -> HistTradeRow(t.symbol ?: "", t.side?.value ?: "", t.price, t.qty, t.time) }
            else -> vm.futBills.forEach { b -> BillRow(b) }
        }
    }
}

private fun listForSpot(vm: TradeHistoryViewModel, tab: Int) = when (tab) {
    0 -> vm.spotOpen; 1 -> vm.spotHistory; else -> vm.spotTrades
}
private fun listForFut(vm: TradeHistoryViewModel, tab: Int) = when (tab) {
    0 -> vm.futOrders; 1 -> vm.futHistory; 2 -> vm.futPositions; 3 -> vm.futTrades; else -> vm.futBills
}

@Composable
private fun HistoryShell(title: String, onClose: () -> Unit, content: @Composable () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding()) {
        Row(Modifier.fillMaxWidth().height(48.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            LbIcon(R.drawable.ic_arrow_left, 22.dp, p.textPrimary, Modifier.pressable(scale = 1f, onClick = onClose))
            Text(title, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary, textAlign = TextAlign.Center)
            Box(Modifier.width(22.dp))
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            content()
        }
    }
}

@Composable
private fun HistOrderRow(symbol: String, side: String, price: String?, qty: String?, filled: String?, status: String?, showCancel: Boolean = true, onCancel: (() -> Unit)? = null) {
    val p = LB.p
    val buy = side.equals("buy", true)
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(symbol.replace("USDT", "/USDT"), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (buy) "买入" else "卖出"}", style = lb(FontSize.small), color = if (buy) p.up else p.down)
            Box(Modifier.weight(1f))
            if (showCancel && onCancel != null) ChipButton("撤单", Modifier.height(24.dp), onClick = onCancel)
            else Text(status ?: "", style = lb(FontSize.small), color = p.textSecondary)
        }
        Row {
            KVCell("价格", price ?: "市价", Modifier.weight(1f))
            KVCell("数量", qty ?: "--", Modifier.weight(1f), TextAlign.Center)
            KVCell("已成交", filled ?: "0", Modifier.weight(1f), TextAlign.End)
        }
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun HistTradeRow(symbol: String, side: String, price: String?, qty: String?, time: String?) {
    val p = LB.p
    val buy = side.equals("buy", true)
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(symbol.replace("USDT", "/USDT"), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (buy) "买入" else "卖出"}", style = lb(FontSize.small), color = if (buy) p.up else p.down)
            Box(Modifier.weight(1f))
            Text(time?.take(19)?.replace('T', ' ') ?: "--", style = lb(FontSize.small), color = p.textTertiary)
        }
        Row {
            KVCell("成交价", price ?: "--", Modifier.weight(1f))
            KVCell("数量", qty ?: "--", Modifier.weight(1f), TextAlign.End)
        }
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun PosHistRow(h: ApiFuturesPositionHistory) {
    val p = LB.p
    val pnl = Dec.parse(h.netPnl ?: h.realizedPnl)
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(h.symbol ?: "", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (h.side == ApiFuturesPositionHistory.Side.SHORT) "空" else "多"}", style = lb(FontSize.small), color = if (h.side == ApiFuturesPositionHistory.Side.SHORT) p.down else p.up)
            Box(Modifier.weight(1f))
            Text((if (pnl.signum() > 0) "+" else "") + Fmt.amount(pnl, 4), style = lbNum(FontSize.label, FontWeight.Medium), color = if (pnl.signum() < 0) p.down else p.up)
        }
        Row {
            KVCell("开仓价", Fmt.priceAuto(Dec.parse(h.entryPrice)), Modifier.weight(1f))
            KVCell("平仓价", Fmt.priceAuto(Dec.parse(h.closePrice)), Modifier.weight(1f), TextAlign.Center)
            KVCell("数量", h.closedQty ?: h.maxQty ?: "--", Modifier.weight(1f), TextAlign.End)
        }
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun BillRow(b: ApiFuturesBills200ResponseBillsInner) {
    val p = LB.p
    val amt = Dec.parse(b.amount)
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(b.kind ?: "--", style = lb(FontSize.body), color = p.textPrimary)
            Box(Modifier.weight(1f))
            Text((if (amt.signum() > 0) "+" else "") + Fmt.amount(amt, 4), style = lbNum(FontSize.label), color = if (amt.signum() < 0) p.down else p.up)
        }
        Text(b.time?.take(19)?.replace('T', ' ') ?: "--", style = lb(FontSize.small), color = p.textTertiary)
    }
    HorizontalDivider(color = p.divider)
}
