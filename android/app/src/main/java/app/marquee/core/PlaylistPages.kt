package app.marquee.core

import app.marquee.api.models.PlaylistEntry

/** The most playlist entries the server sends at once. */
private const val PLAYLIST_PAGE = 2000

/**
 * Every entry of a list the server sends a page at a time: [page] gives the entries from an
 * offset and how many there are in all. It stops at the total, or at a page that comes back
 * empty (the list got shorter meanwhile).
 */
internal fun <T> allPages(pageSize: Int, page: (offset: Int, limit: Int) -> Pair<List<T>, Int>): List<T> {
    val out = ArrayList<T>()
    while (true) {
        val (items, total) = page(out.size, pageSize)
        out += items
        if (items.isEmpty() || out.size >= total) return out
    }
}

/**
 * Every entry of a playlist, however long: asked for without a limit the server sends only the
 * first 500. Blocking, like the API calls it makes: not for the main thread.
 */
fun Marquee.allPlaylistEntries(id: Long): List<PlaylistEntry> =
    allPages(PLAYLIST_PAGE) { offset, limit -> playlists.listPlaylistItems(id, offset, limit).let { it.items to it.total } }
