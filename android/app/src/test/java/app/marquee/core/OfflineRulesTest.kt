package app.marquee.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** When a failed request leads to asking whether the server has gone, and when that means offline. */
class OfflineRulesTest {
    @Test fun aSingleFailureOnlyStartsACheck() {
        val r = OfflineRules()
        assertTrue(r.requestFailed(1_000, offline = false))
        // The server answers: nothing happened.
        assertEquals(OfflineRules.Next.Reachable, r.checked(true, 2_000))
    }

    @Test fun failuresDuringACheckJoinIt() {
        val r = OfflineRules()
        assertTrue(r.requestFailed(1_000, offline = false))
        assertFalse(r.requestFailed(1_100, offline = false))
        assertFalse(r.requestFailed(1_200, offline = false))
    }

    @Test fun oneFailedCheckIsNotEnough() {
        val r = OfflineRules()
        r.requestFailed(1_000, offline = false)
        assertEquals(OfflineRules.Next.CheckAgain, r.checked(false, 2_000))
        assertEquals(OfflineRules.Next.Lost, r.checked(false, 3_000))
    }

    @Test fun aFailedCheckThenAnAnswerIsFine() {
        val r = OfflineRules()
        r.requestFailed(1_000, offline = false)
        assertEquals(OfflineRules.Next.CheckAgain, r.checked(false, 2_000))
        assertEquals(OfflineRules.Next.Reachable, r.checked(true, 3_000))
        // Counted afresh next time.
        assertTrue(r.requestFailed(100_000, offline = false))
        assertEquals(OfflineRules.Next.CheckAgain, r.checked(false, 101_000))
    }

    @Test fun afterTheServerAnsweredItWaitsBeforeCheckingAgain() {
        val r = OfflineRules(quietMs = 15_000)
        r.requestFailed(1_000, offline = false)
        r.checked(true, 2_000)
        assertFalse(r.requestFailed(16_999, offline = false))
        assertTrue(r.requestFailed(17_000, offline = false))
    }

    @Test fun offlineAlreadyStartsNoCheck() {
        assertFalse(OfflineRules().requestFailed(1_000, offline = true))
    }

    @Test fun afterLosingItACheckCanStartAgain() {
        val r = OfflineRules()
        r.requestFailed(1_000, offline = false)
        r.checked(false, 2_000)
        r.checked(false, 3_000)
        assertTrue(r.requestFailed(4_000, offline = false))
    }

    @Test fun aDroppedCheckDoesNotBlockTheNext() {
        val r = OfflineRules()
        r.requestFailed(1_000, offline = false)
        r.checkDropped()
        assertTrue(r.requestFailed(2_000, offline = false))
    }
}
