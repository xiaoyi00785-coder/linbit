package click.cowa.linbit.feature.assets

import androidx.compose.foundation.background

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.data.paper.*
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import java.util.UUID
import javax.inject.Inject

@HiltViewModel
class PaperAccountViewModel @Inject constructor(
    private val client: PaperTradingClient,
    val tokens: TokenStore,
    private val toast: ToastCenter,
) : androidx.lifecycle.ViewModel() {
    enum class HistoryKind { ORDERS, TRADES }
    data class HistoryPage<T>(
        val items: List<T> = emptyList(),
        val page: Int = 0,
        val total: Int = 0,
        val hasNext: Boolean = true,
        val loading: Boolean = false,
        val failure: String? = null,
        val generation: Long = 0L,
    )
    var ordersHistory by mutableStateOf(HistoryPage<PaperOrder>(hasNext = false)); private set
    var tradesHistory by mutableStateOf(HistoryPage<PaperTrade>(hasNext = false)); private set
    var historyKind by mutableStateOf(HistoryKind.ORDERS); private set
    private var historyGeneration = 0L
    private var historyAccount = ""
    private var historyAccountMode = ""
    private var ordersRequest = 0L
    private var tradesRequest = 0L

    val available: Boolean get() = client.enabled
    var loading by mutableStateOf(false); private set
    var account by mutableStateOf<PaperAccount?>(null); private set
    var balances by mutableStateOf<PaperBalances?>(null); private set
    var failure by mutableStateOf<String?>(null)
    var resetting by mutableStateOf(false); private set
    var resetFailure by mutableStateOf<String?>(null)
    private var epoch = 0L
    private var orderId = ""
    private var requestedOrder: PaperOrderRequest? = null
    private var loadedGeneration = 0L
    var orderBuy by mutableStateOf(true); private set
    var orderAmount by mutableStateOf(""); private set
    var orderSubmitting by mutableStateOf(false); private set
    var orderFailure by mutableStateOf<String?>(null); private set
    var lastOrder by mutableStateOf<PaperOrder?>(null); private set
    var orderRetryAfter by mutableStateOf<Int?>(null); private set
    val sessionVersion: StateFlow<Long> get() = tokens.sessionVersion
    val featureState: StateFlow<PaperFeatureSnapshot> get() = client.featureState

    init { observeSession() }
    private fun observeSession() {
        viewModelScope.launch {
            tokens.sessionVersion.collect {
                epoch += 1
                account = null; balances = null; failure = null; resetFailure = null; resetting = false; lastOrder = null; clearOrder(); historyGeneration = 0L; historyAccount = ""; historyAccountMode = ""; ordersRequest += 1; tradesRequest += 1; ordersHistory = HistoryPage(hasNext = false); tradesHistory = HistoryPage(hasNext = false)
                load()
            }
        }
    }
    fun load() {
        if (!tokens.loggedIn.value) { loading = false; failure = null; clearOrder(); return }
        val version = epoch
        loading = true; failure = null
        viewModelScope.launch {
            client.refreshFeature()
            if (!client.enabled || version != epoch || !tokens.loggedIn.value) {
                if (version == epoch) { account = null; balances = null; loading = false; failure = client.featureFailure; clearOrder(); historyGeneration = 0L; historyAccount = ""; historyAccountMode = ""; ordersRequest += 1; tradesRequest += 1; ordersHistory = HistoryPage(hasNext = false); tradesHistory = HistoryPage(hasNext = false) }
                return@launch
            }
            runCatching {
                val act = client.account()
                require(tokens.loggedIn.value && version == epoch) { "Session changed" }
                val page = client.balances()
                require(page.generation == act.generation) { "虚拟账户余额代次不一致，请重试" }
                act to page
            }.onSuccess { (act, page) ->
                if (version == epoch && tokens.loggedIn.value) { account = act; balances = page; if (historyGeneration != act.generation || (historyAccount != act.accountId || historyAccountMode != act.mode)) { ordersHistory = HistoryPage(generation = act.generation); tradesHistory = HistoryPage(generation = act.generation); historyGeneration = act.generation; historyAccount = act.accountId; historyAccountMode = act.mode } }
            }.onFailure {
                if (version == epoch && tokens.loggedIn.value) failure = it.lbMessage()
            }
            if (version == epoch) loading = false
        }
    }
    fun selectHistory(kind: HistoryKind) {
        if (historyKind != kind) historyKind = kind
    }
    fun openHistory(kind: HistoryKind) { selectHistory(kind); refreshHistory(kind) }
    fun refreshHistory(kind: HistoryKind) {
        val state = if (kind == HistoryKind.ORDERS) ordersHistory else tradesHistory
        val retryPage = state.page + 1
        if (state.failure != null && state.hasNext) loadHistory(kind, retryPage) else loadHistory(kind, 1)
    }
    fun nextHistoryPage(kind: HistoryKind) {
        val state = if (kind == HistoryKind.ORDERS) ordersHistory else tradesHistory
        if (state.page > 0 && state.hasNext) loadHistory(kind, state.page + 1)
    }
    /** Independent immutable pages. Responses outside the current account generation/session cannot append. */
    fun loadHistory(kind: HistoryKind, page: Int) {
        val generation = account?.generation ?: return
        val accountId = account?.accountId ?: return
        val state = if (kind == HistoryKind.ORDERS) ordersHistory else tradesHistory
        if (!client.enabled || !tokens.loggedIn.value || state.loading || !state.hasNext) return
        val version = epoch
        // Acquire the request/loading gate before launch: a first suspension cannot allow a duplicate page.
        val requestVersion = if (kind == HistoryKind.ORDERS) ++ordersRequest else ++tradesRequest
        if (historyGeneration != generation || historyAccount != accountId || historyAccountMode != "paper") {
            ordersHistory = HistoryPage(generation = generation)
            tradesHistory = HistoryPage(generation = generation)
            historyGeneration = generation
            historyAccount = accountId
            historyAccountMode = "paper"
        }
        if (currentPage(kind) != page - 1) return
        if (kind == HistoryKind.ORDERS) ordersHistory = ordersHistory.copy(loading = true, failure = null) else tradesHistory = tradesHistory.copy(loading = true, failure = null)
        viewModelScope.launch {
            runCatching {
                if (client.enabled && tokens.loggedIn.value) {
                    require(account?.generation == generation) { "虚拟账户代次已变化，请重试" }
                    if (kind == HistoryKind.ORDERS) {
                        val response = client.orders(symbol = "BTCUSDT", generation = generation, page = page, pageSize = HISTORY_PAGE_SIZE)
                        require(response.page == page && response.generation == generation && response.items.all { it.generation == generation && it.accountId == accountId }) { "订单分页代次不一致" }
                        val previous = ordersHistory.items.filter { it.generation == generation }
                        HistoryPage(if (page == 1) response.items else previous + response.items, response.page, response.total, (response.items.size.toLong() + (if (page == 1) 0 else previous.size)) < response.total, false, null, generation)
                    } else {
                        val response = client.trades(symbol = "BTCUSDT", generation = generation, page = page, pageSize = HISTORY_PAGE_SIZE)
                        require(response.page == page && response.generation == generation) { "成交分页代次不一致" }
                        val previous = tradesHistory.items
                        HistoryPage(if (page == 1) response.items else previous + response.items, response.page, response.total, (response.items.size.toLong() + (if (page == 1) 0 else previous.size)) < response.total, false, null, generation)
                    }
                } else null
            }.onSuccess { value ->
                if (value != null && version == epoch && account?.mode == "paper" && tokens.loggedIn.value && account?.accountId == accountId && account?.generation == generation && currentGeneration(kind) == generation && currentPage(kind) == page - 1 && currentRequest(kind) == requestVersion) {
                    // One response must not race a same-generation refresh.
                    if (kind == HistoryKind.ORDERS) ordersHistory = value as HistoryPage<PaperOrder> else tradesHistory = value as HistoryPage<PaperTrade>
                }
            }.onFailure {
                if (version == epoch && tokens.loggedIn.value && account?.mode == "paper" && account?.accountId == accountId && account?.generation == generation && currentGeneration(kind) == generation && currentRequest(kind) == requestVersion) {
                    setHistory(kind, (if (kind == HistoryKind.ORDERS) ordersHistory else tradesHistory).copy(loading = false, failure = it.lbMessage()))
                }
            }
        }
    }
    private fun currentRequest(kind: HistoryKind) = if (kind == HistoryKind.ORDERS) ordersRequest else tradesRequest
    private fun currentGeneration(kind: HistoryKind) = if (kind == HistoryKind.ORDERS) ordersHistory.generation else tradesHistory.generation
    private fun currentPage(kind: HistoryKind) = if (kind == HistoryKind.ORDERS) ordersHistory.page else tradesHistory.page
@Suppress("UNCHECKED_CAST")
    private fun setHistory(kind: HistoryKind, value: HistoryPage<*>) {
        if (kind == HistoryKind.ORDERS) ordersHistory = value as HistoryPage<PaperOrder> else tradesHistory = value as HistoryPage<PaperTrade>
    }
    companion object { const val HISTORY_PAGE_SIZE = 2 }

    fun orderDraft(buy: Boolean, text: String): String? {
        if (orderSubmitting) return "请等待当前虚拟委托响应"
        val amount = text.trim().toBigDecimalOrNull() ?: return "请输入有效的${if (buy) "买入金额" else "卖出数量"}"
        val generation = account?.generation ?: return "虚拟账户尚未就绪"
        if (amount.signum() <= 0 || generation != balances?.generation || loading || !client.enabled || !tokens.loggedIn.value) return "请输入大于零的金额或数量，并等待虚拟账户就绪"
        orderBuy = buy; orderAmount = text.trim(); orderFailure = null
        orderId = "android-paper-${UUID.randomUUID()}"; loadedGeneration = generation
        return null
    }
    fun confirmOrder(): String? {
        if (orderSubmitting) return null
        val generation = account?.generation
        if (!client.enabled || !tokens.loggedIn.value) { clearOrder(); return "模拟功能已关闭或登录已失效" }
        if (generation == null || generation != balances?.generation || generation != loadedGeneration) return "虚拟账户已变化，请重新确认"
        requestedOrder = PaperOrderRequest("BTCUSDT", if (orderBuy) "buy" else "sell", quantity = if (orderBuy) "" else orderAmount, quoteOrderQty = if (orderBuy) orderAmount else "", clientOrderId = orderId)
        retryOrder()
        return null
    }
    /** Retry keeps the exact request and ID, including timeout / 429 / idempotency responses. */
    fun retryOrder() {
        val request = requestedOrder ?: return
        val generation = account?.generation ?: return
        if (!client.enabled || !tokens.loggedIn.value || orderSubmitting || generation != loadedGeneration || generation != balances?.generation) return
        val clientId = orderId
        val version = epoch
        orderSubmitting = true; orderFailure = null; orderRetryAfter = null
        viewModelScope.launch {
            runCatching { client.placeOrder(request) }.onSuccess { order ->
                if (version == epoch && orderId == clientId && order.generation == generation && request.clientOrderId == order.clientOrderId && tokens.loggedIn.value) {
                    if (order.status == "filled") {
                        lastOrder = order
                        toast.show("虚拟${if (order.side == "buy") "买入" else "卖出"}${order.symbol}已按服务端事实成交", ToastCenter.Kind.SUCCESS)
                        clearOrder(); load()
                    } else orderFailure = "服务端未确认成交，状态：${order.status}"
                } else if (version == epoch && orderId == clientId && tokens.loggedIn.value) {
                    orderSubmitting = false
                    orderFailure = "服务端订单与当前复核的会话或代次不匹配，未提交为成交；请取消后重新发起"
                }
            }.onFailure {
                if (version == epoch && orderId == clientId && tokens.loggedIn.value) {
                    if (!client.enabled) clearOrder()
                    val failure = it as? ApiFailure
                    orderRetryAfter = failure?.retryAfterSeconds
                    orderFailure = if (failure?.httpStatus == 429) it.lbMessage() + "；稍后用同一虚拟委托号重试" else it.lbMessage()
                }
            }
            if (version == epoch && orderId == clientId) orderSubmitting = false
        }
    }
    fun cancelOrderReview() { clearOrder() }
    private fun clearOrder() {
        orderRetryAfter = null
        requestedOrder = null; orderId = ""; orderAmount = ""; orderSubmitting = false; orderFailure = null; loadedGeneration = 0L
    }
    fun reset(idempotencyKey: String, onDone: (Boolean) -> Unit) {
        if (!client.enabled || resetting || loading || account == null || !tokens.loggedIn.value) return
        val version = epoch
        resetting = true; resetFailure = null
        viewModelScope.launch {
            runCatching { client.reset(idempotencyKey) }
                .onSuccess {
                    if (version == epoch && tokens.loggedIn.value) {
                        // Clear the old generation synchronously. URI page response also requires its read-back generation.
                        account = null; balances = null
                        toast.show("虚拟账户已重置，不影响真实资产", ToastCenter.Kind.SUCCESS)
                        onDone(true); load()
                    }
                }
                .onFailure {
                    if (version == epoch && tokens.loggedIn.value) {
                        resetFailure = if (it is ApiFailure.Server && it.httpStatus == 429) "操作较频繁，请${it.retryAfterSeconds?.let { seconds -> "在${seconds}秒后重试" } ?: "稍后重试"}" else it.lbMessage()
                        onDone(false)
                    }
                }
            if (version == epoch) resetting = false
        }
    }
}

