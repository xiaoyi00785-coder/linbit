package click.cowa.linbit.core.storage

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import click.cowa.linbit.core.util.Fmt
import java.math.BigDecimal

/** GET /market/usd-rate：rate = 1 USDT 兑多少 USD；available=false 时不回退为 1。 */
@kotlinx.serialization.Serializable
data class ApiUsdRate(val pair: String? = null, val available: Boolean = false, val rate: String? = null, val source: String? = null, val updatedAt: String? = null, val stale: Boolean = false)

/**
 * 折合价：USD 用 /market/usd-rate 的 USDTUSD；其它法币 = rates[法币]（每 1 USD）× USDTUSD。
 * 任一汇率缺失时显示 "--"，绝不把 USDT 当 1:1 USD。
 */
object QuoteDisplay {
    var currency by mutableStateOf("USD")
    /** /market/fiat-rates?base=USD 的 rates：每 1 USD 的法币数量。 */
    var rates by mutableStateOf<Map<String, BigDecimal>>(emptyMap())
    var usdPerUsdt by mutableStateOf<BigDecimal?>(null)
    var usdStale by mutableStateOf(false)
    fun perUsdt(cur: String = currency): BigDecimal? {
        val usd = usdPerUsdt ?: return null
        return if (cur == "USD") usd else rates[cur]?.multiply(usd)
    }
    /** 行情行：优先用后端 usd.price（USD 直接显示；其它法币 × rates），否则按汇率折算。 */
    fun formatQuote(usdt: BigDecimal, usdPrice: BigDecimal?, precision: Int? = 2): String {
        if (usdPrice == null) return format(usdt, precision)
        val v = if (currency == "USD") usdPrice else rates[currency]?.multiply(usdPrice) ?: return format(usdt, precision)
        val prefix = when (currency) { "USD" -> "$"; "CNY" -> "¥"; "EUR" -> "€"; else -> "$currency " }
        return prefix + if (precision == null) Fmt.priceAuto(v) else Fmt.price(v, precision)
    }
    fun format(usdt: BigDecimal, precision: Int? = 2, @Suppress("UNUSED_PARAMETER") cnyFallback: BigDecimal? = null): String {
        val rate = perUsdt()
        val prefix = when (currency) { "USD" -> "$"; "CNY" -> "¥"; "EUR" -> "€"; else -> "$currency " }
        return prefix + if (rate == null) "--" else if (precision == null) Fmt.priceAuto(usdt.multiply(rate)) else Fmt.price(usdt.multiply(rate), precision)
    }
}
