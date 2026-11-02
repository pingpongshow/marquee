package app.marquee.core

import app.marquee.api.models.ItemType
import app.marquee.core.DownloadQuality.Kind
import app.marquee.core.Downloads.Quality
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The quality downloads use until one is chosen, and how a converted file is named. */
class DownloadQualityTest {
    private val plenty = 64 * SmartSelection.GB
    private val short = DownloadQuality.LOW_STORAGE_BYTES - 1

    @Test fun songsAreHigh() {
        assertEquals(Quality.High, DownloadQuality.recommended(Kind.Songs, tablet = false, plenty))
        assertEquals(Quality.High, DownloadQuality.recommended(Kind.Songs, tablet = true, plenty))
    }

    @Test fun videosFollowTheDevice() {
        assertEquals(Quality.Medium, DownloadQuality.recommended(Kind.Videos, tablet = false, plenty))
        assertEquals(Quality.High, DownloadQuality.recommended(Kind.Videos, tablet = true, plenty))
    }

    @Test fun oneStepLowerWhenStorageIsShort() {
        assertEquals(Quality.Medium, DownloadQuality.recommended(Kind.Songs, tablet = false, short))
        assertEquals(Quality.Low, DownloadQuality.recommended(Kind.Videos, tablet = false, short))
        assertEquals(Quality.Medium, DownloadQuality.recommended(Kind.Videos, tablet = true, short))
    }

    @Test fun exactly16GbIsNotShort() {
        assertEquals(16_000_000_000L, DownloadQuality.LOW_STORAGE_BYTES)
        assertEquals(Quality.Medium, DownloadQuality.recommended(Kind.Videos, tablet = false, DownloadQuality.LOW_STORAGE_BYTES))
    }

    @Test fun neverBelowLow() = assertEquals(Quality.Low, DownloadQuality.lower(Quality.Low))

    @Test fun everyOptionHasALabelAndSizeGuide() {
        Kind.entries.forEach { k ->
            Quality.entries.forEach { q ->
                assertTrue(DownloadQuality.label(k, q).startsWith(q.name))
                assertTrue(DownloadQuality.sizeGuide(k, q).isNotBlank())
            }
        }
        assertEquals("High (256 kbps)", DownloadQuality.label(Kind.Songs, Quality.High))
        assertEquals("Low (480p)", DownloadQuality.label(Kind.Videos, Quality.Low))
    }

    @Test fun kindsFollowTheItem() {
        assertEquals(Kind.Songs, DownloadQuality.kind(ItemType.TRACK))
        listOf(ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO).forEach { assertEquals(Kind.Videos, DownloadQuality.kind(it)) }
    }

    @Test fun theServersFileNameGivesTheExtension() {
        assertEquals("m4a", DownloadQuality.extension("abc123.m4a", ItemType.TRACK, Quality.High))
        assertEquals("flac", DownloadQuality.extension("01 Song.flac", ItemType.TRACK, Quality.Original))
        assertEquals("mp4", DownloadQuality.extension("abc123.mp4", ItemType.MOVIE, Quality.Low))
    }

    @Test fun withoutOneTheExtensionFollowsTheKind() {
        assertEquals("m4a", DownloadQuality.extension(null, ItemType.TRACK, Quality.Medium))
        assertEquals("audio", DownloadQuality.extension(null, ItemType.TRACK, Quality.Original))
        assertEquals("mp4", DownloadQuality.extension(null, ItemType.MOVIE, Quality.Medium))
        assertEquals("mkv", DownloadQuality.extension(null, ItemType.EPISODE, Quality.Original))
        // No usable extension in the name.
        assertEquals("m4a", DownloadQuality.extension("noextension", ItemType.TRACK, Quality.Low))
    }

    @Test fun aConvertedSongIsEstimatedAtItsBitRate() {
        // Four minutes: at 256 kbps; a 128 kbps original stays 128 (the server never goes above it).
        assertEquals(7_680_000L, SmartSelection.estimateTrackBytes(240_000, 320, false, Quality.High))
        assertEquals(3_840_000L, SmartSelection.estimateTrackBytes(240_000, 128, false, Quality.High))
        assertEquals(30_000_000L, SmartSelection.estimateTrackBytes(240_000, null, true))
    }
}
