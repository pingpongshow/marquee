package app.marquee.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Sort
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.VerticalAlignBottom
import androidx.compose.material.icons.filled.VerticalAlignTop
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistEntry
import app.marquee.api.models.PlaylistKind
import app.marquee.api.models.PlaylistMerge
import app.marquee.api.models.SetPlaylistItemsRequest
import app.marquee.core.allPlaylistEntries
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// ---------- Editing and combining playlists (MUSIC-29) ----------

/** Ways the editor sorts a playlist. */
internal enum class PlaylistSort(val label: String) {
    Title("Title"), Artist("Artist"), Album("Album"), Added("Date added"), Shuffle("Shuffle"), Reverse("Reverse order")
}

/** The editor's changes to a list of entries, apart from the screen so they can be tested. */
internal object PlaylistEdits {
    private val words = compareBy<String, String>(String.CASE_INSENSITIVE_ORDER) { it.removePrefix("The ").removePrefix("the ") }

    /** The entries sorted [by]; ties keep their order. */
    fun sorted(entries: List<PlaylistEntry>, by: PlaylistSort): List<PlaylistEntry> = when (by) {
        PlaylistSort.Title -> entries.sortedWith(compareBy(words) { it.item.title })
        PlaylistSort.Artist -> entries.sortedWith(compareBy<PlaylistEntry, String>(words) { artist(it) }
            .thenBy { it.item.parentTitle.orEmpty().lowercase() }.thenBy { it.item.index ?: 0 })
        PlaylistSort.Album -> entries.sortedWith(compareBy<PlaylistEntry, String>(words) { it.item.parentTitle.orEmpty() }
            .thenBy { it.item.index ?: 0 })
        // Entries are numbered as they're added, so their ids are the order they went in.
        PlaylistSort.Added -> entries.sortedBy { it.entryId }
        PlaylistSort.Shuffle -> entries.shuffled()
        PlaylistSort.Reverse -> entries.reversed()
    }

    private fun artist(e: PlaylistEntry) = e.item.artistCredit ?: e.item.grandparentTitle ?: ""

    /** Each song or video once, where it first appears. */
    fun withoutDuplicates(entries: List<PlaylistEntry>): List<PlaylistEntry> = entries.distinctBy { it.item.id }

    /** How many entries [withoutDuplicates] would take out. */
    fun duplicates(entries: List<PlaylistEntry>): Int = entries.size - entries.distinctBy { it.item.id }.size

    /** The selected entries moved together to the top (or the bottom), in their order. */
    fun moved(entries: List<PlaylistEntry>, selected: Set<Long>, top: Boolean): List<PlaylistEntry> {
        val (picked, rest) = entries.partition { it.entryId in selected }
        return if (top) picked + rest else rest + picked
    }

    /** The entry at [i] swapped with its neighbour ([by] = -1 up, 1 down). */
    fun nudged(entries: List<PlaylistEntry>, i: Int, by: Int): List<PlaylistEntry> {
        val j = i + by
        if (i !in entries.indices || j !in entries.indices) return entries
        return entries.toMutableList().also { it[i] = entries[j]; it[j] = entries[i] }
    }
}

/** Every entry of a playlist, a page at a time (the server sends up to 2,000 at once). */
private suspend fun allEntries(marquee: app.marquee.core.Marquee, id: Long): List<PlaylistEntry> =
    withContext(Dispatchers.IO) { marquee.allPlaylistEntries(id) }

