package click.cowa.linbit.ui.theme

import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * 屏幕与系统字体适配（纯逻辑，可供单测）。
 * 覆盖宽度档位对应常见机型约 320 / 360 / 412 dp；不等于真机验收。
 */
object LayoutAdapt {
    enum class WidthBucket { COMPACT, REGULAR, EXPANDED }

    fun widthBucket(widthDp: Int): WidthBucket = when {
        widthDp < 360 -> WidthBucket.COMPACT
        widthDp < 412 -> WidthBucket.REGULAR
        else -> WidthBucket.EXPANDED
    }

    /** 页边距：紧凑屏收窄，避免合约顶栏与历史筛选项挤爆。 */
    fun pageH(widthDp: Int): Dp = when (widthBucket(widthDp)) {
        WidthBucket.COMPACT -> 12.dp
        WidthBucket.REGULAR -> Spacing.pageH
        WidthBucket.EXPANDED -> 20.dp
    }

    fun tabSpacing(widthDp: Int): Dp = when (widthBucket(widthDp)) {
        WidthBucket.COMPACT -> 12.dp
        WidthBucket.REGULAR -> 16.dp
        WidthBucket.EXPANDED -> 20.dp
    }

    /** 大字体时标题允许两行，避免挖孔/刘海机顶栏裁切。 */
    fun titleMaxLines(fontScale: Float): Int = if (fontScale >= 1.3f) 2 else 1

    /** 窄屏或顶栏项 ≥4 时启用横向滚动。 */
    fun tabsScrollable(widthDp: Int, itemCount: Int): Boolean =
        itemCount >= 4 || widthBucket(widthDp) == WidthBucket.COMPACT

    /**
     * 内容区底垫：悬浮 Tab + 系统导航预留。
     * gestureNav=true 对应全面屏手势（通常 inset 更小）；三键导航可多留。
     * 实际 inset 仍以 WindowInsets / navigationBarsPadding 为准。
     */
    fun contentBottomExtra(gestureNav: Boolean): Dp = if (gestureNav) 8.dp else 16.dp
}
