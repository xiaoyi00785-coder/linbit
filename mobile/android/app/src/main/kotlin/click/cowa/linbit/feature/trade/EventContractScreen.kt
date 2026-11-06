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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiEventOrder
import click.cowa.linbit.core.generated.ApiEventProducts
import click.cowa.linbit.core.generated.ApiEventProductsProductsInner
import click.cowa.linbit.core.generated.ApiPlaceEventOrderRequest
import click.cowa.linbit.core.network.ApiFailure
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.network.lbMessage
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.realtime.WsMessageParser
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.KvRow
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.NumberBox
import click.cowa.linbit.ui.components.ToastCenter
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.LayoutAdapt
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.UUID
import javax.inject.Inject

/** 事件合约下单入参校验与订单合并（对照 OpenAPI /event/orders 与 WS event:orders）。 */
object EventContractLogic {
    fun validateStake(stakeText: String, minStake: String?, maxStake: String?): String? {
        val stake = Dec.parse(stakeText)
        if (stakeText.isBlank() || stake.signum() <= 0) return "请输入投注金额"
        val min = Dec.parse(minStake)
        val max = Dec.parse(maxStake)
        if (min.signum() > 0 && stake < min) return "低于最小投注 ${apiAmount(minStake)}"
        if (max.signum() > 0 && stake > max) return "超过最大投注 ${apiAmount(maxStake)}"
        return null
    }

    fun periodTitle(period: String?): String = when (period) {
        "1m" -> "1 分钟"; "5m" -> "5 分钟"; "15m" -> "15 分钟"; "30m" -> "30 分钟"; "1h" -> "1 小时"
        else -> period ?: "--"
    }

    fun resultLabel(o: ApiEventOrder): String = when (o.result) {
        ApiEventOrder.Result.WON -> "猜中"
        ApiEventOrder.Result.LOST -> "未中"
        ApiEventOrder.Result.TIE -> "平局退本"
        ApiEventOrder.Result.REFUNDED -> "已退款"
        null -> o.status?.value ?: "--"
    }

    fun scaleStake(min: String?, mult: Int): String? {
        val m = Dec.parse(min)
        if (m.signum() <= 0) return null
        return m.multiply(java.math.BigDecimal(mult)).stripTrailingZeros().toPlainString()
    }

    fun isActive(o: ApiEventOrder): Boolean = when (o.status) {
        ApiEventOrder.Status.PENDING, ApiEventOrder.Status.OPEN, ApiEventOrder.Status.SETTLING -> true
        else -> false
    }

    /** 将 WS/轮询订单合并进进行中与历史列表；返回结算提示文案（无则 null）。 */
    fun applyOrder(
        o: ApiEventOrder,
        open: List<ApiEventOrder>,
        history: List<ApiEventOrder>,
        asset: String,
    ): Triple<List<ApiEventOrder>, List<ApiEventOrder>, String?> {
        val id = o.id
        return if (isActive(o)) {
            val nextOpen = if (id == null) listOf(o) + open
            else {
                val i = open.indexOfFirst { it.id == id }
                if (i >= 0) open.toMutableList().also { it[i] = o } else listOf(o) + open
            }
            Triple(nextOpen, history.filter { it.id == null || it.id != id }, null)
        } else {
            val nextOpen = if (id == null) open else open.filter { it.id != id }
            val histWithout = if (id == null) history else history.filter { it.id != id }
            val nextHist = listOf(o) + histWithout
            val toast = if (o.status == ApiEventOrder.Status.SETTLED) {
                val r = when (o.result) {
                    ApiEventOrder.Result.WON -> "猜中，获得 ${apiAmount(o.payout)} $asset"
                    ApiEventOrder.Result.LOST -> "未猜中"
                    ApiEventOrder.Result.TIE -> "平局，本金已退还"
                    ApiEventOrder.Result.REFUNDED -> "已退款"
                    null -> "已结算"
                }
                "${o.symbol ?: ""} $r".trim()
            } else null
            Triple(nextOpen, nextHist, toast)
        }
    }

    fun rules(cfg: ApiEventProducts?, asset: String): List<String> {
        val pct = run {
            val r = Dec.parse(cfg?.payoutRatio)
            if (r.signum() <= 0) "--" else r.multiply(java.math.BigDecimal(100)).stripTrailingZeros().toPlainString()
        }
        return listOf(
            "选择产品与结算周期，预测到期时指数价格相对开仓价的涨跌。",
            "猜中获得投注金额 × ${pct}% 的收益并返还本金；未猜中损失投注金额。",
            "到期价格与开仓价相同视为平局，退还本金。",
            "单笔投注 ${apiAmount(cfg?.minStake)} – ${apiAmount(cfg?.maxStake)} $asset，每日累计上限 ${apiAmount(cfg?.dailyLimit)} $asset。",
            "指数价格不可用或产品暂停时无法下单；已开仓订单按规则退款。",
        )
    }
}

