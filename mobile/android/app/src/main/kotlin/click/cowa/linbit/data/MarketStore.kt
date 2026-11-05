package click.cowa.linbit.data

import click.cowa.linbit.core.generated.ApiMarketCategory
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.network.LinbitApi
import click.cowa.linbit.core.network.OneOfResponses
import click.cowa.linbit.core.network.apiCall
import click.cowa.linbit.core.realtime.MarketSocket
import click.cowa.linbit.core.realtime.WsMessageParser
import click.cowa.linbit.core.util.Dec
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.math.BigDecimal

data class MarketState(
    val spot: List<MarketRow> = emptyList(),
    val futures: List<MarketRow> = emptyList(),
    val categories: List<ApiMarketCategory> = emptyList(),
    /** USDT→CNY 汇率（/market/fiat-rates）；取不到时为 null，界面不显示折合金额（不使用硬编码汇率）。 */
    val cnyRate: BigDecimal? = null,
    val loading: Boolean = false,
    val error: String? = null,
) {
    fun rows(kind: MarketKind) = if (kind == MarketKind.SPOT) spot else futures

    fun filtered(kind: MarketKind, categoryIndex: Int): List<MarketRow> {
        val all = rows(kind)
        if (categoryIndex <= 0 || categoryIndex - 1 >= categories.size) return all
        val set = categories[categoryIndex - 1].symbols.toSet()
        return all.filter { it.symbol in set }
    }
}

/**
 * 行情数据中心（对应 iOS MarketStore）：REST 拉全量 + WS ticker 增量，首页与行情页共用。
 */
class MarketStore(
    private val api: LinbitApi,
    private val socket: MarketSocket,
    private val scope: CoroutineScope,
) {
    private val _state = MutableStateFlow(MarketState())
    val state: StateFlow<MarketState> = _state.asStateFlow()

    private var symbolMeta: Map<String, ApiSymbol> = emptyMap()
    private var refPrice: Map<String, BigDecimal> = emptyMap()
    private var wsJob: Job? = null

    fun meta(symbol: String): ApiSymbol? = symbolMeta[symbol]

    suspend fun load() {
        _state.update { it.copy(loading = true) }
        val r = coroutineScope {
            val symbols = async { runCatching { apiCall { api.symbols() } }.getOrNull() }
            val spot = async { runCatching { apiCall { OneOfResponses.tickers(api.ticker()) } }.getOrNull() }
            val fut = async { runCatching { apiCall { OneOfResponses.futuresTickers(api.futuresTicker()) } }.getOrNull() }
            val cats = async { runCatching { apiCall { api.categories() } }.getOrNull() }
            val fx = async { runCatching { apiCall { api.fiatRates() } }.getOrNull() }
            Loaded(symbols.await(), spot.await(), fut.await(), cats.await(), fx.await()?.rates?.get("CNY")?.content)
        }
        symbolMeta = (r.symbols ?: symbolMeta.values.toList()).associateBy { it.symbol }
        // 现货无成交（lastPrice=0）时用同名合约的 标记价 → 指数价 兜底
        r.futures?.let { list ->
            refPrice = list.mapNotNull { t -> t.symbol?.let { it to MarketRow.effectivePrice(t.lastPrice, t.markPrice, t.indexPrice) } }.toMap()
        }
        _state.update { cur ->
            var spot = cur.spot
            var futures = cur.futures
            r.spot?.let { list ->
                spot = list.map { t ->
                    val row = MarketRow.spot(t, symbolMeta[t.symbol])
                    if (row.last.signum() == 0) row.copy(last = refPrice[row.symbol] ?: BigDecimal.ZERO) else row
                }
            }
            r.futures?.let { list ->
                val spotMap = spot.associate { it.symbol to it.last }
                futures = list.mapNotNull { MarketRow.futures(it) }.map { row ->
                    if (row.last.signum() == 0) row.copy(last = spotMap[row.symbol] ?: BigDecimal.ZERO) else row
                }
            }
            val rate = Dec.parseOrNull(r.cnyRate)?.takeIf { it.signum() > 0 }
            cur.copy(
                spot = spot, futures = futures, categories = r.categories ?: cur.categories,
                cnyRate = rate ?: cur.cnyRate, loading = false,
                error = if (r.spot == null && r.futures == null) "行情加载失败，请检查网络" else null,
            )
        }
        subscribeTickers()
    }

    private class Loaded(
        val symbols: List<ApiSymbol>?,
        val spot: List<click.cowa.linbit.core.generated.ApiTicker>?,
        val futures: List<click.cowa.linbit.core.generated.ApiFuturesTicker>?,
        val categories: List<ApiMarketCategory>?,
        val cnyRate: String?,
    )

    /** 订阅所有现货 ticker:SYMBOL 与合约 futures-ticker:SYMBOL，一个收集器处理全部帧。 */
    private fun subscribeTickers() {
        wsJob?.cancel()
        val s = _state.value
        val channels = s.spot.map { "ticker:${it.symbol}" } + s.futures.map { "futures-ticker:${it.symbol}" }
        if (channels.isEmpty()) return
        wsJob = scope.launch {
            socket.subscribe(channels).collect { frame ->
                when {
                    frame.channel.startsWith("ticker:") -> WsMessageParser.ticker(frame)?.let { t ->
                        _state.update { cur ->
                            val i = cur.spot.indexOfFirst { it.symbol == t.symbol }
                            if (i < 0) cur else {
                                val old = cur.spot[i]
                                val v = Dec.parse(t.lastPrice)
                                val row = old.copy(
                                    last = if (v.signum() > 0) v else (refPrice[t.symbol] ?: old.last),
                                    changePct = Dec.parse(t.priceChangePercent),
                                    quoteVolume = t.quoteVolume?.let { Dec.parse(it) } ?: old.quoteVolume,
                                )
                                cur.copy(spot = cur.spot.toMutableList().also { it[i] = row })
                            }
                        }
                    }
                    frame.channel.startsWith("futures-ticker:") -> WsMessageParser.futuresTicker(frame)?.let { t ->
                        _state.update { cur ->
                            val i = cur.futures.indexOfFirst { it.symbol == t.symbol }
                            if (i < 0) cur else cur.copy(futures = cur.futures.toMutableList().also { it[i] = MarketRow.futures(t, cur.futures[i]) })
                        }
                    }
                }
            }
        }
    }
}
