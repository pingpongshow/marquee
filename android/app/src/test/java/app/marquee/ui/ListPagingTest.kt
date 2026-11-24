package app.marquee.ui

import org.junit.Assert.assertEquals
import org.junit.Test

/** Paged lists: a next page never repeats a shown title. */
class ListPagingTest {
    @Test fun nextPageSkipsTitlesAlreadyShown() {
        // New titles at the top moved the list on: the next page starts with ones already shown.
        val shown = listOf("m1", "m2", "m3")
        assertEquals(listOf("m1", "m2", "m3", "m4", "m5"), appendUnseen(shown, listOf("m3", "m4", "m5"), { it }))
        // A page repeating itself adds each title once.
        assertEquals(listOf("m1", "m2"), appendUnseen(listOf("m1"), listOf("m2", "m2", "m1"), { it }))
    }
}
