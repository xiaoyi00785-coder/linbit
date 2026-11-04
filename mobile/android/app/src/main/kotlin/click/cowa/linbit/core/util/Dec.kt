package click.cowa.linbit.core.util

import java.math.BigDecimal

/**
 * 价格 / 数量 / 金额一律用 BigDecimal（服务端为十进制字符串），禁止转 Double / Float 参与计算。
 * 8 位小数的字符串（合约手续费、realizedPnl、netPnl、fundingFee、账单）不要经过本类格式化：
 * 直接用 [Fmt.exact] 原样显示（保留末尾的 0）。
 */
object Dec {
    /** 解析服务端十进制字符串；null / 空 / 非法 → 0。 */
    fun parse(s: String?): BigDecimal = s?.trim()?.takeIf { it.isNotEmpty() }?.toBigDecimalOrNull() ?: BigDecimal.ZERO

    fun parseOrNull(s: String?): BigDecimal? = s?.trim()?.takeIf { it.isNotEmpty() }?.toBigDecimalOrNull()

    /** 向下截断到 prec 位小数的纯文本（不带千分位，用于提交给服务端 / 回填输入框）。 */
    fun plain(d: BigDecimal, prec: Int): String = d.setScale(prec.coerceAtLeast(0), java.math.RoundingMode.DOWN).toPlainString()

    /** "0.01" → 2；"1" → 0；"0.50" → 1（去掉末尾 0 后的小数位数）。 */
    fun precision(of: String?): Int {
        val d = parseOrNull(of) ?: return 0
        return d.stripTrailingZeros().scale().coerceAtLeast(0)
    }
}
