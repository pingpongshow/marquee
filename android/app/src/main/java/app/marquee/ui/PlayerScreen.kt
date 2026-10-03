package app.marquee.ui

import app.marquee.core.WatchTogether
import androidx.compose.runtime.collectAsState
import androidx.compose.material.icons.filled.Groups
import android.app.Activity
import android.content.pm.ActivityInfo
import android.net.Uri
import android.view.WindowManager
import androidx.annotation.OptIn
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Remove
import androidx.compose.material.icons.filled.Forward30
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.CastConnected
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.Marker
import app.marquee.api.models.MediaStream
import app.marquee.api.models.PlaybackProgress
import app.marquee.api.models.PlaybackRequest
import app.marquee.api.models.PlaybackSession
import app.marquee.api.models.RemoteCommand
import app.marquee.api.models.RemotePlayerState
import app.marquee.core.AndroidProfile
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * What the viewer picked in the player's settings; null means the server's choice. The
 * timing offsets (PLAY-17) are sent together once either is changed, and the server
 * remembers them for this file.
 */
private data class Choice(val fileId: Long? = null, val audio: Long? = null, val subtitle: Long? = null, val maxKbps: Int? = null,
    val subOffset: Int? = null, val audioOffset: Int? = null)

/** Quality caps offered in the player (kbps; null = no cap). */
private val qualities = listOf<Pair<String, Int?>>(
    "Original" to null, "20 Mbps 1080p" to 20_000, "12 Mbps 1080p" to 12_000, "8 Mbps 1080p" to 8_000,
    "4 Mbps 720p" to 4_000, "2 Mbps 720p" to 2_000, "1 Mbps 480p" to 1_000,
)

/** Playback speeds offered in the player (PLAY-19). */
private val speeds = listOf(0.5f, 0.75f, 1f, 1.25f, 1.5f, 1.75f, 2f)

private fun speedLabel(s: Float) = (if (s == s.toInt().toFloat()) s.toInt().toString() else s.toString()) + "×"

/** Subtitle and audio timing limits and step (PLAY-17), ms. */
private const val maxOffset = 30_000
private const val offsetStep = 100

/**
 * Plays a video: asks the server for a session (direct play, direct stream or transcode for
 * this device), plays it with ExoPlayer under Marquee's own controls (seek previews, audio,
 * subtitles, quality and versions), reports progress, offers Skip Intro/Credits and moves on
 * to the next episode. Downloads play from the device.
 */