/**
 * Edit a playlist (MUSIC-29): pick entries to remove or move to the top or bottom, move one up or
 * down, sort the whole playlist, or take out duplicates. Nothing changes on the server until Save,
 * which sends the new order (and what's gone) in one go. Leaving with changes asks first.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun PlaylistEditScreen(nav: NavHostController, playlistId: Long) {
    val marquee = LocalMarquee.current
    val context = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    val playlist by produceState<Playlist?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.playlists.getPlaylist(playlistId) }.getOrNull() } }
    val entries = remember { mutableStateListOf<PlaylistEntry>() }
    val selected = remember { mutableStateListOf<Long>() }
    var original by remember { mutableStateOf<List<Long>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var saving by remember { mutableStateOf(false) }
    var sorting by remember { mutableStateOf(false) }
    var leaving by remember { mutableStateOf(false) }
    LaunchedEffect(playlistId) {
        runCatching { allEntries(marquee, playlistId) }
            .onSuccess { entries.clear(); entries.addAll(it); original = it.map { e -> e.entryId } }
            .onFailure { error = "Couldn't load the playlist: ${serverMessage(it)}" }
    }
    val changed = original != null && original != entries.map { it.entryId }
    fun set(list: List<PlaylistEntry>) { entries.clear(); entries.addAll(list) }
    fun save() {
        saving = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.playlists.setPlaylistItems(playlistId, SetPlaylistItemsRequest(entries.map { it.entryId })) } }
                .onSuccess { nav.popBackStack() }
                .onFailure { toast(context, "Couldn't save the playlist: ${serverMessage(it)}") }
            saving = false
        }
    }
    BackHandler(enabled = changed) { leaving = true }
    val music = playlist?.kind != PlaylistKind.VIDEO
    val dupes = PlaylistEdits.duplicates(entries)

    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp), modifier = Modifier.testTag("playlistEditor")) {
        item {
            Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    Column(Modifier.weight(1f)) {
                        Text("Edit playlist", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(playlist?.title ?: "", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold, maxLines = 2)
                    }
                    TextButton({ if (changed) leaving = true else nav.popBackStack() }, Modifier.focusRing()) { Text("Cancel") }
                    Button(::save, Modifier.focusRing().initialFocus(marquee.isTv), enabled = changed && !saving) { Text(if (saving) "Saving…" else "Save") }
                }
                Text(buildString {
                    append(if (entries.size == 1) "1 ${if (music) "song" else "video"}" else "${entries.size} ${if (music) "songs" else "videos"}")
                    if (selected.isNotEmpty()) append(" · ${selected.size} selected")
                    if (changed) append(" · not saved yet")
                }, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton({ if (selected.size == entries.size) selected.clear() else { selected.clear(); selected.addAll(entries.map { it.entryId }) } },
                        Modifier.focusRing(), enabled = entries.isNotEmpty()) {
                        Text(if (selected.size == entries.size && entries.isNotEmpty()) "Select none" else "Select all", maxLines = 1, softWrap = false)
                    }
                    OutlinedButton({ set(entries.filter { it.entryId !in selected }); selected.clear() }, Modifier.focusRing(), enabled = selected.isNotEmpty()) {
                        Icon(Icons.Filled.Delete, null); Text("Remove", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                    }
                    OutlinedButton({ set(PlaylistEdits.moved(entries, selected.toSet(), top = true)) }, Modifier.focusRing(), enabled = selected.isNotEmpty()) {
                        Icon(Icons.Filled.VerticalAlignTop, null); Text("To top", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                    }
                    OutlinedButton({ set(PlaylistEdits.moved(entries, selected.toSet(), top = false)) }, Modifier.focusRing(), enabled = selected.isNotEmpty()) {
                        Icon(Icons.Filled.VerticalAlignBottom, null); Text("To bottom", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                    }
                    Box {
                        OutlinedButton({ sorting = true }, Modifier.focusRing(), enabled = entries.size > 1) {
                            Icon(Icons.AutoMirrored.Filled.Sort, null); Text("Sort", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                        DropdownMenu(sorting, { sorting = false }) {
                            PlaylistSort.entries.filter { music || (it != PlaylistSort.Artist && it != PlaylistSort.Album) }.forEach { s ->
                                DropdownMenuItem({ Text(s.label) }, { sorting = false; set(PlaylistEdits.sorted(entries, s)) })
                            }
                        }
                    }
                    OutlinedButton({ set(PlaylistEdits.withoutDuplicates(entries)); selected.retainAll(entries.map { it.entryId }.toSet()) },
                        Modifier.focusRing(), enabled = dupes > 0) {
                        Icon(Icons.Filled.ContentCopy, null)
                        Text(if (dupes > 0) "Remove $dupes duplicate${if (dupes == 1) "" else "s"}" else "No duplicates", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                    }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (original == null && error == null) Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            }
        }
        itemsIndexed(entries, key = { _, e -> e.entryId }) { i, e ->
            val on = e.entryId in selected
            fun toggle() { if (on) selected.remove(e.entryId) else selected.add(e.entryId) }
            Row(Modifier.fillMaxWidth().padding(horizontal = sidePadding - 8.dp)
                .background(if (on) Gold.copy(alpha = 0.10f) else MaterialTheme.colorScheme.background)
                .semantics { stateDescription = if (on) "Selected" else "Not selected" },
                verticalAlignment = Alignment.CenterVertically) {
                Checkbox(on, { toggle() }, Modifier.focusRing().semantics { contentDescription = "Select ${e.item.title}" })
                Column(Modifier.weight(1f).focusCard(::toggle).padding(vertical = 10.dp, horizontal = 4.dp)) {
                    Text(e.item.title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(subtitleFor(e.item), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
                        overflow = TextOverflow.Ellipsis)
                }
                IconButton({ set(PlaylistEdits.nudged(entries, i, -1)) }, Modifier.focusRing(), enabled = i > 0) { Icon(Icons.Filled.KeyboardArrowUp, "Move ${e.item.title} up") }
                IconButton({ set(PlaylistEdits.nudged(entries, i, 1)) }, Modifier.focusRing(), enabled = i < entries.size - 1) {
                    Icon(Icons.Filled.KeyboardArrowDown, "Move ${e.item.title} down")
                }
            }
        }
    }
    if (leaving) AlertDialog(onDismissRequest = { leaving = false },
        title = { Text("Discard your changes?") },
        text = { Text("The playlist stays as it was.") },
        confirmButton = { TextButton({ leaving = false; nav.popBackStack() }, Modifier.focusRing()) { Text("Discard", color = MaterialTheme.colorScheme.error) } },
        dismissButton = { TextButton({ leaving = false }, Modifier.focusRing().initialFocus(marquee.isTv)) { Text("Keep editing") } })
}

/**
 * Combine playlists (MUSIC-29): pick two or more of one kind (yours, shared with you or smart),
 * then put them into a new playlist or onto the end of one of yours, leaving out duplicates if
 * you like, and deleting your originals afterwards if you like. [onDone] has the playlist they
 * went into, or null when cancelled.
 */
