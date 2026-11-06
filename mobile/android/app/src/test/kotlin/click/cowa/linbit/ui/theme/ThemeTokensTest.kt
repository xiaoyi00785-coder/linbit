package click.cowa.linbit.ui.theme

import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 明暗配色 / 字号 / 间距令牌锁。
 * 对照 mobile/tokens/tokens.json 与 iOS DesignSystem/Tokens.swift；
 * 本测试通过 ≠ 真机视觉验收，亦 ≠ ref 截图像素对齐。
 */
class ThemeTokensTest {
    @Test fun lightPaletteMatchesSharedTokens() {
        assertEquals(Color(0xFFFFFFFF), LightPalette.bg)
        assertEquals(Color(0xFF080808), LightPalette.textPrimary)
        assertEquals(Color(0xFF7F7F7F), LightPalette.textSecondary)
        assertEquals(Color(0xFFA3A3A3), LightPalette.textTertiary)
        assertEquals(Color(0xFFFFFFFF), LightPalette.textOnPrimary)
        assertEquals(Color(0xFF111111), LightPalette.primary)
        assertEquals(Color(0xFF9BCB2A), LightPalette.brand)
        assertEquals(Color(0xFF5FB971), LightPalette.up)
        assertEquals(Color(0xFFDD5754), LightPalette.down)
        assertEquals(Color(0xFFF3F3F3), LightPalette.fill)
        assertFalse(LightPalette.isDark)
    }

    @Test fun darkPaletteMatchesSharedTokensAndIos() {
        assertEquals(Color(0xFF0B0B0C), DarkPalette.bg)
        assertEquals(Color(0xFF0B0B0C), DarkPalette.textOnPrimary)
        assertEquals(Color(0xFFF5F5F5), DarkPalette.textPrimary)
        assertEquals(Color(0xFF8C8C8F), DarkPalette.textSecondary)
        assertEquals(Color(0xFFABE127), DarkPalette.brand)
        assertEquals(Color(0xFFF5F5F5), DarkPalette.primary)
        assertEquals(Color(0xFF161618), DarkPalette.bgElevated)
        assertTrue(DarkPalette.isDark)
    }

    @Test fun fontSizesMatchDesignScale() {
        assertEquals(40f, FontSize.assetHero.value)
        assertEquals(18f, FontSize.tabPrimary.value)
        assertEquals(17f, FontSize.title.value)
        assertEquals(15f, FontSize.body.value)
        assertEquals(14f, FontSize.label.value)
        assertEquals(13f, FontSize.caption.value)
        assertEquals(12f, FontSize.small.value)
        assertEquals(11f, FontSize.tabBar.value)
    }

    @Test fun spacingAndRadiusMatchSharedTokens() {
        assertEquals(16f, Spacing.pageH.value)
        assertEquals(12f, Spacing.rowV.value)
        assertEquals(8f, Spacing.sm.value)
        assertEquals(16f, Radius.sheet.value)
        assertEquals(8f, Radius.md.value)
    }
}
