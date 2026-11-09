package click.cowa.linbit.core.storage

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import click.cowa.linbit.core.util.Fmt
import java.math.BigDecimal

/** GET /market/usd-rate：rate = 1 USDT 兑多少 USD；available=false 时不回退为 1。 */
typealias ApiUsdRate = click.cowa.linbit.core.generated.ApiUsdRate

/**
 * 折合价：
 * - ratesBase=USDT（生产 /market/fiat-rates?base=USDT）：rates[币种] = 每 1 USDT 的法币数量（含 API 下发的 USD），直接乘总资产。
 * - ratesBase=USD：rates[币种] = 每 1 USD 的法币；需先乘 /market/usd-rate 的 USDT→USD。
 * 任一所需汇率缺失时显示 "--"，绝不本地硬锚定 USDT=USD；仅使用接口返回的 rate/rates。
 */
object QuoteDisplay {
    var currency by mutableStateOf("USD")
    /** 与最近一次成功的 fiat-rates.base 对齐：USDT 或 USD。 */
    var ratesBase by mutableStateOf("USD")
    /** fiat-rates.rates：含义取决于 ratesBase。 */
    var rates by mutableStateOf<Map<String, BigDecimal>>(emptyMap())
    var usdPerUsdt by mutableStateOf<BigDecimal?>(null)
    var usdStale by mutableStateOf(false)
    var usdRate by mutableStateOf<ApiUsdRate?>(null)
    /** 最近一次折合失败原因（供 UI 展示/重试，非假值）。 */
    var lastMissingReason by mutableStateOf<String?>(null); private set

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

    /** 1 USDT 兑当前计价货币的数量；缺汇率返回 null。 */
    fun perUsdt(cur: String = currency): BigDecimal? {
        if (ratesBase.equals("USDT", true)) {
            rates[cur]?.let { return it }
            // base=USDT 时若缺当前币种，USD 可用独立 usd-rate（非假锚定）
            if (cur == "USD") return usdPerUsdt
            lastMissingReason = "缺少 fiat-rates[$cur]（base=USDT）"
            return null
        }
        val usd = usdPerUsdt
        if (usd == null) {
            lastMissingReason = "缺少 /market/usd-rate 或 ticker.usd.rate"
            return null
        }
        if (cur == "USD") return usd
        val fx = rates[cur]
        if (fx == null) {
            lastMissingReason = "缺少 fiat-rates[$cur]（base=USD）"
            return null
        }
        return fx.multiply(usd)
    }

    /** 行情行：优先用后端 usd.price（USD 直接显示；其它法币 × rates），否则按汇率折算。 */
    fun formatQuote(usdt: BigDecimal, usdPrice: BigDecimal?, precision: Int? = 2, usd: click.cowa.linbit.core.generated.ApiWsUsd? = null, symbol: String? = null): String {
        if ((usd != null && (!usd.available || usdPrice == null || usd.pegAssumed == true)) || (symbol != null && !symbol.endsWith("USDT"))) return missing()
        if (usdPrice == null) return format(usdt, precision)
        val v = if (currency == "USD") usdPrice else {
            if (ratesBase.equals("USDT", true)) {
                // usdPrice 为 USD；需 USD→法币。rates 为每 USDT 法币时，用 rates[cur]/rates[USD]
                val perUsdtFiat = rates[currency] ?: return format(usdt, precision)
                val perUsdtUsd = rates["USD"] ?: usdPerUsdt ?: return format(usdt, precision)
                if (perUsdtUsd.signum() <= 0) return format(usdt, precision)
                usdPrice.multiply(perUsdtFiat).divide(perUsdtUsd, 12, java.math.RoundingMode.HALF_UP)
            } else {
                rates[currency]?.multiply(usdPrice) ?: return format(usdt, precision)
            }
        }
        return prefix() + if (precision == null) Fmt.priceAuto(v) else Fmt.price(v, precision)
    }

    private fun prefix() = when (currency) { "USD" -> "$"; "CNY" -> "¥"; "EUR" -> "€"; else -> "$currency " }
    private fun missing(): String {
        if (lastMissingReason == null) lastMissingReason = "汇率不可用"
        return prefix() + "--"
    }

    fun format(usdt: BigDecimal, precision: Int? = 2, @Suppress("UNUSED_PARAMETER") cnyFallback: BigDecimal? = null): String {
        val rate = perUsdt()
        val amount = rate?.let { raw ->
            val a = usdt.multiply(raw)
            val scale = usdRate?.precision?.takeIf { it in 0..30 }
            if (currency == "USD" && scale != null && usdRate?.rounding == "half_away_from_zero") {
                a.setScale(scale, java.math.RoundingMode.HALF_UP)
            } else a
        }
        if (amount == null) return missing()
        lastMissingReason = null
        return prefix() + if (precision == null) Fmt.priceAuto(amount) else Fmt.price(amount, precision)
    }
}
