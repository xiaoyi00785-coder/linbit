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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.widthIn
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.positionInWindow
import androidx.compose.ui.layout.positionInRoot
import androidx.compose.ui.graphics.graphicsLayer
import click.cowa.linbit.R
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.ThemedIcon
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
    var stakeText by mutableStateOf("")
    var placing by mutableStateOf(false); private set
    var openOrders by mutableStateOf<List<ApiEventOrder>>(emptyList()); private set
    var historyOrders by mutableStateOf<List<ApiEventOrder>>(emptyList()); private set
    var historyCursor by mutableStateOf<String?>(null); private set
    var loadingMore by mutableStateOf(false); private set
    var dailyUsed by mutableStateOf<String?>(null); private set
    var ordersTab by mutableStateOf(0)
    var spotFree by mutableStateOf<String?>(null); private set

    fun loadSpot() {
        if (!tokens.loggedIn.value) { spotFree = null; return }
        viewModelScope.launch {
            spotFree = runCatching { apiCall { api.balances(account = "spot") } }.getOrNull()?.items?.firstOrNull { it.asset == asset }?.free ?: spotFree
        }
    }

    suspend fun klines(interval: String): List<Candle> = apiCall { api.spotKlines(symbol, interval) }.map(Candle::fromSpot)

    /** 到期可得（本金 + 收益）= 投注 × (1 + 赔率)；未输入金额为 0。 */
    fun payoutAmount(): String {
        val st = Dec.parse(stakeText)
        if (st.signum() <= 0) return "0"
        return st.multiply(java.math.BigDecimal.ONE + Dec.parse(cfg?.payoutRatio)).stripTrailingZeros().toPlainString()
    }

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
        if (disabled || cfg?.enabled == false) return "事件合约已暂停"
        if (cfg == null || product == null) return "产品信息不可用，请刷新后重试"
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

private val EV_INTERVALS = listOf("1m" to "1m", "5m" to "5m", "15m" to "15m", "30m" to "30m", "1h" to "1h", "4h" to "4h", "1d" to "1D")

private fun ma(c: List<Candle>, n: Int): List<Double?> = c.indices.map { i -> if (i + 1 < n) null else (i - n + 1..i).sumOf { c[it].close.toDouble() } / n }
private fun ema(c: List<Candle>, n: Int): List<Double?> {
    val k = 2.0 / (n + 1); var e: Double? = null
    return c.map { x -> val v = x.close.toDouble(); e = e?.let { v * k + it * (1 - k) } ?: v; e }
}
private fun boll(c: List<Candle>): Triple<List<Double?>, List<Double?>, List<Double?>> {
    val m = ma(c, 20)
    val sd = c.indices.map { i -> m[i]?.let { mean -> kotlin.math.sqrt((i - 19..i).sumOf { val d = c[it].close.toDouble() - mean; d * d } / 20) } }
    return Triple(m, m.indices.map { i -> m[i]?.let { it + 2 * sd[i]!! } }, m.indices.map { i -> m[i]?.let { it - 2 * sd[i]!! } })
}

