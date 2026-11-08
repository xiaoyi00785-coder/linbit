package click.cowa.linbit.core.storage

import click.cowa.linbit.core.generated.ApiTicker
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.math.BigDecimal

class UsdContractTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test fun restTickerUsdParsed() {
        val t = json.decodeFromString(ApiTicker.serializer(), """{"symbol":"BTCUSDT","lastPrice":"100","priceChangePercent":"0","high":"1","low":"1","volume":"0","quoteVolume":"0","usd":{"available":true,"price":"99.9","rate":"0.999","stale":false}}""")
        assertEquals("99.9", t.usd?.price)
        val old = json.decodeFromString(ApiTicker.serializer(), """{"symbol":"BTCUSDT","lastPrice":"100","priceChangePercent":"0","high":"1","low":"1","volume":"0","quoteVolume":"0"}""")
        assertNull(old.usd)
    }

    @Test fun formatPrefersBackendUsdAndNeverAssumesPeg() {
        QuoteDisplay.currency = "USD"; QuoteDisplay.usdPerUsdt = null
        assertEquals("$--", QuoteDisplay.formatQuote(BigDecimal("100"), null, 2))
        assertEquals("$99.90", QuoteDisplay.formatQuote(BigDecimal("100"), BigDecimal("99.9"), 2))
    }
}
