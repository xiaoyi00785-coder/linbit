package click.cowa.linbit.core.util

import org.junit.Assert.assertEquals
import org.junit.Test
import java.math.BigDecimal

class FmtTest {
    private fun d(s: String) = BigDecimal(s)

    @Test fun priceGroupsAndRoundsHalfUp() {
        assertEquals("80,991.90", Fmt.price(d("80991.9"), 2))
        assertEquals("2,349.39", Fmt.price(d("2349.385"), 2))
        assertEquals("0.10083", Fmt.price(d("0.100830"), 5))
        assertEquals("-1,234.50", Fmt.price(d("-1234.5"), 2))
    }

    @Test fun priceAutoPicksPrecisionByMagnitude() {
        assertEquals("80,991.90", Fmt.priceAuto(d("80991.9")))
        assertEquals("101.59", Fmt.priceAuto(d("101.5900")))
        assertEquals("1.2345", Fmt.priceAuto(d("1.2345")))
        assertEquals("0.04791", Fmt.priceAuto(d("0.04791")))
        assertEquals("0.00001234", Fmt.priceAuto(d("0.00001234")))
    }

    @Test fun amountTruncatesNeverRounds() {
        assertEquals("1.99", Fmt.amount(d("1.999"), 2))
        assertEquals("1,000.00000000", Fmt.amount(d("1000"), 8))
    }

    @Test fun percentHasSignAndTwoDecimals() {
        assertEquals("+0.62%", Fmt.percent(d("0.62")))
        assertEquals("-1.13%", Fmt.percent(d("-1.126")))
        assertEquals("+0.00%", Fmt.percent(d("0")))
    }

    @Test fun volumeUsesChineseUnits() {
        assertEquals("12.94亿", Fmt.volumeCN(d("1294000000")))
        assertEquals("6512.29万", Fmt.volumeCN(d("65122900")))
        assertEquals("9998.000", Fmt.volumeCN(d("9998")))
    }

    @Test fun fiatMultipliesExactly() {
        // 80,991.9 × 6.68 = 541,025.892（精确乘法，不经过 Double）
        assertEquals("¥" + Fmt.price(d("80991.9").multiply(d("6.68")), 2), Fmt.fiat(d("80991.9"), d("6.68"), 2))
        assertEquals("¥541,025.89", Fmt.fiat(d("80991.9"), d("6.68"), 2))
    }

    @Test fun exactKeepsServerStringIncludingTrailingZeros() {
        assertEquals("0.00120000", Fmt.exact("0.00120000"))
        assertEquals("--", Fmt.exact(null))
        assertEquals("--", Fmt.exact("  "))
    }

    @Test fun decParseNeverUsesDouble() {
        // 0.1 + 0.2 在 Double 下不等于 0.3；BigDecimal 必须精确
        assertEquals(d("0.3"), Dec.parse("0.1").add(Dec.parse("0.2")))
        assertEquals(BigDecimal.ZERO, Dec.parse(""))
        assertEquals(BigDecimal.ZERO, Dec.parse("abc"))
    }
}