/** 事件合约 K 线：周期 + 蜡烛 + 指标线（MA / BOLL / EMA，均由真实 K 线计算）。 */
@Composable
private fun EventChart(vm: EventContractViewModel) {
    val p = LB.p
    var interval by remember { mutableStateOf("15m") }
    var ind by remember { mutableStateOf(0) }
    var candles by remember { mutableStateOf<List<Candle>?>(null) }
    var err by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(vm.symbol, interval) {
        err = null
        while (true) {
            try { candles = vm.klines(interval).takeLast(60); err = null } catch (e: Throwable) { if (candles == null) err = e.lbMessage() }
            delay(10_000)
        }
    }
    val pageH = Spacing.pageH
    var showInd by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().padding(start = pageH - 4.dp, end = pageH).height(40.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(2.dp)) {
        EV_INTERVALS.forEach { (k, label) ->
            val on = k == interval
            Text(label, Modifier.clip(RoundedCornerShape(4.dp)).background(if (on) p.fill else androidx.compose.ui.graphics.Color.Transparent)
                .pressable(scale = 1f) { interval = k }.padding(horizontal = 8.dp, vertical = 4.dp), style = lb(FontSize.small, if (on) FontWeight.SemiBold else FontWeight.Normal),
                color = if (on) p.textPrimary else p.textSecondary)
        }
        Spacer(Modifier.weight(1f))
        LbIcon(R.drawable.ic_draw_line, 16.dp, p.textSecondary, Modifier.pressable { vm.toast.show("画线工具暂未开放") })
        Spacer(Modifier.width(12.dp))
        LbIcon(R.drawable.ic_chart_settings, 16.dp, p.textSecondary, Modifier.pressable { showInd = true })
    }
    if (showInd) LbSheet("指标设置", { showInd = false }, showClose = true) {
        Column(Modifier.padding(bottom = 16.dp)) {
            listOf("MA（5 / 10 / 20）", "BOLL（20, 2）", "EMA（5 / 10 / 20）").forEachIndexed { i, t ->
                Row(Modifier.fillMaxWidth().height(52.dp).pressable(scale = 1f) { ind = i; showInd = false }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                    Text(t, Modifier.weight(1f), style = lb(FontSize.body), color = p.textPrimary)
                    if (ind == i) LbIcon(R.drawable.ic_check, 18.dp, p.textPrimary)
                }
            }
        }
    }
    val c = candles
    val lines: List<Pair<String, List<Double?>>> = if (c == null) emptyList() else when (ind) {
        0 -> listOf("MA-5" to ma(c, 5), "MA-10" to ma(c, 10), "MA-20" to ma(c, 20))
        1 -> boll(c).let { (m, u, l) -> listOf("BOLL" to m, "UB" to u, "LB" to l) }
        else -> listOf("EMA-5" to ema(c, 5), "EMA-10" to ema(c, 10), "EMA-20" to ema(c, 20))
    }
    val colors = listOf(androidx.compose.ui.graphics.Color(0xFFF0A23B), androidx.compose.ui.graphics.Color(0xFFE0457B), androidx.compose.ui.graphics.Color(0xFF7B5CE0))
    Row(Modifier.padding(horizontal = pageH).height(18.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        lines.forEachIndexed { i, (n, v) -> Text("$n: ${v.lastOrNull()?.let { Fmt.price(java.math.BigDecimal(it), 2) } ?: "--"}", style = lbNum(10.sp), color = colors[i]) }
    }
    Box(Modifier.fillMaxWidth().height(150.dp)) {
        when {
            err != null && c == null -> Text(err ?: "", Modifier.align(Alignment.Center), style = lb(FontSize.small), color = p.down)
            c == null -> CircularProgressIndicator(Modifier.align(Alignment.Center).height(24.dp), color = p.textSecondary, strokeWidth = 2.dp)
            c.isEmpty() -> EmptyState("暂无 K 线数据", Modifier.align(Alignment.Center))
            else -> {
                val hi = maxOf(c.maxOf { it.high.toDouble() }, lines.flatMap { it.second }.filterNotNull().maxOrNull() ?: 0.0)
                val lo = minOf(c.minOf { it.low.toDouble() }, lines.flatMap { it.second }.filterNotNull().minOrNull() ?: Double.MAX_VALUE)
                val last = c.last().close
                androidx.compose.foundation.Canvas(Modifier.fillMaxSize().padding(end = 64.dp, top = 6.dp, bottom = 18.dp)) {
                    val step = size.width / c.size
                    fun y(d: Double) = if (hi <= lo) size.height / 2 else ((hi - d) / (hi - lo) * size.height).toFloat()
                    for (g in 0..4) { val yy = size.height * g / 4f; drawLine(p.divider, androidx.compose.ui.geometry.Offset(0f, yy), androidx.compose.ui.geometry.Offset(size.width + 200f, yy), 0.5f) }
                    c.forEachIndexed { i, k ->
                        val col = if (k.close >= k.open) p.up else p.down
                        val x = step * (i + 0.5f)
                        drawLine(col, androidx.compose.ui.geometry.Offset(x, y(k.high.toDouble())), androidx.compose.ui.geometry.Offset(x, y(k.low.toDouble())), 1.5f)
                        val t = y(maxOf(k.open, k.close).toDouble()); val b = y(minOf(k.open, k.close).toDouble())
                        drawRect(col, androidx.compose.ui.geometry.Offset(x - step * 0.35f, t), androidx.compose.ui.geometry.Size(step * 0.7f, maxOf(b - t, 1.5f)))
                    }
                    lines.forEachIndexed { li, (_, v) ->
                        val path = androidx.compose.ui.graphics.Path(); var started = false
                        v.forEachIndexed { i, d -> if (d != null) { val x = step * (i + 0.5f); if (!started) { path.moveTo(x, y(d)); started = true } else path.lineTo(x, y(d)) } }
                        drawPath(path, colors[li], style = androidx.compose.ui.graphics.drawscope.Stroke(1.5f))
                    }
                    val ly = y(last.toDouble())
                    drawLine(p.textTertiary, androidx.compose.ui.geometry.Offset(0f, ly), androidx.compose.ui.geometry.Offset(size.width, ly), 1f, pathEffect = androidx.compose.ui.graphics.PathEffect.dashPathEffect(floatArrayOf(6f, 6f)))
                }
                Column(Modifier.align(Alignment.TopEnd).fillMaxHeight().padding(top = 0.dp, bottom = 12.dp, end = 4.dp), verticalArrangement = Arrangement.SpaceBetween, horizontalAlignment = Alignment.End) {
                    (0..4).forEach { g -> Text(Fmt.price(java.math.BigDecimal(hi - (hi - lo) * g / 4), 2), style = lbNum(9.sp), color = p.textTertiary) }
                }
                val frac = if (hi <= lo) 0.5f else ((hi - last.toDouble()) / (hi - lo)).toFloat()
                androidx.compose.foundation.layout.BoxWithConstraints(Modifier.fillMaxSize().padding(top = 6.dp, bottom = 18.dp)) {
                    Text(Fmt.price(last, 2), Modifier.align(Alignment.TopEnd).padding(top = (maxHeight * frac - 10.dp).coerceAtLeast(0.dp), end = 2.dp)
                        .background(p.bg, RoundedCornerShape(3.dp)).border(1.dp, p.textPrimary, RoundedCornerShape(3.dp)).padding(horizontal = 4.dp, vertical = 2.dp),
                        style = lbNum(10.sp), color = p.textPrimary)
                }
            }
        }
    }
    Row(Modifier.fillMaxWidth().padding(horizontal = pageH).height(36.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(18.dp)) {
        listOf("MA", "BOLL", "EMA").forEachIndexed { i, t ->
            Text(t, Modifier.pressable(scale = 1f) { ind = i }, style = lb(FontSize.caption, if (ind == i) FontWeight.SemiBold else FontWeight.Normal), color = if (ind == i) p.textPrimary else p.textSecondary)
        }
    }
}

private data class GuideStep(val title: String, val body: String)
private val EV_GUIDE = listOf(
    GuideStep("划转资产", "使用现货账户资产下单，可在此查看余额并划转资金"),
    GuideStep("合约到期时间", "在这里设置合约的到期时间。"),
    GuideStep("交易详情", "在此输入交易金额以查看到期可得，并选择价格方向。"),
    GuideStep("进行中与已结束的交易", "进行中的交易可在「当前持仓」查看；到期的交易可在「历史持仓」查看"),
)

@Composable
fun EventContractScreen(
    bottomInset: Dp,
    onLogin: () -> Unit,
    onTransfer: () -> Unit = {},
    vm: EventContractViewModel = hiltViewModel(),
) {
    val p = LB.p
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    val pageH = LayoutAdapt.pageH(LocalConfiguration.current.screenWidthDp)
    var pendingUp by remember { mutableStateOf<Boolean?>(null) }
    var showSymbols by remember { mutableStateOf(false) }
    var showPeriods by remember { mutableStateOf(false) }
    var showMore by remember { mutableStateOf(false) }
    var showRules by remember { mutableStateOf(false) }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val prefs = remember { ctx.getSharedPreferences("event_guide", android.content.Context.MODE_PRIVATE) }
    var guide by remember { mutableStateOf(if (prefs.getBoolean("seen", false)) -1 else 0) }
    val targets = remember { mutableStateMapOf<Int, androidx.compose.ui.geometry.Rect>() }
    val hostView = androidx.compose.ui.platform.LocalView.current
    fun Modifier.target(i: Int) = onGloballyPositioned {
        val loc = IntArray(2); hostView.getLocationOnScreen(loc)
        targets[i] = it.boundsInRoot().translate(loc[0].toFloat(), loc[1].toFloat())
    }
    DisposableEffect(Unit) { vm.start(); onDispose { vm.stop() } }
    LaunchedEffect(loggedIn, vm.symbol) { if (loggedIn) { vm.refreshOrders(); vm.loadSpot() } }
    val pct = Dec.parse(vm.cfg?.payoutRatio).let { if (it.signum() <= 0) "--" else it.multiply(java.math.BigDecimal(100)).stripTrailingZeros().toPlainString() + "%" }

    Box(Modifier.fillMaxSize()) {
    when {
        vm.loading && vm.cfg == null -> Box(Modifier.fillMaxSize().padding(bottom = bottomInset), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        vm.error != null && vm.cfg == null -> Column(Modifier.fillMaxSize().padding(bottom = bottomInset), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            EmptyState(vm.error ?: "加载失败") { ChipButton("重试", Modifier.height(32.dp)) { vm.load() } }
        }
        vm.disabled -> Column(Modifier.fillMaxSize().padding(bottom = bottomInset), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            EmptyState("事件合约") { Text("当前服务已暂停事件合约，无法创建订单", style = lb(FontSize.small), color = p.textSecondary, textAlign = TextAlign.Center) }
        }
        else -> Column(Modifier.fillMaxSize().imePadding().padding(bottom = bottomInset).verticalScroll(rememberScrollState())) {
            // 标的行：BTCUSDT ▾ ● | 看涨: 80% 看跌: 80% | ⋯
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).height(48.dp), verticalAlignment = Alignment.CenterVertically) {
                Row(Modifier.pressable(scale = 1f) { showSymbols = true }, verticalAlignment = Alignment.CenterVertically) {
                    Text(vm.symbol, style = lb(FontSize.title, FontWeight.Bold), color = p.textPrimary)
                    LbIcon(R.drawable.ic_caret_down, 10.dp, p.textSecondary, Modifier.padding(start = 6.dp))
                    Box(Modifier.padding(start = 8.dp).size(6.dp).clip(androidx.compose.foundation.shape.CircleShape).background(if (vm.product?.priceAvailable == false || vm.product?.paused == true) p.textTertiary else p.up))
                }
                Spacer(Modifier.weight(1f))
                Text("看涨: $pct  看跌: $pct", style = lb(FontSize.label, FontWeight.SemiBold), color = p.up)
                Box(Modifier.padding(start = 12.dp).size(28.dp).pressable(onClick = { showMore = true }), contentAlignment = Alignment.Center) { LbIcon(R.drawable.ic_more, 20.dp, p.textPrimary) }
            }
            EventChart(vm)
            Box(Modifier.fillMaxWidth().height(0.5.dp).background(p.divider))
            if (vm.product?.priceAvailable == false) Text("指数价格暂不可用，暂停下单", Modifier.padding(horizontal = pageH, vertical = 6.dp), style = lb(FontSize.small), color = p.down)
            // 周期 + 金额
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Column(Modifier.weight(0.42f).target(1)) {
                    Text("周期", style = lb(FontSize.caption), color = p.textSecondary)
                    Row(Modifier.padding(top = 8.dp).fillMaxWidth().height(44.dp).clip(RoundedCornerShape(Radius.md)).background(p.fill).pressable(scale = 1f) { showPeriods = true }.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(vm.period.replace("d", "D"), Modifier.weight(1f), style = lbNum(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                        LbIcon(R.drawable.ic_caret_down, 10.dp, p.textSecondary)
                    }
                }
                Column(Modifier.weight(1f).target(2)) {
                    Text("金额(${vm.asset})", style = lb(FontSize.caption), color = p.textSecondary)
                    Row(Modifier.padding(top = 8.dp).fillMaxWidth().height(44.dp).clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(start = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        androidx.compose.foundation.text.BasicTextField(vm.stakeText, { v -> vm.stakeText = v.filter { it.isDigit() || it == '.' } }, Modifier.weight(1f),
                            textStyle = lbNum(FontSize.body).copy(color = p.textPrimary), singleLine = true,
                            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal),
                            cursorBrush = androidx.compose.ui.graphics.SolidColor(p.textPrimary),
                            decorationBox = { inner -> Box { if (vm.stakeText.isEmpty()) Text("${apiAmount(vm.cfg?.minStake)}-${apiAmount(vm.cfg?.maxStake)}", style = lbNum(FontSize.body), color = p.textTertiary); inner() } })
                        val step = { d: Int -> val cur = Dec.parse(vm.stakeText); val mn = Dec.parse(vm.cfg?.minStake); val mx = Dec.parse(vm.cfg?.maxStake)
                            var n = cur + java.math.BigDecimal(d); if (n < mn) n = mn; if (mx.signum() > 0 && n > mx) n = mx; vm.stakeText = n.stripTrailingZeros().toPlainString() }
                        Text("−", Modifier.width(36.dp).pressable(scale = 1f) { step(-1) }, style = lb(FontSize.title), color = p.textTertiary, textAlign = TextAlign.Center)
                        Box(Modifier.width(0.5.dp).height(14.dp).background(p.border))
                        Text("+", Modifier.width(36.dp).pressable(scale = 1f) { step(1) }, style = lb(FontSize.title), color = p.textPrimary, textAlign = TextAlign.Center)
                    }
                }
            }
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("现货可用余额", Modifier.weight(1f), style = lb(FontSize.caption), color = p.textSecondary)
                Row(Modifier.target(0).pressable(scale = 1f) { if (loggedIn) onTransfer() else onLogin() }.padding(4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text("${if (loggedIn) Fmt.amount(Dec.parse(vm.spotFree ?: "0"), 2) else "--"} ${vm.asset}", style = lbNum(FontSize.caption, FontWeight.Medium), color = p.textPrimary)
                    ThemedIcon(R.drawable.ic_mc_plus_square_light, R.drawable.ic_mc_plus_square_dark, 14.dp, Modifier.padding(start = 4.dp))
                }
            }
            Box(Modifier.padding(horizontal = pageH).padding(top = 10.dp).fillMaxWidth().height(0.5.dp).background(p.divider))
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                listOf(true, false).forEach { up ->
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Row { Text("收益率", Modifier.weight(1f), style = lb(FontSize.small), color = p.textSecondary); Text(pct, style = lbNum(FontSize.small, FontWeight.Medium), color = p.up) }
                        Row { Text("到期可得", Modifier.weight(1f), style = lb(FontSize.small), color = p.textSecondary); Text("${apiAmount(vm.payoutAmount())} ${vm.asset}", style = lbNum(FontSize.small, FontWeight.Medium), color = p.textPrimary) }
                        LbButton(if (!loggedIn) "登录 / 注册" else if (up) "↑ 看涨" else "↓ 看跌", Modifier.padding(top = 6.dp), kind = if (!loggedIn) ButtonKind.PRIMARY else if (up) ButtonKind.BUY else ButtonKind.SELL, height = 44.dp, loading = vm.placing) {
                            if (!loggedIn) onLogin() else vm.validation()?.let { vm.toast.show(it, ToastCenter.Kind.ERROR) } ?: run { pendingUp = up }
                        }
                    }
                }
            }
            Row(Modifier.fillMaxWidth().padding(horizontal = pageH).padding(top = 8.dp).height(36.dp), verticalAlignment = Alignment.CenterVertically) {
                Row(Modifier.target(3), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    listOf("当前持仓 (${vm.openOrders.size})", "历史持仓 (${vm.historyOrders.size})").forEachIndexed { i, t ->
                        Text(t, Modifier.pressable(scale = 1f) { vm.ordersTab = i }, style = lb(FontSize.body, if (vm.ordersTab == i) FontWeight.Medium else FontWeight.Normal), color = if (vm.ordersTab == i) p.textPrimary else p.textSecondary)
                    }
                }
                Spacer(Modifier.weight(1f))
                ThemedIcon(R.drawable.ic_duo_clock_fill_light, R.drawable.ic_duo_clock_fill_dark, 18.dp, Modifier.pressable { vm.ordersTab = 1 })
            }
            val list = if (vm.ordersTab == 0) vm.openOrders else vm.historyOrders
            when {
                !loggedIn -> EmptyState("登录后查看事件合约订单", Modifier.fillMaxWidth().padding(vertical = 24.dp)) { ChipButton("登录 / 注册", Modifier.width(140.dp).height(36.dp), onClick = onLogin) }
                list.isEmpty() -> EmptyState("暂无记录", Modifier.fillMaxWidth().padding(top = 4.dp, bottom = 16.dp))
                else -> {
                    list.forEach { EventOrderRow(it) }
                    if (vm.ordersTab == 1 && vm.historyCursor != null) ChipButton(if (vm.loadingMore) "加载中…" else "加载更多", Modifier.fillMaxWidth().height(40.dp).padding(horizontal = pageH, vertical = 8.dp)) { if (!vm.loadingMore) vm.loadMoreHistory() }
                }
            }
            Spacer(Modifier.height(24.dp))
        }
    }
    // ── 新手引导（参考 1/4 ~ 4/4，聚光 + 气泡）──
    if (guide >= 0 && vm.cfg != null && !vm.disabled) targets[guide]?.let { r -> EventGuide(guide, r, onPrev = { guide-- }, onNext = { if (guide < 3) guide++ else { prefs.edit().putBoolean("seen", true).apply(); guide = -1 } }, onClose = { prefs.edit().putBoolean("seen", true).apply(); guide = -1 }) }
    }
    if (showSymbols) LbSheet("选择标的", { showSymbols = false }, showClose = true) {
        Column(Modifier.padding(bottom = 16.dp)) {
            vm.cfg?.products.orEmpty().forEach { pr ->
                Row(Modifier.fillMaxWidth().height(52.dp).pressable(scale = 1f) { vm.symbol = pr.symbol ?: vm.symbol; showSymbols = false }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                    CoinIcon(pr.baseAsset ?: "?", 22.dp)
                    Text(pr.symbol ?: "--", Modifier.padding(start = 10.dp).weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                    Text(if (pr.paused == true) "暂停" else apiAmount(pr.indexPrice), style = lbNum(FontSize.label), color = p.textSecondary)
                    if (pr.symbol == vm.symbol) LbIcon(R.drawable.ic_check, 16.dp, p.textPrimary, Modifier.padding(start = 10.dp))
                }
            }
        }
    }
    if (showPeriods) LbSheet("周期", { showPeriods = false }) {
        Column(Modifier.padding(bottom = 16.dp)) {
            vm.cfg?.periods.orEmpty().forEach { pd ->
                Row(Modifier.fillMaxWidth().height(52.dp).pressable(scale = 1f) { vm.period = pd.period ?: vm.period; showPeriods = false }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                    Text((pd.period ?: "--").replace("d", "D"), Modifier.weight(1f), style = lb(FontSize.body), color = p.textPrimary)
                    if (pd.period == vm.period) LbIcon(R.drawable.ic_check, 18.dp, p.textPrimary)
                }
            }
        }
    }
    if (showMore) LbSheet(null, { showMore = false }) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 24.dp)) {
            Text("常用", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
            Row(Modifier.padding(top = 18.dp)) {
                listOf(Triple("交易规则", R.drawable.ic_doc_text) { showMore = false; showRules = true }, Triple("新手指引", R.drawable.ic_cv_help) { showMore = false; prefs.edit().putBoolean("seen", false).apply(); guide = 0 }).forEach { (t, ic, act) ->
                    Column(Modifier.weight(1f).pressable(onClick = act), horizontalAlignment = Alignment.CenterHorizontally) {
                        LbIcon(ic, 24.dp, p.textPrimary); Text(t, Modifier.padding(top = 8.dp), style = lb(FontSize.small), color = p.textPrimary)
                    }
                }
                Spacer(Modifier.weight(2f))
            }
        }
    }
    if (showRules) LbSheet("交易规则", { showRules = false }, showClose = true) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            EventContractLogic.rules(vm.cfg, vm.asset).forEach { r -> Row { Text("• ", style = lb(FontSize.caption), color = p.textSecondary); Text(r, style = lb(FontSize.caption), color = p.textSecondary) } }
            LbButton("知道了") { showRules = false }
        }
    }
    pendingUp?.let { up ->
        LbSheet(if (up) "确认看涨" else "确认看跌", { pendingUp = null }, showClose = true) {
            Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                KvRow("标的", vm.symbol)
                KvRow("周期", EventContractLogic.periodTitle(vm.period))
                KvRow("投注", "${vm.stakeText} ${vm.asset}")
                KvRow("到期可得", "${apiAmount(vm.payoutAmount())} ${vm.asset}")
                KvRow("开仓参考", apiAmount(vm.product?.indexPrice))
                Text("开仓价以下单时指数价为准；到期结算，猜对获本金+收益，猜错没收本金。", style = lb(FontSize.small), color = p.textTertiary)
                LbButton("确认下单", kind = if (up) ButtonKind.BUY else ButtonKind.SELL, loading = vm.placing) { vm.place(up) { ok -> if (ok) pendingUp = null } }
            }
        }
    }
}

