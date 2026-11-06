package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiFuturesOrder

/** 合约历史筛选 / 分页辅助（纯函数，单测覆盖）。 */
object TradeHistoryLogic {
    val timeFilterLabels = listOf("近 7 天", "近 30 天", "近 90 天", "全部")
    val orderTypeLabels = listOf("全部", "限价单", "市价单")
    val billKindOptions: List<Pair<String?, String>> = listOf(
        null to "全部",
        "transfer_in" to "转入",
        "transfer_out" to "转出",
        "fee" to "手续费",
        "realized_pnl" to "已实现盈亏",
        "funding" to "资金费用",
        "liquidation_fee" to "强平费",
    )

    /** timeFilter: 0=7d 1=30d 2=90d 3=全部；返回平仓时间下限毫秒，全部为 null。 */
    fun positionStartTimeMs(timeFilter: Int, nowMs: Long = System.currentTimeMillis()): Long? {
        val days = when (timeFilter) {
            0 -> 7; 1 -> 30; 2 -> 90; else -> return null
        }
        return nowMs - days * 86_400_000L
    }

    fun isOpenStatus(status: String?): Boolean {
        val s = status?.lowercase() ?: return false
        return s == "new" || s == "open" || s == "partially_filled" || s == "partial"
    }

    fun matchesOrderType(order: ApiFuturesOrder, typeFilter: Int): Boolean = when (typeFilter) {
        1 -> order.type.equals("limit", true) || order.type.equals("post_only", true)
        2 -> order.type.equals("market", true)
        else -> true
    }

    fun filterOrders(orders: List<ApiFuturesOrder>, typeFilter: Int): List<ApiFuturesOrder> =
        orders.filter { matchesOrderType(it, typeFilter) }

    fun algoStatusParam(history: Boolean): String = if (history) "history" else "active"
}
