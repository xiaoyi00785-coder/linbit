package click.cowa.linbit.ui.components

import androidx.annotation.DrawableRes
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.composed
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import click.cowa.linbit.R
import click.cowa.linbit.core.util.Fmt
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum
import coil.compose.SubcomposeAsyncImage
import java.math.BigDecimal

/** 点击态：按下变暗 + 轻微缩放（对应 iOS PressableStyle），无涟漪。 */
fun Modifier.pressable(enabled: Boolean = true, scale: Float = 0.98f, onClick: () -> Unit): Modifier = composed2(enabled, scale, onClick)

@Composable
private fun Modifier.pressableImpl(enabled: Boolean, scale: Float, onClick: () -> Unit): Modifier {
    val source = remember { MutableInteractionSource() }
    val pressed by source.collectIsPressedAsState()
    val a by animateFloatAsState(if (pressed) 0.6f else 1f, label = "pressAlpha")
    val s by animateFloatAsState(if (pressed) scale else 1f, label = "pressScale")
    return this
        .alpha(a)
        .scale(s)
        .clickable(interactionSource = source, indication = null, enabled = enabled, onClick = onClick)
}

private fun Modifier.composed2(enabled: Boolean, scale: Float, onClick: () -> Unit): Modifier =
    composed { pressableImpl(enabled, scale, onClick) }

/** 单色矢量图标（assets 来自 mobile/icons 下的 SVG 经 gen_android_vectors.py 转换）。 */
@Composable
fun LbIcon(@DrawableRes id: Int, size: Dp = Metrics.navIcon, tint: Color = LB.p.textPrimary, modifier: Modifier = Modifier) {
    Icon(painterResource(id), contentDescription = null, tint = tint, modifier = modifier.size(size))
}

/** 保留原色的多色图标（如首页宫格）。 */
@Composable
fun LbImageIcon(@DrawableRes id: Int, size: Dp, modifier: Modifier = Modifier) {
    Image(painterResource(id), contentDescription = null, modifier = modifier.size(size))
}

/** 品牌符号：单一来源 mobile/android/brand/mark*.svg → drawable/ic_mark / ic_mark_lime（浅色底用深色符号，深色底用品牌绿符号）。 */
@Composable
fun LinbitLogo(size: Dp, modifier: Modifier = Modifier) {
    LbImageIcon(if (LB.p.isDark) R.drawable.ic_mark_lime else R.drawable.ic_mark, size, modifier)
}

/** 横版 Logo（符号 + Linbit 字标）。宽 [width]，高按 1427:540 等比。 */
@Composable
fun LinbitWordmark(width: Dp, modifier: Modifier = Modifier) {
    Image(
        painterResource(if (LB.p.isDark) R.drawable.ic_wordmark_dark else R.drawable.ic_wordmark), null,
        modifier.width(width).height(width * 540f / 1427f),
    )
}

enum class ButtonKind { PRIMARY, SECONDARY, BUY, SELL, OUTLINE }

/** 主按钮：黑底（夜间白底）/ 买绿 / 卖红 / 次级灰；禁用灰；加载转圈（对应 iOS LBButton，高 44，圆角 8）。 */
@Composable
fun LbButton(
    title: String,
    modifier: Modifier = Modifier,
    kind: ButtonKind = ButtonKind.PRIMARY,
    height: Dp = 44.dp,
    enabled: Boolean = true,
    loading: Boolean = false,
    radius: Dp = Radius.md,
    subtitle: String? = null,
    onClick: () -> Unit,
) {
    val p = LB.p
    val bg = if (!enabled) p.fill else when (kind) {
        ButtonKind.PRIMARY -> p.primary
        ButtonKind.SECONDARY -> p.fill
        ButtonKind.BUY -> p.up
        ButtonKind.SELL -> p.down
        ButtonKind.OUTLINE -> Color.Transparent
    }
    val fg = if (!enabled) p.textTertiary else when (kind) {
        ButtonKind.PRIMARY -> p.textOnPrimary
        ButtonKind.SECONDARY, ButtonKind.OUTLINE -> p.textPrimary
        ButtonKind.BUY, ButtonKind.SELL -> Color.White
    }
    Box(
        modifier
            .fillMaxWidth()
            .height(height)
            .clip(RoundedCornerShape(radius))
            .background(bg)
            .let { if (kind == ButtonKind.OUTLINE) it.border(BorderStroke(1.dp, p.border), RoundedCornerShape(radius)) else it }
            .pressable(enabled && !loading, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        if (loading) CircularProgressIndicator(color = fg, strokeWidth = 2.dp, modifier = Modifier.size(20.dp))
        else if (subtitle != null) Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(title, style = lb(FontSize.body, FontWeight.Medium), color = fg)
            Text(subtitle, style = lbNum(10.sp), color = fg.copy(alpha = 0.8f), maxLines = 1)
        } else Text(title, style = lb(FontSize.body, FontWeight.Medium), color = fg)
    }
}

/** 涨跌幅色块（对照图 #8：76×31，圆角 6，15sp medium 白字）。 */
@Composable
fun ChangeBadge(percent: BigDecimal, modifier: Modifier = Modifier) {
    val p = LB.p
    Box(
        modifier
            .size(Metrics.changeBadgeW, Metrics.changeBadgeH)
            .clip(RoundedCornerShape(Radius.sm))
            .background(if (percent.signum() < 0) p.down else p.up),
        contentAlignment = Alignment.Center,
    ) {
        Text(Fmt.percent(percent), style = lbNum(FontSize.change, FontWeight.Medium), color = p.onChange, maxLines = 1)
    }
}

