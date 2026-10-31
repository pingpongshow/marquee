package app.marquee.music

import app.marquee.api.models.RadioRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Steering a radio while it plays (MUSIC-17): quick skips, played-through tracks and early top-ups. */
class RadioSteeringTest {
    @Test fun quickSkipIsBefore30sAndHalfWay() {
        assertTrue(RadioSteering.quickSkip(10_000, 200_000))
        assertFalse(RadioSteering.quickSkip(30_000, 200_000))
        // A short track: half way comes first.
        assertFalse(RadioSteering.quickSkip(25_000, 40_000))
        // Length not known yet: the 30 s alone.
        assertTrue(RadioSteering.quickSkip(5_000, 0))
    }

    @Test fun playedThroughIsFourFifths() {
        assertTrue(RadioSteering.playedThrough(160_000, 200_000))
        assertFalse(RadioSteering.playedThrough(159_999, 200_000))
        assertFalse(RadioSteering.playedThrough(10_000, 0))
    }

    @Test fun twoQuickSkipsInARowAskForATopUp() {
        val s = RadioSteering()
        assertFalse(s.left(1, 5_000, 200_000, natural = false))
        assertTrue(s.left(2, 5_000, 200_000, natural = false))
        assertEquals(listOf(1L, 2L), s.skipped)
        s.toppedUp()
        assertFalse(s.left(3, 5_000, 200_000, natural = false))
    }

    @Test fun playingThroughEndsARunOfSkips() {
        val s = RadioSteering()
        s.left(1, 5_000, 200_000, natural = false)
        // Ended by itself, however little was heard (a seek near the end).
        assertFalse(s.left(2, 1_000, 200_000, natural = true))
        assertFalse(s.left(3, 5_000, 200_000, natural = false))
        assertEquals(listOf(2L), s.finished)
        assertEquals(listOf(1L, 3L), s.skipped)
    }

    @Test fun neitherSkippedNorFinishedCountsForNothing() {
        val s = RadioSteering()
        s.left(1, 5_000, 200_000, natural = false)
        assertFalse(s.left(2, 60_000, 200_000, natural = false))
        assertEquals(1, s.inARow)
        assertEquals(listOf(1L), s.skipped)
        assertEquals(emptyList<Long>(), s.finished)
    }

    @Test fun keepsTheLatestAndOneListPerTrack() {
        val s = RadioSteering(keep = 3)
        (1L..5L).forEach { s.finished(it) }
        assertEquals(listOf(3L, 4L, 5L), s.finished)
        s.skipped(4)
        assertEquals(listOf(3L, 5L), s.finished)
        assertEquals(listOf(4L), s.skipped)
        s.finished(3)
        assertEquals(listOf(5L, 3L), s.finished)
    }

    @Test fun requestCarriesTheSteering() {
        val base = RadioRequest(RadioRequest.Seed.MOOD, value = "calm", libraryId = 7, limit = 50)
        val empty = RadioSteering().request(base, listOf(1, 2))
        assertEquals(listOf(1L, 2L), empty.exclude)
        assertEquals(25, empty.limit)
        assertNull(empty.skipped)
        assertNull(empty.finished)
        val s = RadioSteering()
        s.skipped(9)
        s.finished(8)
        val r = s.request(base, listOf(1))
        assertEquals(listOf(9L), r.skipped)
        assertEquals(listOf(8L), r.finished)
        assertEquals("calm", r.value)
        assertEquals(7L, r.libraryId)
    }
}
