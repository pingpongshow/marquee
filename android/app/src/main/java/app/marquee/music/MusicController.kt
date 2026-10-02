package app.marquee.music

import android.content.ComponentName
import android.content.Context
import android.net.Uri
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import app.marquee.api.models.ItemSummary
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.guava.await
import kotlinx.coroutines.launch

/** The app's handle on background music playback, as Compose-friendly state. */
class MusicController(private val context: Context, private val marquee: Marquee) {
    data class Now(val title: String, val artist: String, val album: String, val artwork: Uri?)

    private val scope = MainScope()
    private var controller: MediaController? = null

    private val _now = MutableStateFlow<Now?>(null)
    val now: StateFlow<Now?> = _now
    private val _playing = MutableStateFlow(false)
    val playing: StateFlow<Boolean> = _playing
    private val _position = MutableStateFlow(0L to 0L) // position, duration (ms)
    val position: StateFlow<Pair<Long, Long>> = _position
    private val _queue = MutableStateFlow<List<Now>>(emptyList())
    val queue: StateFlow<List<Now>> = _queue
    private val _index = MutableStateFlow(0)
    val index: StateFlow<Int> = _index
    private val _source = MutableStateFlow<String?>(null)
    val source: StateFlow<String?> = _source
    private val _shuffle = MutableStateFlow(false)
    val shuffle: StateFlow<Boolean> = _shuffle
    private val _repeat = MutableStateFlow(Player.REPEAT_MODE_OFF)
    val repeat: StateFlow<Int> = _repeat

    private suspend fun connect(): MediaController {
        controller?.let { return it }
        val c = MediaController.Builder(context, SessionToken(context, ComponentName(context, MusicService::class.java))).buildAsync().await()
        c.addListener(object : Player.Listener {
            override fun onEvents(player: Player, events: Player.Events) = sync(player)
        })
        controller = c
        scope.launch {
            while (true) {
                controller?.let { _position.value = it.currentPosition to it.duration.coerceAtLeast(0) }
                delay(500)
            }
        }
        sync(c)
        return c
    }

    private fun meta(m: MediaMetadata) = Now(m.title?.toString() ?: "", m.artist?.toString() ?: "", m.albumTitle?.toString() ?: "", m.artworkUri)

    private fun sync(p: Player) {
        _now.value = p.currentMediaItem?.let { meta(it.mediaMetadata) }
        _playing.value = p.isPlaying
        _index.value = p.currentMediaItemIndex
        _queue.value = (0 until p.mediaItemCount).map { meta(p.getMediaItemAt(it).mediaMetadata) }
        _shuffle.value = p.shuffleModeEnabled
        _repeat.value = p.repeatMode
    }

    private fun mediaItem(t: ItemSummary): MediaItem = MediaItem.Builder()
        .setMediaId(t.id.toString())
        .setUri("marquee://track/${t.id}")
        .setMediaMetadata(
            MediaMetadata.Builder()
                .setTitle(t.title)
                .setArtist(t.artistCredit ?: t.grandparentTitle)
                .setAlbumTitle(t.parentTitle)
                .setArtworkUri(marquee.imageUrl(t.images?.poster, 512)?.let(Uri::parse))
                .build(),
        )
        .build()

    /** Plays tracks from start; source names what's playing (an album or playlist). */
    fun play(tracks: List<ItemSummary>, start: Int, source: String? = null) {
        if (tracks.isEmpty()) return
        _source.value = source
        scope.launch(Dispatchers.Main) {
            val c = connect()
            c.setMediaItems(tracks.map(::mediaItem), start.coerceIn(0, tracks.size - 1), 0)
            c.prepare()
            c.play()
        }
    }

    private fun with(block: (MediaController) -> Unit) { scope.launch { block(connect()) } }
    fun toggle() = with { if (it.isPlaying) it.pause() else it.play() }
    fun next() = with { it.seekToNextMediaItem() }
    fun previous() = with { if (it.currentPosition > 3000) it.seekTo(0) else it.seekToPreviousMediaItem() }
    fun seek(ms: Long) = with { it.seekTo(ms) }
    fun jump(index: Int) = with { it.seekTo(index, 0) }
    fun toggleShuffle() = with { it.shuffleModeEnabled = !it.shuffleModeEnabled }
    fun cycleRepeat() = with {
        it.repeatMode = when (it.repeatMode) {
            Player.REPEAT_MODE_OFF -> Player.REPEAT_MODE_ALL
            Player.REPEAT_MODE_ALL -> Player.REPEAT_MODE_ONE
            else -> Player.REPEAT_MODE_OFF
        }
    }
    fun stop() = with { it.stop(); it.clearMediaItems() }
}
