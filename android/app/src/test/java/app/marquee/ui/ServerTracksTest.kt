package app.marquee.ui

import app.marquee.api.models.MediaStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The server's chosen audio is found among the file's own tracks as ExoPlayer lists them (PLAY-8). */
class ServerTracksTest {
    private fun stream(id: Long, kind: MediaStream.Kind, external: Boolean = false) =
        MediaStream(id = id, kind = kind, codec = "x", default = false, forced = false, hearingImpaired = false, external = external)

    private val streams = listOf(
        stream(10, MediaStream.Kind.VIDEO), stream(12, MediaStream.Kind.AUDIO), stream(11, MediaStream.Kind.AUDIO),
        stream(13, MediaStream.Kind.SUBTITLE), stream(14, MediaStream.Kind.AUDIO, external = true),
    )

    @Test fun findsTheChosenAudioInFileOrder() {
        assertEquals(0, serverAudioPosition(streams, 11, 2))
        assertEquals(1, serverAudioPosition(streams, 12, 2))
    }

    @Test fun nothingWhenItCantBeTold() {
        assertNull(serverAudioPosition(streams, 12, 3)) // the player sees another number of tracks
        assertNull(serverAudioPosition(streams, 14, 2)) // a track outside the file
        assertNull(serverAudioPosition(streams, null, 2))
    }
}
