package app.marquee.core

import app.marquee.api.models.ItemType

/**
 * The quality downloads use: the choice in Settings, one for songs and one for videos, else the
 * recommended one. Plain data in and out, so it can be tested without a device.
 */
object DownloadQuality {
    enum class Kind { Songs, Videos }

    /** Under this much free storage, the recommendation is one step lower. */
    const val LOW_STORAGE_BYTES = 16 * SmartSelection.GB

    fun kind(type: ItemType) = if (type == ItemType.TRACK) Kind.Songs else Kind.Videos

    /** Songs High; videos High on a tablet and Medium on a phone; one step lower when storage is short. */
    fun recommended(kind: Kind, tablet: Boolean, freeBytes: Long): Downloads.Quality {
        val q = if (kind == Kind.Videos && !tablet) Downloads.Quality.Medium else Downloads.Quality.High
        return if (freeBytes < LOW_STORAGE_BYTES) lower(q) else q
    }

    /** One step down; Low stays Low. */
    fun lower(q: Downloads.Quality) = when (q) {
        Downloads.Quality.Original -> Downloads.Quality.High
        Downloads.Quality.High -> Downloads.Quality.Medium
        Downloads.Quality.Medium, Downloads.Quality.Low -> Downloads.Quality.Low
    }

    /** "High (1080p)", "High (256 kbps)". */
    fun label(kind: Kind, q: Downloads.Quality): String = when (kind) {
        Kind.Videos -> when (q) {
            Downloads.Quality.Original -> "Original"
            Downloads.Quality.High -> "High (1080p)"
            Downloads.Quality.Medium -> "Medium (720p)"
            Downloads.Quality.Low -> "Low (480p)"
        }
        Kind.Songs -> when (q) {
            Downloads.Quality.Original -> "Original"
            Downloads.Quality.High -> "High (256 kbps)"
            Downloads.Quality.Medium -> "Medium (160 kbps)"
            Downloads.Quality.Low -> "Low (96 kbps)"
        }
    }

    /** Roughly the space it takes (the server's bit rates; a song of about four minutes). */
    fun sizeGuide(kind: Kind, q: Downloads.Quality): String = when (kind) {
        Kind.Videos -> when (q) {
            Downloads.Quality.Original -> "The file as it is, often 4 GB or more an hour"
            Downloads.Quality.High -> "About 2 GB an hour"
            Downloads.Quality.Medium -> "About 1 GB an hour"
            Downloads.Quality.Low -> "About 500 MB an hour"
        }
        Kind.Songs -> when (q) {
            Downloads.Quality.Original -> "The file as it is, up to about 30 MB a song"
            Downloads.Quality.High -> "About 8 MB a song"
            Downloads.Quality.Medium -> "About 5 MB a song"
            Downloads.Quality.Low -> "About 3 MB a song"
        }
    }

    /**
     * The extension for a downloaded file: the server's file name says (a converted song is
     * "<job>.m4a"). Without one, what that quality would be; players sniff the format anyway.
     */
    fun extension(fileName: String?, type: ItemType, q: Downloads.Quality): String =
        fileName?.substringAfterLast('.', "")?.takeIf { it.isNotEmpty() && it.length <= 5 }
            ?: when {
                type == ItemType.TRACK -> if (q == Downloads.Quality.Original) "audio" else "m4a"
                else -> if (q == Downloads.Quality.Original) "mkv" else "mp4"
            }
}
