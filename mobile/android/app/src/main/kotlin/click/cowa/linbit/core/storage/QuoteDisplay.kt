package click.cowa.linbit.core.storage

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import click.cowa.linbit.core.util.Fmt
import java.math.BigDecimal

/** GET /market/usd-rate：rate = 1 USDT 兑多少 USD；available=false 时不回退为 1。 */
typealias ApiUsdRate = click.cowa.linbit.core.generated.ApiUsdRate

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
    var usdRate by mutableStateOf<ApiUsdRate?>(null)
    fun applyRate(value: ApiUsdRate?) {
        usdRate = value
        usdPerUsdt = value?.takeIf { it.available && it.pegAssumed != true }?.rate?.let { validRate(it) }
        usdStale = value?.stale == true
    }
    fun applyTickerRate(value: click.cowa.linbit.core.generated.ApiWsUsd) {
        val valid = value.reason != "rate_unavailable" && value.pegAssumed != true && validRate(value.rate) != null
        applyRate(ApiUsdRate(pair="USDTUSD", available=valid, rate=value.rate, source=value.source,
            updatedAt=value.updatedAt, stale=value.stale, precision=value.precision, rounding=value.rounding, pegAssumed=value.pegAssumed))
    }
    private fun validRate(raw: String?) = raw?.toBigDecimalOrNull()?.takeIf { it.signum() > 0 }
    fun perUsdt(cur: String = currency): BigDecimal? {
        val usd = usdPerUsdt ?: return null
        return if (cur == "USD") usd else rates[cur]?.multiply(usd)
    }
    /** 行情行：优先用后端 usd.price（USD 直接显示；其它法币 × rates），否则按汇率折算。 */
    fun formatQuote(usdt: BigDecimal, usdPrice: BigDecimal?, precision: Int? = 2, usd: click.cowa.linbit.core.generated.ApiWsUsd? = null, symbol: String? = null): String {
        if ((usd != null && (!usd.available || usdPrice == null || usd.pegAssumed == true)) || (symbol != null && !symbol.endsWith("USDT"))) return missing()
        if (usdPrice == null) return format(usdt, precision)
        val v = if (currency == "USD") usdPrice else rates[currency]?.multiply(usdPrice) ?: return format(usdt, precision)
        val prefix = when (currency) { "USD" -> "$"; "CNY" -> "¥"; "EUR" -> "€"; else -> "$currency " }
        return prefix + if (precision == null) Fmt.priceAuto(v) else Fmt.price(v, precision)
    }
    private fun missing() = when(currency) { "USD" -> "$--"; "CNY" -> "¥--"; "EUR" -> "€--"; else -> "$currency --" }
    fun format(usdt: BigDecimal, precision: Int? = 2, @Suppress("UNUSED_PARAMETER") cnyFallback: BigDecimal? = null): String {
        val usd = usdPerUsdt?.let { rate ->
            val amount = usdt.multiply(rate)
            val scale = usdRate?.precision?.takeIf { it in 0..30 }
            if (scale != null && usdRate?.rounding == "half_away_from_zero") amount.setScale(scale, java.math.RoundingMode.HALF_UP) else amount
        }
        val amount = if (currency == "USD") usd else usd?.let { rates[currency]?.multiply(it) }
        val prefix = when (currency) { "USD" -> "$"; "CNY" -> "¥"; "EUR" -> "€"; else -> "$currency " }
        return prefix + if (amount == null) "--" else if (precision == null) Fmt.priceAuto(amount) else Fmt.price(amount, precision)
    }
}
