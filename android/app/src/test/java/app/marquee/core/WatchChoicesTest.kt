package app.marquee.core

import app.marquee.api.infrastructure.Serializer
import app.marquee.api.models.ItemSummary
import org.junit.Assert.assertEquals
import org.junit.Test

/** Mark as watched / Mark as unwatched (PLAY-5): what menus offer for videos, shows and seasons. */
class WatchChoicesTest {
    private fun item(type: String, extra: String = "", leafCount: Int = 0) = Serializer.kotlinxSerializationJson.decodeFromString(ItemSummary.serializer(),
        """{"id":1,"libraryId":1,"type":"$type","title":"T","childCount":0,"leafCount":$leafCount,"available":true,
            "matchState":"matched","addedAt":"2026-01-01T00:00:00Z"$extra}""")

    private val watchedOnly = WatchChoices(markWatched = true, markUnwatched = false)
    private val unwatchedOnly = WatchChoices(markWatched = false, markUnwatched = true)
    private val both = WatchChoices(markWatched = true, markUnwatched = true)

    @Test fun videos() {
        assertEquals(watchedOnly, WatchState.choices(item("movie"), null))
        assertEquals(unwatchedOnly, WatchState.choices(item("episode", ""","viewCount":1"""), null))
        // Part-way through (Continue Watching, On Deck): both.
        assertEquals(both, WatchState.choices(item("movie", ""","viewOffsetMs":60000"""), null))
        assertEquals(both, WatchState.choices(item("video", ""","viewOffsetMs":1"""), null))
    }

    @Test fun showsAndSeasons() {
        assertEquals(watchedOnly, WatchState.choices(item("show", ""","watchedLeafCount":0""", leafCount = 10), null))
        assertEquals(both, WatchState.choices(item("season", ""","watchedLeafCount":3""", leafCount = 10), null))
        assertEquals(unwatchedOnly, WatchState.choices(item("show", ""","watchedLeafCount":10""", leafCount = 10), null))
    }

    @Test fun thisDevicesMarkWins() {
        val partWay = item("episode", ""","viewOffsetMs":60000""")
        assertEquals(unwatchedOnly, WatchState.choices(partWay, true))
        // Unwatched forgets the resume point too.
        assertEquals(watchedOnly, WatchState.choices(partWay, false))
        assertEquals(unwatchedOnly, WatchState.choices(item("movie"), true))
    }

    @Test fun fromFlags() {
        assertEquals(watchedOnly, WatchState.choices(watched = false, hasState = false))
        assertEquals(both, WatchState.choices(watched = false, hasState = true))
        assertEquals(unwatchedOnly, WatchState.choices(watched = true, hasState = false))
    }
}
