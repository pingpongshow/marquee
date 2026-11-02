package app.marquee.music

import app.marquee.api.models.RadioRequest

/**
 * Steering a radio while it plays (MUSIC-17): the station's tracks the listener skipped quickly
 * and those they played through, sent with every top-up so the station leans away from one
 * sound and towards the other. Two quick skips in a row ask for an early top-up.
 */
class RadioSteering(private val keep: Int = 100, private val historyLimit: Int = MAX_EXCLUDE) {
    private val skips = LinkedHashSet<Long>()
    private val finishes = LinkedHashSet<Long>()
    private val history = LinkedHashSet<Long>()
    /** Quick skips since the last track played through or the last top-up. */
    var inARow = 0
        private set

    /** The latest quick skips and played-through tracks, oldest first. */
    val skipped: List<Long> get() = skips.toList()
    val finished: List<Long> get() = finishes.toList()

    /** A track played through: steers towards it and ends a run of skips. */
    fun finished(id: Long) {
        note(finishes, skips, id)
        inARow = 0
    }

    /** A quick skip; true from the second in a row, when the station should top up early. */
    fun skipped(id: Long): Boolean {
        note(skips, finishes, id)
        return ++inARow >= 2
    }

    /**
     * A track left the player at positionMs of durationMs; natural when it ended by itself.
     * True when the station should top up early.
     */
    fun left(id: Long, positionMs: Long, durationMs: Long, natural: Boolean): Boolean = when {
        natural || playedThrough(positionMs, durationMs) -> { finished(id); false }
        quickSkip(positionMs, durationMs) -> skipped(id)
        else -> false
    }

    fun toppedUp() { inARow = 0 }

    /**
     * What a top-up leaves out: everything this station has queued so far, oldest first and each
     * track at its latest place, up to the most recent [historyLimit]. It outlives the queue,
     * which forgets what played long ago. When the station has been through everything, the server
     * repeats it, leaving out only the end of this list.
     */
    fun exclude(queue: List<Long>): List<Long> {
        queue.forEach { history.remove(it); history.add(it) }
        val over = history.size - historyLimit
        if (over > 0) history.toList().take(over).forEach(history::remove)
        return history.toList()
    }

    /** The station's request continued: what's queued left out, and the steering so far. */
    fun request(base: RadioRequest, exclude: List<Long>, limit: Int = 25): RadioRequest =
        base.copy(exclude = exclude, limit = limit, skipped = skipped.ifEmpty { null }, finished = finished.ifEmpty { null })

    // The latest wins: a track lives in one list, and only the newest few are kept.
    private fun note(into: LinkedHashSet<Long>, other: LinkedHashSet<Long>, id: Long) {
        other.remove(id)
        into.remove(id)
        into.add(id)
        while (into.size > keep) into.remove(into.first())
    }

    companion object {
        /** How many tracks a top-up leaves out at most. */
        const val MAX_EXCLUDE = 2000
        /** How many played tracks stay in the queue of a radio, once it's trimmed. */
        const val KEEP_PLAYED = 100

        /**
         * What to add of a top-up's [tracks]: those not already coming up, so nothing plays twice
         * in close succession; but all of them rather than none, because a station that adds
         * nothing stops (a small library comes round again straight away).
         */
        fun <T> toAdd(tracks: List<T>, upcoming: Set<Long>, id: (T) -> Long): List<T> =
            tracks.filter { id(it) !in upcoming }.ifEmpty { tracks }

        /** How many of the played tracks before [current] to drop from a long-playing radio's queue (in batches, not each time). */
        fun toTrim(current: Int, keep: Int = KEEP_PLAYED): Int = if (current >= keep * 2) current - keep else 0

        /** Moved on in the first 30 s and before half way (the server's definition of a skip). */
        fun quickSkip(positionMs: Long, durationMs: Long) = positionMs < 30_000 && (durationMs <= 0 || positionMs < durationMs / 2)

        /** Played to 80% or more, as the server counts a finished play. */
        fun playedThrough(positionMs: Long, durationMs: Long) = durationMs > 0 && positionMs >= durationMs * 4 / 5
    }
}

/**
 * Media ids of queue entries. A track's id is its entry's, as the player's items must each have
 * their own: when a radio comes round to a track again, the new entry is "<id>~<n>".
 */
object QueueIds {
    /** The track an entry plays, or null for a browse id ("album:3"). */
    fun trackId(mediaId: String?): Long? = mediaId?.substringBefore('~')?.toLongOrNull()

    /** A media id for each of [tracks] that nothing in [taken] or before it in the list has. */
    fun assign(tracks: List<Long>, taken: Collection<String> = emptyList()): List<String> {
        val used = HashSet(taken)
        return tracks.map { id ->
            var mediaId = id.toString()
            var n = 2
            while (!used.add(mediaId)) mediaId = "$id~${n++}"
            mediaId
        }
    }
}
