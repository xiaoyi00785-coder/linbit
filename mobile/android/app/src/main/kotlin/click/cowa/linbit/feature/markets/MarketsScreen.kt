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
fun MarketsScreen(
    bottomInset: Dp, onOpenTrade: (MarketKind, String) -> Unit = { _, _ -> }, modifier: Modifier = Modifier, vm: MarketsViewModel = hiltViewModel(),
    initialTop: Int = 1, openSearch: Boolean = false, onSearchConsumed: () -> Unit = {}, loggedIn: Boolean = true, onLogin: () -> Unit = {}, onOverlay: (Boolean) -> Unit = {},
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val watch by vm.watch.collectAsStateWithLifecycle()
    val ctx = androidx.compose.ui.platform.LocalContext.current
    MarketPrefs.ensure(ctx)
    androidx.compose.runtime.LaunchedEffect(state.spot) {
        val symbol = ctx.getSharedPreferences("market_widget", android.content.Context.MODE_PRIVATE).getString("symbol", "BTCUSDT")
        state.spot.firstOrNull { it.symbol == symbol }?.let { MarketQuoteWidget.cache(ctx, it) }
    }
    var search by rememberSaveable { mutableStateOf(false) }
    var chatOpen by remember { mutableStateOf(false) }
    androidx.compose.runtime.LaunchedEffect(search, chatOpen) { onOverlay(search || chatOpen) }
    androidx.compose.runtime.DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    androidx.compose.runtime.LaunchedEffect(openSearch) { if (openSearch) { search = true; onSearchConsumed() } }
    Box(modifier.fillMaxSize()) {
        MarketsContent(state, watch, bottomInset, vm::refresh, vm::addToWatch, initialTop = initialTop, onOpenTrade = onOpenTrade,
            onSearch = { search = true }, loggedIn = loggedIn, onLogin = onLogin, onChatOverlay = { chatOpen = it })
        if (search) MarketSearchScreen(state, watch, vm::toggleWatch, { search = false }, onOpenTrade)
    }
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
    /** 市场二级：0 现货 / 1 合约 / 2 TradFi（离屏证据与深链用）。 */
    initialKind: Int = 0,
    onOpenTrade: (MarketKind, String) -> Unit = { _, _ -> },
    onSearch: () -> Unit = {},
    onChatOverlay: (Boolean) -> Unit = {},
    loggedIn: Boolean = true,
    onLogin: () -> Unit = {},
) {
    val p = LB.p
    var top by rememberSaveable(initialTop) { mutableIntStateOf(initialTop) }
    var chatOverlay by remember { mutableStateOf(false) }
    var menu by remember { mutableStateOf(false) }
    var widgets by remember { mutableStateOf(false) }
    var iconSheet by remember { mutableStateOf(false) }       // 0 自选 1 市场 2 聊天室
    var kind by rememberSaveable(initialKind) { mutableIntStateOf(initialKind.coerceIn(0, 2)) }      // 现货 / 合约 / TradFi
    var category by rememberSaveable { mutableIntStateOf(0) }
    var field by rememberSaveable { mutableStateOf(SortField.VOLUME) }
    var dir by rememberSaveable { mutableStateOf(SortDir.NONE) }

    Column(modifier.fillMaxSize().background(p.bg)) {
        // 顶栏：一级 Tab + 搜索 / 更多（更多带绿点）
        if (!(top == 2 && chatOverlay)) Row(
            Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            PrimaryTabs(listOf("自选", "市场", "聊天室"), top, { top = it })
            Box(Modifier.weight(1f))
            if (top != 2) LbIcon(R.drawable.ic_search, 22.dp, modifier = Modifier.pressable(onClick = onSearch))
            if (top != 2) Box(Modifier.padding(start = 18.dp)) {
                LbIcon(R.drawable.ic_more, 22.dp, modifier = Modifier.pressable { menu = true })
                Box(Modifier.align(Alignment.TopEnd).padding(top = 0.dp).size(6.dp).clip(CircleShape).background(p.badgeDot))
                MarketsMoreMenu(menu, { menu = false }, { iconSheet = true }, { widgets = true })
            }
        }
        if (!(top == 2 && chatOverlay)) HorizontalDivider(color = p.divider)
        when (top) {
            0 -> WatchlistTab(onAddWatch, state, watch, bottomInset, onOpenTrade)
            1 -> {
                Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp)) {
                    PrimaryTabs(listOf("现货", "合约", "TradFi"), kind, { kind = it; category = 0 }, size = FontSize.tabSecondary, underline = true)
                }
                HorizontalDivider(color = p.divider)
                if (kind == 2) {
                    click.cowa.linbit.feature.more.TradFiScreen(onBack = {}, onTrade = { fut, sym -> onOpenTrade(if (fut) MarketKind.FUTURES else MarketKind.SPOT, sym) }, embedded = true, bottomInset = bottomInset)
                } else {
                    val mk = if (kind == 0) MarketKind.SPOT else MarketKind.FUTURES
                    val cats = state.categoriesFor(mk)
                    val chips = listOf("全部") + cats.map { it.name }
                    val catIdx = category.coerceIn(0, (chips.size - 1).coerceAtLeast(0))
                    ChipBar(chips, catIdx, { category = it }, Modifier.padding(vertical = 10.dp))
                    MarketListHeader(field, dir, { f, d -> field = f; dir = d })
                    val rows = state.filtered(mk, catIdx).sortedByField(field, dir)
                    PullToRefreshBox(isRefreshing = state.loading && rows.isNotEmpty(), onRefresh = onRefresh, modifier = Modifier.fillMaxSize()) {
                        LazyColumn(contentPadding = PaddingValues(bottom = bottomInset)) {
                            if (rows.isEmpty()) item("empty") {
                                if (state.loading) Box(Modifier.fillMaxWidth().padding(40.dp), contentAlignment = Alignment.Center) {
                                    androidx.compose.material3.CircularProgressIndicator(color = p.textTertiary, strokeWidth = 2.dp, modifier = Modifier.size(24.dp))
                                } else EmptyState(state.error ?: if (mk == MarketKind.FUTURES) "暂无合约交易对" else "暂无现货交易对")
                            }
                            items(rows, key = { "${mk.name}:${it.symbol}" }) {
                                MarketRowView(it, state.cnyRate, onClick = { onOpenTrade(mk, it.symbol) })
                            }
                        }
                    }
                }
            }
            else -> if (loggedIn) ChatRoomTab(loggedIn, onLogin, { chatOverlay = it; onChatOverlay(it) }, bottomInset = bottomInset) else ChatRoomGuest(onLogin)
        }
        if (iconSheet) CoinIconSettingsSheet { iconSheet = false }
        if (widgets) MarketWidgetsSheet(state.spot) { widgets = false }
    }
}

