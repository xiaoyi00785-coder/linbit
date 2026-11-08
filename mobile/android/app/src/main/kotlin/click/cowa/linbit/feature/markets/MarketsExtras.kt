package click.cowa.linbit.feature.markets

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.data.MarketRow
import click.cowa.linbit.data.MarketState
import click.cowa.linbit.data.SortDir
import click.cowa.linbit.data.SortField
import click.cowa.linbit.data.sortedByField
import click.cowa.linbit.ui.components.ChangeBadge
import click.cowa.linbit.ui.components.ChipBar
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum

/** 行情本地偏好：币种图标显示开关、搜索历史（对照参考「币种图标设置」与搜索页）。 */
object MarketPrefs {
    private const val FILE = "linbit_market_prefs"
    var showCoinIcon by mutableStateOf(true)
        private set
    private var loaded = false

    fun ensure(ctx: Context) {
        if (loaded) return
        loaded = true
        showCoinIcon = ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).getBoolean("show_icon", true)
    }

    fun setShowIcon(ctx: Context, v: Boolean) {
        showCoinIcon = v
        ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit().putBoolean("show_icon", v).apply()
    }

    fun history(ctx: Context): List<String> =
        ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).getString("history", "").orEmpty().split(',').filter { it.isNotBlank() }

    fun pushHistory(ctx: Context, s: String) {
        val v = (listOf(s) + history(ctx).filter { it != s }).take(10)
        ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit().putString("history", v.joinToString(",")).apply()
    }

    fun clearHistory(ctx: Context) = ctx.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit().remove("history").apply()
}

/** 右上角「…」下拉菜单。 */
@Composable
fun MarketsMoreMenu(expanded: Boolean, onDismiss: () -> Unit, onIconSettings: () -> Unit, onWidgets: () -> Unit = {}) {
    val p = LB.p
    androidx.compose.material3.DropdownMenu(
        expanded = expanded, onDismissRequest = onDismiss,
        containerColor = p.bgElevated, shape = RoundedCornerShape(Radius.xl),
    ) {
        androidx.compose.material3.DropdownMenuItem(
            text = { Text("Widgets", style = lb(FontSize.body), color = p.textPrimary) },
            leadingIcon = { LbIcon(R.drawable.ic_doc_text, 18.dp, p.textPrimary) },
            onClick = { onDismiss(); onWidgets() },
        )
        androidx.compose.material3.DropdownMenuItem(
            text = { Text("币种图标设置", style = lb(FontSize.body), color = p.textPrimary) },
            leadingIcon = { LbIcon(R.drawable.ic_doc_text, 18.dp, p.textPrimary) },
            onClick = { onDismiss(); onIconSettings() },
        )
    }
}

