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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
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
import app.marquee.api.models.ItemType
import app.marquee.api.models.Marker
import app.marquee.api.models.MediaStream
import app.marquee.api.models.PlaybackProgress
import app.marquee.api.models.PlaybackRequest
import app.marquee.api.models.PlaybackSession
import app.marquee.core.AndroidProfile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** What the viewer picked in the player's settings; null means the server's choice. */
private data class Choice(val fileId: Long? = null, val audio: Long? = null, val subtitle: Long? = null, val maxKbps: Int? = null)

/** Quality caps offered in the player (kbps; null = no cap). */
private val qualities = listOf<Pair<String, Int?>>(
    "Original" to null, "20 Mbps 1080p" to 20_000, "12 Mbps 1080p" to 12_000, "8 Mbps 1080p" to 8_000,
    "4 Mbps 720p" to 4_000, "2 Mbps 720p" to 2_000, "1 Mbps 480p" to 1_000,
)

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
    val player = remember { ExoPlayer.Builder(context).setSeekBackIncrementMs(10_000).setSeekForwardIncrementMs(30_000).build() }
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
    // Watch together (SYNC-1).
    val together = remember { WatchTogether(marquee, player, itemId, scope) }
    val group by together.group.collectAsState()
    val groupError by together.error.collectAsState()
    var groupPanel by remember { mutableStateOf(false) }
    var finding by remember { mutableStateOf(false) }
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

    /** Starts (or restarts, after a settings change) a server session at a position. */
    suspend fun start(at: Long?, c: Choice) = withContext(Dispatchers.Main) {
        val old = session
        val req = PlaybackRequest(itemId, AndroidProfile.profile, fileId = c.fileId, audioStreamId = c.audio, subtitleStreamId = c.subtitle,
            startMs = at, maxBitrateKbps = c.maxKbps)
        runCatching { withContext(Dispatchers.IO) { marquee.playback.startPlayback(req) } }
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
        launch(Dispatchers.IO) { detail = runCatching { marquee.items.getItem(itemId) }.getOrNull() }
        launch { thumbs = TrickplayThumbs.load(marquee, itemId) }
        if (local != null) withContext(Dispatchers.Main) {
            player.setMediaItem(MediaItem.fromUri(Uri.fromFile(local)))
            player.prepare()
            val begin = startMs ?: downloads.resumePosition(itemId)
            if (begin > 0) player.seekTo(begin)
            player.playWhenReady = true
        } else start(startMs, choice)
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
                if (!isPlaying) recordLocal()
                report(if (isPlaying) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)
            }
            override fun onPlaybackStateChanged(state: Int) {
                buffering = state == Player.STATE_BUFFERING || state == Player.STATE_IDLE
                if (state != Player.STATE_ENDED) return
                if (local != null) { recordLocal(ended = true); nav.popBackStack(); return }
                report(PlaybackProgress.State.PAUSED)
                // Up next: the following episode (or nothing).
                marquee.scope.launch(Dispatchers.Main) {
                    val next = withContext(Dispatchers.IO) { runCatching { marquee.items.nextItem(itemId) }.getOrNull() }
                    if (next != null) nav.navigate("player/${next.id}?start=0") { popUpTo("player/{id}?start={start}&group={group}") { inclusive = true } }
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
    var casting by remember { mutableStateOf(false) }
    LaunchedEffect(castDevice) {
        if (castDevice != null && local == null && !casting) {
            casting = true
            val at = player.currentPosition
            player.pause()
            closeSession(session, at)
            session = null
            val d = detail
            val title = d?.let { if (it.type == ItemType.EPISODE) "${it.grandparentTitle ?: ""} · ${it.title}" else it.title } ?: ""
            marquee.cast.onFinished = { nav.popBackStack() }
            marquee.cast.load(itemId, at, title, d?.year?.toString(), marquee.imageUrl(d?.images?.backdrop ?: d?.images?.poster, 640), music = false,
                fileId = choice.fileId, audioStreamId = choice.audio, subtitleStreamId = choice.subtitle)?.let { error = it }
        } else if (castDevice == null && casting) {
            casting = false
            marquee.cast.onFinished = null
            start(marquee.cast.position.value.first, choice)
        }
    }
    DisposableEffect(Unit) {
        marquee.cast.videoActive = true
        onDispose { marquee.cast.videoActive = false; if (casting) marquee.cast.onFinished = null }
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
        AndroidView({
            PlayerView(it).apply {
                this.player = player
                useController = false
                keepScreenOn = true
                resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            }
        }, Modifier.fillMaxSize())
        if ((buffering && error == null) || (session == null && local == null && error == null)) CircularProgressIndicator(Modifier.align(Alignment.Center))
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error) }

        // Scrubbing with the controls hidden (TV): just the preview and the bar.
        if (!controls && scrub != null) Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 48.dp, vertical = 32.dp)) {
            ScrubBar(scrub!!, duration, thumbs, onChange = { scrub = it }, onDone = ::commitScrub)
        }

        if (casting) CastingPanel(castDevice ?: "", detail?.title ?: "", onClose = { nav.popBackStack() })
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
                    if (local == null) IconButton({ if (group == null) together.start() else groupPanel = true; poke() }, Modifier.focusRing()) {
                        val g = group
                        if (g == null) Icon(Icons.Filled.Groups, "Watch together", tint = Color.White)
                        else Row(verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Filled.Groups, "Watching together", tint = Gold)
                            Text("${g.members.size}", color = Gold, style = MaterialTheme.typography.labelSmall)
                        }
                    }
                    if (local == null) CastButton()
                    if (local == null) IconButton({ settings = true; poke() }, Modifier.focusRing()) { Icon(Icons.Filled.Settings, "Playback settings", tint = Color.White) }
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
            onPick = { c ->
                settings = false
                choice = c
                val at = player.currentPosition
                scope.launch { start(at, c) }
            },
            onClose = { settings = false; poke() },
            onFind = { settings = false; finding = true },
        )
        if (finding) FindSubtitles(itemId, onClose = { finding = false; poke() }) { streamId ->
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
private fun PlayerSettings(detail: ItemDetail?, session: PlaybackSession?, choice: Choice, onPick: (Choice) -> Unit, onClose: () -> Unit, onFind: () -> Unit = {}) {
    val version = detail?.versions?.firstOrNull { v -> v.files.any { it.id == (choice.fileId ?: session?.fileId) } } ?: detail?.versions?.firstOrNull()
    val file = version?.files?.firstOrNull { it.id == (choice.fileId ?: session?.fileId) } ?: version?.files?.firstOrNull()
    val streams = file?.streams.orEmpty()
    val audio = streams.filter { it.kind == MediaStream.Kind.AUDIO }
    val subs = streams.filter { it.kind == MediaStream.Kind.SUBTITLE }
    val curAudio = choice.audio ?: session?.audioStreamId
    val curSub = if (choice.subtitle == -1L) null else choice.subtitle ?: session?.subtitleStreamId
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
private fun FindSubtitles(itemId: Long, onClose: () -> Unit, onDownloaded: (Long) -> Unit) {
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
                .clickable(remember { MutableInteractionSource() }, null) {},
        ) {
            item { Text("Find subtitles", Modifier.padding(16.dp), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold) }
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
private fun CastingPanel(device: String, title: String, onClose: () -> Unit) {
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
