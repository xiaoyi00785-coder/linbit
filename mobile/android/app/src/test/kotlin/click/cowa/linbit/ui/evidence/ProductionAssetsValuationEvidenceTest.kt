package click.cowa.linbit.ui.evidence

import click.cowa.linbit.core.storage.DisplayPrefs
import click.cowa.linbit.core.storage.QuoteDisplay
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.math.BigDecimal

/**
 * 资产 ≈ / 估值单位：用生产已部署的 fiat-rates?base=USDT，不依赖未部署的 /market/usd-rate；
 * 不把 USDT 本地硬锚定为 USD。估值单位偏好持久化。
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ProductionAssetsValuationEvidenceTest {
    @After fun reset() {
        QuoteDisplay.currency = "USD"
        QuoteDisplay.ratesBase = "USD"
        QuoteDisplay.rates = emptyMap()
        QuoteDisplay.usdPerUsdt = null
        DisplayPrefs.setValuationAsset(RuntimeEnvironment.getApplication(), "USDT")
    }

    @Test fun fiatApproxUsesUsdtBaseRatesWithoutUsdEndpoint() {
        QuoteDisplay.ratesBase = "USDT"
        QuoteDisplay.currency = "CNY"
        QuoteDisplay.usdPerUsdt = null
        QuoteDisplay.rates = mapOf("CNY" to BigDecimal("6.68"), "USD" to BigDecimal.ONE)
        val text = QuoteDisplay.format(BigDecimal("6100"), 2)
        assertEquals("¥40,748.00", text)
        assertFalse(text.endsWith("--"))
    }

    @Test fun valuationPersistAcrossSet() {
        val ctx = RuntimeEnvironment.getApplication()
        DisplayPrefs.setValuationAsset(ctx, "BTC")
        assertEquals("BTC", DisplayPrefs.valuationAsset)
        DisplayPrefs.setValuationAsset(ctx, "ETH")
        assertEquals("ETH", DisplayPrefs.valuationAsset)
        DisplayPrefs.setValuationAsset(ctx, "USDT")
        assertEquals("USDT", DisplayPrefs.valuationAsset)
        QuoteDisplay.ratesBase = "USDT"
        QuoteDisplay.currency = "CNY"
        QuoteDisplay.rates = mapOf("CNY" to BigDecimal("6.68"))
        assertTrue(QuoteDisplay.format(BigDecimal("6100")).startsWith("¥"))
    }
}
