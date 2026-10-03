package app.marquee.core

import android.content.Context
import android.os.SystemClock
import android.widget.Toast
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.RemoteCapability
import app.marquee.api.models.RemoteCommand
import app.marquee.api.models.RemoteInboxRequest
import app.marquee.api.models.RemotePlayerState
import app.marquee.music.MusicController
import app.marquee.ui.summary
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.runInterruptible
import kotlinx.coroutines.withContext
import kotlin.math.abs

/**
 * What an open video player offers remote control: its state, and the commands it runs itself
 * (pause, seek, audio and subtitle tracks…). PlayerScreen registers one while it's showing.
 */
interface RemoteVideo {
    fun state(): RemotePlayerState
    fun execute(c: RemoteCommand)
}

/**
 * Remote control (USER-14): this app as a player the person's other apps can control. Phones
 * and tablets are players while the app is in the foreground and while music plays in the
 * background; TVs while the app runs. It long-polls its inbox (which lists it as a player),
 * runs the commands that arrive, and reports what it's playing on every change and every
 * 10 s while playing. Main thread only.
 */
class RemoteReceiver(private val context: Context, private val marquee: Marquee, private val music: () -> MusicController) {
    private val scope = MainScope()
    private var loop: Job? = null
    private var ticker: Job? = null
    private var foreground = 0
    private var cursor = 0L
    private var cursorToken: String? = null

    /** The open video player, if any (set by PlayerScreen). */
    var video: RemoteVideo? = null
        set(v) { field = v; changed() }

    private val _navigate = MutableSharedFlow<String>(extraBufferCapacity = 4)
    /** Screens the app should open for a command (a video to play); MainScreen follows it. */
    val navigate: SharedFlow<String> = _navigate

    /** Called by the activity lifecycle: started and stopped activities. */
    fun activityStarted() { foreground++; update() }
    fun activityStopped() { foreground = (foreground - 1).coerceAtLeast(0); update() }

    /** Starts watching the session and music; call once. */
    fun attach() {
        scope.launch {
            combine(marquee.state, marquee.connection, music().playing) { _, _, _ -> }.collect { update() }
        }
    }

    private fun shouldRun(): Boolean =
        marquee.state.value == Marquee.State.SignedIn && marquee.token != null && marquee.baseUrl != null &&
            (marquee.isTv || foreground > 0 || music().playing.value)

    private fun update() {
        if (shouldRun()) {
            if (loop?.isActive != true) loop = scope.launch { poll() }
            if (ticker?.isActive != true) ticker = scope.launch { tick() }
        } else {
            loop?.cancel(); loop = null
            ticker?.cancel(); ticker = null
        }
    }

    private suspend fun poll() {
        while (scope.isActive && shouldRun()) {
            val token = marquee.token
            if (token != cursorToken) { cursorToken = token; cursor = 0 } // another person signed in
            val req = RemoteInboxRequest(listOf(RemoteCapability.VIDEO, RemoteCapability.MUSIC), cursor, state())
            val r = runCatching { runInterruptible(Dispatchers.IO) { marquee.playback.remoteInbox(req) } }
            r.onSuccess { inbox ->
                if (marquee.token != token) return@onSuccess
                cursor = inbox.cursor
                inbox.commands.forEach { runCatching { execute(it) } }
            }.onFailure { e ->
                if (e is kotlinx.coroutines.CancellationException) throw e
                delay(5_000)
            }
        }
    }

    // Reporting: on every play, pause, seek or track change, and every 10 s while playing.
    private var last: RemotePlayerState? = null
    private var lastAt = 0L
    private var forced = false

    /** Something changed here (a command ran, a player opened or closed): report it soon. */
    fun changed() { forced = true }

    private suspend fun tick() {
        while (true) {
            delay(1_000)
            val s = state()
            val now = SystemClock.elapsedRealtime()
            val prev = last
            val playing = s.state == RemotePlayerState.State.PLAYING
            // Where it should be by now if nothing was sought.
            val expected = prev?.let { it.positionMs + if (it.state == RemotePlayerState.State.PLAYING) now - lastAt else 0 } ?: 0
            val moved = prev == null || prev.itemId != s.itemId || prev.state != s.state || abs(s.positionMs - expected) > 2_500 ||
                prev.audioStreamId != s.audioStreamId || prev.subtitleStreamId != s.subtitleStreamId || prev.volume != s.volume ||
                prev.queueIndex != s.queueIndex || prev.queueLength != s.queueLength
            if (forced || moved || (playing && now - lastAt >= 10_000)) {
                forced = false
                last = s
                lastAt = now
                runCatching { withContext(Dispatchers.IO) { marquee.playback.reportRemoteState(s) } }
            }
        }
    }

