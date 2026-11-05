package click.cowa.linbit.data

import click.cowa.linbit.core.generated.ApiFuturesTicker
import click.cowa.linbit.core.generated.ApiSymbol
import click.cowa.linbit.core.generated.ApiTicker
import click.cowa.linbit.core.realtime.WsFuturesTicker
import click.cowa.linbit.core.util.Dec
import java.math.BigDecimal

enum class MarketKind { SPOT, FUTURES }

/** 行情统一行模型（现货 Ticker / 合约 FuturesTicker 归一）。所有数值为 BigDecimal。 */
data class MarketRow(
    val symbol: String,
    val base: String,
    val quote: String,
    val last: BigDecimal,
    /** 百分数：0.66 表示 +0.66% */
    val changePct: BigDecimal,
    val quoteVolume: BigDecimal,
    val precision: Int?,
) {
    companion object {
        fun spot(t: ApiTicker, meta: ApiSymbol?): MarketRow {
            val base = meta?.base ?: t.symbol.removeSuffix("USDT")
            return MarketRow(
                t.symbol, base, meta?.quote ?: "USDT", Dec.parse(t.lastPrice), Dec.parse(t.priceChangePercent),
                Dec.parse(t.quoteVolume), meta?.pricePrecision,
            )
        }

        fun futures(t: ApiFuturesTicker): MarketRow? {
            val sym = t.symbol ?: return null
            return MarketRow(
                sym, sym.removeSuffix("USDT"), "USDT",
                effectivePrice(t.lastPrice, t.markPrice, t.indexPrice),
                futuresChange(t.priceChangePercent, t.priceChangePercent24h),
                Dec.parse(t.quoteVolume ?: t.turnover24h), null,
            )
        }

        fun futures(t: WsFuturesTicker, old: MarketRow): MarketRow = old.copy(
            last = effectivePrice(t.lastPrice, t.markPrice, t.indexPrice).takeIf { it.signum() > 0 } ?: old.last,
            changePct = t.priceChangePercent?.let { Dec.parse(it) } ?: old.changePct,
            quoteVolume = (t.quoteVolume ?: t.turnover24h)?.let { Dec.parse(it) } ?: old.quoteVolume,
        )

        /** 显示用最新价：lastPrice 为 0（无成交）时依次退回 标记价 → 指数价。 */
        fun effectivePrice(vararg candidates: String?): BigDecimal =
            candidates.map { Dec.parse(it) }.firstOrNull { it.signum() > 0 } ?: BigDecimal.ZERO

        /**
         * 合约涨跌幅：优先 priceChangePercent（已是百分数，如 "1.52"）；
         * 仅有 priceChangePercent24h（小数比例，如 "0.015174"）时乘以 100。
         */
        fun futuresChange(percent: String?, ratio24h: String?): BigDecimal =
            Dec.parseOrNull(percent) ?: Dec.parseOrNull(ratio24h)?.multiply(BigDecimal(100)) ?: BigDecimal.ZERO
    }
}

enum class SortField { VOLUME, PRICE, CHANGE }
enum class SortDir { NONE, DESC, ASC }

fun List<MarketRow>.sortedByField(field: SortField, dir: SortDir): List<MarketRow> {
    if (dir == SortDir.NONE) return this
    val key: (MarketRow) -> BigDecimal = {
        when (field) { SortField.VOLUME -> it.quoteVolume; SortField.PRICE -> it.last; SortField.CHANGE -> it.changePct }
    }
    return if (dir == SortDir.DESC) sortedByDescending(key) else sortedBy(key)
}
