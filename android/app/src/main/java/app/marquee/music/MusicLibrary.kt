package app.marquee.music

import android.net.Uri
import android.os.Bundle
import androidx.annotation.OptIn
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.LibraryResult
import androidx.media3.session.MediaLibraryService.LibraryParams
import androidx.media3.session.MediaLibraryService.MediaLibrarySession
import androidx.media3.session.MediaSession
import androidx.media3.session.SessionError
import app.marquee.api.apis.ItemsApi
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.LibraryType
import app.marquee.api.models.PlaylistKind
import app.marquee.api.models.RadioRequest
import app.marquee.api.models.Station
import app.marquee.core.Marquee
import com.google.common.collect.ImmutableList
import com.google.common.util.concurrent.ListenableFuture
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.guava.future
import java.util.concurrent.ConcurrentHashMap

/**
 * The music browse tree for Android Auto, Assistant and other media browsers (MUSIC-14):
 * For You (mixes and stations), Playlists, Artists and Recently Added albums. Choosing
 * anything plays it in full: a track inside an album or playlist plays the whole list from
 * there, and a station keeps going like it does in the app.
 */
@OptIn(UnstableApi::class)
class MusicLibrary(private val marquee: Marquee, private val music: () -> MusicController) : MediaLibrarySession.Callback {
    private val mixes = ConcurrentHashMap<String, Station>()

    private fun <T> io(block: suspend () -> T): ListenableFuture<T> = marquee.scope.future(Dispatchers.IO) { block() }

    private fun folder(id: String, title: String, playable: Boolean = false, art: Long? = null, type: Int = MediaMetadata.MEDIA_TYPE_FOLDER_MIXED) =
        MediaItem.Builder().setMediaId(id).setMediaMetadata(
            MediaMetadata.Builder().setTitle(title).setIsBrowsable(true).setIsPlayable(playable).setMediaType(type)
                .setArtworkUri(marquee.imageUrl(art, 300)?.let(Uri::parse)).build(),
        ).build()

    private fun playable(id: String, title: String, subtitle: String? = null, art: Long? = null, type: Int = MediaMetadata.MEDIA_TYPE_RADIO_STATION) =
        MediaItem.Builder().setMediaId(id).setMediaMetadata(
            MediaMetadata.Builder().setTitle(title).setSubtitle(subtitle).setIsBrowsable(false).setIsPlayable(true).setMediaType(type)
                .setArtworkUri(marquee.imageUrl(art, 300)?.let(Uri::parse)).build(),
        ).build()

    private fun inList(listId: String, t: ItemSummary): MediaItem =
        trackItem(marquee, t).buildUpon().setMediaId("$listId/${t.id}").build()

    private fun musicLibraries() = marquee.libraries.listLibraries().filter { it.type == LibraryType.MUSIC }

    override fun onGetLibraryRoot(session: MediaLibrarySession, browser: MediaSession.ControllerInfo, params: LibraryParams?): ListenableFuture<LibraryResult<MediaItem>> {
        // Auto shows the root's children as tabs.
        val extras = Bundle().apply { putBoolean("android.media.browse.SEARCH_SUPPORTED", true) }
        return com.google.common.util.concurrent.Futures.immediateFuture(
            LibraryResult.ofItem(folder(ROOT, "Marquee"), LibraryParams.Builder().setExtras(extras).build()),
        )
    }

    override fun onGetChildren(
        session: MediaLibrarySession, browser: MediaSession.ControllerInfo, parentId: String, page: Int, pageSize: Int, params: LibraryParams?,
    ): ListenableFuture<LibraryResult<ImmutableList<MediaItem>>> = io {
        if (marquee.token == null || marquee.baseUrl == null) return@io LibraryResult.ofError(SessionError.ERROR_SESSION_AUTHENTICATION_EXPIRED)
        runCatching { children(parentId) }.fold(
            { LibraryResult.ofItemList(ImmutableList.copyOf(it), params) },
            { LibraryResult.ofError(SessionError.ERROR_IO) },
        )
    }

