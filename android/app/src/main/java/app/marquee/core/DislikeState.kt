package app.marquee.core

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * The thumbs down on songs (MUSIC-25) as this device last set it, and what it does to ratings,
 * shown at once in every view before the server's lists catch up. A thumbs down clears a rating
 * of 3.5 stars or more; a rating of 3.5 or more (or a favourite) clears the thumbs down. The heart
 * follows through [FavoriteState].
 */
class DislikeState {
    /** This device's changes, over what a list or page last had from the server. */
    data class Marks(
        val dislikes: Map<Long, Boolean> = emptyMap(),
        val ratings: Map<Long, Double?> = emptyMap(),
        /** Thumbs downs given here: any rating of 3.5 or more that a list still has is gone. */
        val highCleared: Set<Long> = emptySet(),
    ) {
        fun disliked(id: Long, server: Boolean?): Boolean = dislikes[id] ?: (server == true)

        /** The rating to show for [id], where a list or page has [shown]. */
        fun rating(id: Long, shown: Double?): Double? {
            val r = if (id in ratings) ratings[id] else shown
            return if (id in highCleared) r?.takeIf { it < TOP_RATING } else r
        }

        fun thumbsDown(id: Long, on: Boolean) = copy(dislikes = dislikes + (id to on), highCleared = if (on) highCleared + id else highCleared)

        fun rated(id: Long, rating: Double?) = copy(
            dislikes = if (rating != null && rating >= TOP_RATING) dislikes + (id to false) else dislikes,
            ratings = ratings + (id to rating),
            highCleared = highCleared - id,
        )

        fun favourited(id: Long) = copy(dislikes = dislikes + (id to false))

        /** These marks with [id]'s as they are in [from] (a refused change going back). */
        fun restored(id: Long, from: Marks) = Marks(
            if (id in from.dislikes) dislikes + (id to from.dislikes.getValue(id)) else dislikes - id,
            if (id in from.ratings) ratings + (id to from.ratings[id]) else ratings - id,
            if (id in from.highCleared) highCleared + id else highCleared - id,
        )
    }

    private val _marks = MutableStateFlow(Marks())
    val marks: StateFlow<Marks> = _marks

    // Each returns what undoes it, for a change the server refuses.

    fun thumbsDown(id: Long, on: Boolean): () -> Unit = change(id) { it.thumbsDown(id, on) }
    fun rated(id: Long, rating: Double?): () -> Unit = change(id) { it.rated(id, rating) }
    fun favourited(id: Long): () -> Unit = change(id) { it.favourited(id) }

    /** Another profile's marks mean nothing to the next one. */
    @Synchronized fun clear() { _marks.value = Marks() }

    @Synchronized private fun change(id: Long, f: (Marks) -> Marks): () -> Unit {
        val before = _marks.value
        _marks.value = f(before)
        return { synchronized(this) { _marks.value = _marks.value.restored(id, before) } }
    }

    companion object {
        /** 3.5 stars on the server's 0–10 scale (topRatingSQL): where a rating and a thumbs down clear each other. */
        const val TOP_RATING = 7.0
    }
}
