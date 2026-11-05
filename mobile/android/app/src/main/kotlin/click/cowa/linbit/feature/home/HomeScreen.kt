package click.cowa.linbit.feature.home

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import click.cowa.linbit.R
import click.cowa.linbit.core.util.Dec
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.data.MarketKind
import click.cowa.linbit.feature.markets.MarketRowView
import click.cowa.linbit.ui.components.ChipBar
import click.cowa.linbit.ui.components.CoinIcon
import click.cowa.linbit.ui.components.EmptyState
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.LbImageIcon
import click.cowa.linbit.ui.components.LinbitLogo
import click.cowa.linbit.ui.components.PrimaryTabs
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import kotlinx.coroutines.delay
import java.time.Duration
import java.time.Instant

private data class GridItem(val light: Int, val dark: Int, val title: String)

private val GRID = listOf(
    GridItem(R.drawable.ic_g_chat_light, R.drawable.ic_g_chat_dark, "聊天室"), GridItem(R.drawable.ic_g_gift_light, R.drawable.ic_g_gift_dark, "福利中心"), GridItem(R.drawable.ic_g_earn_light, R.drawable.ic_g_earn_dark, "理财"),
    GridItem(R.drawable.ic_g_tradfi_light, R.drawable.ic_g_tradfi_dark, "TradFi"), GridItem(R.drawable.ic_g_copy_light, R.drawable.ic_g_copy_dark, "跟单"), GridItem(R.drawable.ic_g_bot_light, R.drawable.ic_g_bot_dark, "策略交易"),
    GridItem(R.drawable.ic_g_more_light, R.drawable.ic_g_more_dark, "更多"),
)

