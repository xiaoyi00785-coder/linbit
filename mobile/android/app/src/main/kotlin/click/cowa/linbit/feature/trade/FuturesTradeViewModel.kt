package click.cowa.linbit.feature.trade

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiFuturesAlgoOrder
import click.cowa.linbit.core.generated.ApiFuturesAccount
import click.cowa.linbit.core.generated.ApiFuturesBonus
import click.cowa.linbit.core.generated.ApiFuturesTodayPnl200Response
import click.cowa.linbit.core.generated.ApiFuturesContract
import click.cowa.linbit.core.generated.ApiFuturesOrder
import click.cowa.linbit.core.generated.ApiFuturesPlaceOrderRequest
import click.cowa.linbit.core.generated.ApiFuturesPosition
import click.cowa.linbit.core.generated.ApiFuturesPositionMode
import click.cowa.linbit.core.generated.ApiFuturesRiskTiers200ResponseContractsInnerTiersInner
import click.cowa.linbit.core.generated.ApiFuturesSetLeverageRequest
import click.cowa.linbit.core.generated.ApiFuturesAdjustMarginRequest
import click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest
import click.cowa.linbit.core.generated.ApiFuturesAmendOrderRequest
import click.cowa.linbit.core.generated.ApiFuturesSetTpslRequest
import click.cowa.linbit.core.generated.ApiFuturesTpslOrderRequest
import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.OneOfResponses
import click.cowa.linbit.core.network.TokenStore
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.data.MarketStore
import click.cowa.linbit.data.trade.OrderBook
import click.cowa.linbit.data.trade.TradeNav
import click.cowa.linbit.ui.components.ToastCenter
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import java.math.BigDecimal
import java.math.RoundingMode
import javax.inject.Inject

/** 合约委托类型（对照图 ref/11「委托类型」弹窗）。 */
enum class FuturesKind(val title: String, val desc: String) {
    LIMIT("限价单", "以指定价格或更优价格成交"),
    MARKET("市价单", "以当前最优价格立即成交"),
    PLAN("计划委托", "价格到达触发价后自动下单"),
    POST_ONLY("只做 Maker", "只挂单不吃单，若会立即成交则自动撤销"),
}

