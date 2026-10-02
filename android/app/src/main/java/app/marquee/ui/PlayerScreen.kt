package app.marquee.ui

import android.app.Activity
import android.content.pm.ActivityInfo
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import android.view.WindowManager
import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import app.marquee.api.models.Marker
import app.marquee.api.models.PlaybackProgress
import app.marquee.api.models.PlaybackRequest
import app.marquee.api.models.PlaybackSession
import app.marquee.core.AndroidProfile
import kotlinx.coroutines.Dispatchers
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Plays a video: asks the server for a session (direct play, direct stream or transcode for
 * this device), plays it with ExoPlayer, reports progress, offers Skip Intro/Credits and
 * moves on to the next episode.
 */
@OptIn(UnstableApi::class)
@Composable
fun PlayerScreen(nav: NavHostController, itemId: Long, startMs: Long?) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val player = remember { ExoPlayer.Builder(context).build() }
    var session by remember { mutableStateOf<PlaybackSession?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var position by remember { mutableLongStateOf(0L) }
    var playing by remember { mutableStateOf(false) }

    fun report(state: PlaybackProgress.State) {
        val s = session ?: return
        val pos = player.currentPosition
        marquee.scope.launch { runCatching { marquee.playback.reportPlayback(s.id, PlaybackProgress(pos, state)) } }
    }

    // ExoPlayer is main-thread only; don't rely on the effect's dispatcher for that.
    LaunchedEffect(itemId) {
        withContext(Dispatchers.Main) { runCatching {
            withContext(Dispatchers.IO) { marquee.playback.startPlayback(PlaybackRequest(itemId, AndroidProfile.profile, startMs = startMs)) }
        }.onSuccess { s ->
            session = s
            player.setMediaItem(MediaItem.fromUri(marquee.absolute(s.url)!!))
            player.prepare()
            if (s.startMs > 0) player.seekTo(s.startMs)
            player.playWhenReady = true
        }.onFailure { error = it.message ?: "Couldn't start playback" } }
    }
    LaunchedEffect(session) {
        withContext(Dispatchers.Main) { while (session != null) {
            position = player.currentPosition
            if (player.isPlaying && position / 1000 % 10 == 0L) report(PlaybackProgress.State.PLAYING)
            delay(1000)
        } }
    }
    DisposableEffect(Unit) {
        val activity = context as? Activity
        val window = activity?.window
        window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        // Phones: full screen in landscape, like any video app.
        val bars = window?.let { WindowCompat.getInsetsController(it, it.decorView) }
        if (!marquee.isTv) {
            activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
            bars?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            bars?.hide(WindowInsetsCompat.Type.systemBars())
        }
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                playing = isPlaying
                report(if (isPlaying) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)
            }
            override fun onPlaybackStateChanged(state: Int) {
                if (state != Player.STATE_ENDED) return
                report(PlaybackProgress.State.PAUSED)
                // Up next: the following episode (or nothing).
                marquee.scope.launch(Dispatchers.Main) {
                    val next = withContext(Dispatchers.IO) { runCatching { marquee.items.nextItem(itemId) }.getOrNull() }
                    if (next != null) nav.navigate("player/${next.id}?start=0") { popUpTo("player/{id}?start={start}") { inclusive = true } }
                    else nav.popBackStack()
                }
            }
            override fun onPlayerError(e: androidx.media3.common.PlaybackException) {
                error = "This video can't be played on this device (${e.errorCodeName})."
            }
        }
        player.addListener(listener)
        onDispose {
            window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            if (!marquee.isTv) {
                activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                bars?.show(WindowInsetsCompat.Type.systemBars())
            }
            val s = session
            val pos = player.currentPosition
            player.removeListener(listener)
            player.release()
            if (s != null) marquee.scope.launch {
                runCatching { marquee.playback.reportPlayback(s.id, PlaybackProgress(pos, PlaybackProgress.State.PAUSED)) }
                runCatching { marquee.playback.stopPlayback(s.id) }
            }
        }
    }

    Box(Modifier.fillMaxSize().background(Color.Black).semantics {
        contentDescription = "Video player"
        stateDescription = if (playing) "Playing" else "Paused"
    }) {
        AndroidView({ PlayerView(it).apply { this.player = player; keepScreenOn = true } }, Modifier.fillMaxSize())
        if (session == null && error == null) CircularProgressIndicator(Modifier.align(Alignment.Center))
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error) }
        val marker = session?.markers?.firstOrNull { position >= it.startMs && position < it.endMs - 1000 }
        if (marker != null) Button(
            onClick = { player.seekTo(marker.endMs + 500) },
            modifier = Modifier.align(Alignment.BottomEnd).padding(32.dp),
        ) { Text(if (marker.kind == Marker.Kind.CREDITS) "Skip Credits" else "Skip Intro") }
    }
}
