package click.cowa.linbit.feature.trade

import kotlin.math.abs

/** Wilder ATR + trailing bands. Null means insufficient history, never a fabricated price. */
internal data class SupertrendPoint(val value: Double, val bullish: Boolean)

internal fun supertrend(candles: List<Candle>, period: Int = 10, multiplier: Double = 3.0): List<SupertrendPoint?> {
    require(period > 0 && multiplier.isFinite() && multiplier > 0)
    val result = MutableList<SupertrendPoint?>(candles.size) { null }
    // An invalid upstream candle must not crash Compose or invent a usable indicator.
    if (candles.any { c -> !c.high.toDouble().isFinite() || !c.low.toDouble().isFinite() ||
            !c.close.toDouble().isFinite() || c.high < c.low }) return result
    var atr = 0.0
    var seed = 0.0
    var upper = 0.0
    var lower = 0.0
    var bullish = false
    candles.forEachIndexed { i, candle ->
        val high = candle.high.toDouble(); val low = candle.low.toDouble()
        val close = candle.close.toDouble()
        val previousClose = if (i == 0) close else candles[i - 1].close.toDouble()
        val tr = if (i == 0) high - low else maxOf(high - low, abs(high - previousClose), abs(low - previousClose))
        if (i < period) seed += tr
        if (i < period - 1) return@forEachIndexed
        atr = if (i == period - 1) seed / period else (atr * (period - 1) + tr) / period
        val midpoint = low + (high - low) / 2
        val basicUpper = midpoint + multiplier * atr
        val basicLower = midpoint - multiplier * atr
        if (i == period - 1) {
            upper = basicUpper; lower = basicLower
        } else {
            upper = if (basicUpper < upper || previousClose > upper) basicUpper else upper
            lower = if (basicLower > lower || previousClose < lower) basicLower else lower
            bullish = if (bullish) close >= lower else close > upper
        }
        result[i] = SupertrendPoint(if (bullish) lower else upper, bullish)
    }
    return result
}
