package app.marquee.core

import androidx.media3.common.Player
import app.marquee.api.models.CreateWatchGroupRequest
import app.marquee.api.models.WatchGroup
import app.marquee.api.models.WatchGroupCommandRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.Duration
import java.time.OffsetDateTime
import kotlin.math.abs

/**
 * Watch together (SyncPlay, D73): keeps an ExoPlayer in step with a group. The group's state is
 * long-polled; the viewer's own play, pause, seek and loading are sent as commands, and changes
 * made to follow the group aren't echoed back. Main thread only (ExoPlayer's rule).
 */
class WatchTogether(private val marquee: Marquee, private val player: Player, private val itemId: Long, private val scope: CoroutineScope) {
    private val _group = MutableStateFlow<WatchGroup?>(null)
    val group: StateFlow<WatchGroup?> = _group
    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error

    private var quietUntil = 0L
    private var buffering = false
    private var polling: Job? = null

    private val listener = object : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            val g = _group.value ?: return
            if (!mine() || buffering) return
            if (isPlaying && !g.playing) send(WatchGroupCommandRequest.Action.PLAY)
            if (!isPlaying && player.playbackState == Player.STATE_READY && !player.playWhenReady && g.playing) send(WatchGroupCommandRequest.Action.PAUSE)
        }

        override fun onPositionDiscontinuity(old: Player.PositionInfo, new: Player.PositionInfo, reason: Int) {
            if (reason == Player.DISCONTINUITY_REASON_SEEK && _group.value != null && mine()) send(WatchGroupCommandRequest.Action.SEEK)
        }

        override fun onPlaybackStateChanged(state: Int) {
            if (_group.value == null) return
            when {
                state == Player.STATE_BUFFERING && !buffering -> { buffering = true; send(WatchGroupCommandRequest.Action.BUFFERING) }
                state == Player.STATE_READY && buffering -> { buffering = false; send(WatchGroupCommandRequest.Action.READY) }
            }
        }
    }

    private fun mine() = System.currentTimeMillis() > quietUntil
    private fun quiet() { quietUntil = System.currentTimeMillis() + 1200 }

    fun start() = scope.launch(Dispatchers.Main) {
        val pos = player.currentPosition
        withContext(Dispatchers.IO) { runCatching { marquee.syncplay.createWatchGroup(CreateWatchGroupRequest(itemId, pos)) } }
            .onSuccess(::begin).onFailure { _error.value = it.message }
    }

    fun join(id: String) = scope.launch(Dispatchers.Main) {
        withContext(Dispatchers.IO) { runCatching { marquee.syncplay.joinWatchGroup(id) } }
            .onSuccess(::begin).onFailure { _error.value = "Couldn't join: ${it.message}" }
    }

    fun leave() {
        val g = _group.value ?: return
        _group.value = null
        polling?.cancel()
        player.removeListener(listener)
        marquee.scope.launch { runCatching { marquee.syncplay.leaveWatchGroup(g.id) } }
    }

    private fun begin(g: WatchGroup) {
        _error.value = null
        player.removeListener(listener)
        player.addListener(listener)
        apply(g)
        polling?.cancel()
        polling = scope.launch(Dispatchers.Main) {
            while (isActive) {
                val cur = _group.value ?: return@launch
                val next = withContext(Dispatchers.IO) { runCatching { marquee.syncplay.getWatchGroup(cur.id, cur.version) } }
                if (!isActive || _group.value == null) return@launch
                next.onSuccess(::apply).onFailure {
                    _error.value = "The group has ended."
                    _group.value = null
                    player.removeListener(listener)
                    return@launch
                }
            }
        }
    }

    private fun apply(g: WatchGroup) {
        _group.value = g
        // The server's clock, so differences between the devices' clocks don't matter.
        val skewMs = Duration.between(OffsetDateTime.now(), g.serverTime).toMillis()
        val serverNow = OffsetDateTime.now().plusNanos(skewMs * 1_000_000)
        val target = if (g.playing) g.positionMs + Duration.between(g.at, serverNow).toMillis() else g.positionMs
        if (abs(player.currentPosition - target) > 1500) { quiet(); player.seekTo(target) }
        if (g.playing && !player.playWhenReady) { quiet(); player.play() }
        if (!g.playing && player.playWhenReady) { quiet(); player.pause() }
    }

    private fun send(action: WatchGroupCommandRequest.Action) {
        val g = _group.value ?: return
        val pos = player.currentPosition
        scope.launch(Dispatchers.Main) {
            withContext(Dispatchers.IO) { runCatching { marquee.syncplay.watchGroupCommand(g.id, WatchGroupCommandRequest(action, pos)) } }
                .onSuccess { if (_group.value != null) _group.value = it }
                .onFailure { _error.value = it.message }
        }
    }
}
