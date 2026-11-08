package click.cowa.linbit.feature.trade

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import click.cowa.linbit.feature.auth.LbSheet
import click.cowa.linbit.ui.components.*
import click.cowa.linbit.ui.theme.*
import kotlin.math.floor

/** Stored in candle-time / quote-price coordinates, never screen pixels. Local to the chart session. */
internal data class ChartAnchor(val timeMs: Long, val price: Double)
internal enum class DrawingTool(val title: String) { NONE("浏览 / 缩放"), HORIZONTAL("水平线"), TREND("趋势线") }
internal data class ChartDrawing(val id: Long, val tool: DrawingTool, val start: ChartAnchor, val end: ChartAnchor? = null)

internal class ChartDrawingState {
    val items = mutableStateListOf<ChartDrawing>()
    var tool by mutableStateOf(DrawingTool.NONE); private set
    var pending by mutableStateOf<ChartAnchor?>(null); private set
    var selected by mutableStateOf<Long?>(null)
    private var nextId = 1L
    fun choose(value: DrawingTool) { tool = value; pending = null; selected = null }
    fun cancelDraft() { tool = DrawingTool.NONE; pending = null; selected = null }
    fun tap(anchor: ChartAnchor) {
        if (!anchor.price.isFinite()) return
        when (tool) {
            DrawingTool.NONE -> Unit
            DrawingTool.HORIZONTAL -> { items += ChartDrawing(nextId++, tool, anchor); tool = DrawingTool.NONE }
            DrawingTool.TREND -> {
                val first = pending
                if (first == null) pending = anchor
                else if (first != anchor) { items += ChartDrawing(nextId++, tool, first, anchor); pending = null; tool = DrawingTool.NONE }
            }
        }
    }
    fun undo() { pending = null; if (items.isNotEmpty()) items.removeAt(items.lastIndex); selected = null }
    fun deleteSelected() { items.removeAll { it.id == selected }; selected = null }
    fun clear() { items.clear(); cancelDraft() }
}

internal class ChartDrawingBook {
    private val contexts = mutableMapOf<Pair<String,String>, ChartDrawingState>()
    fun state(symbol: String, interval: String) = contexts.getOrPut(symbol to interval) { ChartDrawingState() }
}

/** Nearest actual candle, including gaps. Zoom changes the visible list, not the saved timestamp. */
internal fun chartAnchorAt(candles: List<Candle>, x: Float, y: Float, low: Double, high: Double): ChartAnchor? {
    if (candles.isEmpty() || !x.isFinite() || !y.isFinite() || !low.isFinite() || !high.isFinite()) return null
    val index = floor(x.coerceIn(0f,1f) * candles.size).toInt().coerceAtMost(candles.lastIndex)
    return ChartAnchor(candles[index].timeMs, high - y.coerceIn(0f,1f) * (high-low))
}

internal fun chartAnchorPosition(candles: List<Candle>, anchor: ChartAnchor, low: Double, high: Double): Offset? {
    if (candles.isEmpty()) return null
    var index = candles.binarySearchBy(anchor.timeMs) { it.timeMs }.toDouble()
    if (index < 0) {
        val insertion = -index.toInt()-1
        val left = (insertion-1).coerceIn(0,candles.lastIndex)
        val right = (left+1).coerceAtMost(candles.lastIndex)
        val delta = (candles[right].timeMs-candles[left].timeMs).toDouble()
        index = if (delta == 0.0) left.toDouble() else left + (anchor.timeMs-candles[left].timeMs)/delta
        if (insertion == 0 && candles.size > 1) {
            val step = (candles[1].timeMs-candles[0].timeMs).toDouble()
            index = if (step > 0) (anchor.timeMs-candles[0].timeMs)/step else 0.0
        }
        if (insertion == candles.size && candles.size > 1) {
            val step = (candles.last().timeMs-candles[candles.lastIndex-1].timeMs).toDouble()
            index = if (step > 0) candles.lastIndex + (anchor.timeMs-candles.last().timeMs)/step else candles.lastIndex.toDouble()
        }
    }
    return Offset(((index+0.5)/candles.size).toFloat(), if (high <= low) 0.5f else ((high-anchor.price)/(high-low)).toFloat())
}

@Composable
internal fun ChartDrawingOverlay(candles: List<Candle>, low: Double, high: Double, state: ChartDrawingState, modifier: Modifier = Modifier) {
    val p = LB.p
    Canvas(modifier.testTag("event.drawing.plot").pointerInput(candles, low, high, state.tool) {
        detectTapGestures { pos -> if (size.width > 0 && size.height > 0) {
            chartAnchorAt(candles,pos.x/size.width,pos.y/size.height,low,high)?.let(state::tap)
        } }
    }) {
        fun point(a: ChartAnchor): Offset? = chartAnchorPosition(candles,a,low,high)?.let { Offset(it.x*size.width,it.y*size.height) }
        clipRect {
            state.items.forEach { line ->
                val first = point(line.start) ?: return@forEach
                val color = if (line.id == state.selected) p.brand else p.textPrimary
                if (line.tool == DrawingTool.HORIZONTAL) drawLine(color,Offset(0f,first.y),Offset(size.width,first.y),2.dp.toPx())
                else line.end?.let { end -> point(end)?.let { drawLine(color,first,it,2.dp.toPx()); drawCircle(color,3.dp.toPx(),first);drawCircle(color,3.dp.toPx(),it) } }
            }
            state.pending?.let { point(it)?.let { pos -> drawCircle(p.brand,4.dp.toPx(),pos) } }
        }
    }
}

@Composable
internal fun ChartDrawingSheet(state: ChartDrawingState, onClose: () -> Unit) {
    var confirmClear by remember { mutableStateOf(false) }
    LbSheet("画线工具",onClose,showClose=true) {
        Column(Modifier.heightIn(max=520.dp).verticalScroll(rememberScrollState()).padding(horizontal=Spacing.pageH,vertical=8.dp)) {
            Text("仅保存在当前图表会话；按币种与周期分别管理",style=lb(FontSize.small),color=LB.p.textSecondary)
            DrawingTool.entries.forEach { tool ->
                Row(Modifier.fillMaxWidth().height(44.dp).pressable { state.choose(tool);onClose() },horizontalArrangement=Arrangement.SpaceBetween) {
                    Text(tool.title,style=lb(FontSize.body),color=LB.p.textPrimary)
                    if (state.tool==tool) SheetCheck()
                }
            }
            if (state.items.isNotEmpty()) {
                Text("管理画线（${state.items.size}）",style=lb(FontSize.body,FontWeight.SemiBold),color=LB.p.textPrimary)
                state.items.forEach { line ->
                    Row(Modifier.fillMaxWidth().height(40.dp).pressable { state.selected=line.id },horizontalArrangement=Arrangement.SpaceBetween) {
                        Text("${line.tool.title} #${line.id}",style=lb(FontSize.label),color=LB.p.textPrimary)
                        if (state.selected==line.id) SheetCheck()
                    }
                }
                Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.spacedBy(12.dp)) {
                    ChipButton("撤销上一条") { state.undo() }
                    ChipButton("删除选中",enabled=state.selected!=null) { state.deleteSelected() }
                    ChipButton("清空本图") { confirmClear=true }
                }
            }
            Spacer(Modifier.height(20.dp))
        }
    }
    if (confirmClear) LbDialog("清空本图画线",{ confirmClear=false },"清空",{state.clear();confirmClear=false},message="仅清除此币种、此周期的画线，不影响其他图表。",destructive=true)
}