@OptIn(UnstableApi::class)
@Composable
fun PlayerScreen(nav: NavHostController, itemId: Long, startMs: Long?, groupId: String? = null) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val player = remember { videoPlayer(context, seekIncrements = true) }
    var session by remember { mutableStateOf<PlaybackSession?>(null) }
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var thumbs by remember { mutableStateOf<TrickplayThumbs?>(null) }
    var choice by remember { mutableStateOf(Choice()) }
    var error by remember { mutableStateOf<String?>(null) }
    var position by remember { mutableLongStateOf(0L) }
    var duration by remember { mutableLongStateOf(0L) }
    var playing by remember { mutableStateOf(false) }
    var buffering by remember { mutableStateOf(true) }
    var controls by remember { mutableStateOf(true) }
    var settings by remember { mutableStateOf(false) }
    var lastTouch by remember { mutableIntStateOf(0) } // bumps keep the controls up
    var scrub by remember { mutableStateOf<Long?>(null) }
    // Set while the video plays on a Cast device (D82); the local player stays idle then.
    var casting by remember { mutableStateOf(false) }
    // The item's details, once loaded (or null if they couldn't be): the Cast hand-off waits for them.
    val detailReady = remember { CompletableDeferred<ItemDetail?>() }
    // This player's own place in navigation: it only moves on (or back) while it's still showing.
    val ownEntry = remember { nav.currentBackStackEntry }
    fun stillShowing() = ownEntry != null && nav.currentBackStackEntry === ownEntry
    // Watch together (SYNC-1).
    val together = remember { WatchTogether(marquee, player, itemId, scope) }
    val group by together.group.collectAsState()
    val groupError by together.error.collectAsState()
    var groupPanel by remember { mutableStateOf(false) }
    var finding by remember { mutableStateOf(false) }
    // Playback speed (PLAY-19): 1× for every new video.
    var speed by remember { mutableFloatStateOf(1f) }
    // Cinema trailers (PLAY-18): what plays before the movie, and which one is playing (-1 = the movie).
    var rolls by remember { mutableStateOf(emptyList<ItemSummary>()) }
    var roll by remember { mutableIntStateOf(-1) }
    // Bumped by a timing change; the session restarts once the stepping stops.
    var timingChange by remember { mutableIntStateOf(0) }
    LaunchedEffect(groupId) { if (groupId != null) { delay(1500); together.join(groupId) } }
    // Downloaded: play the file on the device, even when the server is reachable (D64).
    val downloads = LocalDownloads.current
    val local = remember(itemId) { downloads.localFile(itemId) }
    fun recordLocal(ended: Boolean = false) {
        val dur = player.duration
        val pos = player.currentPosition
        if (local == null || pos <= 0) return
        downloads.recordProgress(itemId, pos, watched = ended || (dur > 0 && pos >= dur * 0.9))
    }

    fun report(state: PlaybackProgress.State) {
        val s = session ?: return
        val pos = player.currentPosition
        marquee.scope.launch { runCatching { marquee.playback.reportPlayback(s.id, PlaybackProgress(pos, state)) } }
    }

    fun closeSession(s: PlaybackSession?, pos: Long) {
        if (s != null) marquee.scope.launch {
            runCatching { marquee.playback.reportPlayback(s.id, PlaybackProgress(pos, PlaybackProgress.State.PAUSED)) }
            runCatching { marquee.playback.stopPlayback(s.id) }
        }
    }

    /**
     * Starts (or restarts, after a settings change) a server session at a position. A trailer
     * before the movie plays as its own session, with the server's choices.
     */
    suspend fun start(at: Long?, c: Choice, playId: Long = itemId) = withContext(Dispatchers.Main) {
        val old = session
        val req = if (playId != itemId) PlaybackRequest(playId, AndroidProfile.profile, startMs = at)
        else PlaybackRequest(itemId, AndroidProfile.profile, fileId = c.fileId, audioStreamId = c.audio, subtitleStreamId = c.subtitle,
            startMs = at, maxBitrateKbps = c.maxKbps, subtitleOffsetMs = c.subOffset, audioOffsetMs = c.audioOffset)
        // Started on the session's own scope: if the screen goes away while the server is
        // starting it, the session is stopped as soon as it arrives rather than left running.
        val pending = marquee.scope.async { runCatching { marquee.playback.startPlayback(req) } }
        val result = try {
            pending.await()
        } catch (e: CancellationException) {
            marquee.scope.launch { pending.await().getOrNull()?.let { closeSession(it, at ?: 0) } }
            throw e
        }
        // Casting began meanwhile: the video plays there, not here.
        if (casting) { result.getOrNull()?.let { closeSession(it, at ?: 0) }; return@withContext }
        result
            .onSuccess { s ->
                if (old != null) closeSession(old, player.currentPosition)
                session = s
                error = null
                val item = MediaItem.Builder().setUri(marquee.absolute(s.url))
                // Text subtitles come as a WebVTT sidecar.
                s.subtitleUrl?.takeIf { s.subtitleFormat == PlaybackSession.SubtitleFormat.VTT }?.let { u ->
                    item.setSubtitleConfigurations(listOf(
                        MediaItem.SubtitleConfiguration.Builder(Uri.parse(marquee.absolute(u))).setMimeType(MimeTypes.TEXT_VTT)
                            .setSelectionFlags(C.SELECTION_FLAG_DEFAULT or C.SELECTION_FLAG_FORCED).build(),
                    ))
                }
                player.setMediaItem(item.build())
                player.prepare()
                if (s.startMs > 0) player.seekTo(s.startMs)
                player.playWhenReady = true
            }
            .onFailure { error = it.message ?: "Couldn't start playback" }
    }

    // ExoPlayer is main-thread only; don't rely on the effect's dispatcher for that.
    LaunchedEffect(itemId) {
        launch {
            detail = withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(itemId) }.getOrNull() }
            detailReady.complete(detail)
        }
        launch { thumbs = TrickplayThumbs.load(marquee, itemId) }
        // Already casting: the Cast hand-off below plays it there instead.
        if (local == null && marquee.cast.device.value != null) return@LaunchedEffect
        if (local != null) withContext(Dispatchers.Main) {
            player.setMediaItem(MediaItem.fromUri(Uri.fromFile(local)))
            player.prepare()
            val begin = startMs ?: downloads.resumePosition(itemId)
            if (begin > 0) player.seekTo(begin)
            player.playWhenReady = true
        } else {
            // Cinema trailers (PLAY-18): a movie started from the beginning (not resumed) gets
            // the server's trailers and pre-roll first; the list is empty when they're off.
            val d = detailReady.await()
            val fromStart = startMs == 0L || (startMs == null && (d?.viewOffsetMs ?: 0) <= 0)
            val pre = if (d?.type == ItemType.MOVIE && fromStart && groupId == null)
                withContext(Dispatchers.IO) { runCatching { marquee.playback.listPrerolls(itemId) }.getOrDefault(emptyList()) } else emptyList()
            if (pre.isNotEmpty() && !casting) {
                rolls = pre
                roll = 0
                start(0, choice, pre[0].id)
            } else start(startMs, choice)
        }
    }
    // Watching together keeps everyone at 1×.
    LaunchedEffect(group != null) { if (group != null && speed != 1f) { speed = 1f; player.setPlaybackSpeed(1f) } }
    LaunchedEffect(timingChange) {
        if (timingChange == 0) return@LaunchedEffect
        delay(800)
        start(player.currentPosition, choice)
    }

    /** After the trailers (or on Skip all): the movie itself, at 1×. */
    fun startFeature() {
        roll = -1
        rolls = emptyList()
        speed = 1f
        player.setPlaybackSpeed(1f)
        scope.launch { start(startMs, choice) }
    }

    fun nextRoll() {
        if (roll + 1 < rolls.size) {
            roll++
            val r = rolls[roll]
            scope.launch { start(0, Choice(), r.id) }
        } else startFeature()
    }
    LaunchedEffect(Unit) {
        withContext(Dispatchers.Main) {
            var ticks = 0
            while (true) {
                position = player.currentPosition
                duration = player.duration.coerceAtLeast(0)
                if (player.isPlaying && ++ticks % 10 == 0) report(PlaybackProgress.State.PLAYING)
                delay(500)
            }
        }
    }
    // Controls fade after a few seconds of playing untouched.
    LaunchedEffect(controls, lastTouch, playing, settings, scrub) {
        if (controls && playing && !settings && scrub == null) { delay(4000); controls = false }
    }
    VideoWindow()
    DisposableEffect(Unit) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                playing = isPlaying
                if (!isPlaying) recordLocal()
                report(if (isPlaying) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)
            }
            override fun onPlaybackStateChanged(state: Int) {
                buffering = state == Player.STATE_BUFFERING || state == Player.STATE_IDLE
                if (state != Player.STATE_ENDED) return
                if (roll >= 0) { nextRoll(); return }
                if (local != null) { recordLocal(ended = true); if (stillShowing()) nav.popBackStack(); return }
                report(PlaybackProgress.State.PAUSED)
                // Up next: the following episode (or nothing), unless the viewer has already left.
                marquee.scope.launch(Dispatchers.Main) {
                    val next = withContext(Dispatchers.IO) { runCatching { marquee.items.nextItem(itemId) }.getOrNull() }
                    if (!stillShowing()) return@launch
                    if (next != null) nav.navigate("player/${next.id}?start=0") { popUpTo("player/{id}?start={start}&group={group}") { inclusive = true } }
                    else nav.popBackStack()
                }
            }
            override fun onPlayerError(e: androidx.media3.common.PlaybackException) {
                // A trailer that won't play is skipped rather than holding up the movie.
                if (roll >= 0) { nextRoll(); return }
                error = "This video can't be played on this device (${e.errorCodeName})."
            }
        }
        player.addListener(listener)
        onDispose {
            val s = session
            val pos = player.currentPosition
            recordLocal()
            together.leave()
            player.removeListener(listener)
            player.release()
            closeSession(s, pos)
        }
    }

    fun poke() { controls = true; lastTouch++ }

    // Chromecast (D82): when a Cast device connects, the video moves there at the current
    // position; when casting stops, it carries on here from where the TV was.
    val castDevice by marquee.cast.device.collectAsState()
    var castError by remember { mutableStateOf<String?>(null) }
    val onCastFinished = remember { { if (stillShowing()) nav.popBackStack() } }
    LaunchedEffect(castDevice) {
        if (castDevice != null && local == null && !casting) {
            casting = true
            roll = -1
            rolls = emptyList()
            castError = null
            val here = session
            player.pause()
            closeSession(here, player.currentPosition)
            session = null
            // The title, artwork and resume point come from the details, so wait for them.
            val d = detailReady.await()
            // Opened while already casting nothing played here: start where asked, or resume.
            val at = if (here == null) startMs ?: d?.viewOffsetMs ?: 0 else player.currentPosition
            val title = d?.let { if (it.type == ItemType.EPISODE) "${it.grandparentTitle ?: ""} · ${it.title}" else it.title } ?: ""
            marquee.cast.onFinished = onCastFinished
            castError = marquee.cast.load(itemId, at, title, d?.year?.toString(), marquee.imageUrl(d?.images?.backdrop ?: d?.images?.poster, 640), music = false,
                fileId = choice.fileId, audioStreamId = choice.audio, subtitleStreamId = choice.subtitle)
        } else if (castDevice == null && casting) {
            casting = false
            castError = null
            if (marquee.cast.onFinished === onCastFinished) marquee.cast.onFinished = null
            start(marquee.cast.position.value.first, choice)
        }
    }
    DisposableEffect(Unit) {
        // Only clears its own hand-off: the next episode's player may have set one already.
        onDispose { if (marquee.cast.onFinished === onCastFinished) marquee.cast.onFinished = null }
    }
    // Remote control (USER-14): this player obeys the person's other apps while it's showing.
    val remote = LocalRemote.current
    DisposableEffect(Unit) {
        val target = object : app.marquee.core.RemoteVideo {
            override fun state(): RemotePlayerState {
                val d = detail
                val st = when {
                    error != null -> RemotePlayerState.State.STOPPED
                    player.isPlaying -> RemotePlayerState.State.PLAYING
                    player.playWhenReady && player.playbackState == Player.STATE_BUFFERING -> RemotePlayerState.State.BUFFERING
                    else -> RemotePlayerState.State.PAUSED
                }
                val sub = choice.subtitle ?: session?.subtitleStreamId
                return RemotePlayerState(
                    state = st, positionMs = player.currentPosition.coerceAtLeast(0), itemId = itemId, itemType = d?.type,
                    title = d?.title,
                    subtitle = d?.let { if (it.type == ItemType.EPISODE) listOfNotNull(it.grandparentTitle, it.parentTitle, it.index?.let { i -> "E$i" }).joinToString(" · ") else it.year?.toString() },
                    artItemId = if (d?.type == ItemType.EPISODE) d.grandparentId ?: itemId else itemId,
                    durationMs = player.duration.takeIf { it > 0 },
                    volume = player.volume.toDouble(),
                    audioStreamId = choice.audio ?: session?.audioStreamId,
                    subtitleStreamId = if (sub == null || sub == -1L) -1L else sub,
                )
            }

            override fun execute(c: RemoteCommand) {
                when (c.type) {
                    RemoteCommand.Type.PAUSE -> player.pause()
                    RemoteCommand.Type.RESUME -> player.play()
                    RemoteCommand.Type.SEEK -> c.positionMs?.let { player.seekTo(it) }
                    RemoteCommand.Type.STOP -> if (stillShowing()) nav.popBackStack()
                    RemoteCommand.Type.PREVIOUS -> player.seekTo(0)
                    RemoteCommand.Type.NEXT -> marquee.scope.launch(Dispatchers.Main) {
                        val next = withContext(Dispatchers.IO) { runCatching { marquee.items.nextItem(itemId) }.getOrNull() }
                        if (next != null && stillShowing()) nav.navigate("player/${next.id}?start=0") { popUpTo("player/{id}?start={start}&group={group}") { inclusive = true } }
                    }
                    RemoteCommand.Type.SET_VOLUME -> c.volume?.let { player.volume = it.toFloat().coerceIn(0f, 1f) }
                    RemoteCommand.Type.SET_AUDIO, RemoteCommand.Type.SET_SUBTITLE -> if (local == null) {
                        val id = c.streamId ?: return
                        val next = if (c.type == RemoteCommand.Type.SET_AUDIO) choice.copy(audio = id) else choice.copy(subtitle = if (id < 0) -1 else id)
                        choice = next
                        val at = player.currentPosition
                        scope.launch { start(at, next) }
                    }
                    else -> {}
                }
                poke()
            }
        }
        remote.video = target
        onDispose { if (remote.video === target) remote.video = null }
    }

    fun seekBy(ms: Long) {
        val target = ((scrub ?: player.currentPosition) + ms).coerceIn(0, duration.coerceAtLeast(1))
        scrub = target
        poke()
    }
    fun commitScrub() { scrub?.let { player.seekTo(it) }; scrub = null; poke() }

    val rootFocus = remember { FocusRequester() }
    LaunchedEffect(controls, settings) { if (!controls && !settings) runCatching { rootFocus.requestFocus() } }

    Box(
        Modifier.fillMaxSize().background(Color.Black)
            .semantics {
                contentDescription = "Video player"
                stateDescription = if (playing) "Playing" else "Paused"
                // Where it is, in seconds (for TalkBack and the UI tests).
                progressBarRangeInfo = ProgressBarRangeInfo((position / 1000f), 0f..(duration / 1000f).coerceAtLeast(1f))
            }
            .focusRequester(rootFocus)
            // TV remote with the controls hidden, or mid-scrub: left/right scrub with previews,
            // select jumps there (or plays/pauses).
            .onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown || settings || (controls && scrub == null)) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionLeft -> { seekBy(-10_000); true }
                    Key.DirectionRight -> { seekBy(10_000); true }
                    Key.DirectionCenter, Key.Enter, Key.MediaPlayPause -> {
                        if (scrub != null) commitScrub() else { if (player.isPlaying) player.pause() else player.play(); poke() }
                        true
                    }
                    Key.DirectionUp, Key.DirectionDown -> { if (scrub != null) { scrub = null; poke(); true } else { poke(); true } }
                    Key.Back, Key.Escape -> if (scrub != null) { scrub = null; true } else false
                    else -> false
                }
            }
            .focusable()
            .clickable(remember { MutableInteractionSource() }, indication = null) { if (controls) controls = false else poke() },
    ) {
        // Subtitle appearance (PLAY-20) follows the person to every app.
        val me by marquee.me.collectAsState()
        val subStyle = me?.preferences?.subtitleStyle
        AndroidView({
            PlayerView(it).apply {
                this.player = player
                useController = false
                keepScreenOn = true
                resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            }
        }, Modifier.fillMaxSize(), update = { v -> v.subtitleView?.let { applySubtitleStyle(it, subStyle) } })
        if ((buffering && error == null) || (session == null && local == null && error == null)) CircularProgressIndicator(Modifier.align(Alignment.Center))
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error) }

        // Scrubbing with the controls hidden (TV): just the preview and the bar.
        if (!controls && scrub != null) Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 48.dp, vertical = 32.dp)) {
            ScrubBar(scrub!!, duration, thumbs, onChange = { scrub = it }, onDone = ::commitScrub)
        }

        if (casting) CastingPanel(castDevice ?: "", detail?.title ?: "", castError, onClose = { nav.popBackStack() })
        AnimatedVisibility(controls && !casting, Modifier.fillMaxSize(), enter = fadeIn(), exit = fadeOut()) {
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color.Black.copy(alpha = 0.7f), 0.3f to Color.Transparent, 0.65f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.8f)))) {
                // Top: back, title, settings.
                Row(Modifier.align(Alignment.TopStart).fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton({ nav.popBackStack() }, Modifier.focusRing()) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Close player", tint = Color.White) }
                    Column(Modifier.weight(1f).padding(start = 8.dp)) {
                        val d = detail
                        if (d?.type == ItemType.EPISODE) Text(d.grandparentTitle ?: "", color = Color.White.copy(alpha = 0.75f), style = MaterialTheme.typography.labelLarge)
                        Text(d?.let { if (it.type == ItemType.EPISODE) listOfNotNull(it.parentTitle, it.index?.let { i -> "E$i" }, it.title).joinToString(" · ") else it.title } ?: "",
                            color = Color.White, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    if (local == null && roll < 0) IconButton({ if (group == null) together.start() else groupPanel = true; poke() }, Modifier.focusRing()) {
                        val g = group
                        if (g == null) Icon(Icons.Filled.Groups, "Watch together", tint = Color.White)
                        else Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Filled.Groups, "Watching together", tint = Gold)
                            Text("${g.members.size}", color = Gold, style = MaterialTheme.typography.labelSmall)
                        }
                    }
                    if (local == null && roll < 0) CastButton()
                    // Play on another Marquee app (USER-14): hands over at this position, then pauses here.
                    if (local == null && roll < 0) PlayOnButton(nav, handoff = {
                        RemoteCommand(RemoteCommand.Type.PLAY, itemIds = listOf(itemId), startMs = player.currentPosition)
                    }, onSent = { player.pause() })
                    if (local == null && roll < 0) IconButton({ settings = true; poke() }, Modifier.focusRing()) { Icon(Icons.Filled.Settings, "Playback settings", tint = Color.White) }
                }
                // Middle: back 10, play/pause, forward 30.
                Row(Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(36.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton({ player.seekBack(); poke() }, Modifier.focusRing().size(56.dp)) { Icon(Icons.Filled.Replay10, "Back 10 seconds", Modifier.size(36.dp), tint = Color.White) }
                    IconButton({ if (player.isPlaying) player.pause() else player.play(); poke() }, Modifier.focusRing().size(76.dp).initialFocus(marquee.isTv)) {
                        Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size(56.dp), tint = Color.White)
                    }
                    IconButton({ player.seekForward(); poke() }, Modifier.focusRing().size(56.dp)) { Icon(Icons.Filled.Forward30, "Forward 30 seconds", Modifier.size(36.dp), tint = Color.White) }
                }
                // Bottom: the scrubber.
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 24.dp, vertical = 16.dp)) {
                    ScrubBar(scrub ?: position, duration, thumbs, scrubbing = scrub != null, onChange = { scrub = it; poke() }, onDone = ::commitScrub)
                }
            }
        }

        val marker = session?.markers?.firstOrNull { position >= it.startMs && position < it.endMs - 1000 }
        if (marker != null) Button(
            onClick = { player.seekTo(marker.endMs + 500) },
            modifier = Modifier.align(Alignment.BottomEnd).padding(bottom = if (controls) 96.dp else 32.dp, end = 32.dp).focusRing(),
        ) { Text(if (marker.kind == Marker.Kind.CREDITS) "Skip Credits" else "Skip Intro") }

        // The trailer playing, and the way past it (PLAY-18).
        rolls.getOrNull(roll)?.let { r ->
            Row(
                Modifier.align(Alignment.BottomEnd).padding(bottom = if (controls) 96.dp else 32.dp, end = 32.dp)
                    .background(Color.Black.copy(alpha = 0.6f), androidx.compose.foundation.shape.RoundedCornerShape(12.dp)).padding(start = 16.dp, end = 8.dp, top = 6.dp, bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("Trailer · ${r.parentTitle ?: r.title}", color = Color.White, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.widthIn(max = 280.dp))
                TextButton({ nextRoll() }, Modifier.focusRing()) { Text("Skip", color = Gold) }
                Button({ startFeature() }, Modifier.focusRing()) { Text("Skip all") }
            }
        }

        groupError?.let { Text(it, Modifier.align(Alignment.TopCenter).padding(top = 72.dp), color = MaterialTheme.colorScheme.error) }
        val g = group
        if (groupPanel && g != null) androidx.compose.material3.AlertDialog(
            onDismissRequest = { groupPanel = false },
            title = { Text("Watching together") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("Play, pause and seeking are shared. Others join from Home.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    g.members.forEach { m -> Text("• ${m.name}${if (m.buffering) " (loading)" else ""}") }
                    if (g.lastBy != null && g.lastAction != null) Text("${g.lastBy}: ${g.lastAction}", style = MaterialTheme.typography.bodySmall)
                }
            },
            confirmButton = { TextButton({ together.leave(); groupPanel = false }, Modifier.focusRing()) { Text("Leave the group") } },
            dismissButton = { TextButton({ groupPanel = false }, Modifier.focusRing()) { Text("Close") } },
        )
        if (settings) PlayerSettings(
            detail, session, choice,
            together = group != null, speed = speed,
            onSpeed = { s ->
                settings = false
                speed = s
                player.setPlaybackSpeed(s)
                poke()
            },
            onTiming = { sub, audio ->
                // Both are sent once either changes; the restart waits for the stepping to stop.
                choice = choice.copy(subOffset = sub, audioOffset = audio)
                timingChange++
            },
            onPick = { c ->
                settings = false
                choice = c
                val at = player.currentPosition
                scope.launch { start(at, c) }
            },
            onClose = { settings = false; poke() },
            onFind = { settings = false; finding = true },
        )
        if (finding) FindSubtitles(itemId, bazarr = detail?.type == ItemType.MOVIE || detail?.type == ItemType.EPISODE, onClose = { finding = false; poke() },
            onQueued = {
                // Bazarr fetches it in the background (META-12): pick up the new track when it lands.
                scope.launch {
                    for (wait in listOf(15_000L, 15_000L, 30_000L)) {
                        delay(wait)
                        withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(itemId) }.getOrNull() }?.let { detail = it }
                    }
                }
            }) { streamId ->
            finding = false
            val c = choice.copy(subtitle = streamId)
            choice = c
            val at = player.currentPosition
            scope.launch {
                detail = withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(itemId) }.getOrNull() } ?: detail
                start(at, c)
            }
        }
    }
}