/** 胶囊筛选（对照图 #8「全部/公链…」：高 24，选中灰底）。 */
@Composable
fun ChipBar(items: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    val p = LB.p
    LazyRow(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = Spacing.pageH),
    ) {
        itemsIndexed(items) { i, t ->
            val sel = i == selected
            Box(
                Modifier
                    .height(Metrics.chipH)
                    .clip(RoundedCornerShape(Radius.sm))
                    .background(if (sel) p.fill else Color.Transparent)
                    .pressable(scale = 1f) { onSelect(i) }
                    .padding(horizontal = 10.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    t, maxLines = 1, softWrap = false,
                    style = lb(FontSize.chip, if (sel) FontWeight.Medium else FontWeight.Normal),
                    color = if (sel) p.textPrimary else p.textSecondary,
                )
            }
        }
    }
}

/** 一级 Tab（18sp，选中黑色 semibold，未选灰色；underline 时下方 16×3 胶囊）。 */
@Composable
fun PrimaryTabs(
    items: List<String>,
    selected: Int,
    onSelect: (Int) -> Unit,
    modifier: Modifier = Modifier,
    size: TextUnit = FontSize.tabPrimary,
    underline: Boolean = false,
    spacing: Dp = 20.dp,
) {
    val p = LB.p
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(spacing)) {
        items.forEachIndexed { i, t ->
            val sel = i == selected
            Column(
                Modifier.pressable(scale = 1f) { onSelect(i) },
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(t, style = lb(size, if (sel) FontWeight.SemiBold else FontWeight.Normal), color = if (sel) p.textPrimary else p.textSecondary, maxLines = 1)
                if (underline) {
                    Spacer(Modifier.height(6.dp))
                    Box(Modifier.size(16.dp, 3.dp).clip(CircleShape).background(if (sel) p.textPrimary else Color.Transparent))
                }
            }
        }
    }
}

/** 排序表头（「币种/成交额 ⇕」）。 */
@Composable
fun SortLabel(title: String, showArrow: Boolean = true, modifier: Modifier = Modifier) {
    val p = LB.p
    Row(modifier, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(title, style = lb(FontSize.caption), color = p.textTertiary)
        if (showArrow) LbIcon(R.drawable.ic_sort, 10.dp, p.textTertiary)
    }
}

/** 币种图标：优先本地 CC0 矢量图；未打包时远程加载，仍失败才显示首字母圆。 */
@Composable
fun CoinIcon(base: String, size: Dp = Metrics.coinIcon, modifier: Modifier = Modifier) {
    val local = remember(base) { CoinIcons.drawable(base) }
    if (local != null) {
        Image(painterResource(local), contentDescription = null, modifier = modifier.size(size))
        return
    }
    val p = LB.p
    val fallback: @Composable () -> Unit = {
        Box(modifier.size(size).clip(CircleShape).background(p.fillStrong), contentAlignment = Alignment.Center) {
            Text(base.trim().firstOrNull()?.uppercase() ?: "?", style = lb(FontSize.small, FontWeight.SemiBold), color = p.textSecondary)
        }
    }
    SubcomposeAsyncImage(
        model = "https://assets.coincap.io/assets/icons/${base.trim().lowercase()}@2x.png",
        contentDescription = null,
        contentScale = ContentScale.Fit,
        modifier = modifier.size(size).clip(CircleShape),
        loading = { fallback() },
        error = { fallback() },
    )
}

/** 空状态（灰色文件夹插画 + 文案，对照图「暂无数据」「划转资金，开始现货交易」）。 */
@Composable
fun EmptyState(text: String = "暂无数据", modifier: Modifier = Modifier, iconSize: Dp = 64.dp, verticalPadding: Dp = 24.dp, content: (@Composable () -> Unit)? = null) {
    val p = LB.p
    Column(modifier.fillMaxWidth().padding(vertical = verticalPadding), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(Spacing.sm)) {
        LbImageIcon(R.drawable.ic_empty_folder, iconSize)
        Text(text, style = lb(FontSize.caption), color = if (content != null) p.textPrimary else p.textSecondary, textAlign = TextAlign.Center)
        content?.invoke()
    }
}

/** 一行 14sp 虚线下划线文字链接（「忘记密码?」「收不到验证码?」）。 */
@Composable
fun DashedLink(title: String, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val p = LB.p
    val dash = p.textTertiary
    Column(modifier.pressable(onClick = onClick).width(androidx.compose.foundation.layout.IntrinsicSize.Max)) {
        Text(title, style = lb(FontSize.label), color = p.textPrimary)
        Spacer(Modifier.height(3.dp))
        androidx.compose.foundation.Canvas(Modifier.fillMaxWidth().height(1.dp)) {
            drawLine(
                color = dash, start = androidx.compose.ui.geometry.Offset(0f, 0f), end = androidx.compose.ui.geometry.Offset(size.width, 0f),
                strokeWidth = 1.dp.toPx(),
                pathEffect = androidx.compose.ui.graphics.PathEffect.dashPathEffect(floatArrayOf(3.dp.toPx(), 3.dp.toPx())),
            )
        }
    }
}

@Composable
fun HSpace(w: Dp) = Spacer(Modifier.width(w))

@Composable
fun VSpace(h: Dp) = Spacer(Modifier.height(h))
