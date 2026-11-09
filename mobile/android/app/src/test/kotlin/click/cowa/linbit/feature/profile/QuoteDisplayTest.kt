package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.storage.QuoteDisplay
import org.junit.After
import org.junit.Assert.*
import org.junit.Test
import java.math.BigDecimal

class QuoteDisplayTest {
    @After fun reset() {
        QuoteDisplay.currency = "USD"
        QuoteDisplay.rates = emptyMap()
        QuoteDisplay.ratesBase = "USD"
        QuoteDisplay.usdPerUsdt = null
    }
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
    @Test fun usdtBaseFiatDoesNotNeedUsdRateEndpoint() {
        QuoteDisplay.ratesBase = "USDT"
        QuoteDisplay.currency = "CNY"
        QuoteDisplay.usdPerUsdt = null
        QuoteDisplay.rates = mapOf("CNY" to BigDecimal("6.68"), "USD" to BigDecimal.ONE)
        assertEquals("¥66.80", QuoteDisplay.format(BigDecimal.TEN))
    }
    @Test fun usdtBaseUsdUsesApiRateNotLocalPeg() {
        QuoteDisplay.ratesBase = "USDT"
        QuoteDisplay.currency = "USD"
        QuoteDisplay.usdPerUsdt = null
        QuoteDisplay.rates = mapOf("USD" to BigDecimal("0.999"))
        assertEquals("$9.99", QuoteDisplay.format(BigDecimal.TEN))
    }
}
