package click.cowa.linbit.feature.markets

import click.cowa.linbit.core.generated.ApiMarketCategory
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow

/**
 * 行情「自选 / 现货 / 合约 / TradFi」列表分流（纯函数，便于单测）。
 *
 * 约定：
 * - 现货 = GET /market/ticker 减去 /tradfi/assets 的全部 symbol（后端若把股票/贵金属混进 spot ticker，客户端剔除并上报）
 * - 合约 = GET /futures/market/ticker 减去 tradfi symbol（含 XAG/WTI 等 TradFi 永续；参考 App 未确认其归属合约，只放 TradFi）
 * - TradFi = GET /tradfi/assets（唯一展示 TradFi 品种的入口）
 * - 自选 = Watchlist ∩ 各自分流后的集合；空则推荐，不回退全市场
 */
object MarketsTabCatalog {
    enum class WatchSubTab { ALL, SPOT, FUTURES, TRADFI }

    data class Partitioned(
        val spot: List<MarketRow>,
        val futures: List<MarketRow>,
        /** 从 spot ticker 剔出的 TradFi 行（供自选·TradFi 展示行情用）。 */
        val tradFiSpotRows: List<MarketRow>,
        /** 从 futures ticker 剔出的 TradFi 行。 */
        val tradFiFuturesRows: List<MarketRow>,
    )

    fun watchSubTab(index: Int): WatchSubTab = when (index) {
        1 -> WatchSubTab.SPOT
        2 -> WatchSubTab.FUTURES
        3 -> WatchSubTab.TRADFI
        else -> WatchSubTab.ALL
    }

    /** 现货/合约列表剔除 TradFi symbol，保证三 tab 无交集。 */
    fun partition(
        spotRaw: List<MarketRow>,
        futuresRaw: List<MarketRow>,
        tradFiSymbols: Set<String>,
    ): Partitioned {
        if (tradFiSymbols.isEmpty()) {
            return Partitioned(spotRaw, futuresRaw, emptyList(), emptyList())
        }
        val spot = spotRaw.filter { it.symbol !in tradFiSymbols }
        val futures = futuresRaw.filter { it.symbol !in tradFiSymbols }
        val tfSpot = spotRaw.filter { it.symbol in tradFiSymbols }
        val tfFut = futuresRaw.filter { it.symbol in tradFiSymbols }
        return Partitioned(spot, futures, tfSpot, tfFut)
    }

    /** 二级分类里也去掉 TradFi symbol；空分类剔除。 */
    fun sanitizeCategories(
        cats: List<ApiMarketCategory>,
        tradFiSymbols: Set<String>,
        allowedSymbols: Set<String>,
    ): List<ApiMarketCategory> = cats.mapNotNull { c ->
        val syms = c.symbols.filter { it in allowedSymbols && it !in tradFiSymbols }
        if (syms.isEmpty()) null else c.copy(symbols = syms)
    }

    fun watchRows(
        watch: List<String>,
        spot: List<MarketRow>,
        futures: List<MarketRow>,
        tradFiSymbols: Set<String>,
        sub: WatchSubTab,
        tradFiSpotRows: List<MarketRow> = emptyList(),
        tradFiFuturesRows: List<MarketRow> = emptyList(),
    ): List<MarketRow> {
        if (watch.isEmpty()) return emptyList()
        val spotMap = spot.associateBy { it.symbol }
        val futMap = futures.associateBy { it.symbol }
        val tfMap = (tradFiSpotRows + tradFiFuturesRows).associateBy { it.symbol }
        return watch.mapNotNull { sym ->
            when (sub) {
                WatchSubTab.ALL -> spotMap[sym] ?: futMap[sym] ?: tfMap[sym]
                WatchSubTab.SPOT -> spotMap[sym]
                WatchSubTab.FUTURES -> futMap[sym]
                WatchSubTab.TRADFI -> if (sym in tradFiSymbols) tfMap[sym] ?: spotMap[sym] ?: futMap[sym] else null
            }
        }
    }

    fun recommendSymbols(
        spot: List<MarketRow>,
        futures: List<MarketRow>,
        tradFiSymbols: List<String>,
        sub: WatchSubTab,
        limit: Int = 8,
    ): List<String> = when (sub) {
        WatchSubTab.ALL -> (futures.take(4).map { it.symbol } + spot.take(4).map { it.symbol }).distinct().take(limit)
        WatchSubTab.SPOT -> spot.take(limit).map { it.symbol }
        WatchSubTab.FUTURES -> futures.take(limit).map { it.symbol }
        WatchSubTab.TRADFI -> tradFiSymbols.take(limit)
    }

    /**
     * TradFi 不得出现在现货/合约列表中。
     * 现货∩合约可共享 BTCUSDT 等（参考 App 两边都有同名交易对，属不同市场）。
     */
    fun tradFiLeaksIntoSpotOrFutures(spot: Collection<String>, futures: Collection<String>, tradFi: Collection<String>): Set<String> {
        val t = tradFi.toSet()
        return (spot.toSet() intersect t) + (futures.toSet() intersect t)
    }

    /** 现货与合约的 symbol 交集（参考允许的同名永续/现货对）。 */
    fun spotFuturesSymbolOverlap(spot: Collection<String>, futures: Collection<String>): Set<String> =
        spot.toSet() intersect futures.toSet()
}

