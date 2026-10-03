package app.marquee.music

import android.content.ComponentName
import android.content.Context
import android.net.Uri
import androidx.media3.common.MediaItem
import androidx.media3.common.C
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.MusicDJRequest
import app.marquee.api.models.RadioRequest
import app.marquee.api.models.RateItemRequest
import app.marquee.api.models.Station
import app.marquee.core.Marquee
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.guava.await
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * The app's handle on background music playback, as Compose-friendly state, plus the
 * music extras (M6.5): stations that keep going, the DJ, a sleep timer and ratings.
 */
class MusicController(private val context: Context, private val marquee: Marquee) {
    /** A queued track; dj names the DJ that wove it in. */
    data class Now(val id: Long, val title: String, val artist: String, val album: String, val artwork: Uri?, val dj: String? = null, val albumId: Long? = null,
        val audio: app.marquee.api.models.AudioFormat? = null)

    /** DJ modes (MUSIC-6): a pick every few of your own tracks. */
    enum class DJ(val label: String, val mode: MusicDJRequest.Mode, val blurb: String) {
        Wander("DJ: Wander", MusicDJRequest.Mode.WANDER, "Similar sound, other artists"),
        Superfan("DJ: Superfan", MusicDJRequest.Mode.SUPERFAN, "More from the same artists"),
        DeepCuts("DJ: Deep Cuts", MusicDJRequest.Mode.DEEP_CUTS, "Their tracks you play least"),
        SameEra("DJ: Same Era", MusicDJRequest.Mode.SAME_ERA, "Similar sound from the same years");

        companion object {
            /** A saved choice, including the modes' earlier names. */
            fun saved(name: String?): DJ? = when (name) {
                null -> null
                "Stretch" -> Wander
                "Groupie" -> Superfan
                "Contempo" -> SameEra
                else -> entries.firstOrNull { it.name == name }
            }
        }
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
    private val _dj = MutableStateFlow(DJ.saved(prefs.getString("dj", null)))
    val dj: StateFlow<DJ?> = _dj

    /** Set while a station plays: it asks for more before the queue runs out. */
    private var radio: RadioRequest? = null
    private var refilling = false
    private var djCount = 0
    private var djBusy = false
    private var lastId: Long? = null
    private var ratingJob: Job? = null

    /** The controller being built: every caller waits for the same one (main thread only). */
    private var connecting: Deferred<MediaController>? = null

    private suspend fun connect(): MediaController {
        controller?.let { return it }
        val pending = connecting ?: scope.async { build() }.also { connecting = it }
        return try { pending.await() } finally { if (connecting === pending && pending.isCompleted) connecting = null }
    }

    private suspend fun build(): MediaController {
        val c = MediaController.Builder(context, SessionToken(context, ComponentName(context, MusicService::class.java))).buildAsync().await()
        c.addListener(object : Player.Listener {
            override fun onEvents(player: Player, events: Player.Events) = sync(player, events.contains(Player.EVENT_TIMELINE_CHANGED))
            override fun onMediaItemTransition(item: MediaItem?, reason: Int) {
                if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO && _sleep.value == Sleep.EndOfTrack) {
                    c.pause()
                    _sleep.value = null
                }
            }
        })
        controller = c
        watchCast()
        sync(c, queueChanged = true)
        return c
    }

    private var ticker: Job? = null

    /** Moves the position along and checks the sleep timer, only while something plays. */
    private fun tick() {
        if (ticker?.isActive == true || !(_playing.value || casting)) return
        ticker = scope.launch {
            while (_playing.value || casting) {
                if (casting) _position.value = marquee.cast.position.value
                else controller?.let { _position.value = it.currentPosition to it.duration.coerceAtLeast(0) }
                (_sleep.value as? Sleep.At)?.let {
                    if (System.currentTimeMillis() >= it.epochMs) {
                        if (casting) marquee.cast.pause() else controller?.pause()
                        _sleep.value = null
                    }
                }
                delay(500)
            }
        }
    }

    private fun meta(item: MediaItem): Now {
        val m = item.mediaMetadata
        return Now(item.mediaId.toLongOrNull() ?: 0, m.title?.toString() ?: "", m.artist?.toString() ?: "", m.albumTitle?.toString() ?: "", m.artworkUri,
            m.extras?.getString("dj"), m.extras?.getLong("album", 0L)?.takeIf { it > 0 }, AudioQuality.from(m.extras))
    }

