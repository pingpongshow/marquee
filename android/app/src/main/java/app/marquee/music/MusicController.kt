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
    /**
     * A queued track; dj names the DJ that wove it in. context is what it was started from, for
     * the playlist statistics (MUSIC-17); radio marks a track the playing radio added itself.
     */
    data class Now(val id: Long, val title: String, val artist: String, val album: String, val artwork: Uri?, val dj: String? = null, val albumId: Long? = null,
        val audio: app.marquee.api.models.AudioFormat? = null, val favorite: Boolean? = null, val context: String? = null, val radio: Boolean = false,
        val disliked: Boolean? = null)

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
    private val _favorite = MutableStateFlow<Boolean?>(null)
    /** Whether the current track is a favourite on the server (the heart shows this device's changes over it). */
    val favorite: StateFlow<Boolean?> = _favorite
    private val _disliked = MutableStateFlow<Boolean?>(null)
    /** Whether the current track has a thumbs down on the server (the views show this device's changes over it). */
    val disliked: StateFlow<Boolean?> = _disliked
    private val _sleep = MutableStateFlow<Sleep?>(null)
    val sleep: StateFlow<Sleep?> = _sleep
    private val _dj = MutableStateFlow(DJ.saved(prefs.getString("dj", null)))
    val dj: StateFlow<DJ?> = _dj

    /** Set while a station plays: it asks for more before the queue runs out. */
    private var radio: RadioRequest? = null
    private var refilling = false
    /** The playing station's context, which its top-ups keep (MUSIC-17). */
    private var stationContext: String? = null
    /** The queue is a radio or a generated station (a mix, Muse, a journey), not something the listener chose song by song. */
    private var generated = false
    /** What the listener skipped and played through on the playing radio (MUSIC-17). */
    private var steering = RadioSteering()
    /** Set while an early top-up (after quick skips) is on its way. */
    private var steeringNow = false
    // Bumped when a new queue starts and when an early top-up begins: a top-up that was
    // overtaken by either is dropped.
    private var queueGen = 0
    private var steerGen = 0
    /** The playing track, how far it got and how long it is, and whether it counted as played through. */
    private var playingNow: Now? = null
    private var heardMs = 0L
    private var heardDur = 0L
    private var heardThrough = false
    /** Why the player last moved to another track; and that the Cast receiver finished one. */
    private var transitionReason = -1
    private var castEnded = false
    private var djCount = 0
    private var djBusy = false
    private var lastId: Long? = null
    /** The entry playing: the same track again (a radio coming round) is a new entry. */
    private var lastEntry: String? = null
    private var ratingJob: Job? = null
    /** A top-up that failed with nothing left to play tries again by itself. */
    private var topUpRetry: Job? = null
    private var topUpWaitMs = TOP_UP_RETRY_MS

    init {
        // After changes made offline were sent: show the server's rating for the current track.
        scope.launch { marquee.sync.syncs.collect { n -> if (n > 0) lastId?.let { loadRating(it, reset = false) } } }
    }

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
                // Comes before onEvents, so sync knows how the last track left.
                transitionReason = reason
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
                heard(_position.value.first, _position.value.second)
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
        return Now(QueueIds.trackId(item.mediaId) ?: 0, m.title?.toString() ?: "", m.artist?.toString() ?: "", m.albumTitle?.toString() ?: "", m.artworkUri,
            m.extras?.getString("dj"), m.extras?.getLong("album", 0L)?.takeIf { it > 0 }, AudioQuality.from(m.extras),
            m.extras?.takeIf { it.containsKey("favorite") }?.getBoolean("favorite"), m.extras?.getString("context"), m.extras?.getBoolean("radio") == true,
            m.extras?.takeIf { it.containsKey("disliked") }?.getBoolean("disliked"))
    }

    private fun sync(p: Player, queueChanged: Boolean) {
        val cur = p.currentMediaItem?.let(::meta)
        val prevIndex = _index.value
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
        if (p.currentMediaItem?.mediaId != lastEntry) {
            lastEntry = p.currentMediaItem?.mediaId
            lastId = cur?.id
            left(p, prevIndex)
            playingNow = cur
            heardMs = 0
            heardDur = p.currentMediaItem?.mediaMetadata?.durationMs ?: 0
            heardThrough = false
            if (cur != null) onNewTrack(p, cur)
        }
        // While casting, the receiver's position comes in through tick.
        if (!casting) heard(p.currentPosition, p.duration.coerceAtLeast(0))
        // Played to the end of what's queued: a radio goes on when more comes.
        if (p.playbackState == Player.STATE_ENDED) {
            if (_sleep.value == Sleep.EndOfTrack) {
                // The sleep timer was waiting for this: no top-up may start the music again.
                _sleep.value = null
                radio = null
                topUpRetry?.cancel()
                queueGen++
            } else topUpRadio(p)
        }
    }

    /** Keeps how far the playing track got, and counts a radio's track as played through at 80% (MUSIC-17). */
    private fun heard(positionMs: Long, durationMs: Long) {
        val cur = playingNow ?: return
        // Just after a change of track the receiver may still report the last one: its length gives it away.
        if (casting && heardDur > 0 && durationMs > 0 && kotlin.math.abs(durationMs - heardDur) > 2000) return
        heardMs = positionMs
        if (durationMs > 0) heardDur = durationMs
        if (!heardThrough && cur.radio && radio != null && RadioSteering.playedThrough(heardMs, heardDur)) {
            heardThrough = true
            steering.finished(cur.id)
        }
    }

    /**
     * The radio's track that was playing has left the player: played through, or skipped
     * quickly. Two quick skips in a row top the station up early. Going back, or a new queue,
     * doesn't count.
     */
    private fun left(p: Player, prevIndex: Int) {
        val prev = playingNow
        val reason = transitionReason.also { transitionReason = -1 }
        val natural = reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO || castEnded
        castEnded = false
        if (prev == null || !prev.radio || radio == null) return
        if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_PLAYLIST_CHANGED || reason == Player.MEDIA_ITEM_TRANSITION_REASON_REPEAT) return
        val forward = natural || p.previousMediaItemIndex == prevIndex || (!p.shuffleModeEnabled && p.currentMediaItemIndex > prevIndex)
        if (!forward) return
        if (steering.left(prev.id, heardMs, heardDur, natural)) topUpEarly()
    }

    private fun onNewTrack(p: Player, cur: Now) {
        loadRating(cur.id)
        topUpRadio(p)
        djPick(p, cur)
    }

    private fun loadRating(id: Long, reset: Boolean = true) {
        if (reset) {
            val n = _now.value?.takeIf { it.id == id }
            _rating.value = null
            _favorite.value = n?.favorite
            _disliked.value = n?.disliked
        }
        ratingJob?.cancel()
        // A rating made offline and not yet sent wins over the server's older one (USER-18).
        marquee.sync.queued(id, app.marquee.core.PendingChange.Kind.Rating)?.let { _rating.value = it.rating }
        ratingJob = scope.launch {
            val item = withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(id) } }
            val r = item.map { it.userRating }
            val queued = marquee.sync.queued(id, app.marquee.core.PendingChange.Kind.Rating)
            if (lastId == id) {
                _rating.value = if (queued != null) queued.rating else r.getOrElse { _rating.value }
                item.getOrNull()?.let {
                    _favorite.value = it.favorite == true
                    _disliked.value = it.disliked == true
                }
            }
        }
    }

    private fun mediaItem(t: ItemSummary, dj: String? = null, context: String? = null, radio: Boolean = false): MediaItem {
        val item = trackItem(marquee, t, dj, context, radio)
        if (item.mediaMetadata.artworkUri != null) return item
        // Offline: the artwork saved with its download.
        val poster = (this.context.applicationContext as app.marquee.MarqueeApplication).downloads.posterFile(t.id) ?: return item
        return item.buildUpon().setMediaMetadata(item.mediaMetadata.buildUpon().setArtworkUri(Uri.fromFile(poster)).build()).build()
    }

    /** Entries for [tracks], each with a media id of its own among those [taken] (a track can come round again). */
    private fun mediaItems(tracks: List<ItemSummary>, taken: Collection<String> = emptyList(), dj: String? = null, context: String? = null, radio: Boolean = false): List<MediaItem> {
        val ids = QueueIds.assign(tracks.map { it.id }, taken)
        return tracks.mapIndexed { i, t -> mediaItem(t, dj, context, radio).buildUpon().setMediaId(ids[i]).build() }
    }

    /** The media ids in the queue. */
    private fun entryIds(p: Player) = (0 until p.mediaItemCount).map { p.getMediaItemAt(it).mediaId }

    /**
     * Plays tracks from start (at startMs into it); source names what's playing (an album or
     * playlist). context is the station they came from, when they did (MUSIC-17).
     */
    fun play(tracks: List<ItemSummary>, start: Int, source: String? = null, startMs: Long = 0, context: String? = null) =
        start(tracks, start, source, null, startMs, context)

    /** Plays a generated station; with a radio request it keeps topping itself up. */
    fun playStation(station: Station, radio: RadioRequest? = null) = start(station.items, 0, station.title, radio, context = station.context, generated = true)

    private fun start(tracks: List<ItemSummary>, start: Int, source: String?, radio: RadioRequest?, startMs: Long = 0, context: String? = null,
        generated: Boolean = radio != null || context != null) {
        if (tracks.isEmpty()) return
        _source.value = source
        newQueue(radio, context, generated)
        scope.launch(Dispatchers.Main) {
            val c = connect()
            val index = start.coerceIn(0, tracks.size - 1)
            c.setMediaItems(mediaItems(tracks, context = context, radio = radio != null), index, startMs.coerceAtLeast(0))
            // While casting (or with a device connected and idle) the new queue plays there.
            if (casting || beginCasting()) castTrack(index, startMs.coerceAtLeast(0))
            else { c.prepare(); c.play() }
        }
    }

    /** Plays the best match for a spoken or typed search (MusicLibrary resolves it). */
    fun playFromSearch(query: String) = with { c ->
        _source.value = null
        newQueue(null, null)
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

    /** A new queue: its station (if any) and context, with nothing skipped or played on it yet. */
    private fun newQueue(radio: RadioRequest?, context: String?, generated: Boolean = radio != null || context != null) {
        this.radio = radio
        stationContext = context
        this.generated = generated
        steering = RadioSteering()
        queueGen++
        djCount = 0
        topUpRetry?.cancel()
        topUpWaitMs = TOP_UP_RETRY_MS
    }

    /**
     * Continues a station when five or fewer tracks are left, steered by what was skipped and
     * played through. A station never ends: the server comes round again once everything has
     * played, and a top-up that fails or brings nothing tries again on the next track, or by
     * itself when there isn't one.
     */
    private fun topUpRadio(p: Player) {
        val r = radio ?: return
        if (refilling || steeringNow || tracksLeft(p) > 5) return
        refilling = true
        val exclude = steering.exclude(_queue.value.map { it.id })
        val gens = queueGen to steerGen
        scope.launch {
            val st = withContext(Dispatchers.IO) { runCatching { marquee.music.musicRadio(steering.request(r, exclude)) }.getOrNull() }
            refilling = false
            val c = controller
            if (c == null || gens != queueGen to steerGen) return@launch
            val upcoming = (c.currentMediaItemIndex until c.mediaItemCount).map { meta(c.getMediaItemAt(it)).id }.toSet()
            val fresh = RadioSteering.toAdd(wanted(st), upcoming) { it.id }
            if (fresh.isEmpty()) { retryTopUp(c); return@launch }
            topUpWaitMs = TOP_UP_RETRY_MS
            addToStation(c, fresh, st?.context)
        }
    }

    /** A top-up's tracks, without any given a thumbs down since it was asked for. */
    private fun wanted(st: Station?): List<ItemSummary> {
        val marks = marquee.dislikes.marks.value
        return st?.items.orEmpty().filterNot { marks.disliked(it.id, it.disliked) }
    }

    /** With nothing left to play there may be no next track to try again on. */
    private fun retryTopUp(c: Player) {
        if (tracksLeft(c) > 1 || topUpRetry?.isActive == true) return
        val wait = topUpWaitMs
        topUpWaitMs = (wait * 2).coerceAtMost(TOP_UP_RETRY_MAX_MS)
        topUpRetry = scope.launch {
            delay(wait)
            topUpRadio(c)
        }
    }

    /** How many tracks are left to play, this one included, in the order they'll play. */
    private fun tracksLeft(p: Player): Int {
        if (!p.shuffleModeEnabled) return p.mediaItemCount - p.currentMediaItemIndex
        val timeline = p.currentTimeline
        if (timeline.isEmpty) return 0
        var left = 1
        var i = p.currentMediaItemIndex
        while (true) {
            i = timeline.getNextWindowIndex(i, Player.REPEAT_MODE_OFF, true)
            if (i == C.INDEX_UNSET) return left
            left++
        }
    }

    /** Adds to the end of a station's queue; if it had already played to its end, on with the first of them. */
    private fun addToStation(c: Player, tracks: List<ItemSummary>, context: String?) {
        val ended = c.playbackState == Player.STATE_ENDED
        val first = c.mediaItemCount
        c.addMediaItems(mediaItems(tracks, entryIds(c), context = stationContext ?: context, radio = true))
        if (ended) {
            c.seekTo(first, 0)
            c.prepare()
            c.play()
        }
        // A radio can play for days: what played long ago leaves the queue (top-ups still leave it out).
        if (!c.shuffleModeEnabled) RadioSteering.toTrim(c.currentMediaItemIndex).takeIf { it > 0 }?.let { c.removeMediaItems(0, it) }
    }

    /**
     * After two quick skips in a row: asks the station again now, steered away from them, and
     * puts its answer in place of the station's own tracks after the current one. Tracks queued
     * by hand and DJ picks stay.
     */
    private fun topUpEarly() {
        val r = radio ?: return
        if (steeringNow) return
        steeringNow = true
        steerGen++
        steering.toppedUp()
        val exclude = steering.exclude(_queue.value.map { it.id })
        val gen = queueGen
        scope.launch {
            val st = withContext(Dispatchers.IO) { runCatching { marquee.music.musicRadio(steering.request(r, exclude)) }.getOrNull() }
            steeringNow = false
            val c = controller ?: return@launch
            if (st == null || gen != queueGen) return@launch
            // Nothing new (or no answer): the tracks already queued play on.
            val fresh = wanted(st)
            if (fresh.isEmpty()) return@launch
            // From the end back, removing each run of the station's entries in one go.
            val at = c.currentMediaItemIndex
            var i = c.mediaItemCount - 1
            while (i > at) {
                if (!meta(c.getMediaItemAt(i)).radio) { i--; continue }
                var from = i
                while (from - 1 > at && meta(c.getMediaItemAt(from - 1)).radio) from--
                c.removeMediaItems(from, i + 1)
                i = from - 1
            }
            val upcoming = (c.currentMediaItemIndex until c.mediaItemCount).map { meta(c.getMediaItemAt(it)).id }.toSet()
            addToStation(c, RadioSteering.toAdd(fresh, upcoming) { it.id }, st.context)
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
            c.addMediaItems(c.currentMediaItemIndex + 1, mediaItems(listOf(pick), entryIds(c), dj = dj.label))
            djCount = 0
        }
    }

    /** Connects to the music service so stations and the DJ work even when playback starts elsewhere (Android Auto). */
    fun attach() { scope.launch { connect() } }

    /** Takes over a queue started outside the app: its name and, for a station, how to continue it. */
    fun adopt(source: String?, radio: RadioRequest?, context: String? = null) = scope.launch {
        _source.value = source
        newQueue(radio, context)
    }

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

    fun setDJ(dj: DJ?) {
        _dj.value = dj
        djCount = 0
        prefs.edit().putString("dj", dj?.name).apply()
    }

    fun setSleep(s: Sleep?) { _sleep.value = s }

    /** A rating set elsewhere (a track row or an item page): Now Playing follows it when that track is playing. */
    fun ratingChanged(id: Long, rating: Double?) { if (lastId == id) _rating.value = rating }

    /**
     * Rates a track 0–10, or clears it with null (MUSIC-11). It takes the track rather than going
     * by what's playing: the song may have changed by the time the person picks.
     */
    fun rate(id: Long, rating: Double?) {
        val before = _rating.value.takeIf { lastId == id }
        if (lastId == id) _rating.value = rating
        val undo = marquee.dislikes.rated(id, rating)
        scope.launch {
            // Kept on the device and sent later when the server can't be reached (USER-18).
            val ok = marquee.sync.saveShowing(marquee.sync.rating(id, rating))
            if (!ok) {
                undo()
                if (lastId == id) _rating.value = before
            }
        }
    }

    /**
     * Gives a song a thumbs down or takes it back (MUSIC-25). It shows at once in every view, with
     * the heart and a rating of 3.5 or more cleared as the server clears them; a refusal puts them
     * back. Once saved, a radio or generated station plays on without the song.
     */
    fun setDisliked(id: Long, title: String, on: Boolean): Job {
        val undo = marquee.dislikes.thumbsDown(id, on)
        val heart = marquee.favorites.marks.value[id]
        if (on) marquee.favorites.mark(id, false)
        return scope.launch {
            // Kept on the device and sent later when the server can't be reached (USER-18).
            val ok = marquee.sync.saveShowing(marquee.sync.dislike(id, on))
            if (!ok) {
                undo()
                if (on) { if (heart == null) marquee.favorites.forget(id) else marquee.favorites.mark(id, heart) }
                android.widget.Toast.makeText(context, if (on) "Couldn't give “$title” a thumbs down" else "Couldn't take back the thumbs down on “$title”", android.widget.Toast.LENGTH_SHORT).show()
                return@launch
            }
            if (on) {
                marquee.favorites.changed()
                leaveDisliked(id)
            } else steering.notDisliked(id)
        }
    }

    /** A radio or generated station plays on without a song given a thumbs down. */
    private fun leaveDisliked(id: Long) {
        val c = controller ?: return
        val queue = (0 until c.mediaItemCount).map { meta(c.getMediaItemAt(it)).id }
        val move = RadioSteering.afterDislike(id, generated, queue, c.currentMediaItemIndex, c.shuffleModeEnabled)
        if (generated) steering.disliked(id)
        move.remove.forEach(c::removeMediaItem)
        if (!move.skip) return
        val next = c.nextMediaItemIndex
        when {
            next != C.INDEX_UNSET && next != c.currentMediaItemIndex -> if (casting) castNext() else c.seekToNextMediaItem()
            casting -> { marquee.cast.pause(); topUpRadio(c) }
            // The last of the queue goes to its end, where a radio's top-up carries on as it always does.
            c.duration > 0 && c.repeatMode == Player.REPEAT_MODE_OFF -> c.seekTo(c.duration)
            else -> c.pause()
        }
    }

    /** Adds tracks after the current one (Play Next) or at the end. */
    fun enqueue(tracks: List<ItemSummary>, next: Boolean) = with { c ->
        val items = mediaItems(tracks, entryIds(c))
        if (c.mediaItemCount == 0) {
            c.setMediaItems(items)
            if (casting || beginCasting()) castTrack(0, 0) else { c.prepare(); c.play() }
        }
        else if (next) c.addMediaItems(c.currentMediaItemIndex + 1, items) else c.addMediaItems(items)
    }

    private fun with(block: (MediaController) -> Unit) { scope.launch { block(connect()) } }
    /** An explicit pause (not a toggle). */
    fun pause() = with { if (casting) marquee.cast.pause() else it.pause() }
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
            if (_sleep.value == Sleep.EndOfTrack) _sleep.value = null else castNext(ended = true)
        }
        tick()
        return true
    }

    private fun castTrack(index: Int, at: Long) {
        val c = controller ?: return
        if (index !in 0 until c.mediaItemCount) return
        val t = meta(c.getMediaItemAt(index))
        scope.launch {
            val error = marquee.cast.load(t.id, at, t.title, t.artist.ifBlank { null }, t.artwork?.toString(), music = true, context = t.context)
            if (error != null) android.widget.Toast.makeText(context, "Couldn't play on the Cast device: $error", android.widget.Toast.LENGTH_LONG).show()
        }
    }

    /** Moves the receiver on; ended when it finished the track (so it counts as played through). */
    private fun castNext(ended: Boolean = false) {
        val c = controller ?: return
        val n = c.nextMediaItemIndex
        if (n == C.INDEX_UNSET) { marquee.cast.pause(); return }
        castEnded = ended
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
        newQueue(null, null)
        _sleep.value = null
        it.stop()
        it.clearMediaItems()
    }

    private companion object {
        const val TOP_UP_RETRY_MS = 10_000L
        const val TOP_UP_RETRY_MAX_MS = 60_000L
    }
}