/** 自选：只显示已加自选；空时按子 tab 推荐（对照 ref/7），绝不回退成全市场。 */
@Composable
private fun WatchlistTab(onAddWatch: (Set<String>) -> Unit, state: click.cowa.linbit.data.MarketState, watch: List<String>, bottomInset: Dp, onOpenTrade: (MarketKind, String) -> Unit) {
    val p = LB.p
    var kind by rememberSaveable { mutableIntStateOf(0) }
    var field by rememberSaveable { mutableStateOf(SortField.VOLUME) }
    var dir by rememberSaveable { mutableStateOf(SortDir.NONE) }
    val sub = MarketsTabCatalog.watchSubTab(kind)
    Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        PrimaryTabs(listOf("全部", "现货", "合约", "TradFi"), kind, { kind = it }, size = FontSize.label, underline = true)
    }
    if (watch.isEmpty()) {
        val recs = MarketsTabCatalog.recommendSymbols(state.spot, state.futures, state.tradFiSymbols.toList().sorted(), sub)
        if (recs.isEmpty()) {
            EmptyState(
                when (sub) {
                    MarketsTabCatalog.WatchSubTab.TRADFI -> "暂无 TradFi 品种可添加"
                    MarketsTabCatalog.WatchSubTab.FUTURES -> "暂无合约交易对可添加"
                    MarketsTabCatalog.WatchSubTab.SPOT -> "暂无现货交易对可添加"
                    else -> "暂无推荐"
                },
                Modifier.padding(top = 40.dp),
            )
        } else {
            var picked by remember(recs, kind) { mutableStateOf(recs.toSet()) }
            Column(Modifier.fillMaxSize().padding(Spacing.pageH), verticalArrangement = Arrangement.spacedBy(Spacing.lg)) {
                Text("为你推荐", style = lb(FontSize.label, FontWeight.Medium), color = p.textSecondary)
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
        }
        return
    }
    val watchedRows = MarketsTabCatalog.watchRows(watch, state.spot, state.futures, state.tradFiSymbols, sub, state.tradFiSpotRows, state.tradFiFuturesRows)
        .sortedByField(field, dir)
    if (watchedRows.isEmpty()) {
        EmptyState(
            when (sub) {
                MarketsTabCatalog.WatchSubTab.SPOT -> "该分类暂无现货自选"
                MarketsTabCatalog.WatchSubTab.FUTURES -> "该分类暂无合约自选"
                MarketsTabCatalog.WatchSubTab.TRADFI -> "该分类暂无 TradFi 自选"
                else -> "暂无自选"
            },
            Modifier.padding(top = 40.dp),
        )
        return
    }
    MarketListHeader(field, dir, { f, d -> field = f; dir = d }, Modifier.padding(top = 8.dp))
    LazyColumn(contentPadding = PaddingValues(bottom = bottomInset)) {
        items(watchedRows, key = { "${it.kind}:${it.symbol}" }) { r ->
            MarketRowView(r, state.cnyRate, onClick = { onOpenTrade(r.kind, r.symbol) })
        }
    }
}
