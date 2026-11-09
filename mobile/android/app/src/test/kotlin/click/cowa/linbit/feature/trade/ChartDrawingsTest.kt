package click.cowa.linbit.feature.trade

import org.junit.Assert.*
import org.junit.Test
import java.math.BigDecimal

class ChartDrawingsTest {
    private val candles = (0..99).map { i -> Candle(BigDecimal.TEN,BigDecimal(20),BigDecimal.ZERO,BigDecimal.TEN,timeMs=1000L+i*60000) }
    @Test fun horizontalNeedsOnePointTrendNeedsTwo() {
        val s=ChartDrawingState();val a=ChartAnchor(1,10.0);val b=ChartAnchor(2,12.0)
        s.choose(DrawingTool.HORIZONTAL);s.tap(a)
        assertEquals(1,s.items.size);assertEquals(DrawingTool.NONE,s.tool)
        s.choose(DrawingTool.TREND);s.tap(a);assertEquals(1,s.items.size);assertEquals(a,s.pending)
        s.tap(a);assertEquals(1,s.items.size)
        s.tap(b);assertEquals(2,s.items.size);assertEquals(b,s.items.last().end);assertNull(s.pending)
    }
    @Test fun selectionDeletionUndoAndClearAreLocal() {
        val s=ChartDrawingState()
        repeat(3) { s.choose(DrawingTool.HORIZONTAL);s.tap(ChartAnchor(it.toLong(),10.0)) }
        s.selected=s.items[1].id;s.deleteSelected();assertEquals(listOf(1L,3L),s.items.map { it.id })
        s.undo();assertEquals(1,s.items.size)
        s.choose(DrawingTool.TREND);s.tap(ChartAnchor(9,1.0));s.clear()
        assertTrue(s.items.isEmpty());assertNull(s.pending);assertEquals(DrawingTool.NONE,s.tool)
    }
    @Test fun contextsDoNotLeakAndReturnRetainsLinesButCancelsDraft() {
        val book=ChartDrawingBook();val btc=book.state("BTCUSDT","15m")
        btc.choose(DrawingTool.HORIZONTAL);btc.tap(ChartAnchor(1,20.0))
        assertTrue(book.state("ETHUSDT","15m").items.isEmpty());assertTrue(book.state("BTCUSDT","1h").items.isEmpty())
        btc.choose(DrawingTool.TREND);btc.tap(ChartAnchor(2,21.0));btc.cancelDraft()
        assertSame(btc,book.state("BTCUSDT","15m"));assertEquals(1,btc.items.size);assertNull(btc.pending)
        book.state("ETHUSDT","15m").clear();assertEquals(1,btc.items.size)
    }
    @Test fun zoomAndChangingPriceRangeReprojectSavedTimePrice() {
        val a=chartAnchorAt(candles,0.805f,0.25f,0.0,20.0)!!
        assertEquals(candles[80].timeMs,a.timeMs);assertEquals(15.0,a.price,0.0)
        val zoom=chartAnchorPosition(candles.takeLast(40),a,10.0,30.0)!!
        assertEquals(20.5f/40,zoom.x,1e-6f);assertEquals(0.75f,zoom.y,1e-6f)
        assertEquals(a,chartAnchorAt(candles.takeLast(40),zoom.x,zoom.y,10.0,30.0))
    }
    @Test fun offscreenAndGapsKeepCorrectCoordinates() {
        val visible=candles.takeLast(20)
        val p=chartAnchorPosition(visible,ChartAnchor(candles[0].timeMs,10.0),0.0,20.0)!!
        assertTrue(p.x<0)
        val gaps=listOf(candles[0],candles[4],candles[9])
        val a=chartAnchorAt(gaps,0.5f,0.5f,0.0,20.0)!!
        assertEquals(candles[4].timeMs,a.timeMs)
        assertEquals(0.5f,chartAnchorPosition(gaps,a,0.0,20.0)!!.x,0.0f)
    }
    @Test fun emptyFlatAndInvalidInputAreSafe() {
        assertNull(chartAnchorAt(emptyList(),0f,0f,0.0,1.0));assertNull(chartAnchorAt(candles,Float.NaN,0f,0.0,1.0))
        assertNull(chartAnchorPosition(emptyList(),ChartAnchor(1,1.0),0.0,1.0))
        assertEquals(0.5f,chartAnchorPosition(candles,ChartAnchor(1,1.0),1.0,1.0)!!.y,0.0f)
        val s=ChartDrawingState();s.choose(DrawingTool.HORIZONTAL);s.tap(ChartAnchor(1,Double.NaN));assertTrue(s.items.isEmpty())
    }
}
