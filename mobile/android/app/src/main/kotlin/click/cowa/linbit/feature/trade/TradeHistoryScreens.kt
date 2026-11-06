package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.R
import click.cowa.linbit.core.generated.ApiFuturesAlgoOrder
import click.cowa.linbit.core.generated.ApiFuturesBills200ResponseBillsInner
import click.cowa.linbit.core.generated.ApiFuturesMyTrades200ResponseTradesInner
import click.cowa.linbit.core.generated.ApiFuturesOrder
import click.cowa.linbit.core.generated.ApiFuturesPositionHistory
import click.cowa.linbit.core.generated.ApiOrder
import click.cowa.linbit.core.generated.ApiTrade
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LayoutAdapt
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
    val sessionVersion get() = tokens.sessionVersion

    var spotOpen by mutableStateOf<List<ApiOrder>>(emptyList()); private set
    var spotHistory by mutableStateOf<List<ApiOrder>>(emptyList()); private set
    var spotTrades by mutableStateOf<List<ApiTrade>>(emptyList()); private set
    var futOrders by mutableStateOf<List<ApiFuturesOrder>>(emptyList()); private set
    var futHistory by mutableStateOf<List<ApiFuturesOrder>>(emptyList()); private set
    var futPositions by mutableStateOf<List<ApiFuturesPositionHistory>>(emptyList()); private set
    var futTrades by mutableStateOf<List<ApiFuturesMyTrades200ResponseTradesInner>>(emptyList()); private set
    var futBills by mutableStateOf<List<ApiFuturesBills200ResponseBillsInner>>(emptyList()); private set
    var futAlgos by mutableStateOf<List<ApiFuturesAlgoOrder>>(emptyList()); private set
    var posCursor by mutableStateOf<Long?>(null); private set
    var algoCursor by mutableStateOf<Long?>(null); private set
    var loading by mutableStateOf(false); private set
    var loadingMore by mutableStateOf(false); private set
    var error by mutableStateOf<String?>(null); private set
    var actionError by mutableStateOf<String?>(null); private set

    fun clear() {
        spotOpen = emptyList(); spotHistory = emptyList(); spotTrades = emptyList()
        futOrders = emptyList(); futHistory = emptyList(); futPositions = emptyList()
        futTrades = emptyList(); futBills = emptyList(); futAlgos = emptyList()
        posCursor = null; algoCursor = null
        loading = false; loadingMore = false; error = null; actionError = null
    }

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
            } catch (e: Throwable) { error = e.lbMessage() }
            finally { loading = false }
        }
    }

    /**
     * @param tab 0 当前委托 1 历史委托 2 历史仓位 3 成交 4 流水 5 条件委托
     * @param timeFilter 仓位时间档
     * @param billKind 流水 kind
     * @param algoPlan true=计划委托 false=止盈止损
     * @param algoHistory status=history
     */
    fun loadFutures(
        tab: Int,
        symbol: String?,
        timeFilter: Int = 1,
        billKind: String? = null,
        algoPlan: Boolean = true,
        algoHistory: Boolean = false,
    ) {
        if (!tokens.loggedIn.value) return
        viewModelScope.launch {
            loading = true; error = null; posCursor = null; algoCursor = null
            try {
                when (tab) {
                    0 -> futOrders = apiCall { api.futuresOrders(symbol = symbol, status = "open") }.orders.orEmpty()
                    1 -> futHistory = apiCall { api.futuresOrders(symbol = symbol) }.orders.orEmpty()
                        .filter { !TradeHistoryLogic.isOpenStatus(it.status) }
                    2 -> {
                        val start = TradeHistoryLogic.positionStartTimeMs(timeFilter)
                        val r = apiCall { api.futuresPositionHistory(symbol = symbol, startTime = start, limit = 50) }
                        futPositions = r.positions.orEmpty(); posCursor = r.nextCursor
                    }
                    3 -> futTrades = apiCall { api.futuresMyTrades(symbol = symbol) }.trades.orEmpty()
                    4 -> futBills = apiCall { api.futuresBills(kind = billKind) }.bills.orEmpty()
                    5 -> {
                        val status = TradeHistoryLogic.algoStatusParam(algoHistory)
                        val r = if (algoPlan) apiCall { api.futuresPlanOrders(symbol = symbol, status = status) }
                        else apiCall { api.futuresTpslOrders(symbol = symbol, status = status) }
                        futAlgos = r.orders.orEmpty(); algoCursor = r.nextCursor
                    }
                }
            } catch (e: Throwable) { error = e.lbMessage() }
            finally { loading = false }
        }
    }

    fun loadMorePositions(symbol: String?, timeFilter: Int) {
        val c = posCursor ?: return
        if (loadingMore) return
        viewModelScope.launch {
            loadingMore = true
            try {
                val start = TradeHistoryLogic.positionStartTimeMs(timeFilter)
                val r = apiCall { api.futuresPositionHistory(symbol = symbol, startTime = start, cursor = c, limit = 50) }
                futPositions = futPositions + r.positions.orEmpty(); posCursor = r.nextCursor
            } catch (e: Throwable) { actionError = e.lbMessage() }
            finally { loadingMore = false }
        }
    }

    fun loadMoreAlgos(symbol: String?, algoPlan: Boolean, algoHistory: Boolean) {
        val c = algoCursor ?: return
        if (loadingMore) return
        viewModelScope.launch {
            loadingMore = true
            try {
                val status = TradeHistoryLogic.algoStatusParam(algoHistory)
                val r = if (algoPlan) apiCall { api.futuresPlanOrders(symbol = symbol, status = status, cursor = c) }
                else apiCall { api.futuresTpslOrders(symbol = symbol, status = status, cursor = c) }
                futAlgos = futAlgos + r.orders.orEmpty(); algoCursor = r.nextCursor
            } catch (e: Throwable) { actionError = e.lbMessage() }
            finally { loadingMore = false }
        }
    }

    fun cancelSpot(id: String, onDone: () -> Unit) {
        viewModelScope.launch {
            try { apiCall { api.cancelSpotOrder(id) }; actionError = null; onDone() }
            catch (e: Throwable) { actionError = e.lbMessage() }
        }
    }

    fun cancelFutures(id: Long, onDone: () -> Unit) {
        viewModelScope.launch {
            try { apiCall { api.cancelFuturesOrder(id) }; actionError = null; onDone() }
            catch (e: Throwable) { actionError = e.lbMessage() }
        }
    }

    fun cancelAlgo(id: Long, plan: Boolean, onDone: () -> Unit) {
        viewModelScope.launch {
            try {
                if (plan) apiCall { api.cancelFuturesPlan(id) } else apiCall { api.cancelFuturesTpsl(id) }
                actionError = null; onDone()
            } catch (e: Throwable) { actionError = e.lbMessage() }
        }
    }
}

