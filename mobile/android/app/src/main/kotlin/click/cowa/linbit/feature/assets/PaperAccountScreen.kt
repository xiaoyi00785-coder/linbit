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
                account = null; balances = null; failure = null; resetFailure = null; resetting = false; lastOrder = null; clearOrder()
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
                if (version == epoch) { account = null; balances = null; loading = false; failure = client.featureFailure; clearOrder() }
                return@launch
            }
            runCatching {
                val act = client.account()
                require(tokens.loggedIn.value && version == epoch) { "Session changed" }
                val page = client.balances()
                require(page.generation == act.generation) { "虚拟账户余额代次不一致，请重试" }
                act to page
            }.onSuccess { (act, page) ->
                if (version == epoch && tokens.loggedIn.value) { account = act; balances = page }
            }.onFailure {
                if (version == epoch && tokens.loggedIn.value) failure = it.lbMessage()
            }
            if (version == epoch) loading = false
        }
    }
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
                Text("只使用虚拟账户，独立外部指数撮合；不影响真实资金。订单/成交分页后续开放。", Modifier.padding(top = 12.dp), style = lb(FontSize.small), color = p.textSecondary)
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

}
