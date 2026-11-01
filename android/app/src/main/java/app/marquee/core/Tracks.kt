package app.marquee.core

import app.marquee.api.models.MediaStream
import app.marquee.api.models.MediaVersion
import java.util.Locale

/** The audio tracks and subtitles a video has, as its page lists them: every version's, each
 *  once. Labels match the web and Apple pages. */
data class TrackSummary(val audio: List<String>, val subtitles: List<String>) {
    companion object {
        /** For a video's page: null for anything without a video stream (music). */
        fun forPage(versions: List<MediaVersion>, locale: Locale = Locale.getDefault()): TrackSummary? {
            val streams = versions.flatMap { it.files.firstOrNull()?.streams.orEmpty() }
            if (streams.none { it.kind == MediaStream.Kind.VIDEO }) return null
            return of(streams, locale)
        }

        fun of(streams: List<MediaStream>, locale: Locale = Locale.getDefault()) = TrackSummary(
            audio = streams.filter { it.kind == MediaStream.Kind.AUDIO }.map { audioLabel(it, locale) }.distinct(),
            subtitles = streams.filter { it.kind == MediaStream.Kind.SUBTITLE }.map { s ->
                val flags = listOfNotNull("Forced".takeIf { s.forced }, "SDH".takeIf { s.hearingImpaired }).joinToString(", ")
                language(s.language, locale) + if (flags.isEmpty()) "" else " ($flags)"
            }.distinct(),
        )

        /** "English · Dolby TrueHD 7.1 · Commentary". */
        fun audioLabel(s: MediaStream, locale: Locale): String {
            val ch = s.channels?.takeIf { it > 0 }?.let { when { it == 1 -> "Mono"; it == 2 -> "Stereo"; it >= 6 -> "${it - 1}.1"; else -> "$it.0" } }
            // Track titles worth showing say what the track is, not the release group or its format again.
            val title = s.title?.trim()?.takeIf { usefulTitle.containsMatchIn(it) }
            return listOfNotNull(language(s.language, locale), listOfNotNull(codecName(s.codec, s.profile), ch).joinToString(" "), title).joinToString(" · ")
        }

        private val usefulTitle = Regex("commentary|descripti|narrat|isolated|score|brazil|latin|castil|european|canad|quebec|mandarin|cantonese", RegexOption.IGNORE_CASE)

        private val codecs = mapOf("truehd" to "Dolby TrueHD", "eac3" to "Dolby Digital Plus", "ac3" to "Dolby Digital", "aac" to "AAC", "flac" to "FLAC",
            "opus" to "Opus", "mp3" to "MP3", "mp2" to "MP2", "vorbis" to "Vorbis", "alac" to "ALAC")

        fun codecName(codec: String, profile: String?): String {
            val p = profile.orEmpty()
            if (codec == "dts") return when { "MA" in p -> "DTS-HD MA"; "HD" in p -> "DTS-HD"; "X" in p -> "DTS:X"; else -> "DTS" }
            if (codec.startsWith("pcm_")) return "PCM"
            val name = codecs[codec] ?: codec.uppercase()
            return if ("Atmos" in p) "$name Atmos" else name
        }

        // ISO 639-2 bibliographic codes (what files often carry) to the ones Locale knows.
        private val bibliographic = mapOf("fre" to "fra", "ger" to "deu", "chi" to "zho", "dut" to "nld", "cze" to "ces", "gre" to "ell",
            "rum" to "ron", "ice" to "isl", "may" to "msa", "per" to "fas", "arm" to "hye", "baq" to "eus", "slo" to "slk", "wel" to "cym",
            "alb" to "sqi", "geo" to "kat", "mac" to "mkd", "bur" to "mya", "tib" to "bod", "mao" to "mri")

        // Three-letter codes to the two-letter ones every Locale names ("fra" → "fr").
        private val twoLetter by lazy {
            Locale.getISOLanguages().mapNotNull { c -> runCatching { Locale(c).isO3Language to c }.getOrNull() }.toMap()
        }

        fun language(code: String?, locale: Locale): String {
            if (code.isNullOrBlank() || code == "und") return "Unknown"
            val tag = (bibliographic[code] ?: code).let { twoLetter[it] ?: it }
            return Locale.forLanguageTag(tag).getDisplayLanguage(locale).takeIf { it.isNotBlank() && !it.equals(tag, ignoreCase = true) } ?: code.uppercase()
        }
    }
}
