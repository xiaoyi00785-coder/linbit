package click.cowa.linbit.feature.profile

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import click.cowa.linbit.R
import click.cowa.linbit.ui.components.LbIcon
import click.cowa.linbit.ui.components.pressable
import click.cowa.linbit.ui.theme.*
import coil.compose.SubcomposeAsyncImage

@Composable
fun ProfileNavBar(title: String, onBack: () -> Unit, trailing: (@Composable () -> Unit)? = null) {
    val p = LB.p
    Row(Modifier.fillMaxWidth().height(44.dp).padding(horizontal = Spacing.pageH), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(32.dp).pressable(scale = 1f, onClick = onBack), contentAlignment = Alignment.CenterStart) {
            LbIcon(R.drawable.ic_arrow_left, 22.dp, p.textPrimary)
        }
        Text(title, Modifier.weight(1f), style = lb(FontSize.title, FontWeight.SemiBold), color = p.textPrimary)
        if (trailing != null) trailing() else Spacer(Modifier.size(32.dp))
    }
}

@Composable
fun ProfileRow(
    title: String,
    value: String? = null,
    destructive: Boolean = false,
    onClick: () -> Unit,
) {
    val p = LB.p
    Row(
        Modifier.fillMaxWidth().height(50.dp).pressable(scale = 1f, onClick = onClick).padding(horizontal = Spacing.pageH),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = lb(FontSize.label), color = if (destructive) p.down else p.textPrimary)
        Spacer(Modifier.weight(1f))
        if (value != null) Text(value, style = lb(FontSize.label), color = p.textSecondary, maxLines = 1)
        Spacer(Modifier.width(8.dp))
        LbIcon(R.drawable.ic_chevron_right, 13.dp, p.textTertiary)
    }
}

@Composable
fun ProfileAvatar(url: String?, size: androidx.compose.ui.unit.Dp = 68.dp) {
    val p = LB.p
    val fallback: @Composable () -> Unit = {
        Box(Modifier.size(size).background(p.fill, RoundedCornerShape(Radius.xl)), contentAlignment = Alignment.Center) {
            LbIcon(R.drawable.ic_avatar, size * 0.7f, p.brand)
        }
    }
    if (url.isNullOrBlank()) fallback() else SubcomposeAsyncImage(
        model = url, contentDescription = "头像", contentScale = ContentScale.Crop,
        modifier = Modifier.size(size).background(p.fill, RoundedCornerShape(Radius.xl)),
        loading = { fallback() }, error = { fallback() },
    )
}

@Composable
fun ProfileLoading() {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(Modifier.size(28.dp), color = LB.p.brand, strokeWidth = 2.dp)
    }
}

@Composable
fun ProfileError(message: String, onRetry: () -> Unit) {
    val p = LB.p
    Column(Modifier.fillMaxWidth().padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(message, style = lb(FontSize.body), color = p.textSecondary)
        Spacer(Modifier.height(12.dp))
        Text("重试", Modifier.pressable(scale = 1f, onClick = onRetry), style = lb(FontSize.label, FontWeight.Medium), color = p.brand)
    }
}

@Composable
fun InfoNote(text: String, warning: Boolean = false) {
    val p = LB.p
    val bg = if (warning) Color(0xFFFFF1E5) else p.fill
    val fg = if (warning) Color(0xFFD97706) else p.textSecondary
    Row(Modifier.fillMaxWidth().background(bg, RoundedCornerShape(Radius.md)).padding(12.dp)) {
        Text(if (warning) "!" else "i", style = lb(FontSize.small, FontWeight.Bold), color = fg)
        Spacer(Modifier.width(8.dp))
        Text(text, style = lb(FontSize.small), color = fg)
    }
}

@Composable
fun StatusTag(text: String, color: Color) {
    Text(text, Modifier.background(color.copy(alpha = 0.12f), RoundedCornerShape(Radius.xs)).padding(horizontal = 6.dp, vertical = 2.dp), style = lb(FontSize.small), color = color)
}

@Composable
fun CurrentBadge() {
    val p = LB.p
    Text("当前登录", Modifier.background(p.brand, RoundedCornerShape(topEnd = Radius.lg, bottomStart = Radius.sm)).padding(horizontal = 7.dp, vertical = 3.dp), style = lb(10.sp, FontWeight.Medium), color = Color(0xFF080808))
}

@Composable
fun CheckCircle(on: Boolean) {
    val p = LB.p
    Box(Modifier.size(16.dp).border(1.dp, if (on) p.brand else p.textTertiary, CircleShape).background(if (on) p.brand else Color.Transparent, CircleShape), contentAlignment = Alignment.Center) {
        if (on) Text("✓", style = lb(10.sp, FontWeight.Bold), color = Color(0xFF080808))
    }
}
