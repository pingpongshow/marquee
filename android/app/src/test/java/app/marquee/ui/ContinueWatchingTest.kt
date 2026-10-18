package app.marquee.ui

import app.marquee.api.apis.HubsApi
import app.marquee.api.infrastructure.Serializer
import app.marquee.api.models.ContinueEntry
import app.marquee.api.models.Hub
import app.marquee.core.WatchChoices
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Continue Watching (USER-12, LIVE-6): the row's entries, View more, VOD routes, removal and progress. */
class ContinueWatchingTest {
    private val json = Serializer.kotlinxSerializationJson
    private val item = """{"id":12,"libraryId":1,"type":"movie","title":"Library film","childCount":0,"leafCount":0,"available":true,
        "matchState":"matched","addedAt":"2026-01-01T00:00:00Z","viewOffsetMs":600000,"durationMs":6000000}"""
    private val hub = """{"id":"continue-watching","title":"Continue Watching","items":[$item],"total":25,"entries":[
        {"kind":"vodEpisode","key":"vodEpisode:7","positionMs":300000,"durationMs":0,"lastWatchedAt":"2026-10-10T10:00:00Z",
         "vodEpisode":{"id":7,"seriesId":3,"seriesTitle":"A Show","season":1,"episode":3,"title":"Third","watched":false,"runtimeMs":1200000}},
        {"kind":"item","key":"item:12","positionMs":600000,"durationMs":6000000,"lastWatchedAt":"2026-10-09T10:00:00Z","item":$item},
        {"kind":"vodMovie","key":"vodMovie:5","positionMs":1000,"durationMs":4000,"lastWatchedAt":"2026-10-08T10:00:00Z",
         "vodMovie":{"id":5,"title":"A Film","year":1999,"watched":false}}]}"""

    @Test fun entriesInTheServersOrder() {
        val h = json.decodeFromString(Hub.serializer(), hub)
        assertEquals(listOf("vodEpisode:7", "item:12", "vodMovie:5"), continueEntries(h).map { it.key })
        assertTrue(continueHasMore(h))
        assertFalse(continueHasMore(h.copy(total = 3)))
    }

    @Test fun olderServerFallsBackToItems() {
        val h = json.decodeFromString(Hub.serializer(), hub).copy(propertyEntries = null, total = null)
        val e = continueEntries(h).single()
        assertEquals("item:12", e.key)
        assertEquals(ContinueEntry.Kind.ITEM, e.kind)
        assertFalse(continueHasMore(h))
    }

    @Test fun routesRemovalTitlesAndProgress() {
        val (ep, lib, film) = continueEntries(json.decodeFromString(Hub.serializer(), hub))
        assertEquals("vodepplay/3/7", continueRoute(ep))
        assertEquals("vodplay/5", continueRoute(film))
        assertNull(continueRoute(lib))
        assertEquals(HubsApi.KindRemoveVodFromContinueWatching.EPISODE to 7L, vodRemoval(ep))
        assertEquals(HubsApi.KindRemoveVodFromContinueWatching.MOVIE to 5L, vodRemoval(film))
        assertNull(vodRemoval(lib))
        assertEquals("A Show" to "VOD · S1 · E3 · Third", vodTitles(ep))
        assertEquals("A Film" to "VOD · 1999", vodTitles(film))
        // Unknown duration: the episode's runtime.
        assertEquals(0.25f, continueProgress(ep)!!, 0.001f)
        assertEquals(0.25f, continueProgress(film)!!, 0.001f)
    }

    @Test fun vodEntriesOfferBothWhilePartWay() {
        val (ep, _, film) = continueEntries(json.decodeFromString(Hub.serializer(), hub))
        val both = WatchChoices(markWatched = true, markUnwatched = true)
        assertEquals(both, vodWatchChoices(ep))
        assertEquals(both, vodWatchChoices(film))
        assertEquals(WatchChoices(markWatched = true, markUnwatched = false), vodWatchChoices(film.copy(positionMs = 0)))
    }
}
