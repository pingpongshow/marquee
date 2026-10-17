package app.marquee.ui

import org.junit.Assert.assertEquals
import org.junit.Test

/** Paged lists and playlists: a next page never repeats a shown title, and a repeated video keeps its own place. */
class ListPagingTest {
    @Test fun nextPageSkipsTitlesAlreadyShown() {
        // New titles at the top moved the list on: the next page starts with ones already shown.
        val shown = listOf("m1", "m2", "m3")
        assertEquals(listOf("m1", "m2", "m3", "m4", "m5"), appendUnseen(shown, listOf("m3", "m4", "m5"), { it }))
        // A page repeating itself adds each title once.
        assertEquals(listOf("m1", "m2"), appendUnseen(listOf("m1"), listOf("m2", "m2", "m1"), { it }))
    }

    @Test fun repeatedVideoPlaysOnFromItsOwnPlace() {
        // A playlist: video 7, a track, video 8, video 7 again.
        val entries = listOf("v7", "t1", "v8", "v7")
        val isVideo = { s: String -> s.startsWith("v") }
        assertEquals(0, positionAmong(entries, 0, isVideo))
        assertEquals(-1, positionAmong(entries, 1, isVideo))
        assertEquals(1, positionAmong(entries, 2, isVideo))
        // The second copy of video 7 is the third video, not the first.
        assertEquals(2, positionAmong(entries, 3, isVideo))
        assertEquals(-1, positionAmong(entries, 9, isVideo))
    }
}
