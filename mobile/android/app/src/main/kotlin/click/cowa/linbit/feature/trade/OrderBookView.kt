package click.cowa.linbit.feature.trade

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.trade.BookLevel
import click.cowa.linbit.data.trade.OrderBook
import click.cowa.linbit.ui.components.DownTriangle
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal
import kotlin.math.roundToInt

/** 盘口显示模式：0 默认（买卖各半）、1 只看买盘、2 只看卖盘。 */
const val BOOK_MODES = 3

/**
 * 订单簿（对照图 ref/11 合约 7 档 × 18pt、ref/28 现货 5 档 × 23pt）：
 * 表头（价格/数量 + 计价币）→ 卖盘（红，价格升序自下而上）→ 最新价 → 买盘（绿）→ B/S 占比条 → 模式图标 + 精度选择。
 */
@Composable
fun OrderBookView(
    book: OrderBook,
    rows: Int,
    rowHeight: Dp,
    pricePrec: Int,
    quote: String,
    mid: @Composable () -> Unit,
    mode: Int,
    precisionText: String,
    onPick: (BigDecimal) -> Unit,
    onMode: () -> Unit,
    onPrecision: () -> Unit,
    modifier: Modifier = Modifier,
    baseUnit: String = quote,
) {
    val p = LB.p
    val asks = if (mode == 1) emptyList() else book.asks.take(if (mode == 2) rows * 2 else rows)
    val bids = if (mode == 2) emptyList() else book.bids.take(if (mode == 1) rows * 2 else rows)
    val maxQ = (asks + bids).maxOfOrNull { it.qty } ?: BigDecimal.ONE
    Column(modifier) {
        Row(Modifier.fillMaxWidth()) {
            Column(Modifier.weight(1f)) {
                Text("价格", style = lb(FontSize.small), color = p.textSecondary)
                Text("($quote)", style = lb(FontSize.small), color = p.textSecondary)
            }
            Column(horizontalAlignment = Alignment.End) {
                Text("数量", style = lb(FontSize.small), color = p.textSecondary)
                Text("($baseUnit)", style = lb(FontSize.small), color = p.textSecondary)
            }
        }
        Box(Modifier.height(8.dp))
        // 卖盘：价格从高到低排布，最靠近最新价的是卖一（最低）
        val shownAsks = asks.reversed()
        if (mode != 1) {
            repeat((if (mode == 2) rows * 2 else rows) - shownAsks.size) { BookRow(null, true, maxQ, rowHeight, pricePrec, onPick) }
            shownAsks.forEach { BookRow(it, true, maxQ, rowHeight, pricePrec, onPick) }
        }
        mid()
        if (mode != 2) {
            bids.forEach { BookRow(it, false, maxQ, rowHeight, pricePrec, onPick) }
            repeat((if (mode == 1) rows * 2 else rows) - bids.size) { BookRow(null, false, maxQ, rowHeight, pricePrec, onPick) }
        }
        RatioBar(book.buyRatio, Modifier.padding(top = 8.dp))
        Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            BookModeIcon(mode, onMode)
            Row(
                Modifier.weight(1f).height(26.dp).clip(RoundedCornerShape(Radius.sm)).background(p.fill)
                    .pressable(scale = 1f, onClick = onPrecision).padding(horizontal = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(precisionText, Modifier.weight(1f), style = lbNum(FontSize.small), color = p.textPrimary, maxLines = 1)
                DownTriangle(tint = p.textSecondary, size = 14.dp)
            }
        }
    }
}

@Composable
private fun BookRow(l: BookLevel?, ask: Boolean, maxQ: BigDecimal, h: Dp, prec: Int, onPick: (BigDecimal) -> Unit) {
    val p = LB.p
    val ratio = if (l == null || maxQ.signum() == 0) 0f else l.qty.divide(maxQ, 4, java.math.RoundingMode.HALF_UP).toFloat().coerceIn(0f, 1f)
    Box(Modifier.fillMaxWidth().height(h).pressable(scale = 1f) { if (l != null) onPick(l.price) }) {
        Box(
            Modifier.fillMaxHeight().fillMaxWidth(ratio).align(Alignment.CenterEnd).background(if (ask) p.downTint else p.upTint),
        )
        Row(Modifier.fillMaxWidth().fillMaxHeight(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                l?.let { Fmt.price(it.price, prec) } ?: "--", Modifier.weight(1f), style = lbNum(12.5.sp),
                color = if (ask) p.down else p.up, maxLines = 1,
            )
            Text(l?.let { Fmt.compact(it.qty) } ?: "--", style = lbNum(12.5.sp), color = p.textSecondary, maxLines = 1)
        }
    }
}

@Composable
private fun RatioBar(buy: Double, modifier: Modifier = Modifier) {
    val p = LB.p
    val pct = (buy * 100).roundToInt()
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text("B $pct%", style = lbNum(10.sp), color = p.up, maxLines = 1)
        Row(Modifier.weight(1f).padding(horizontal = 4.dp).height(2.dp), horizontalArrangement = Arrangement.spacedBy(1.dp)) {
            Box(Modifier.weight(buy.toFloat().coerceIn(0.02f, 0.98f)).fillMaxHeight().clip(CircleShape).background(p.up))
            Box(Modifier.weight((1 - buy).toFloat().coerceIn(0.02f, 0.98f)).fillMaxHeight().clip(CircleShape).background(p.down))
        }
        Text("${100 - pct}% S", style = lbNum(10.sp), color = p.down, maxLines = 1)
    }
}

/** 盘口模式图标（2×2 小块：左侧色块 + 右侧两条横线）。 */
@Composable
fun BookModeIcon(mode: Int, onClick: () -> Unit) {
    val p = LB.p
    Column(
        Modifier.size(26.dp).pressable(scale = 1f, onClick = onClick).padding(4.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        repeat(2) { i ->
            val c = when (mode) { 0 -> if (i == 0) p.down else p.up; 1 -> p.up; else -> p.down }
            Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(2.dp), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(7.dp).clip(RoundedCornerShape(1.5.dp)).background(c))
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    repeat(2) { Box(Modifier.fillMaxWidth().height(1.5.dp).background(p.textTertiary)) }
                }
            }
        }
    }
}

/** 现货中间价：粗体最新价 + 「≈ ¥ …」。 */
@Composable
fun SpotMid(last: BigDecimal, prec: Int, down: Boolean, fiat: String?, onClick: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().pressable(scale = 1f, onClick = onClick).padding(vertical = 6.dp)) {
        Text(if (last.signum() > 0) Fmt.price(last, prec) else "--", style = lbNum(20.sp, FontWeight.Bold), color = if (down) p.down else p.textPrimary, maxLines = 1)
        if (fiat != null) Text("≈ $fiat", style = lbNum(13.sp), color = p.textSecondary, maxLines = 1)
    }
}