/** Times and a slider; while scrubbing, the preview frame floats above the thumb. */
@Composable
private fun ScrubBar(pos: Long, dur: Long, thumbs: TrickplayThumbs?, scrubbing: Boolean = true, onChange: (Long) -> Unit, onDone: () -> Unit) {
    val frac = if (dur > 0) pos.toFloat() / dur else 0f
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        if (scrubbing && thumbs != null && dur > 0) {
            val thumbW = 200.dp
            val x = (maxWidth * frac - thumbW / 2).coerceIn(0.dp, maxWidth - thumbW)
            PreviewThumb(thumbs, pos, Modifier.offset(x = x, y = (-150).dp).width(thumbW))
        }
        Column {
            Slider(
                value = frac, onValueChange = { onChange((it * dur).toLong()) }, onValueChangeFinished = onDone,
                colors = SliderDefaults.colors(thumbColor = Gold, activeTrackColor = Gold, inactiveTrackColor = Color.White.copy(alpha = 0.3f)),
                modifier = Modifier.fillMaxWidth().focusRing().semantics { contentDescription = "Position" },
            )
            Row(Modifier.fillMaxWidth()) {
                Text(formatTime(pos), color = Color.White, style = MaterialTheme.typography.labelMedium)
                Spacer(Modifier.weight(1f))
                Text("-" + formatTime((dur - pos).coerceAtLeast(0)), color = Color.White, style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}

/** Audio, subtitles, quality and version, applied by restarting the session where it is. */
@Composable
private fun PlayerSettings(
    detail: ItemDetail?, session: PlaybackSession?, choice: Choice, together: Boolean, speed: Float,
    onSpeed: (Float) -> Unit, onTiming: (sub: Int, audio: Int) -> Unit,
    onPick: (Choice) -> Unit, onClose: () -> Unit, onFind: () -> Unit = {},
) {
    val version = detail?.versions?.firstOrNull { v -> v.files.any { it.id == (choice.fileId ?: session?.fileId) } } ?: detail?.versions?.firstOrNull()
    val file = version?.files?.firstOrNull { it.id == (choice.fileId ?: session?.fileId) } ?: version?.files?.firstOrNull()
    val streams = file?.streams.orEmpty()
    val audio = streams.filter { it.kind == MediaStream.Kind.AUDIO }
    val subs = streams.filter { it.kind == MediaStream.Kind.SUBTITLE }
    val curAudio = choice.audio ?: session?.audioStreamId
    val curSub = if (choice.subtitle == -1L) null else choice.subtitle ?: session?.subtitleStreamId
    val subOffset = choice.subOffset ?: session?.subtitleOffsetMs ?: 0
    val audioOffset = choice.audioOffset ?: session?.audioOffsetMs ?: 0
    Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.6f)).clickable(remember { MutableInteractionSource() }, null, onClick = onClose)) {
        LazyColumn(
            Modifier.align(Alignment.CenterEnd).width(340.dp).fillMaxSize().background(MaterialTheme.colorScheme.surface).padding(vertical = 16.dp)
                .clickable(remember { MutableInteractionSource() }, null) {},
        ) {
            item { Text("Playback", Modifier.padding(16.dp), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold) }
            if ((detail?.versions?.size ?: 0) > 1) {
                item { Heading("Version") }
                items(detail!!.versions) { v ->
                    Option(versionLabel(v), v.id == version?.id, first = false) { onPick(choice.copy(fileId = v.files.first().id, audio = null, subtitle = null)) }
                }
            }
            if (audio.size > 1) {
                item { Heading("Audio") }
                items(audio) { a -> Option(streamLabel(a), a.id == curAudio) { onPick(choice.copy(audio = a.id)) } }
            }
            item { Heading("Subtitles") }
            item { Option("Off", curSub == null, first = true) { onPick(choice.copy(subtitle = -1)) } }
            items(subs) { s -> Option(streamLabel(s), s.id == curSub) { onPick(choice.copy(subtitle = s.id)) } }
            item { Option("Find subtitles…", false, onClick = onFind) }
            // Speed and timing are each viewer's own, so not while watching together.
            if (!together) {
                item { Heading("Speed") }
                items(speeds) { s -> Option(speedLabel(s), s == speed) { onSpeed(s) } }
                item { Heading("Subtitle timing") }
                item { Timing("Subtitle timing", subOffset) { onTiming(it, audioOffset) } }
                if (file == null || streams.any { it.kind == MediaStream.Kind.VIDEO }) {
                    item { Heading("Audio timing") }
                    item { Timing("Audio timing", audioOffset) { onTiming(subOffset, it) } }
                }
            }
            item { Heading("Quality") }
            items(qualities) { (label, kbps) -> Option(label, choice.maxKbps == kbps) { onPick(choice.copy(maxKbps = kbps)) } }
            session?.let { s ->
                item {
                    Text(
                        listOfNotNull(s.decision.method.value.replace('_', ' ').replaceFirstChar { it.uppercase() }, s.limitReason).joinToString(" · "),
                        Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            item { TextButton(onClose, Modifier.padding(horizontal = 8.dp).focusRing()) { Text("Close") } }
        }
    }
}

/** A timing offset: earlier and later by 100 ms, the value, and Reset. Positive = later. */
@Composable
private fun Timing(label: String, ms: Int, onChange: (Int) -> Unit) {
    fun set(v: Int) = onChange(v.coerceIn(-maxOffset, maxOffset))
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton({ set(ms - offsetStep) }, Modifier.focusRing()) { Icon(Icons.Filled.Remove, "$label earlier") }
        Text(if (ms == 0) "0 ms" else "%+d ms".format(ms), Modifier.weight(1f).semantics { contentDescription = "$label value" },
            textAlign = androidx.compose.ui.text.style.TextAlign.Center, fontWeight = FontWeight.SemiBold)
        IconButton({ set(ms + offsetStep) }, Modifier.focusRing()) { Icon(Icons.Filled.Add, "$label later") }
        TextButton({ set(0) }, Modifier.focusRing(), enabled = ms != 0) { Text("Reset") }
    }
}

@Composable
private fun Heading(t: String) = Text(t.uppercase(), Modifier.padding(start = 16.dp, top = 16.dp, bottom = 4.dp), style = MaterialTheme.typography.labelMedium, color = Gold)

@Composable
private fun Option(label: String, selected: Boolean, first: Boolean = false, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().initialFocus(first).focusCard(onClick).semantics { this.selected = selected }.padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, Modifier.weight(1f), fontWeight = if (selected) FontWeight.Bold else FontWeight.Normal)
        if (selected) Icon(Icons.Filled.Check, null, tint = Gold)
    }
}

