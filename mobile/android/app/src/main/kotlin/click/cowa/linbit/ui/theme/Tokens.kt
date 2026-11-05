// 由 mobile/tools/gen_android_tokens.py 从 mobile/tokens/tokens.json 生成，请勿手改。
package click.cowa.linbit.ui.theme

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** 颜色令牌（与 iOS Palette 一一对应）。 */
data class Palette(
    val bg: Color,
    val bgElevated: Color,
    val fill: Color,
    val fillStrong: Color,
    val border: Color,
    val divider: Color,
    val textPrimary: Color,
    val textSecondary: Color,
    val textTertiary: Color,
    val textOnPrimary: Color,
    val primary: Color,
    val brand: Color,
    val up: Color,
    val down: Color,
    val upTint: Color,
    val downTint: Color,
    val onChange: Color,
    val tabBarBg: Color,
    val tabBarSelected: Color,
    val tabBarBorder: Color,
    val iconTileBg: Color,
    val badgeDot: Color,
    val mask: Color,
    val isDark: Boolean,
)

val LightPalette = Palette(
    bg = Color(0xFFFFFFFF),
    bgElevated = Color(0xFFFFFFFF),
    fill = Color(0xFFF3F3F3),
    fillStrong = Color(0xFFE9E9E9),
    border = Color(0xFFEDEDED),
    divider = Color(0xFFF2F2F2),
    textPrimary = Color(0xFF080808),
    textSecondary = Color(0xFF7F7F7F),
    textTertiary = Color(0xFFA3A3A3),
    textOnPrimary = Color(0xFFFFFFFF),
    primary = Color(0xFF111111),
    brand = Color(0xFF9BCB2A),
    up = Color(0xFF5FB971),
    down = Color(0xFFDD5754),
    upTint = Color(0xFFEFFAF1),
    downTint = Color(0xFFFDEEEE),
    onChange = Color(0xFFFFFFFF),
    tabBarBg = Color(0xE6FCFCFE),
    tabBarSelected = Color(0xFFE9E9EB),
    tabBarBorder = Color(0xFFE4E4E6),
    iconTileBg = Color(0xFFF3F3F3),
    badgeDot = Color(0xFF9BCB2A),
    mask = Color(0x66000000),
    isDark = false,
)

val DarkPalette = Palette(
    bg = Color(0xFF070808),
    bgElevated = Color(0xFF161618),
    fill = Color(0xFF1E1E20),
    fillStrong = Color(0xFF2A2A2D),
    border = Color(0xFF2A2A2D),
    divider = Color(0xFF1C1C1E),
    textPrimary = Color(0xFFF5F5F5),
    textSecondary = Color(0xFF8C8C8F),
    textTertiary = Color(0xFF5E5E62),
    textOnPrimary = Color(0xFF070808),
    primary = Color(0xFFF5F5F5),
    brand = Color(0xFFABE127),
    up = Color(0xFF5FB971),
    down = Color(0xFFDD5754),
    upTint = Color(0xFF14261A),
    downTint = Color(0xFF2C1616),
    onChange = Color(0xFFFFFFFF),
    tabBarBg = Color(0xE61C1C1E),
    tabBarSelected = Color(0xFF2E2E31),
    tabBarBorder = Color(0xFF2A2A2D),
    iconTileBg = Color(0xFF1E1E20),
    badgeDot = Color(0xFFABE127),
    mask = Color(0xA0000000),
    isDark = true,
)

object FontSize {
    val assetHero = 40.sp
    val tabPrimary = 18.sp
    val title = 17.sp
    val symbol = 17.sp
    val symbolWithIcon = 16.sp
    val body = 15.sp
    val price = 16.sp
    val change = 15.sp
    val label = 14.sp
    val tabSecondary = 15.sp
    val chip = 14.sp
    val caption = 13.sp
    val small = 12.sp
    val tabBar = 11.sp
}

object Spacing {
    val xxs = 2.dp
    val xs = 4.dp
    val sm = 8.dp
    val md = 12.dp
    val lg = 16.dp
    val xl = 20.dp
    val xxl = 24.dp
    val pageH = 16.dp
    val rowV = 12.dp
}

object Radius {
    val xs = 4.dp
    val sm = 6.dp
    val md = 8.dp
    val lg = 10.dp
    val xl = 12.dp
    val sheet = 16.dp
    val pill = 999.dp
}

object Metrics {
    val searchBarH = 31.dp
    val heroButtonW = 80.dp
    val heroButtonH = 32.dp
    val taskCardH = 44.dp
    val bannerH = 60.dp
    val gridIcon = 40.dp
    val gridIconGlyph = 22.dp
    val marketRowH = 56.dp
    val coinIcon = 32.dp
    val changeBadgeW = 76.dp
    val changeBadgeH = 31.dp
    val chipH = 24.dp
    val tabBarH = 61.dp
    val tabBarInset = 21.dp
    val navIcon = 22.dp
}
