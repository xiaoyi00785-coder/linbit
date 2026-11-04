package click.cowa.linbit.core.util

import java.math.BigDecimal
import java.math.RoundingMode

/** 数字格式化。全部基于 BigDecimal + 固定的 en_US 风格（千分位 ","、小数点 "."），不依赖设备 Locale。 */
object Fmt {
    private val BIG_10K = BigDecimal("10000")
    private val BIG_100M = BigDecimal("100000000")
    private val BIG_1000 = BigDecimal(1000)
    private val BIG_1 = BigDecimal.ONE
    private val BIG_001 = BigDecimal("0.01")

    /** 服务端字符串原样显示（8 位小数的手续费 / 盈亏 / 账单；末尾的 0 也保留）。空值显示 "--"。 */
    fun exact(s: String?): String = s?.trim()?.takeIf { it.isNotEmpty() } ?: "--"

    /** 在整数部分插入千分位。入参为 toPlainString() 的结果。 */
    internal fun group(plain: String): String {
        val neg = plain.startsWith("-")
        val body = if (neg) plain.substring(1) else plain
        val dot = body.indexOf('.')
        val intPart = if (dot >= 0) body.substring(0, dot) else body
        val frac = if (dot >= 0) body.substring(dot) else ""
        val sb = StringBuilder()
        intPart.forEachIndexed { i, c ->
            if (i > 0 && (intPart.length - i) % 3 == 0) sb.append(',')
            sb.append(c)
        }
        return (if (neg) "-" else "") + sb + frac
    }

    private fun fixed(d: BigDecimal, scale: Int, mode: RoundingMode, grouping: Boolean): String {
        val plain = d.setScale(scale, mode).toPlainString()
        return if (grouping) group(plain) else plain
    }

    /** 按交易对精度显示价格（四舍五入），例如 81,133.25。 */
    fun price(d: BigDecimal, precision: Int): String = fixed(d, precision.coerceAtLeast(0), RoundingMode.HALF_UP, true)

    /** 未知精度时按量级自适应（≥1000 两位；≥1 四位并去掉多余尾零但至少保留 2 位；<1 五位或八位）。 */
    fun priceAuto(d: BigDecimal): String {
        val a = d.abs()
        val p = when {
            a >= BIG_1000 -> 2
            a >= BIG_1 -> 4
            a >= BIG_001 -> 5
            else -> 8
        }
        var s = price(d, p)
        if (a >= BIG_1 && s.contains('.')) {
            while (s.endsWith("0") && s.length - s.indexOf('.') > 3) s = s.dropLast(1)
        }
        return s
    }

    /** 数量 / 金额：固定小数位、向下截断、千分位。 */
    fun amount(d: BigDecimal, prec: Int): String = fixed(d, prec.coerceAtLeast(0), RoundingMode.DOWN, true)

    /** 涨跌幅：+0.66% / -1.13%。入参为百分数（"0.66" 即 0.66%）。 */
    fun percent(d: BigDecimal): String {
        val r = d.setScale(2, RoundingMode.HALF_UP)
        return if (r.signum() >= 0) "+${r.toPlainString()}%" else "${r.toPlainString()}%"
    }

    /** 成交额：万 / 亿，例如 4643.10万、12.94亿；不足一万显示三位小数（对照图「9998.000」）。 */
    fun volumeCN(d: BigDecimal): String = when {
        d >= BIG_100M -> fixed(d.divide(BIG_100M), 2, RoundingMode.HALF_UP, false) + "亿"
        d >= BIG_10K -> fixed(d.divide(BIG_10K), 2, RoundingMode.HALF_UP, false) + "万"
        else -> fixed(d, 3, RoundingMode.HALF_UP, false)
    }

    /** 法币折合：¥541,797.84（小数位与价格一致，对照图 #9）。 */
    fun fiat(usdt: BigDecimal, rate: BigDecimal, precision: Int?): String {
        val v = usdt.multiply(rate)
        return "¥" + if (precision != null) price(v, precision) else priceAuto(v)
    }
}
