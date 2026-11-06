package click.cowa.linbit.feature.home

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class CountdownLogicTest {
    @Test fun missingDeadlineYieldsNull() {
        assertNull(CountdownLogic.newcomerText(null))
        assertNull(CountdownLogic.newcomerText(""))
        assertNull(CountdownLogic.newcomerText("not-a-date"))
    }

    @Test fun isoDeadlineFormats() {
        val now = Instant.parse("2026-11-06T00:00:00Z")
        val text = CountdownLogic.newcomerText("2026-11-12T12:34:56Z", now)!!
        assertTrue(text.startsWith("6D:"))
        assertEquals("6D:12:34:56", text)
    }

    @Test fun epochDeadlineAccepted() {
        val now = Instant.ofEpochMilli(1_700_000_000_000L)
        val deadlineMs = 1_700_000_000_000L + 3661_000L
        assertEquals("0D:01:01:01", CountdownLogic.newcomerText(deadlineMs.toString(), now))
        assertEquals("0D:01:01:01", CountdownLogic.newcomerText((deadlineMs / 1000).toString(), now))
    }

    @Test fun fundingMissingIsNull() {
        assertNull(CountdownLogic.fundingText(null))
        assertNull(CountdownLogic.fundingText(0L))
    }

    @Test fun fundingNormalizesSecondsAndMillis() {
        val now = 1_700_000_000_000L
        assertEquals("01:00:00", CountdownLogic.fundingText(now / 1000 + 3600, now))
        assertEquals("00:30:00", CountdownLogic.fundingText(now + 1_800_000L, now))
    }
}
