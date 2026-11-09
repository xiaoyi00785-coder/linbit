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
    private fun row(sym: String, kind: MarketKind) = MarketRow(
        symbol = sym, base = sym.removeSuffix("USDT"), quote = "USDT",
        last = BigDecimal.ONE, changePct = BigDecimal.ZERO, quoteVolume = BigDecimal.ONE,
        precision = 2, kind = kind,
    )

    private val tradFi = setOf(
        "AAPLUSDT", "AMZNUSDT", "COINUSDT", "GOOGLUSDT", "METAUSDT", "MSFTUSDT",
        "NVDAUSDT", "PAXGUSDT", "SPYUSDT", "TSLAUSDT", "XAUTUSDT", "XAGUSDT", "WTIUSDT",
    )

    private val spotRaw = listOf(
        row("BTCUSDT", MarketKind.SPOT), row("ETHUSDT", MarketKind.SPOT), row("BNBUSDT", MarketKind.SPOT),
        row("SOLUSDT", MarketKind.SPOT), row("TRXUSDT", MarketKind.SPOT),
        row("AAPLUSDT", MarketKind.SPOT), row("AMZNUSDT", MarketKind.SPOT), row("COINUSDT", MarketKind.SPOT),
        row("GOOGLUSDT", MarketKind.SPOT), row("METAUSDT", MarketKind.SPOT), row("MSFTUSDT", MarketKind.SPOT),
        row("NVDAUSDT", MarketKind.SPOT), row("PAXGUSDT", MarketKind.SPOT), row("SPYUSDT", MarketKind.SPOT),
        row("TSLAUSDT", MarketKind.SPOT), row("XAUTUSDT", MarketKind.SPOT),
    )
    private val futRaw = listOf(
        row("BTCUSDT", MarketKind.FUTURES), row("ETHUSDT", MarketKind.FUTURES), row("BNBUSDT", MarketKind.FUTURES),
        row("SOLUSDT", MarketKind.FUTURES), row("XRPUSDT", MarketKind.FUTURES), row("DOGEUSDT", MarketKind.FUTURES),
        row("ARBUSDT", MarketKind.FUTURES), row("TONUSDT", MarketKind.FUTURES), row("LINKUSDT", MarketKind.FUTURES),
        row("AVAXUSDT", MarketKind.FUTURES), row("XAGUSDT", MarketKind.FUTURES), row("WTIUSDT", MarketKind.FUTURES),
    )

    @Test fun partitionRemovesTradFiFromSpotAndFutures() {
        val p = MarketsTabCatalog.partition(spotRaw, futRaw, tradFi)
        assertEquals(
            listOf("BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "TRXUSDT"),
            p.spot.map { it.symbol },
        )
        assertFalse(p.spot.any { it.symbol in tradFi })
        assertFalse(p.futures.any { it.symbol in tradFi })
        assertTrue("XAG must leave futures", "XAGUSDT" !in p.futures.map { it.symbol })
        assertTrue("WTI must leave futures", "WTIUSDT" !in p.futures.map { it.symbol })
        assertTrue(p.tradFiSpotRows.map { it.symbol }.toSet().containsAll(setOf("AAPLUSDT", "PAXGUSDT")))
        assertTrue(p.tradFiFuturesRows.map { it.symbol }.toSet().containsAll(setOf("XAGUSDT", "WTIUSDT")))
        assertEquals(
            emptySet<String>(),
            MarketsTabCatalog.tradFiLeaksIntoSpotOrFutures(
                p.spot.map { it.symbol }, p.futures.map { it.symbol }, tradFi,
            ),
        )
    }

    @Test fun tradFiDoesNotLeakIntoSpotOrFutures() {
        val p = MarketsTabCatalog.partition(spotRaw, futRaw, tradFi)
        assertEquals(
            emptySet<String>(),
            MarketsTabCatalog.tradFiLeaksIntoSpotOrFutures(
                p.spot.map { it.symbol }, p.futures.map { it.symbol }, tradFi,
            ),
        )
        // 参考 App：现货/合约可同名（BTCUSDT 等）；不得与 TradFi 集合交叉
        val cryptoOverlap = MarketsTabCatalog.spotFuturesSymbolOverlap(p.spot.map { it.symbol }, p.futures.map { it.symbol })
        assertTrue(cryptoOverlap.contains("BTCUSDT"))
        assertTrue(cryptoOverlap.none { it in tradFi })
    }

    @Test fun watchEmptyNeverReturnsFullMarket() {
        val p = MarketsTabCatalog.partition(spotRaw, futRaw, tradFi)
        assertTrue(
            MarketsTabCatalog.watchRows(
                emptyList(), p.spot, p.futures, tradFi, MarketsTabCatalog.WatchSubTab.ALL,
                p.tradFiSpotRows, p.tradFiFuturesRows,
            ).isEmpty(),
        )
    }

    @Test fun watchTradFiUsesExcludedRows() {
        val p = MarketsTabCatalog.partition(spotRaw, futRaw, tradFi)
        val rows = MarketsTabCatalog.watchRows(
            listOf("AAPLUSDT", "WTIUSDT", "BTCUSDT"),
            p.spot, p.futures, tradFi, MarketsTabCatalog.WatchSubTab.TRADFI,
            p.tradFiSpotRows, p.tradFiFuturesRows,
        )
        assertEquals(setOf("AAPLUSDT", "WTIUSDT"), rows.map { it.symbol }.toSet())
    }

    @Test fun sanitizeCategoriesDropsTradFiAndEmpty() {
        val cats = listOf(
            ApiMarketCategory("stocks", "股票", listOf("AAPLUSDT", "TSLAUSDT")),
            ApiMarketCategory("layer1", "公链", listOf("BTCUSDT", "ETHUSDT", "AAPLUSDT")),
            ApiMarketCategory("meme", "Meme", emptyList()),
        )
        val allowed = setOf("BTCUSDT", "ETHUSDT")
        val out = MarketsTabCatalog.sanitizeCategories(cats, tradFi, allowed)
        assertEquals(1, out.size)
        assertEquals("layer1", out[0].id)
        assertEquals(listOf("BTCUSDT", "ETHUSDT"), out[0].symbols)
    }

    @Test fun marketStateFilteredUsesSanitizedCats() {
        val p = MarketsTabCatalog.partition(spotRaw, futRaw, tradFi)
        val spotCats = MarketsTabCatalog.sanitizeCategories(
            listOf(ApiMarketCategory("layer1", "公链", listOf("BTCUSDT", "ETHUSDT", "AAPLUSDT"))),
            tradFi, p.spot.map { it.symbol }.toSet(),
        )
        val state = MarketState(spot = p.spot, futures = p.futures, spotCategories = spotCats, categories = spotCats, tradFiSymbols = tradFi)
        assertEquals(listOf("BTCUSDT", "ETHUSDT"), state.filtered(MarketKind.SPOT, 1).map { it.symbol })
        assertTrue(state.filtered(MarketKind.SPOT, 0).none { it.symbol in tradFi })
    }
}