@Composable
fun PaperAccountScreen(onBack: () -> Unit, onLogin: () -> Unit, vm: PaperAccountViewModel = hiltViewModel(), onDialogWindow: (android.view.Window?) -> Unit = {}) {
    val loggedIn by vm.tokens.loggedIn.collectAsStateWithLifecycle()
    val feature by vm.featureState.collectAsStateWithLifecycle()
    PaperAccountContent(loggedIn, feature.enabled, vm, onBack, onLogin, onDialogWindow)
}

@Composable
private fun PaperAccountContent(loggedIn: Boolean, featureEnabled: Boolean, vm: PaperAccountViewModel, onBack: () -> Unit, onLogin: () -> Unit, onDialogWindow: (android.view.Window?) -> Unit) {
    val p = LB.p
    var showReset by remember { mutableStateOf(false) }
    var resetKey by remember { mutableStateOf("") }
    var review by remember { mutableStateOf(false) }
    var buy by remember { mutableStateOf(true) }
    var orderInput by remember { mutableStateOf("") }
    var inputFailure by remember { mutableStateOf<String?>(null) }
    var showHistory by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxSize().background(p.bg).verticalScroll(rememberScrollState()).padding(Spacing.pageH)) {
        AssetsNavBar("模拟交易", onBack = onBack)
        Text("模拟账户", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Text("独立模拟 / 虚拟账户，不影响真实资产", Modifier.padding(top = 4.dp, bottom = 16.dp), style = lb(FontSize.small), color = p.textSecondary)
        when {
            !loggedIn -> EmptyState("登录后可查看模拟账户") { LbButton("登录 / 注册", Modifier.fillMaxWidth(), onClick = onLogin) }
            !featureEnabled -> {
                Text("模拟功能尚未开放", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                Text("当前版本仅保留独立模拟入口，未发起虚拟或真实交易请求。", Modifier.padding(top = 12.dp), style = lb(FontSize.body), color = p.textSecondary)
            }
            vm.loading -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) { CircularProgressIndicator(color = p.brand) }
            vm.failure != null && vm.account == null -> EmptyState(vm.failure ?: "模拟账户暂不可用") { LbButton("重试", Modifier.fillMaxWidth(), onClick = vm::load) }
            vm.account != null -> {
                Text("虚拟账户 · ${vm.account?.quoteAsset ?: "USDT"}", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
                Text("账户标识 ${vm.account?.accountId.orEmpty()} · 代次 ${vm.account?.generation ?: 0}", style = lb(FontSize.small), color = p.textSecondary)
                vm.account?.let { KvRow("初始虚拟资金", "${it.initialBalance} ${it.quoteAsset}") }
                Text("虚拟余额", Modifier.padding(vertical = 10.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                if (vm.balances?.items?.isEmpty() == true) Text("暂无虚拟余额", style = lb(FontSize.body), color = p.textSecondary)
                vm.balances?.items?.forEach { balance ->
                    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                        KvRow("资产", balance.asset)
                        KvRow("可用", balance.free)
                        KvRow("锁定", balance.locked)
                        KvRow("合计", balance.total)
                    }
                }
                Text("只使用虚拟账户，独立外部指数（INDEX）为撮合参考；不影响真实资金。订单与成交记录分别读取服务端分页。", Modifier.padding(top = 12.dp), style = lb(FontSize.small), color = p.textSecondary)
                Row(Modifier.fillMaxWidth().padding(top = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    LbButton("市价买入", Modifier.weight(1f), kind = ButtonKind.BUY, enabled = !vm.orderSubmitting) { buy = true; orderInput = "" }
                    LbButton("市价卖出", Modifier.weight(1f), kind = ButtonKind.SELL, enabled = !vm.orderSubmitting) { buy = false; orderInput = "" }
                }
                NumberBox(if (buy) "买入金额" else "卖出数量", if (buy) "USDT" else "BTC", orderInput, { orderInput = it; inputFailure = null }, readOnly = vm.orderSubmitting, testTag = "paper-order-amount")
                vm.lastOrder?.let { order ->
                    Text("最近笔虚拟${if (order.side == "buy") "买入" else "卖出"}由服务端确认成交：${order.quantity} BTC", style = lb(FontSize.small), color = p.textSecondary)
                    KvRow("服务端成交价", order.price ?: "未返回")
                    KvRow("服务端手续费", "${order.fee} ${order.feeAsset}")
                }
                LbButton(if (buy) "复核市价买入" else "复核市价卖出", kind = if (buy) ButtonKind.BUY else ButtonKind.SELL, enabled = !vm.orderSubmitting, testTag = "paper-order-review") {
                    inputFailure = vm.orderDraft(buy, orderInput)
                    review = inputFailure == null
                }
                inputFailure?.let { Text(it, style = lb(FontSize.body), color = p.down) }
                LbButton("订单 / 成交记录", Modifier.padding(top = 12.dp), testTag = "paper-history") {
                    vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS)
                    showHistory = true
                }
                LbButton("重置虚拟账户", Modifier.padding(top = 16.dp), enabled = !vm.resetting && !vm.loading) {
                    resetKey = "android-paper-${UUID.randomUUID()}"
                    vm.resetFailure = null
                    showReset = true
                }
            }
        }
    }
    if (showReset) {
        LbSheet("重置虚拟账户最终复核", { if (!vm.resetting) showReset = false }, showClose = !vm.resetting, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("只重置模拟余额和虚拟交易历史，不影响真实资产。此操作不可用于真实资金。", style = lb(FontSize.body), color = p.textSecondary)
                Text("确认语句：RESET_PAPER_ACCOUNT", style = lb(FontSize.small), color = p.textSecondary)
                vm.resetFailure?.let { Text("操作未完成：$it", style = lb(FontSize.small), color = p.down) }
                LbButton(if (vm.resetting) "提交中…" else "确认重置模拟账户", enabled = !vm.resetting) { vm.reset(resetKey) { ok -> if (ok) showReset = false } }
                LbButton("取消", kind = ButtonKind.OUTLINE, enabled = !vm.resetting) { showReset = false }
            }
        }
    }
    if (review && vm.orderAmount.isNotEmpty()) {
        LbSheet("市价虚拟${if (vm.orderBuy) "买入" else "卖出"}最终复核", {
            if (!vm.orderSubmitting) { vm.cancelOrderReview(); review = false }
        }, showClose = !vm.orderSubmitting, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("这是模拟交易，不使用、划扣或变动真实资金。", style = lb(FontSize.body, FontWeight.SemiBold), color = p.down)
                KvRow("虚拟账户", "${vm.account?.accountId.orEmpty()} · 第${vm.account?.generation ?: 0}代")
                KvRow("市场 / 方式", "BTC/USDT · 市价${if (vm.orderBuy) "买入" else "卖出"}")
                KvRow(if (vm.orderBuy) "报价金额" else "卖出数量", "${vm.orderAmount} ${if (vm.orderBuy) "USDT" else "BTC"}")
                KvRow("价格来源", "新鲜外部指数（INDEX）")
                Text("价格、成交数量、手续费与成交状态由服务端返回；不承诺成交或使用本地预估价格。", style = lb(FontSize.small), color = p.textSecondary)
                vm.orderFailure?.let { Text("操作未完成：$it", style = lb(FontSize.body), color = p.down) }
                vm.orderRetryAfter?.let { Text("请在${it}秒后重试", style = lb(FontSize.body), color = p.down) }
                LbButton(if (vm.orderSubmitting) "提交中…" else "确认虚拟${if (vm.orderBuy) "买入" else "卖出"}", kind = if (vm.orderBuy) ButtonKind.BUY else ButtonKind.SELL, enabled = !vm.orderSubmitting, testTag = "paper-order-confirm") {
                    val failure = vm.confirmOrder()
                    if (failure != null) { inputFailure = failure; review = false }
                }
                LbButton("取消", kind = ButtonKind.OUTLINE, enabled = !vm.orderSubmitting) { vm.cancelOrderReview(); review = false }
            }
        }
    }

    if (showHistory) {
        LbSheet("虚拟订单 / 成交记录", { showHistory = false }, onWindowReady = onDialogWindow) {
            Column(Modifier.padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("仅虚拟账户 · 第${vm.account?.generation ?: 0}代；INDEX 是外部指数参考，不是真钱订单成交价。", style = lb(FontSize.small), color = p.textSecondary)
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    LbButton("订单", Modifier.weight(1f), kind = if (vm.historyKind == PaperAccountViewModel.HistoryKind.ORDERS) ButtonKind.PRIMARY else ButtonKind.OUTLINE, testTag = "paper-history-orders") { vm.openHistory(PaperAccountViewModel.HistoryKind.ORDERS) }
                    LbButton("成交", Modifier.weight(1f), kind = if (vm.historyKind == PaperAccountViewModel.HistoryKind.TRADES) ButtonKind.PRIMARY else ButtonKind.OUTLINE, testTag = "paper-history-trades") { vm.openHistory(PaperAccountViewModel.HistoryKind.TRADES) }
                }
                if (vm.historyKind == PaperAccountViewModel.HistoryKind.ORDERS) {
                    OrderList(vm, vm.ordersHistory)
                } else {
                    val state = vm.tradesHistory
                    HistoryControls(state.failure, state.loading, state.page, state.hasNext, retry = { vm.refreshHistory(PaperAccountViewModel.HistoryKind.TRADES) }, next = { vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.TRADES) }) {
                        if (state.items.isEmpty() && !state.loading && state.failure == null) Text("暂无虚拟成交记录", style = lb(FontSize.body), color = p.textSecondary)
                        state.items.forEach { trade ->
                            Text("虚拟${if (trade.side == "buy") "买入" else "卖出"} · ${trade.symbol}", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                            KvRow("成交量", trade.quantity)
                            KvRow("INDEX 参考成交价", trade.price)
                            KvRow("手续费", "${trade.fee} ${trade.feeAsset}")
                            KvRow("订单 / 成交", "${trade.orderId} / ${trade.tradeId}")
                        }
                    }
                }
            }
        }
    }

}