/** 全屏引导（独立窗口，覆盖顶部 Tab 与底部导航）：聚光挖空 + 带箭头气泡；位置按参考逐步对齐。 */
@Composable
private fun EventGuide(i: Int, r: androidx.compose.ui.geometry.Rect, onPrev: () -> Unit, onNext: () -> Unit, onClose: () -> Unit) {
    val p = LB.p
    val d = androidx.compose.ui.platform.LocalDensity.current
    androidx.compose.ui.window.Dialog(onDismissRequest = onClose, properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        val dv = androidx.compose.ui.platform.LocalView.current
        androidx.compose.runtime.SideEffect {
            (dv.parent as? androidx.compose.ui.window.DialogWindowProvider)?.window?.let { w ->
                w.setDimAmount(0f); w.statusBarColor = android.graphics.Color.TRANSPARENT; w.navigationBarColor = android.graphics.Color.TRANSPARENT
                w.setLayout(android.view.ViewGroup.LayoutParams.MATCH_PARENT, android.view.ViewGroup.LayoutParams.MATCH_PARENT)
            }
        }
        val origin = remember { mutableStateOf(androidx.compose.ui.geometry.Offset.Zero) }
        val pad = with(d) { 6.dp.toPx() }
        val hole = androidx.compose.ui.geometry.Rect(r.left - pad - origin.value.x, r.top - pad - origin.value.y, r.right + pad - origin.value.x, r.bottom + pad - origin.value.y)
        androidx.compose.foundation.layout.BoxWithConstraints(Modifier.fillMaxSize().onGloballyPositioned { val loc = IntArray(2); dv.getLocationOnScreen(loc); origin.value = it.positionInRoot() + androidx.compose.ui.geometry.Offset(loc[0].toFloat(), loc[1].toFloat()) }.pressable(scale = 1f) {}) {
            androidx.compose.foundation.Canvas(Modifier.fillMaxSize().graphicsLayer(compositingStrategy = androidx.compose.ui.graphics.CompositingStrategy.Offscreen)) {
                drawRect(androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.55f))
                drawRoundRect(androidx.compose.ui.graphics.Color.Black, hole.topLeft, hole.size, androidx.compose.ui.geometry.CornerRadius(pad), blendMode = androidx.compose.ui.graphics.BlendMode.Clear)
            }
            val g = EV_GUIDE[i]
            val above = i == 3                     // 参考第 4 步气泡在目标上方
            val alignEnd = i == 0 || i == 2         // 参考第 1、3 步气泡靠右
            val cardW = minOf(maxWidth - 24.dp, 300.dp)
            val holeTop = with(d) { hole.top.toDp() }; val holeBottom = with(d) { hole.bottom.toDp() }
            val holeCx = with(d) { hole.center.x.toDp() }
            val cardX = if (alignEnd) maxWidth - 12.dp - cardW else 12.dp
            val arrowX = (holeCx - cardX).coerceIn(20.dp, cardW - 20.dp)
            var cardH by remember { mutableStateOf(0.dp) }
            val cardY = if (above) holeTop - 10.dp - cardH else holeBottom + 10.dp
            Column(Modifier.offset(x = cardX, y = cardY).width(cardW).onGloballyPositioned { cardH = with(d) { it.size.height.toDp() } }) {
                val tri = @Composable { up: Boolean ->
                    androidx.compose.foundation.Canvas(Modifier.padding(start = arrowX - 8.dp).size(16.dp, 8.dp)) {
                        val path = androidx.compose.ui.graphics.Path().apply {
                            if (up) { moveTo(0f, size.height); lineTo(size.width / 2, 0f); lineTo(size.width, size.height) } else { moveTo(0f, 0f); lineTo(size.width / 2, size.height); lineTo(size.width, 0f) }
                            close()
                        }
                        drawPath(path, p.bg)
                    }
                }
                if (!above) tri(true)
                Column(Modifier.fillMaxWidth().background(p.bg, RoundedCornerShape(12.dp)).padding(16.dp)) {
                    Row(verticalAlignment = Alignment.Top) {
                        Text(g.title, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.Bold), color = p.textPrimary)
                        LbIcon(R.drawable.ic_close, 16.dp, p.textTertiary, Modifier.pressable(onClick = onClose))
                    }
                    Text(g.body, Modifier.padding(top = 8.dp), style = lb(FontSize.body), color = p.textPrimary)
                    Row(Modifier.padding(top = 14.dp).fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text("${i + 1}/4", Modifier.weight(1f), style = lbNum(FontSize.small), color = p.textSecondary)
                        LbIcon(R.drawable.ic_arrow_left, 18.dp, if (i == 0) p.textTertiary else p.textPrimary, Modifier.pressable(enabled = i > 0, onClick = onPrev))
                        Spacer(Modifier.width(18.dp))
                        if (i < 3) LbIcon(R.drawable.ic_chevron_right, 18.dp, p.textPrimary, Modifier.pressable(onClick = onNext))
                        else Text("完成", Modifier.clip(RoundedCornerShape(14.dp)).background(p.textPrimary).pressable(onClick = onNext).padding(horizontal = 14.dp, vertical = 5.dp), style = lb(FontSize.small, FontWeight.Medium), color = p.bg)
                    }
                }
                if (above) tri(false)
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
