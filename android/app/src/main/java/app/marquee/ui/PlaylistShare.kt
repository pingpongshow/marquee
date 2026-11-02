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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistShare
import app.marquee.api.models.PlaylistShareTarget
import app.marquee.api.models.PlaylistSharing
import app.marquee.api.models.PlaylistSharingUpdate
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Who can be offered a playlist: the server's list, plus anyone it's already offered to who isn't on it. */
internal fun sharePeople(targets: List<PlaylistShareTarget>, shares: List<PlaylistShare>): List<PlaylistShareTarget> =
    (targets + shares.filter { s -> targets.none { it.id == s.userId } }.map { PlaylistShareTarget(it.userId, it.name) }).sortedBy { it.name.lowercase() }

/**
 * Share (MUSIC-24): choose who your playlist is offered to, seeing who has accepted. Admins can
 * offer it to everyone. People must accept before it joins their playlists; they can play it but
 * not change it. [onDone] says whether anything changed.
 */
@Composable
fun PlaylistShareDialog(p: Playlist, onDone: (Boolean) -> Unit) {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    val scope = rememberCoroutineScope()
    var sharing by remember { mutableStateOf<PlaylistSharing?>(null) }
    var targets by remember { mutableStateOf<List<PlaylistShareTarget>>(emptyList()) }
    val picked = remember { mutableStateListOf<Long>() }
    var everyone by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(p.id) {
        withContext(Dispatchers.IO) { runCatching { marquee.playlists.getPlaylistSharing(p.id) to marquee.playlists.listPlaylistShareTargets() } }
            .onSuccess { (s, t) ->
                targets = t
                picked.clear()
                picked.addAll(s.shares.map { it.userId })
                everyone = s.all
                sharing = s
            }
            .onFailure { error = serverMessage(it, "Couldn't load who it's shared with") }
    }
    fun save() {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.playlists.setPlaylistSharing(p.id, PlaylistSharingUpdate(picked.toList(), all = everyone.takeIf { me?.isAdmin == true })) } }
                .onSuccess { onDone(true) }
                .onFailure { error = serverMessage(it, "Couldn't share it") }
            busy = false
        }
    }
    // Read here, not just inside the dialog: an answer arriving during the dialog's first composition could otherwise never show.
    val s = sharing
    AlertDialog(
        onDismissRequest = { if (!busy) onDone(false) },
        title = { Text("Share “${p.title}”") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                when {
                    s == null && error == null -> Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                    s != null -> {
                        Text("They'll see it once they accept, and can play it but not change it.", style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Column(Modifier.heightIn(max = 340.dp).verticalScroll(rememberScrollState())) {
                            // Only admins may share with everyone (the server refuses anyone else).
                            if (me?.isAdmin == true) ShareRow("Everyone", "Everyone on this server, and people added later", everyone, !busy) { everyone = it }
                            val people = sharePeople(targets, s.shares)
                            people.forEach { t ->
                                val status = when (s.shares.firstOrNull { it.userId == t.id }?.status) {
                                    PlaylistShare.Status.PENDING -> "Pending"
                                    PlaylistShare.Status.ACCEPTED -> "Accepted"
                                    null -> null
                                }
                                // With everyone on, nobody's left out, whoever is ticked.
                                ShareRow(t.name, status, everyone || t.id in picked, !busy && !everyone) { on -> if (on) picked.add(t.id) else picked.remove(t.id) }
                            }
                            if (people.isEmpty() && me?.isAdmin != true) Text("There's nobody else on this server yet.", Modifier.padding(vertical = 8.dp),
                                color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(::save, Modifier.focusRing(), enabled = s != null && !busy) { Text("Save") } },
        dismissButton = { TextButton({ onDone(false) }, Modifier.focusRing(), enabled = !busy) { Text("Cancel") } },
    )
}

@Composable
private fun ShareRow(name: String, detail: String?, on: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().focusRing(RoundedCornerShape(8.dp)).clickable(enabled = enabled) { onChange(!on) }
        .semantics(mergeDescendants = true) {
            contentDescription = name
            role = Role.Checkbox
            stateDescription = listOfNotNull(if (on) "Shared" else "Not shared", detail).joinToString(", ")
        }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Checkbox(on, null, enabled = enabled, modifier = Modifier.padding(12.dp))
        Column(Modifier.weight(1f)) {
            Text(name)
            detail?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}
