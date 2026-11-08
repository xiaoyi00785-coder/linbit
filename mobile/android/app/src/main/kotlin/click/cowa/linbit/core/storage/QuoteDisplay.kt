package click.cowa.linbit.core.storage

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import click.cowa.linbit.core.util.Fmt
import java.math.BigDecimal

/** USD reference default for guests; authenticated preference is loaded from /account/preferences. */
object QuoteDisplay {
    var currency by mutableStateOf("USD")
    var rates by mutableStateOf<Map<String, BigDecimal>>(emptyMap())
    fun format(usdt: BigDecimal, precision: Int? = 2, cnyFallback: BigDecimal? = null): String {
        val rate = rates[currency] ?: when (currency) { "USD" -> BigDecimal.ONE; "CNY" -> cnyFallback; else -> null }
        val prefix = when (currency) { "USD" -> "$"; "CNY" -> "¥"; "EUR" -> "€"; else -> "$currency " }
        return prefix + if (rate == null) "--" else if (precision == null) Fmt.priceAuto(usdt.multiply(rate)) else Fmt.price(usdt.multiply(rate), precision)
    }
}