/** 币种图标设置底部弹层：两张预览卡（显示 / 隐藏），选中黑边。 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CoinIconSettingsSheet(onDismiss: () -> Unit) {
    val p = LB.p
    val ctx = LocalContext.current
    val state = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = state, containerColor = p.bg) {
        Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("币种图标设置", style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
            listOf(true to "显示币种图标", false to "隐藏币种图标").forEach { (show, title) ->
                val sel = MarketPrefs.showCoinIcon == show
                Column(
                    Modifier.fillMaxWidth().clip(RoundedCornerShape(Radius.xl))
                        .border(BorderStroke(if (sel) 1.dp else 0.5.dp, if (sel) p.textPrimary else p.border), RoundedCornerShape(Radius.xl))
                        .pressable(scale = 1f) { MarketPrefs.setShowIcon(ctx, show); onDismiss() }
                        .padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Text(title, style = lb(FontSize.body, FontWeight.SemiBold), color = p.textPrimary)
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        if (show) CoinIcon("BTC", 32.dp)
                        Column(Modifier.weight(1f)) {
                            Row(verticalAlignment = Alignment.Bottom) {
                                Text("BTC", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                                Text(" / USDT", style = lb(FontSize.small), color = p.textSecondary)
                            }
                            Text("成交额 88.88T", style = lbNum(11.sp), color = p.textSecondary)
                        }
                        Column(horizontalAlignment = Alignment.End) {
                            Text("60,000.00", style = lbNum(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                            Text("$60,000.00", style = lbNum(11.sp), color = p.textSecondary)
                        }
                        ChangeBadge(java.math.BigDecimal("1.90"), Modifier.padding(start = 8.dp))
                    }
                }
            }
        }
    }
}

/** 行情搜索页（参考：输入框 + 取消；热门搜索 · 现货/合约 · 分类 · 星标自选；输入后按交易对实时过滤）。 */
@Composable
fun MarketSearchScreen(
    state: MarketState,
    watch: List<String>,
    onToggleWatch: (String) -> Unit,
    onClose: () -> Unit,
    onOpenTrade: (MarketKind, String) -> Unit,
) {
    val p = LB.p
    val ctx = LocalContext.current
    BackHandler(onBack = onClose)
    var q by rememberSaveable { mutableStateOf("") }
    var kind by rememberSaveable { mutableIntStateOf(0) }
    var category by rememberSaveable { mutableIntStateOf(0) }
    var searchTab by rememberSaveable { mutableIntStateOf(0) }
    var history by remember { mutableStateOf(MarketPrefs.history(ctx)) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
    fun open(k: MarketKind, s: String) { MarketPrefs.pushHistory(ctx, s); history = MarketPrefs.history(ctx); onOpenTrade(k, s); onClose() }

    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        Row(Modifier.fillMaxWidth().height(52.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            Row(
                Modifier.weight(1f).height(36.dp).clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(horizontal = 10.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                LbIcon(R.drawable.ic_search, 16.dp, p.textTertiary)
                Box(Modifier.weight(1f)) {
                    if (q.isEmpty()) Text("BTC", style = lb(FontSize.label), color = p.textTertiary)
                    BasicTextField(
                        q, { q = it.trim().uppercase() }, singleLine = true,
                        textStyle = lb(FontSize.label).copy(color = p.textPrimary),
                        cursorBrush = SolidColor(p.textPrimary),
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                        keyboardActions = KeyboardActions(onSearch = { if (q.isNotBlank()) { MarketPrefs.pushHistory(ctx, q); history = MarketPrefs.history(ctx) } }),
                        modifier = Modifier.fillMaxWidth().focusRequester(focus),
                    )
                }
                if (q.isNotEmpty()) LbIcon(R.drawable.ic_close, 14.dp, p.textTertiary, Modifier.pressable { q = "" })
            }
            Text("取消", Modifier.padding(start = 14.dp).pressable(scale = 1f, onClick = onClose), style = lb(FontSize.body), color = p.brand)
        }
        PrimaryTabs(listOf("热门搜索", "学院"), searchTab, { searchTab = it }, Modifier.padding(horizontal = Spacing.pageH).padding(top = 8.dp), size = FontSize.tabSecondary, underline = true)
        if (searchTab == 1) {
            AcademyTab(q)
            return@Column
        }
        if (q.isNotEmpty()) {
            val hits = buildList {
                state.spot.filter { it.symbol.contains(q) || it.base.contains(q) }.forEach { add(MarketKind.SPOT to it) }
                state.futures.filter { it.symbol.contains(q) || it.base.contains(q) }.forEach { add(MarketKind.FUTURES to it) }
            }
            if (hits.isEmpty()) EmptyState("未找到相关交易对", Modifier.padding(top = 40.dp))
            LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
                items(hits, key = { "${it.first}-${it.second.symbol}" }) { (k, r) ->
                    SearchRow(r, k, r.symbol in watch, { onToggleWatch(r.symbol) }) { open(k, r.symbol) }
                }
            }
            return@Column
        }
        if (history.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("搜索历史", Modifier.weight(1f), style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                Text("清空", Modifier.pressable(scale = 1f) { MarketPrefs.clearHistory(ctx); history = emptyList() }, style = lb(FontSize.caption), color = p.textSecondary)
            }
            @OptIn(ExperimentalLayoutApi::class)
            FlowRow(Modifier.fillMaxWidth().padding(Spacing.pageH), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                history.forEach { h ->
                    Text(h, Modifier.clip(RoundedCornerShape(Radius.sm)).background(p.fill).pressable(scale = 1f) { q = h }.padding(horizontal = 10.dp, vertical = 5.dp), style = lb(FontSize.caption), color = p.textPrimary)
                }
            }
        }
        ChipBar(listOf("现货", "合约"), kind, { kind = it; category = 0 }, Modifier.padding(top = 14.dp))
        val mk = if (kind == 0) MarketKind.SPOT else MarketKind.FUTURES
        if (kind == 0) {
            val chips = listOf("全部") + state.categories.map { it.name }
            ChipBar(chips, category.coerceAtMost(chips.size - 1), { category = it }, Modifier.padding(vertical = 10.dp))
        } else Spacer(Modifier.height(10.dp))
        val rows = (if (kind == 0) state.filtered(mk, category) else state.futures).sortedByField(SortField.VOLUME, SortDir.DESC)
        LazyColumn(contentPadding = PaddingValues(bottom = 24.dp)) {
            if (rows.isEmpty()) item { EmptyState(state.error ?: "暂无数据") }
            items(rows, key = { it.symbol }) { r -> SearchRow(r, mk, r.symbol in watch, { onToggleWatch(r.symbol) }) { open(mk, r.symbol) } }
        }
    }
}

@Composable
private fun SearchRow(r: MarketRow, kind: MarketKind, starred: Boolean, onStar: () -> Unit, onClick: () -> Unit) {
    val p = LB.p
    Row(
        Modifier.fillMaxWidth().height(60.dp).pressable(scale = 1f, onClick = onClick).padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (MarketPrefs.showCoinIcon) CoinIcon(r.base, 32.dp)
        Column(Modifier.weight(1f)) {
            if (kind == MarketKind.SPOT) Row(verticalAlignment = Alignment.Bottom) {
                Text(r.base, style = lb(FontSize.symbolWithIcon, FontWeight.Medium), color = p.textPrimary)
                Text(" / ${r.quote}", style = lb(FontSize.caption), color = p.textSecondary)
            } else Text(r.symbol, style = lb(FontSize.symbolWithIcon, FontWeight.Medium), color = p.textPrimary)
        }
        Column(horizontalAlignment = Alignment.End) {
            Text(priceText(r), style = lbNum(FontSize.price, FontWeight.Medium), color = p.textPrimary)
            Text(Fmt.percent(r.changePct), style = lbNum(FontSize.small), color = if (r.changePct.signum() < 0) p.down else p.up)
        }
        LbIcon(R.drawable.ic_star, 20.dp, if (starred) p.warning else p.textTertiary, Modifier.pressable(onClick = onStar))
    }
}

/** 游客态聊天室：参考「登录后查看聊天记录 / 立即登录」。 */
@Composable
fun ChatRoomGuest(onLogin: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxSize().padding(top = 120.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        EmptyState("登录后查看聊天记录")
        Box(
            Modifier.width(140.dp).height(40.dp).clip(RoundedCornerShape(Radius.xl)).background(p.primary).pressable(onClick = onLogin),
            contentAlignment = Alignment.Center,
        ) { Text("立即登录", style = lb(FontSize.label, FontWeight.Medium), color = p.textOnPrimary) }
    }
}
