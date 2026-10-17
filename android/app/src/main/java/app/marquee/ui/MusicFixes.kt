package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.MusicMatchCandidate
import app.marquee.api.models.MusicMatchRequest
import app.marquee.api.models.TrackPlacementRequest
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Music artists and albums can be matched to a MusicBrainz entry (META-3). */
fun musicMatchable(type: ItemType) = type == ItemType.ARTIST || type == ItemType.ALBUM

private val musicTypeLabels = mapOf("ep" to "EP")

/** MusicBrainz's type ("person", "ep", "compilation") as a label. */
fun musicTypeLabel(t: String?): String =
    if (t.isNullOrBlank()) "" else musicTypeLabels[t] ?: t.replaceFirstChar { it.uppercaseChar() }

/** A candidate's second line: artist, type, year and tracks for albums; type, country and years for artists. */
fun musicCandidateDetails(c: MusicMatchCandidate, album: Boolean): String {
    val years = c.beginYear?.let { "$it–${c.endYear ?: ""}" }
    val parts = if (album) listOf(c.artist, musicTypeLabel(c.type), c.year?.toString(), c.trackCount?.takeIf { it > 0 }?.let { "$it tracks" })
        else listOf(musicTypeLabel(c.type), c.country, years)
    return parts.filterNot { it.isNullOrBlank() }.joinToString(" · ")
}

/**
 * Fix match for a music artist or album (admins, META-3): a MusicBrainz search pre-filled with the
 * name (and the artist, for an album); picking an entry and confirming applies it.
 * [onDone] gets the re-matched item, or null when cancelled.
 */
