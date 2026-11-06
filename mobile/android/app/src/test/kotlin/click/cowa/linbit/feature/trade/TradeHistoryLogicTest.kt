package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiFuturesOrder
import click.cowa.linbit.ui.theme.LayoutAdapt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TradeHistoryLogicTest {
    @Test fun positionStartTimeWindows() {
        val now = 1_700_000_000_000L
        assertEquals(now - 7L * 86_400_000, TradeHistoryLogic.positionStartTimeMs(0, now))
        assertEquals(now - 30L * 86_400_000, TradeHistoryLogic.positionStartTimeMs(1, now))
        assertEquals(now - 90L * 86_400_000, TradeHistoryLogic.positionStartTimeMs(2, now))
        assertNull(TradeHistoryLogic.positionStartTimeMs(3, now))
    }

    @Test fun openStatusDetection() {
        assertTrue(TradeHistoryLogic.isOpenStatus("new"))
        assertTrue(TradeHistoryLogic.isOpenStatus("partially_filled"))
        assertFalse(TradeHistoryLogic.isOpenStatus("filled"))
        assertFalse(TradeHistoryLogic.isOpenStatus("canceled"))
    }

    @Test fun orderTypeFilter() {
        val limit = ApiFuturesOrder(type = "limit")
        val market = ApiFuturesOrder(type = "market")
        assertEquals(2, TradeHistoryLogic.filterOrders(listOf(limit, market), 0).size)
        assertEquals(listOf(limit), TradeHistoryLogic.filterOrders(listOf(limit, market), 1))
        assertEquals(listOf(market), TradeHistoryLogic.filterOrders(listOf(limit, market), 2))
    }

    @Test fun algoStatusParam() {
        assertEquals("active", TradeHistoryLogic.algoStatusParam(false))
        assertEquals("history", TradeHistoryLogic.algoStatusParam(true))
    }
}

class LayoutAdaptTest {
    @Test fun widthBucketsMatchCommonPhones() {
        assertEquals(LayoutAdapt.WidthBucket.COMPACT, LayoutAdapt.widthBucket(320))
        assertEquals(LayoutAdapt.WidthBucket.REGULAR, LayoutAdapt.widthBucket(360))
        assertEquals(LayoutAdapt.WidthBucket.EXPANDED, LayoutAdapt.widthBucket(412))
    }

    @Test fun pageHAndTabSpacingTightenOnCompact() {
        assertTrue(LayoutAdapt.pageH(320).value < LayoutAdapt.pageH(412).value)
        assertTrue(LayoutAdapt.tabSpacing(320).value < LayoutAdapt.tabSpacing(412).value)
    }

    @Test fun tabsScrollableOnNarrowOrManyItems() {
        assertTrue(LayoutAdapt.tabsScrollable(320, 2))
        assertTrue(LayoutAdapt.tabsScrollable(412, 4))
        assertFalse(LayoutAdapt.tabsScrollable(412, 2))
    }

    @Test fun largeFontAllowsTwoLineTitle() {
        assertEquals(1, LayoutAdapt.titleMaxLines(1.0f))
        assertEquals(2, LayoutAdapt.titleMaxLines(1.3f))
        assertEquals(2, LayoutAdapt.titleMaxLines(1.5f))
    }
}

class EventContractLogicTest {
    @Test fun stakeValidationRespectsMinMax() {
        assertEquals("请输入投注金额", EventContractLogic.validateStake("", "5", "1000"))
        assertEquals("低于最小投注 5", EventContractLogic.validateStake("1", "5", "1000"))
        assertEquals("超过最大投注 1000", EventContractLogic.validateStake("2000", "5", "1000"))
        assertNull(EventContractLogic.validateStake("10", "5", "1000"))
    }

    @Test fun periodTitles() {
        assertEquals("5 分钟", EventContractLogic.periodTitle("5m"))
        assertEquals("1 小时", EventContractLogic.periodTitle("1h"))
    }
}
