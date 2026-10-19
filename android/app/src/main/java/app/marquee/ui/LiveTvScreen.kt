package app.marquee.ui

import android.widget.Toast
import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.ChevronLeft
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.util.VelocityTracker
import kotlin.math.abs
import androidx.compose.material.icons.filled.VolumeOff
import androidx.compose.material.icons.filled.VolumeUp
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.datasource.HttpDataSource
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import app.marquee.api.models.LiveChannel
import app.marquee.api.models.LiveProgramme
import app.marquee.api.models.PlayLiveChannelRequest
import app.marquee.core.AndroidProfile
import coil3.compose.AsyncImage
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.Duration
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit

private val clock = DateTimeFormatter.ofPattern("h:mm a")
private fun OffsetDateTime.local() = atZoneSameInstant(ZoneId.systemDefault())
private fun LiveProgramme.minutesLeft() = Duration.between(OffsetDateTime.now(), end).toMinutes().coerceAtLeast(0)
private fun LiveProgramme.isOn(): Boolean { val n = OffsetDateTime.now(); return !start.isAfter(n) && end.isAfter(n) }
private fun LiveProgramme.progress(): Float {
    val total = Duration.between(start, end).seconds.toFloat()
    return if (total > 0) (Duration.between(start, OffsetDateTime.now()).seconds / total).coerceIn(0f, 1f) else 0f
}

private fun defaultStart(): OffsetDateTime {
    val n = OffsetDateTime.now().truncatedTo(ChronoUnit.MINUTES)
    return n.withMinute(if (n.minute < 30) 0 else 30).minusMinutes(30)
}