    private fun children(parentId: String): List<MediaItem> {
        val parts = parentId.split(":")
        return when (parts[0]) {
            ROOT -> listOf(folder(FOR_YOU, "For You"), folder(PLAYLISTS, "Playlists"), folder(ARTISTS, "Artists"), folder(ALBUMS, "Recently Added"))
            FOR_YOU -> {
                val lib = musicLibraries().firstOrNull()?.id
                val ms = runCatching { marquee.music.musicMixes(lib) }.getOrDefault(emptyList())
                ms.forEachIndexed { i, m -> mixes["mix:$i"] = m }
                ms.mapIndexed { i, m -> playable("mix:$i", m.title, m.description, m.items.firstOrNull()?.images?.poster, MediaMetadata.MEDIA_TYPE_PLAYLIST) } +
                    listOf(playable("radio:library", "Library Radio"), playable("radio:favourites", "Favourites Radio")) +
                    musicMoods.map { playable("radio:mood:${it.lowercase()}", "$it Radio") }
            }
            PLAYLISTS -> marquee.playlists.listPlaylists(PlaylistKind.AUDIO).map {
                folder("playlist:${it.id}", it.title, playable = true, art = it.imageIds.firstOrNull(), type = MediaMetadata.MEDIA_TYPE_PLAYLIST)
            }
            ARTISTS -> musicLibraries().flatMap { lib -> marquee.items.listLibraryItems(lib.id, ItemType.ARTIST, ItemsApi.SortListLibraryItems.TITLE, limit = 500).items }
                .map { folder("artist:${it.id}", it.title, playable = true, art = it.images?.poster, type = MediaMetadata.MEDIA_TYPE_ARTIST) }
            ALBUMS -> musicLibraries().flatMap { lib -> marquee.items.listLibraryItems(lib.id, ItemType.ALBUM, ItemsApi.SortListLibraryItems._ADDED, limit = 100).items }
                .map { album(it) }
            "artist" -> marquee.items.listItemChildren(parts[1].toLong(), limit = 200).items.map { album(it) }
            "album" -> tracksOf(parentId).map { inList(parentId, it) }
            "playlist" -> tracksOf(parentId).map { inList(parentId, it) }
            else -> emptyList()
        }
    }

    private fun album(a: ItemSummary) = MediaItem.Builder().setMediaId("album:${a.id}").setMediaMetadata(
        MediaMetadata.Builder().setTitle(a.title).setArtist(a.artistCredit ?: a.parentTitle).setIsBrowsable(true).setIsPlayable(true)
            .setMediaType(MediaMetadata.MEDIA_TYPE_ALBUM).setArtworkUri(marquee.imageUrl(a.images?.poster, 300)?.let(Uri::parse)).build(),
    ).build()

    /** The tracks of an album, artist or playlist id. */
    private fun tracksOf(listId: String, shuffle: Boolean = false): List<ItemSummary> {
        val (kind, id) = listId.split(":").let { it[0] to it[1].toLong() }
        return when (kind) {
            "playlist" -> marquee.playlists.listPlaylistItems(id).items.map { it.item }
            else -> marquee.items.itemLeaves(id, shuffle = shuffle)
        }
    }

    // Playing

    override fun onSetMediaItems(
        mediaSession: MediaSession, controller: MediaSession.ControllerInfo, mediaItems: MutableList<MediaItem>, startIndex: Int, startPositionMs: Long,
    ): ListenableFuture<MediaSession.MediaItemsWithStartPosition> {
        val only = mediaItems.singleOrNull()
        val query = only?.requestMetadata?.searchQuery
        if (only == null || (only.mediaId.toLongOrNull() != null && query == null)) {
            // The app's own queue: rebuild URIs only.
            return com.google.common.util.concurrent.Futures.immediateFuture(
                MediaSession.MediaItemsWithStartPosition(mediaItems.map(::withUri), startIndex, startPositionMs),
            )
        }
        return io {
            val (items, start) = if (query != null) search(query) else expand(only.mediaId)
            MediaSession.MediaItemsWithStartPosition(items, start, 0)
        }
    }

    override fun onAddMediaItems(mediaSession: MediaSession, controller: MediaSession.ControllerInfo, mediaItems: MutableList<MediaItem>): ListenableFuture<MutableList<MediaItem>> =
        io { mediaItems.flatMap { if (it.mediaId.toLongOrNull() != null) listOf(withUri(it)) else expand(it.mediaId).first }.toMutableList() }

    private fun withUri(item: MediaItem) = item.buildUpon().setUri("marquee://track/${item.mediaId}").build()

