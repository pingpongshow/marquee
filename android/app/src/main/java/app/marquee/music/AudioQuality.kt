package app.marquee.music

import android.os.Bundle
import app.marquee.api.models.AudioFormat
import app.marquee.api.models.PlaybackSession

/** Show audio quality (MUSIC-23): labels for a track's format and what's actually streamed. */
object AudioQuality {
    fun codecLabel(codec: String): String {
        val c = codec.lowercase()
        return when {
            c == "flac" -> "FLAC"
            c == "alac" -> "ALAC"
            c == "mp3" -> "MP3"
            c == "aac" -> "AAC"
            c == "opus" -> "Opus"
            c == "vorbis" -> "Ogg Vorbis"
            c.startsWith("pcm_") -> "PCM"
            c == "wavpack" -> "WavPack"
            c == "ape" -> "APE"
            c.startsWith("dsd_") -> "DSD"
            else -> codec.uppercase()
        }
    }

    /** 44100 → "44.1", 96000 → "96". */
    private fun khz(rate: Int): String {
        val k = rate / 1000.0
        return if (k == Math.floor(k)) k.toInt().toString() else "%.1f".format(java.util.Locale.US, k).trimEnd('0').trimEnd('.')
    }

    fun isHiRes(f: AudioFormat) = f.lossless && ((f.bitDepth ?: 0) >= 24 || (f.sampleRate ?: 0) > 48000)

    /** "FLAC · 24-bit/96 kHz", "FLAC · 44.1 kHz", "MP3 · 320 kbps". */
    fun label(f: AudioFormat): String {
        val codec = codecLabel(f.codec)
        val detail = if (f.lossless) {
            val rate = f.sampleRate?.let { "${khz(it)} kHz" }
            when {
                f.bitDepth != null && rate != null -> "${f.bitDepth}-bit/$rate"
                f.bitDepth != null -> "${f.bitDepth}-bit"
                else -> rate
            }
        } else f.bitrateKbps?.let { "$it kbps" }
        return if (detail == null) codec else "$codec · $detail"
    }

    /** Compact, for track lists: "FLAC 24/96", "FLAC 44.1", "MP3 320". */
    fun short(f: AudioFormat): String {
        val codec = codecLabel(f.codec)
        val detail = if (f.lossless) listOfNotNull(f.bitDepth?.toString(), f.sampleRate?.let(::khz)).joinToString("/").ifEmpty { null }
        else f.bitrateKbps?.toString()
        return if (detail == null) codec else "$codec $detail"
    }

    /** What a session streams when it isn't the original file ("AAC 256 kbps"), else null. */
    fun streamed(s: PlaybackSession): String? {
        val d = s.decision
        if (d.method == app.marquee.api.models.PlaybackDecision.Method.DIRECT_PLAY || d.audioCopy) return null
        val codec = d.audioCodec?.let(::codecLabel)
        val kbps = d.audioKbps?.let { "$it kbps" } // the bitrate the server converts at
        return listOfNotNull(codec, kbps).joinToString(" ").ifEmpty {
            if (d.method == app.marquee.api.models.PlaybackDecision.Method.TRANSCODE) "transcoded" else "remuxed"
        }
    }

    /** Carries a format in a MediaItem's extras. */
    fun put(b: Bundle, f: AudioFormat) {
        b.putString("aq.codec", f.codec)
        b.putBoolean("aq.lossless", f.lossless)
        f.bitrateKbps?.let { b.putInt("aq.kbps", it) }
        f.sampleRate?.let { b.putInt("aq.rate", it) }
        f.bitDepth?.let { b.putInt("aq.depth", it) }
        f.channels?.let { b.putInt("aq.channels", it) }
    }

    fun from(b: Bundle?): AudioFormat? {
        val codec = b?.getString("aq.codec") ?: return null
        fun int(k: String) = if (b.containsKey(k)) b.getInt(k) else null
        return AudioFormat(codec, b.getBoolean("aq.lossless"), int("aq.kbps"), int("aq.rate"), int("aq.depth"), int("aq.channels"))
    }
}
