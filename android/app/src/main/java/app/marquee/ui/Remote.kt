package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Computer
import androidx.compose.material.icons.filled.ConnectedTv
import androidx.compose.material.icons.filled.Forward30
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PhoneAndroid
import androidx.compose.material.icons.filled.PhoneIphone
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.MediaStream
import app.marquee.api.models.RemoteCommand
import app.marquee.api.models.RemotePlayer
import app.marquee.api.models.RemotePlayerState
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** An icon for a player's platform. */
private fun platformIcon(p: String): ImageVector = when (p.lowercase()) {
    "androidtv", "appletv", "tvos", "tv" -> Icons.Filled.Tv
    "android" -> Icons.Filled.PhoneAndroid
    "ios", "ipados", "iphone", "ipad" -> Icons.Filled.PhoneIphone
    else -> Icons.Filled.Computer
}

/** What a player is doing, in a line. */
private fun nowLine(s: RemotePlayerState?): String = when {
    s == null || s.itemId == null || s.state == RemotePlayerState.State.IDLE || s.state == RemotePlayerState.State.STOPPED -> "Nothing playing"
    else -> (if (s.state == RemotePlayerState.State.PAUSED) "Paused: " else "Playing ") + listOfNotNull(s.title, s.subtitle).joinToString(" · ")
}

/** Sends a command to a player; the error's message, or null when it went. */
suspend fun sendRemote(marquee: app.marquee.core.Marquee, deviceId: Long, c: RemoteCommand): String? =
    withContext(Dispatchers.IO) { runCatching { marquee.playback.sendRemoteCommand(deviceId, c) } }.exceptionOrNull()?.let { it.message ?: "Couldn't reach it" }

/**
 * "Play on…" (USER-14): lists the person's other Marquee apps that are open and sends them
 * handoff's play command (an item, or what's playing here at its position), then opens the
 * Remote for that player. Phones and tablets only; Chromecast stays behind the Cast button.
 */
@Composable
fun PlayOnButton(nav: NavHostController? = null, tint: Color = Color.White, handoff: () -> RemoteCommand?, onSent: () -> Unit = {},
    onPlayer: ((Long) -> Unit)? = null) {
    val marquee = LocalMarquee.current
    if (marquee.isTv) return
    var open by remember { mutableStateOf(false) }
    IconButton({ open = true }, Modifier.focusRing()) { Icon(Icons.Filled.ConnectedTv, "Play on…", tint = tint) }
    if (open) PlayOnDialog(handoff, onDismiss = { open = false }) { player ->
        open = false
        onSent()
        onPlayer?.invoke(player.deviceId) ?: nav?.navigate("remote/${player.deviceId}")
    }
}

