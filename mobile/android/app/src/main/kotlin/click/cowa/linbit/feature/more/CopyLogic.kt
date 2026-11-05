package click.cowa.linbit.feature.more

import click.cowa.linbit.core.generated.ApiCopyFollowRequest
import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal

/** 跟单输入校验与服务端一致；提交前不静默改数值。 */
object CopyLogic {
    data class Input(
        val amount: String, val fixed: Boolean, val ratio: String, val fixedMargin: String,
        val maxPerTrade: String = "", val takeProfit: String = "", val stopLoss: String = "",
    )

    fun error(input: Input, minimum: BigDecimal): String? {
        if (Dec.parse(input.amount) < minimum) return "跟单金额最低 ${minimum.stripTrailingZeros().toPlainString()} USDT"
        if (input.fixed) {
            if (Dec.parse(input.fixedMargin) <= BigDecimal.ZERO) return "请输入有效的单笔保证金"
        } else {
            val r = Dec.parse(input.ratio)
            if (r <= BigDecimal.ZERO || r > BigDecimal.TEN) return "跟单比例需在 0.01 - 10 倍之间"
        }
        if (input.maxPerTrade.isNotBlank() && Dec.parse(input.maxPerTrade) <= BigDecimal.ZERO) return "单笔最大保证金需大于 0"
        if (input.takeProfit.isNotBlank() && Dec.parse(input.takeProfit) <= BigDecimal.ZERO) return "止盈比例需大于 0%"
        val sl = Dec.parse(input.stopLoss)
        if (input.stopLoss.isNotBlank() && (sl <= BigDecimal.ZERO || sl > BigDecimal(100))) return "止损比例需大于 0%，且不超过 100%"
        return null
    }

    fun request(traderId: String, input: Input): ApiCopyFollowRequest = ApiCopyFollowRequest(
        traderId = traderId,
        amount = input.amount,
        mode = if (input.fixed) ApiCopyFollowRequest.Mode.FIXED else ApiCopyFollowRequest.Mode.RATIO,
        fixedMargin = input.fixedMargin.takeIf { input.fixed },
        ratio = input.ratio.takeUnless { input.fixed },
        maxPerTrade = input.maxPerTrade.takeIf { it.isNotBlank() },
        tpRatio = input.takeProfit.takeIf { it.isNotBlank() }?.let { Dec.parse(it).divide(BigDecimal(100)).stripTrailingZeros().toPlainString() },
        slRatio = input.stopLoss.takeIf { it.isNotBlank() }?.let { Dec.parse(it).divide(BigDecimal(100)).stripTrailingZeros().toPlainString() },
    )
}
