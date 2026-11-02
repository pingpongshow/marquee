package app.marquee.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** A thumbs down and ratings clearing each other in every view (MUSIC-25). */
class DislikeStateTest {
    @Test fun aThumbsDownClearsHighStarsOnly() {
        val s = DislikeState()
        s.thumbsDown(1, true)
        val m = s.marks.value
        assertTrue(m.disliked(1, null))
        assertNull(m.rating(1, 7.0))
        assertNull(m.rating(1, 10.0))
        assertEquals(6.0, m.rating(1, 6.0))
        // Other songs aren't touched.
        assertEquals(8.0, m.rating(2, 8.0))
    }

    @Test fun takingItBackLeavesTheRatingCleared() {
        val s = DislikeState()
        s.thumbsDown(1, true)
        s.thumbsDown(1, false)
        assertFalse(s.marks.value.disliked(1, true))
        assertNull(s.marks.value.rating(1, 8.0))
    }

    @Test fun ratingHighClearsTheThumbsDownAndLowDoesNot() {
        val s = DislikeState()
        s.thumbsDown(1, true)
        s.rated(1, 6.5)
        assertTrue(s.marks.value.disliked(1, null))
        assertEquals(6.5, s.marks.value.rating(1, 9.0))
        s.rated(1, 7.0)
        assertFalse(s.marks.value.disliked(1, true))
        assertEquals(7.0, s.marks.value.rating(1, null))
        // Cleared by hand: the list's older value doesn't come back.
        s.rated(1, null)
        assertNull(s.marks.value.rating(1, 9.0))
    }

    @Test fun favouritingClearsTheThumbsDown() {
        val s = DislikeState()
        s.favourited(1)
        assertFalse(s.marks.value.disliked(1, true))
    }

    @Test fun theServersValueShowsWithoutAChangeHere() {
        val m = DislikeState().marks.value
        assertTrue(m.disliked(1, true))
        assertFalse(m.disliked(1, null))
        assertEquals(8.0, m.rating(1, 8.0))
    }

    @Test fun aRefusalPutsTheSongBackAndLeavesOthers() {
        val s = DislikeState()
        s.rated(1, 8.0)
        val undo = s.thumbsDown(1, true)
        s.thumbsDown(2, true)
        undo()
        assertFalse(s.marks.value.disliked(1, null))
        assertEquals(8.0, s.marks.value.rating(1, null))
        assertTrue(s.marks.value.disliked(2, null))
    }

    @Test fun anotherProfileStartsClean() {
        val s = DislikeState()
        s.thumbsDown(1, true)
        s.clear()
        assertFalse(s.marks.value.disliked(1, null))
        assertEquals(8.0, s.marks.value.rating(1, 8.0))
    }

    private fun change(item: Long, kind: PendingChange.Kind, seq: Long, on: Boolean = true, rating: Double? = null) =
        PendingChange("s", 1, item, kind, seq, rating = rating, on = on, seq = seq)

    @Test fun theLatestOfWhatsWaitingDecides() {
        val d = PendingChange.Kind.Dislike
        val f = PendingChange.Kind.Favorite
        val r = PendingChange.Kind.Rating
        // Only a thumbs down waiting says anything; a heart alone leaves what the server has.
        assertEquals(emptyMap<Long, Boolean>(), DislikeState.waitingDislikes(listOf(change(1, f, 1))))
        assertEquals(mapOf(1L to true), DislikeState.waitingDislikes(listOf(change(1, d, 1))))
        // A later heart or high rating takes it back; a low one or a heart removed doesn't.
        assertEquals(mapOf(1L to false), DislikeState.waitingDislikes(listOf(change(1, d, 1), change(1, f, 2))))
        assertEquals(mapOf(1L to false), DislikeState.waitingDislikes(listOf(change(1, d, 1), change(1, r, 2, rating = 8.0))))
        assertEquals(mapOf(1L to true), DislikeState.waitingDislikes(listOf(change(1, d, 1), change(1, r, 2, rating = 2.0), change(1, f, 3, on = false))))
        // An earlier heart doesn't, whatever order the list is in; a taken-back one is off.
        assertEquals(mapOf(1L to true), DislikeState.waitingDislikes(listOf(change(1, d, 2), change(1, f, 1))))
        assertEquals(mapOf(1L to false), DislikeState.waitingDislikes(listOf(change(1, d, 1, on = false))))
    }

    @Test fun whatsWaitingDoesntOverrideThisRun() {
        val s = DislikeState()
        s.thumbsDown(1, false)
        s.showWaiting(listOf(change(1, PendingChange.Kind.Dislike, 1), change(2, PendingChange.Kind.Dislike, 2)))
        assertEquals(false, s.marks.value.disliked(1, true))
        assertEquals(true, s.marks.value.disliked(2, false))
    }
}
