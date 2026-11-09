package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiEarnProduct
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigDecimal

class EarnAprTest {
    @Test fun bonusAprIsTierCeilingNotReplacementWhenAprMaxPresent() {
        val p = ApiEarnProduct(id = "s", kind = ApiEarnProduct.Kind.SIMPLE, apr = "0.01", bonusApr = "0.012", aprMax = "0.012")
        assertEquals(BigDecimal("0.01"), EarnApr.base(p))
        assertEquals(0, EarnApr.ceiling(p).compareTo(BigDecimal("0.012")))
        assertTrue(EarnApr.hasTier(p))
    }

    @Test fun neverTreatBonusAloneAsOnlyAprWhenEqualBase() {
        val p = ApiEarnProduct(id = "s", kind = ApiEarnProduct.Kind.SIMPLE, apr = "0.05", bonusApr = "0.05")
        assertEquals(0, EarnApr.base(p).compareTo(EarnApr.ceiling(p)))
        assertFalse(EarnApr.hasTier(p))
    }

    @Test fun fixedUsesAprOnly() {
        val p = ApiEarnProduct(id = "f", kind = ApiEarnProduct.Kind.FIXED, apr = "0.087", termDays = 14)
        assertEquals(0, EarnApr.ceiling(p).compareTo(BigDecimal("0.087")))
    }

    @Test fun catalogueSkipsUnknownKindWithoutDroppingOthers() {
        val raw = """
          {"items":[
            {"id":"a","kind":"simple","asset":"USDT","apr":"0.01"},
            {"id":"b","kind":"mystery-future","asset":"XYZ","apr":"0.99"},
            {"id":"c","kind":"onchain","asset":"SOL","apr":"0.064"}
          ]}
        """.trimIndent()
        val el = click.cowa.linbit.core.network.NetworkFactory.json.parseToJsonElement(raw)
        val items = EarnCatalogue.itemsFrom(el)
        assertEquals(listOf("a", "c"), items.map { it.id })
    }
}
