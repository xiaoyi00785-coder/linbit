package click.cowa.linbit.feature.markets

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.R
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.SortDir
import click.cowa.linbit.data.SortField
import click.cowa.linbit.data.sortedByField
import click.cowa.linbit.ui.components.ChipBar
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb

/** 行情页（对照图 ref/7 自选空态推荐、ref/8 / ref/9 市场-现货，ref/15 合约列表样式在后续里程碑）。 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MarketsScreen(bottomInset: Dp, modifier: Modifier = Modifier, vm: MarketsViewModel = hiltViewModel()) {
    val p = LB.p
    val state by vm.state.collectAsStateWithLifecycle()
    val watch by vm.watch.collectAsStateWithLifecycle()
    MarketsContent(state, watch, bottomInset, vm::refresh, vm::addToWatch, modifier)
}

/** 无状态内容（便于截图对照与预览）。 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MarketsContent(
    state: click.cowa.linbit.data.MarketState,
    watch: List<String>,
    bottomInset: Dp,
    onRefresh: () -> Unit,
    onAddWatch: (Set<String>) -> Unit,
    modifier: Modifier = Modifier,
    initialTop: Int = 1,
) {
    val p = LB.p
    var top by rememberSaveable { mutableIntStateOf(initialTop) }       // 0 自选 1 市场 2 聊天室
    var kind by rememberSaveable { mutableIntStateOf(0) }      // 现货 / 合约 / TradFi
    var category by rememberSaveable { mutableIntStateOf(0) }
    var field by rememberSaveable { mutableStateOf(SortField.VOLUME) }
    var dir by rememberSaveable { mutableStateOf(SortDir.NONE) }

    Column(modifier.fillMaxSize().background(p.bg)) {
        // 顶栏：一级 Tab + 搜索 / 更多（更多带绿点）
        Row(
            Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            PrimaryTabs(listOf("自选", "市场", "聊天室"), top, { top = it })
            Box(Modifier.weight(1f))
            LbIcon(R.drawable.ic_search, 22.dp)
            Box(Modifier.padding(start = 18.dp)) {
                LbIcon(R.drawable.ic_more, 22.dp)
                Box(Modifier.align(Alignment.TopEnd).padding(top = 0.dp).size(6.dp).clip(CircleShape).background(p.badgeDot))
            }
        }
        HorizontalDivider(color = p.divider)
        when (top) {
            0 -> WatchlistTab(onAddWatch, state, watch, bottomInset)
            1 -> {
                Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp)) {
                    PrimaryTabs(listOf("现货", "合约", "TradFi"), kind, { kind = it; category = 0 }, size = FontSize.tabSecondary, underline = true)
                }
                HorizontalDivider(color = p.divider)
                if (kind == 2) {
                    Placeholder("TradFi", "TradFi 后端暂未提供")
                } else {
                    val chips = listOf("全部") + state.categories.map { it.name }
                    ChipBar(chips, category.coerceAtMost(chips.size - 1), { category = it }, Modifier.padding(vertical = 10.dp))
                    MarketListHeader(field, dir, { f, d -> field = f; dir = d })
                    val mk = if (kind == 0) MarketKind.SPOT else MarketKind.FUTURES
                    val rows = state.filtered(mk, category).sortedByField(field, dir)
                    PullToRefreshBox(isRefreshing = state.loading && rows.isNotEmpty(), onRefresh = onRefresh, modifier = Modifier.fillMaxSize()) {
                        LazyColumn(contentPadding = PaddingValues(bottom = bottomInset)) {
                            if (rows.isEmpty()) item("empty") {
                                if (state.loading) Box(Modifier.fillMaxWidth().padding(40.dp), contentAlignment = Alignment.Center) {
                                    androidx.compose.material3.CircularProgressIndicator(color = p.textTertiary, strokeWidth = 2.dp, modifier = Modifier.size(24.dp))
                                } else EmptyState(state.error ?: "暂无数据")
                            }
                            items(rows, key = { it.symbol }) { MarketRowView(it, state.cnyRate) }
                        }
                    }
                }
            }
            else -> Placeholder("聊天室", "聊天室后端暂未提供，敬请期待")
        }
    }
}

/** 自选：空时显示推荐（ref/7：两列卡片 + 勾选 + 黑色「加入自选」）；有自选时显示列表。 */
@Composable
private fun WatchlistTab(onAddWatch: (Set<String>) -> Unit, state: click.cowa.linbit.data.MarketState, watch: List<String>, bottomInset: Dp) {
    val p = LB.p
    var kind by rememberSaveable { mutableIntStateOf(0) }
    var field by rememberSaveable { mutableStateOf(SortField.VOLUME) }
    var dir by rememberSaveable { mutableStateOf(SortDir.NONE) }
    Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        PrimaryTabs(listOf("全部", "现货", "合约", "TradFi"), kind, { kind = it }, size = FontSize.label, underline = true)
    }
    if (watch.isEmpty()) {
        val recs = (state.futures.take(6) + state.spot.take(4)).map { it.symbol }.distinct()
        var picked by remember(recs) { mutableStateOf(recs.toSet()) }
        Column(Modifier.fillMaxSize().padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(Spacing.lg)) {
            recs.chunked(2).forEach { pair ->
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    pair.forEach { s ->
                        val on = s in picked
                        Row(
                            Modifier
                                .weight(1f).height(58.dp)
                                .border(BorderStroke(1.dp, p.border), RoundedCornerShape(Radius.lg))
                                .pressable(scale = 1f) { picked = if (on) picked - s else picked + s }
                                .padding(horizontal = 16.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(s, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.Normal), color = p.textPrimary, maxLines = 1)
                            Box(
                                Modifier.size(18.dp).clip(RoundedCornerShape(4.dp)).background(if (on) p.primary else androidx.compose.ui.graphics.Color.Transparent)
                                    .border(BorderStroke(1.5.dp, p.primary), RoundedCornerShape(4.dp)),
                                contentAlignment = Alignment.Center,
                            ) { if (on) Text("✓", style = lb(FontSize.small, FontWeight.Bold), color = p.textOnPrimary) }
                        }
                    }
                    if (pair.size == 1) Box(Modifier.weight(1f))
                }
            }
            Box(
                Modifier.fillMaxWidth().height(48.dp).clip(RoundedCornerShape(Radius.xl)).background(p.primary)
                    .pressable { onAddWatch(picked) },
                contentAlignment = Alignment.Center,
            ) { Text("加入自选", style = lb(FontSize.body, FontWeight.Medium), color = p.textOnPrimary) }
        }
    } else {
        MarketListHeader(field, dir, { f, d -> field = f; dir = d }, Modifier.padding(top = 8.dp))
        val all = (state.spot + state.futures).filter { it.symbol in watch }.sortedByField(field, dir)
        LazyColumn(contentPadding = PaddingValues(bottom = bottomInset)) {
            items(all) { MarketRowView(it, state.cnyRate) }
        }
    }
}

@Composable
fun Placeholder(title: String, note: String = "功能开发中", modifier: Modifier = Modifier.fillMaxSize()) {
    val p = LB.p
    Column(modifier.background(p.bg), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
        click.cowa.linbit.ui.components.LinbitLogo(44.dp)
        Box(Modifier.height(10.dp))
        Text(title, style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        Box(Modifier.height(10.dp))
        Text(note, style = lb(FontSize.caption), color = p.textSecondary)
    }
}
