package app.marquee.core

import app.marquee.core.SmartSelection.Candidate
import app.marquee.core.SmartSelection.ShowQueue
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Smart downloads: which episodes and tracks are kept within a budget, and what changes on the device. */
class SmartSelectionTest {
    private fun eps(vararg ids: Long, bytes: Long = 1) = ids.map { Candidate(it, bytes) }

    @Test fun nextEpisodesPerShow() {
        val shows = listOf(ShowQueue(1, 100, eps(11, 12, 13, 14)), ShowQueue(2, 200, eps(21, 22)))
        // The most recently watched show first; each keeps at most its next N.
        assertEquals(listOf(21L, 22L, 11L, 12L, 13L), SmartSelection.episodes(shows, 3, 100))
        assertEquals(listOf(21L, 11L), SmartSelection.episodes(shows, 1, 100))
    }

    @Test fun budgetLeavesOutTheLeastRecentlyWatchedShows() {
        val shows = listOf(ShowQueue(1, 100, eps(11, 12, bytes = 4)), ShowQueue(2, 300, eps(21, 22, bytes = 4)), ShowQueue(3, 200, eps(31, 32, bytes = 4)))
        assertEquals(listOf(21L, 22L, 31L), SmartSelection.episodes(shows, 2, 12))
    }

    @Test fun anEpisodeIsNotKeptWithoutTheOnesBeforeIt() {
        val shows = listOf(ShowQueue(1, 300, listOf(Candidate(11, 2), Candidate(12, 10), Candidate(13, 1))), ShowQueue(2, 100, eps(21, bytes = 3)))
        // 12 doesn't fit, so 13 isn't taken; the next show still can be.
        assertEquals(listOf(11L, 21L), SmartSelection.episodes(shows, 3, 6))
    }

    @Test fun nothingFitsAZeroBudget() {
        assertEquals(emptyList<Long>(), SmartSelection.episodes(listOf(ShowQueue(1, 1, eps(11))), 3, 0))
        // Already downloaded by hand: costs nothing.
        assertEquals(listOf(11L), SmartSelection.episodes(listOf(ShowQueue(1, 1, eps(11, bytes = 0))), 3, 0))
    }

    @Test fun tracksFillTheBudgetInOrder() {
        val c = listOf(Candidate(1, 5), Candidate(2, 5), Candidate(1, 5), Candidate(3, 8), Candidate(4, 2), Candidate(5, 1))
        // Repeats count once; 3 is too big for what's left, smaller ones after it still go in.
        assertEquals(listOf(1L, 2L, 4L), SmartSelection.tracks(c, 12))
        assertEquals(listOf(1L, 2L, 4L, 5L), SmartSelection.tracks(c, 13))
    }

    @Test fun planDownloadsWhatsMissingAndRemovesWhatsNotWanted() {
        val p = SmartSelection.plan(wanted = listOf(1, 2, 3, 4), smartNow = setOf(2, 5, 6), manual = setOf(3, 7))
        assertEquals(listOf(1L, 4L), p.download)
        assertEquals(setOf(5L, 6L), p.remove.toSet())
    }

    @Test fun planNeverRemovesManualDownloads() {
        val p = SmartSelection.plan(wanted = emptyList(), smartNow = setOf(1), manual = setOf(2, 3))
        assertEquals(listOf(1L), p.remove)
        assertTrue(p.download.isEmpty())
    }

    @Test fun failedSmartDownloadsAreRetried() {
        val p = SmartSelection.plan(wanted = listOf(1, 2), smartNow = setOf(1, 2, 3), manual = emptySet(), failed = setOf(2, 3))
        assertEquals(listOf(2L), p.download)
        assertEquals(setOf(2L, 3L), p.remove.toSet())
    }

    @Test fun estimates() {
        // An hour at Medium (3000 kbps) is 1.35 GB.
        assertEquals(1_350_000_000L, SmartSelection.estimateVideoBytes(3_600_000, Downloads.Quality.Medium))
        // Four minutes at 320 kbps is 9.6 MB; lossless without a bit rate counts 1000 kbps.
        assertEquals(9_600_000L, SmartSelection.estimateTrackBytes(240_000, 320, false))
        assertEquals(30_000_000L, SmartSelection.estimateTrackBytes(240_000, null, true))
    }
}
