package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.storage.QuoteDisplay
import org.junit.After
import org.junit.Assert.*
import org.junit.Test
import java.math.BigDecimal

class QuoteDisplayTest {
    @After fun reset() { QuoteDisplay.currency = "USD"; QuoteDisplay.rates = emptyMap() }
    @Test fun dollarDefaultDoesNotRequireChineseExchangeRate() {
        QuoteDisplay.currency = "USD"; QuoteDisplay.rates = emptyMap()
        assertEquals("$76,000.12", QuoteDisplay.format(BigDecimal("76000.12"), 2))
    }
    @Test fun missingNonDollarRateNeverFabricatesAConversion() {
        QuoteDisplay.currency = "EUR"; QuoteDisplay.rates = emptyMap()
        assertEquals("€--", QuoteDisplay.format(BigDecimal.TEN))
    }
    @Test fun selectedCurrencyUsesActualRate() {
        QuoteDisplay.currency = "CNY"; QuoteDisplay.rates = mapOf("CNY" to BigDecimal("7.10"))
        assertEquals("¥71.00", QuoteDisplay.format(BigDecimal.TEN))
    }
}
