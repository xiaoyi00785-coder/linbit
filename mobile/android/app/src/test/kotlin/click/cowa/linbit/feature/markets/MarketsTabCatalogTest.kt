package click.cowa.linbit.feature.markets

import click.cowa.linbit.core.generated.ApiMarketCategory
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.MarketState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigDecimal

class MarketsTabCatalogTest {
    private fun row(sym: String, kind: MarketKind, vol: String = "1") = MarketRow(
        symbol = sym,
        base = sym.removeSuffix("USDT"),
        quote = "USDT",
        last = BigDecimal.ONE,
        changePct = BigDecimal.ZERO,
        quoteVolume = BigDecimal(vol),
        precision = 2,
        kind = kind,
    )

    private val spot = listOf(
        row("BTCUSDT", MarketKind.SPOT),
        row("ETHUSDT", MarketKind.SPOT),
        row("AAPLUSDT", MarketKind.SPOT),
        row("TSLAUSDT", MarketKind.SPOT),
    )
    private val futures = listOf(
        row("BTCUSDT", MarketKind.FUTURES),
        row("DOGEUSDT", MarketKind.FUTURES),
        row("XRPUSDT", MarketKind.FUTURES),
        row("WTIUSDT", MarketKind.FUTURES),
    )
    private val tradFi = setOf("AAPLUSDT", "TSLAUSDT", "WTIUSDT", "XAGUSDT")

    @Test fun spotAndFuturesListsAreDistinct() {
        val spotSyms = spot.map { it.symbol }.toSet()
        val futSyms = futures.map { it.symbol }.toSet()
        assertTrue("futures-only DOGE must not be in spot", "DOGEUSDT" in futSyms && "DOGEUSDT" !in spotSyms)
        assertTrue("spot-only AAPL must not be in futures", "AAPLUSDT" in spotSyms && "AAPLUSDT" !in futSyms)
        assertFalse(spotSyms == futSyms)
    }

    @Test fun watchEmptyNeverReturnsFullMarket() {
        val rows = MarketsTabCatalog.watchRows(emptyList(), spot, futures, tradFi, MarketsTabCatalog.WatchSubTab.ALL)
        assertTrue(rows.isEmpty())
    }

    @Test fun watchFiltersBySubTab() {
        val watch = listOf("BTCUSDT", "AAPLUSDT", "DOGEUSDT", "WTIUSDT")
        val all = MarketsTabCatalog.watchRows(watch, spot, futures, tradFi, MarketsTabCatalog.WatchSubTab.ALL)
        val sp = MarketsTabCatalog.watchRows(watch, spot, futures, tradFi, MarketsTabCatalog.WatchSubTab.SPOT)
        val fu = MarketsTabCatalog.watchRows(watch, spot, futures, tradFi, MarketsTabCatalog.WatchSubTab.FUTURES)
        val tf = MarketsTabCatalog.watchRows(watch, spot, futures, tradFi, MarketsTabCatalog.WatchSubTab.TRADFI)
        assertEquals(setOf("BTCUSDT", "AAPLUSDT", "DOGEUSDT", "WTIUSDT"), all.map { it.symbol }.toSet())
        assertEquals(setOf("BTCUSDT", "AAPLUSDT"), sp.map { it.symbol }.toSet())
        assertTrue(sp.all { it.kind == MarketKind.SPOT })
        assertEquals(setOf("BTCUSDT", "DOGEUSDT", "WTIUSDT"), fu.map { it.symbol }.toSet())
        assertTrue(fu.all { it.kind == MarketKind.FUTURES })
        assertEquals(setOf("AAPLUSDT", "WTIUSDT"), tf.map { it.symbol }.toSet())
    }

    @Test fun recommendDiffersBySubTab() {
        val all = MarketsTabCatalog.recommendSymbols(spot, futures, tradFi.toList(), MarketsTabCatalog.WatchSubTab.ALL)
        val sp = MarketsTabCatalog.recommendSymbols(spot, futures, tradFi.toList(), MarketsTabCatalog.WatchSubTab.SPOT)
        val fu = MarketsTabCatalog.recommendSymbols(spot, futures, tradFi.toList(), MarketsTabCatalog.WatchSubTab.FUTURES)
        val tf = MarketsTabCatalog.recommendSymbols(spot, futures, tradFi.toList(), MarketsTabCatalog.WatchSubTab.TRADFI)
        assertTrue(sp.all { it in spot.map { r -> r.symbol } })
        assertTrue(fu.all { it in futures.map { r -> r.symbol } })
        assertEquals(tradFi.toList().sorted().take(8), tf)
        assertFalse(sp == fu)
        assertFalse(sp.toSet() == tf.toSet())
    }

    @Test fun marketStateFilteredUsesKindSpecificCategories() {
        val spotCats = listOf(ApiMarketCategory(id = "stocks", name = "股票", symbols = listOf("AAPLUSDT", "TSLAUSDT")))
        val futCats = listOf(ApiMarketCategory(id = "commodities", name = "大宗商品", symbols = listOf("WTIUSDT")))
        val state = MarketState(
            spot = spot,
            futures = futures,
            spotCategories = spotCats,
            futuresCategories = futCats,
            categories = spotCats,
        )
        assertEquals(listOf("AAPLUSDT", "TSLAUSDT"), state.filtered(MarketKind.SPOT, 1).map { it.symbol })
        assertEquals(listOf("WTIUSDT"), state.filtered(MarketKind.FUTURES, 1).map { it.symbol })
        assertEquals(spot.map { it.symbol }, state.filtered(MarketKind.SPOT, 0).map { it.symbol })
        assertEquals(futures.map { it.symbol }, state.filtered(MarketKind.FUTURES, 0).map { it.symbol })
    }
}
