package app.marquee.core

import app.marquee.api.models.ItemType
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * Favourites (the heart) set or cleared on this device, shown at once everywhere (item pages,
 * track rows, menus, Now Playing) before the server's lists catch up. [changes] is bumped once a
 * change reached the server (or was queued), so Home's Your Favourites row reloads.
 */
class FavoriteState {
    private val _marks = MutableStateFlow<Map<Long, Boolean>>(emptyMap())
    val marks: StateFlow<Map<Long, Boolean>> = _marks
    private val _changes = MutableStateFlow(0)
    val changes: StateFlow<Int> = _changes

    @Synchronized fun mark(item: Long, on: Boolean) { _marks.value = _marks.value + (item to on) }

    /** Forgets a mark (a refused change). */
    @Synchronized fun forget(item: Long) { _marks.value = _marks.value - item }

    fun changed() { _changes.value++ }

    companion object {
        /** What can be a favourite. */
        val types = setOf(ItemType.MOVIE, ItemType.SHOW, ItemType.EPISODE, ItemType.TRACK, ItemType.ALBUM, ItemType.ARTIST)
    }
}
