package click.cowa.linbit.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import click.cowa.linbit.core.storage.ThemeMode

val LocalPalette = staticCompositionLocalOf { LightPalette }

/** 取当前主题的调色板：`LB.p.textPrimary`。 */
object LB {
    val p: Palette @Composable @ReadOnlyComposable get() = LocalPalette.current
}

/** 文字样式（对应 iOS Font.lb / lbNum）：系统字体（Roboto / Noto Sans SC），数字用等宽数字避免行情跳动时抖动。 */
fun lb(size: TextUnit, weight: FontWeight = FontWeight.Normal): TextStyle = TextStyle(fontSize = size, fontWeight = weight)

fun lbNum(size: TextUnit, weight: FontWeight = FontWeight.Normal): TextStyle =
    TextStyle(fontSize = size, fontWeight = weight, fontFeatureSettings = "tnum")

@Composable
fun resolveDark(mode: ThemeMode): Boolean = when (mode) {
    ThemeMode.SYSTEM -> isSystemInDarkTheme()
    ThemeMode.LIGHT -> false
    ThemeMode.DARK -> true
}

@Composable
fun LinbitTheme(darkTheme: Boolean, content: @Composable () -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    click.cowa.linbit.core.storage.DisplayPrefs.load(ctx)
    // Android reference palette sampled from the supplied EasiCoin screenshots.
    // Keep generated cross-platform tokens untouched.
    val base = if (darkTheme) DarkPalette else LightPalette.copy(
        fill = androidx.compose.ui.graphics.Color(0xFFF2F3F4),
        textPrimary = androidx.compose.ui.graphics.Color(0xFF101112),
        textSecondary = androidx.compose.ui.graphics.Color(0xFF808588),
        textTertiary = androidx.compose.ui.graphics.Color(0xFFA8AAAD),
        up = androidx.compose.ui.graphics.Color(0xFF31BD65),
        down = androidx.compose.ui.graphics.Color(0xFFED4C52),
        upTint = androidx.compose.ui.graphics.Color(0xFFEFFAF3),
        downTint = androidx.compose.ui.graphics.Color(0xFFFEF1F1),
    )
    val p = if (click.cowa.linbit.core.storage.DisplayPrefs.redUp) base.copy(up = base.down, down = base.up, upTint = base.downTint, downTint = base.upTint) else base
    val scheme = if (darkTheme) {
        darkColorScheme(
            primary = p.primary, onPrimary = p.textOnPrimary, background = p.bg, onBackground = p.textPrimary,
            surface = p.bg, onSurface = p.textPrimary, surfaceVariant = p.fill, onSurfaceVariant = p.textSecondary,
            outline = p.border, error = p.down,
        )
    } else {
        lightColorScheme(
            primary = p.primary, onPrimary = p.textOnPrimary, background = p.bg, onBackground = p.textPrimary,
            surface = p.bg, onSurface = p.textPrimary, surfaceVariant = p.fill, onSurfaceVariant = p.textSecondary,
            outline = p.border, error = p.down,
        )
    }
    CompositionLocalProvider(LocalPalette provides p) {
        MaterialTheme(colorScheme = scheme, content = content)
    }
}