@HiltViewModel
class EventContractViewModel @Inject constructor(
    private val api: LinbitApi,
    private val tokens: TokenStore,
    private val socket: MarketSocket,
    val toast: ToastCenter,
) : ViewModel() {
    val loggedIn get() = tokens.loggedIn
    val sessionVersion get() = tokens.sessionVersion

    var cfg by mutableStateOf<ApiEventProducts?>(null); private set
    var disabled by mutableStateOf(false); private set
    var loading by mutableStateOf(true); private set
    var error by mutableStateOf<String?>(null); private set
    var symbol by mutableStateOf("BTCUSDT")
    var period by mutableStateOf("5m")
    var stakeText by mutableStateOf("10")
    var placing by mutableStateOf(false); private set
    var openOrders by mutableStateOf<List<ApiEventOrder>>(emptyList()); private set
    var historyOrders by mutableStateOf<List<ApiEventOrder>>(emptyList()); private set
    var historyCursor by mutableStateOf<String?>(null); private set
    var loadingMore by mutableStateOf(false); private set
    var dailyUsed by mutableStateOf<String?>(null); private set
    var ordersTab by mutableStateOf(0)

    private var poll: Job? = null
    private var wsJob: Job? = null
    private var sessionJob: Job? = null

    val product: ApiEventProductsProductsInner?
        get() = cfg?.products?.firstOrNull { it.symbol == symbol } ?: cfg?.products?.firstOrNull()

    val asset: String get() = cfg?.stakeAsset ?: "USDT"

    fun start() {
        if (sessionJob == null) {
            sessionJob = viewModelScope.launch {
                tokens.sessionVersion.drop(1).collect {
                    clearAccountData()
                    if (tokens.loggedIn.value) {
                        refreshOrders(silent = true)
                        ensureWs()
                    } else {
                        stopWs()
                    }
                }
            }
        }
        viewModelScope.launch { load() }
        poll?.cancel()
        poll = viewModelScope.launch {
            while (isActive) {
                delay(8_000)
                load(silent = true)
                if (tokens.loggedIn.value) refreshOrders(silent = true)
            }
        }
        if (tokens.loggedIn.value) ensureWs() else stopWs()
    }

    fun stop() {
        poll?.cancel(); poll = null
        stopWs()
    }

    private fun clearAccountData() {
        openOrders = emptyList()
        historyOrders = emptyList()
        historyCursor = null
        dailyUsed = null
    }

    private fun ensureWs() {
        if (wsJob?.isActive == true) return
        stopWs()
        if (!tokens.loggedIn.value) return
        // OpenAPI：鉴权后订阅 event:orders，帧 data 为 EventOrder；账号隔离靠 auth + session 清理
        wsJob = viewModelScope.launch {
            socket.subscribe("event:orders").collect { frame ->
                if (!tokens.loggedIn.value) return@collect
                val o = WsMessageParser.eventOrder(frame) ?: return@collect
                val (nextOpen, nextHist, tip) = EventContractLogic.applyOrder(o, openOrders, historyOrders, asset)
                openOrders = nextOpen
                historyOrders = nextHist
                tip?.let { toast.show(it, if (o.result == ApiEventOrder.Result.WON) ToastCenter.Kind.SUCCESS else ToastCenter.Kind.INFO) }
            }
        }
    }

    private fun stopWs() { wsJob?.cancel(); wsJob = null }

    fun load(silent: Boolean = false) {
        viewModelScope.launch {
            if (!silent) { loading = true; error = null }
            try {
                val r = apiCall { api.eventProducts() }
                cfg = r
                disabled = r.enabled == false
                if (r.products?.none { it.symbol == symbol } == true) {
                    symbol = r.products.firstOrNull()?.symbol ?: symbol
                }
                if (r.periods?.none { it.period == period } == true) {
                    period = r.periods.firstOrNull()?.period ?: period
                }
                if (stakeText.isBlank()) stakeText = r.minStake ?: "10"
                loading = false
            } catch (e: Throwable) {
                if ((e as? ApiFailure)?.code == "FEATURE_DISABLED") {
                    disabled = true; cfg = ApiEventProducts(enabled = false); loading = false; error = null
                } else {
                    error = e.lbMessage(); loading = false
                }
            }
        }
    }

    fun refreshOrders(silent: Boolean = false) {
        if (!tokens.loggedIn.value) {
            clearAccountData(); stopWs(); return
        }
        viewModelScope.launch {
            try {
                val open = apiCall { api.eventOrders(status = "open", symbol = symbol, limit = 50) }
                openOrders = open.orders.orEmpty().filter(EventContractLogic::isActive)
                dailyUsed = open.dailyUsed
                val hist = apiCall { api.eventOrders(status = "settled", symbol = symbol, limit = 50) }
                historyOrders = hist.orders.orEmpty()
                historyCursor = hist.nextBefore
                ensureWs()
            } catch (e: Throwable) {
                if (!silent) toast.show(e.lbMessage(), ToastCenter.Kind.ERROR)
            }
        }
    }

    /** GET /event/orders?status=settled&before=… */
    fun loadMoreHistory() {
        val c = historyCursor ?: return
        if (loadingMore || !tokens.loggedIn.value) return
        viewModelScope.launch {
            loadingMore = true
            try {
                val hist = apiCall { api.eventOrders(status = "settled", symbol = symbol, before = c, limit = 50) }
                val more = hist.orders.orEmpty()
                val seen = historyOrders.mapNotNull { it.id }.toHashSet()
                historyOrders = historyOrders + more.filter { it.id == null || it.id !in seen }
                historyCursor = hist.nextBefore
            } catch (e: Throwable) {
                toast.show(e.lbMessage(), ToastCenter.Kind.ERROR)
            } finally { loadingMore = false }
        }
    }

    fun validation(): String? {
        if (product?.paused == true) return "该标的已暂停"
        if (product?.priceAvailable == false) return "指数价格暂不可用"
        return EventContractLogic.validateStake(stakeText, cfg?.minStake, cfg?.maxStake)
    }

    fun place(up: Boolean, onDone: (Boolean) -> Unit) {
        val err = validation()
        if (err != null) { toast.show(err, ToastCenter.Kind.ERROR); onDone(false); return }
        placing = true
        viewModelScope.launch {
            try {
                apiCall {
                    api.placeEventOrder(
                        ApiPlaceEventOrderRequest(
                            symbol = symbol,
                            period = period,
                            direction = if (up) ApiPlaceEventOrderRequest.Direction.UP else ApiPlaceEventOrderRequest.Direction.DOWN,
                            stake = stakeText.trim(),
                            clientId = "ev-${UUID.randomUUID()}",
                        ),
                    )
                }
                toast.show("下单成功", ToastCenter.Kind.SUCCESS)
                refreshOrders(); onDone(true)
            } catch (e: Throwable) {
                toast.show(e.lbMessage(), ToastCenter.Kind.ERROR); onDone(false)
            } finally { placing = false }
        }
    }

    override fun onCleared() {
        sessionJob?.cancel(); sessionJob = null
        stop()
    }
}

