package click.cowa.linbit.feature.trade

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.horizontalScroll
import androidx.compose.runtime.collectAsState
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.R
import click.cowa.linbit.core.storage.ThemeMode
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.ButtonKind
import click.cowa.linbit.ui.components.CheckRow
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbButton
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.LbTextField
import click.cowa.linbit.ui.components.SheetOptionRow
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum

/** 价格精度（盘口聚合步长）选择：以交易对价格精度为最小步长，×10 / ×100 / ×1000。 */
@Composable
fun PrecisionSheet(pricePrec: Int, current: java.math.BigDecimal?, onPick: (java.math.BigDecimal) -> Unit, onClose: () -> Unit) {
    val base = java.math.BigDecimal.ONE.movePointLeft(pricePrec)
    LbSheet("价格精度", onClose) {
        Column(Modifier.padding(bottom = 20.dp)) {
            listOf(1, 10, 100, 1000).map { base.multiply(java.math.BigDecimal(it)) }.forEach { d ->
                SheetOptionRow(d.stripTrailingZeros().toPlainString(), (current ?: base).compareTo(d) == 0) { onPick(d); onClose() }
            }
        }
    }
}

@dagger.hilt.EntryPoint
@dagger.hilt.InstallIn(dagger.hilt.components.SingletonComponent::class)
interface PairSheetEntryPoint { fun watchlist(): click.cowa.linbit.data.Watchlist; fun marketStore(): click.cowa.linbit.data.MarketStore }