@Composable
private fun PlayOnDialog(handoff: () -> RemoteCommand?, onDismiss: () -> Unit, onSent: (RemotePlayer) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var players by remember { mutableStateOf<List<RemotePlayer>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        while (isActive) {
            withContext(Dispatchers.IO) { runCatching { marquee.playback.listRemotePlayers() } }
                .onSuccess { players = it }.onFailure { error = it.message }
            delay(5_000)
        }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Play on…") },
        text = {
            Column {
                val list = players
                when {
                    list == null -> CircularProgressIndicator()
                    list.isEmpty() -> Text("No other Marquee apps are open. Open Marquee on a TV, phone or browser signed in as you, and it appears here.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                    else -> list.forEach { p ->
                        Row(
                            Modifier.fillMaxWidth().focusCard({
                                if (busy) return@focusCard
                                val c = handoff()
                                if (c == null) { onSent(p); return@focusCard }
                                busy = true
                                // Navigates afterwards, so on the main thread whatever the caller's scope.
                                scope.launch(Dispatchers.Main) {
                                    val e = sendRemote(marquee, p.deviceId, c)
                                    busy = false
                                    if (e == null) onSent(p) else error = e
                                }
                            }).semantics { contentDescription = "Play on ${p.name}" }.padding(vertical = 10.dp, horizontal = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Icon(platformIcon(p.platform), null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                            Column(Modifier.padding(start = 14.dp).weight(1f)) {
                                Text(p.name + if (p.userId != marquee.me.value?.id) " · ${p.userName}" else "", fontWeight = FontWeight.Medium)
                                Text(nowLine(p.state), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                        }
                    }
                }
                error?.let { Text(it, Modifier.padding(top = 8.dp), color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(onDismiss, Modifier.focusRing()) { Text("Cancel") } },
    )
}

/** Settings → Remote: pick one of your open Marquee apps to control. */
@Composable
fun RemotePlayersScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    var players by remember { mutableStateOf<List<RemotePlayer>?>(null) }
    LaunchedEffect(Unit) {
        while (isActive) {
            withContext(Dispatchers.IO) { runCatching { marquee.playback.listRemotePlayers() } }.onSuccess { players = it }
            delay(5_000)
        }
    }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Remote", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        item {
            Text("Control Marquee on your TV, another phone or a browser. Apps appear here while they're open.",
                Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        val list = players
        if (list == null) item { CircularProgressIndicator(Modifier.padding(sidePadding)) }
        else if (list.isEmpty()) item { Text("No other Marquee apps are open.", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        else items(list, key = { it.deviceId }) { p ->
            ListItem(
                headlineContent = { Text(p.name + if (p.userId != marquee.me.value?.id) " · ${p.userName}" else "") },
                supportingContent = { Text(nowLine(p.state), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingContent = { Icon(platformIcon(p.platform), null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ nav.navigate("remote/${p.deviceId}") }).semantics { contentDescription = "Control ${p.name}" },
            )
            HorizontalDivider()
        }
    }
}

/**
 * The Remote for one player (USER-14): what it's playing with artwork and progress (long-polled),
 * play/pause, seeking, previous/next, audio and subtitle tracks, volume, Stop and Disconnect.
 */
@Composable
fun RemoteScreen(nav: NavHostController, deviceId: Long) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var player by remember { mutableStateOf<RemotePlayer?>(null) }
    var gone by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // The position, moved along locally between updates while playing.
    var shownPos by remember { mutableLongStateOf(0L) }
    var drag by remember { mutableStateOf<Float?>(null) }
    LaunchedEffect(deviceId) {
        var since: Long? = null
        while (isActive) {
            val r = withContext(Dispatchers.IO) { runCatching { marquee.playback.getRemotePlayer(deviceId, since) } }
            r.onSuccess { player = it; since = it.version; gone = false; shownPos = it.state?.positionMs ?: 0 }
                .onFailure { e ->
                    if ((e as? ClientException)?.statusCode == 404) { gone = true; player = null }
                    delay(5_000)
                }
        }
    }
    val st = player?.state
    val playing = st?.state == RemotePlayerState.State.PLAYING
    LaunchedEffect(playing, st) {
        while (playing && isActive) {
            delay(1_000)
            shownPos = (shownPos + 1_000).coerceAtMost(st?.durationMs ?: Long.MAX_VALUE)
        }
    }
    // The item's details: its artwork and its audio and subtitle tracks.
    val detail by produceState<ItemDetail?>(null, st?.itemId) {
        value = st?.itemId?.let { id -> withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(id) }.getOrNull() } }
    }
    val art by produceState<Long?>(null, st?.artItemId, detail) {
        val a = st?.artItemId
        value = if (a == null || a == detail?.id) detail?.images?.poster ?: detail?.images?.thumb
        else withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(a).images?.let { it.poster ?: it.thumb } }.getOrNull() }
    }

    fun send(c: RemoteCommand) = scope.launch { error = sendRemote(marquee, deviceId, c) }

    Column(Modifier.fillMaxSize().padding(sidePadding), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            player?.let { Icon(platformIcon(it.platform), null, tint = Gold) }
            Text(player?.name ?: "Remote", Modifier.padding(start = 10.dp).weight(1f), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (gone) {
            Text("This player isn't available any more. Marquee may have closed there.", color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton({ nav.popBackStack() }, Modifier.focusRing()) { Text("Close") }
            return@Column
        }
        if (player == null) { CircularProgressIndicator(); return@Column }
        Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) {
            val url = marquee.imageUrl(art, 480)
            if (url != null) AsyncImage(url, null, Modifier.widthIn(max = 360.dp).fillMaxWidth().aspectRatio(if (st?.itemType == app.marquee.api.models.ItemType.TRACK) 1f else 2f / 3f)
                .clip(RoundedCornerShape(12.dp)).background(Surface2))
            else Icon(platformIcon(player!!.platform), null, Modifier.size(96.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Text(st?.title ?: "Nothing playing", Modifier.semantics { contentDescription = "Remote title" }, style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis)
        st?.subtitle?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        val dur = st?.durationMs ?: 0
        if (dur > 0) {
            Slider(drag ?: (shownPos.toFloat() / dur).coerceIn(0f, 1f), { drag = it },
                Modifier.widthIn(max = 480.dp).fillMaxWidth().semantics { contentDescription = "Remote position" },
                onValueChangeFinished = { drag?.let { val to = (it * dur).toLong(); shownPos = to; send(RemoteCommand(RemoteCommand.Type.SEEK, positionMs = to)) }; drag = null })
            Row(Modifier.widthIn(max = 480.dp).fillMaxWidth()) {
                Text(formatTime(shownPos), style = MaterialTheme.typography.labelMedium)
                Spacer(Modifier.weight(1f))
                Text(formatTime(dur), style = MaterialTheme.typography.labelMedium)
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.semantics { stateDescription = st?.state?.value ?: "idle" }) {
            IconButton({ send(RemoteCommand(RemoteCommand.Type.PREVIOUS)) }, Modifier.focusRing()) { Icon(Icons.Filled.SkipPrevious, "Previous") }
            IconButton({ send(RemoteCommand(RemoteCommand.Type.SEEK, positionMs = (shownPos - 10_000).coerceAtLeast(0))) }, Modifier.focusRing()) {
                Icon(Icons.Filled.Replay10, "Back 10 seconds")
            }
            IconButton({
                send(RemoteCommand(if (playing) RemoteCommand.Type.PAUSE else RemoteCommand.Type.RESUME))
            }, Modifier.focusRing().size(68.dp).clip(RoundedCornerShape(34.dp)).background(Gold)) {
                Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size(38.dp), tint = MaterialTheme.colorScheme.onPrimary)
            }
            IconButton({ send(RemoteCommand(RemoteCommand.Type.SEEK, positionMs = shownPos + 30_000)) }, Modifier.focusRing()) { Icon(Icons.Filled.Forward30, "Forward 30 seconds") }
            IconButton({ send(RemoteCommand(RemoteCommand.Type.NEXT)) }, Modifier.focusRing()) { Icon(Icons.Filled.SkipNext, "Next") }
        }
        // Audio and subtitle tracks of a video.
        val file = detail?.versions?.flatMap { it.files }?.let { files ->
            files.firstOrNull { f -> f.streams.any { it.id == st?.audioStreamId || it.id == st?.subtitleStreamId } } ?: files.firstOrNull()
        }
        val audio = file?.streams.orEmpty().filter { it.kind == MediaStream.Kind.AUDIO }
        val subs = file?.streams.orEmpty().filter { it.kind == MediaStream.Kind.SUBTITLE }
        if (audio.size > 1 || subs.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (audio.size > 1) TrackPicker("Audio", audio, st?.audioStreamId, offLabel = null) { send(RemoteCommand(RemoteCommand.Type.SET_AUDIO, streamId = it)) }
            if (subs.isNotEmpty()) TrackPicker("Subtitles", subs, st?.subtitleStreamId?.takeIf { it >= 0 }, offLabel = "Off") {
                send(RemoteCommand(RemoteCommand.Type.SET_SUBTITLE, streamId = it))
            }
        }
        st?.volume?.let { v ->
            var vol by remember(v) { mutableStateOf(v.toFloat()) }
            Row(Modifier.widthIn(max = 480.dp).fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.AutoMirrored.Filled.VolumeUp, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Slider(vol, { vol = it }, Modifier.weight(1f).padding(start = 8.dp).semantics { contentDescription = "Volume" },
                    onValueChangeFinished = { send(RemoteCommand(RemoteCommand.Type.SET_VOLUME, volume = vol.toDouble())) })
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton({ send(RemoteCommand(RemoteCommand.Type.STOP)) }, Modifier.focusRing()) { Text("Stop") }
            OutlinedButton({ nav.popBackStack() }, Modifier.focusRing()) { Text("Disconnect") }
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
    }
}

/** A button that opens a list of tracks; null offLabel = no "off" choice. */
@Composable
private fun TrackPicker(label: String, streams: List<MediaStream>, current: Long?, offLabel: String?, onPick: (Long) -> Unit) {
    var open by remember { mutableStateOf(false) }
    fun name(s: MediaStream) = listOfNotNull(
        s.language?.let { java.util.Locale.forLanguageTag(it).displayLanguage.takeIf { d -> d.isNotBlank() } ?: it },
        s.title?.takeIf { it.isNotBlank() }, "Forced".takeIf { s.forced },
    ).joinToString(" · ").ifBlank { "Track ${s.id}" }
    Box {
        OutlinedButton({ open = true }, Modifier.focusRing()) {
            Text("$label: " + (streams.firstOrNull { it.id == current }?.let(::name) ?: offLabel ?: "Default"), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        DropdownMenu(open, { open = false }) {
            offLabel?.let { DropdownMenuItem({ Text(it) }, { open = false; onPick(-1) }) }
            streams.forEach { s -> DropdownMenuItem({ Text(name(s) + if (s.id == current) "  ✓" else "") }, { open = false; onPick(s.id) }) }
        }
    }
}