/** 合约交易页状态（对应 iOS FuturesModel）。 */
@HiltViewModel
class FuturesTradeViewModel @Inject constructor(
    private val api: LinbitApi,
    private val store: MarketStore,
    private val socket: MarketSocket,
    private val tokens: TokenStore,
    private val nav: TradeNav,
    val toast: ToastCenter,
) : ViewModel() {
    private val json = Json { ignoreUnknownKeys = true; isLenient = true }

    val symbolFlow: StateFlow<String> = nav.futuresSymbol
    var symbol by mutableStateOf(nav.futuresSymbol.value); private set
    var contract by mutableStateOf<ApiFuturesContract?>(null); private set
    var ticker by mutableStateOf<ApiFuturesTicker?>(null); private set
    var book by mutableStateOf(OrderBook()); private set
    var account by mutableStateOf<ApiFuturesAccount?>(null); private set
    var positions by mutableStateOf<List<ApiFuturesPosition>>(emptyList()); private set
    var openOrders by mutableStateOf<List<ApiFuturesOrder>>(emptyList()); private set
    var tpslAlgos by mutableStateOf<List<ApiFuturesAlgoOrder>>(emptyList()); private set
    var planAlgos by mutableStateOf<List<ApiFuturesAlgoOrder>>(emptyList()); private set
    var tiers by mutableStateOf<List<ApiFuturesRiskTiers200ResponseContractsInnerTiersInner>>(emptyList()); private set
    var accountLoaded by mutableStateOf(false); private set
    var wsMark by mutableStateOf<Map<String, BigDecimal>>(emptyMap()); private set
    var wsFunding by mutableStateOf<Map<String, Pair<BigDecimal, Long?>>>(emptyMap()); private set
    var nowMs by mutableStateOf(System.currentTimeMillis()); private set

    var leverage by mutableStateOf(20); private set
    var maxLeverage by mutableStateOf(100); private set
    var cross by mutableStateOf(true); private set
    var hedge by mutableStateOf(false); private set

    var isOpen by mutableStateOf(true)
    var kind by mutableStateOf(FuturesKind.LIMIT)
    var priceText by mutableStateOf("")
    var costText by mutableStateOf("")
    var percent by mutableStateOf(0f)
    var tpsl by mutableStateOf(false)
    var tpText by mutableStateOf("")
    var slText by mutableStateOf("")
    var triggerText by mutableStateOf("")
    var planMarket by mutableStateOf(false)
    var bookPrecision by mutableStateOf<BigDecimal?>(null)
    var bookMode by mutableStateOf(0)
    var onlyCurrent by mutableStateOf(false)
    var placing by mutableStateOf(false); private set
    var chartOpen by mutableStateOf(false)
    /** 开仓表单的输入口径：false = 成本（USDT），true = 数量（币）。对照图 ref/11 与 ref/17 的单位下拉。 */
    var byQty by mutableStateOf(false)
    var qtyText by mutableStateOf("")
    var bonus by mutableStateOf<List<ApiFuturesBonus>>(emptyList()); private set
    var todayPnl by mutableStateOf<ApiFuturesTodayPnl200Response?>(null); private set
    var todayPnlError by mutableStateOf<String?>(null); private set

    val loggedIn: StateFlow<Boolean> get() = tokens.loggedIn
    val market get() = store.state
    private var jobs = emptyList<Job>()
    private var markJobs = emptyMap<String, Job>()

    init {
        viewModelScope.launch {
            tokens.sessionVersion.drop(1).collect {
                clearAccountData()
                if (tokens.loggedIn.value) {
                    refreshAccountNow()
                    loadTodayPnl()
                }
            }
        }
    }

    val base get() = contract?.baseAsset ?: symbol.removeSuffix("USDT")
    val quote get() = contract?.quoteAsset ?: "USDT"
    val pricePrec get() = maxOf(Dec.precision(contract?.tickSize), 1)
    val qtyPrec get() = Dec.precision(contract?.minQty)
    val minQty get() = Dec.parse(contract?.minQty)
    val isPlan get() = kind == FuturesKind.PLAN
    val marketLike get() = kind == FuturesKind.MARKET || (isPlan && planMarket)

    private val spotLast get() = store.state.value.futures.firstOrNull { it.symbol == symbol }?.last ?: BigDecimal.ZERO
    val last: BigDecimal get() = Dec.parse(ticker?.lastPrice).takeIf { it.signum() > 0 } ?: spotLast
    val mark: BigDecimal
        get() {
            wsMark[symbol]?.takeIf { it.signum() > 0 }?.let { return it }
            val m = Dec.parse(ticker?.markPrice)
            return if (m.signum() > 0) m else last
        }
    val changePct get() = Dec.parse(ticker?.priceChangePercent24h)
    val fundingRate: BigDecimal get() = (wsFunding[symbol]?.first ?: Dec.parse(ticker?.fundingRate)).multiply(BigDecimal(100))
    val available get() = Dec.parse(account?.availableBalance)
    val mmr get() = Dec.parseOrNull(contract?.maintenanceMarginRate) ?: BigDecimal("0.004")

    val orderPrice: BigDecimal
        get() {
            if (marketLike) {
                val t = Dec.parse(triggerText)
                return if (isPlan && t.signum() > 0) t else if (last.signum() > 0) last else mark
            }
            return Dec.parse(priceText)
        }
    val cost: BigDecimal
        get() = if (byQty && isOpen) {
            if (orderPrice.signum() > 0 && leverage > 0) Dec.parse(qtyText).multiply(orderPrice).divide(BigDecimal(leverage), 8, RoundingMode.UP) else BigDecimal.ZERO
        } else Dec.parse(costText)

    val current: ApiFuturesPosition? get() = positions.firstOrNull { it.symbol == symbol && Dec.parse(it.qty).signum() != 0 }
    fun position(long: Boolean): ApiFuturesPosition? =
        positions.firstOrNull { it.symbol == symbol && Dec.parse(it.qty).signum() != 0 && (it.side == ApiFuturesPosition.Side.SHORT) != long }
    val closeTarget: ApiFuturesPosition? get() = if (hedge) position(true) ?: position(false) else current

    /** 成本 × 杠杆 / 价格 = 数量，按最小数量步长向下取整；平仓时按仓位数量 × 百分比。 */
    val qty: BigDecimal
        get() {
            if (orderPrice.signum() <= 0) return BigDecimal.ZERO
            if (!isOpen) closeTarget?.let { return Dec.parse(Dec.plain(Dec.parse(it.qty).abs().multiply(BigDecimal((if (percent > 0) percent else 1f).toDouble())), qtyPrec)) }
            if (byQty) return Dec.parse(Dec.plain(Dec.parse(qtyText), qtyPrec))
            return Dec.parse(Dec.plain(cost.multiply(BigDecimal(leverage)).divide(orderPrice, 12, RoundingMode.DOWN), qtyPrec))
        }
    val maxOpenQty: BigDecimal get() = if (orderPrice.signum() > 0) available.multiply(BigDecimal(leverage)).divide(orderPrice, qtyPrec, RoundingMode.DOWN) else BigDecimal.ZERO
    fun liqPrice(long: Boolean): BigDecimal {
        val p = orderPrice
        if (p.signum() <= 0) return BigDecimal.ZERO
        val inv = BigDecimal.ONE.divide(BigDecimal(leverage), 12, RoundingMode.HALF_UP)
        return if (long) p.multiply(BigDecimal.ONE.subtract(inv).add(mmr)) else p.multiply(BigDecimal.ONE.add(inv).subtract(mmr))
    }

    val shownPositions get() = if (onlyCurrent) positions.filter { it.symbol == symbol } else positions
    val shownOrders get() = if (onlyCurrent) openOrders.filter { it.symbol == symbol } else openOrders
    val shownPlans get() = if (onlyCurrent) planAlgos.filter { it.symbol == symbol } else planAlgos
    val shownTpsl get() = if (onlyCurrent) tpslAlgos.filter { it.symbol == symbol } else tpslAlgos

    val fundingCountdown: String
        get() {
            val t = wsFunding[symbol]?.second ?: ticker?.nextFundingTime ?: return "--:--:--"
            val s = maxOf(0L, t / 1000 - nowMs / 1000)
            return "%02d:%02d:%02d".format(s / 3600, s % 3600 / 60, s % 60)
        }

    fun fmt(d: BigDecimal, prec: Int) = Dec.plain(d, prec)

    fun markPriceFor(sym: String?, fallback: String?): BigDecimal {
        sym?.let { s -> wsMark[s]?.takeIf { it.signum() > 0 }?.let { return it } }
        if (sym == symbol) Dec.parse(ticker?.markPrice).takeIf { it.signum() > 0 }?.let { return it }
        return Dec.parse(fallback)
    }

    /** 按实时标记价重算未实现盈亏与收益率（多：(mark-entry)*qty；空：(entry-mark)*qty；收益率 = 盈亏 / 保证金）。 */
    fun livePnl(pos: ApiFuturesPosition): Triple<BigDecimal, BigDecimal, BigDecimal> {
        val mk = markPriceFor(pos.symbol, pos.markPrice)
        val entry = Dec.parse(pos.entryPrice)
        val q = Dec.parse(pos.qty).abs()
        if (mk.signum() <= 0 || entry.signum() <= 0 || q.signum() <= 0) return Triple(Dec.parse(pos.unrealizedPnl), Dec.parse(pos.roe).multiply(BigDecimal(100)), mk)
        val pnl = (if (pos.side == ApiFuturesPosition.Side.SHORT) entry.subtract(mk) else mk.subtract(entry)).multiply(q)
        val margin = Dec.parse(pos.margin)
        val roe = if (margin.signum() > 0) pnl.divide(margin, 8, RoundingMode.HALF_UP).multiply(BigDecimal(100)) else Dec.parse(pos.roe).multiply(BigDecimal(100))
        return Triple(pnl, roe, mk)
    }

    fun start() {
        stop()
        symbol = nav.futuresSymbol.value
        jobs = listOf(
            viewModelScope.launch {
                if (store.state.value.futures.isEmpty()) store.load()
                loadContract(); refreshMarket(); refreshAccountNow(); loadTodayPnl()
                if (priceText.isEmpty() && last.signum() > 0) priceText = fmt(last, pricePrec)
                syncMarks()
                var n = 0
                while (isActive) {
                    delay(1000); nowMs = System.currentTimeMillis(); n++
                    if (n % 3 == 0) refreshMarket()
                    if (n % 6 == 0) { refreshAccount(); syncMarks() }
                    if (n % 30 == 0) loadTodayPnl()
                }
            },
        )
    }

    fun stop() {
        jobs.forEach { it.cancel() }; jobs = emptyList()
        markJobs.values.forEach { it.cancel() }; markJobs = emptyMap()
    }

    fun switchSymbol(s: String) {
        nav.setFutures(s)
        priceText = ""; costText = ""; qtyText = ""; percent = 0f; ticker = null; book = OrderBook()
        start()
    }

    private suspend fun loadContract() {
        runCatching { apiCall { api.futuresContracts() } }.getOrNull()?.let { l ->
            contract = l.contracts?.firstOrNull { it.symbol == symbol }
            maxLeverage = contract?.maxLeverage ?: 100
        }
        tiers = runCatching { apiCall { api.futuresRiskTiers(symbol) } }.getOrNull()
            ?.contracts?.firstOrNull { it.symbol == symbol }?.tiers ?: emptyList()
    }

    private suspend fun refreshMarket() {
        coroutineScope {
            val t = async { runCatching { apiCall { OneOfResponses.futuresTickers(api.futuresTicker(symbol)) } }.getOrNull() }
            val b = async { runCatching { apiCall { api.futuresDepth(symbol) } }.getOrNull() }
            t.await()?.firstOrNull()?.let { ticker = it }
            b.await()?.let { book = OrderBook.parse(it.bids, it.asks) }
        }
    }

    /** 订阅当前合约 + 持仓合约的 mark:<symbol>，退订不再需要的。 */
    private fun syncMarks() {
        val want = (listOf(symbol) + positions.mapNotNull { it.symbol }).toSet()
        markJobs.filterKeys { it !in want }.forEach { (s, j) -> j.cancel(); wsMark = wsMark - s }
        val keep = markJobs.filterKeys { it in want }.toMutableMap()
        for (s in want) if (s !in keep) keep[s] = viewModelScope.launch {
            socket.subscribe("mark:$s").collect { f ->
                val o = runCatching { f.data.jsonObject }.getOrNull() ?: return@collect
                val v = Dec.parseOrNull(o["markPrice"]?.jsonPrimitive?.contentOrNull)?.takeIf { it.signum() > 0 } ?: return@collect
                val sym = o["symbol"]?.jsonPrimitive?.contentOrNull ?: s
                wsMark = wsMark + (sym to v)
                Dec.parseOrNull(o["fundingRate"]?.jsonPrimitive?.contentOrNull)?.let { r ->
                    wsFunding = wsFunding + (sym to (r to o["nextFundingTime"]?.jsonPrimitive?.longOrNull))
                }
            }
        }
        markJobs = keep
    }

    fun refreshAccount() {
        viewModelScope.launch { refreshAccountNow() }
    }

    fun clearAccountData() {
        account = null
        positions = emptyList()
        openOrders = emptyList()
        tpslAlgos = emptyList()
        planAlgos = emptyList()
        bonus = emptyList()
        todayPnl = null
        todayPnlError = null
        accountLoaded = false
    }

    private suspend fun refreshAccountNow() {
        if (!tokens.loggedIn.value) {
            clearAccountData()
            accountLoaded = true
            return
        }
        coroutineScope {
            val pm = async { runCatching { apiCall { api.futuresPositionMode() } }.getOrNull() }
            val ta = async { runCatching { apiCall { api.futuresTpslOrders() } }.getOrNull() }
            val pa = async { runCatching { apiCall { api.futuresPlanOrders() } }.getOrNull() }
            val a = async { runCatching { apiCall { api.futuresAccount() } }.getOrNull() }
            val o = async { runCatching { apiCall { api.futuresOrders() } }.getOrNull() }
            val l = async { runCatching { apiCall { api.futuresLeverage(symbol) } }.getOrNull() }
            val bn = async { runCatching { apiCall { api.futuresBonus() } }.getOrNull() }
            bn.await()?.let { bonus = it.items ?: emptyList() }
            a.await()?.let { acc -> account = acc; positions = (acc.positions ?: emptyList()).filter { Dec.parse(it.qty).signum() != 0 } }
            o.await()?.let { openOrders = it.orders ?: emptyList() }
            l.await()?.let { lv ->
                leverage = lv.leverage ?: leverage; maxLeverage = lv.maxLeverage ?: maxLeverage
                lv.marginMode?.let { cross = it != click.cowa.linbit.core.generated.ApiFuturesGetLeverage200Response.MarginMode.ISOLATED }
            }
            pm.await()?.let { hedge = it.positionMode == ApiFuturesPositionMode.PositionMode.HEDGE }
            ta.await()?.let { tpslAlgos = it.orders ?: emptyList() }
            pa.await()?.let { planAlgos = it.orders ?: emptyList() }
        }
        accountLoaded = true
    }

    fun loadTodayPnl() {
        if (!tokens.loggedIn.value) { todayPnl = null; return }
        viewModelScope.launch {
            try { todayPnl = apiCall { api.futuresTodayPnl() }; todayPnlError = null } catch (e: Throwable) { if (todayPnl == null) todayPnlError = e.message ?: "加载失败" }
        }
    }

    fun applyPercent(v: Float) {
        percent = v
        if (!isOpen) return
        if (byQty) {
            qtyText = if (v == 0f) "" else fmt(maxOpenQty.multiply(BigDecimal(v.toDouble())), qtyPrec)
        } else costText = if (v == 0f) "" else fmt(available.multiply(BigDecimal(v.toDouble())), 2)
    }

    val validation: String?
        get() {
            if (isPlan && Dec.parse(triggerText).signum() <= 0) return "请输入触发价"
            if (!marketLike && Dec.parse(priceText).signum() <= 0) return "请输入价格"
            if (isOpen && !byQty && cost.signum() <= 0) return "请输入成本"
            if (isOpen && byQty && Dec.parse(qtyText).signum() <= 0) return "请输入数量"
            if (qty.signum() <= 0 || (minQty.signum() > 0 && qty < minQty)) return "数量低于最小下单量 ${fmt(minQty, qtyPrec)} $base"
            if (isOpen && cost > available) return "可用保证金不足"
            if (!isOpen && closeTarget == null) return "当前无持仓可平"
            return null
        }

    /** long=true：开多 / 平空（买）；false：开空 / 平多（卖）。分仓模式：开多/平多 → LONG，开空/平空 → SHORT。 */
    fun place(long: Boolean, onDone: (Boolean) -> Unit = {}) {
        if (placing) return
        placing = true
        viewModelScope.launch {
            try {
                var posSide: ApiFuturesPlaceOrderRequest.PositionSide? = null
                var q = qty
                if (hedge) {
                    val isLongPos = if (isOpen) long else !long
                    posSide = if (isLongPos) ApiFuturesPlaceOrderRequest.PositionSide.LONG else ApiFuturesPlaceOrderRequest.PositionSide.SHORT
                    if (!isOpen) {
                        val pos = position(isLongPos) ?: run { toast.show("当前无${if (isLongPos) "多" else "空"}仓可平", ToastCenter.Kind.ERROR); onDone(false); return@launch }
                        q = Dec.parse(Dec.plain(Dec.parse(pos.qty).abs().multiply(BigDecimal((if (percent > 0) percent else 1f).toDouble())), qtyPrec))
                    }
                }
                val withTp = isOpen && tpsl && !isPlan
                val req = ApiFuturesPlaceOrderRequest(
                    symbol = symbol,
                    side = if (long) ApiFuturesPlaceOrderRequest.Side.BUY else ApiFuturesPlaceOrderRequest.Side.SELL,
                    type = if (marketLike) ApiFuturesPlaceOrderRequest.Type.MARKET else ApiFuturesPlaceOrderRequest.Type.LIMIT,
                    qty = fmt(q, qtyPrec),
                    timeInForce = if (kind == FuturesKind.POST_ONLY) ApiFuturesPlaceOrderRequest.TimeInForce.POST_ONLY
                    else if (marketLike) ApiFuturesPlaceOrderRequest.TimeInForce.IOC else ApiFuturesPlaceOrderRequest.TimeInForce.GTC,
                    price = if (marketLike) null else fmt(Dec.parse(priceText), pricePrec),
                    reduceOnly = if (isOpen || hedge) null else true,
                    tpPrice = if (withTp && tpText.isNotEmpty()) tpText else null,
                    slPrice = if (withTp && slText.isNotEmpty()) slText else null,
                    tpTriggerPriceType = if (withTp && tpText.isNotEmpty()) ApiFuturesPlaceOrderRequest.TpTriggerPriceType.MARK else null,
                    slTriggerPriceType = if (withTp && slText.isNotEmpty()) ApiFuturesPlaceOrderRequest.SlTriggerPriceType.MARK else null,
                    positionSide = posSide,
                    triggerPrice = if (isPlan) fmt(Dec.parse(triggerText), pricePrec) else null,
                    triggerPriceType = if (isPlan) ApiFuturesPlaceOrderRequest.TriggerPriceType.LAST else null,
                )
                val label = if (isOpen) (if (long) "开多" else "开空") else (if (long) "平空" else "平多")
                if (isPlan) {
                    apiCall { api.createFuturesPlan(req) }
                    toast.show("${label}计划委托已提交", ToastCenter.Kind.SUCCESS); triggerText = ""
                } else {
                    apiCall { api.placeFuturesOrder(req) }
                    toast.show("${label}委托已提交", ToastCenter.Kind.SUCCESS)
                }
                costText = ""; qtyText = ""; percent = 0f
                refreshAccountNow(); onDone(true)
            } catch (e: Throwable) {
                toast.error(e); onDone(false)
            } finally { placing = false }
        }
    }

    /** 切换持仓模式前的本地拦截：有仓位 / 委托都不可切换（后端同样返回 409）。 */
    val positionModeBlockReason: String?
        get() {
            val r = buildList {
                if (positions.isNotEmpty()) add("${positions.size} 个仓位")
                if (openOrders.isNotEmpty()) add("${openOrders.size} 笔挂单")
                if (planAlgos.isNotEmpty()) add("${planAlgos.size} 笔计划委托")
                if (tpslAlgos.isNotEmpty()) add("${tpslAlgos.size} 笔止盈止损委托")
            }
            return if (r.isEmpty()) null else "当前存在${r.joinToString("、")}，请先平仓并撤销全部委托后再切换仓位模式"
        }

    fun setPositionMode(h: Boolean, onDone: (Boolean) -> Unit = {}) {
        if (h == hedge) { onDone(true); return }
        positionModeBlockReason?.let { toast.show(it, ToastCenter.Kind.ERROR); onDone(false); return }
        viewModelScope.launch {
            try {
                val r = apiCall { api.setFuturesPositionMode(ApiFuturesPositionMode(if (h) ApiFuturesPositionMode.PositionMode.HEDGE else ApiFuturesPositionMode.PositionMode.ONE_WAY)) }
                hedge = r.positionMode == ApiFuturesPositionMode.PositionMode.HEDGE
                toast.show(if (hedge) "已切换为分仓模式" else "已切换为合仓模式", ToastCenter.Kind.SUCCESS)
                refreshAccountNow(); onDone(true)
            } catch (e: Throwable) { toast.error(e); onDone(false) }
        }
    }

    fun saveLeverage(v: Int, isCross: Boolean, onDone: (Boolean) -> Unit = {}) {
        viewModelScope.launch {
            try {
                val r = apiCall {
                    api.setFuturesLeverage(
                        ApiFuturesSetLeverageRequest(symbol, v, if (isCross) ApiFuturesSetLeverageRequest.MarginMode.CROSS else ApiFuturesSetLeverageRequest.MarginMode.ISOLATED),
                    )
                }
                leverage = r.leverage ?: v
                r.marginMode?.let { cross = it != click.cowa.linbit.core.generated.ApiFuturesGetLeverage200Response.MarginMode.ISOLATED }
                toast.show("已保存", ToastCenter.Kind.SUCCESS); onDone(true)
            } catch (e: Throwable) {
                if (!tokens.loggedIn.value) { leverage = v; cross = isCross; onDone(true) } else { toast.error(e); onDone(false) }
            }
        }
    }

    /** 分仓仓位的 positionSide（合仓为 null）。 */
    private fun sideOf(pos: ApiFuturesPosition): String? = pos.positionSide?.takeIf { it != ApiFuturesPosition.PositionSide.BOTH }?.value

    fun close(pos: ApiFuturesPosition) {
        viewModelScope.launch { closeNow(pos); refreshAccountNow() }
    }

    private suspend fun closeNow(pos: ApiFuturesPosition) {
        val sym = pos.symbol ?: symbol
        try {
            val side = sideOf(pos)
            if (side == null) apiCall { api.closeFuturesPosition(sym) }
            else apiCall { api.closeFuturesPositionSide(sym, JsonObject(mapOf("positionSide" to JsonPrimitive(side)))) }
            toast.show("已提交市价平仓", ToastCenter.Kind.SUCCESS)
        } catch (e: Throwable) { toast.error(e) }
    }

    fun closeAll() {
        val l = shownPositions
        viewModelScope.launch { l.forEach { closeNow(it) }; refreshAccountNow() }
    }

    fun cancel(o: ApiFuturesOrder) {
        val id = o.orderId ?: return
        viewModelScope.launch {
            try { apiCall { api.cancelFuturesOrder(id) }; toast.show("已撤销", ToastCenter.Kind.SUCCESS); refreshAccountNow() } catch (e: Throwable) { toast.error(e) }
        }
    }

    fun cancelAll() {
        val l = shownOrders
        viewModelScope.launch {
            var fails = 0
            l.mapNotNull { it.orderId }.map { id -> async { runCatching { apiCall { api.cancelFuturesOrder(id) } }.isFailure } }.forEach { if (it.await()) fails++ }
            toast.show(if (fails == 0) "已全部撤销" else "${l.size - fails} 笔撤销成功，$fails 笔失败", if (fails == 0) ToastCenter.Kind.SUCCESS else ToastCenter.Kind.ERROR)
            refreshAccountNow()
        }
    }

    fun cancelAlgo(a: ApiFuturesAlgoOrder) {
        val id = a.algoId ?: return
        viewModelScope.launch {
            try {
                if (a.algoType == ApiFuturesAlgoOrder.AlgoType.PLAN) apiCall { api.cancelFuturesPlan(id) } else apiCall { api.cancelFuturesTpsl(id) }
                toast.show("已撤销", ToastCenter.Kind.SUCCESS); refreshAccountNow()
            } catch (e: Throwable) { toast.error(e) }
        }
    }

    /** 指定杠杆下可持有的最大名义价值：取 maxLeverage ≥ lev 的最高档上限。 */
    fun maxNotional(lev: Int): BigDecimal? =
        tiers.filter { (it.maxLeverage ?: 0) >= lev }.maxByOrNull { Dec.parse(it.maxNotional) }?.let { Dec.parse(it.maxNotional) }


    /** 加载合约 K 线（/futures/market/klines）。 */
    suspend fun loadKlines(interval: String): List<Candle> =
        apiCall { api.futuresKlines(symbol, interval) }.klines.orEmpty().map(Candle::fromFutures)

    suspend fun loadMarketTrades() =
        apiCall { api.futuresMarketTrades(symbol) }.trades.orEmpty()

    /** 改单：PUT /futures/orders/{id}。 */
    fun amend(o: ApiFuturesOrder, price: String?, qty: String?, onDone: (Boolean, String?) -> Unit = { _, _ -> }) {
        val id = o.orderId ?: run { onDone(false, "无效订单"); return }
        if (price == null && qty == null) { onDone(false, "请至少修改价格或数量"); return }
        viewModelScope.launch {
            try {
                apiCall { api.amendFuturesOrder(id, ApiFuturesAmendOrderRequest(price = price, qty = qty)) }
                toast.show("改单成功", ToastCenter.Kind.SUCCESS); refreshAccountNow(); onDone(true, null)
            } catch (e: Throwable) { toast.error(e); onDone(false, e.message) }
        }
    }

    /** 整仓止盈止损：POST /futures/positions/{symbol}/tpsl（空字符串清除该项）。 */
    fun setTpsl(pos: ApiFuturesPosition, tp: String, sl: String, onDone: (Boolean) -> Unit = {}) {
        val sym = pos.symbol ?: symbol
        viewModelScope.launch {
            try {
                apiCall {
                    api.setFuturesPositionTpsl(
                        sym, sideOf(pos),
                        ApiFuturesSetTpslRequest(
                            tpPrice = tp.ifBlank { "" }, slPrice = sl.ifBlank { "" },
                            positionSide = sideOf(pos)?.let { runCatching { ApiFuturesSetTpslRequest.PositionSide.valueOf(it) }.getOrNull() },
                            tpTriggerPriceType = if (tp.isNotBlank()) ApiFuturesSetTpslRequest.TpTriggerPriceType.MARK else null,
                            slTriggerPriceType = if (sl.isNotBlank()) ApiFuturesSetTpslRequest.SlTriggerPriceType.MARK else null,
                        ),
                    )
                }
                toast.show(if (tp.isBlank() && sl.isBlank()) "已撤销止盈止损" else "止盈止损已设置", ToastCenter.Kind.SUCCESS)
                refreshAccountNow(); onDone(true)
            } catch (e: Throwable) { toast.error(e); onDone(false) }
        }
    }

    fun clearTpsl(pos: ApiFuturesPosition, onDone: (Boolean) -> Unit = {}) = setTpsl(pos, "", "", onDone)

    /** 部分止盈止损委托：POST /futures/tpsl-orders。 */
    fun createTpslOrder(pos: ApiFuturesPosition, qty: String?, tp: String?, sl: String?, onDone: (Boolean) -> Unit = {}) {
        val sym = pos.symbol ?: symbol
        viewModelScope.launch {
            try {
                apiCall {
                    api.createFuturesTpsl(
                        ApiFuturesTpslOrderRequest(
                            symbol = sym,
                            positionSide = sideOf(pos)?.let { runCatching { ApiFuturesTpslOrderRequest.PositionSide.valueOf(it) }.getOrNull() },
                            qty = qty,
                            tpTriggerPrice = tp,
                            slTriggerPrice = sl,
                            tpTriggerPriceType = if (tp != null) ApiFuturesTpslOrderRequest.TpTriggerPriceType.MARK else null,
                            slTriggerPriceType = if (sl != null) ApiFuturesTpslOrderRequest.SlTriggerPriceType.MARK else null,
                        ),
                    )
                }
                toast.show("止盈止损委托已提交", ToastCenter.Kind.SUCCESS); refreshAccountNow(); onDone(true)
            } catch (e: Throwable) { toast.error(e); onDone(false) }
        }
    }

    /** 逐仓调整保证金：正数追加、负数减少。 */
    fun adjustMargin(pos: ApiFuturesPosition, amount: BigDecimal, onDone: (Boolean) -> Unit = {}) {
        val sym = pos.symbol ?: symbol
        viewModelScope.launch {
            try {
                apiCall {
                    api.adjustFuturesMargin(sym, sideOf(pos), ApiFuturesAdjustMarginRequest(amount = amount.stripTrailingZeros().toPlainString()))
                }
                toast.show("保证金已调整", ToastCenter.Kind.SUCCESS); refreshAccountNow(); onDone(true)
            } catch (e: Throwable) { toast.error(e); onDone(false) }
        }
    }

    /** 截图 / 单测：直接注入行情状态，不触发网络。 */
    internal fun seedForShot(contract: ApiFuturesContract, ticker: ApiFuturesTicker, book: OrderBook, account: ApiFuturesAccount?) {
        this.contract = contract; this.ticker = ticker; this.book = book; this.account = account
        maxLeverage = contract.maxLeverage; leverage = contract.maxLeverage
        priceText = fmt(last, pricePrec)
        accountLoaded = true
    }

    override fun onCleared() { stop() }
}
