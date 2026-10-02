package app.marquee.ui

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
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import androidx.media3.common.Player
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
import kotlinx.coroutines.Dispatchers
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

/** Live TV (LIVE-2): Guide and What's On, a live preview on phones, full screen on TV. */
@Composable
fun LiveTvScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val status by produceState<app.marquee.api.models.LiveTvStatus?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.livetv.liveTvStatus() }.getOrNull() } }
    val groups by produceState(emptyList<String>()) { value = withContext(Dispatchers.IO) { runCatching { marquee.livetv.listLiveGroups().map { it.name } }.getOrDefault(emptyList()) } }
    var tab by remember { mutableIntStateOf(0) }
    var filter by remember { mutableStateOf("all") }
    var start by remember { mutableStateOf(defaultStart()) }
    var channels by remember { mutableStateOf<List<LiveChannel>?>(null) }
    var guide by remember { mutableStateOf<Map<Long, List<LiveProgramme>>>(emptyMap()) }
    var preview by remember { mutableStateOf<Long?>(null) }
    var details by remember { mutableStateOf<Pair<LiveProgramme, LiveChannel>?>(null) }
    var reload by remember { mutableIntStateOf(0) }

    LaunchedEffect(filter, start, reload) {
        val group = filter.takeUnless { it == "all" || it == "favorites" }
        val fav = if (filter == "favorites") true else null
        withContext(Dispatchers.IO) {
            channels = runCatching { marquee.livetv.listLiveChannels(group, fav) }.getOrDefault(emptyList())
            guide = runCatching { marquee.livetv.liveGuide(start, start.plusHours(4), group, fav).associate { it.channelId to it.programmes } }.getOrDefault(emptyMap())
        }
    }
    fun watch(id: Long) = nav.navigate("live/$id")
    fun pick(c: LiveChannel) { if (marquee.isTv || preview == c.id) watch(c.id) else preview = c.id }

    val s = status
    if (s == null) { Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }; return }
    if (!s.enabled) {
        Column(Modifier.fillMaxWidth().padding(48.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Live TV isn't set up", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            Text("An admin can add Dispatcharr or an M3U playlist in the web app's Settings → Live TV.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        return
    }
    val list = channels
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Text("Live TV", Modifier.padding(horizontal = sidePadding), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        }
        item {
            Row(Modifier.padding(horizontal = sidePadding), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                (if (s.canRecord == true) listOf("Guide", "What's On", "Recordings") else listOf("Guide", "What's On")).forEachIndexed { i, t ->
                    FilterChip(tab == i, { tab = i }, { Text(t) }, Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(marquee.isTv && i == 0))
                }
            }
        }
        if (tab == 2) {
            item { RecordingsList(nav) }
            return@LazyColumn
        }
        item {
            LazyRow(contentPadding = PaddingValues(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                val options = listOf("all" to "All Channels", "favorites" to "Favorites") + groups.map { it to it }
                items(options) { (v, label) -> FilterChip(filter == v, { filter = v }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp))) }
            }
        }
        if (!marquee.isTv) list?.let { l -> (l.firstOrNull { it.id == preview } ?: l.firstOrNull())?.let { c ->
            item(key = "preview") { LivePreview(c) { watch(c.id) } }
        } }
        if (list == null) item { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        else if (list.isEmpty()) item {
            Text(if (filter == "favorites") "No favourites yet. Add some with the heart next to a channel." else "No channels.",
                Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else if (tab == 0) {
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
                        val previewed = preview ?: list.firstOrNull()?.id
                        if (p.isOn() && (marquee.isTv || previewed != c.id)) pick(c) else details = p to c
                    },
                    onFavorite = { c ->
                        scope.launch {
                            withContext(Dispatchers.IO) { runCatching { if (c.favorite) marquee.livetv.unfavoriteLiveChannel(c.id) else marquee.livetv.favoriteLiveChannel(c.id) } }
                            reload++
                        }
                    })
            }
        } else {
            items(list, key = { it.id }) { c -> WhatsOnRow(c) { watch(c.id) } }
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
                    if (s.canRecord == true && p.end.isAfter(OffsetDateTime.now())) RecordActions(p, c) { details = null; reload++ }
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
                Row(Modifier.height(row).fillMaxWidth().border(1.dp, if (selected == c.id) Gold else Color.Transparent, RoundedCornerShape(6.dp)),
                    verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f).focusCard({ onChannel(c) }).padding(4.dp).semantics { contentDescription = "Preview ${c.name}" },
                        horizontalAlignment = Alignment.CenterHorizontally) {
                        ChannelLogo(c, Modifier.width(56.dp).height(row * 0.45f))
                        c.number?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                    IconButton({ onFavorite(c) }, Modifier.focusRing()) {
                        Icon(if (c.favorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
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
                    Box(Modifier.width(width).height(row)) {
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
    Column(Modifier.padding(horizontal = sidePadding).fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Surface2).focusCard(onWatch, RoundedCornerShape(10.dp))
        .semantics { contentDescription = "Watch ${c.name}" }.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            ChannelLogo(c, Modifier.width(64.dp).height(40.dp))
            Column(Modifier.weight(1f)) {
                Text(c.now?.title ?: c.name, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(c.now?.let { "${it.minutesLeft()}m left · ${c.name}" } ?: c.name, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        c.now?.let { LinearProgressIndicator(progress = { it.progress() }, Modifier.fillMaxWidth(), color = Gold) }
        c.next?.let { Text("Next: ${it.title} at ${it.start.local().format(clock)}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** Plays a channel in an ExoPlayer, stopping the server stream when it goes away. */
@OptIn(UnstableApi::class)
@Composable
private fun LiveVideo(channelId: Long, muted: Boolean, modifier: Modifier, onState: (String?) -> Unit = {}, onPlaying: (Boolean) -> Unit = {}) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val player = remember(channelId) { ExoPlayer.Builder(context).build() }
    val session = remember(channelId) { arrayOfNulls<String>(1) } // read when the player goes away
    LaunchedEffect(muted, player) { player.volume = if (muted) 0f else 1f }
    LaunchedEffect(channelId) {
        onState(null)
        val s = withContext(Dispatchers.IO) { runCatching { marquee.livetv.playLiveChannel(channelId, PlayLiveChannelRequest(AndroidProfile.profile)) } }
        s.onSuccess {
            withContext(Dispatchers.Main) {
                player.setMediaItem(MediaItem.fromUri(marquee.absolute(it.url)!!))
                player.prepare()
                player.playWhenReady = true
            }
        }.onFailure { onState(it.message ?: "Couldn't tune the channel") }
        session[0] = s.getOrNull()?.id
    }
    DisposableEffect(player) {
        val l = object : Player.Listener { override fun onIsPlayingChanged(isPlaying: Boolean) = onPlaying(isPlaying) }
        player.addListener(l)
        onDispose {
            player.removeListener(l)
            player.release()
            session[0]?.let { id -> marquee.scope.launch { runCatching { marquee.livetv.stopLiveSession(id) } } }
        }
    }
    AndroidView({ PlayerView(it).apply { this.player = player; useController = false; resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT } }, modifier,
        update = { it.player = player })
}

@Composable
private fun LivePreview(c: LiveChannel, onExpand: () -> Unit) {
    var muted by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf<String?>(null) }
    var playing by remember { mutableStateOf(false) }
    Box(Modifier.padding(horizontal = sidePadding).fillMaxWidth().aspectRatio(16f / 9f).clip(RoundedCornerShape(10.dp)).background(Color.Black)
        .semantics { contentDescription = "Live preview"; stateDescription = if (playing) "Playing" else "Tuning" }) {
        LiveVideo(c.id, muted, Modifier.fillMaxSize(), onState = { error = it }, onPlaying = { playing = it })
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

/** Full-screen live TV with channel up/down (buttons on phones, the D-pad on TV). */
@Composable
fun LiveWatchScreen(nav: NavHostController, startId: Long) {
    val marquee = LocalMarquee.current
    val channels by produceState(emptyList<LiveChannel>()) { value = withContext(Dispatchers.IO) { runCatching { marquee.livetv.listLiveChannels() }.getOrDefault(emptyList()) } }
    var current by remember { mutableLongStateOf(startId) }
    var error by remember { mutableStateOf<String?>(null) }
    var playing by remember { mutableStateOf(false) }
    val c = channels.firstOrNull { it.id == current }
    // Phones: full screen in landscape, as for video.
    val context = LocalContext.current
    DisposableEffect(Unit) {
        val activity = context as? android.app.Activity
        val window = activity?.window
        val bars = window?.let { androidx.core.view.WindowCompat.getInsetsController(it, it.decorView) }
        window?.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        if (!marquee.isTv) {
            activity?.requestedOrientation = android.content.pm.ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
            bars?.systemBarsBehavior = androidx.core.view.WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            bars?.hide(androidx.core.view.WindowInsetsCompat.Type.systemBars())
        }
        onDispose {
            window?.clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            if (!marquee.isTv) {
                activity?.requestedOrientation = android.content.pm.ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                bars?.show(androidx.core.view.WindowInsetsCompat.Type.systemBars())
            }
        }
    }
    fun step(d: Int) {
        if (channels.isEmpty()) return
        val i = channels.indexOfFirst { it.id == current }.coerceAtLeast(0)
        current = channels[(i + d + channels.size) % channels.size].id
    }
    Box(Modifier.fillMaxSize().background(Color.Black)
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
        Row(Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.8f), Color.Transparent))).padding(12.dp),
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
            if (!marquee.isTv) {
                IconButton({ step(-1) }) { Icon(Icons.Filled.KeyboardArrowUp, "Channel up", tint = Color.White) }
                IconButton({ step(1) }) { Icon(Icons.Filled.KeyboardArrowDown, "Channel down", tint = Color.White) }
            }
        }
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error) }
    }
}
