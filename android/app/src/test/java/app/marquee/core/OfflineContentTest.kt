package app.marquee.core

import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.OffsetDateTime

/** How the Offline Library groups what's downloaded. */
class OfflineContentTest {
    private fun item(id: Long, type: ItemType, title: String, parent: Long? = null, parentTitle: String? = null, grandparent: Long? = null,
        grandparentTitle: String? = null, index: Int? = null, disc: Int? = null) = ItemSummary(
        id = id, libraryId = 1, type = type, title = title, childCount = 0, leafCount = 0, available = true,
        matchState = ItemSummary.MatchState.entries.first(), addedAt = OffsetDateTime.now(), index = index, disc = disc,
        parentId = parent, grandparentId = grandparent, parentTitle = parentTitle, grandparentTitle = grandparentTitle)

    private fun ep(id: Long, season: String, index: Int) = item(id, ItemType.EPISODE, "E$index", 10, season, 1, "Show", index)

    @Test fun nothingIsEmpty() {
        assertTrue(OfflineContent.of(emptyList()).isEmpty)
        assertFalse(OfflineContent.of(listOf(item(1, ItemType.MOVIE, "Film"))).isEmpty)
    }

    @Test fun moviesAndVideosByTitle() {
        val c = OfflineContent.of(listOf(item(1, ItemType.MOVIE, "b film"), item(2, ItemType.VIDEO, "A clip")))
        assertEquals(listOf(2L, 1L), c.movies.map { it.id })
        assertTrue(c.shows.isEmpty() && c.songs.isEmpty())
    }

    @Test fun episodesUnderTheirShowInViewingOrder() {
        val c = OfflineContent.of(listOf(ep(3, "Season 10", 1), ep(2, "Season 2", 2), ep(1, "Season 2", 1), ep(4, "Specials", 1),
            item(9, ItemType.EPISODE, "Other", 20, "Season 1", 2, "Another", 1)))
        assertEquals(listOf("Another", "Show"), c.shows.map { it.title })
        // Season 2 before Season 10; specials, with no number, last.
        assertEquals(listOf(1L, 2L, 3L, 4L), c.shows[1].episodes.map { it.id })
    }

    @Test fun tracksAreSongsAndUnderTheirAlbum() {
        val c = OfflineContent.of(listOf(
            item(2, ItemType.TRACK, "Two", 5, "Album", 7, "Artist", index = 2),
            item(1, ItemType.TRACK, "One", 5, "Album", 7, "Artist", index = 1),
            item(3, ItemType.TRACK, "Disc two", 5, "Album", 7, "Artist", index = 1, disc = 2),
            item(4, ItemType.TRACK, "Single"),
        ))
        assertEquals(1, c.albums.size)
        assertEquals("Artist", c.albums[0].artist)
        assertEquals(listOf(1L, 2L, 3L), c.albums[0].tracks.map { it.id })
        // A song without an album is still among the songs.
        assertEquals(setOf(1L, 2L, 3L, 4L), c.songs.map { it.id }.toSet())
    }

    @Test fun seasonNumbers() {
        assertEquals(12, OfflineContent.season("Season 12"))
        assertEquals(Int.MAX_VALUE, OfflineContent.season("Specials"))
        assertEquals(Int.MAX_VALUE, OfflineContent.season(null))
    }
}
