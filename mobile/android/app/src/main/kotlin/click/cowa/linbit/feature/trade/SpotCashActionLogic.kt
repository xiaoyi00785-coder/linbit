package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiConvertPairsPairsInner
import click.cowa.linbit.core.generated.ApiConvertQuote
import click.cowa.linbit.core.generated.ApiGridBot
import click.cowa.linbit.core.generated.ApiGridCreateRequest
import click.cowa.linbit.core.generated.ApiStrategyConfig200Response
import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal

/**
 * 策略停止 / 闪兑报价的纯逻辑（无网络、无资金副作用）。
 * 供 UI 与单测共用，避免旧 quoteId / 单击停网格等资金动作缺口。
 */
object StrategyGridLogic {
    private val knownQuotes = listOf("USDT", "USDC", "USD", "BTC", "ETH", "FDUSD")

    /** 广场项无 quoteAsset 字段时，从 symbol 后缀推断计价币；无法推断则 null。 */
    fun quoteAssetFromSymbol(symbol: String?): String? {
        val s = symbol?.uppercase()?.trim().orEmpty()
        if (s.isEmpty()) return null
        return knownQuotes.firstOrNull { s.endsWith(it) && s.length > it.length }
    }

    fun canStop(status: ApiGridBot.Status?): Boolean = status == ApiGridBot.Status.RUNNING

    fun isStopping(status: ApiGridBot.Status?): Boolean = status == ApiGridBot.Status.STOPPING

    /** 复制投资额校验：正数，且不低于配置最小额（若有）。 */
    fun validateInvestment(raw: String, minInvestment: String?): String? {
        val v = Dec.parse(raw.trim())
        if (v.signum() <= 0) return "请输入投资额"
        val min = Dec.parse(minInvestment)
        if (min.signum() > 0 && v < min) return "不低于最小投资额 $minInvestment"
        return null
    }

    /** Validate both preview and final confirmation with one immutable form. */
    data class GridForm(val symbol: String, val mode: ApiGridCreateRequest.Mode, val lower: String, val upper: String, val grids: Int, val investment: String, val sellBaseOnStop: Boolean)

    fun validateForm(form: GridForm, config: ApiStrategyConfig200Response?): String? = when {
        validateInvestment(form.investment, config?.minInvestment) != null -> validateInvestment(form.investment, config?.minInvestment)
        form.symbol.trim().length !in 6..20 -> "请输入有效交易对"
        Dec.parse(form.lower) <= BigDecimal.ZERO || Dec.parse(form.upper) <= Dec.parse(form.lower) -> "上限价必须大于下限价"
        form.grids < (config?.minGrids ?: 2) || form.grids > (config?.maxGrids ?: 100) -> "网格数不在允许区间"
        else -> null
    }
    data class GridPreview (
        val symbol: String, val mode: ApiGridCreateRequest.Mode, val currentPrice: String?, val levels: List<String>?, val qtyPerGrid: String?,
        val buyOrders: Int?, val sellOrders: Int?, val initialBuyQuote: String?, val profitPerGridMin: String?, val profitPerGridMax: String?, val form: GridForm,
    )
    fun previewMatches(preview: GridPreview?, form: GridForm): Boolean = preview?.form == form

}

object ConvertQuoteLogic {
    data class Form(val from: String, val to: String, val amount: String)

    fun normalizeExpiresAtMs(expiresAt: Long?): Long? {
        val t = expiresAt ?: return null
        return if (t < 1_000_000_000_000L) t * 1000 else t
    }

    fun isExpired(quote: ApiConvertQuote?, nowMs: Long = System.currentTimeMillis()): Boolean {
        val exp = normalizeExpiresAtMs(quote?.expiresAt) ?: return false
        return nowMs >= exp
    }

    /** 仅当报价字段与当前表单一致才允许确认（防旧 quoteId）。 */
    fun matchesForm(quote: ApiConvertQuote?, form: Form): Boolean {
        if (quote == null) return false
        val qFrom = quote.from?.trim().orEmpty()
        val qTo = quote.to?.trim().orEmpty()
        val qAmt = Dec.parse(quote.fromAmount)
        val fAmt = Dec.parse(form.amount)
        return qFrom.equals(form.from.trim(), ignoreCase = true) &&
            qTo.equals(form.to.trim(), ignoreCase = true) &&
            qAmt.signum() > 0 && fAmt.signum() > 0 && qAmt.compareTo(fAmt) == 0
    }

    fun canAccept(quote: ApiConvertQuote?, form: Form, nowMs: Long = System.currentTimeMillis()): Boolean =
        matchesForm(quote, form) && !isExpired(quote, nowMs) && !quote?.quoteId.isNullOrBlank()

    /** 错误码需清空本地报价并提示重取。 */
    fun shouldClearQuoteOnError(code: String?): Boolean =
        code == "QUOTE_EXPIRED" || code == "QUOTE_NOT_FOUND"

    /**
     * 仅当响应仍对应当前 revision 与表单时采纳，避免并发过期/慢响应覆盖新输入。
     */
    fun shouldApplyQuote(
        response: ApiConvertQuote,
        requestRev: Long,
        currentRev: Long,
        form: Form,
    ): Boolean = requestRev == currentRev && matchesForm(response, form)

    fun findPair(
        pairs: List<ApiConvertPairsPairsInner>?,
        from: String,
        to: String,
    ): ApiConvertPairsPairsInner? =
        pairs?.firstOrNull {
            it.from.equals(from, ignoreCase = true) && it.to.equals(to, ignoreCase = true)
        }

    fun isPairPaused(pair: ApiConvertPairsPairsInner?): Boolean = pair?.paused == true

    fun pauseHint(pair: ApiConvertPairsPairsInner?): String? {
        if (pair?.paused != true) return null
        return when (pair.pauseReason) {
            ApiConvertPairsPairsInner.PauseReason.DEPEG -> "该币对因脱锚保护已暂停"
            ApiConvertPairsPairsInner.PauseReason.PRICE_UNAVAILABLE -> "该币对暂无可用价格"
            null -> "该币对已暂停闪兑"
        }
    }

    /** 从 pairs 集合推导可选兑出币（不做 assets 假列表回退）。 */
    fun fromAssets(pairs: List<ApiConvertPairsPairsInner>?): List<String> =
        pairs.orEmpty().mapNotNull { it.from?.uppercase() }.distinct().sorted()

    fun toAssets(pairs: List<ApiConvertPairsPairsInner>?, from: String): List<String> =
        pairs.orEmpty()
            .filter { it.from.equals(from, ignoreCase = true) }
            .mapNotNull { it.to?.uppercase() }
            .distinct()
            .sorted()
}