@Composable
fun EventContractScreen(
    bottomInset: Dp,
    onLogin: () -> Unit,
    vm: EventContractViewModel = hiltViewModel(),
) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val pageH = LayoutAdapt.pageH(LocalConfiguration.current.screenWidthDp)
    var pendingUp by remember { mutableStateOf<Boolean?>(null) }
    DisposableEffect(Unit) { vm.start(); onDispose { vm.stop() } }
    LaunchedEffect(loggedIn, vm.symbol) { if (loggedIn) vm.refreshOrders() }

    when {
        vm.loading && vm.cfg == null -> Box(Modifier.fillMaxSize().padding(bottom = bottomInset), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        vm.error != null && vm.cfg == null -> Column(
            Modifier.fillMaxSize().padding(bottom = bottomInset),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            EmptyState(vm.error ?: "加载失败") { ChipButton("重试", Modifier.height(32.dp)) { vm.load() } }
        }
        vm.disabled -> Column(
            Modifier.fillMaxSize().padding(bottom = bottomInset),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            EmptyState("事件合约") {
                Text("猜涨跌，到期按指数价结算，敬请期待", style = lb(FontSize.small), color = p.textSecondary, textAlign = TextAlign.Center)
            }
        }
        else -> Column(Modifier.fillMaxSize().imePadding().padding(bottom = bottomInset).verticalScroll(rememberScrollState())) {
            Row(
                Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = pageH, vertical = 10.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                (vm.cfg?.products.orEmpty()).forEach { pr ->
                    val on = pr.symbol == vm.symbol
                    Row(
                        Modifier.clip(RoundedCornerShape(Radius.sm)).background(if (on) p.fillStrong else p.fill)
                            .pressable(scale = 1f) { vm.symbol = pr.symbol ?: vm.symbol }
                            .padding(horizontal = 10.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        CoinIcon(pr.baseAsset ?: (pr.symbol?.removeSuffix("USDT") ?: "?"), 16.dp)
                        Text(
                            pr.baseAsset ?: pr.symbol ?: "--",
                            style = lb(FontSize.caption, if (on) FontWeight.SemiBold else FontWeight.Normal),
                            color = if (on) p.textPrimary else p.textSecondary,
                        )
                        if (pr.paused == true) Text("暂停", style = lb(FontSize.small), color = p.textTertiary)
                    }
                }
            }
            var showRules by remember { mutableStateOf(false) }
            Row(
                Modifier.fillMaxWidth().padding(horizontal = pageH).padding(bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("事件合约", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                Spacer(Modifier.weight(1f))
                Text(
                    "规则",
                    Modifier.pressable(scale = 1f) { showRules = true },
                    style = lb(FontSize.small),
                    color = p.brand,
                )
            }
            Column(
                Modifier.padding(horizontal = pageH).clip(RoundedCornerShape(Radius.lg)).background(p.fill).padding(14.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Row {
                    Text("${vm.symbol} 指数价格", style = lb(FontSize.small), color = p.textSecondary)
                    Spacer(Modifier.weight(1f))
                    Text("赔率 ${apiAmount(vm.cfg?.payoutRatio)}", style = lb(FontSize.small, FontWeight.Medium), color = p.brand)
                }
                Text(
                    if (vm.product?.priceAvailable == false) "--" else apiAmount(vm.product?.indexPrice),
                    style = lbNum(28.sp, FontWeight.Bold),
                    color = p.textPrimary,
                )
                if (vm.product?.priceAvailable == false) {
                    Text(
                        "指数价格暂不可用，暂停下单",
                        Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.sm)).background(p.bg).padding(8.dp),
                        style = lb(FontSize.small),
                        color = p.down,
                    )
                }
            }
            Text("结算周期", Modifier.padding(horizontal = pageH).padding(top = 16.dp), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                (vm.cfg?.periods.orEmpty()).forEach { pd ->
                    val on = pd.period == vm.period
                    Box(
                        Modifier.weight(1f).height(34.dp).clip(RoundedCornerShape(Radius.sm))
                            .border(if (on) 1.2.dp else 1.dp, if (on) p.textPrimary else p.border, RoundedCornerShape(Radius.sm))
                            .pressable(scale = 1f) { vm.period = pd.period ?: vm.period },
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            EventContractLogic.periodTitle(pd.period),
                            style = lb(FontSize.caption, if (on) FontWeight.SemiBold else FontWeight.Normal),
                            color = if (on) p.textPrimary else p.textSecondary,
                        )
                    }
                }
            }
            Column(Modifier.padding(horizontal = pageH).padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                NumberBox("投注金额", vm.asset, vm.stakeText, { vm.stakeText = it })
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf(
                        vm.cfg?.minStake,
                        EventContractLogic.scaleStake(vm.cfg?.minStake, 5),
                        EventContractLogic.scaleStake(vm.cfg?.minStake, 10),
                        vm.cfg?.maxStake,
                    ).forEach { v ->
                        Box(
                            Modifier.weight(1f).height(26.dp).clip(RoundedCornerShape(Radius.xs)).background(p.fill)
                                .pressable(scale = 1f, enabled = v != null) { if (v != null) vm.stakeText = v },
                            contentAlignment = Alignment.Center,
                        ) { Text(apiAmount(v), style = lbNum(FontSize.small), color = p.textPrimary) }
                    }
                }
                KvRow("范围", "${apiAmount(vm.cfg?.minStake)} – ${apiAmount(vm.cfg?.maxStake)} ${vm.asset}")
                KvRow("今日已用 / 上限", "${apiAmount(vm.dailyUsed)} / ${apiAmount(vm.cfg?.dailyLimit)}")
            }
            Row(Modifier.padding(horizontal = pageH).padding(top = 18.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                LbButton(
                    if (loggedIn) "看涨" else "登录 / 注册",
                    Modifier.weight(1f),
                    kind = if (loggedIn) ButtonKind.BUY else ButtonKind.PRIMARY,
                    height = 48.dp,
                    loading = vm.placing,
                ) {
                    if (!loggedIn) onLogin()
                    else vm.validation()?.let { vm.toast.show(it, ToastCenter.Kind.ERROR) } ?: run { pendingUp = true }
                }
                LbButton(
                    if (loggedIn) "看跌" else "登录 / 注册",
                    Modifier.weight(1f),
                    kind = if (loggedIn) ButtonKind.SELL else ButtonKind.PRIMARY,
                    height = 48.dp,
                    loading = vm.placing,
                ) {
                    if (!loggedIn) onLogin()
                    else vm.validation()?.let { vm.toast.show(it, ToastCenter.Kind.ERROR) } ?: run { pendingUp = false }
                }
            }
            Box(Modifier.fillMaxWidth().padding(top = 20.dp).height(8.dp).background(p.fill))
            Row(Modifier.padding(horizontal = pageH, vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                listOf("进行中", "历史").forEachIndexed { i, t ->
                    Text(
                        t,
                        Modifier.pressable(scale = 1f) { vm.ordersTab = i },
                        style = lb(FontSize.label, if (vm.ordersTab == i) FontWeight.SemiBold else FontWeight.Normal),
                        color = if (vm.ordersTab == i) p.textPrimary else p.textSecondary,
                    )
                }
            }
            HorizontalDivider(color = p.divider)
            val list = if (vm.ordersTab == 0) vm.openOrders else vm.historyOrders
            when {
                !loggedIn -> EmptyState("登录后查看事件合约订单") {
                    ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin)
                }
                list.isEmpty() -> EmptyState("暂无订单")
                else -> {
                    list.forEach { EventOrderRow(it) }
                    if (vm.ordersTab == 1 && vm.historyCursor != null) {
                        ChipButton(
                            if (vm.loadingMore) "加载中…" else "加载更多",
                            Modifier.fillMaxWidth().height(40.dp).padding(horizontal = pageH, vertical = 8.dp),
                        ) { if (!vm.loadingMore) vm.loadMoreHistory() }
                    }
                }
            }
            Spacer(Modifier.height(24.dp))
            if (showRules) {
                LbSheet("事件合约规则", { showRules = false }, showClose = true) {
                    Column(
                        Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        EventContractLogic.rules(vm.cfg, vm.asset).forEach { r ->
                            Row {
                                Text("• ", style = lb(FontSize.caption), color = p.textSecondary)
                                Text(r, style = lb(FontSize.caption), color = p.textSecondary)
                            }
                        }
                        LbButton("知道了") { showRules = false }
                    }
                }
            }
        }
    }
    pendingUp?.let { up ->
        LbSheet(if (up) "确认看涨" else "确认看跌", { pendingUp = null }, showClose = true) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                KvRow("标的", vm.symbol)
                KvRow("周期", EventContractLogic.periodTitle(vm.period))
                KvRow("投注", "${vm.stakeText} ${vm.asset}")
                KvRow("开仓参考", apiAmount(vm.product?.indexPrice))
                Text("开仓价以下单时指数价为准；到期结算，猜对获本金+收益，猜错没收本金。", style = lb(FontSize.small), color = p.textTertiary)
                LbButton("确认下单", kind = if (up) ButtonKind.BUY else ButtonKind.SELL, loading = vm.placing) {
                    vm.place(up) { ok -> if (ok) pendingUp = null }
                }
            }
        }
    }
}

@Composable
private fun EventOrderRow(o: ApiEventOrder) {
    val p = LB.p
    val up = o.direction == ApiEventOrder.Direction.UP
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(o.symbol ?: "--", style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            Text(" ${if (up) "看涨" else "看跌"}", style = lb(FontSize.small), color = if (up) p.up else p.down)
            Spacer(Modifier.weight(1f))
            Text(EventContractLogic.resultLabel(o), style = lb(FontSize.small), color = p.textSecondary)
        }
        Row {
            KVCell("投注", apiAmount(o.stake), Modifier.weight(1f))
            KVCell("开仓价", apiAmount(o.openPrice), Modifier.weight(1f), TextAlign.Center)
            KVCell("结算价", apiAmount(o.settlePrice), Modifier.weight(1f), TextAlign.End)
        }
        o.pnl?.let {
            Text("盈亏 ${apiAmount(it)}", style = lbNum(FontSize.small), color = if (Dec.parse(it).signum() < 0) p.down else p.up)
        }
    }
    HorizontalDivider(color = p.divider)
}
