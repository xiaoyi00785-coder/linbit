package click.cowa.linbit.feature.markets

import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow

/**
 * 行情「自选 / 现货 / 合约 / TradFi」列表分流（纯函数，便于单测）。
 *
 * 数据来源约定：
 * - 现货：GET /market/ticker（+ /market/categories?market=spot）
 * - 合约：GET /futures/market/ticker（+ /market/categories?market=futures）
 * - TradFi：GET /tradfi/assets（无数据则空态，不做假列表）
 * - 自选：仅本地 Watchlist ∩ 上述集合；空则推荐添加，绝不回退成全市场。
 */
object MarketsTabCatalog {
    enum class WatchSubTab { ALL, SPOT, FUTURES, TRADFI }

    fun watchSubTab(index: Int): WatchSubTab = when (index) {
        1 -> WatchSubTab.SPOT
        2 -> WatchSubTab.FUTURES
        3 -> WatchSubTab.TRADFI
        else -> WatchSubTab.ALL
    }

    /** 自选列表行：只含已加自选的交易对，并按子 tab 分流。 */
    fun watchRows(
        watch: List<String>,
        spot: List<MarketRow>,
        futures: List<MarketRow>,
        tradFiSymbols: Set<String>,
        sub: WatchSubTab,
    ): List<MarketRow> {
        if (watch.isEmpty()) return emptyList()
        val watched = watch.toSet()
        val spotMap = spot.associateBy { it.symbol }
        val futMap = futures.associateBy { it.symbol }
        return watch.mapNotNull { sym ->
            if (sym !in watched) return@mapNotNull null
            when (sub) {
                WatchSubTab.ALL -> spotMap[sym] ?: futMap[sym]
                WatchSubTab.SPOT -> spotMap[sym]
                WatchSubTab.FUTURES -> futMap[sym]
                WatchSubTab.TRADFI -> {
                    if (sym !in tradFiSymbols) null
                    else spotMap[sym] ?: futMap[sym]?.copy(kind = MarketKind.FUTURES)
                }
            }
        }
    }

    /** 空自选时的推荐交易对（按子 tab；上限 8）。 */
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

    /** 市场 tab 二级分类：现货用 spotCategories，合约用 futuresCategories。 */
    fun categoriesFor(kind: MarketKind, spotCats: List<click.cowa.linbit.core.generated.ApiMarketCategory>, futCats: List<click.cowa.linbit.core.generated.ApiMarketCategory>) =
        if (kind == MarketKind.SPOT) spotCats else futCats
}
