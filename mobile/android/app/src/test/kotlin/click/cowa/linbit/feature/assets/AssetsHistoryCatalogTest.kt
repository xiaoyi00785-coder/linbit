package click.cowa.linbit.feature.assets

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AssetsHistoryCatalogTest {
    @Test fun earnTabOffersEarnOrdersAndRecordsNotOnlySpot() {
        val opts = AssetsHistoryCatalog.optionsForTab(4)
        assertEquals(listOf("理财订单", "理财流水"), opts.map { it.title })
        assertEquals(
            listOf(AssetsHistoryOption.Action.EARN_ORDERS, AssetsHistoryOption.Action.EARN_RECORDS),
            opts.map { it.action },
        )
        assertFalse(opts.any { it.title.contains("现货") })
    }

    @Test fun spotTabKeepsSpotTradeHistory() {
        val titles = AssetsHistoryCatalog.optionsForTab(3).map { it.title }
        assertTrue(titles.contains("现货交易历史"))
        assertFalse(titles.contains("理财订单"))
    }

    @Test fun futuresTabKeepsFuturesEntries() {
        val titles = AssetsHistoryCatalog.optionsForTab(2).map { it.title }
        assertTrue(titles.any { it.contains("合约") })
    }

    @Test fun overviewHasDepositWithdrawSpotFutures() {
        val titles = AssetsHistoryCatalog.optionsForTab(0).map { it.title }
        assertEquals(listOf("充值记录", "提现记录", "现货交易历史", "合约交易历史"), titles)
    }
}