    /** What's playing here: the video player's, the music's, or nothing. */
    fun state(): RemotePlayerState {
        video?.let { return it.state() }
        val m = music()
        val n = m.now.value ?: return RemotePlayerState(RemotePlayerState.State.IDLE, 0)
        val (pos, dur) = m.position.value
        return RemotePlayerState(
            state = if (m.playing.value) RemotePlayerState.State.PLAYING else RemotePlayerState.State.PAUSED,
            positionMs = pos, itemId = n.id, itemType = ItemType.TRACK, title = n.title,
            subtitle = listOf(n.artist, n.album).filter { it.isNotBlank() }.joinToString(" · ").ifBlank { null },
            artItemId = n.albumId ?: n.id, durationMs = dur.takeIf { it > 0 },
            queueIndex = m.index.value, queueLength = m.queue.value.size,
            volume = m.volume?.toDouble(), shuffle = m.shuffle.value,
        )
    }

    // Commands

    private var lastFrom: String? = null
    private var lastCommandAt = 0L

    private suspend fun execute(c: RemoteCommand) {
        // "Controlled from …" on the first command from a controller (or after a long pause).
        val now = SystemClock.elapsedRealtime()
        if (c.from != lastFrom || now - lastCommandAt > 10 * 60_000) {
            Toast.makeText(context, "Controlled from ${c.from ?: "another device"}", Toast.LENGTH_SHORT).show()
        }
        lastFrom = c.from
        lastCommandAt = now
        val v = video
        val m = music()
        when (c.type) {
            RemoteCommand.Type.PLAY -> play(c)
            else -> if (v != null) v.execute(c) else when (c.type) {
                RemoteCommand.Type.PAUSE -> m.pause()
                RemoteCommand.Type.RESUME -> m.resume()
                RemoteCommand.Type.SEEK -> c.positionMs?.let { m.seek(it) }
                RemoteCommand.Type.STOP -> m.stop()
                RemoteCommand.Type.NEXT -> m.next()
                RemoteCommand.Type.PREVIOUS -> m.previous()
                RemoteCommand.Type.SET_VOLUME -> c.volume?.let { m.setVolume(it.toFloat()) }
                else -> {} // audio and subtitle tracks are for videos
            }
        }
        delay(300) // let the player catch up before reporting
        changed()
    }

    /** Plays like the item's own Play button; several ids are a track queue. */
    private suspend fun play(c: RemoteCommand) {
        val ids = c.itemIds.orEmpty()
        if (ids.isEmpty()) return
        val m = music()
        val start = c.startMs
        if (ids.size > 1) {
            val items = withContext(Dispatchers.IO) {
                ids.chunked(16).flatMap { chunk -> chunk.map { id -> async { runCatching { summary(id) }.getOrNull() } }.awaitAll() }
            }.filterNotNull()
            val index = (c.index ?: 0).coerceIn(0, items.size - 1)
            val first = items.getOrNull(index) ?: return
            if (first.type == ItemType.TRACK) {
                stopVideo()
                val list = if (c.shuffle == true) listOf(first) + (items - first).shuffled() else items
                m.play(list, if (c.shuffle == true) 0 else index, startMs = start ?: 0)
            } else playVideo(first.id, start)
            return
        }
        val d = withContext(Dispatchers.IO) { marquee.items.getItem(ids[0]) }
        when (d.type) {
            ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO -> playVideo(d.id, start)
            ItemType.SHOW, ItemType.SEASON -> {
                val next = withContext(Dispatchers.IO) {
                    runCatching { marquee.items.itemLeaves(d.id, unwatched = true).firstOrNull() ?: marquee.items.itemLeaves(d.id).firstOrNull() }.getOrNull()
                } ?: return
                playVideo(next.id, start)
            }
            ItemType.TRACK -> {
                stopVideo()
                m.play(listOf(withContext(Dispatchers.IO) { summary(d.id) }), 0, startMs = start ?: 0)
            }
            else -> {
                // Albums, artists, collections: their tracks (or the first video) in order.
                val leaves = withContext(Dispatchers.IO) { marquee.items.itemLeaves(d.id, shuffle = c.shuffle == true) }
                val first = leaves.firstOrNull() ?: return
                if (first.type == ItemType.TRACK) {
                    stopVideo()
                    m.play(leaves.filter { it.type == ItemType.TRACK }, 0, source = d.title, startMs = start ?: 0)
                } else playVideo(first.id, start)
            }
        }
    }

    private fun summary(id: Long): ItemSummary = marquee.items.getItem(id).summary()

    private fun playVideo(id: Long, startMs: Long?) {
        music().pause()
        _navigate.tryEmit("player/$id" + (startMs?.let { "?start=$it" } ?: ""))
    }

    /** Music replaces a video playing here. */
    private fun stopVideo() {
        video?.execute(RemoteCommand(RemoteCommand.Type.STOP))
    }
}
