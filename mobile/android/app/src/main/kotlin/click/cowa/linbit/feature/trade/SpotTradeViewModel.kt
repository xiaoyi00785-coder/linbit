package click.cowa.linbit.feature.trade

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import click.cowa.linbit.core.generated.ApiBalance
import click.cowa.linbit.core.generated.ApiConditionalOrder
import click.cowa.linbit.core.generated.ApiConditionalOrderRequest
import click.cowa.linbit.core.generated.ApiDepth
import click.cowa.linbit.core.generated.ApiOrder
import click.cowa.linbit.core.generated.ApiOrderSide
import click.cowa.linbit.core.generated.ApiOrderStatus
import click.cowa.linbit.core.generated.ApiOrderType
import click.cowa.linbit.core.generated.ApiPlaceOrderRequest
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.generated.ApiTimeInForce
import click.cowa.linbit.core.network.LinbitApi
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
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import java.math.BigDecimal
import java.math.RoundingMode
import java.util.UUID
import javax.inject.Inject

/** 现货委托类型（对照图 ref/19：限价单 / 市价单 / 计划委托 / 只做 Maker）。 */
enum class SpotOrderKind(val title: String, val desc: String) {
    LIMIT("限价单", "以指定价格或更优价格成交"),
    MARKET("市价单", "以当前市场最优价格立即成交"),
    PLAN("计划委托", "价格达到触发价后按设定价格下单"),
    MAKER("只做 Maker", "仅作为挂单方成交，否则自动撤销"),
}

