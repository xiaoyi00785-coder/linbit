package click.cowa.linbit.feature.trade

import org.junit.Assert.*
import org.junit.Test
import java.math.BigDecimal

class SupertrendTest {
    private fun candles(vararg closes: Int) = closes.map { value ->
        val c = BigDecimal(value)
        Candle(c, c + BigDecimal.ONE, c - BigDecimal.ONE, c)
    }
    @Test fun warmupNeverInventsValues() {
        assertTrue(supertrend(emptyList()).isEmpty())
        assertTrue(supertrend(candles(10, 11), 3, 1.0).all { it == null })
        val p = supertrend(candles(10, 11, 12), 3, 1.0)
        assertNull(p[0]); assertNull(p[1]); assertEquals(14.0, p[2]!!.value, 1e-10)
        assertFalse(p[2]!!.bullish)
    }
    @Test fun wilderAtrAndBothReversalsMatchHandCalculation() {
        // TR=2,2,2,5,8; ATR=2,3,14/3 at indexes 2,3,4.
        val p = supertrend(candles(10, 11, 12, 16, 9), 3, 1.0)
        assertEquals(14.0, p[2]!!.value, 1e-10)
        assertTrue(p[3]!!.bullish); assertEquals(13.0, p[3]!!.value, 1e-10)
        assertFalse(p[4]!!.bullish); assertEquals(9 + 14.0/3, p[4]!!.value, 1e-10)
    }
    @Test fun flatAndSinglePeriodRemainFinite() {
        val c = BigDecimal.TEN
        val flat = List(20) { Candle(c, c, c, c) }
        assertTrue(supertrend(flat).filterNotNull().all { it.value == 10.0 })
        assertEquals(10.0, supertrend(flat.take(1), 1)[0]!!.value, 0.0)
    }
    @Test fun doesNotLookAheadAndIsTranslationInvariant() {
        val c = candles(10,11,12,16,9,8,11,18,14,7,16)
        val all = supertrend(c, 3, 1.0)
        assertEquals(all.take(6), supertrend(c.take(6), 3, 1.0))
        val shifted = c.map { it.copy(open=it.open+BigDecimal(100), high=it.high+BigDecimal(100), low=it.low+BigDecimal(100), close=it.close+BigDecimal(100)) }
        supertrend(shifted,3,1.0).forEachIndexed { i, p -> if(p != null) {
            assertEquals(all[i]!!.value+100,p.value,1e-9); assertEquals(all[i]!!.bullish,p.bullish)
        } }
    }
    @Test fun malformedUpstreamHasNoIndicatorRatherThanCrashing() {
        val c = candles(10,11,12).toMutableList(); c[1] = c[1].copy(high=BigDecimal.ZERO)
        assertTrue(supertrend(c,1).all { it == null })
    }
    @Test(expected = IllegalArgumentException::class) fun rejectsInvalidPeriod() { supertrend(candles(10),0) }
    @Test(expected = IllegalArgumentException::class) fun rejectsNonfiniteMultiplier() { supertrend(candles(10),1,Double.NaN) }
}
