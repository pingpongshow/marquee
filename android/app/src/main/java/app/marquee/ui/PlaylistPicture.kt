package app.marquee.ui

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Photo
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistKind
import app.marquee.api.models.SetPlaylistImageLinkRequest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

/** The most the server takes for a picture. */
private const val MAX_PICTURE_BYTES = 20L * 1024 * 1024

/**
 * A picked picture as a file to upload, named for its type (the upload's Content-Type comes
 * from the name). The server takes JPEG, PNG and WebP: anything else (HEIC from a camera, say)
 * is converted to JPEG here.
 */
private fun pictureFile(context: Context, uri: Uri): File {
    val ext = when (context.contentResolver.getType(uri)) { "image/jpeg" -> "jpg"; "image/png" -> "png"; "image/webp" -> "webp"; else -> null }
    val out = File(context.cacheDir, "playlist-picture." + (ext ?: "jpg"))
    val input = context.contentResolver.openInputStream(uri) ?: throw java.io.IOException("Couldn't open that picture")
    input.use { src ->
        if (ext != null) out.outputStream().use { src.copyTo(it) }
        else {
            val bitmap = BitmapFactory.decodeStream(src) ?: throw java.io.IOException("That file isn't a picture")
            out.outputStream().use { bitmap.compress(Bitmap.CompressFormat.JPEG, 90, it) }
        }
    }
    if (out.length() > MAX_PICTURE_BYTES) { out.delete(); throw java.io.IOException("That picture is too big (20 MB at most)") }
    return out
}

/**
 * A picture of its own for a playlist or collection (MUSIC-30), for its owner: upload one
 * (phones and tablets; a TV has no picker), give an image's link, or remove it to go back to
 * the mosaic. It closes after a change; [onDone] says whether there was one.
 */
@Composable
fun PlaylistPictureDialog(p: Playlist, onDone: (Boolean) -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val noun = if (p.kind == PlaylistKind.VIDEO) "collection" else "playlist"
    var link by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    fun change(work: () -> Unit) {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching(work) }
                .onSuccess { onDone(true) }
                .onFailure { error = serverMessage(it, "Couldn't change the picture") }
            busy = false
        }
    }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri != null) change {
            val file = pictureFile(context, uri)
            try { marquee.playlists.uploadPlaylistImage(p.id, file) } finally { file.delete() }
        }
    }
    AlertDialog(onDismissRequest = { if (!busy) onDone(false) },
        title = { Text("Picture for “${p.title}”") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("Shown in place of the $noun's mosaic, to everyone it's shared with too.", style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (!marquee.isTv) OutlinedButton({ picker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) },
                    Modifier.focusRing(), enabled = !busy) {
                    Icon(Icons.Filled.Photo, null, Modifier.size(18.dp)); Text("Upload image", Modifier.padding(start = 6.dp))
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(link, { link = it }, Modifier.weight(1f), singleLine = true, label = { Text("Image link") }, placeholder = { Text("https://…") },
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false))
                    TextButton({ val url = link.trim(); change { marquee.playlists.setPlaylistImageLink(p.id, SetPlaylistImageLinkRequest(url)) } },
                        Modifier.focusRing(), enabled = !busy && link.isNotBlank()) { Text("Use link") }
                }
                if (p.imageVersion != null) OutlinedButton({ change { marquee.playlists.removePlaylistImage(p.id) } }, Modifier.focusRing(), enabled = !busy) {
                    Icon(Icons.Filled.Delete, null, Modifier.size(18.dp)); Text("Remove picture", Modifier.padding(start = 6.dp))
                }
                if (busy) Text("Saving…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                error?.let { Text(it, Modifier.fillMaxWidth(), color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton({ onDone(false) }, Modifier.focusRing(), enabled = !busy) { Text("Close") } })
}