@Composable
fun CombinePlaylistsDialog(playlists: List<Playlist>, onDone: (Playlist?) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val picked = remember { mutableStateListOf<Long>() }
    var intoNew by remember { mutableStateOf(true) }
    var into by remember { mutableStateOf<Long?>(null) }
    var title by remember { mutableStateOf("") }
    var skipDuplicates by remember { mutableStateOf(true) }
    var deleteSources by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // One kind at a time: the first picked decides.
    val kind = playlists.firstOrNull { it.id in picked }?.kind
    val sources = playlists.filter { it.id in picked }
    // Where they can go: your own (not smart) playlists of the same kind, not already a source.
    val targets = playlists.filter { it.readOnly != true && it.rules == null && (kind == null || it.kind == kind) && it.id !in picked }
    val ready = picked.size >= (if (intoNew) 2 else 1) && (intoNew || into != null)
    fun combine() {
        busy = true
        error = null
        scope.launch {
            val body = PlaylistMerge(sourceIds = picked.toList(),
                intoId = if (intoNew) null else into, title = if (intoNew) title.trim().ifEmpty { null } else null,
                skipDuplicates = skipDuplicates, deleteSources = deleteSources)
            withContext(Dispatchers.IO) { runCatching { marquee.playlists.mergePlaylists(body) } }
                .onSuccess { onDone(it) }
                .onFailure { error = serverMessage(it, "Couldn't combine them") }
            busy = false
        }
    }
    AlertDialog(
        onDismissRequest = { onDone(null) },
        title = { Text("Combine playlists") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("Pick the playlists to combine, in the order they should go in.", style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                Column(Modifier.heightIn(max = 260.dp).verticalScroll(rememberScrollState())) {
                    playlists.forEach { p ->
                        val on = p.id in picked
                        val usable = kind == null || p.kind == kind
                        Row(Modifier.fillMaxWidth().focusCard({ if (on) picked.remove(p.id) else if (usable) { picked.add(p.id); if (into == p.id) into = null } })
                            .padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(on, null, enabled = usable)
                            Column(Modifier.weight(1f)) {
                                Text(p.title, maxLines = 1, overflow = TextOverflow.Ellipsis,
                                    color = if (usable) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant)
                                Text(listOfNotNull("${p.itemCount} ${if (p.kind == PlaylistKind.AUDIO) "songs" else "videos"}", playlistSharing(p),
                                    if (p.rules != null) "Smart" else null).joinToString(" · "),
                                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            if (on) Text("${picked.indexOf(p.id) + 1}", color = Gold, fontWeight = FontWeight.Bold, modifier = Modifier.padding(end = 8.dp))
                        }
                    }
                }
                Row(Modifier.fillMaxWidth().focusCard({ intoNew = true }), verticalAlignment = Alignment.CenterVertically) {
                    RadioButton(intoNew, null); Text("Into a new playlist")
                }
                if (intoNew) OutlinedTextField(title, { title = it }, Modifier.fillMaxWidth(), singleLine = true, label = { Text("Name") },
                    placeholder = { Text(sources.firstOrNull()?.title ?: "Combined") })
                Row(Modifier.fillMaxWidth().focusCard({ intoNew = false }), verticalAlignment = Alignment.CenterVertically) {
                    RadioButton(!intoNew, null, enabled = targets.isNotEmpty()); Text("Onto the end of one of yours")
                }
                if (!intoNew) targets.forEach { p ->
                    Row(Modifier.fillMaxWidth().padding(start = 24.dp).focusCard({ into = p.id }), verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(into == p.id, null); Text(p.title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
                Row(Modifier.fillMaxWidth().focusCard({ skipDuplicates = !skipDuplicates }), verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(skipDuplicates, null); Text("Leave out duplicates")
                }
                Row(Modifier.fillMaxWidth().focusCard({ deleteSources = !deleteSources }), verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(deleteSources, null)
                    Text("Delete the originals afterwards" + if (sources.any { it.readOnly == true }) " (yours only)" else "")
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            Button(::combine, Modifier.focusRing(), enabled = ready && !busy) {
                if (busy) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp) else Text("Combine")
            }
        },
        dismissButton = { TextButton({ onDone(null) }, Modifier.focusRing()) { Text("Cancel") } },
    )
}