    /** Turns a browse id into a queue and the index to start at. */
    private fun expand(mediaId: String): Pair<List<MediaItem>, Int> {
        val (listId, trackId) = mediaId.split("/").let { it[0] to it.getOrNull(1)?.toLongOrNull() }
        val parts = listId.split(":")
        val ui = music()
        fun queue(tracks: List<ItemSummary>, title: String?, radio: RadioRequest? = null): Pair<List<MediaItem>, Int> {
            ui.adopt(title, radio)
            return tracks.map { trackItem(marquee, it) } to (tracks.indexOfFirst { it.id == trackId }.takeIf { it >= 0 } ?: 0)
        }
        return when (parts[0]) {
            "mix" -> {
                val m = mixes[listId] ?: marquee.music.musicMixes(musicLibraries().firstOrNull()?.id).getOrNull(parts[1].toInt())
                    ?: throw IllegalStateException("mix gone")
                queue(m.items, m.title)
            }
            "radio" -> {
                val lib = musicLibraries().firstOrNull()?.id
                val req = when (parts[1]) {
                    "favourites" -> RadioRequest(RadioRequest.Seed.FAVOURITES, libraryId = lib, limit = 50)
                    "mood" -> RadioRequest(RadioRequest.Seed.MOOD, value = parts[2], libraryId = lib, limit = 50)
                    "item" -> RadioRequest(RadioRequest.Seed.ITEM, itemId = parts[2].toLong(), limit = 50)
                    else -> RadioRequest(RadioRequest.Seed.LIBRARY, libraryId = lib, limit = 50)
                }
                val st = marquee.music.musicRadio(req)
                queue(st.items, st.title, req)
            }
            "playlist" -> queue(tracksOf(listId), runCatching { marquee.playlists.getPlaylist(parts[1].toLong()).title }.getOrNull())
            "album" -> queue(tracksOf(listId), runCatching { marquee.items.getItem(parts[1].toLong()).title }.getOrNull())
            "artist" -> queue(tracksOf(listId, shuffle = true), runCatching { marquee.items.getItem(parts[1].toLong()).title }.getOrNull())
            else -> emptyList<MediaItem>() to 0
        }
    }

    /** "Play … on Marquee": the best match, played in full (an artist shuffles, a track starts its radio). */
    private fun search(query: String): Pair<List<MediaItem>, Int> {
        if (query.isBlank()) return expand("radio:library")
        val groups = marquee.search.search(query, 5).groups
        fun first(t: ItemType) = groups.firstOrNull { it.type == t }?.items?.firstOrNull()
        first(ItemType.ARTIST)?.let { return expand("artist:${it.id}") }
        first(ItemType.ALBUM)?.let { return expand("album:${it.id}") }
        first(ItemType.TRACK)?.let { return expand("radio:item:${it.id}") }
        marquee.playlists.listPlaylists(PlaylistKind.AUDIO).firstOrNull { it.title.contains(query, ignoreCase = true) }?.let { return expand("playlist:${it.id}") }
        return expand("radio:library")
    }

    override fun onSearch(session: MediaLibrarySession, browser: MediaSession.ControllerInfo, query: String, params: LibraryParams?): ListenableFuture<LibraryResult<Void>> =
        io {
            val n = runCatching { marquee.search.search(query, 10).groups.sumOf { it.items.size } }.getOrDefault(0)
            session.notifySearchResultChanged(browser, query, n, params)
            LibraryResult.ofVoid()
        }

    override fun onGetSearchResult(
        session: MediaLibrarySession, browser: MediaSession.ControllerInfo, query: String, page: Int, pageSize: Int, params: LibraryParams?,
    ): ListenableFuture<LibraryResult<ImmutableList<MediaItem>>> = io {
        val items = marquee.search.search(query, 10).groups.flatMap { g ->
            g.items.mapNotNull {
                when (g.type) {
                    ItemType.ARTIST -> folder("artist:${it.id}", it.title, playable = true, art = it.images?.poster, type = MediaMetadata.MEDIA_TYPE_ARTIST)
                    ItemType.ALBUM -> album(it)
                    ItemType.TRACK -> trackItem(marquee, it).buildUpon().setMediaId("radio:item:${it.id}").build()
                    else -> null
                }
            }
        }
        LibraryResult.ofItemList(ImmutableList.copyOf(items), params)
    }

    companion object {
        const val ROOT = "root"
        const val FOR_YOU = "foryou"
        const val PLAYLISTS = "playlists"
        const val ARTISTS = "artists"
        const val ALBUMS = "albums"
    }
}
