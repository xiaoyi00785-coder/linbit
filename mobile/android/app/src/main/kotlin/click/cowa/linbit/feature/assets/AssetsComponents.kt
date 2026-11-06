package click.cowa.linbit.feature.assets

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.FontSize
import click.cowa.linbit.ui.theme.LB
import click.cowa.linbit.ui.theme.Metrics
import click.cowa.linbit.ui.theme.Radius
import click.cowa.linbit.ui.theme.Spacing
import click.cowa.linbit.ui.theme.lb
import click.cowa.linbit.ui.theme.lbNum

@Composable
fun AssetsNavBar(title: String, onBack: (() -> Unit)? = null, trailing: (@Composable () -> Unit)? = null) {
    val p = LB.p
    Row(
        Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onBack != null) {
            LbIcon(R.drawable.ic_arrow_left, Metrics.navIcon, p.textPrimary, Modifier.pressable(scale = 1f, onClick = onBack))
            Spacer(Modifier.width(8.dp))
        } else Spacer(Modifier.width(Metrics.navIcon + 8.dp))
        Text(title, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        if (trailing != null) trailing() else Spacer(Modifier.width(Metrics.navIcon))
    }
}

@Composable
fun WarnBanner(text: String, modifier: Modifier = Modifier) {
    val bg = androidx.compose.ui.graphics.Color(0xFFFFF1E5)
    val fg = LB.p.warning
    Row(
        modifier.fillMaxWidth().background(bg, RoundedCornerShape(Radius.md)).padding(10.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Text("!", style = lb(FontSize.label, FontWeight.Bold), color = fg)
        Spacer(Modifier.width(6.dp))
        Text(text, style = lb(FontSize.small), color = fg)
    }
}

@Composable
fun RecommendedChip() {
    val p = LB.p
    Text(
        "推荐",
        Modifier
            .background(p.brand.copy(alpha = 0.12f), RoundedCornerShape(3.dp))
            .padding(horizontal = 4.dp, vertical = 1.dp),
        style = lb(10.sp, FontWeight.Medium),
        color = p.brand,
    )
}

@Composable
fun AssetsCard(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val p = LB.p
    Column(
        modifier
            .fillMaxWidth()
            .background(p.bgElevated, RoundedCornerShape(Radius.xl))
            .border(1.dp, p.border, RoundedCornerShape(Radius.xl))
            .padding(14.dp),
    ) { content() }
}

@Composable
fun AssetKv(k: String, v: String, modifier: Modifier = Modifier) {
    val p = LB.p
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(k, style = lb(FontSize.caption), color = p.textSecondary)
        Spacer(Modifier.weight(1f))
        Text(v, style = lbNum(FontSize.caption, FontWeight.Medium), color = p.textPrimary)
    }
}

@Composable
fun PickerField(label: String, value: String, placeholder: String = "请选择", onClick: () -> Unit) {
    val p = LB.p
    Row(
        Modifier
            .fillMaxWidth()
            .height(52.dp)
            .background(p.fill, RoundedCornerShape(Radius.md))
            .pressable(scale = 1f, onClick = onClick)
            .padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(label, style = lb(11.sp), color = p.textSecondary)
            Text(
                value.ifBlank { placeholder },
                style = lb(FontSize.body, FontWeight.Medium),
                color = if (value.isBlank()) p.textTertiary else p.textPrimary,
            )
        }
        Text("▾", style = lb(FontSize.small), color = p.textSecondary)
    }
}
