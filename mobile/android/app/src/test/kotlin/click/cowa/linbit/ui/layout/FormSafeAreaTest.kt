package click.cowa.linbit.ui.layout

import click.cowa.linbit.ui.theme.LayoutAdapt
import click.cowa.linbit.ui.theme.Spacing
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 表单安全区/键盘相关纯逻辑与档位约定（离屏 ≠ 真机 IME）。
 * 实际 WindowInsets 依赖设备；此处锁定 contentBottomExtra 与 pageH 档位，
 * 避免交易/资产表单底垫被回退。
 */
class FormSafeAreaTest {
    @Test fun contentBottomExtraGestureVsKeys() {
        assertEquals(8, LayoutAdapt.contentBottomExtra(gestureNav = true).value.toInt())
        assertEquals(16, LayoutAdapt.contentBottomExtra(gestureNav = false).value.toInt())
    }

    @Test fun pageHCoversTradeFormWidths() {
        assertEquals(12, LayoutAdapt.pageH(320).value.toInt())
        assertEquals(Spacing.pageH.value.toInt(), LayoutAdapt.pageH(360).value.toInt())
        assertEquals(20, LayoutAdapt.pageH(412).value.toInt())
    }

    @Test fun tabsScrollableForAssetsSevenTabsOnCompact() {
        assertTrue(LayoutAdapt.tabsScrollable(320, 7))
        assertTrue(LayoutAdapt.tabsScrollable(390, 7))
    }

    @Test fun titleMaxLinesForLargeFont() {
        assertEquals(1, LayoutAdapt.titleMaxLines(1.0f))
        assertEquals(2, LayoutAdapt.titleMaxLines(1.3f))
        assertEquals(2, LayoutAdapt.titleMaxLines(1.5f))
    }
}
