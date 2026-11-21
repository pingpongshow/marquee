package app.marquee.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** A thumbs up and a thumbs down take each other back, here and in what's waiting to sync (USER-21). */
class ThumbsTest {
    private fun change(item: Long, kind: PendingChange.Kind, seq: Long, on: Boolean = true) = PendingChange("s", 1, item, kind, seq, on = on, seq = seq)

    @Test fun upAndDownTakeEachOtherBack() {
        val s = DislikeState()
        s.thumbsUp(1, true)
        assertTrue(s.marks.value.liked(1, null))
        assertFalse(s.marks.value.disliked(1, true))
        s.thumbsDown(1, true)
        assertFalse(s.marks.value.liked(1, true))
        assertTrue(s.marks.value.disliked(1, null))
        // Taking back a thumbs up leaves a thumbs down alone.
        s.thumbsUp(2, true)
        val undo = s.thumbsUp(2, false)
        assertFalse(s.marks.value.liked(2, true))
        undo()
        assertTrue(s.marks.value.liked(2, null))
    }

    @Test fun whatsWaitingShowsAfterARestart() {
        val l = PendingChange.Kind.Like
        val d = PendingChange.Kind.Dislike
        assertEquals(mapOf(1L to true), DislikeState.waitingLikes(listOf(change(1, l, 1))))
        assertEquals(mapOf(1L to false), DislikeState.waitingLikes(listOf(change(1, l, 1), change(1, d, 2))))
        assertEquals(mapOf(1L to false), DislikeState.waitingDislikes(listOf(change(1, d, 1), change(1, l, 2))))
        val s = DislikeState()
        s.showWaiting(listOf(change(1, l, 1), change(2, d, 2)))
        assertTrue(s.marks.value.liked(1, null))
        assertTrue(s.marks.value.disliked(2, null))
    }
}
