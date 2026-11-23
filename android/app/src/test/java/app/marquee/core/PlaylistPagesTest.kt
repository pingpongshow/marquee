package app.marquee.core

import org.junit.Assert.assertEquals
import org.junit.Test

/** A long playlist is loaded whole, a page at a time. */
class PlaylistPagesTest {
    private fun pages(all: List<Int>, asked: MutableList<Int> = mutableListOf(), total: Int = all.size) =
        allPages(2) { offset, limit -> asked += offset; all.drop(offset).take(limit) to total }

    @Test fun readsEveryPage() {
        val asked = mutableListOf<Int>()
        assertEquals(listOf(1, 2, 3, 4, 5), pages(listOf(1, 2, 3, 4, 5), asked))
        assertEquals(listOf(0, 2, 4), asked)
    }

    @Test fun onePageIsOneRequest() {
        val asked = mutableListOf<Int>()
        assertEquals(listOf(1, 2), pages(listOf(1, 2), asked))
        assertEquals(listOf(0), asked)
        assertEquals(emptyList<Int>(), pages(emptyList()))
    }

    @Test fun stopsWhenThePlaylistGotShorter() {
        // The total says five, but only three are left: the empty page ends it.
        assertEquals(listOf(1, 2, 3), pages(listOf(1, 2, 3), total = 5))
    }
}