/**
 * 首页（对照图 ref/1.jpg、ref/2.jpg）：顶栏 → 总资产 → 新手任务 → 红包横幅 → 4 列功能宫格 → 卡片轮播 → 理财宝 → 榜单（自选/热门/涨幅/跌幅/成交额 × 现货/合约）。
 * 区块之间是发丝分隔线（ref 中为 0.33pt 浅灰），不是色块。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    bottomInset: Dp,
    onOpenAvatar: () -> Unit,
    onOpenMarkets: () -> Unit,
    onOpenMore: () -> Unit,
    onSoon: (String) -> Unit,
    onDeposit: () -> Unit,
    modifier: Modifier = Modifier,
    vm: HomeViewModel = hiltViewModel(),
) {
    val p = LB.p
    val s by vm.state.collectAsStateWithLifecycle()
    val m by vm.market_.collectAsStateWithLifecycle()
    val watch by vm.watchlist.symbols.collectAsStateWithLifecycle()
    val loggedIn by vm.loggedIn.collectAsStateWithLifecycle()
    HomeContent(s, m, watch, loggedIn, bottomInset, onOpenAvatar, onOpenMarkets, onOpenMore, onSoon, onDeposit, onRefresh = vm::refresh, modifier = modifier)
}

/** 无状态首页内容（便于截图测试 / 预览）。 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeContent(
    s: HomeState, m: click.cowa.linbit.data.MarketState, watch: List<String>, loggedIn: Boolean, bottomInset: Dp,
    onOpenAvatar: () -> Unit, onOpenMarkets: () -> Unit, onOpenMore: () -> Unit, onSoon: (String) -> Unit, onDeposit: () -> Unit,
    onRefresh: () -> Unit, modifier: Modifier = Modifier,
) {
    val p = LB.p
    var hide by rememberSaveable { mutableStateOf(false) }
    var rankTab by rememberSaveable { mutableIntStateOf(1) }
    var rankKind by rememberSaveable { mutableIntStateOf(1) } // ref/2：默认选中「合约」
    var bannerPage by remember { mutableIntStateOf(0) }
    var earnPage by remember { mutableIntStateOf(0) }

    LaunchedEffect(s.banners.size) { while (s.banners.size > 1) { delay(5_000); bannerPage = (bannerPage + 1) % s.banners.size } }
    LaunchedEffect(s.earn.size) { earnPage = 0; while (s.earn.size > 1) { delay(4_000); earnPage = (earnPage + 1) % s.earn.size } }

    Column(modifier.fillMaxSize().background(p.bg)) {
        TopBar(loggedIn, onOpenAvatar, onSoon)
        PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = onRefresh, modifier = Modifier.weight(1f)) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = bottomInset + 40.dp)) {
                AssetHeader(hide, { hide = !hide }, s.totalUsdt, m.cnyRate, onDeposit)
                TaskCard(s.newcomer, Modifier.padding(top = 12.dp)) { onSoon("福利中心即将开放") }
                RedPacket(Modifier.padding(top = 15.dp)) { onSoon("福利中心即将开放") }
                Grid(Modifier.padding(top = 15.dp), onTap = { if (it == "更多") onOpenMore() else onSoon("$it 即将开放") })
                Hairline(Modifier.padding(top = (64f / 3).dp))
                CardPromo(s.banners.getOrNull(bannerPage), bannerPage, s.banners.size) { onSoon("Linbit Card 即将开放") }
                Hairline()
                EarnStrip(s.earn.getOrNull(earnPage), earnPage, s.earn.size) { onSoon("理财即将开放") }
                Hairline()
                Ranking(m, watch, rankTab, { rankTab = it }, rankKind, { rankKind = it }, onOpenMarkets)
            }
        }
    }
}

@Composable
private fun Hairline(modifier: Modifier = Modifier) {
    HorizontalDivider(modifier, thickness = 0.5.dp, color = LB.p.border)
}

// ── 顶栏：Logo / 搜索 / 扫码 / 客服 / 通知（ref/1：左上为品牌 Logo，搜索框 31 高，3 个 22 图标）──
@Composable
private fun TopBar(loggedIn: Boolean, onAvatar: () -> Unit, onSoon: (String) -> Unit) {
    val p = LB.p
    Row(
        Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Box(Modifier.size(28.dp).pressable(onClick = onAvatar), contentAlignment = Alignment.Center) {
            if (loggedIn) LbIcon(R.drawable.ic_avatar, 28.dp) else LinbitLogo(30.dp)
        }
        Row(
            Modifier.weight(1f).height(Metrics.searchBarH).clip(RoundedCornerShape(Radius.md)).background(p.fill)
                .pressable(scale = 1f) { onSoon("搜索即将开放") }.padding(horizontal = 10.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            LbIcon(R.drawable.ic_search, 15.dp, p.textTertiary)
            Text("BTC", style = lb(FontSize.label), color = p.textTertiary)
        }
        LbIcon(R.drawable.ic_scan, modifier = Modifier.pressable { onSoon("扫一扫即将开放") })
        LbIcon(R.drawable.ic_headset, modifier = Modifier.pressable { onSoon("在线客服即将开放") })
        LbIcon(R.drawable.ic_bell, modifier = Modifier.pressable { onSoon("通知即将开放") })
    }
}

// 首页局部尺寸按 1170px 参考图的 3 倍密度换算；不修改其他页面的共享令牌。
// ── 总资产（ref/1：标签 → 40 高数字行（含 80×32 充值按钮）→ ≈ 折合）──
@Composable
private fun AssetHeader(hide: Boolean, onToggle: () -> Unit, total: java.math.BigDecimal, cnyRate: java.math.BigDecimal?, onDeposit: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.pageH).padding(top = (79f / 3).dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("总资产估值", style = lb(FontSize.caption), color = p.textSecondary)
            LbIcon(if (hide) R.drawable.ic_eye_off else R.drawable.ic_eye, 14.dp, p.textSecondary, Modifier.pressable(scale = 1f, onClick = onToggle))
        }
        Spacer(Modifier.height(6.dp))
        Row(Modifier.fillMaxWidth().height(34.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                if (hide) "****" else Fmt.price(total, 2),
                style = lbNum(34.sp, FontWeight.Bold).copy(lineHeight = 34.sp, platformStyle = androidx.compose.ui.text.PlatformTextStyle(includeFontPadding = false)),
                color = p.textPrimary,
            )
            Row(Modifier.padding(start = 6.dp, top = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                Text("USDT", style = lb(FontSize.label, FontWeight.Medium), color = p.textPrimary)
                LbIcon(R.drawable.ic_caret_down, 10.dp, p.textPrimary)
            }
            Spacer(Modifier.weight(1f))
            Box(
                Modifier.size(Metrics.heroButtonW, Metrics.heroButtonH).clip(RoundedCornerShape(Radius.md)).background(p.primary).pressable(onClick = onDeposit),
                contentAlignment = Alignment.Center,
            ) { Text("充值", style = lb(12.sp, FontWeight.Medium), color = p.textOnPrimary) }
        }
        Spacer(Modifier.height(10.dp))
        // 汇率取不到时不用硬编码汇率：总资产为 0 仍显示 0.00，否则显示 --
        val fiat = when {
            hide -> "≈ ****"
            cnyRate != null -> "≈ ¥ " + Fmt.price(total.multiply(cnyRate), 2)
            total.signum() == 0 -> "≈ ¥ 0.00"
            else -> "≈ ¥ --"
        }
        Text(fiat, style = lbNum(FontSize.small), color = p.textSecondary)
    }
}

// ── 新手任务卡（描边，含倒计时）──
@Composable
private fun TaskCard(n: click.cowa.linbit.core.generated.ApiHomeNewcomer?, modifier: Modifier, onClick: () -> Unit) {
    val p = LB.p
    val lead = if (n?.available == true && (n.completed ?: 0) > 0 && (n.tasks?.size ?: 0) > 0) "新手任务 ${n.completed}/${n.tasks!!.size}，可获得 " else "完成新手任务，可获得 "
    val left = remember(n?.deadline) { countdown(n?.deadline) }
    var tick by remember { mutableIntStateOf(0) }
    LaunchedEffect(n?.deadline) { while (n?.deadline != null) { delay(1_000); tick++ } }
    val text = remember(tick, n?.deadline) { left() }
    Row(
        modifier.padding(horizontal = Spacing.pageH).fillMaxWidth().height(Metrics.taskCardH + 1.dp)
            .border(0.5.dp, p.border, RoundedCornerShape(Radius.md)).clip(RoundedCornerShape(Radius.md))
            .pressable(scale = 1f, onClick = onClick).padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(1.dp)) {
            Text(
                buildAnnotatedString {
                    withStyle(SpanStyle(color = p.textPrimary)) { append(lead) }
                    withStyle(SpanStyle(color = p.up, fontWeight = FontWeight.SemiBold)) { append(n?.total?.takeIf { it.isNotBlank() } ?: "--") }
                    withStyle(SpanStyle(color = p.textPrimary)) { append(" ${n?.asset ?: "USDT"}体验金") }
                },
                style = lb(FontSize.small),
            )
            if (text != null) Text(text, style = lbNum(10.sp), color = p.textTertiary)
        }
        LbIcon(R.drawable.ic_chevron_right, 14.dp, p.textSecondary)
    }
}

/** 截止时间倒计时 "6D:23:46:26"（无 deadline 时不显示）。 */
private fun countdown(deadline: String?): () -> String? = {
    val d = deadline?.let { runCatching { Instant.parse(it) }.getOrNull() }
    if (d == null) null else {
        val dur = Duration.between(Instant.now(), d).coerceAtLeast(Duration.ZERO)
        "%dD:%02d:%02d:%02d".format(dur.toDays(), dur.toHours() % 24, dur.toMinutes() % 60, dur.seconds % 60)
    }
}

