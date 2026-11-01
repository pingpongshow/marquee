package app.marquee.core

import app.marquee.api.models.MediaFile
import app.marquee.api.models.MediaStream
import app.marquee.api.models.MediaStream.Kind
import app.marquee.api.models.MediaVersion
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.util.Locale

/** The audio and subtitles a title's page lists. */
class TrackSummaryTest {
    private var nextId = 0L
    private fun stream(kind: Kind, language: String?, codec: String = "", profile: String? = null, channels: Int? = null,
                       title: String? = null, forced: Boolean = false, sdh: Boolean = false) =
        MediaStream(id = ++nextId, kind = kind, codec = codec, default = false, forced = forced, hearingImpaired = sdh, external = false,
            profile = profile, language = language, title = title, channels = channels)

    @Test fun labels() {
        val t = TrackSummary.of(listOf(
            stream(Kind.VIDEO, null, "hevc"),
            stream(Kind.AUDIO, "eng", "truehd", "Dolby TrueHD + Dolby Atmos", 8),
            stream(Kind.AUDIO, "eng", "ac3", channels = 2, title = "Director's Commentary"),
            stream(Kind.AUDIO, "fre", "dts", "DTS-HD MA", 6, "French 5.1"),
            stream(Kind.AUDIO, "eng", "eac3", channels = 6, title = "MiNX - Small HD episodes"),
            stream(Kind.SUBTITLE, "eng", "subrip"), stream(Kind.SUBTITLE, "eng", "pgs"),
            stream(Kind.SUBTITLE, "eng", "subrip", sdh = true), stream(Kind.SUBTITLE, "ger", "subrip", forced = true),
            stream(Kind.SUBTITLE, "und", "subrip"),
        ), Locale.ENGLISH)
        assertEquals(listOf("English · Dolby TrueHD Atmos 7.1", "English · Dolby Digital Stereo · Director's Commentary",
            "French · DTS-HD MA 5.1", "English · Dolby Digital Plus 5.1"), t.audio)
        assertEquals(listOf("English", "English (SDH)", "German (Forced)", "Unknown"), t.subtitles)
    }

    @Test fun onlyVideos() {
        fun version(vararg s: MediaStream) = MediaVersion(1, "", listOf(MediaFile(1, 1, 0, true, s.toList())))
        assertNull(TrackSummary.forPage(listOf(version(stream(Kind.AUDIO, "eng", "flac", channels = 2))), Locale.ENGLISH))
        val t = TrackSummary.forPage(listOf(version(stream(Kind.VIDEO, null, "h264"), stream(Kind.AUDIO, "eng", "aac", channels = 2)),
            version(stream(Kind.VIDEO, null, "hevc"), stream(Kind.AUDIO, "eng", "aac", channels = 2))), Locale.ENGLISH)
        assertEquals(listOf("English · AAC Stereo"), t?.audio)
        assertEquals(emptyList<String>(), t?.subtitles)
    }
}
