package app.marquee.music

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistCreate
import app.marquee.api.models.PlaylistKind
import app.marquee.ui.LocalMarquee
import app.marquee.ui.focusRing
import app.marquee.ui.serverMessage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Playlists hold at most this many tracks when saved from a queue or mix. */
private const val MAX_TRACKS = 500

/** The title a saved queue gets by default: the station or mix's name, else "Queue – <date>". */
fun defaultPlaylistTitle(source: String?): String = source?.takeIf { it.isNotBlank() }
    ?: ("Queue – " + java.time.LocalDate.now().format(java.time.format.DateTimeFormatter.ofLocalizedDate(java.time.format.FormatStyle.MEDIUM)))

/**
 * Save as playlist: names a new audio playlist (pre-filled with [defaultTitle]) and makes it from
 * [trackIds] in order, de-duplicated, up to 500. [onDone] gets the playlist, or null when cancelled.
 */
@Composable
fun SaveAsPlaylistDialog(defaultTitle: String, trackIds: List<Long>, onDone: (Playlist?) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var title by remember { mutableStateOf(defaultTitle) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val ids = remember(trackIds) { trackIds.distinct().take(MAX_TRACKS) }
    fun save() {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.playlists.createPlaylist(PlaylistCreate(title.trim(), PlaylistKind.AUDIO, ids)) } }
                .onSuccess { onDone(it) }
                .onFailure { error = serverMessage(it) }
            busy = false
        }
    }
    AlertDialog(
        onDismissRequest = { if (!busy) onDone(null) },
        title = { Text("Save as playlist") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(if (ids.size == 1) "1 track" else "${ids.size} tracks", style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(title, { title = it }, Modifier.fillMaxWidth().testTag("playlistTitle"), singleLine = true, label = { Text("Playlist name") })
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(::save, Modifier.focusRing(), enabled = !busy && title.isNotBlank() && ids.isNotEmpty()) { Text("Save") } },
        dismissButton = { TextButton({ onDone(null) }, Modifier.focusRing(), enabled = !busy) { Text("Cancel") } },
    )
}
