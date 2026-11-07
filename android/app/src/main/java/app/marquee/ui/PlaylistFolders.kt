package app.marquee.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CreateNewFolder
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.FolderOff
import androidx.compose.material.icons.filled.FolderOpen
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistFolder
import app.marquee.api.models.PlaylistFolderUpdate
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Playlist folders (MUSIC-28): optional, and each person's own.

/** Puts [p] in [folderId], or takes it out of its folder with null. */
private suspend fun moveToFolder(marquee: Marquee, p: Playlist, folderId: Long?) = withContext(Dispatchers.IO) {
    runCatching {
        if (folderId != null) marquee.playlists.addPlaylistToFolder(folderId, p.id)
        else p.folderId?.let { marquee.playlists.removePlaylistFromFolder(it, p.id) }
    }
}

/** A playlist's folder menu: the person's folders (its own ticked), New folder…, and Remove from folder. */
@Composable
fun PlaylistFolderMenu(p: Playlist, expanded: Boolean, onDismiss: () -> Unit, onChanged: () -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    var folders by remember { mutableStateOf<List<PlaylistFolder>?>(null) }
    var naming by remember { mutableStateOf(false) }
    LaunchedEffect(expanded) {
        if (expanded) folders = withContext(Dispatchers.IO) { runCatching { marquee.playlists.listPlaylistFolders() }.getOrNull() } ?: folders
    }
    fun move(folderId: Long?) {
        onDismiss()
        if (folderId == p.folderId) return
        marquee.scope.launch {
            moveToFolder(marquee, p, folderId)
                .onSuccess { onChanged() }
                .onFailure { toast(context, "Couldn't move “${p.title}”: ${serverMessage(it)}") }
        }
    }
    DropdownMenu(expanded, onDismiss) {
        folders?.forEach { f ->
            DropdownMenuItem({ Text(f.name) }, { move(f.id) },
                leadingIcon = { Icon(if (f.id == p.folderId) Icons.Filled.Check else Icons.Filled.Folder, null) })
        }
        DropdownMenuItem({ Text("New folder…") }, { onDismiss(); naming = true }, leadingIcon = { Icon(Icons.Filled.CreateNewFolder, null) })
        if (p.folderId != null) {
            HorizontalDivider()
            DropdownMenuItem({ Text("Remove from folder") }, { move(null) }, leadingIcon = { Icon(Icons.Filled.FolderOff, null) })
        }
    }
    if (naming) FolderNameDialog("New folder", "", "Create", { naming = false }) { name ->
        withContext(Dispatchers.IO) { runCatching { marquee.playlists.createPlaylistFolder(PlaylistFolderUpdate(name)) } }
            .mapCatching { f -> moveToFolder(marquee, p, f.id).getOrThrow() }
            .onSuccess { onChanged() }
    }
}

/** "Folder" on a playlist's page, named after the folder it's in. */
@Composable
fun PlaylistFolderButton(p: Playlist, folderName: String?, onChanged: () -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        OutlinedButton({ open = true }, Modifier.focusRing()) {
            Icon(if (p.folderId == null) Icons.Filled.Folder else Icons.Filled.FolderOpen, null)
            Text(folderName ?: "Folder", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
        }
        PlaylistFolderMenu(p, open, { open = false }, onChanged)
    }
}

/** Asks for a folder's name; [save] says whether it worked, and the dialog closes when it did. */
@Composable
fun FolderNameDialog(title: String, initial: String, confirm: String, onDismiss: () -> Unit, save: suspend (String) -> Result<*>) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    var name by remember { mutableStateOf(initial) }
    var busy by remember { mutableStateOf(false) }
    AlertDialog(onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { OutlinedTextField(name, { name = it }, label = { Text("Name") }, placeholder = { Text("Electronic") }, singleLine = true) },
        confirmButton = {
            TextButton({
                busy = true
                marquee.scope.launch {
                    save(name.trim())
                        .onSuccess { onDismiss() }
                        .onFailure { toast(context, "Couldn't save the folder: ${serverMessage(it)}") }
                    busy = false
                }
            }, Modifier.focusRing(), enabled = !busy && name.isNotBlank() && name.trim() != initial) { Text(confirm) }
        },
        dismissButton = { TextButton(onDismiss, Modifier.focusRing()) { Text("Cancel") } })
}
