package app.marquee.ui

import app.marquee.api.infrastructure.ServerException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** VOD Movies' and VOD Shows' filters (LIVE-6): kept as a line in the preferences, and the decade as a year range. */
class VodFiltersTest {
    @Test fun roundTrips() {
        val f = VodFilters(genre = "Sci-Fi & Fantasy", decade = 1980, minRating = 7, watched = "inProgress", sourceId = "src=1&2", inLibrary = false)
        assertEquals(f, VodFilters.decode(f.encode()))
        assertTrue(f.active)
        assertEquals(VodFilters(inLibrary = true), VodFilters.decode(VodFilters(inLibrary = true).encode()))
        assertTrue(VodFilters(inLibrary = false).active)
    }

    @Test fun emptyAndJunk() {
        assertEquals(VodFilters(), VodFilters.decode(null))
        assertEquals(VodFilters(), VodFilters.decode(""))
        assertEquals("", VodFilters().encode())
        assertFalse(VodFilters().active)
        // Unknown keys and bad values are dropped.
        assertEquals(VodFilters(genre = "Drama"), VodFilters.decode("genre=Drama&decade=abc&minRating=42&watched=maybe&inLibrary=perhaps&x=1&broken"))
    }

    @Test fun decadeIsTenYears() {
        val f = VodFilters(decade = 1990)
        assertEquals(1990, f.yearFrom)
        assertEquals(1999, f.yearTo)
        assertEquals(null, VodFilters().yearTo)
    }

    @Test fun startErrors() {
        assertTrue(vodStartError(ServerException("x", 502), "episode").startsWith("The source couldn't play this episode"))
        assertTrue(vodStartError(ServerException("x", 503), "movie").startsWith("The source is busy"))
        assertEquals("boom", vodStartError(RuntimeException("boom"), "movie"))
    }
}
