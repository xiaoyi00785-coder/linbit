package click.cowa.linbit.feature.trade

import click.cowa.linbit.data.paper.PaperBalance
import click.cowa.linbit.data.paper.PaperBalances
import click.cowa.linbit.feature.trade.DemoTradeLogic.AmountUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.math.BigDecimal

class DemoTradeLogicTest {
    private val balances = PaperBalances(
        1, 20, 3,
        listOf(PaperBalance("USDT", "9900.5", "0", "9900.5"), PaperBalance("BTC", "0.0125", "0", "0.0125"), PaperBalance("ETH", "0", "0", "0")),
        "paper", "paper", 2,
    )

    @Test fun orderTypesFollowPaperContractAndDefaultToMarketOnly() {
        assertEquals(setOf("market"), DemoTradeLogic.supportedOrderTypes(null))
        assertEquals(setOf("market"), DemoTradeLogic.supportedOrderTypes(emptyList()))
        assertEquals(setOf("market", "limit"), DemoTradeLogic.supportedOrderTypes(listOf("MARKET", "limit")))
        assertTrue(DemoTradeLogic.kindKey(FuturesKind.MARKET) in DemoTradeLogic.supportedOrderTypes(null))
        listOf(FuturesKind.LIMIT, FuturesKind.PLAN, FuturesKind.POST_ONLY).forEach {
            assertFalse(DemoTradeLogic.kindKey(it) in DemoTradeLogic.supportedOrderTypes(null))
        }
    }

    @Test fun valueUnitIsNotMappedToPaperOrders() {
        assertTrue(DemoTradeLogic.unitSupported(AmountUnit.QUANTITY))
        assertTrue(DemoTradeLogic.unitSupported(AmountUnit.COST))
        assertFalse(DemoTradeLogic.unitSupported(AmountUnit.VALUE))
        assertNull(DemoTradeLogic.draft(true, AmountUnit.VALUE, "100"))
    }

    @Test fun openBuysByCostOrQuantityAndCloseSellsByQuantityOnly() {
        assertEquals(DemoTradeLogic.Draft(true, "100", true), DemoTradeLogic.draft(true, AmountUnit.COST, " 100 "))
        assertEquals(DemoTradeLogic.Draft(true, "0.01", false), DemoTradeLogic.draft(true, AmountUnit.QUANTITY, "0.01"))
        assertEquals(AmountUnit.QUANTITY, DemoTradeLogic.effectiveUnit(false, AmountUnit.COST))
        assertEquals(DemoTradeLogic.Draft(false, "0.01", false), DemoTradeLogic.draft(false, DemoTradeLogic.effectiveUnit(false, AmountUnit.COST), "0.01"))
        assertNull(DemoTradeLogic.draft(true, AmountUnit.COST, "0"))
        assertNull(DemoTradeLogic.draft(true, AmountUnit.COST, "-1"))
        assertNull(DemoTradeLogic.draft(true, AmountUnit.COST, "abc"))
        assertNull(DemoTradeLogic.draft(true, AmountUnit.COST, ""))
    }

    @Test fun balancesAndHoldingsComeOnlyFromPaperBalances() {
        assertEquals(BigDecimal("9900.5"), DemoTradeLogic.free(balances, "USDT"))
        assertEquals(BigDecimal("0.0125"), DemoTradeLogic.free(balances, "btc"))
        assertEquals(BigDecimal.ZERO, DemoTradeLogic.free(null, "USDT"))
        assertEquals(listOf("BTC"), DemoTradeLogic.holdings(balances, "USDT", null).map { it.asset })
        assertEquals(emptyList<String>(), DemoTradeLogic.holdings(balances, "USDT", "ETH").map { it.asset })
        assertEquals(listOf("BTC"), DemoTradeLogic.holdings(balances, "USDT", "BTC").map { it.asset })
    }

    @Test fun maxBuyAndSliderUseRealPaperBalances() {
        val price = BigDecimal("80000")
        assertEquals(BigDecimal("9900.5"), DemoTradeLogic.maxBuy(BigDecimal("9900.5"), price, AmountUnit.COST, 4))
        assertEquals(BigDecimal("0.1237"), DemoTradeLogic.maxBuy(BigDecimal("9900.5"), price, AmountUnit.QUANTITY, 4))
        assertEquals(BigDecimal.ZERO, DemoTradeLogic.maxBuy(BigDecimal("9900.5"), BigDecimal.ZERO, AmountUnit.QUANTITY, 4))
        assertEquals("4950.25", DemoTradeLogic.percentAmount(true, AmountUnit.COST, 0.5f, BigDecimal("9900.5"), BigDecimal("0.0125"), price, 4))
        assertEquals("0.0062", DemoTradeLogic.percentAmount(false, AmountUnit.QUANTITY, 0.5f, BigDecimal("9900.5"), BigDecimal("0.0125"), price, 4))
        assertEquals("", DemoTradeLogic.percentAmount(true, AmountUnit.COST, 0f, BigDecimal("9900.5"), BigDecimal.ZERO, price, 4))
    }
}
