package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal

/** 历史与委托列表：金额/价格按 OpenAPI 字符串原样展示，空则占位。 */
fun apiAmount(value: String?, empty: String = "--"): String =
    value?.takeIf { it.isNotBlank() } ?: empty

/** 改单入参校验与格式化（对应 PUT /futures/orders/{id}）。 */
object AmendOrderLogic {
    data class Outcome(val error: String? = null, val price: String? = null, val qty: String? = null)

    fun prepare(
        newPrice: String,
        newQty: String,
        oldPrice: String?,
        oldQty: String?,
        filledQty: String?,
        formatPrice: (BigDecimal) -> String,
        formatQty: (BigDecimal) -> String,
    ): Outcome {
        val np = Dec.parse(newPrice)
        val nq = Dec.parse(newQty)
        val op = Dec.parse(oldPrice)
        val oq = Dec.parse(oldQty)
        val filled = Dec.parse(filledQty)
        return when {
            newPrice.isBlank() && newQty.isBlank() -> Outcome(error = "请至少修改价格或数量")
            newPrice.isNotBlank() && np.signum() <= 0 -> Outcome(error = "请输入有效价格")
            newQty.isNotBlank() && nq.signum() <= 0 -> Outcome(error = "请输入有效数量")
            newQty.isNotBlank() && nq < filled -> Outcome(error = "新数量不能小于已成交数量")
            np.compareTo(op) == 0 && nq.compareTo(oq) == 0 -> Outcome(error = "价格与数量均未变化")
            else -> Outcome(
                price = if (newPrice.isNotBlank() && np.compareTo(op) != 0) formatPrice(np) else null,
                qty = if (newQty.isNotBlank() && nq.compareTo(oq) != 0) formatQty(nq) else null,
            )
        }
    }
}

/** 仓位止盈止损触发价相对开仓价方向校验。 */
object TpslLogic {
    /** @return 错误文案；null 表示通过。 */
    fun validate(long: Boolean, entry: BigDecimal, tp: String, sl: String): String? {
        val t = Dec.parse(tp)
        val s = Dec.parse(sl)
        if (tp.isNotBlank() && (if (long) t <= entry else t >= entry)) {
            return "止盈价需${if (long) "高于" else "低于"}开仓价"
        }
        if (sl.isNotBlank() && (if (long) s >= entry else s <= entry)) {
            return "止损价需${if (long) "低于" else "高于"}开仓价"
        }
        return null
    }
}


/** 条件委托改单入参（PUT /futures/plan-orders/{id} 或 /futures/tpsl-orders/{id}）。 */
object AmendAlgoLogic {
    data class Outcome(
        val error: String? = null,
        val triggerPrice: String? = null,
        val price: String? = null,
        val qty: String? = null,
        val orderType: click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest.OrderType? = null,
    )

    /**
     * @param market true 表示改为市价触发后委托（清除 price）
     * 至少修改触发价 / 委托价 / 数量 / 市价其一；与旧值相同的字段省略。
     */
    fun prepare(
        newTrigger: String,
        newPrice: String,
        newQty: String,
        market: Boolean,
        oldTrigger: String?,
        oldPrice: String?,
        oldQty: String?,
        oldMarket: Boolean,
        formatPrice: (java.math.BigDecimal) -> String,
        formatQty: (java.math.BigDecimal) -> String,
    ): Outcome {
        val nt = Dec.parse(newTrigger)
        val np = Dec.parse(newPrice)
        val nq = Dec.parse(newQty)
        val ot = Dec.parse(oldTrigger)
        val op = Dec.parse(oldPrice)
        val oq = Dec.parse(oldQty)
        if (newTrigger.isBlank() && newPrice.isBlank() && newQty.isBlank() && market == oldMarket) {
            return Outcome(error = "请至少修改一项")
        }
        if (newTrigger.isNotBlank() && nt.signum() <= 0) return Outcome(error = "请输入有效触发价")
        if (!market && newPrice.isNotBlank() && np.signum() <= 0) return Outcome(error = "请输入有效委托价")
        if (newQty.isNotBlank() && nq.signum() <= 0) return Outcome(error = "请输入有效数量")
        val triggerOut = if (newTrigger.isNotBlank() && nt.compareTo(ot) != 0) formatPrice(nt) else null
        val orderType = when {
            market && !oldMarket -> click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest.OrderType.MARKET
            !market && oldMarket -> click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest.OrderType.LIMIT
            !market && newPrice.isNotBlank() && np.compareTo(op) != 0 -> click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest.OrderType.LIMIT
            else -> null
        }
        val priceOut = when {
            market -> null
            newPrice.isNotBlank() && np.compareTo(op) != 0 -> formatPrice(np)
            orderType == click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest.OrderType.LIMIT && newPrice.isNotBlank() -> formatPrice(np)
            else -> null
        }
        val qtyOut = if (newQty.isNotBlank() && nq.compareTo(oq) != 0) formatQty(nq) else null
        if (triggerOut == null && priceOut == null && qtyOut == null && orderType == null) {
            return Outcome(error = "内容未变化")
        }
        return Outcome(triggerPrice = triggerOut, price = priceOut, qty = qtyOut, orderType = orderType)
    }
}