/** 搜索 + 分类 Tab（自选 / 后端分类）+ 币对列表（现货 / 合约共用）。参考：近全屏弹层，自选为空时推荐热门币对一键添加。 */
@Composable
fun PairPickerSheet(
    rows: List<MarketRow>,
    current: String,
    spot: Boolean,
    onPick: (String) -> Unit,
    onClose: () -> Unit,
    /** 历史筛选：可选「全部」回传空串，表示不限 symbol。 */
    allowAll: Boolean = false,
    onPickAll: (() -> Unit)? = null,
) {
    val p = LB.p
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val ep = remember { runCatching { dagger.hilt.android.EntryPointAccessors.fromApplication(ctx.applicationContext, PairSheetEntryPoint::class.java) }.getOrNull() }
    val watch by (ep?.watchlist()?.symbols ?: kotlinx.coroutines.flow.MutableStateFlow(emptyList<String>())).collectAsState()
    val cats = if (spot && !allowAll) ep?.marketStore()?.state?.collectAsState()?.value?.categories.orEmpty() else emptyList()
    val tabs = if (ep != null && !allowAll) listOf("自选") + cats.map { it.name } + listOf("全部") else emptyList()
    var tab by remember { mutableStateOf(if (tabs.isEmpty()) -1 else 0) }
    var q by remember { mutableStateOf("") }
    val base = when {
        q.isNotBlank() || tab < 0 || tab == tabs.lastIndex -> rows
        tab == 0 -> rows.filter { it.symbol in watch }
        else -> cats.getOrNull(tab - 1)?.symbols?.toSet()?.let { set -> rows.filter { it.symbol in set } } ?: rows
    }
    val list = base.filter { q.isBlank() || it.symbol.contains(q.trim(), ignoreCase = true) }
    LbSheet(null, onClose) {
      Column(Modifier.fillMaxWidth().height((androidx.compose.ui.platform.LocalConfiguration.current.screenHeightDp * 0.86f).dp)) {
        LbTextField(q, { q = it }, "搜索币对", Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 8.dp), height = 36.dp,
            leading = { LbIcon(R.drawable.ic_search, 15.dp, p.textTertiary) })
        if (tabs.isNotEmpty() && q.isBlank()) Row(Modifier.fillMaxWidth().horizontalScroll(androidx.compose.foundation.rememberScrollState()).padding(horizontal = Spacing.pageH, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(18.dp)) {
            tabs.forEachIndexed { i, t ->
                Column(Modifier.pressable(scale = 1f) { tab = i }, horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(t, style = lb(FontSize.body, if (i == tab) FontWeight.SemiBold else FontWeight.Normal), color = if (i == tab) p.textPrimary else p.textSecondary)
                    Box(Modifier.padding(top = 4.dp).size(14.dp, 3.dp).clip(RoundedCornerShape(2.dp)).background(if (i == tab) p.textPrimary else Color.Transparent))
                }
            }
        }
        if (allowAll && onPickAll != null) {
            Text(
                "全部币对",
                Modifier.fillMaxWidth().height(44.dp).pressable(scale = 1f) { onPickAll(); onClose() }
                    .padding(horizontal = Spacing.pageH),
                style = lb(FontSize.body, if (current.isEmpty()) FontWeight.SemiBold else FontWeight.Normal),
                color = if (current.isEmpty()) p.brand else p.textPrimary,
            )
        }
        val emptyFav = tab == 0 && q.isBlank() && list.isEmpty() && ep != null
        if (emptyFav) Text("交易对", Modifier.padding(horizontal = Spacing.pageH, vertical = 6.dp), style = lb(FontSize.small), color = p.textTertiary)
        else Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH, vertical = 6.dp)) {
            Text("交易对", Modifier.weight(1f), style = lb(FontSize.small), color = p.textTertiary)
            Text("最新价", style = lb(FontSize.small), color = p.textTertiary)
            Text("涨跌幅", Modifier.width(72.dp), style = lb(FontSize.small), color = p.textTertiary, textAlign = androidx.compose.ui.text.style.TextAlign.End)
        }
        if (emptyFav) {
            // 参考：自选为空 → 推荐成交额前列币对（默认全选）+「添加自选」
            val rec = remember(rows) { (listOf("BTCUSDT", "ETHUSDT").mapNotNull { s -> rows.firstOrNull { it.symbol == s } } + rows.sortedByDescending { it.quoteVolume }).distinct().take(2) }
            var picked by remember(rec) { mutableStateOf(rec.map { it.symbol }.toSet()) }
            if (rec.isEmpty()) EmptyState("暂无币对", Modifier.height(200.dp)) else {
                Column(Modifier.padding(horizontal = Spacing.pageH, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    rec.chunked(2).forEach { pair -> Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        pair.forEach { r ->
                            val on = r.symbol in picked
                            Row(Modifier.weight(1f).height(56.dp).border(0.5.dp, if (on) p.textPrimary else p.border, RoundedCornerShape(Radius.md)).pressable(scale = 1f) { picked = if (on) picked - r.symbol else picked + r.symbol }.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                                Text(if (spot) r.symbol.replace("USDT", " / USDT") else r.symbol, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
                                Box(Modifier.size(16.dp).clip(RoundedCornerShape(3.dp)).background(if (on) p.textPrimary else Color.Transparent).border(1.dp, if (on) p.textPrimary else p.textTertiary, RoundedCornerShape(3.dp)), contentAlignment = Alignment.Center) {
                                    if (on) LbIcon(R.drawable.ic_check, 12.dp, p.bg)
                                }
                            }
                        }
                        if (pair.size == 1) Box(Modifier.weight(1f))
                    } }
                    LbButton("添加自选", Modifier.fillMaxWidth().padding(top = 8.dp), enabled = picked.isNotEmpty()) { ep.watchlist().add(picked) }
                }
            }
        } else {
            if (list.isEmpty()) EmptyState("暂无相关币种", Modifier.height(240.dp))
            LazyColumn(Modifier.weight(1f)) {
                items(list, key = { it.symbol }) { r ->
                    Row(Modifier.fillMaxWidth().height(52.dp).pressable(scale = 1f) { onPick(r.symbol); onClose() }.padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            if (spot) r.symbol.replace("USDT", "/USDT") else r.symbol, Modifier.weight(1f),
                            style = lb(FontSize.body, FontWeight.Medium), color = if (r.symbol == current) p.brand else p.textPrimary,
                        )
                        Text(Fmt.priceAuto(r.last), style = lbNum(FontSize.label), color = p.textPrimary)
                        Text(
                            Fmt.percent(r.changePct), Modifier.width(72.dp), style = lbNum(FontSize.small),
                            color = if (r.changePct.signum() < 0) p.down else p.up, textAlign = androidx.compose.ui.text.style.TextAlign.End,
                        )
                    }
                }
            }
        }
      }
    }
}

/** 键值确认行。 */
@Composable
fun ConfirmRow(k: String, v: String, color: Color? = null) {
    val p = LB.p
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(k, Modifier.weight(1f), style = lb(FontSize.label), color = p.textSecondary)
        Text(v, style = lbNum(FontSize.label, FontWeight.Medium), color = color ?: p.textPrimary)
    }
}

