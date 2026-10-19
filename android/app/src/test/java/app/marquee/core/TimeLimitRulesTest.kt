package app.marquee.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalTime

/** Kids' time limits: reading the server's refusal, and the offline check from the last status. */
class TimeLimitRulesTest {
    private fun cached(allowed: Boolean = true, resetsAt: Long? = null, from: String? = null, until: String? = null, reason: String? = "Time's up for today") =
        TimeLimitRules.Cached(limited = true, allowedNow = allowed, reason = reason, resetsAtMs = resetsAt, allowedFrom = from, allowedUntil = until, checkedAtMs = 1)

    @Test fun readsTheReasonOfATimeLimitRefusal() {
        assertEquals("Time's up for today", TimeLimitRules.reasonFromBody("""{"code":"time_limit","message":"Time's up for today"}"""))
        assertEquals(TimeLimitRules.DEFAULT_REASON, TimeLimitRules.reasonFromBody("""{"code":"time_limit","message":""}"""))
        assertNull(TimeLimitRules.reasonFromBody("""{"code":"forbidden","message":"No"}"""))
        assertNull(TimeLimitRules.reasonFromBody("not json"))
        assertNull(TimeLimitRules.reason(RuntimeException("x")))
    }

    @Test fun offlineRefusedUntilItResets() {
        val noon = LocalTime.NOON
        assertEquals("Time's up for today", TimeLimitRules.refusal(cached(allowed = false, resetsAt = 1000), 500, noon))
        assertNull(TimeLimitRules.refusal(cached(allowed = false, resetsAt = 1000), 1000, noon))
        assertEquals("Time's up for today", TimeLimitRules.refusal(cached(allowed = false), 5000, noon))
        assertNull(TimeLimitRules.refusal(cached(), 5000, noon))
        assertNull(TimeLimitRules.refusal(cached(allowed = false).copy(limited = false), 5000, noon))
    }

    @Test fun offlineViewingHours() {
        val c = cached(from = "07:00", until = "20:00")
        assertNull(TimeLimitRules.refusal(c, 0, LocalTime.of(7, 0)))
        assertEquals("Viewing hours are 07:00–20:00.", TimeLimitRules.refusal(c, 0, LocalTime.of(20, 0)))
        assertEquals("Viewing hours are 07:00–20:00.", TimeLimitRules.refusal(c, 0, LocalTime.of(6, 59)))
    }

    @Test fun hoursPastMidnight() {
        val from = LocalTime.of(22, 0)
        val until = LocalTime.of(2, 0)
        assertTrue(TimeLimitRules.withinHours(LocalTime.of(23, 0), from, until))
        assertTrue(TimeLimitRules.withinHours(LocalTime.of(1, 0), from, until))
        assertFalse(TimeLimitRules.withinHours(LocalTime.of(12, 0), from, until))
    }

    @Test fun cachedSurvivesARoundTrip() {
        val c = cached(allowed = false, resetsAt = 42, from = "07:00", until = "20:00")
        assertEquals(c, TimeLimitRules.Cached.decode(c.encode()))
        val bare = cached(reason = null)
        assertEquals(bare, TimeLimitRules.Cached.decode(bare.encode()))
    }

    @Test fun minutesLeft() {
        assertEquals("45 min", TimeLimitRules.formatMinutes(45))
        assertEquals("1 hr", TimeLimitRules.formatMinutes(60))
        assertEquals("1 hr 5 min", TimeLimitRules.formatMinutes(65))
    }
}
