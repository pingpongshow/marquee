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
    }
}