// ── 红包横幅 ──
@Composable
private fun RedPacket(modifier: Modifier, onClick: () -> Unit) {
    val p = LB.p
    Row(
        modifier.padding(horizontal = Spacing.pageH).fillMaxWidth().height(62.dp).clip(RoundedCornerShape(Radius.lg)).background(p.fill)
            .pressable(scale = 1f, onClick = onClick).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text("全员红包雨火爆来袭", style = lb(FontSize.small, FontWeight.SemiBold), color = p.textPrimary)
            Text("每日登入领红包，各种福利领不停", style = lb(11.sp), color = p.textSecondary)
        }
        LbImageIcon(R.drawable.ic_g_redpacket, 40.dp)
    }
}

// ── 功能宫格（4 列，行距 24，图标块 40 圆角 10）──
@Composable
private fun Grid(modifier: Modifier, onTap: (String) -> Unit) {
    val p = LB.p
    Column(modifier, verticalArrangement = Arrangement.spacedBy(24.dp)) {
        GRID.chunked(4).forEach { rowItems ->
            Row(Modifier.fillMaxWidth()) {
                for (i in 0 until 4) {
                    val item = rowItems.getOrNull(i)
                    Box(Modifier.weight(1f), contentAlignment = Alignment.TopCenter) {
                        if (item != null) Column(Modifier.pressable(scale = 1f) { onTap(item.title) }, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            Box(Modifier.size(Metrics.gridIcon).clip(RoundedCornerShape(Radius.lg)).background(p.iconTileBg), contentAlignment = Alignment.Center) {
                                LbImageIcon(if (p.isDark) item.dark else item.light, Metrics.gridIconGlyph)
                            }
                            Text(item.title, style = lb((32f / 3).sp), color = p.textPrimary)
                        }
                    }
                }
            }
        }
    }
}

