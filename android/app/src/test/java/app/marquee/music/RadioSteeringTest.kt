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

    // A radio never ends (MUSIC-24)

    @Test fun excludeIsOldestFirstWithEachTrackAtItsLatestPlace() {
        val s = RadioSteering()
        assertEquals(listOf(1L, 2L, 3L), s.exclude(listOf(1, 2, 3)))
        // The station came round to 1 again: it's now among the latest.
        assertEquals(listOf(2L, 3L, 1L), s.exclude(listOf(1, 2, 3, 1)))
    }

    @Test fun excludeKeepsTheMostRecent2000() {
        val s = RadioSteering()
        val r = s.exclude((1L..2500L).toList())
        assertEquals(RadioSteering.MAX_EXCLUDE, r.size)
        assertEquals(501L, r.first())
        assertEquals(2500L, r.last())
    }

    @Test fun excludeRemembersWhatLeftTheQueue() {
        val s = RadioSteering(historyLimit = 5)
        s.exclude(listOf(1, 2, 3))
        // The queue was trimmed: 1 and 2 have gone from it but not from what's left out.
        assertEquals(listOf(1L, 2L, 3L, 4L, 5L), s.exclude(listOf(3, 4, 5)))
        assertEquals(listOf(2L, 3L, 4L, 5L, 6L), s.exclude(listOf(5, 6)))
    }

    @Test fun aRepeatIsAddedRatherThanDropped() {
        val played = setOf(1L, 2L, 3L)
        // What comes back has all played before: repeats are fine, they keep the radio going.
        assertEquals(listOf(1L, 2L), RadioSteering.toAdd(listOf(1L, 2L), emptySet()) { it })
        assertEquals(played.toList(), RadioSteering.toAdd(played.toList(), setOf(9L)) { it })
    }

    @Test fun whatIsAlreadyComingUpIsNotQueuedAgain() {
        assertEquals(listOf(3L), RadioSteering.toAdd(listOf(1L, 2L, 3L), setOf(1L, 2L)) { it })
    }

    @Test fun aSmallLibraryStillAddsSomething() {
        // Everything the station has is already coming up: added anyway, or it would stop.
        assertEquals(listOf(1L, 2L), RadioSteering.toAdd(listOf(1L, 2L), setOf(1L, 2L)) { it })
        assertEquals(emptyList<Long>(), RadioSteering.toAdd(emptyList<Long>(), setOf(1L)) { it })
    }

    @Test fun onlyWhatPlayedLongAgoIsTrimmed() {
        assertEquals(0, RadioSteering.toTrim(0))
        assertEquals(0, RadioSteering.toTrim(199))
        assertEquals(100, RadioSteering.toTrim(200))
        assertEquals(150, RadioSteering.toTrim(250))
    }

    @Test fun aRepeatedTrackGetsItsOwnMediaId() {
        assertEquals(listOf("1", "2", "1~2", "1~3"), QueueIds.assign(listOf(1, 2, 1, 1)))
        // Against what's queued already, including earlier repeats.
        assertEquals(listOf("1~3", "2", "1~4"), QueueIds.assign(listOf(1, 2, 1), listOf("1", "1~2", "3")))
    }

    @Test fun aMediaIdGivesItsTrack() {
        assertEquals(12L, QueueIds.trackId("12"))
        assertEquals(12L, QueueIds.trackId("12~3"))
        assertNull(QueueIds.trackId("album:12"))
        assertNull(QueueIds.trackId("search"))
        assertNull(QueueIds.trackId(null))
    }
}