/** A version's name, or what it is when it has none ("4K · HEVC · HDR10"). */
private fun versionLabel(v: app.marquee.api.models.MediaVersion): String {
    if (v.label.isNotBlank()) return v.label
    val f = v.files.firstOrNull() ?: return "Version ${v.id}"
    val res = f.height?.let { h -> when { h >= 2000 -> "4K"; h >= 1000 -> "1080p"; h >= 700 -> "720p"; else -> "${h}p" } }
    return listOfNotNull(res, f.videoCodec?.uppercase(), f.hdrFormat?.value?.uppercase()).joinToString(" · ").ifBlank { "Version ${v.id}" }
}

private fun streamLabel(s: MediaStream): String {
    val lang = s.language?.let { java.util.Locale.forLanguageTag(it).displayLanguage.takeIf { d -> d.isNotBlank() && d != it } ?: it.uppercase() }
    val bits = listOfNotNull(
        lang, s.title?.takeIf { it.isNotBlank() && it != lang },
        s.codec.uppercase().takeIf { s.kind == MediaStream.Kind.AUDIO }, s.channelLayout ?: s.channels?.let { "${it}ch" },
        "Forced".takeIf { s.forced }, "SDH".takeIf { s.hearingImpaired },
    )
    return bits.joinToString(" · ").ifBlank { "Track ${s.id}" }
}

