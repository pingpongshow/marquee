package app.marquee.music

import android.content.ComponentName
import android.content.Context
import android.net.Uri
import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.MusicDJRequest
import app.marquee.api.models.RadioRequest
import app.marquee.api.models.RateItemRequest
import app.marquee.api.models.Station
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.guava.await
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * The app's handle on background music playback, as Compose-friendly state, plus what
 * Plexamp layers on top (M6.5): stations that keep going, the Guest DJ, a sleep timer and
 * ratings.
 */
class MusicController(private val context: Context, private val marquee: Marquee) {
    /** A queued track; dj names the Guest DJ that wove it in. */
    data class Now(val id: Long, val title: String, val artist: String, val album: String, val artwork: Uri?, val dj: String? = null)

    /** Guest DJ modes (MUSIC-6): a pick every few of your own tracks. */
    enum class DJ(val label: String, val mode: MusicDJRequest.Mode, val blurb: String) {
        Stretch("DJ Stretch", MusicDJRequest.Mode.STRETCH, "Similar sound, other artists"),
        Groupie("DJ Groupie", MusicDJRequest.Mode.GROUPIE, "More from the same artists"),
        DeepCuts("DJ Deep Cuts", MusicDJRequest.Mode.DEEP_CUTS, "Their tracks you play least"),
        Contempo("DJ Contempo", MusicDJRequest.Mode.CONTEMPO, "Similar sound from the same years"),
    }

    /** The sleep timer: a time, or the end of the current track. */
    sealed interface Sleep {
        data class At(val epochMs: Long) : Sleep
        data object EndOfTrack : Sleep
    }

    private val scope = MainScope()
    private var controller: MediaController? = null
    private val prefs = context.getSharedPreferences("marquee.music", Context.MODE_PRIVATE)

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
    private val _rating = MutableStateFlow<Double?>(null)
    /** The current track's rating, 0–10 (10 = loved). */
    val rating: StateFlow<Double?> = _rating
    private val _sleep = MutableStateFlow<Sleep?>(null)
    val sleep: StateFlow<Sleep?> = _sleep
    private val _dj = MutableStateFlow(prefs.getString("dj", null)?.let { n -> DJ.entries.firstOrNull { it.name == n } })
    val dj: StateFlow<DJ?> = _dj

    /** Set while a station plays: it asks for more before the queue runs out. */
    private var radio: RadioRequest? = null
    private var refilling = false
    private var djCount = 0
    private var djBusy = false
    private var lastId: Long? = null
    private var ratingJob: Job? = null

