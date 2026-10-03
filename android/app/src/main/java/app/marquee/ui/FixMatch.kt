package app.marquee.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.marquee.api.infrastructure.ClientError
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.infrastructure.ServerError
import app.marquee.api.infrastructure.ServerException
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.ItemType
import app.marquee.api.models.MatchCandidate
import app.marquee.api.models.MatchRequest
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The server's own message for a failed call ({code, message}), else the exception's. */
fun serverMessage(e: Throwable): String {
    val body = when (e) {
        is ClientException -> (e.response as? ClientError<*>)?.body
        is ServerException -> (e.response as? ServerError<*>)?.body
        else -> null
    }?.toString()
    val msg = body?.let { runCatching { org.json.JSONObject(it).optString("message") }.getOrNull() }?.takeIf { it.isNotBlank() }
    return msg ?: e.message ?: "That didn't work"
}

/** Movies and shows can be matched again (TMDB). */
fun matchable(type: ItemType) = type == ItemType.MOVIE || type == ItemType.SHOW

/** A path's base name and its folder. */
fun splitPath(path: String): Pair<String, String> {
    val p = path.trimEnd('/', '\\')
    val i = maxOf(p.lastIndexOf('/'), p.lastIndexOf('\\'))
    return if (i < 0) p to "" else p.substring(i + 1) to p.substring(0, i)
}

/** A file's name, with its folder in small text below; a long press shows the whole path. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun FileName(path: String, modifier: Modifier = Modifier, label: String? = null) {
    val (name, folder) = splitPath(path)
    var full by remember(path) { mutableStateOf(false) }
    Column(modifier.fillMaxWidth().combinedClickable(onClick = { full = !full }, onLongClick = { full = !full })
        .semantics { contentDescription = "File $name" }) {
        Text(if (label != null) "$name · $label" else name, maxLines = if (full) Int.MAX_VALUE else 1, overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        if (folder.isNotEmpty()) Text(folder, maxLines = if (full) Int.MAX_VALUE else 1, overflow = TextOverflow.Ellipsis,
            style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * Fix match (admins): the item's files at the top so it's clear what's being matched, then a
 * TMDB search by title and year (pre-filled) and its candidates; picking one applies it.
 * [onDone] gets the re-matched item, or null when cancelled.
 */
@Composable
fun FixMatchDialog(itemId: Long, onDone: (ItemDetail?) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var title by remember { mutableStateOf("") }
    var year by remember { mutableStateOf("") }
    var query by remember { mutableStateOf<Pair<String, Int?>?>(null) }
    var candidates by remember { mutableStateOf<List<MatchCandidate>?>(null) }
    var searching by remember { mutableStateOf(false) }
    var applying by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(itemId) {
        withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(itemId) } }
            .onSuccess { d ->
                detail = d
                title = d.title
                year = d.year?.toString().orEmpty()
                query = d.title to d.year
            }
            .onFailure { error = serverMessage(it) }
    }
    LaunchedEffect(query) {
        val q = query ?: return@LaunchedEffect
        searching = true
        error = null
        withContext(Dispatchers.IO) { runCatching { marquee.items.searchMatches(itemId, q.first, q.second) } }
            .onSuccess { candidates = it }
            .onFailure { error = serverMessage(it); candidates = null }
        searching = false
    }
    fun apply(c: MatchCandidate) {
        applying = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.applyMatch(itemId, MatchRequest(MatchRequest.Provider.TMDB, c.id)) } }
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
                val d = detail
                if (d == null && error == null) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                if (d != null) {
                    // What's being matched: each version's files (shows have none of their own).
                    val files = d.versions.flatMap { v -> v.files.mapNotNull { f -> f.path?.let { it to v.label.takeIf { l -> l.isNotBlank() && d.versions.size > 1 } } } }
                    if (files.isNotEmpty()) Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant)
                        .padding(10.dp).testTag("matchFiles"), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(if (files.size == 1) "File" else "Files", style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        files.forEach { (path, label) -> FileName(path, label = label) }
                    }
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedTextField(title, { title = it }, Modifier.weight(1f).testTag("matchTitle"), singleLine = true, label = { Text("Title") })
                        OutlinedTextField(year, { v -> year = v.filter(Char::isDigit).take(4) }, Modifier.width(92.dp).testTag("matchYear"), singleLine = true,
                            label = { Text("Year") }, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                    }
                    OutlinedButton({ query = title.trim() to year.toIntOrNull() }, Modifier.focusRing(), enabled = title.isNotBlank() && !searching) { Text("Search") }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (searching || applying) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                if (!searching && candidates?.isEmpty() == true) Text("No matches. Try a different title or remove the year.",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (!searching) candidates.orEmpty().forEach { c ->
                    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).focusCard({ if (!applying) apply(c) })
                        .semantics { contentDescription = "Match ${c.title}${c.year?.let { " ($it)" } ?: ""}" }.padding(6.dp),
                        horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        Box(Modifier.width(52.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(4.dp)).background(MaterialTheme.colorScheme.surfaceVariant)) {
                            c.posterUrl?.let { AsyncImage(it, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize()) }
                        }
                        Column(Modifier.weight(1f)) {
                            Text(c.title + (c.year?.let { " ($it)" } ?: ""), fontWeight = FontWeight.SemiBold)
                            if (c.current == true) Text("Current", style = MaterialTheme.typography.labelSmall, color = Gold)
                            c.originalTitle?.takeIf { it != c.title }?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            c.overview?.let { Text(it, maxLines = 3, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton({ onDone(null) }, Modifier.focusRing()) { Text("Cancel") } },
    )
}
