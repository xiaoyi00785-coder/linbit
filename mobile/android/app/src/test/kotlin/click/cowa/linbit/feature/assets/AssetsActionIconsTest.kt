package click.cowa.linbit.feature.assets

import click.cowa.linbit.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class AssetsActionIconsTest {
    @Test fun spotTabIconsAreDistinctVectorsNotLetterPlaceholders() {
        val titles = listOf("去交易", "充值", "快捷买币", "划转")
        val ids = titles.map { AssetsActionIcons.drawable(it) }
        assertEquals(R.drawable.ic_asset_trade, ids[0])
        assertEquals(R.drawable.ic_asset_deposit, ids[1])
        assertEquals(R.drawable.ic_asset_card, ids[2])
        assertEquals(R.drawable.ic_asset_transfer, ids[3])
        // 互不相同，排除「全用同一占位」
        assertEquals(4, ids.toSet().size)
        titles.forEach { t ->
            assertNotEquals("must not map to empty/0", 0, AssetsActionIcons.drawable(t))
        }
    }

    @Test fun overviewAndFuturesIconsResolved() {
        assertEquals(R.drawable.ic_asset_deposit, AssetsActionIcons.drawable("充值"))
        assertEquals(R.drawable.ic_asset_withdraw, AssetsActionIcons.drawable("提现"))
        assertEquals(R.drawable.ic_candles, AssetsActionIcons.drawable("盈亏"))
    }
}
