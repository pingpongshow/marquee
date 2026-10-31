package app.marquee.music

import app.marquee.api.models.RadioRequest

/**
 * Steering a radio while it plays (MUSIC-17): the station's tracks the listener skipped quickly
 * and those they played through, sent with every top-up so the station leans away from one
 * sound and towards the other. Two quick skips in a row ask for an early top-up.
 */
class RadioSteering(private val keep: Int = 100) {
    private val skips = LinkedHashSet<Long>()
    private val finishes = LinkedHashSet<Long>()
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
        /** Moved on in the first 30 s and before half way (the server's definition of a skip). */
        fun quickSkip(positionMs: Long, durationMs: Long) = positionMs < 30_000 && (durationMs <= 0 || positionMs < durationMs / 2)

        /** Played to 80% or more, as the server counts a finished play. */
        fun playedThrough(positionMs: Long, durationMs: Long) = durationMs > 0 && positionMs >= durationMs * 4 / 5
    }
}
