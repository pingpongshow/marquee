package app.marquee.music

import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.OffsetDateTime

/** The music queue saved on the device comes back as it was, for the same person (the iOS fix's twin). */
class SavedQueueTest {
    private fun track(id: Long) = ItemSummary(id = id, libraryId = 1, type = ItemType.TRACK, title = "T$id", childCount = 0, leafCount = 0, available = true,
        matchState = ItemSummary.MatchState.entries.first(), addedAt = OffsetDateTime.parse("2026-01-01T00:00:00Z"))

    @Test fun roundTrips() {
        val q = SavedQueue(SavedQueue.owner("srv", 7)!!, listOf(track(1), track(2)),
            listOf(SavedQueue.Entry(1), SavedQueue.Entry(2, dj = "Wander"), SavedQueue.Entry(1)), index = 2, positionMs = 61_000, source = "Daily Mix 1")
        assertEquals(q, SavedQueue.decode(SavedQueue.encode(q)))
        assertNull(SavedQueue.decode("not json"))
        assertNull(SavedQueue.owner(null, 7))
    }

    @Test fun keepsTheCurrentEntryWhenTracksAreMissing() {
        val q = SavedQueue("o", listOf(track(2), track(3)), listOf(SavedQueue.Entry(1), SavedQueue.Entry(2), SavedQueue.Entry(3)), index = 2)
        val (entries, at) = q.playable()!!
        assertEquals(listOf(2L, 3L), entries.map { it.id })
        assertEquals(1, at)
        assertNull(SavedQueue("o", emptyList(), listOf(SavedQueue.Entry(1)), 0).playable())
    }
}
