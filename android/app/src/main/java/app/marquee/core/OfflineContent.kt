package app.marquee.core

import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType

/**
 * What's downloaded, as the Offline Library lists it: movies, shows with their episodes, albums
 * and songs. Plain data in and out, so it can be tested without a device.
 */
data class OfflineContent(
    val movies: List<ItemSummary> = emptyList(),
    val shows: List<Show> = emptyList(),
    val albums: List<Album> = emptyList(),
    val songs: List<ItemSummary> = emptyList(),
) {
    data class Show(val id: Long, val title: String, val episodes: List<ItemSummary>)
    data class Album(val id: Long, val title: String, val artist: String?, val tracks: List<ItemSummary>)

    val isEmpty: Boolean get() = movies.isEmpty() && shows.isEmpty() && songs.isEmpty()

    companion object {
        /** Sorts [items] (what's on the device) into its groups, each in the order it plays. */
        fun of(items: List<ItemSummary>): OfflineContent {
            val tracks = items.filter { it.type == ItemType.TRACK }
            return OfflineContent(
                // Other videos (home videos, extras) are played the same way.
                movies = items.filter { it.type == ItemType.MOVIE || it.type == ItemType.VIDEO }.sortedBy { it.title.lowercase() },
                shows = items.filter { it.type == ItemType.EPISODE }.groupBy { it.grandparentId ?: it.parentId ?: it.id }.map { (id, eps) ->
                    Show(id, eps.first().grandparentTitle ?: eps.first().parentTitle ?: eps.first().title,
                        eps.sortedWith(compareBy({ season(it.parentTitle) }, { it.parentTitle.orEmpty() }, { it.index ?: 0 }, { it.title })))
                }.sortedBy { it.title.lowercase() },
                albums = tracks.filter { it.parentId != null }.groupBy { it.parentId!! }.map { (id, list) ->
                    Album(id, list.first().parentTitle ?: list.first().title, list.first().grandparentTitle ?: list.first().artistCredit, inPlayOrder(list))
                }.sortedWith(compareBy({ it.artist.orEmpty().lowercase() }, { it.title.lowercase() })),
                songs = tracks.sortedWith(compareBy({ (it.artistCredit ?: it.grandparentTitle).orEmpty().lowercase() },
                    { it.parentTitle.orEmpty().lowercase() }, { it.disc ?: 0 }, { it.index ?: 0 }, { it.title.lowercase() })),
            )
        }

        private fun inPlayOrder(tracks: List<ItemSummary>) = tracks.sortedWith(compareBy({ it.disc ?: 0 }, { it.index ?: 0 }, { it.title.lowercase() }))

        /** A season's number from its title ("Season 10" after "Season 2"); specials and the like go last. */
        internal fun season(title: String?): Int = title?.let { Regex("\\d+").find(it)?.value?.toIntOrNull() } ?: Int.MAX_VALUE
    }
}
