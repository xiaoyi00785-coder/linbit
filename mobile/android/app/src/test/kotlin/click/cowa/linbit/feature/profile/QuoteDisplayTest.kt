package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.storage.QuoteDisplay
import org.junit.After
import org.junit.Assert.*
import org.junit.Test
import java.math.BigDecimal

class QuoteDisplayTest {
    @After fun reset() { QuoteDisplay.currency = "USD"; QuoteDisplay.rates = emptyMap(); QuoteDisplay.usdPerUsdt = null }
    @Test fun dollarUsesUsdtUsdRate() {
        QuoteDisplay.currency = "USD"; QuoteDisplay.usdPerUsdt = BigDecimal("0.9995")
        assertEquals("$75,962.12", QuoteDisplay.format(BigDecimal("76000.12"), 2))
    }
    @Test fun dollarWithoutRateNeverAssumesPeg() {
        QuoteDisplay.currency = "USD"; QuoteDisplay.usdPerUsdt = null
        assertEquals("$--", QuoteDisplay.format(BigDecimal.TEN))
    }
    @Test fun missingNonDollarRateNeverFabricatesAConversion() {
        QuoteDisplay.currency = "EUR"; QuoteDisplay.usdPerUsdt = BigDecimal.ONE; QuoteDisplay.rates = emptyMap()
        assertEquals("€--", QuoteDisplay.format(BigDecimal.TEN))
    }
    @Test fun fiatIsPerUsdTimesUsdtUsd() {
        QuoteDisplay.currency = "CNY"; QuoteDisplay.rates = mapOf("CNY" to BigDecimal("7.10")); QuoteDisplay.usdPerUsdt = BigDecimal("1.0")
        assertEquals("¥71.00", QuoteDisplay.format(BigDecimal.TEN))
    }
}
