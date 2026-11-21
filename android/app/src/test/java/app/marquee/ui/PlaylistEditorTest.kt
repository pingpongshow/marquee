package app.marquee.ui

import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.PlaylistEntry
import androidx.compose.ui.input.key.Key
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.OffsetDateTime

/** The playlist editor's sorting, moving and duplicate removal (MUSIC-29), and the PIN pad's keys. */
class PlaylistEditorTest {
    private fun entry(entryId: Long, itemId: Long, title: String, artist: String = "", album: String = "", index: Int? = null) = PlaylistEntry(
        entryId, ItemSummary(id = itemId, libraryId = 1, type = ItemType.TRACK, title = title, childCount = 0, leafCount = 0, available = true,
            matchState = ItemSummary.MatchState.entries.first(), addedAt = OffsetDateTime.now(), artistCredit = artist, parentTitle = album, index = index))

    private val list = listOf(
        entry(3, 30, "The Wall", "Pink Floyd", "The Wall", 2),
        entry(1, 10, "Angie", "The Rolling Stones", "Goats Head Soup", 1),
        entry(2, 20, "Money", "Pink Floyd", "The Dark Side of the Moon", 6),
        entry(4, 10, "Angie", "The Rolling Stones", "Goats Head Soup", 1),
    )
    private fun ids(l: List<PlaylistEntry>) = l.map { it.entryId }

    @Test fun sorts() {
        // "The" is ignored: Angie, Money, The Wall.
        assertEquals(listOf(1L, 4L, 2L, 3L), ids(PlaylistEdits.sorted(list, PlaylistSort.Title)))
        // By artist, then album.
        assertEquals(listOf(2L, 3L, 1L, 4L), ids(PlaylistEdits.sorted(list, PlaylistSort.Artist)))
        // "The Dark Side…" files under D: Dark Side, Goats Head Soup, Wall.
        assertEquals(listOf(2L, 1L, 4L, 3L), ids(PlaylistEdits.sorted(list, PlaylistSort.Album)))
        assertEquals(listOf(1L, 2L, 3L, 4L), ids(PlaylistEdits.sorted(list, PlaylistSort.Added)))
        assertEquals(listOf(4L, 2L, 1L, 3L), ids(PlaylistEdits.sorted(list, PlaylistSort.Reverse)))
        assertEquals(list.toSet(), PlaylistEdits.sorted(list, PlaylistSort.Shuffle).toSet())
    }

    @Test fun duplicatesAndMoves() {
        assertEquals(1, PlaylistEdits.duplicates(list))
        assertEquals(listOf(3L, 1L, 2L), ids(PlaylistEdits.withoutDuplicates(list)))
        assertEquals(listOf(2L, 4L, 3L, 1L), ids(PlaylistEdits.moved(list, setOf(2, 4), top = true)))
        assertEquals(listOf(3L, 1L, 2L, 4L), ids(PlaylistEdits.moved(list, setOf(2, 4), top = false)))
        assertEquals(listOf(1L, 3L, 2L, 4L), ids(PlaylistEdits.nudged(list, 0, 1)))
        assertEquals(list, PlaylistEdits.nudged(list, 0, -1))
    }

    @Test fun pinKeys() {
        assertEquals(PinKey.Digit('7'), pinKey(Key.Seven))
        assertEquals(PinKey.Digit('3'), pinKey(Key.NumPad3))
        assertEquals(PinKey.Back, pinKey(Key.Backspace))
        assertNull(pinKey(Key.A))
        assertEquals("123", pinAfter("12", PinKey.Digit('3')))
        assertEquals("1234", pinAfter("1234", PinKey.Digit('5')))
        assertEquals("1", pinAfter("12", PinKey.Back))
        assertEquals("", pinAfter("", PinKey.Back))
    }
}
