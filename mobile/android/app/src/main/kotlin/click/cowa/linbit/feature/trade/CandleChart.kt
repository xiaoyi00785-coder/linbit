package click.cowa.linbit.feature.trade

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.core.generated.ApiFuturesKlinesKlinesInner
import click.cowa.linbit.core.generated.ApiFuturesPublicTrade
import click.cowa.linbit.core.generated.ApiKline
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.ChipButton
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/** 统一蜡烛数据（现货 ApiKline / 合约 ApiFuturesKlinesKlinesInner）。 */
data class Candle(
    val open: BigDecimal,
    val high: BigDecimal,
    val low: BigDecimal,
    val close: BigDecimal,
    val volume: BigDecimal = BigDecimal.ZERO,
    val timeMs: Long = 0L,
) {
    companion object {
        fun fromSpot(k: ApiKline) = Candle(Dec.parse(k.open), Dec.parse(k.high), Dec.parse(k.low), Dec.parse(k.close), Dec.parse(k.volume), k.time * 1000L)
        fun fromFutures(k: ApiFuturesKlinesKlinesInner) = Candle(
            Dec.parse(k.open), Dec.parse(k.high), Dec.parse(k.low), Dec.parse(k.close), Dec.parse(k.volume), k.openTime ?: 0L,
        )
    }
}

/** 蜡烛图：对齐 iOS CandleChart。 */
@Composable
fun CandleChart(candles: List<Candle>, pricePrec: Int, modifier: Modifier = Modifier) {
    val p = LB.p
    val hi = candles.maxOfOrNull { it.high } ?: BigDecimal.ZERO
    val lo = candles.minOfOrNull { it.low } ?: BigDecimal.ZERO
    val range = hi.subtract(lo)
    Box(modifier.height(180.dp)) {
        Canvas(Modifier.fillMaxSize()) {
            val n = maxOf(candles.size, 1)
            val step = size.width / n
            val rangeD = range.toDouble()
            fun y(d: BigDecimal): Float {
                if (rangeD <= 0) return size.height / 2f
                return ((hi.subtract(d).toDouble() / rangeD) * (size.height - 8) + 4).toFloat()
            }
            for (gy in listOf(0f, 0.25f, 0.5f, 0.75f, 1f)) {
                val yy = size.height * gy
                drawLine(p.divider, Offset(0f, yy), Offset(size.width, yy), strokeWidth = 0.5f)
            }
            candles.forEachIndexed { i, c ->
                val color = if (c.close >= c.open) p.up else p.down
                val x = step * (i + 0.5f)
                drawLine(color, Offset(x, y(c.high)), Offset(x, y(c.low)), strokeWidth = 1f)
                val top = y(maxOf(c.open, c.close))
                val bot = y(minOf(c.open, c.close))
                val w = maxOf(step * 0.7f, 1f)
                drawRect(color, Offset(x - w / 2f, top), Size(w, maxOf(bot - top, 1f)))
            }
        }
        Column(Modifier.fillMaxSize().padding(end = 2.dp), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.SpaceBetween) {
            Text(Fmt.price(hi, pricePrec), style = lbNum(10.sp), color = p.textTertiary)
            Text(Fmt.price(lo, pricePrec), style = lbNum(10.sp), color = p.textTertiary)
        }
    }
}

private val INTERVALS = listOf("15m" to "15分", "1h" to "1小时", "4h" to "4小时", "1d" to "1天", "1w" to "1周")

/**
 * 图表面板：K 线 +（合约）最近成交。加载函数由 ViewModel 注入，现货走 /market/klines，合约走 /futures/market/klines。
 */