    private fun sync(p: Player, queueChanged: Boolean) {
        val cur = p.currentMediaItem?.let(::meta)
        _now.value = cur
        if (!casting) {
            _playing.value = p.isPlaying
            _position.value = p.currentPosition to p.duration.coerceAtLeast(0)
        }
        tick()
        _index.value = p.currentMediaItemIndex
        // The queue list is only rebuilt when the queue itself changed.
        if (queueChanged) _queue.value = (0 until p.mediaItemCount).map { meta(p.getMediaItemAt(it)) }
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
        djPick(p, cur)
    }

    private fun loadRating(id: Long) {
        _rating.value = null
        ratingJob?.cancel()
        ratingJob = scope.launch {
            val r = withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(id).userRating }.getOrNull() }
            if (lastId == id) _rating.value = r
        }
    }

    private fun mediaItem(t: ItemSummary, dj: String? = null): MediaItem = trackItem(marquee, t, dj)

    /** Plays tracks from start (at startMs into it); source names what's playing (an album or playlist). */
    fun play(tracks: List<ItemSummary>, start: Int, source: String? = null, startMs: Long = 0) = start(tracks, start, source, null, startMs)

    /** Plays a generated station; with a radio request it keeps topping itself up. */
    fun playStation(station: Station, radio: RadioRequest? = null) = start(station.items, 0, station.title, radio)

    private fun start(tracks: List<ItemSummary>, start: Int, source: String?, radio: RadioRequest?, startMs: Long = 0) {
        if (tracks.isEmpty()) return
        _source.value = source
        this.radio = radio
        djCount = 0
        scope.launch(Dispatchers.Main) {
            val c = connect()
            val index = start.coerceIn(0, tracks.size - 1)
            c.setMediaItems(tracks.map { mediaItem(it) }, index, startMs.coerceAtLeast(0))
            // While casting (or with a device connected and idle) the new queue plays there.
            if (casting || beginCasting()) castTrack(index, startMs.coerceAtLeast(0))
            else { c.prepare(); c.play() }
        }
    }

    /** Plays the best match for a spoken or typed search (MusicLibrary resolves it). */
    fun playFromSearch(query: String) = with { c ->
        _source.value = null
        radio = null
        djCount = 0
        val item = MediaItem.Builder().setMediaId("search")
            .setRequestMetadata(MediaItem.RequestMetadata.Builder().setSearchQuery(query).build()).build()
        c.setMediaItems(listOf(item))
        c.prepare()
        c.play()
    }

    /** Starts a station from a seed (MUSIC-3). Throws when the server can't make one. */
    suspend fun startRadio(req: RadioRequest) {
        val r = req.copy(limit = 50, exclude = null)
        val st = withContext(Dispatchers.IO) { marquee.music.musicRadio(r) }
        if (st.items.isEmpty()) throw IllegalStateException("Nothing to play yet: Soundprint analysis may still be running.")
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

    /** After every third of your own tracks, asks the DJ for one to play next. */
    private fun djPick(p: Player, cur: Now) {
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

    /** Connects to the music service so stations and the DJ work even when playback starts elsewhere (Android Auto). */
    fun attach() { scope.launch { connect() } }

    /** Takes over a queue started outside the app: its name and, for a station, how to continue it. */
    fun adopt(source: String?, radio: RadioRequest?) = scope.launch {
        _source.value = source
        this@MusicController.radio = radio
        djCount = 0
    }

    private val _levelling = MutableStateFlow(levellingPref())
    /** Volume levelling (MUSIC-10); MusicService applies it. */
    val levelling: StateFlow<Levelling> = _levelling
    private fun levellingPref() = prefs.getString("levelling", null)?.let { n -> Levelling.entries.firstOrNull { it.name == n } } ?: Levelling.Auto
    private val _showQuality = MutableStateFlow(prefs.getBoolean("showQuality", false))
    /** Show audio quality (MUSIC-23), per device, off by default. */
    val showQuality: StateFlow<Boolean> = _showQuality
    fun setShowQuality(on: Boolean) {
        _showQuality.value = on
        prefs.edit().putBoolean("showQuality", on).apply()
    }
    private val _crossfade = MutableStateFlow(prefs.getInt("crossfade", 0))
    /** Crossfade length in seconds; 0 = off (MUSIC-9). MusicService applies it. */
    val crossfade: StateFlow<Int> = _crossfade
    fun setCrossfade(seconds: Int) {
        _crossfade.value = seconds
        prefs.edit().putInt("crossfade", seconds).apply()
    }

    private val _eq = MutableStateFlow(EqSettings.load(prefs))
    /** The equaliser, kept per device; MusicService applies it. */
    val eq: StateFlow<EqSettings> = _eq
    fun setEq(s: EqSettings) {
        _eq.value = s
        s.save(prefs)
    }

    fun setLevelling(l: Levelling) {
        _levelling.value = l
        prefs.edit().putString("levelling", l.name).apply()
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
        if (c.mediaItemCount == 0) {
            c.setMediaItems(items)
            if (casting || beginCasting()) castTrack(0, 0) else { c.prepare(); c.play() }
        }
        else if (next) c.addMediaItems(c.currentMediaItemIndex + 1, items) else c.addMediaItems(items)
    }

    private fun with(block: (MediaController) -> Unit) { scope.launch { block(connect()) } }
    /** Remote control (USER-14): explicit pause and resume, and the player's own volume (0–1). */
    fun pause() = with { if (casting) marquee.cast.pause() else it.pause() }
    fun resume() = with { if (casting) { if (!marquee.cast.playing.value) marquee.cast.toggle() } else it.play() }
    fun setVolume(v: Float) = with { it.volume = v.coerceIn(0f, 1f) }
    /** The player's volume, once connected. */
    val volume: Float? get() = controller?.volume
    fun toggle() = with { if (casting) marquee.cast.toggle() else if (it.isPlaying) it.pause() else it.play() }
    fun next() = with { if (casting) castNext() else it.seekToNextMediaItem() }
    fun previous() = with {
        when {
            casting && marquee.cast.position.value.first > 3000 -> marquee.cast.seek(0)
            casting -> it.previousMediaItemIndex.takeIf { i -> i != C.INDEX_UNSET }?.let { i -> it.seekTo(i, 0); castTrack(i, 0) }
            it.currentPosition > 3000 -> it.seekTo(0)
            else -> it.seekToPreviousMediaItem()
        }
    }
    fun seek(ms: Long) = with { if (casting) marquee.cast.seek(ms) else it.seekTo(ms) }
    fun jump(index: Int) = with { it.seekTo(index, 0); if (casting) castTrack(index, 0) }

    // Chromecast (D82): when a Cast device connects while music is playing, the queue plays
    // there track by track; the phone keeps the queue (paused) and carries on when casting stops.
    private var casting = false
    private var castWatch: Job? = null

    private fun watchCast() {
        if (castWatch != null || marquee.isTv || !marquee.cast.available) return
        castWatch = scope.launch {
            launch { marquee.cast.playing.collect { if (casting) _playing.value = it } }
            marquee.cast.device.collect { device ->
                val c = controller ?: return@collect
                when {
                    // A video being cast owns the device; otherwise music goes there if any is queued.
                    device != null && !casting && !marquee.cast.videoActive && c.mediaItemCount > 0 && (c.isPlaying || _now.value != null) -> {
                        val at = c.currentPosition
                        beginCasting()
                        castTrack(c.currentMediaItemIndex, at)
                    }
                    device == null && casting -> {
                        casting = false
                        marquee.cast.onFinished = null
                        c.seekTo(c.currentMediaItemIndex, marquee.cast.position.value.first)
                        // Carries on here only if it was playing there.
                        c.prepare()
                        if (marquee.cast.playingAtEnd) c.play()
                        _playing.value = c.isPlaying
                    }
                }
            }
        }
    }

    /**
     * Hands music to the connected Cast device, unless a video owns it: the phone pauses and
     * keeps the queue. False when there's nothing to cast to.
     */
    private fun beginCasting(): Boolean {
        val c = controller ?: return false
        if (castWatch == null || marquee.cast.device.value == null || marquee.cast.videoActive) return false
        casting = true
        c.pause()
        marquee.cast.onFinished = {
            // The sleep timer's "end of track" stops the receiver here instead of moving on.
            if (_sleep.value == Sleep.EndOfTrack) _sleep.value = null else castNext()
        }
        tick()
        return true
    }

    private fun castTrack(index: Int, at: Long) {
        val c = controller ?: return
        if (index !in 0 until c.mediaItemCount) return
        val t = meta(c.getMediaItemAt(index))
        scope.launch {
            val error = marquee.cast.load(t.id, at, t.title, t.artist.ifBlank { null }, t.artwork?.toString(), music = true)
            if (error != null) android.widget.Toast.makeText(context, "Couldn't play on the Cast device: $error", android.widget.Toast.LENGTH_LONG).show()
        }
    }

    private fun castNext() {
        val c = controller ?: return
        val n = c.nextMediaItemIndex
        if (n == C.INDEX_UNSET) { marquee.cast.pause(); return }
        c.seekTo(n, 0)
        castTrack(n, 0)
    }
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