// ── 卡片轮播（ref/1：左侧卡面 + 右侧文案，右下角 n/N）──
@Composable
private fun CardPromo(banner: HomeBanner?, page: Int, count: Int, onClick: () -> Unit) {
    val p = LB.p
    val b = banner ?: LOCAL_BANNERS[0]
    val isCard = b.id == "card"
    Box(Modifier.fillMaxWidth().height((413f / 3).dp).pressable(scale = 1f, onClick = onClick)) {
        Row(Modifier.fillMaxSize().padding(start = 24.dp, end = Spacing.pageH), verticalAlignment = Alignment.Top) {
            if (isCard) {
                // ref/1：卡面整体 ≈ 59×48dp，居中于文案块上半（比 92×80 容器缩 0.8，左对齐）
                CardArt(Modifier.padding(top = 21.dp).size(width = 92.dp, height = 80.dp).graphicsLayer { scaleX = 0.8f; scaleY = 0.8f; transformOrigin = androidx.compose.ui.graphics.TransformOrigin(0f, 0.5f) })
                Spacer(Modifier.width(2.dp))
            }
            Column(Modifier.weight(1f).padding(top = 20.dp)) {
                b.subtitle?.let { Text(it, style = lb(FontSize.small), color = p.textSecondary, maxLines = 1) }
                Spacer(Modifier.height(5.dp))
                Text(b.title, style = lb(14.sp, FontWeight.Medium), color = p.textPrimary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Row(Modifier.padding(top = 39.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text("查看详情", style = lb(FontSize.small), color = p.textPrimary)
                    LbIcon(R.drawable.ic_arrow_right, 14.dp, p.textPrimary)
                }
            }
        }
        PageCount(page, count, Modifier.align(Alignment.BottomEnd).padding(end = Spacing.pageH, bottom = 8.dp))
    }
}

@Composable
private fun PageCount(page: Int, count: Int, modifier: Modifier) {
    val p = LB.p
    Text(
        buildAnnotatedString {
            withStyle(SpanStyle(color = p.textPrimary, fontWeight = FontWeight.Medium)) { append("${page + 1}") }
            withStyle(SpanStyle(color = p.textTertiary)) { append("/${maxOf(count, 1)}") }
        },
        modifier, style = lbNum(11.sp),
    )
}

/** Linbit Card 卡面：品牌绿卡在后，黑色 VISA 卡在前（对应 ref/1 的卡面）。 */
@Composable
private fun CardArt(modifier: Modifier) {
    val p = LB.p
    Box(modifier) {
        Box(Modifier.align(Alignment.TopStart).padding(start = 0.dp, top = 6.dp).size(66.dp, 42.dp).rotate(-9f).clip(RoundedCornerShape(5.dp)).background(Color(0xFFABE127)))
        Box(Modifier.align(Alignment.BottomEnd).size(66.dp, 42.dp).rotate(5f).clip(RoundedCornerShape(5.dp)).background(Color(0xFF070808))) {
            Image(R.drawable.ic_mark_lime, 14.dp, Modifier.align(Alignment.TopStart).padding(6.dp))
            Text("VISA", Modifier.align(Alignment.BottomEnd).padding(end = 5.dp, bottom = 3.dp), style = lb(7.sp, FontWeight.Bold), color = Color.White)
        }
    }
}

@Composable
private fun Image(res: Int, size: Dp, modifier: Modifier) =
    androidx.compose.foundation.Image(painterResource(res), null, modifier.size(size))

// ── 理财宝（ref/1：第一行「理财宝 + 币图标」，第二行产品名 16 medium，右下 n/N）──
@Composable
private fun EarnStrip(e: click.cowa.linbit.core.generated.ApiEarnProduct?, page: Int, count: Int, onClick: () -> Unit) {
    val p = LB.p
    Box(Modifier.fillMaxWidth().height((260f / 3).dp).pressable(scale = 1f, onClick = onClick).padding(horizontal = Spacing.pageH)) {
        Column(Modifier.align(Alignment.CenterStart), verticalArrangement = Arrangement.spacedBy(0.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("理财宝", style = lb(11.sp), color = p.textPrimary)
                if (e != null) CoinIcon(e.asset ?: "USDT", 18.dp)
            }
            val apr = e?.let { Dec.parse(it.apr).multiply(java.math.BigDecimal(100)).setScale(2, java.math.RoundingMode.HALF_UP).stripTrailingZeros().toPlainString() + "%" }
            Text(
                if (e != null) "${e.title ?: e.asset ?: ""}，${apr}APR" else "",
                style = lb(15.sp, FontWeight.Medium), color = p.textPrimary, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
        }
        PageCount(page, count, Modifier.align(Alignment.BottomEnd).padding(bottom = 10.dp))
    }
}

// ── 榜单（ref/2）──
@Composable
private fun Ranking(
    m: click.cowa.linbit.data.MarketState, watch: List<String>,
    tab: Int, onTab: (Int) -> Unit, kind: Int, onKind: (Int) -> Unit, onMore: () -> Unit,
) {
    val p = LB.p
    Column(Modifier.fillMaxWidth()) {
        PrimaryTabs(listOf("自选", "热门", "涨幅榜", "跌幅榜", "成交额榜"), tab, onTab, Modifier.padding(horizontal = Spacing.pageH).padding(top = 16.dp), size = 16.sp, underline = true)
        ChipBar(listOf("现货", "合约"), kind, onKind, Modifier.padding(vertical = 12.dp))
        Row(Modifier.fillMaxWidth().height(24.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
            Text("交易对", style = lb(FontSize.small), color = p.textSecondary, modifier = Modifier.weight(1f))
            Text("最新价", style = lb(FontSize.small), color = p.textSecondary)
            Text("涨跌幅", Modifier.width(Metrics.changeBadgeW + 12.dp), style = lb(FontSize.small), color = p.textSecondary, textAlign = androidx.compose.ui.text.style.TextAlign.End)
        }
        val rows = rankingRows(tab, if (kind == 0) MarketKind.SPOT else MarketKind.FUTURES, m, watch)
        if (rows.isEmpty()) EmptyState(if (tab == 0) "暂无自选" else "暂无数据", Modifier.fillMaxWidth().height(160.dp))
        rows.forEach { MarketRowView(it, m.cnyRate, showIcon = false, compactPair = true, onClick = onMore) }
        Row(Modifier.fillMaxWidth().height(44.dp).pressable(scale = 1f, onClick = onMore), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.Center) {
            Text("查看更多", style = lb(FontSize.caption), color = p.textSecondary)
            LbIcon(R.drawable.ic_arrow_right, 12.dp, p.textSecondary)
        }
    }
}