@Composable
fun MusicMatchDialog(item: ItemDetail, onDone: (ItemDetail?) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val album = item.type == ItemType.ALBUM
    var q by remember { mutableStateOf(item.title) }
    var artist by remember { mutableStateOf(if (album) item.parentTitle ?: item.artistCredit ?: "" else "") }
    var query by remember { mutableStateOf(q to artist) }
    var candidates by remember { mutableStateOf<List<MusicMatchCandidate>?>(null) }
    var searching by remember { mutableStateOf(false) }
    var applying by remember { mutableStateOf(false) }
    var confirm by remember { mutableStateOf<MusicMatchCandidate?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(query) {
        val (qq, aa) = query
        searching = true
        error = null
        withContext(Dispatchers.IO) {
            runCatching { marquee.items.searchMusicMatches(item.id, qq.ifBlank { null }, aa.takeIf { album && it.isNotBlank() }) }
        }.onSuccess { candidates = it }.onFailure { error = serverMessage(it); candidates = null }
        searching = false
    }
    fun apply(c: MusicMatchCandidate) {
        applying = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.applyMusicMatch(item.id, MusicMatchRequest(c.mbid)) } }
                .onSuccess { onDone(it) }
                .onFailure { error = serverMessage(it) }
            applying = false
        }
    }
    AlertDialog(
        onDismissRequest = { onDone(null) },
        title = { Text("Fix match") },
        text = {
            Column(Modifier.heightIn(max = 560.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("Choose the MusicBrainz ${if (album) "album" else "artist"}. Its details replace the current ones; fields you edited stay as they are. A MusicBrainz link or ID works too.",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(q, { q = it }, Modifier.fillMaxWidth().testTag("musicMatchQuery"), singleLine = true,
                    label = { Text(if (album) "Album title" else "Artist name") })
                if (album) OutlinedTextField(artist, { artist = it }, Modifier.fillMaxWidth().testTag("musicMatchArtist"), singleLine = true, label = { Text("Artist") })
                OutlinedButton({ query = q.trim() to artist.trim() }, Modifier.focusRing(), enabled = !searching && !applying) { Text("Search") }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (searching || applying) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                if (!searching && candidates?.isEmpty() == true) Text("No matches. Try a different spelling${if (album) " or clear the artist" else ""}.",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (!searching) candidates.orEmpty().forEach { c ->
                    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).focusCard({ if (!applying) confirm = c })
                        .semantics { contentDescription = "Match ${c.name}${c.disambiguation?.let { " ($it)" } ?: ""}" }.padding(8.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text(c.name, Modifier.weight(1f, fill = false), fontWeight = FontWeight.SemiBold)
                            if (c.current == true) Text("Current", style = MaterialTheme.typography.labelSmall, color = Gold)
                        }
                        c.disambiguation?.takeIf { it.isNotBlank() }?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        musicCandidateDetails(c, album).takeIf { it.isNotEmpty() }?.let {
                            Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton({ onDone(null) }, Modifier.focusRing()) { Text("Cancel") } },
    )
    confirm?.let { c ->
        AlertDialog(
            onDismissRequest = { confirm = null },
            title = { Text("Match to “${c.name}”?") },
            text = {
                Text(listOfNotNull(c.disambiguation?.takeIf { it.isNotBlank() }, musicCandidateDetails(c, album).takeIf { it.isNotEmpty() }).joinToString("\n")
                    .ifEmpty { "Details are fetched again from MusicBrainz." })
            },
            confirmButton = { Button({ confirm = null; apply(c) }, Modifier.focusRing().testTag("musicMatchConfirm")) { Text("Match") } },
            dismissButton = { TextButton({ confirm = null }, Modifier.focusRing()) { Text("Cancel") } },
        )
    }
}

/**
 * Move a track to another album in the same library, or a new one, without editing the file (admins,
 * META-3); "Use the file's tags" undoes a move. [onDone] gets the moved track, or null when cancelled.
 */
@Composable
fun MoveTrackDialog(trackId: Long, trackTitle: String, onDone: (ItemDetail?) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var newAlbum by remember { mutableStateOf(false) }
    var q by remember { mutableStateOf("") }
    var query by remember { mutableStateOf("") }
    var albumArtist by remember { mutableStateOf("") }
    var albumTitle by remember { mutableStateOf("") }
    var albums by remember { mutableStateOf<List<ItemSummary>?>(null) }
    var searching by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var confirm by remember { mutableStateOf<ItemSummary?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(trackId) {
        withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(trackId) } }
            .onSuccess { d ->
                detail = d
                q = d.parentTitle.orEmpty()
                query = q
                albumArtist = d.grandparentTitle ?: d.artistCredit ?: ""
            }
            .onFailure { error = serverMessage(it) }
    }
    LaunchedEffect(query, detail) {
        val d = detail ?: return@LaunchedEffect
        if (query.isBlank()) { albums = null; return@LaunchedEffect }
        searching = true
        error = null
        withContext(Dispatchers.IO) {
            runCatching { marquee.search.search(query, 50).groups.firstOrNull { it.type == ItemType.ALBUM }?.items.orEmpty().filter { it.libraryId == d.libraryId } }
        }.onSuccess { albums = it }.onFailure { error = serverMessage(it); albums = null }
        searching = false
    }
    fun run(call: () -> ItemDetail) {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching(call) }
                .onSuccess { onDone(it) }
                .onFailure { error = serverMessage(it) }
            busy = false
        }
    }
    AlertDialog(
        onDismissRequest = { onDone(null) },
        title = { Text("Move “$trackTitle”", maxLines = 2, overflow = TextOverflow.Ellipsis) },
        text = {
            Column(Modifier.heightIn(max = 560.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                val d = detail
                if (d == null && error == null) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                if (d != null) {
                    Text("The file isn't changed. The track keeps its plays, ratings and favourite, and stays on the album you choose when the library is scanned again.",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    if (d.placementOverride == true) Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant).padding(10.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text("This track was moved by hand.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        OutlinedButton({ run { marquee.items.clearTrackPlacement(trackId) } }, Modifier.focusRing().testTag("useFileTags"), enabled = !busy) { Text("Use the file's tags") }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        FilterChip(!newAlbum, { newAlbum = false }, { Text("Album in the library") }, Modifier.focusRing())
                        FilterChip(newAlbum, { newAlbum = true }, { Text("New album") }, Modifier.focusRing().testTag("moveNewAlbum"))
                    }
                    error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                    if (!newAlbum) {
                        OutlinedTextField(q, { q = it }, Modifier.fillMaxWidth().testTag("moveAlbumQuery"), singleLine = true, label = { Text("Search albums") })
                        OutlinedButton({ query = q.trim() }, Modifier.focusRing(), enabled = q.isNotBlank() && !searching && !busy) { Text("Search") }
                        if (searching || busy) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                        if (!searching && albums?.isEmpty() == true) Text("No albums found. Try another title, or make a new album.",
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (!searching) albums.orEmpty().forEach { a ->
                            val current = a.id == d.parentId
                            Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).focusCard({ if (!busy && !current) confirm = a })
                                .semantics { contentDescription = "Move to ${a.title}" }.padding(6.dp),
                                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                                Box(Modifier.size(48.dp).clip(RoundedCornerShape(4.dp)).background(MaterialTheme.colorScheme.surfaceVariant)) {
                                    marquee.imageUrl(a.images?.poster, 96)?.let { AsyncImage(it, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize()) }
                                }
                                Column(Modifier.weight(1f)) {
                                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                        Text(a.title, Modifier.weight(1f, fill = false), fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                        if (current) Text("Current", style = MaterialTheme.typography.labelSmall, color = Gold)
                                    }
                                    listOfNotNull(a.artistCredit ?: a.parentTitle, a.year?.toString()).joinToString(" · ").takeIf { it.isNotEmpty() }?.let {
                                        Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    }
                                }
                            }
                        }
                    } else {
                        OutlinedTextField(albumArtist, { albumArtist = it }, Modifier.fillMaxWidth().testTag("moveAlbumArtist"), singleLine = true, label = { Text("Album artist") })
                        OutlinedTextField(albumTitle, { albumTitle = it }, Modifier.fillMaxWidth().testTag("moveAlbumTitle"), singleLine = true, label = { Text("Album title") })
                        Text("An album with this artist and title is used if there is one; otherwise it's created.",
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (busy) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                        Button({ run { marquee.items.setTrackPlacement(trackId, TrackPlacementRequest(albumArtist = albumArtist.trim(), album = albumTitle.trim())) } },
                            Modifier.focusRing().testTag("moveNewAlbumSubmit"), enabled = !busy && albumArtist.isNotBlank() && albumTitle.isNotBlank()) { Text("Move") }
                    }
                }
                if (d == null) error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton({ onDone(null) }, Modifier.focusRing()) { Text("Cancel") } },
    )
    confirm?.let { a ->
        AlertDialog(
            onDismissRequest = { confirm = null },
            title = { Text("Move to “${a.title}”?") },
            text = { Text(listOfNotNull(a.artistCredit ?: a.parentTitle, a.year?.toString()).joinToString(" · ").ifEmpty { "The track moves to this album." }) },
            confirmButton = { Button({ confirm = null; run { marquee.items.setTrackPlacement(trackId, TrackPlacementRequest(albumId = a.id)) } }, Modifier.focusRing().testTag("moveConfirm")) { Text("Move") } },
            dismissButton = { TextButton({ confirm = null }, Modifier.focusRing()) { Text("Cancel") } },
        )
    }
}