@Composable
fun SpotHistoryScreen(onClose: () -> Unit, onLogin: () -> Unit, symbol: String? = null, vm: TradeHistoryViewModel = hiltViewModel()) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    var tab by remember { mutableIntStateOf(0) }
    LaunchedEffect(tab, sessionVersion) {
        vm.clear()
        if (loggedIn) vm.loadSpot(tab, symbol)
    }
    HistoryShell("历史记录", onClose) {
        PrimaryTabs(listOf("当前委托", "历史委托", "成交明细"), tab, { tab = it }, size = FontSize.label, scrollable = true)
        HorizontalDivider(Modifier.padding(top = 8.dp), color = p.divider)
        vm.actionError?.let { Text(it, Modifier.padding(vertical = 6.dp), style = lb(FontSize.small), color = p.down) }
        when {
            !loggedIn -> EmptyState("登录后查看历史记录") { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) }
            vm.loading && listForSpot(vm, tab).isEmpty() -> Box(Modifier.fillMaxWidth().height(200.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && listForSpot(vm, tab).isEmpty() -> EmptyState(vm.error ?: "加载失败") { ChipButton("重试", Modifier.height(32.dp)) { vm.loadSpot(tab, symbol) } }
            listForSpot(vm, tab).isEmpty() -> EmptyState("暂无数据")
            tab == 0 -> vm.spotOpen.forEach { o ->
                HistOrderRow(o.symbol, o.side.value, o.price, o.quantity, o.filledQuantity, o.status.value) {
                    vm.cancelSpot(o.orderId) { vm.loadSpot(0, symbol) }
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
    val sessionVersion by vm.sessionVersion.collectAsStateWithLifecycle()
    var tab by remember { mutableIntStateOf(0) }
    var typeFilter by remember { mutableIntStateOf(0) }
    var timeFilter by remember { mutableIntStateOf(1) }
    var billKind by remember { mutableStateOf<String?>(null) }
    var algoPlan by remember { mutableStateOf(true) }
    var algoHistory by remember { mutableStateOf(false) }
    val tabs = listOf("当前委托", "历史委托", "历史仓位", "成交明细", "资金流水", "条件委托")
    fun reload() {
        if (loggedIn) vm.loadFutures(tab, symbol, timeFilter, billKind, algoPlan, algoHistory)
    }
    LaunchedEffect(tab, sessionVersion, timeFilter, billKind, algoPlan, algoHistory) {
        vm.clear()
        reload()
    }
    HistoryShell("历史记录", onClose) {
        Row(
            Modifier.fillMaxWidth().padding(bottom = 4.dp).horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            tabs.forEachIndexed { i, label ->
                Column(Modifier.pressable(scale = 1f) { tab = i }, horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(label, style = lb(FontSize.label, if (tab == i) FontWeight.SemiBold else FontWeight.Normal), color = if (tab == i) p.textPrimary else p.textSecondary, maxLines = 1, softWrap = false)
                    Box(Modifier.padding(top = 6.dp).width(16.dp).height(2.dp).background(if (tab == i) p.textPrimary else androidx.compose.ui.graphics.Color.Transparent))
                }
            }
        }
        HistoryFilterBar(
            tab = tab,
            typeFilter = typeFilter,
            onType = { typeFilter = it },
            timeFilter = timeFilter,
            onTime = { timeFilter = it },
            billKind = billKind,
            onBillKind = { billKind = it },
            algoPlan = algoPlan,
            onAlgoPlan = { algoPlan = it },
            algoHistory = algoHistory,
            onAlgoHistory = { algoHistory = it },
        )
        HorizontalDivider(color = p.divider)
        vm.actionError?.let { Text(it, Modifier.padding(vertical = 6.dp), style = lb(FontSize.small), color = p.down) }
        val emptyList = listForFut(vm, tab).isEmpty()
        when {
            !loggedIn -> EmptyState("登录后查看历史记录") { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) }
            vm.loading && emptyList -> Box(Modifier.fillMaxWidth().height(200.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            vm.error != null && emptyList -> EmptyState(vm.error ?: "加载失败") { ChipButton("重试", Modifier.height(32.dp)) { reload() } }
            emptyList -> EmptyState("暂无数据")
            tab == 0 -> TradeHistoryLogic.filterOrders(vm.futOrders, typeFilter).forEach { o ->
                HistOrderRow(o.symbol ?: "", o.side ?: "", o.price, o.qty, o.filledQty, o.status, showCancel = true) {
                    o.orderId?.let { id -> vm.cancelFutures(id) { reload() } }
                }
            }
            tab == 1 -> TradeHistoryLogic.filterOrders(vm.futHistory, typeFilter).forEach { o ->
                HistOrderRow(o.symbol ?: "", o.side ?: "", o.price, o.qty, o.filledQty, o.status, showCancel = false)
            }
            tab == 2 -> {
                vm.futPositions.forEach { PosHistRow(it) }
                if (vm.posCursor != null) {
                    ChipButton(if (vm.loadingMore) "加载中…" else "加载更多", Modifier.fillMaxWidth().height(40.dp).padding(vertical = 8.dp)) {
                        if (!vm.loadingMore) vm.loadMorePositions(symbol, timeFilter)
                    }
                }
            }
            tab == 3 -> vm.futTrades.forEach { t -> HistTradeRow(t.symbol ?: "", t.side?.value ?: "", t.price, t.qty, t.time) }
            tab == 4 -> vm.futBills.forEach { BillRow(it) }
            else -> {
                vm.futAlgos.forEach { a ->
                    AlgoHistRow(a, canCancel = !algoHistory) {
                        a.algoId?.let { id -> vm.cancelAlgo(id, algoPlan) { reload() } }
                    }
                }
                if (vm.algoCursor != null) {
                    ChipButton(if (vm.loadingMore) "加载中…" else "加载更多", Modifier.fillMaxWidth().height(40.dp).padding(vertical = 8.dp)) {
                        if (!vm.loadingMore) vm.loadMoreAlgos(symbol, algoPlan, algoHistory)
                    }
                }
            }
        }
    }
}

@Composable
private fun HistoryFilterBar(
    tab: Int,
    typeFilter: Int,
    onType: (Int) -> Unit,
    timeFilter: Int,
    onTime: (Int) -> Unit,
    billKind: String?,
    onBillKind: (String?) -> Unit,
    algoPlan: Boolean,
    onAlgoPlan: (Boolean) -> Unit,
    algoHistory: Boolean,
    onAlgoHistory: (Boolean) -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        when (tab) {
            0, 1 -> TradeHistoryLogic.orderTypeLabels.forEachIndexed { i, label ->
                FilterChip(label, typeFilter == i) { onType(i) }
            }
            2 -> TradeHistoryLogic.timeFilterLabels.forEachIndexed { i, label ->
                FilterChip(label, timeFilter == i) { onTime(i) }
            }
            4 -> TradeHistoryLogic.billKindOptions.forEach { (kind, label) ->
                FilterChip(label, billKind == kind) { onBillKind(kind) }
            }
            5 -> {
                FilterChip("计划委托", algoPlan) { onAlgoPlan(true) }
                FilterChip("止盈止损", !algoPlan) { onAlgoPlan(false) }
                FilterChip("进行中", !algoHistory) { onAlgoHistory(false) }
                FilterChip("历史", algoHistory) { onAlgoHistory(true) }
            }
            else -> { /* 成交明细无额外筛选 */ }
        }
    }
}

@Composable
private fun FilterChip(label: String, selected: Boolean, onClick: () -> Unit) {
    val p = LB.p
    Text(
        label,
        Modifier.background(if (selected) p.fillStrong else p.fill, androidx.compose.foundation.shape.RoundedCornerShape(4.dp))
            .pressable(scale = 1f, onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 4.dp),
        style = lb(FontSize.small, if (selected) FontWeight.Medium else FontWeight.Normal),
        color = if (selected) p.textPrimary else p.textSecondary,
        maxLines = 1,
    )
}

private fun listForSpot(vm: TradeHistoryViewModel, tab: Int) = when (tab) {
    0 -> vm.spotOpen; 1 -> vm.spotHistory; else -> vm.spotTrades
}
private fun listForFut(vm: TradeHistoryViewModel, tab: Int): List<*> = when (tab) {
    0 -> vm.futOrders; 1 -> vm.futHistory; 2 -> vm.futPositions; 3 -> vm.futTrades; 4 -> vm.futBills; else -> vm.futAlgos
}

@Composable
private fun HistoryShell(title: String, onClose: () -> Unit, content: @Composable () -> Unit) {
    val p = LB.p
    val cfg = LocalConfiguration.current
    val pageH = LayoutAdapt.pageH(cfg.screenWidthDp)
    val titleLines = LayoutAdapt.titleMaxLines(cfg.fontScale)
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        Row(Modifier.fillMaxWidth().height(48.dp).padding(horizontal = pageH), verticalAlignment = Alignment.CenterVertically) {
            LbIcon(R.drawable.ic_arrow_left, 22.dp, p.textPrimary, Modifier.pressable(scale = 1f, onClick = onClose))
            Text(
                title,
                Modifier.weight(1f).padding(horizontal = 8.dp),
                style = lb(FontSize.title, FontWeight.SemiBold),
                color = p.textPrimary,
                textAlign = TextAlign.Center,
                maxLines = titleLines,
                overflow = TextOverflow.Ellipsis,
            )
            Box(Modifier.width(22.dp))
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = pageH).padding(bottom = 24.dp)) {
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
            Text(symbol.replace("USDT", "/USDT"), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(" ${if (buy) "买入" else "卖出"}", style = lb(FontSize.small), color = if (buy) p.up else p.down)
            Box(Modifier.weight(1f))
            if (showCancel && onCancel != null) ChipButton("撤单", Modifier.height(24.dp), onClick = onCancel)
            else Text(status ?: "", style = lb(FontSize.small), color = p.textSecondary, maxLines = 1)
        }
        Row {
            KVCell("价格", apiAmount(price, "市价"), Modifier.weight(1f))
            KVCell("数量", apiAmount(qty), Modifier.weight(1f), TextAlign.Center)
            KVCell("已成交", apiAmount(filled, "0"), Modifier.weight(1f), TextAlign.End)
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
            KVCell("成交价", apiAmount(price), Modifier.weight(1f))
            KVCell("数量", apiAmount(qty), Modifier.weight(1f), TextAlign.End)
        }
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun PosHistRow(h: ApiFuturesPositionHistory) {
    val p = LB.p
    val pnlRaw = h.netPnl ?: h.realizedPnl
    val pnlSign = Dec.parse(pnlRaw).signum()
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(h.symbol ?: "", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (h.side == ApiFuturesPositionHistory.Side.SHORT) "空" else "多"}", style = lb(FontSize.small), color = if (h.side == ApiFuturesPositionHistory.Side.SHORT) p.down else p.up)
            Box(Modifier.weight(1f))
            Text(apiAmount(pnlRaw), style = lbNum(FontSize.label, FontWeight.Medium), color = if (pnlSign < 0) p.down else p.up)
        }
        Row {
            KVCell("开仓价", apiAmount(h.entryPrice), Modifier.weight(1f))
            KVCell("平仓价", apiAmount(h.closePrice), Modifier.weight(1f), TextAlign.Center)
            KVCell("数量", apiAmount(h.closedQty ?: h.maxQty), Modifier.weight(1f), TextAlign.End)
        }
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun BillRow(b: ApiFuturesBills200ResponseBillsInner) {
    val p = LB.p
    val amtRaw = b.amount
    val amtSign = Dec.parse(amtRaw).signum()
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(b.kind ?: "--", style = lb(FontSize.body), color = p.textPrimary)
            Box(Modifier.weight(1f))
            Text(apiAmount(amtRaw), style = lbNum(FontSize.label), color = if (amtSign < 0) p.down else p.up)
        }
        Text(b.time?.take(19)?.replace('T', ' ') ?: "--", style = lb(FontSize.small), color = p.textTertiary)
    }
    HorizontalDivider(color = p.divider)
}

@Composable
private fun AlgoHistRow(a: ApiFuturesAlgoOrder, canCancel: Boolean, onCancel: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(a.symbol ?: "--", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${a.algoType?.value ?: ""}", style = lb(FontSize.small), color = p.textSecondary)
            Box(Modifier.weight(1f))
            if (canCancel) ChipButton("撤销", Modifier.height(24.dp), onClick = onCancel)
            else Text(a.status?.value ?: "", style = lb(FontSize.small), color = p.textSecondary)
        }
        Row {
            KVCell("触发价", apiAmount(a.triggerPrice), Modifier.weight(1f))
            KVCell("委托价", apiAmount(a.price, "市价"), Modifier.weight(1f), TextAlign.Center)
            KVCell("数量", apiAmount(a.qty, "整仓"), Modifier.weight(1f), TextAlign.End)
        }
        a.failReason?.takeIf { it.isNotBlank() }?.let {
            Text(it, style = lb(FontSize.small), color = p.down, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
    HorizontalDivider(color = p.divider)
}
