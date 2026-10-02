package app.marquee.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import app.marquee.api.models.AddPlaylistItemsRequest
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistCreate
import app.marquee.api.models.PlaylistKind
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** "Add to playlist" (USER-7) for any track, album, artist, movie, show or episode. */
@Composable
fun AddToPlaylistButton(itemId: Long, music: Boolean) {
    var open by remember { mutableStateOf(false) }
    OutlinedButton(modifier = Modifier.focusRing(), onClick = { open = true }) {
        Icon(Icons.AutoMirrored.Filled.PlaylistAdd, null)
        Text("Add to playlist")
    }
    if (open) AddToPlaylistDialog(itemId, music) { open = false }
}

/** Picks one of the person's playlists of the right kind, or makes a new one with the item in it. */
@Composable
fun AddToPlaylistDialog(itemId: Long, music: Boolean, onDone: () -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val kind = if (music) PlaylistKind.AUDIO else PlaylistKind.VIDEO
    val playlists by produceState<List<Playlist>?>(null) {
        // Smart playlists fill themselves, so they aren't offered.
        value = withContext(Dispatchers.IO) { runCatching { marquee.playlists.listPlaylists(kind).filter { it.rules == null } }.getOrDefault(emptyList()) }
    }
    var newTitle by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var done by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    fun act(label: String, work: suspend () -> Unit) {
        busy = true
        scope.launch {
            runCatching { withContext(Dispatchers.IO) { work() } }
                .onSuccess { done = label }
                .onFailure { error = it.message ?: "That didn't work" }
            busy = false
        }
    }
    AlertDialog(
        onDismissRequest = onDone,
        title = { Text("Add to playlist") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                when {
                    done != null -> Text("Added to $done.")
                    playlists == null -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                    else -> {
                        Column(Modifier.heightIn(max = 320.dp).verticalScroll(rememberScrollState())) {
                            playlists!!.forEach { p ->
                                Row(Modifier.fillMaxWidth().clickable(enabled = !busy) {
                                    act(p.title) { marquee.playlists.addPlaylistItems(p.id, AddPlaylistItemsRequest(listOf(itemId))) }
                                }.focusRing().padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                                    Column(Modifier.weight(1f)) {
                                        Text(p.title)
                                        Text("${p.itemCount} items", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    }
                                }
                            }
                        }
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(newTitle, { newTitle = it }, Modifier.weight(1f), singleLine = true, label = { Text("New playlist") })
                            TextButton({ act(newTitle.trim()) { marquee.playlists.createPlaylist(PlaylistCreate(newTitle.trim(), kind, listOf(itemId))) } },
                                Modifier.focusRing(), enabled = newTitle.isNotBlank() && !busy) { Text("Create") }
                        }
                    }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(onDone, Modifier.focusRing()) { Text(if (done != null) "Done" else "Cancel") } },
    )
}

/** The "…" menu on a track: play next, add to the queue, add to a playlist. */
@Composable
fun TrackMenu(track: ItemSummary) {
    val music = LocalMusic.current
    var open by remember { mutableStateOf(false) }
    var playlist by remember { mutableStateOf(false) }
    Box {
        IconButton({ open = true }, Modifier.focusRing()) { Icon(Icons.Filled.MoreVert, "More actions for ${track.title}") }
        DropdownMenu(open, { open = false }) {
            DropdownMenuItem({ Text("Play next") }, { music.enqueue(listOf(track), next = true); open = false })
            DropdownMenuItem({ Text("Add to queue") }, { music.enqueue(listOf(track), next = false); open = false })
            DropdownMenuItem({ Text("Add to playlist…") }, { playlist = true; open = false })
        }
    }
    if (playlist) AddToPlaylistDialog(track.id, music = true) { playlist = false }
}