/** 现货交易页状态（对应 iOS SpotModel）。所有金额 / 价格 / 数量为 BigDecimal。 */
@HiltViewModel
class SpotTradeViewModel @Inject constructor(
    private val api: LinbitApi,
    private val store: MarketStore,
    private val socket: MarketSocket,
    private val tokens: TokenStore,
    private val nav: TradeNav,
    val toast: ToastCenter,
) : ViewModel() {
    private val json = Json { ignoreUnknownKeys = true; isLenient = true }

    val symbolFlow: StateFlow<String> = nav.spotSymbol
    var symbol by mutableStateOf(nav.spotSymbol.value); private set
    var meta by mutableStateOf<ApiSymbol?>(null); private set
    var book by mutableStateOf(OrderBook()); private set
    var bookLoaded by mutableStateOf(false); private set
    var last by mutableStateOf(BigDecimal.ZERO); private set
    var changePct by mutableStateOf(BigDecimal.ZERO); private set

    var isBuy by mutableStateOf(true)
    var kind by mutableStateOf(SpotOrderKind.LIMIT)
    var priceText by mutableStateOf("")
    var qtyText by mutableStateOf("")
    var amountText by mutableStateOf("")
    var percent by mutableStateOf(0f)
    var tpsl by mutableStateOf(false)
    var tpText by mutableStateOf("")
    var slText by mutableStateOf("")
    var triggerText by mutableStateOf("")
    var planMarket by mutableStateOf(false)
    var marketBuyByAmount by mutableStateOf(true)
    var bookPrecision by mutableStateOf<BigDecimal?>(null)
    var bookMode by mutableStateOf(0)
    var onlyCurrent by mutableStateOf(false)
    var placing by mutableStateOf(false); private set

    var balances by mutableStateOf<Map<String, ApiBalance>>(emptyMap()); private set
    var openOrders by mutableStateOf<List<ApiOrder>>(emptyList()); private set
    var conditional by mutableStateOf<List<ApiConditionalOrder>>(emptyList()); private set
    var makerRate by mutableStateOf<BigDecimal?>(null); private set
    var takerRate by mutableStateOf<BigDecimal?>(null); private set

    val loggedIn: StateFlow<Boolean> get() = tokens.loggedIn
    val market get() = store.state

    private var jobs = emptyList<Job>()

    val base get() = meta?.base ?: symbol.removeSuffix("USDT")
    val quote get() = meta?.quote ?: "USDT"
    val pricePrec get() = meta?.pricePrecision ?: 2
    val qtyPrec get() = meta?.quantityPrecision ?: 5
    val isPlan get() = kind == SpotOrderKind.PLAN
    val marketLike get() = kind == SpotOrderKind.MARKET || (isPlan && planMarket)
    val byAmount get() = marketLike && isBuy && marketBuyByAmount

    private val price: BigDecimal
        get() = if (kind == SpotOrderKind.MARKET) (if (isBuy) book.bestAsk else book.bestBid) ?: last else Dec.parse(priceText)
    private val qty get() = Dec.parse(qtyText)
    private val refPrice get() = if (isPlan) (if (planMarket) Dec.parse(triggerText) else Dec.parse(priceText)) else price
    val amount: BigDecimal get() = if (byAmount) Dec.parse(amountText) else refPrice.multiply(qty)
    val feeRate: BigDecimal get() = (if (kind == SpotOrderKind.MAKER) makerRate else takerRate) ?: FALLBACK_FEE
    val availQuote get() = Dec.parse(balances[quote]?.free)
    val availBase get() = Dec.parse(balances[base]?.free)
    val maxQty: BigDecimal get() = if (isBuy) (if (price.signum() > 0) availQuote.divide(price, qtyPrec, RoundingMode.DOWN) else BigDecimal.ZERO) else availBase
    val fee: BigDecimal get() = amount.multiply(feeRate)
    val shownOrders get() = if (onlyCurrent) openOrders.filter { it.symbol == symbol } else openOrders
    val shownConditional get() = if (onlyCurrent) conditional.filter { it.symbol == symbol } else conditional

    fun fmt(d: BigDecimal, prec: Int) = Dec.plain(d, prec)

    /** 下单前校验（返回错误文案；通过为 null）。未登录由界面先拦截登录。 */
    val validation: String?
        get() {
            if (isPlan) {
                if (Dec.parse(triggerText).signum() <= 0) return "请输入触发价"
                if (!planMarket && Dec.parse(priceText).signum() <= 0) return "请输入委托价格"
            } else if (kind != SpotOrderKind.MARKET && price.signum() <= 0) return "请输入价格"
            if (byAmount) {
                if (Dec.parse(amountText).signum() <= 0) return "请输入交易额"
                if (Dec.parse(amountText) > availQuote) return "可用 $quote 不足"
                return null
            }
            if (qty.signum() <= 0) return "请输入数量"
            meta?.minQuantity?.let { if (qty < Dec.parse(it)) return "最小下单数量 $it $base" }
            if (isBuy && amount > availQuote) return "可用 $quote 不足"
            if (!isBuy && qty > availBase) return "可用 $base 不足"
            if (kind == SpotOrderKind.MAKER) {
                if (isBuy) book.bestAsk?.let { if (price >= it) return "只做 Maker：买入价需低于卖一价" }
                else book.bestBid?.let { if (price <= it) return "只做 Maker：卖出价需高于买一价" }
            }
            return null
        }

    fun start() {
        stop()
        symbol = nav.spotSymbol.value
        viewModelScope.launch {
            if (store.state.value.spot.isEmpty()) store.load()
            meta = store.meta(symbol) ?: runCatching { apiCall { api.symbols() }.firstOrNull { it.symbol == symbol } }.getOrNull()
            store.state.value.spot.firstOrNull { it.symbol == symbol }?.let { last = it.last; changePct = it.changePct }
            loadBook()
            loadFees()
            refreshAccount()
        }
        jobs = listOf(
            viewModelScope.launch {
                socket.subscribe("depth:$symbol").collect { f ->
                    runCatching { json.decodeFromJsonElement<ApiDepth>(f.data) }.getOrNull()?.let { book = OrderBook.parse(it.bids, it.asks) }
                }
            },
            viewModelScope.launch { // 行情推送：最新价 / 涨跌幅
                store.state.collect { s -> s.spot.firstOrNull { it.symbol == symbol }?.let { if (it.last.signum() > 0) last = it.last; changePct = it.changePct } }
            },
            viewModelScope.launch { // WS 不可用时 3 秒轮询兜底
                while (isActive) { delay(3000); loadBook(silent = true) }
            },
        )
    }

    fun stop() { jobs.forEach { it.cancel() }; jobs = emptyList() }

    fun switchSymbol(s: String) {
        nav.setSpot(s)
        priceText = ""; qtyText = ""; amountText = ""; percent = 0f; last = BigDecimal.ZERO; book = OrderBook()
        start()
    }

    private suspend fun loadBook(silent: Boolean = false) {
        runCatching { apiCall { api.depth(symbol, 20) } }.onSuccess {
            book = OrderBook.parse(it.bids, it.asks)
            bookLoaded = true
            if (last.signum() == 0) last = book.bestBid ?: book.bestAsk ?: BigDecimal.ZERO
            if (priceText.isEmpty() && last.signum() > 0) priceText = fmt(last, pricePrec)
        }.onFailure { if (!silent) bookLoaded = true }
    }

    fun refreshAccount() {
        if (!tokens.loggedIn.value) { balances = emptyMap(); openOrders = emptyList(); conditional = emptyList(); return }
        viewModelScope.launch {
            coroutineScope {
                val b = async { runCatching { apiCall { api.balances() } }.getOrNull() }
                val o = async { runCatching { apiCall { api.spotOpenOrders() } }.getOrNull() }
                val c = async { runCatching { apiCall { api.conditionalOrders("open") } }.getOrNull() }
                b.await()?.let { r -> balances = r.items.associateBy { it.asset } }
                o.await()?.let { openOrders = it.items }
                c.await()?.let { conditional = it.items ?: emptyList() }
            }
        }
    }

    private suspend fun loadFees() {
        if (tokens.loggedIn.value) {
            runCatching { apiCall { api.feeRates(symbol) } }.getOrNull()?.let { r ->
                makerRate = Dec.parseOrNull(r.spot?.maker); takerRate = Dec.parseOrNull(r.spot?.taker); return
            }
        }
        runCatching { apiCall { api.feeSchedule() } }.getOrNull()?.let { r ->
            makerRate = Dec.parseOrNull(r.spot.maker); takerRate = Dec.parseOrNull(r.spot.taker)
        }
    }

    // 数量 / 交易额 / 滑杆 联动
    fun applyPercent(v: Float) {
        percent = v
        if (byAmount) { amountText = fmt(availQuote.multiply(BigDecimal(v.toDouble())), 2); return }
        val q = maxQty.multiply(BigDecimal(v.toDouble())).setScale(qtyPrec, RoundingMode.DOWN)
        qtyText = if (q.signum() > 0) q.toPlainString() else ""
        amountText = if (q.signum() > 0) fmt(q.multiply(price), pricePrec) else ""
    }

    fun qtyEdited() {
        amountText = if (qty.signum() > 0 && price.signum() > 0) fmt(amount, pricePrec) else ""
        percent = if (maxQty.signum() > 0) qty.divide(maxQty, 4, RoundingMode.DOWN).toFloat().coerceIn(0f, 1f) else 0f
    }

    fun amountEdited() {
        val a = Dec.parse(amountText)
        qtyText = if (a.signum() > 0 && price.signum() > 0) fmt(a.divide(price, qtyPrec, RoundingMode.DOWN), qtyPrec) else ""
        percent = if (maxQty.signum() > 0) qty.divide(maxQty, 4, RoundingMode.DOWN).toFloat().coerceIn(0f, 1f) else 0f
    }

    fun pickBookPrice(p: BigDecimal) { if (kind != SpotOrderKind.MARKET) { priceText = fmt(p, pricePrec); qtyEdited() } }

    /** 提交委托（调用方已校验 + 确认）。成功后清空数量并刷新账户。 */
    fun place(onDone: (Boolean) -> Unit = {}) {
        if (placing) return
        placing = true
        viewModelScope.launch {
            val cid = "and-" + UUID.randomUUID().toString().take(12)
            val quoteQty = if (byAmount) fmt(Dec.parse(amountText), pricePrec) else null
            val baseQty = if (byAmount) null else fmt(qty, qtyPrec)
            try {
                if (isPlan) {
                    apiCall {
                        api.createConditional(
                            ApiConditionalOrderRequest(
                                symbol = symbol, side = if (isBuy) ApiOrderSide.BUY else ApiOrderSide.SELL,
                                type = if (planMarket) ApiOrderType.MARKET else ApiOrderType.LIMIT,
                                triggerPrice = fmt(Dec.parse(triggerText), pricePrec),
                                kind = ApiConditionalOrderRequest.Kind.TRIGGER,
                                price = if (planMarket) null else fmt(Dec.parse(priceText), pricePrec),
                                quantity = baseQty, quoteOrderQty = quoteQty,
                                timeInForce = if (planMarket) null else ApiTimeInForce.GTC, clientOrderId = cid,
                            ),
                        )
                    }
                    toast.show("计划委托已提交", ToastCenter.Kind.SUCCESS)
                    triggerText = ""
                } else {
                    val o = apiCall {
                        api.placeSpotOrder(
                            ApiPlaceOrderRequest(
                                symbol = symbol, side = if (isBuy) ApiOrderSide.BUY else ApiOrderSide.SELL,
                                type = if (kind == SpotOrderKind.MARKET) ApiOrderType.MARKET else ApiOrderType.LIMIT,
                                price = if (kind == SpotOrderKind.MARKET) null else fmt(price, pricePrec),
                                quantity = baseQty, quoteOrderQty = quoteQty,
                                timeInForce = if (kind == SpotOrderKind.MARKET) null else ApiTimeInForce.GTC,
                                postOnly = if (kind == SpotOrderKind.MAKER) true else null, clientOrderId = cid,
                            ),
                        )
                    }
                    toast.show(if (o.status == ApiOrderStatus.FILLED) "委托已成交" else "下单成功", ToastCenter.Kind.SUCCESS)
                    if (tpsl) placeTpsl(Dec.parse(o.quantity).takeIf { it.signum() > 0 } ?: Dec.parse(o.filledQuantity))
                }
                qtyText = ""; amountText = ""; percent = 0f
                refreshAccount()
                onDone(true)
            } catch (e: Throwable) {
                toast.error(e); onDone(false)
            } finally {
                placing = false
            }
        }
    }

    /** 附带止盈止损：以反向市价条件单提交（take_profit / stop）。 */
    private suspend fun placeTpsl(q: BigDecimal) {
        if (q.signum() <= 0) return
        val side = if (isBuy) ApiOrderSide.SELL else ApiOrderSide.BUY
        val fails = ArrayList<String>()
        for ((text, k) in listOf(tpText to ApiConditionalOrderRequest.Kind.TAKE_PROFIT, slText to ApiConditionalOrderRequest.Kind.STOP)) {
            if (Dec.parse(text).signum() <= 0) continue
            try {
                apiCall {
                    api.createConditional(
                        ApiConditionalOrderRequest(
                            symbol = symbol, side = side, type = ApiOrderType.MARKET, triggerPrice = fmt(Dec.parse(text), pricePrec),
                            kind = k, quantity = fmt(q, qtyPrec), clientOrderId = "and-" + UUID.randomUUID().toString().take(12),
                        ),
                    )
                }
            } catch (e: Throwable) {
                fails += (if (k == ApiConditionalOrderRequest.Kind.STOP) "止损" else "止盈") + "：" + e.message
            }
        }
        if (fails.isEmpty()) toast.show("止盈止损条件单已提交", ToastCenter.Kind.SUCCESS) else toast.show(fails.joinToString("；"), ToastCenter.Kind.ERROR)
        tpText = ""; slText = ""
    }

    fun cancel(o: ApiOrder) {
        viewModelScope.launch {
            try { apiCall { api.cancelSpotOrder(o.orderId) }; toast.show("撤单成功", ToastCenter.Kind.SUCCESS); refreshAccount() } catch (e: Throwable) { toast.error(e) }
        }
    }

    fun cancelConditional(o: ApiConditionalOrder) {
        val id = o.id ?: return
        viewModelScope.launch {
            try { apiCall { api.cancelConditional(id) }; toast.show("已撤销条件单", ToastCenter.Kind.SUCCESS); refreshAccount() } catch (e: Throwable) { toast.error(e) }
        }
    }

    fun cancelAll() {
        val list = shownOrders
        if (list.isEmpty()) return
        viewModelScope.launch {
            var fails = 0
            list.map { o -> async { runCatching { apiCall { api.cancelSpotOrder(o.orderId) } }.isFailure } }.forEach { if (it.await()) fails++ }
            toast.show(
                if (fails == 0) "已全部撤销" else "${list.size - fails} 笔撤销成功，$fails 笔失败",
                if (fails == 0) ToastCenter.Kind.SUCCESS else ToastCenter.Kind.ERROR,
            )
            refreshAccount()
        }
    }

    /** 截图 / 单测：直接注入行情状态，不触发网络。 */
    internal fun seedForShot(meta: ApiSymbol, book: OrderBook, last: BigDecimal, changePct: BigDecimal) {
        this.meta = meta; this.book = book; this.last = last; this.changePct = changePct
        priceText = fmt(last, meta.pricePrecision)
    }

    override fun onCleared() { stop() }

    companion object { val FALLBACK_FEE = BigDecimal("0.001") }
}
