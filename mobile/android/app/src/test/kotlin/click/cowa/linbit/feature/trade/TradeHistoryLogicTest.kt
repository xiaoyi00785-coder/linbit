package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiEventOrder
import click.cowa.linbit.core.generated.ApiEventProducts
import click.cowa.linbit.core.generated.ApiFuturesAlgoAmendRequest
import click.cowa.linbit.core.generated.ApiFuturesOrder
import java.math.BigDecimal
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


class AmendAlgoLogicTest {
    private fun fmt(v: BigDecimal) = v.stripTrailingZeros().toPlainString()

    @Test fun requiresAtLeastOneChange() {
        val o = AmendAlgoLogic.prepare("", "", "", false, "100", "99", "1", false, ::fmt, ::fmt)
        assertEquals("请至少修改一项", o.error)
    }

    @Test fun rejectsInvalidTrigger() {
        val o = AmendAlgoLogic.prepare("0", "", "", false, "100", "99", "1", false, ::fmt, ::fmt)
        assertEquals("请输入有效触发价", o.error)
    }

    @Test fun emitsTriggerAndMarketSwitch() {
        val o = AmendAlgoLogic.prepare("110", "", "", true, "100", "99", "1", false, ::fmt, ::fmt)
        assertNull(o.error)
        assertEquals("110", o.triggerPrice)
        assertEquals(ApiFuturesAlgoAmendRequest.OrderType.MARKET, o.orderType)
    }

    @Test fun emitsLimitPriceChange() {
        val o = AmendAlgoLogic.prepare("", "105", "2", false, "100", "99", "1", false, ::fmt, ::fmt)
        assertNull(o.error)
        assertEquals("105", o.price)
        assertEquals("2", o.qty)
        assertEquals(ApiFuturesAlgoAmendRequest.OrderType.LIMIT, o.orderType)
    }
}

class EventOrderMergeTest {
    private fun order(
        id: String,
        status: ApiEventOrder.Status,
        result: ApiEventOrder.Result? = null,
        payout: String? = null,
    ) = ApiEventOrder(id = id, symbol = "BTCUSDT", status = status, result = result, payout = payout)

    @Test fun activeUpsertAndRemoveFromHistory() {
        val open = listOf(order("1", ApiEventOrder.Status.OPEN))
        val hist = listOf(order("1", ApiEventOrder.Status.SETTLED, ApiEventOrder.Result.LOST))
        val (nOpen, nHist, tip) = EventContractLogic.applyOrder(
            order("1", ApiEventOrder.Status.OPEN), open, hist, "USDT",
        )
        assertEquals(1, nOpen.size)
        assertTrue(nHist.none { it.id == "1" })
        assertNull(tip)
    }

    @Test fun settledMovesToHistoryWithToast() {
        val open = listOf(order("2", ApiEventOrder.Status.OPEN))
        val (nOpen, nHist, tip) = EventContractLogic.applyOrder(
            order("2", ApiEventOrder.Status.SETTLED, ApiEventOrder.Result.WON, "18"),
            open, emptyList(), "USDT",
        )
        assertTrue(nOpen.isEmpty())
        assertEquals(1, nHist.size)
        assertTrue(tip!!.contains("猜中"))
    }

    @Test fun rulesIncludeLimits() {
        val cfg = ApiEventProducts(minStake = "5", maxStake = "1000", dailyLimit = "5000", payoutRatio = "0.8")
        val rules = EventContractLogic.rules(cfg, "USDT")
        assertEquals(5, rules.size)
        assertTrue(rules[1].contains("80"))
        assertTrue(rules[3].contains("5"))
        assertTrue(rules[3].contains("1000"))
    }
}