    private suspend fun connect(): MediaController {
        controller?.let { return it }
        val c = MediaController.Builder(context, SessionToken(context, ComponentName(context, MusicService::class.java))).buildAsync().await()
        c.addListener(object : Player.Listener {
            override fun onEvents(player: Player, events: Player.Events) = sync(player)
            override fun onMediaItemTransition(item: MediaItem?, reason: Int) {
                if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO && _sleep.value == Sleep.EndOfTrack) {
                    c.pause()
                    _sleep.value = null
                }
            }
        })
        controller = c
        scope.launch {
            while (true) {
                controller?.let { _position.value = it.currentPosition to it.duration.coerceAtLeast(0) }
                (_sleep.value as? Sleep.At)?.let { if (System.currentTimeMillis() >= it.epochMs) { controller?.pause(); _sleep.value = null } }
                delay(500)
            }
        }
        sync(c)
        return c
    }

    private fun meta(item: MediaItem): Now {
        val m = item.mediaMetadata
        return Now(item.mediaId.toLongOrNull() ?: 0, m.title?.toString() ?: "", m.artist?.toString() ?: "", m.albumTitle?.toString() ?: "", m.artworkUri,
            m.extras?.getString("dj"))
    }

    private fun sync(p: Player) {
        val cur = p.currentMediaItem?.let(::meta)
        _now.value = cur
        _playing.value = p.isPlaying
        _index.value = p.currentMediaItemIndex
        _queue.value = (0 until p.mediaItemCount).map { meta(p.getMediaItemAt(it)) }
        _shuffle.value = p.shuffleModeEnabled
        _repeat.value = p.repeatMode
        if (cur?.id != lastId) {
            lastId = cur?.id
            if (cur != null) onNewTrack(p, cur)
        }
    }

    private fun onNewTrack(p: Player, cur: Now) {
        loadRating(cur.id)
        topUpRadio(p)
        guestDJ(p, cur)
    }

    private fun loadRating(id: Long) {
        _rating.value = null
        ratingJob?.cancel()
        ratingJob = scope.launch {
            val r = withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(id).userRating }.getOrNull() }
            if (lastId == id) _rating.value = r
        }
    }

    private fun mediaItem(t: ItemSummary, dj: String? = null): MediaItem = MediaItem.Builder()
        .setMediaId(t.id.toString())
        .setUri("marquee://track/${t.id}")
        .setMediaMetadata(
            MediaMetadata.Builder()
                .setTitle(t.title)
                .setArtist(t.artistCredit ?: t.grandparentTitle)
                .setAlbumTitle(t.parentTitle)
                .setArtworkUri(marquee.imageUrl(t.images?.poster, 512)?.let(Uri::parse))
                .setExtras(Bundle().apply { if (dj != null) putString("dj", dj) })
                .build(),
        )
        .build()

    /** Plays tracks from start; source names what's playing (an album or playlist). */
    fun play(tracks: List<ItemSummary>, start: Int, source: String? = null) = start(tracks, start, source, null)

    /** Plays a generated station; with a radio request it keeps topping itself up. */
    fun playStation(station: Station, radio: RadioRequest? = null) = start(station.items, 0, station.title, radio)

    private fun start(tracks: List<ItemSummary>, start: Int, source: String?, radio: RadioRequest?) {
        if (tracks.isEmpty()) return
        _source.value = source
        this.radio = radio
        djCount = 0
        scope.launch(Dispatchers.Main) {
            val c = connect()
            c.setMediaItems(tracks.map { mediaItem(it) }, start.coerceIn(0, tracks.size - 1), 0)
            c.prepare()
            c.play()
        }
    }

    /** Starts a station from a seed (MUSIC-3). Throws when the server can't make one. */
    suspend fun startRadio(req: RadioRequest) {
        val r = req.copy(limit = 50, exclude = null)
        val st = withContext(Dispatchers.IO) { marquee.music.musicRadio(r) }
        if (st.items.isEmpty()) throw IllegalStateException("Nothing to play yet: sonic analysis may still be running.")
        playStation(st, r)
    }

    /** Continues a station when fewer than five tracks are left. */
    private fun topUpRadio(p: Player) {
        val r = radio ?: return
        if (refilling || p.mediaItemCount - p.currentMediaItemIndex > 5) return
        refilling = true
        val exclude = _queue.value.map { it.id }
        val title = _source.value
        scope.launch {
            val st = withContext(Dispatchers.IO) { runCatching { marquee.music.musicRadio(r.copy(exclude = exclude, limit = 25)) }.getOrNull() }
            refilling = false
            if (st == null || _source.value != title) return@launch
            val fresh = st.items.filter { it.id !in exclude }
            if (fresh.isNotEmpty()) controller?.addMediaItems(fresh.map { mediaItem(it) })
        }
    }

    /** After every third of your own tracks, asks the Guest DJ for one to play next. */
    private fun guestDJ(p: Player, cur: Now) {
        val dj = _dj.value ?: return
        if (djBusy) return
        if (cur.dj != null) { djCount = 0; return }
        djCount++
        val next = p.currentMediaItemIndex + 1
        val nextIsDJ = next < p.mediaItemCount && meta(p.getMediaItemAt(next)).dj != null
        if (djCount < 3 || nextIsDJ) return
        djBusy = true
        val exclude = _queue.value.map { it.id }
        scope.launch {
            val pick = withContext(Dispatchers.IO) { runCatching { marquee.music.musicDJ(MusicDJRequest(cur.id, dj.mode, exclude)) }.getOrNull() }
            djBusy = false
            val c = controller ?: return@launch
            if (pick == null || lastId != cur.id) return@launch
            c.addMediaItem(c.currentMediaItemIndex + 1, mediaItem(pick, dj.label))
            djCount = 0
        }
    }

    fun setDJ(dj: DJ?) {
        _dj.value = dj
        djCount = 0
        prefs.edit().putString("dj", dj?.name).apply()
    }

    fun setSleep(s: Sleep?) { _sleep.value = s }

    /** Rates the current track 0–10, or clears it with null (MUSIC-11). */
    fun rate(rating: Double?) {
        val id = lastId ?: return
        val before = _rating.value
        _rating.value = rating
        scope.launch {
            val ok = withContext(Dispatchers.IO) { runCatching { marquee.items.rateItem(id, RateItemRequest(rating)) }.isSuccess }
            if (!ok && lastId == id) _rating.value = before
        }
    }

    /** Adds tracks after the current one (Play Next) or at the end. */
    fun enqueue(tracks: List<ItemSummary>, next: Boolean) = with { c ->
        val items = tracks.map { mediaItem(it) }
        if (c.mediaItemCount == 0) { c.setMediaItems(items); c.prepare(); c.play() }
        else if (next) c.addMediaItems(c.currentMediaItemIndex + 1, items) else c.addMediaItems(items)
    }

    private fun with(block: (MediaController) -> Unit) { scope.launch { block(connect()) } }
    fun toggle() = with { if (it.isPlaying) it.pause() else it.play() }
    fun next() = with { it.seekToNextMediaItem() }
    fun previous() = with { if (it.currentPosition > 3000) it.seekTo(0) else it.seekToPreviousMediaItem() }
    fun seek(ms: Long) = with { it.seekTo(ms) }
    fun jump(index: Int) = with { it.seekTo(index, 0) }
    fun remove(index: Int) = with { if (index != it.currentMediaItemIndex) it.removeMediaItem(index) }
    fun toggleShuffle() = with { it.shuffleModeEnabled = !it.shuffleModeEnabled }
    fun cycleRepeat() = with {
        it.repeatMode = when (it.repeatMode) {
            Player.REPEAT_MODE_OFF -> Player.REPEAT_MODE_ALL
            Player.REPEAT_MODE_ALL -> Player.REPEAT_MODE_ONE
            else -> Player.REPEAT_MODE_OFF
        }
    }
    fun stop() = with {
        radio = null
        _sleep.value = null
        it.stop()
        it.clearMediaItems()
    }
}