@Composable
fun TradeChartPanel(
    symbol: String,
    pricePrec: Int,
    showTrades: Boolean,
    loadCandles: suspend (interval: String) -> List<Candle>,
    loadTrades: (suspend () -> List<ApiFuturesPublicTrade>)? = null,
    modifier: Modifier = Modifier,
) {
    val p = LB.p
    var tab by remember { mutableStateOf(0) }
    var interval by remember { mutableStateOf("15m") }
    var candles by remember { mutableStateOf<List<Candle>?>(null) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var trades by remember { mutableStateOf<List<ApiFuturesPublicTrade>>(emptyList()) }
    var tradesErr by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(symbol, interval) {
        loading = true; error = null
        try { candles = loadCandles(interval); error = null }
        catch (e: Throwable) { error = e.message ?: "加载失败"; candles = null }
        finally { loading = false }
    }
    LaunchedEffect(symbol, tab, showTrades) {
        if (tab != 1 || loadTrades == null) return@LaunchedEffect
        try { trades = loadTrades(); tradesErr = null } catch (e: Throwable) { tradesErr = e.message ?: "加载失败" }
    }

    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            listOfNotNull("K线", if (showTrades) "成交" else null).forEachIndexed { i, t ->
                Text(
                    t, Modifier.pressable(scale = 1f) { tab = i },
                    style = lb(FontSize.label, if (tab == i) FontWeight.SemiBold else FontWeight.Normal),
                    color = if (tab == i) p.textPrimary else p.textSecondary,
                )
            }
        }
        if (tab == 0) {
            Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                INTERVALS.forEach { (k, label) ->
                    Text(
                        label, Modifier.pressable(scale = 1f) { interval = k },
                        style = lb(FontSize.small, if (interval == k) FontWeight.SemiBold else FontWeight.Normal),
                        color = if (interval == k) p.textPrimary else p.textSecondary,
                    )
                }
            }
            when {
                loading && candles == null -> Box(
                    Modifier.fillMaxWidth().height(180.dp).clip(RoundedCornerShape(Radius.sm)).background(p.fill),
                    contentAlignment = Alignment.Center,
                ) { CircularProgressIndicator(Modifier.height(24.dp), color = p.textSecondary, strokeWidth = 2.dp) }
                error != null && candles == null -> Column(Modifier.fillMaxWidth().height(180.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                    Text(error ?: "", style = lb(FontSize.small), color = p.down)
                    ChipButton("重试", Modifier.padding(top = 8.dp).height(28.dp)) { interval = interval } // retrigger via same value — use key bump
                }
                candles.isNullOrEmpty() -> EmptyState("暂无 K 线数据")
                else -> CandleChart(candles!!, pricePrec)
            }
        } else {
            when {
                tradesErr != null && trades.isEmpty() -> Text(tradesErr ?: "", style = lb(FontSize.small), color = p.down)
                trades.isEmpty() -> EmptyState("暂无成交")
                else -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row {
                        Text("时间", Modifier.weight(1f), style = lb(FontSize.small), color = p.textTertiary)
                        Text("价格(USDT)", Modifier.weight(1f), style = lb(FontSize.small), color = p.textTertiary)
                        Text("数量", Modifier.weight(1f), style = lb(FontSize.small), color = p.textTertiary)
                    }
                    trades.take(20).forEach { t ->
                        val sell = t.takerSide == ApiFuturesPublicTrade.TakerSide.SELL
                        Row {
                            Text(hms(t.time), Modifier.weight(1f), style = lbNum(FontSize.small), color = p.textSecondary)
                            Text(Fmt.price(Dec.parse(t.price), pricePrec), Modifier.weight(1f), style = lbNum(FontSize.small), color = if (sell) p.down else p.up)
                            Text(Fmt.compact(Dec.parse(t.qty)), Modifier.weight(1f), style = lbNum(FontSize.small), color = p.textPrimary)
                        }
                    }
                }
            }
        }
    }
}

internal fun hms(raw: String?): String {
    if (raw.isNullOrBlank()) return "--"
    return try {
        val ms = when {
            raw.all { it.isDigit() } -> raw.toLong()
            else -> {
                val f = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss", Locale.US).apply { timeZone = TimeZone.getTimeZone("UTC") }
                f.parse(raw.take(19))?.time ?: return "--"
            }
        }
        SimpleDateFormat("HH:mm:ss", Locale.US).apply { timeZone = TimeZone.getDefault() }
            .format(Date(if (ms < 1_000_000_000_000L) ms * 1000 else ms))
    } catch (_: Throwable) { "--" }
}

/** 合约计算器：本地推算开仓成本 / 强平价 / 目标盈亏，不走资金操作接口。 */
object TradeCalculator {
    fun cost(qty: BigDecimal, price: BigDecimal, leverage: Int): BigDecimal {
        if (price.signum() <= 0 || leverage <= 0) return BigDecimal.ZERO
        return qty.multiply(price).divide(BigDecimal(leverage), 8, java.math.RoundingMode.HALF_UP)
    }
    fun liqPrice(entry: BigDecimal, leverage: Int, long: Boolean, mmr: BigDecimal): BigDecimal {
        if (entry.signum() <= 0 || leverage <= 0) return BigDecimal.ZERO
        val inv = BigDecimal.ONE.divide(BigDecimal(leverage), 12, java.math.RoundingMode.HALF_UP)
        return if (long) entry.multiply(BigDecimal.ONE.subtract(inv).add(mmr))
        else entry.multiply(BigDecimal.ONE.add(inv).subtract(mmr))
    }
    fun pnl(entry: BigDecimal, exit: BigDecimal, qty: BigDecimal, long: Boolean): BigDecimal {
        val q = qty.abs()
        return if (long) exit.subtract(entry).multiply(q) else entry.subtract(exit).multiply(q)
    }
    fun targetPrice(entry: BigDecimal, qty: BigDecimal, targetPnl: BigDecimal, long: Boolean): BigDecimal {
        if (qty.signum() == 0) return BigDecimal.ZERO
        val delta = targetPnl.divide(qty.abs(), 12, java.math.RoundingMode.HALF_UP)
        return if (long) entry.add(delta) else entry.subtract(delta)
    }
}
