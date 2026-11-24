package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Edit
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
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistCreate
import app.marquee.api.models.PlaylistEntry
import app.marquee.api.models.PlaylistKind
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// ---------- Collections (USER-23): a person's own groups of titles, kept in their video library ----------
// To the server a collection is a playlist of kind video with a library; nothing plays through one.

/** "1 title", "12 titles". */
internal fun titlesCount(n: Int): String = if (n == 1) "1 title" else "$n titles"

/** Whether people may keep their own collections in this library (its personalCollections option); null until it's known. */
@Composable
fun personalCollectionsOn(libraryId: Long): Boolean? {
    val marquee = LocalMarquee.current
    return produceState<Boolean?>(null, libraryId) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.libraries.getLibrary(libraryId).options.personalCollections == true }.getOrDefault(false) }
    }.value
}

/** One of the person's collections in its library's grid: its poster mosaic, its size, and whose it is when it was shared. */
@Composable
fun CollectionCard(p: Playlist, width: Dp, autoFocus: Boolean = false, onClick: () -> Unit) {
    Column(Modifier.width(width)) {
        PlaylistMosaic(p, Modifier.fillMaxWidth().initialFocus(autoFocus).focusCard(onClick).semantics { contentDescription = p.title })
        Text(p.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text(listOfNotNull(titlesCount(p.itemCount), playlistSharing(p)).joinToString(" · "), maxLines = 1, overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Names a new, empty collection in a library. [onDone] has it, or null when cancelled. */
@Composable
fun NewCollectionDialog(libraryId: Long, onDone: (Playlist?) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    AlertDialog(onDismissRequest = { onDone(null) },
        title = { Text("New collection") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                OutlinedTextField(name, { name = it }, Modifier.initialFocus(marquee.isTv), label = { Text("Name") }, singleLine = true)
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            TextButton({
                busy = true
                error = null
                scope.launch {
                    withContext(Dispatchers.IO) { runCatching { marquee.playlists.createPlaylist(PlaylistCreate(name.trim(), PlaylistKind.VIDEO, libraryId = libraryId)) } }
                        .onSuccess { onDone(it) }
                        .onFailure { error = serverMessage(it, "Couldn't make the collection") }
                    busy = false
                }
            }, Modifier.focusRing(), enabled = !busy && name.isNotBlank()) { Text("Create") }
        },
        dismissButton = { TextButton({ onDone(null) }, Modifier.focusRing()) { Text("Cancel") } })
}

/**
 * A collection's page: its titles as a poster grid, each opening its own page. The owner can
 * edit it (reorder and remove), share, rename and delete it, and anyone can pin it to Home; one
 * shared with this person can only leave their collections. [entries] is null while loading.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun CollectionPage(nav: NavHostController, p: Playlist, entries: List<PlaylistEntry>?, editable: Boolean, onOpen: (Long) -> Unit, opened: Long?,
    onRemove: (entryId: Long, title: String) -> Unit, onChanged: () -> Unit) {
    val marquee = LocalMarquee.current
    val min = if (marquee.isTv) 130.dp else 110.dp
    LazyVerticalGrid(GridCells.Adaptive(min), Modifier.semantics { contentDescription = "Collection titles" }, contentPadding = PaddingValues(sidePadding),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(key = "head", span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(p.title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                Text(listOfNotNull("Collection", titlesCount(entries?.size ?: p.itemCount), playlistSharing(p)).joinToString(" · "),
                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                // Full width, wrapping onto another line rather than squeezing a button's label.
                FlowRow(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (editable && !entries.isNullOrEmpty()) OutlinedButton({ nav.navigate("playlist/${p.id}/edit") }, Modifier.focusRing().testTag("editPlaylist")) {
                        Icon(Icons.Filled.Edit, null); Text("Edit", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                    }
                    PinToHomeButton("playlist-${p.id}")
                    if (p.readOnly == true) RemoveSharedPlaylistButton(p, nav) else PlaylistMenu(p, nav, onChanged)
                }
                if (entries != null && entries.isEmpty()) Text(
                    if (editable) "Nothing in this collection yet. Choose Add to my collection on a title's page to put it here." else "Nothing in this collection yet.",
                    Modifier.padding(top = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        val list = entries ?: emptyList()
        items(list.size, key = { list[it].entryId }) { i ->
            val e = list[i]
            val it = e.item
            val remove = if (editable) listOf(TileAction("Remove from collection") { onRemove(e.entryId, it.title) }) else emptyList()
            PosterCard(it, marquee.imageUrl(it.images?.poster ?: it.images?.thumb, 240), min, { onOpen(it.id); openItem(nav, it) },
                autoFocus = marquee.isTv && (if (opened != null) it.id == opened else i == 0), actions = tileActions(it, nav = nav) + remove)
        }
    }
}
