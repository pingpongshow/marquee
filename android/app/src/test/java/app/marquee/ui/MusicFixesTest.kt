package app.marquee.ui

import app.marquee.api.models.ItemType
import app.marquee.api.models.MusicMatchCandidate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Fix match for music (META-3): which items offer it, and how MusicBrainz candidates are described. */
class MusicFixesTest {
    @Test fun artistsAndAlbumsOnly() {
        assertTrue(musicMatchable(ItemType.ARTIST))
        assertTrue(musicMatchable(ItemType.ALBUM))
        assertFalse(musicMatchable(ItemType.TRACK))
        assertFalse(musicMatchable(ItemType.MOVIE))
    }

    @Test fun typeLabels() {
        assertEquals("EP", musicTypeLabel("ep"))
        assertEquals("Compilation", musicTypeLabel("compilation"))
        assertEquals("", musicTypeLabel(null))
    }

    @Test fun artistDetails() {
        val c = MusicMatchCandidate("m", "Nirvana", type = "group", country = "US", beginYear = 1987, endYear = 1994)
        assertEquals("Group · US · 1987–1994", musicCandidateDetails(c, album = false))
        assertEquals("Person · 1990–", musicCandidateDetails(MusicMatchCandidate("m", "X", type = "person", beginYear = 1990), album = false))
        assertEquals("", musicCandidateDetails(MusicMatchCandidate("m", "X"), album = false))
    }

    @Test fun albumDetails() {
        val c = MusicMatchCandidate("m", "Nevermind", type = "album", year = 1991, artist = "Nirvana", trackCount = 12, country = "US")
        assertEquals("Nirvana · Album · 1991 · 12 tracks", musicCandidateDetails(c, album = true))
    }
}
