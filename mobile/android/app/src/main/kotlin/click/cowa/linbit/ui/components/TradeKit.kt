package click.cowa.linbit.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum

/**
 * 交易页通用控件（现货 / 合约共用）：分段、选择框、数字输入框、百分比滑杆、复选行、键值行。
 * 数值尺寸来自对照图 ref/11、ref/28（1170×2532 = 3×390pt）的像素测量。
 */

/** 二段胶囊（买入/卖出、开仓/平仓）：灰底圆角 8，选中项为纯色圆角块。 */
@Composable
fun PillSegment(
    items: List<String>,
    selected: Int,
    onSelect: (Int) -> Unit,
    modifier: Modifier = Modifier,
    height: Dp = 30.dp,
    activeColors: List<Color> = listOf(LB.p.up, LB.p.down),
    activeText: Color = Color.White,
) {
    val p = LB.p
    Row(
        modifier.fillMaxWidth().height(height).clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(2.dp),
        horizontalArrangement = Arrangement.spacedBy(0.dp),
    ) {
        items.forEachIndexed { i, t ->
            val sel = i == selected
            Box(
                Modifier.weight(1f).fillMaxHeight()
                    .clip(RoundedCornerShape(Radius.md - 1.dp))
                    .background(if (sel) activeColors.getOrElse(i) { activeColors.last() } else Color.Transparent)
                    .pressable(scale = 1f) { onSelect(i) },
                contentAlignment = Alignment.Center,
            ) { Text(t, style = lb(FontSize.body, FontWeight.Medium), color = if (sel) activeText else p.textPrimary, maxLines = 1) }
        }
    }
}

/** 带 ⓘ 与下拉三角的选择框（「限价单 ▾」）。 */
@Composable
fun SelectBox(title: String, modifier: Modifier = Modifier, height: Dp = 30.dp, info: Boolean = true, onClick: () -> Unit) {
    val p = LB.p
    Row(
        modifier.fillMaxWidth().height(height).clip(RoundedCornerShape(Radius.md)).background(p.fill)
            .pressable(scale = 1f, onClick = onClick).padding(horizontal = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (info) { ThemedIcon(R.drawable.ic_duo_info_circle_light, R.drawable.ic_duo_info_circle_dark, 14.dp); HSpace(6.dp) }
        Text(title, Modifier.weight(1f), style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary, maxLines = 1)
        LbIcon(R.drawable.ic_caret_down, 16.dp, p.textSecondary)
    }
}

/** 数字输入框：空时「标签」居中灰字，有值时标签缩小置顶；右侧单位。 */
@Composable
fun NumberBox(
    label: String,
    unit: String,
    value: String,
    onChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    height: Dp = 38.dp,
    readOnly: Boolean = false,
    onUnitClick: (() -> Unit)? = null,
) {
    val p = LB.p
    Row(
        modifier.fillMaxWidth().height(height).clip(RoundedCornerShape(Radius.md)).background(p.fill).padding(horizontal = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
            Column {
                if (value.isNotEmpty()) Text(label, style = lb(11.sp), color = p.textSecondary, maxLines = 1)
                BasicTextField(
                    value = value, onValueChange = { if (!readOnly) onChange(it.filterDecimal()) }, singleLine = true,
                    textStyle = lbNum(FontSize.body).copy(color = p.textPrimary), cursorBrush = SolidColor(p.textPrimary),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal), readOnly = readOnly,
                    decorationBox = { inner ->
                        Box { if (value.isEmpty()) Text(label, style = lb(FontSize.body), color = p.textSecondary, maxLines = 1); inner() }
                    },
                )
            }
        }
        val u = Modifier.let { if (onUnitClick != null) it.pressable(scale = 1f, onClick = onUnitClick) else it }
        Row(u, verticalAlignment = Alignment.CenterVertically) {
            Text(unit, style = lb(FontSize.body), color = p.textSecondary, maxLines = 1)
            if (onUnitClick != null) LbIcon(R.drawable.ic_caret_down, 16.dp, p.textSecondary)
        }
    }
}

/** 仅保留数字与一个小数点。 */
fun String.filterDecimal(): String {
    var dot = false
    return filter { c -> if (c == '.') (!dot).also { if (it) dot = true } else c.isDigit() }
}

/** 百分比滑杆：0/25/50/75/100 五个菱形节点，黑色菱形滑块（对照图 ref/28）。 */
@Composable
fun PercentSlider(value: Float, onChange: (Float) -> Unit, modifier: Modifier = Modifier, tint: Color = LB.p.textPrimary) {
    val p = LB.p
    var w by remember { mutableStateOf(1f) }
    fun at(x: Float) = (x / w).coerceIn(0f, 1f)
    Box(
        modifier.fillMaxWidth().height(16.dp).onSizeChanged { w = it.width.toFloat().coerceAtLeast(1f) }
            .pointerInput(Unit) { detectTapGestures { onChange(at(it.x)) } }
            .pointerInput(Unit) { detectDragGestures { c, _ -> onChange(at(c.position.x)) } },
    ) {
        Canvas(Modifier.fillMaxWidth().height(16.dp)) {
            val cy = size.height / 2
            val inset = 5.dp.toPx()
            val x0 = inset; val x1 = size.width - inset
            drawLine(p.border, Offset(x0, cy), Offset(x1, cy), strokeWidth = 1.dp.toPx())
            fun diamond(cx: Float, r: Float, color: Color) = rotate(45f, Offset(cx, cy)) {
                drawRect(color, Offset(cx - r, cy - r), Size(r * 2, r * 2))
            }
            for (i in 0..4) diamond(x0 + (x1 - x0) * i / 4f, 3.dp.toPx(), p.fillStrong)
            diamond(x0 + (x1 - x0) * value, 5.dp.toPx(), tint)
        }
    }
}

