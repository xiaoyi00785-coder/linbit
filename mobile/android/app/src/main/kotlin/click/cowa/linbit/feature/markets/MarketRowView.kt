package click.cowa.linbit.feature.markets

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.SortDir
import click.cowa.linbit.data.SortField
import click.cowa.linbit.ui.components.ChangeBadge
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.SortLabel
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import java.math.BigDecimal

/**
 * 行情行（对照图 ref/9.jpg：行高 56，图标 32，基础币 16 medium + 「/ USDT」13 灰，价格 16 medium + ¥ 12 灰，色块 76×31）。
 * [cnyRate] 为空（汇率接口取不到）时不显示折合金额。
 */
@Composable
fun MarketRowView(
    row: MarketRow,
    cnyRate: BigDecimal?,
    modifier: Modifier = Modifier,
    showIcon: Boolean = true,
    /** 首页榜单样式：显示完整交易对（BTCUSDT），17sp */
    compactPair: Boolean = false,
    onClick: () -> Unit = {},
) {
    val p = LB.p
    Row(
        modifier
            .fillMaxWidth()
            .height(Metrics.marketRowH)
            .pressable(scale = 1f, onClick = onClick)
            .padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (showIcon) CoinIcon(row.base)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            if (compactPair) {
                Text(row.symbol, style = lb(FontSize.symbol, FontWeight.Medium), color = p.textPrimary, maxLines = 1)
            } else {
                Row(verticalAlignment = Alignment.Bottom) {
                    Text(row.base, style = lb(FontSize.symbolWithIcon, FontWeight.Medium), color = p.textPrimary, maxLines = 1)
                    Text(" / ${row.quote}", style = lb(FontSize.caption), color = p.textSecondary, maxLines = 1)
                }
            }
            Text(Fmt.volumeCN(row.quoteVolume), style = lbNum(FontSize.caption), color = p.textSecondary, maxLines = 1)
        }
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                priceText(row), style = lbNum(if (compactPair) FontSize.symbol else FontSize.price, FontWeight.Medium),
                color = p.textPrimary, maxLines = 1,
            )
            if (cnyRate != null) {
                Text(Fmt.fiat(row.last, cnyRate, row.precision), style = lbNum(FontSize.small), color = p.textSecondary, maxLines = 1)
            }
        }
        Spacer(Modifier.width(2.dp))
        ChangeBadge(row.changePct)
    }
}

fun priceText(row: MarketRow): String =
    if (row.precision != null) Fmt.price(row.last, row.precision) else Fmt.priceAuto(row.last)

/** 可排序表头（对照图 ref/9.jpg：币种/成交额 ⇕、最新价 ⇕、涨跌幅 ⇕）。 */
@Composable
fun MarketListHeader(
    field: SortField,
    dir: SortDir,
    onSort: (SortField, SortDir) -> Unit,
    modifier: Modifier = Modifier,
    leftTitle: String = "币种/成交额",
) {
    fun tap(f: SortField) {
        if (field != f) onSort(f, SortDir.DESC)
        else onSort(f, when (dir) { SortDir.DESC -> SortDir.ASC; SortDir.ASC -> SortDir.NONE; SortDir.NONE -> SortDir.DESC })
    }
    Row(
        modifier.fillMaxWidth().height(30.dp).padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        SortLabel(leftTitle, modifier = Modifier.pressable(scale = 1f) { tap(SortField.VOLUME) })
        Spacer(Modifier.weight(1f))
        SortLabel("最新价", modifier = Modifier.pressable(scale = 1f) { tap(SortField.PRICE) })
        Row(Modifier.width(Metrics.changeBadgeW + 12.dp), horizontalArrangement = Arrangement.End) {
            SortLabel("涨跌幅", modifier = Modifier.pressable(scale = 1f) { tap(SortField.CHANGE) })
        }
    }
}
