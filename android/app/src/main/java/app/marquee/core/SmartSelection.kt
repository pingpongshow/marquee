package app.marquee.core

/**
 * What smart downloads keep on the device, worked out from what's wanted and the space allowed.
 * Plain data in and out, so it can be tested without a device or a server.
 */
object SmartSelection {
    /** Something that could be downloaded: its item and how many bytes it takes (known, or estimated). */
    data class Candidate(val id: Long, val bytes: Long)

    /**
     * A show being watched: when it was last watched and its next unwatched episodes, in viewing
     * order. [failed] means its episodes couldn't be fetched this time: what it has stays.
     */
    data class ShowQueue(val showId: Long, val lastWatchedMs: Long, val episodes: List<Candidate>, val failed: Boolean = false)

    /** What to change: download these (in this order) and remove these. */
    data class Plan(val download: List<Long>, val remove: List<Long>)

    /**
     * Episodes to keep: the next [perShow] unwatched episodes of each show, the most recently
     * watched show first, within [budget] bytes. A show stops at its first episode that doesn't
     * fit (an episode isn't kept without the ones before it), so the least recently watched shows
     * are the ones left out.
     */
    fun episodes(shows: List<ShowQueue>, perShow: Int, budget: Long): List<Long> {
        var used = 0L
        val kept = mutableListOf<Long>()
        for (s in shows.sortedByDescending { it.lastWatchedMs }) {
            for (e in s.episodes.take(perShow.coerceAtLeast(0))) {
                if (e.id in kept) continue
                if (used + e.bytes > budget) break
                used += e.bytes
                kept += e.id
            }
        }
        return kept
    }

    /**
     * Tracks to keep, in priority order (favourites first, then most and recently played): each
     * one that still fits in [budget] bytes is kept; one too big for what's left is skipped and
     * smaller ones after it can still go in. Repeats count once.
     */
    fun tracks(candidates: List<Candidate>, budget: Long): List<Long> {
        var used = 0L
        val seen = HashSet<Long>()
        val kept = mutableListOf<Long>()
        for (c in candidates) {
            if (!seen.add(c.id)) continue
            if (used + c.bytes > budget) continue
            used += c.bytes
            kept += c.id
        }
        return kept
    }

    /**
     * The changes that bring the device to [wanted]: what isn't there yet is downloaded, smart
     * downloads no longer wanted (watched, or past the budget) are removed. Downloads made by hand
     * ([manual]) are never removed and never downloaded again. [failed] smart downloads are
     * removed, and downloaded again when still wanted.
     */
    fun plan(wanted: List<Long>, smartNow: Set<Long>, manual: Set<Long>, failed: Set<Long> = emptySet()): Plan {
        val want = wanted.toSet()
        val remove = (smartNow - want - manual) + (failed intersect smartNow)
        val download = wanted.filter { it !in manual && (it !in smartNow || it in failed) }.distinct()
        return Plan(download, remove.toList())
    }

    /** Roughly how big a video download is, before the server says: its length at the quality's bit rate. */
    fun estimateVideoBytes(durationMs: Long?, quality: Downloads.Quality): Long {
        val kbps = when (quality) {
            Downloads.Quality.Original -> 8000
            Downloads.Quality.High -> 6000
            Downloads.Quality.Medium -> 3000
            Downloads.Quality.Low -> 1200
        }
        val ms = durationMs?.takeIf { it > 0 } ?: (45 * 60_000L)
        return ms * kbps / 8
    }

    /** Roughly how big a track is: its length at its bit rate (lossless about 1000 kbps, else 320). */
    fun estimateTrackBytes(durationMs: Long?, bitrateKbps: Int?, lossless: Boolean): Long {
        val kbps = bitrateKbps?.takeIf { it > 0 } ?: if (lossless) 1000 else 320
        val ms = durationMs?.takeIf { it > 0 } ?: (4 * 60_000L)
        return ms * kbps / 8
    }

    const val GB = 1_000_000_000L
    /** The storage budgets offered, in GB. */
    val budgets = listOf(1, 2, 5, 10, 20, 50)
    /** How many episodes ahead to keep. */
    val episodeCounts = listOf(1, 2, 3, 5)
}
