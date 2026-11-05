package click.cowa.linbit.data.trade

import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal
import java.math.RoundingMode

/** 订单簿档位（价格 / 数量均为 BigDecimal）。 */
data class BookLevel(val price: BigDecimal, val qty: BigDecimal)

/** 订单簿：bids 价格降序，asks 价格升序。 */
data class OrderBook(val bids: List<BookLevel> = emptyList(), val asks: List<BookLevel> = emptyList()) {
    val bestBid: BigDecimal? get() = bids.firstOrNull()?.price
    val bestAsk: BigDecimal? get() = asks.firstOrNull()?.price

    /** 买盘占比（0..1），对照图「B 61% … 39% S」；无数据为 0.5。 */
    val buyRatio: Double
        get() {
            val b = bids.fold(BigDecimal.ZERO) { a, l -> a + l.qty }
            val s = asks.fold(BigDecimal.ZERO) { a, l -> a + l.qty }
            val t = b + s
            return if (t.signum() == 0) 0.5 else b.divide(t, 6, RoundingMode.HALF_UP).toDouble()
        }

    /** 按价格精度聚合：买盘向下取整、卖盘向上取整，同价位数量合并。 */
    fun grouped(step: BigDecimal): OrderBook {
        if (step.signum() <= 0) return this
        fun agg(list: List<BookLevel>, mode: RoundingMode): List<BookLevel> {
            val m = LinkedHashMap<BigDecimal, BigDecimal>()
            for (l in list) {
                val k = l.price.divide(step, 0, mode).multiply(step)
                m[k] = (m[k] ?: BigDecimal.ZERO) + l.qty
            }
            return m.map { BookLevel(it.key, it.value) }
        }
        return OrderBook(agg(bids, RoundingMode.FLOOR).sortedByDescending { it.price }, agg(asks, RoundingMode.CEILING).sortedBy { it.price })
    }

    companion object {
        fun parse(bids: List<List<String>>?, asks: List<List<String>>?): OrderBook {
            fun lv(rows: List<List<String>>?) = (rows ?: emptyList()).mapNotNull { r ->
                val p = Dec.parseOrNull(r.getOrNull(0)) ?: return@mapNotNull null
                val q = Dec.parseOrNull(r.getOrNull(1)) ?: return@mapNotNull null
                BookLevel(p, q)
            }
            return OrderBook(lv(bids).sortedByDescending { it.price }, lv(asks).sortedBy { it.price })
        }
    }
}