/** Find subtitles on OpenSubtitles (PLAY-7): hash matches first; picking one downloads it. */
@Composable
private fun FindSubtitles(itemId: Long, bazarr: Boolean, onClose: () -> Unit, onQueued: () -> Unit, onDownloaded: (Long) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val lang = remember { java.util.Locale.getDefault().language.ifBlank { "en" } }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val results by androidx.compose.runtime.produceState<List<app.marquee.api.models.SubtitleResult>?>(null) {
        value = withContext(Dispatchers.IO) {
            runCatching { marquee.playback.searchSubtitles(itemId, if (lang == "en") "en" else "$lang,en") }.onFailure { error = it.message }.getOrDefault(emptyList())
        }
    }
    Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.6f)).clickable(remember { MutableInteractionSource() }, null, onClick = onClose)) {
        LazyColumn(
            Modifier.align(Alignment.CenterEnd).width(380.dp).fillMaxSize().background(MaterialTheme.colorScheme.surface).padding(vertical = 16.dp)
                .semantics { contentDescription = "Find subtitles panel" }
                .clickable(remember { MutableInteractionSource() }, null) {},
        ) {
            item { Text("Find subtitles", Modifier.padding(16.dp), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold) }
            // Bazarr first, when it manages this title (META-12).
            if (bazarr) item { BazarrSection(itemId, Modifier.padding(horizontal = 16.dp), onQueued = onQueued, heading = { Heading(it) }) }
            if (bazarr) item { Heading("OpenSubtitles") }
            error?.let { e -> item { Text(e, Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.error) } }
            val list = results
            if (list == null) item { CircularProgressIndicator(Modifier.padding(16.dp)) }
            else if (list.isEmpty() && error == null) item { Text("Nothing found.", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            else items(list) { r ->
                Column(Modifier.fillMaxWidth().focusCard({
                    if (busy) return@focusCard
                    busy = true
                    scope.launch {
                        withContext(Dispatchers.IO) {
                            runCatching { marquee.playback.downloadSubtitle(itemId, app.marquee.api.models.DownloadSubtitleRequest(r.fileId, r.language, r.release, r.hearingImpaired)).streamId }
                        }.onSuccess(onDownloaded).onFailure { error = it.message; busy = false }
                    }
                }).padding(horizontal = 16.dp, vertical = 10.dp)) {
                    Text(r.release, maxLines = 2, fontWeight = if (r.hashMatch) FontWeight.Bold else FontWeight.Normal)
                    Text(listOfNotNull(r.language.uppercase(), "Exact match".takeIf { r.hashMatch }, "SDH".takeIf { r.hearingImpaired },
                        "Machine translated".takeIf { r.aiTranslated }, "${r.downloads} downloads").joinToString(" · "),
                        style = MaterialTheme.typography.bodySmall, color = if (r.hashMatch) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            item { TextButton(onClose, Modifier.padding(horizontal = 8.dp).focusRing()) { Text("Close") } }
        }
    }
}

/** Shown over the video while it plays on a Cast device: what's casting and its controls. */
@Composable
private fun CastingPanel(device: String, title: String, error: String?, onClose: () -> Unit) {
    val cast = LocalMarquee.current.cast
    val playing by cast.playing.collectAsState()
    val pos by cast.position.collectAsState()
    var drag by remember { mutableStateOf<Float?>(null) }
    Box(Modifier.fillMaxSize().background(Color.Black).clickable(remember { MutableInteractionSource() }, indication = null) {}) {
        Row(Modifier.align(Alignment.TopStart).fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClose, Modifier.focusRing()) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Close player", tint = Color.White) }
            Spacer(Modifier.weight(1f))
            CastButton()
        }
        Column(Modifier.align(Alignment.Center).fillMaxWidth().padding(horizontal = 48.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Icon(Icons.Filled.CastConnected, null, Modifier.size(48.dp), tint = Gold)
            Text("Playing on $device", Modifier.padding(top = 12.dp), color = Color.White.copy(alpha = 0.75f))
            Text(title, Modifier.padding(top = 4.dp), color = Color.White, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold,
                maxLines = 2, overflow = TextOverflow.Ellipsis)
            error?.let { Text("Couldn't play it there: $it", Modifier.padding(top = 12.dp), color = MaterialTheme.colorScheme.error) }
            val (p, d) = pos
            if (d > 0) {
                Slider(drag ?: (p.toFloat() / d), { drag = it }, Modifier.padding(top = 24.dp).fillMaxWidth(),
                    onValueChangeFinished = { drag?.let { cast.seek((it * d).toLong()) }; drag = null })
                Row(Modifier.fillMaxWidth()) {
                    Text(formatTime(p), color = Color.White.copy(alpha = 0.75f), style = MaterialTheme.typography.labelMedium)
                    Spacer(Modifier.weight(1f))
                    Text(formatTime(d), color = Color.White.copy(alpha = 0.75f), style = MaterialTheme.typography.labelMedium)
                }
            }
            Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton({ cast.seek((p - 10_000).coerceAtLeast(0)) }, Modifier.focusRing().size(56.dp)) { Icon(Icons.Filled.Replay10, "Back 10 seconds", Modifier.size(36.dp), tint = Color.White) }
                IconButton({ cast.toggle() }, Modifier.focusRing().size(76.dp)) {
                    Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size(56.dp), tint = Color.White)
                }
                IconButton({ cast.seek(p + 30_000) }, Modifier.focusRing().size(56.dp)) { Icon(Icons.Filled.Forward30, "Forward 30 seconds", Modifier.size(36.dp), tint = Color.White) }
            }
        }
    }
}

/**
 * An ExoPlayer for video: it takes audio focus (so music pauses) and pauses when headphones
 * are unplugged.
 */
@OptIn(UnstableApi::class)
internal fun videoPlayer(context: android.content.Context, seekIncrements: Boolean = false, audioFocus: Boolean = true): ExoPlayer =
    ExoPlayer.Builder(context)
        .apply { if (seekIncrements) setSeekBackIncrementMs(10_000).setSeekForwardIncrementMs(30_000) }
        .setAudioAttributes(videoAudio, audioFocus)
        .setHandleAudioBecomingNoisy(true)
        .build()

internal val videoAudio: AudioAttributes = AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MOVIE).build()

/**
 * While a video screen is up: keeps the screen on and, on phones, goes full screen in
 * landscape; a Cast device connected meanwhile is for the video, not music. Counted on
 * Marquee, because when the next episode replaces a player the outgoing screen closes after
 * the new one opened, and mustn't undo it.
 */
@Composable
internal fun VideoWindow() {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    DisposableEffect(Unit) {
        val activity = context as? Activity
        val window = activity?.window
        val bars = window?.let { WindowCompat.getInsetsController(it, it.decorView) }
        marquee.videoScreens++
        marquee.cast.videoActive = true
        window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        // Phones: full screen in landscape, like any video app.
        if (!marquee.isTv) {
            activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
            bars?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            bars?.hide(WindowInsetsCompat.Type.systemBars())
        }
        onDispose {
            marquee.videoScreens = (marquee.videoScreens - 1).coerceAtLeast(0)
            if (marquee.videoScreens > 0) return@onDispose
            marquee.cast.videoActive = false
            window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            if (!marquee.isTv) {
                activity?.requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                bars?.show(WindowInsetsCompat.Type.systemBars())
            }
        }
    }
}
