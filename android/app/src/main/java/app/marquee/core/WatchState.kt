package app.marquee.core

import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * Watched and unwatched marks made on this device, shown at once everywhere (the item page,
 * menus, tile badges) before the server's lists catch up. A mark on a show or season covers
 * its episodes; the latest mark wins. [changes] is bumped once a change reached the server
 * (or was queued), so Home rows and item pages reload.
 */
/** Which of Mark as watched and Mark as unwatched to offer. */
data class WatchChoices(val markWatched: Boolean, val markUnwatched: Boolean)

class WatchState {
    data class Mark(val on: Boolean, val seq: Long)

    private var seq = 0L
    private val _marks = MutableStateFlow<Map<Long, Mark>>(emptyMap())
    val marks: StateFlow<Map<Long, Mark>> = _marks
    private val _changes = MutableStateFlow(0)
    /** Bumped after watched state, Continue Watching or similar changed on the server. */
    val changes: StateFlow<Int> = _changes

    @Synchronized fun mark(item: Long, on: Boolean) { _marks.value = _marks.value + (item to Mark(on, ++seq)) }

    /** Forgets marks (a refused change, or an item played again). */
    @Synchronized fun forget(vararg items: Long?) { _marks.value = _marks.value - items.filterNotNull().toSet() }

    fun changed() { _changes.value++ }

    /** This device's latest mark covering the item (its own, its season's or its show's). */
    fun markFor(item: ItemSummary, marks: Map<Long, Mark> = _marks.value): Mark? =
        listOfNotNull(marks[item.id], item.parentId?.let { marks[it] }, item.grandparentId?.let { marks[it] }).maxByOrNull { it.seq }

    companion object {
        val videoTypes = setOf(ItemType.MOVIE, ItemType.SHOW, ItemType.SEASON, ItemType.EPISODE, ItemType.VIDEO)

        /** What the server says: a played video, or a show or season with every episode played. */
        fun serverWatched(item: ItemSummary): Boolean =
            (item.viewCount ?: 0) > 0 || (item.leafCount > 0 && item.watchedLeafCount == item.leafCount)

        /**
         * Whether the server has any watch state for the item: a video played or part-way
         * through, or a show or season with any episode played.
         */
        fun serverHasWatchState(item: ItemSummary): Boolean =
            if (item.type == ItemType.SHOW || item.type == ItemType.SEASON) (item.viewCount ?: 0) > 0 || (item.watchedLeafCount ?: 0) > 0
            else (item.viewCount ?: 0) > 0 || (item.viewOffsetMs ?: 0) > 0

        /**
         * What a menu offers (PLAY-5): Mark as watched unless it's fully watched, Mark as unwatched
         * whenever there's any watch state, so something part-way through offers both. [local] is
         * this device's latest mark or queued change, which wins over the server's values.
         */
        fun choices(item: ItemSummary, local: Boolean?): WatchChoices =
            local?.let(::choicesAfterMark) ?: choices(serverWatched(item), serverHasWatchState(item))

        /** The choices from a watched flag and whether there's any watch state (played or part-way through). */
        fun choices(watched: Boolean, hasState: Boolean) = WatchChoices(markWatched = !watched, markUnwatched = watched || hasState)

        /** After marking watched only unwatched is offered; after unwatched (which forgets the resume point) only watched. */
        fun choicesAfterMark(on: Boolean) = WatchChoices(markWatched = !on, markUnwatched = on)
    }
}