/** Live TV (LIVE-2): Guide and What's On, a live preview on phones, full screen on TV. VOD is a page of its own. */
@Composable
fun LiveTvScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    // Null while loading; a failure counts as Live TV being off.
    val statusResult by produceState<Result<app.marquee.api.models.LiveTvStatus>?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.livetv.liveTvStatus() } } }
    val status = statusResult?.getOrNull()
    val groups by produceState(emptyList<String>()) { value = withContext(Dispatchers.IO) { runCatching { marquee.livetv.listLiveGroups().map { it.name } }.getOrDefault(emptyList()) } }
    LaunchedEffect(Unit) { marquee.refreshLiveTv() }
    // Saved, so coming back from a channel keeps the tab.
    var tab by rememberSaveable { mutableStateOf("guide") }
    var filter by remember { mutableStateOf("all") }
    var start by remember { mutableStateOf(defaultStart()) }
    var channels by remember { mutableStateOf<List<LiveChannel>?>(null) }
    var guide by remember { mutableStateOf<Map<Long, List<LiveProgramme>>>(emptyMap()) }
    var preview by remember { mutableStateOf<Long?>(null) }
    var details by remember { mutableStateOf<Pair<LiveProgramme, LiveChannel>?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    // Bumped when a recording is stopped, so the strip, the Recordings list and channel availability catch up.
    var recReload by remember { mutableIntStateOf(0) }
    // Recordings starting or ending change which channels can be tuned: reload them when the count changes.
    var recordingCount by remember { mutableStateOf<Int?>(null) }
    var availability by remember { mutableIntStateOf(0) }
    val context = LocalContext.current

    LaunchedEffect(filter, start, reload, recReload, availability) {
        val group = filter.takeUnless { it in setOf("all", "favorites", "recent", "hidden") }
        val fav = if (filter == "favorites") true else null
        val recent = if (filter == "recent") true else null
        val hidden = if (filter == "hidden") true else null
        withContext(Dispatchers.IO) {
            channels = runCatching { marquee.livetv.listLiveChannels(group, fav, recent, hidden) }.getOrDefault(emptyList())
            guide = if (hidden == true) emptyMap()
            else runCatching { marquee.livetv.liveGuide(start, start.plusHours(4), group, fav, recent).associate { it.channelId to it.programmes } }.getOrDefault(emptyMap())
        }
    }
    fun watch(id: Long) = nav.navigate("live/$id")
    // Every connection of its source in use (LIVE-3): it can't be tuned now, so say why instead.
    fun unavailable(c: LiveChannel) = Toast.makeText(context, c.unavailableReason ?: "${c.name} isn't available right now.", Toast.LENGTH_LONG).show()
    fun pick(c: LiveChannel) {
        if (c.available == false) unavailable(c)
        else if (marquee.isTv || preview == c.id) watch(c.id) else preview = c.id
    }

    val s = status
    if (statusResult == null) { Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }; return }
    val liveOn = marquee.fakeLiveTv ?: (s?.enabled == true) // the test hook stands in for the server's status
    val tabs = buildList {
        if (liveOn) {
            add("guide" to "Guide")
            add("now" to "What's On")
            // Older servers don't send canViewRecordings: recording implied seeing recordings.
            if ((s?.canViewRecordings ?: s?.canRecord) == true) add("recordings" to "Recordings")
        }
    }
    if (tabs.isEmpty()) {
        Column(Modifier.fillMaxWidth().padding(48.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Live TV isn't set up", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            Text("An admin can add Dispatcharr or an M3U playlist in the web app's Settings → Live TV.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        return
    }
    val current = tab.takeIf { t -> tabs.any { it.first == t } } ?: tabs.first().first
    val header: @Composable () -> Unit = {
        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Live TV", Modifier.padding(horizontal = sidePadding), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            Row(Modifier.padding(horizontal = sidePadding), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                tabs.forEachIndexed { i, (key, label) ->
                    FilterChip(current == key, { tab = key }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(marquee.isTv && i == 0))
                }
            }
        }
    }
    if (s == null) return
    val list = channels
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { header() }
        if ((s.canViewRecordings ?: s.canRecord) == true) item(key = "recording-now") { RecordingNowStrip(recReload, onActive = { n ->
            if (recordingCount != null && recordingCount != n) availability++
            recordingCount = n
        }) { recReload++ } }
        if (current == "recordings") {
            item { RecordingsList(nav, canRecord = s.canRecord == true, reloadKey = recReload) { recReload++ } }
            return@LazyColumn
        }
        item {
            LazyRow(contentPadding = PaddingValues(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                val options = listOf("all" to "All Channels", "favorites" to "Favorites", "recent" to "Recently watched", "hidden" to "Hidden") + groups.map { it to it }
                items(options) { (v, label) -> FilterChip(filter == v, { filter = v }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp))) }
            }
        }
        if (!marquee.isTv) list?.let { l -> (l.firstOrNull { it.id == preview } ?: l.firstOrNull { it.available != false } ?: l.firstOrNull())?.let { c ->
            item(key = "preview") { LivePreview(c) { watch(c.id) } }
        } }
        if (list == null) item { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        else if (list.isEmpty()) item {
            Text(when (filter) {
                "favorites" -> "No favourites yet. Add some with the heart next to a channel."
                "recent" -> "Channels you watch will appear here."
                "hidden" -> "No hidden channels. Hide one from a programme's details in the guide."
                else -> "No channels."
            },
                Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else if (current == "guide") {
            item {
                Row(Modifier.padding(horizontal = sidePadding), verticalAlignment = Alignment.CenterVertically) {
                    Text(start.local().format(DateTimeFormatter.ofPattern("EEE h:mm a")), Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    IconButton({ start = start.minusMinutes(90) }, Modifier.focusRing(), enabled = start.isAfter(OffsetDateTime.now().minusHours(3))) { Icon(Icons.Filled.ChevronLeft, "Earlier") }
                    TextButton({ start = defaultStart() }, Modifier.focusRing()) { Text("Now") }
                    IconButton({ start = start.plusMinutes(90) }, Modifier.focusRing()) { Icon(Icons.Filled.ChevronRight, "Later") }
                }
            }
            item {
                Guide(list, guide, start, preview,
                    onChannel = ::pick,
                    onProgramme = { p, c ->
                        // On now: preview (phone) or watch (TV); the previewed channel's programme
                        // or a later one shows its details, with Record.
                        // An unavailable channel's programme shows its details, which say why.
                        val previewed = preview ?: list.firstOrNull { it.available != false }?.id
                        if (p.isOn() && c.available != false && (marquee.isTv || previewed != c.id)) pick(c) else details = p to c
                    },
                    onFavorite = { c ->
                        scope.launch {
                            withContext(Dispatchers.IO) {
                                runCatching {
                                    when {
                                        c.hidden == true -> marquee.livetv.unhideLiveChannel(c.id) // the hidden view's button shows it again
                                        c.favorite -> marquee.livetv.unfavoriteLiveChannel(c.id)
                                        else -> marquee.livetv.favoriteLiveChannel(c.id)
                                    }
                                }
                            }
                            reload++
                        }
                    })
            }
        } else {
            items(list, key = { it.id }) { c -> WhatsOnRow(c) { if (c.available == false) unavailable(c) else watch(c.id) } }
        }
    }
    details?.let { (p, c) ->
        AlertDialog(
            onDismissRequest = { details = null },
            title = { Text(p.title) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("${c.name} · ${p.start.local().format(DateTimeFormatter.ofPattern("EEE h:mm a"))}–${p.end.local().format(clock)}", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    listOfNotNull(p.episode, p.subtitle).takeIf { it.isNotEmpty() }?.let { Text(it.joinToString(" · ")) }
                    p.description?.let { Text(it) }
                    if (c.available == false) Text(c.unavailableReason ?: "This channel isn't available right now.", color = MaterialTheme.colorScheme.error)
                    if (s.canRecord == true && p.end.isAfter(OffsetDateTime.now())) RecordActions(p, c) { details = null; reload++; recReload++ }
                    TextButton({
                        scope.launch {
                            withContext(Dispatchers.IO) { runCatching { marquee.livetv.hideLiveChannel(c.id) } }
                            details = null
                            reload++
                        }
                    }, Modifier.focusRing()) { Text("Hide ${c.name} from the guide", color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            },
            confirmButton = { TextButton({ details = null }, Modifier.focusRing()) { Text("Done") } },
        )
    }
}

@Composable
fun ChannelLogo(c: LiveChannel, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    Box(modifier.clip(RoundedCornerShape(4.dp)), contentAlignment = Alignment.Center) {
        Text(c.name.split(" ").mapNotNull { it.firstOrNull() }.take(3).joinToString(""), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        c.logoUrl?.let { AsyncImage(marquee.absolute(it), null, contentScale = ContentScale.Fit, modifier = Modifier.matchParentSize().background(Surface2)) }
    }
}

@Composable
private fun Guide(
    channels: List<LiveChannel>, guide: Map<Long, List<LiveProgramme>>, start: OffsetDateTime, selected: Long?,
    onChannel: (LiveChannel) -> Unit, onProgramme: (LiveProgramme, LiveChannel) -> Unit, onFavorite: (LiveChannel) -> Unit,
) {
    val marquee = LocalMarquee.current
    val perMinute: Dp = if (marquee.isTv) 7.dp else 5.dp
    val row: Dp = if (marquee.isTv) 72.dp else 64.dp
    val column: Dp = if (marquee.isTv) 160.dp else 108.dp
    val width = perMinute * (4 * 60)
    fun x(t: OffsetDateTime) = perMinute * Duration.between(start, t).toMinutes().toFloat()
    Row(Modifier.padding(horizontal = sidePadding)) {
        Column(Modifier.width(column)) {
            Spacer(Modifier.height(28.dp))
            channels.forEach { c ->
                val off = c.available == false
                Row(Modifier.height(row).fillMaxWidth().border(1.dp, if (selected == c.id) Gold else Color.Transparent, RoundedCornerShape(6.dp)),
                    verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f).focusCard({ onChannel(c) }).alpha(if (off) 0.4f else 1f).padding(4.dp)
                        .semantics { contentDescription = if (off) "${c.name}, unavailable: ${c.unavailableReason ?: "not available right now"}" else "Preview ${c.name}" },
                        horizontalAlignment = Alignment.CenterHorizontally) {
                        ChannelLogo(c, Modifier.width(56.dp).height(row * 0.45f))
                        c.number?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                    IconButton({ onFavorite(c) }, Modifier.focusRing()) {
                        if (c.hidden == true) Icon(Icons.Filled.Visibility, "Show ${c.name} in the guide", tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        else Icon(if (c.favorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                            if (c.favorite) "Remove ${c.name} from favourites" else "Add ${c.name} to favourites",
                            tint = if (c.favorite) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        Box(Modifier.horizontalScroll(rememberScrollState())) {
            Column {
                Row(Modifier.height(28.dp)) {
                    for (i in 0 until 8) Text(start.plusMinutes(30L * i).local().format(clock), Modifier.width(perMinute * 30),
                        style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                channels.forEach { c ->
                    Box(Modifier.width(width).height(row).alpha(if (c.available == false) 0.5f else 1f)) {
                        guide[c.id].orEmpty().forEach { p ->
                            val left = maxOf(0.dp, x(p.start))
                            val right = minOf(width, x(p.end))
                            if (right > left) Column(
                                Modifier.offset(x = left + 2.dp).padding(vertical = 4.dp).width(right - left - 4.dp).fillMaxHeight()
                                    .clip(RoundedCornerShape(6.dp)).background(if (p.isOn()) Color.White.copy(alpha = 0.14f) else Color.White.copy(alpha = 0.07f))
                                    .focusCard({ onProgramme(p, c) }, RoundedCornerShape(6.dp)).padding(horizontal = 8.dp, vertical = 6.dp)
                                    .semantics { contentDescription = "${p.title}, ${c.name}" + if (p.recording != null) ", will record" else "" },
                            ) {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    if (p.recording != null) Box(Modifier.padding(end = 4.dp).size(7.dp).clip(CircleShape).background(Color(0xFFE53935)))
                                    Text(p.title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
                                }
                                Text(if (p.isOn()) "${p.minutesLeft()}m left" else p.start.local().format(clock), style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                }
            }
            val now = x(OffsetDateTime.now())
            if (now > 0.dp && now < width) Box(Modifier.offset(x = now).width(2.dp).height(28.dp + row * channels.size).background(Gold))
        }
    }
}

@Composable
private fun WhatsOnRow(c: LiveChannel, onWatch: () -> Unit) {
    val off = c.available == false
    Column(Modifier.padding(horizontal = sidePadding).fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Surface2).focusCard(onWatch, RoundedCornerShape(10.dp))
        .alpha(if (off) 0.5f else 1f)
        .semantics { contentDescription = if (off) "${c.name}, unavailable" else "Watch ${c.name}" }.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            ChannelLogo(c, Modifier.width(64.dp).height(40.dp))
            Column(Modifier.weight(1f)) {
                Text(c.now?.title ?: c.name, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(c.now?.let { "${it.minutesLeft()}m left · ${c.name}" } ?: c.name, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        c.now?.let { LinearProgressIndicator(progress = { it.progress() }, Modifier.fillMaxWidth(), color = Gold) }
        if (off) Text(c.unavailableReason ?: "Not available right now", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
        else c.next?.let { Text("Next: ${it.title} at ${it.start.local().format(clock)}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** Plays a channel in an ExoPlayer, stopping the server stream when it goes away. */
@OptIn(UnstableApi::class)
@Composable
private fun LiveVideo(channelId: Long, muted: Boolean, modifier: Modifier, onState: (String?) -> Unit = {}, onPlaying: (Boolean) -> Unit = {}) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val player = remember(channelId) { videoPlayer(context) }
    val session = remember(channelId) { arrayOfNulls<String>(1) } // read when the player goes away
    val url = remember(channelId) { arrayOfNulls<String>(1) } // the session's playlist, asked why it ended
    // Bumped to tune again after the server ended the stream for a reason that tuning again fixes.
    var attempt by remember(channelId) { mutableIntStateOf(0) }
    // Re-tuned since it last played: once is enough; a second end is shown instead.
    var retuned by remember(channelId) { mutableStateOf(false) }
    LaunchedEffect(muted, player) {
        player.volume = if (muted) 0f else 1f
        // A muted preview leaves music alone; with sound it takes audio focus like any video.
        player.setAudioAttributes(videoAudio, !muted)
    }
    LaunchedEffect(channelId, attempt) {
        onState(null)
        // Tuned on the session's own scope: if this goes away while the server is tuning,
        // the stream is stopped as soon as it arrives rather than left running.
        val pending = marquee.scope.async { runCatching { marquee.livetv.playLiveChannel(channelId, PlayLiveChannelRequest(AndroidProfile.profile)) } }
        val s = try {
            pending.await()
        } catch (e: CancellationException) {
            marquee.scope.launch { pending.await().getOrNull()?.let { runCatching { marquee.livetv.stopLiveSession(it.id) } } }
            throw e
        }
        session[0] = s.getOrNull()?.id
        s.onSuccess {
            url[0] = marquee.absolute(it.url)
            withContext(Dispatchers.Main) {
                player.setMediaItem(MediaItem.fromUri(marquee.absolute(it.url)!!))
                player.prepare()
                player.playWhenReady = true
            }
        }.onFailure {
            // A time limit (kids' profiles) shows full screen, as the other players do.
            if (!muted) app.marquee.core.TimeLimitRules.reason(it)?.let(marquee.timeLimits::block)
            onState(serverMessage(it).ifBlank { "Couldn't tune the channel" }) // 409 no_tuner carries its own message
        }
    }
    DisposableEffect(player) {
        val l = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                if (isPlaying) retuned = false
                onPlaying(isPlaying)
            }
            // The server may end the stream: its playlist then answers 410 with why. A reason starting
            // "Stopped:" is shown; any other (the recording it shared ended, it moved to a recording's
            // stream) means tuning the same channel again works, which happens once by itself.
            override fun onPlayerError(error: PlaybackException) {
                val playlist = url[0]
                marquee.scope.launch {
                    val reason = endReason(error) ?: playlist?.let { liveEndReason(marquee.http, it) }
                    withContext(Dispatchers.Main) {
                        when {
                            reason == null -> onState("The stream stopped.")
                            !reason.startsWith("Stopped:") && !retuned -> { session[0] = null; retuned = true; attempt++ }
                            else -> { session[0] = null; onState(reason.ifBlank { "The stream stopped." }) }
                        }
                    }
                }
            }
        }
        player.addListener(l)
        onDispose {
            player.removeListener(l)
            player.release()
            session[0]?.let { id -> marquee.scope.launch { runCatching { marquee.livetv.stopLiveSession(id) } } }
        }
    }
    PauseInBackground(player, live = true)
    AndroidView({ PlayerView(it).apply { this.player = player; useController = false; resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT } }, modifier,
        update = { it.player = player })
}

/** The 410 reason in a playback error, when the player got it from the playlist itself. */
@OptIn(UnstableApi::class)
private fun endReason(error: PlaybackException): String? {
    var e: Throwable? = error
    while (e != null) {
        if (e is HttpDataSource.InvalidResponseCodeException && e.responseCode == 410) return e.responseBody.toString(Charsets.UTF_8).trim()
        e = e.cause
    }
    return null
}

/** Why a live stream ended: its playlist answers 410 with a plain-text reason; null when it didn't end. */
private fun liveEndReason(http: okhttp3.OkHttpClient, url: String): String? = runCatching {
    http.newCall(okhttp3.Request.Builder().url(url).header("Cache-Control", "no-store").build()).execute().use { r ->
        if (r.code == 410) r.body?.string()?.trim().orEmpty() else null
    }
}.getOrNull()

@Composable
private fun LivePreview(c: LiveChannel, onExpand: () -> Unit) {
    var muted by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var playing by remember { mutableStateOf(false) }
    Box(Modifier.padding(horizontal = sidePadding).fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(10.dp)).background(Color.Black)
        .semantics { contentDescription = "Live preview"; stateDescription = if (playing) "Playing" else "Tuning" }) {
        if (c.available == false) Text(c.unavailableReason ?: "This channel isn't available right now.", Modifier.align(Alignment.Center).padding(24.dp),
            color = Color.White.copy(alpha = 0.8f))
        else LiveVideo(c.id, muted, Modifier.fillMaxSize(), onState = { error = it }, onPlaying = { playing = it })
        Row(Modifier.fillMaxWidth().padding(8.dp)) {
            IconButton({ muted = !muted }, Modifier.focusRing().background(Color.Black.copy(alpha = 0.5f), RoundedCornerShape(50))) {
                Icon(if (muted) Icons.Filled.VolumeOff else Icons.Filled.VolumeUp, if (muted) "Unmute" else "Mute", tint = Color.White)
            }
            Spacer(Modifier.weight(1f))
            IconButton(onExpand, Modifier.focusRing().background(Color.Black.copy(alpha = 0.5f), RoundedCornerShape(50))) { Icon(Icons.Filled.Fullscreen, "Watch full screen", tint = Color.White) }
        }
        Row(Modifier.align(Alignment.BottomStart).fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.85f)))).padding(10.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            ChannelLogo(c, Modifier.width(48.dp).height(28.dp))
            Text(error ?: "Now On: ${c.now?.title ?: c.name}", color = if (error != null) MaterialTheme.colorScheme.error else Color.White,
                style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/**
 * Full-screen live TV. TVs change channel with the remote's up/down; phones swipe left (next) or right
 * (previous) on the video, and swipe down from the top to close. A tap shows or hides the info bar.
 */
@Composable
fun LiveWatchScreen(nav: NavHostController, startId: Long) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    // Refreshed now and then: which channels can be tuned changes as recordings start and stop.
    val channels by produceState(emptyList<LiveChannel>()) {
        while (true) {
            withContext(Dispatchers.IO) { runCatching { marquee.livetv.listLiveChannels() } }.onSuccess { value = it }
            delay(30_000)
        }
    }
    var current by remember { mutableLongStateOf(startId) }
    var error by remember { mutableStateOf<String?>(null) }
    var playing by remember { mutableStateOf(false) }
    var controls by remember { mutableStateOf(true) }
    // The channel being switched to by a swipe, shown briefly over the video.
    var switching by remember { mutableIntStateOf(0) }
    val pull = remember { Animatable(0f) }
    val c = channels.firstOrNull { it.id == current }
    // Phones: full screen in landscape, as for video.
    VideoWindow()
    // Channel up/down and swipes skip channels that can't be tuned now (every connection of their
    // source in use); the one playing stays reachable.
    // Whether it moved: with nothing else tunable it stays put.
    fun step(d: Int): Boolean {
        if (channels.isEmpty()) return false
        val i = channels.indexOfFirst { it.id == current }.coerceAtLeast(0)
        for (k in 1..channels.size) {
            val cand = channels[((i + d * k) % channels.size + channels.size) % channels.size]
            if (cand.id == current) return false
            if (cand.available != false) { current = cand.id; return true }
        }
        return false
    }
    LaunchedEffect(switching) { if (switching > 0) { delay(2000); switching = 0 } }
    // Follows a pull down from the top, which closes it.
    Box(Modifier.fillMaxSize().graphicsLayer { translationY = pull.value }.background(Color.Black)
        .semantics { contentDescription = "Live TV"; stateDescription = if (playing) "Playing" else "Tuning" }
        .onPreviewKeyEvent { e ->
            if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
            when (e.key) {
                Key.DirectionUp, Key.ChannelUp -> { step(-1); true }
                Key.DirectionDown, Key.ChannelDown -> { step(1); true }
                else -> false
            }
        }
        .focusable().initialFocus(true)) {
        androidx.compose.runtime.key(current) { LiveVideo(current, false, Modifier.fillMaxSize(), onState = { error = it }, onPlaying = { playing = it }) }
        // Over the video and under the info bar's buttons, so those keep their taps.
        if (!marquee.isTv) Box(Modifier.fillMaxSize().liveGestures(
            onTap = { controls = !controls },
            onSwipe = { d -> if (step(d)) switching++ },
            onPull = { y -> scope.launch { pull.snapTo(y) } },
            onPullEnd = { close -> if (close) nav.popBackStack() else scope.launch { pull.animateTo(0f) } },
        ))
        if (controls || marquee.isTv) Row(Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.8f), Color.Transparent))).padding(12.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            if (!marquee.isTv) IconButton({ nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Close Live TV", tint = Color.White) }
            c?.let {
                ChannelLogo(it, Modifier.width(64.dp).height(36.dp))
                Column(Modifier.weight(1f)) {
                    Text(it.now?.title ?: it.name, color = Color.White, fontWeight = FontWeight.SemiBold, maxLines = 1)
                    Text(listOfNotNull(it.name, it.next?.let { n -> "Next: ${n.title}" }).joinToString(" · "), color = Color.White.copy(alpha = 0.7f),
                        style = MaterialTheme.typography.bodySmall, maxLines = 1)
                }
            }
        }
        if (switching > 0) c?.let { ChannelOverlay(it, Modifier.align(Alignment.Center)) }
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error) }
    }
}

/** The channel a swipe is switching to: number, logo and name. */
@Composable
private fun ChannelOverlay(c: LiveChannel, modifier: Modifier) {
    Row(modifier.clip(RoundedCornerShape(12.dp)).background(Color.Black.copy(alpha = 0.7f)).padding(horizontal = 18.dp, vertical = 12.dp)
        .semantics(mergeDescendants = true) { contentDescription = "Switching to ${c.name}" },
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        c.number?.let { Text(it, color = Gold, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold) }
        ChannelLogo(c, Modifier.width(64.dp).height(36.dp))
        Text(c.name, color = Color.White, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/**
 * The live player's touch gestures: a tap, a horizontal swipe (left is +1, the next channel; right is -1), and a
 * pull down that starts in the top quarter ([onPull] follows the finger; [onPullEnd] says whether it went far or fast enough to close).
 */
private fun Modifier.liveGestures(onTap: () -> Unit, onSwipe: (Int) -> Unit, onPull: (Float) -> Unit, onPullEnd: (Boolean) -> Unit) = pointerInput(Unit) {
    awaitEachGesture {
        val down = awaitFirstDown()
        val fromTop = down.position.y < size.height * 0.25f
        val tracker = VelocityTracker().apply { addPosition(down.uptimeMillis, down.position) }
        val slop = viewConfiguration.touchSlop
        var mode = 0 // 0 undecided, 1 sideways, 2 pulling down, 3 neither
        var delta = Offset.Zero
        while (true) {
            val e = awaitPointerEvent()
            val ch = e.changes.firstOrNull { it.id == down.id } ?: break
            tracker.addPosition(ch.uptimeMillis, ch.position)
            delta = ch.position - down.position
            if (mode == 0 && delta.getDistance() > slop) mode = when {
                abs(delta.x) > abs(delta.y) -> 1
                delta.y > 0 && fromTop -> 2
                else -> 3
            }
            if (!ch.pressed) {
                val v = tracker.calculateVelocity()
                when (mode) {
                    0 -> if (!ch.isConsumed) onTap()
                    1 -> if (abs(delta.x) > 48.dp.toPx() || abs(v.x) > 600.dp.toPx()) onSwipe(if (delta.x < 0) 1 else -1)
                    2 -> onPullEnd(delta.y > size.height * 0.2f || v.y > 900.dp.toPx())
                }
                break
            }
            if (mode == 1 || mode == 2) ch.consume()
            if (mode == 2) onPull(delta.y.coerceAtLeast(0f))
        }
    }
}