@Composable
private fun OrderList(vm: PaperAccountViewModel, state: PaperAccountViewModel.HistoryPage<PaperOrder>) {
    val p = LB.p
    HistoryControls(state.failure, state.loading, state.page, state.hasNext, retry = { vm.refreshHistory(PaperAccountViewModel.HistoryKind.ORDERS) }, next = { vm.nextHistoryPage(PaperAccountViewModel.HistoryKind.ORDERS) }) {
        if (state.items.isEmpty() && !state.loading && state.failure == null) Text("暂无虚拟订单记录", style = lb(FontSize.body), color = p.textSecondary)
        state.items.forEach { order ->
            Column(Modifier.padding(vertical = 5.dp)) {
                Text("虚拟${if (order.side == "buy") "买入" else "卖出"} · ${order.symbol}", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                KvRow("委托数量 / Quote", "${order.quantity} / ${order.quoteOrderQty ?: "—"}")
                KvRow("状态 / 已成交", "${order.status} / ${order.filledQuantity}")
                KvRow(if (order.status == "filled") "INDEX 参考成交价" else "INDEX 参考价", order.price ?: "服务端未返回")
                KvRow("手续费", "${order.fee} ${order.feeAsset}")
                KvRow("订单", order.orderId)
            }
        }
    }
}

@Composable
private fun HistoryControls(failure: String?, loading: Boolean, page: Int, hasNext: Boolean, retry: () -> Unit, next: () -> Unit, content: @Composable () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().heightIn(max = 520.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        content()
        if (loading) CircularProgressIndicator(color = p.brand)
        failure?.let {
            Text("操作未完成：$it", style = lb(FontSize.body), color = p.down)
            LbButton("重试", testTag = "paper-history-retry", onClick = retry)
        }
        Text(if (page == 0) "服务端分页读取中" else "已读取第${page}页${if (hasNext) "，可继续" else " · 已到末页"}", style = lb(FontSize.small), color = p.textSecondary)
        if (hasNext && page > 0 && !loading) LbButton("下一页", enabled = true, testTag = "paper-history-next", onClick = next)
    }
}
