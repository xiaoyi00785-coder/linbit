package click.cowa.linbit.feature.trade

import click.cowa.linbit.core.generated.ApiEarnProduct
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EarnProductAvailabilityTest {
    private fun prod(
        status: String? = "online",
        remaining: String? = null,
        sold: String? = "0",
        totalQuota: String? = null,
        title: String = "ETH-抢购",
    ) = ApiEarnProduct(
        id = "fixed-ETH-14",
        kind = ApiEarnProduct.Kind.FIXED,
        asset = "ETH",
        title = title,
        apr = "0.083",
        termDays = 14,
        minAmount = "0.1",
        status = status,
        sold = sold,
        remaining = remaining,
        totalQuota = totalQuota,
    )

    private val now = 1_762_646_400_000L // 2025-11-09 approx

    @Test fun endedStatusNeverShowsStartTimeToast() {
        val start = now - 10 * 24 * 3600_000L // past start
        val p = prod(status = "ended")
        assertEquals(EarnSubscribePhase.ENDED, EarnProductAvailability.phase(p, now, startAtMs = start))
        assertEquals("申购已结束", EarnProductAvailability.blockedToast(p, now, startAtMs = start))
        assertEquals("已结束", EarnProductAvailability.ctaLabel(p, now, startAtMs = start))
        assertEquals("已结束", EarnProductAvailability.timelineLabel(p, now, startAtMs = start))
    }

    @Test fun offlineIsEnded() {
        assertEquals(EarnSubscribePhase.ENDED, EarnProductAvailability.phase(prod(status = "offline"), now))
        assertEquals("申购已结束", EarnProductAvailability.blockedToast(prod(status = "offline"), now))
    }

    @Test fun notStartedShowsStartTimeWhenKnown() {
        val start = now + 3600_000L
        val p = prod(status = "upcoming")
        assertEquals(EarnSubscribePhase.NOT_STARTED, EarnProductAvailability.phase(p, now, startAtMs = start))
        val toast = EarnProductAvailability.blockedToast(p, now, startAtMs = start)!!
        assertTrue(toast.startsWith("当前产品在"))
        assertTrue(toast.contains("开始申购"))
    }

    @Test fun notStartedWithoutTime() {
        assertEquals("申购尚未开始", EarnProductAvailability.blockedToast(prod(status = "upcoming"), now))
    }

    @Test fun soldOutByRemaining() {
        val p = prod(remaining = "0")
        assertEquals(EarnSubscribePhase.SOLD_OUT, EarnProductAvailability.phase(p, now))
        assertEquals("已售罄", EarnProductAvailability.blockedToast(p, now))
    }

    @Test fun soldOutByQuota() {
        val p = prod(sold = "100", totalQuota = "100")
        assertEquals(EarnSubscribePhase.SOLD_OUT, EarnProductAvailability.phase(p, now))
    }

    @Test fun openOnlineHasNoBlock() {
        val p = prod(status = "online", remaining = "10")
        assertEquals(EarnSubscribePhase.OPEN, EarnProductAvailability.phase(p, now))
        assertNull(EarnProductAvailability.blockedToast(p, now))
        assertEquals("立即申购", EarnProductAvailability.ctaLabel(p, now))
    }

    @Test fun endAtInPastIsEndedEvenIfOnline() {
        val p = prod(status = "online")
        val end = now - 1000L
        assertEquals(EarnSubscribePhase.ENDED, EarnProductAvailability.phase(p, now, endAtMs = end))
        assertEquals("申购已结束", EarnProductAvailability.blockedToast(p, now, endAtMs = end))
    }

    @Test fun startAtInFutureBeatsOnlineStatus() {
        val p = prod(status = "online")
        val start = now + 86_400_000L
        assertEquals(EarnSubscribePhase.NOT_STARTED, EarnProductAvailability.phase(p, now, startAtMs = start))
    }
}