/** 复选行：18dp 圆角描边框 + 标签（dashed 时标签下带点划线，对照图「止盈/止损」）。 */
@Composable
fun CheckRow(on: Boolean, label: String, modifier: Modifier = Modifier, dashed: Boolean = false, onChange: (Boolean) -> Unit) {
    val p = LB.p
    Row(
        modifier.clickable(remember { MutableInteractionSource() }, indication = null) { onChange(!on) },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier.size(16.dp).clip(RoundedCornerShape(4.dp)).background(if (on) p.textPrimary else Color.Transparent)
                .border(BorderStroke(1.2.dp, if (on) p.textPrimary else p.textTertiary), RoundedCornerShape(4.dp)),
            contentAlignment = Alignment.Center,
        ) { if (on) LbIcon(R.drawable.ic_check, 12.dp, p.bg) }
        HSpace(8.dp)
        if (dashed) DashedText(label) else Text(label, style = lb(FontSize.caption), color = p.textPrimary)
    }
}

/** 点划线下划线文字（「手续费率」「止盈/止损」）。 */
@Composable
fun DashedText(text: String, modifier: Modifier = Modifier, color: Color = LB.p.textPrimary, size: androidx.compose.ui.unit.TextUnit = FontSize.caption) {
    val line = LB.p.textTertiary
    Text(
        text, style = lb(size), color = color, maxLines = 1,
        modifier = modifier.drawDashedUnderline(line),
    )
}

private fun Modifier.drawDashedUnderline(color: Color) = this.drawBehind {
    val y = size.height + 1.dp.toPx()
    drawLine(
        color, Offset(0f, y), Offset(size.width, y), strokeWidth = 1.dp.toPx(),
        pathEffect = androidx.compose.ui.graphics.PathEffect.dashPathEffect(floatArrayOf(2.dp.toPx(), 2.dp.toPx())),
    )
}

/** 键值行（「可用  0 USDT」）：左灰右黑，12.5sp。 */
@Composable
fun KvRow(k: String, v: String, modifier: Modifier = Modifier, trailing: (@Composable () -> Unit)? = null, kDashed: Boolean = false) {
    val p = LB.p
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        if (kDashed) DashedText(k, color = p.textSecondary, size = FontSize.small) else Text(k, style = lb(FontSize.small), color = p.textSecondary, maxLines = 1)
        Box(Modifier.weight(1f))
        Text(v, style = lbNum(FontSize.small), color = p.textPrimary, maxLines = 1)
        trailing?.invoke()
    }
}

/** 灰底圆角小按钮（「全仓/合仓」「100x」「全部撤销」）。 */
@Composable
fun ChipButton(title: String, modifier: Modifier = Modifier, color: Color = LB.p.textPrimary, bg: Color = LB.p.fill, onClick: () -> Unit) {
    Box(
        modifier.height(24.dp).clip(RoundedCornerShape(Radius.sm)).background(bg).pressable(scale = 1f, onClick = onClick).padding(horizontal = 10.dp),
        contentAlignment = Alignment.Center,
    ) { Text(title, style = lb(FontSize.caption), color = color, maxLines = 1) }
}

/** 弹层选项行：左文案（可带副标题），右侧选中对勾（黑底圆）。 */
@Composable
fun SheetOptionRow(title: String, selected: Boolean, modifier: Modifier = Modifier, subtitle: String? = null, onClick: () -> Unit) {
    val p = LB.p
    Row(
        modifier.fillMaxWidth().pressable(scale = 1f, onClick = onClick).padding(horizontal = Spacing.pageH, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, style = lb(FontSize.body, FontWeight.Medium), color = p.textPrimary)
            if (subtitle != null) Text(subtitle, style = lb(FontSize.small), color = p.textTertiary)
        }
        if (selected) ThemedIcon(R.drawable.ic_mc_check_circle_light, R.drawable.ic_mc_check_circle_dark, 22.dp)
    }
}

@Composable
fun DownTriangle(modifier: Modifier = Modifier, tint: Color = LB.p.textSecondary, size: Dp = 14.dp) =
    LbIcon(R.drawable.ic_caret_down, size, tint, modifier)

@Composable
fun UpArrow(modifier: Modifier = Modifier, tint: Color = LB.p.textSecondary) =
    LbIcon(R.drawable.ic_chevron_down, 16.dp, tint, modifier.rotate(180f))

/** 日 / 夜两版的多色图标。 */
@Composable
fun ThemedIcon(@androidx.annotation.DrawableRes light: Int, @androidx.annotation.DrawableRes dark: Int, size: Dp, modifier: Modifier = Modifier) =
    LbImageIcon(if (LB.p.isDark) dark else light, size, modifier)
