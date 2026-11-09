package click.cowa.linbit.feature.profile

import click.cowa.linbit.core.generated.ApiPartnerSummary
import click.cowa.linbit.feature.assets.ApiPnlPoint
import click.cowa.linbit.feature.assets.PnlStats
import click.cowa.linbit.feature.more.ReferralLogic
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class Batch3LogicTest {
    @Test fun referralRateUsesServerMaxAndNeverInvents() {
        assertEquals("30", ReferralLogic.maxRatePercent(ApiPartnerSummary(futuresRate = "0.3", spotRate = "0.2")))
        assertNull(ReferralLogic.maxRatePercent(ApiPartnerSummary()))
    }
    @Test fun pnlStatsCumulativeAndDays() {
        val pts = listOf("1", "-2", "0", "3").mapIndexed { i, v -> ApiPnlPoint("d$i", pnl = v, tradeCount = i) }
        assertEquals(listOf("1", "-1", "-1", "2"), PnlStats.cumulative(pts).map { it.stripTrailingZeros().toPlainString() })
        assertEquals(2, PnlStats.winDays(pts)); assertEquals(1, PnlStats.lossDays(pts)); assertEquals(6, PnlStats.trades(pts))
    }
}