/** 下单确认弹层。 */
@Composable
fun ConfirmSheet(
    title: String, rows: List<Triple<String, String, Color?>>, note: String?, buyColor: Boolean, confirmText: String, placing: Boolean,
    skip: Boolean, onSkip: (Boolean) -> Unit, onConfirm: () -> Unit, onClose: () -> Unit,
) {
    val p = LB.p
    LbSheet(title, onClose) {
        Column(Modifier.padding(horizontal = Spacing.pageH).padding(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            rows.forEach { (k, v, c) -> ConfirmRow(k, v, c) }
            if (note != null) Text(
                note, Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(10.dp),
                style = lb(FontSize.small), color = p.textSecondary,
            )
            CheckRow(skip, "不再提示（可在偏好设置中恢复）", Modifier.padding(top = 4.dp), onChange = onSkip)
            LbButton(confirmText, kind = if (buyColor) ButtonKind.BUY else ButtonKind.SELL, loading = placing, onClick = onConfirm)
        }
    }
}

/** 主题模式弹层（对照图 ref/14：跟随系统 / 日间 / 夜间 三张预览卡）。 */
@Composable
fun ThemeModeSheet(mode: ThemeMode, onPick: (ThemeMode) -> Unit, onClose: () -> Unit) {
    val p = LB.p
    LbSheet("主题模式", onClose) {
        Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 12.dp, bottom = 24.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            listOf(Triple(ThemeMode.SYSTEM, "跟随系统", 0), Triple(ThemeMode.LIGHT, "日间模式", 1), Triple(ThemeMode.DARK, "夜间模式", 2)).forEach { (m, label, idx) ->
                Column(Modifier.weight(1f).pressable(scale = 1f) { onPick(m); onClose() }, horizontalAlignment = Alignment.CenterHorizontally) {
                    Box(
                        Modifier.fillMaxWidth().height(116.dp).clip(RoundedCornerShape(Radius.lg))
                            .border(BorderStroke(if (mode == m) 2.dp else 0.dp, if (mode == m) p.brand else Color.Transparent), RoundedCornerShape(Radius.lg)),
                    ) {
                        val light = Color(0xFFF4F4F4); val dark = Color(0xFF1C1C1E)
                        when (idx) {
                            0 -> Row(Modifier.fillMaxSize()) { Box(Modifier.weight(1f).fillMaxSize().background(light)); Box(Modifier.weight(1f).fillMaxSize().background(dark)) }
                            1 -> Box(Modifier.fillMaxSize().background(light))
                            else -> Box(Modifier.fillMaxSize().background(dark))
                        }
                        Box(Modifier.align(Alignment.TopCenter).padding(top = 10.dp).fillMaxWidth(0.84f).height(34.dp).clip(RoundedCornerShape(6.dp)).background(if (idx == 2) Color(0xFF3A3A3C) else Color(0xFFE6E6E6)), contentAlignment = Alignment.Center) {
                            when (idx) {
                                0 -> Text("AUTO", style = lb(FontSize.caption), color = Color(0xFF888888))
                                1 -> LbIcon(R.drawable.ic_sun, 18.dp, Color(0xFF333333))
                                else -> Text("☾", style = lb(FontSize.title), color = Color.White)
                            }
                        }
                        Column(Modifier.align(Alignment.Center).padding(top = 28.dp).fillMaxWidth(0.84f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            repeat(2) { Box(Modifier.fillMaxWidth().height(10.dp).clip(RoundedCornerShape(5.dp)).background(if (idx == 2) Color(0xFF2C2C2E) else Color(0xFFE6E6E6))) }
                        }
                    }
                    Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Box(
                            Modifier.size(16.dp).clip(CircleShape).background(if (mode == m) p.brand else Color.Transparent)
                                .border(BorderStroke(1.dp, if (mode == m) p.brand else p.textTertiary), CircleShape),
                            contentAlignment = Alignment.Center,
                        ) { if (mode == m) LbIcon(R.drawable.ic_check, 10.dp, Color.White) }
                        Text(" $label", style = lb(FontSize.label), color = p.textPrimary)
                    }
                }
            }
        }
    }
}

