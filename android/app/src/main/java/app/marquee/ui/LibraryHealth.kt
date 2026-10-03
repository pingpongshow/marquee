package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.HealthCheck
import app.marquee.api.models.HealthIssue
import app.marquee.api.models.IssueFile
import app.marquee.api.models.MediaFile
import app.marquee.api.models.MediaStream
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedButton
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.ui.platform.testTag
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private fun severityColour(s: HealthCheck.Severity): Color = when (s) {
    HealthCheck.Severity.ERROR -> Color(0xFFE5484D)
    HealthCheck.Severity.WARNING -> Color(0xFFF5A524)
    HealthCheck.Severity.INFO -> Color(0xFF3E8EF7)
}

/** Settings → Library Health (ADM-11), for admins: the checks, each with its count. */
@Composable
fun LibraryHealthScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    var checks by remember { mutableStateOf<List<HealthCheck>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) { runCatching { marquee.libraries.libraryHealth() } }
            .onSuccess { checks = it }.onFailure { error = it.message }
    }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Library Health", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        item {
            Text("Problems Marquee found in your libraries. Fix them, or ignore the ones that are fine.", Modifier.padding(horizontal = sidePadding),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        error?.let { e -> item { Text(e, Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.error) } }
        val list = checks
        if (list == null && error == null) item { CircularProgressIndicator(Modifier.padding(sidePadding)) }
        items(list.orEmpty(), key = { it.id.value }) { c ->
            val available = c.available != false
            Row(
                Modifier.fillMaxWidth().padding(horizontal = sidePadding, vertical = 6.dp).alpha(if (available) 1f else 0.45f)
                    .clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surface)
                    .then(if (available && c.count > 0) Modifier.focusCard({ nav.navigate("health/${c.id.value}") }, RoundedCornerShape(12.dp)) else Modifier)
                    .semantics { contentDescription = "${c.title}: ${if (available) c.count.toString() else "unavailable"}" },
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.width(6.dp).height(84.dp).background(if (available) severityColour(c.severity) else MaterialTheme.colorScheme.onSurfaceVariant))
                Column(Modifier.weight(1f).padding(horizontal = 14.dp, vertical = 10.dp)) {
                    Text(c.title, fontWeight = FontWeight.SemiBold)
                    Text(c.description, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3,
                        overflow = TextOverflow.Ellipsis)
                    if (!available) Text("Unavailable", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (available) Text("${c.count}", Modifier.padding(end = 16.dp), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold,
                    color = if (c.count > 0) severityColour(c.severity) else MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/**
 * One check's issues (ADM-11), a page at a time: the title, what's wrong, where the file is and,
 * for duplicates, the other copies. Open, Refresh metadata, Ignore (with Undo) and, for missing
 * subtitles, Download with Bazarr.
 */
@Composable
fun HealthIssuesScreen(nav: NavHostController, checkId: String) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    val issues = remember { mutableStateListOf<HealthIssue>() }
    var total by remember { mutableIntStateOf(-1) }
    var offset by remember { mutableIntStateOf(0) }
    var loading by remember { mutableStateOf(false) }
    var title by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var bazarr by remember { mutableStateOf<HealthIssue?>(null) }
    var match by remember { mutableStateOf<HealthIssue?>(null) }
    // Deleting duplicate files (admins, only with the server's "Allow deleting media files" on).
    var allowDeletion by remember { mutableStateOf(false) }
    var pendingDelete by remember { mutableStateOf<IssueFile?>(null) }
    var deleting by remember { mutableStateOf(false) }
    fun loadMore() {
        if (loading || (total >= 0 && offset >= total)) return
        loading = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.libraries.libraryHealthIssues(checkId, offset, 50) } }
                .onSuccess { p -> issues.addAll(p.items.filter { n -> issues.none { it.item.id == n.item.id && it.fileId == n.fileId } }); total = p.total; offset = p.offset + p.items.size }
                .onFailure { error = it.message }
            loading = false
        }
    }
    /** Starts the list again from the top (after a delete changes it). */
    fun reload() {
        issues.clear(); total = -1; offset = 0
        loadMore()
    }
    LaunchedEffect(checkId) {
        allowDeletion = withContext(Dispatchers.IO) { runCatching { marquee.settings.getSettings().library.allowMediaDeletion == true } }.getOrDefault(false)
        title = withContext(Dispatchers.IO) { runCatching { marquee.libraries.libraryHealth().firstOrNull { it.id.value == checkId }?.title } }.getOrNull() ?: ""
        loadMore()
    }
    fun ignore(issue: HealthIssue) {
        val at = issues.indexOf(issue)
        issues.remove(issue)
        scope.launch {
            val ok = withContext(Dispatchers.IO) { runCatching { marquee.libraries.ignoreHealthIssue(checkId, issue.item.id) } }
            if (ok.isFailure) { issues.add(at.coerceIn(0, issues.size), issue); snackbar.showSnackbar("Couldn't ignore it: ${ok.exceptionOrNull()?.message}"); return@launch }
            if (snackbar.showSnackbar("Ignored “${issue.item.title}”", actionLabel = "Undo", duration = SnackbarDuration.Long) == SnackbarResult.ActionPerformed) {
                withContext(Dispatchers.IO) { runCatching { marquee.libraries.unignoreHealthIssue(checkId, issue.item.id) } }
                    .onSuccess { issues.add(at.coerceIn(0, issues.size), issue) }
                    .onFailure { snackbar.showSnackbar("Couldn't undo: ${it.message}") }
            }
        }
    }
    fun delete(f: IssueFile) {
        deleting = true
        scope.launch {
            val r = withContext(Dispatchers.IO) { runCatching { marquee.libraries.deleteMediaFile(f.file.id) } }
            deleting = false
            pendingDelete = null
            r.onSuccess {
                reload()
                snackbar.showSnackbar("Moved “${splitPath(f.file.path ?: f.itemTitle).first}” to the trash")
            }.onFailure { snackbar.showSnackbar("Couldn't delete it: ${serverMessage(it)}") }
        }
    }
    fun refresh(issue: HealthIssue) = scope.launch {
        val r = withContext(Dispatchers.IO) { runCatching { marquee.items.refreshItem(issue.item.id) } }
        snackbar.showSnackbar(if (r.isSuccess) "Refreshing “${issue.item.title}”" else "Couldn't refresh: ${r.exceptionOrNull()?.message}")
    }

    Scaffold(snackbarHost = { SnackbarHost(snackbar) }, containerColor = MaterialTheme.colorScheme.background,
        contentWindowInsets = androidx.compose.foundation.layout.WindowInsets(0)) { pad ->
        LazyColumn(Modifier.fillMaxSize().padding(pad), contentPadding = PaddingValues(vertical = 16.dp)) {
            item {
                Column(Modifier.padding(horizontal = sidePadding, vertical = 8.dp)) {
                    Text(title.ifBlank { "Issues" }, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                    if (total >= 0) Text(if (total == 1) "1 issue" else "$total issues", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            error?.let { e -> item { Text(e, Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.error) } }
            if (total == 0 || (total > 0 && issues.isEmpty() && !loading)) item {
                Text("Nothing to fix here.", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            items(issues.size, key = { "${issues[it].item.id}-${issues[it].fileId}" }) { i ->
                val issue = issues[i]
                if (i >= issues.size - 10) LaunchedEffect(i) { loadMore() }
                IssueRow(issue, checkId,
                    onOpen = { openItem(nav, issue.item) }, onRefresh = { refresh(issue) }, onIgnore = { ignore(issue) },
                    onBazarr = { bazarr = issue }, onRelated = { nav.navigate("item/${it}") }, onMatch = { match = issue })
                if (checkId == HealthCheck.Id.DUPLICATES.value && (issue.files?.size ?: 0) > 1)
                    DuplicateComparison(issue.files!!, allowDeletion) { pendingDelete = it }
                HorizontalDivider()
            }
            if (loading) item { CircularProgressIndicator(Modifier.padding(sidePadding)) }
        }
    }
    bazarr?.let { b -> BazarrDialog(b.item.id, b.item.title) { bazarr = null } }
    match?.let { m ->
        FixMatchDialog(m.item.id) { updated ->
            match = null
            if (updated != null) scope.launch { snackbar.showSnackbar("Matched “${updated.title}”") }
        }
    }
    pendingDelete?.let { f ->
        val name = splitPath(f.file.path ?: f.itemTitle).first
        AlertDialog(
            onDismissRequest = { if (!deleting) pendingDelete = null },
            title = { Text("Delete this file?") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("“$name”", fontWeight = FontWeight.SemiBold)
                    Text("It moves to the library's .marquee-trash folder, along with subtitles and artwork that belong only to it, and is removed for good after 30 days.")
                }
            },
            confirmButton = {
                TextButton({ delete(f) }, Modifier.focusRing(), enabled = !deleting) { Text("Move to trash", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = { TextButton({ pendingDelete = null }, Modifier.focusRing(), enabled = !deleting) { Text("Cancel") } },
        )
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun IssueRow(issue: HealthIssue, checkId: String, onOpen: () -> Unit, onRefresh: () -> Unit, onIgnore: () -> Unit, onBazarr: () -> Unit, onRelated: (Long) -> Unit,
    onMatch: () -> Unit) {
    val marquee = LocalMarquee.current
    val it = issue.item
    Row(Modifier.fillMaxWidth().padding(horizontal = sidePadding, vertical = 10.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        Artwork(marquee.imageUrl(it.images?.poster ?: it.images?.thumb, 160), it.title, shapeFor(it), Modifier.width(64.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(it.title, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
            subtitleFor(it).takeIf { s -> s.isNotBlank() }?.let { s -> Text(s, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            Text(issue.detail, style = MaterialTheme.typography.bodyMedium)
            issue.path?.let { p -> Text(p, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis) }
            issue.related?.takeIf { r -> r.isNotEmpty() }?.let { r ->
                Text("Other copies:", Modifier.padding(top = 4.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                r.forEach { o ->
                    Text("• ${o.title}${o.year?.let { y -> " ($y)" } ?: ""}", Modifier.focusCard({ onRelated(o.id) }).padding(vertical = 2.dp),
                        style = MaterialTheme.typography.bodySmall, color = Gold)
                }
            }
            androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(2.dp)) {
                TextButton(onOpen, Modifier.focusRing()) { Text("Open") }
                TextButton(onRefresh, Modifier.focusRing()) { Text("Refresh metadata") }
                if (matchable(it.type)) TextButton(onMatch, Modifier.focusRing().semantics { contentDescription = "Fix match for ${it.title}" }) { Text("Fix match…") }
                if (checkId == HealthCheck.Id.MISSING_SUBTITLES.value) TextButton(onBazarr, Modifier.focusRing()) { Text("Download with Bazarr") }
                TextButton(onIgnore, Modifier.focusRing().semantics { contentDescription = "Ignore ${it.title}" }) { Text("Ignore") }
            }
        }
    }
}


/** One comparable fact about a file: what to show, and a score where higher is better (null: not compared). */
private class Fact(val label: String, val text: String, val score: Double? = null)

private fun resolution(f: MediaFile): Pair<String, Double>? {
    val v = f.streams.firstOrNull { it.kind == MediaStream.Kind.VIDEO }
    val w = f.width ?: v?.width ?: return null
    val h = f.height ?: v?.height ?: return null
    val name = when {
        w >= 3200 || h >= 2000 -> "4K"
        w >= 1800 || h >= 1000 -> "1080p"
        w >= 1200 || h >= 700 -> "720p"
        else -> "SD"
    }
    return "$name · $w×$h" to (w.toDouble() * h)
}

private fun channels(n: Int): String = when (n) {
    1 -> "1.0"; 2 -> "2.0"; 3 -> "2.1"; 6 -> "5.1"; 7 -> "6.1"; 8 -> "7.1"; else -> "$n ch"
}

private val hdrNames = mapOf(
    MediaFile.HdrFormat.HDR10 to "HDR10", MediaFile.HdrFormat.HDR10PLUS to "HDR10+",
    MediaFile.HdrFormat.HLG to "HLG", MediaFile.HdrFormat.DOLBY_VISION to "Dolby Vision",
)

private val addedFormat = java.time.format.DateTimeFormatter.ofLocalizedDate(java.time.format.FormatStyle.MEDIUM)

private fun facts(i: IssueFile): List<Fact> {
    val f = i.file
    val audio = f.streams.firstOrNull { it.kind == MediaStream.Kind.AUDIO }
    val subs = f.streams.count { it.kind == MediaStream.Kind.SUBTITLE }
    val res = resolution(f)
    return listOfNotNull(
        i.versionLabel?.takeIf { it.isNotBlank() }?.let { Fact("Edition", it) },
        Fact("Size", if (f.propertySize >= 100_000_000) "%.2f GB".format(f.propertySize / 1e9) else "%.1f MB".format(f.propertySize / 1e6)),
        Fact("Resolution", res?.first ?: "Unknown", res?.second ?: 0.0),
        Fact("Video", f.videoCodec?.uppercase() ?: "Unknown"),
        Fact("HDR", f.hdrFormat?.let { hdrNames[it] } ?: "SDR", if (f.hdrFormat != null) 1.0 else 0.0),
        Fact("Bitrate", f.bitrateKbps?.let { "%.1f Mbps".format(it / 1000.0) } ?: "Unknown", f.bitrateKbps?.toDouble() ?: 0.0),
        Fact("Audio", audio?.let { a -> listOfNotNull(a.codec.uppercase(), a.channels?.let(::channels)).joinToString(" ") } ?: "None",
            audio?.channels?.toDouble() ?: 0.0),
        Fact("Subtitles", "$subs", subs.toDouble()),
        Fact("Duration", f.durationMs?.let { formatTime(it) } ?: "Unknown"),
        Fact("Container", f.container?.uppercase() ?: "Unknown"),
        Fact("Added", i.addedAt.atZoneSameInstant(java.time.ZoneId.systemDefault()).format(addedFormat)),
    )
}

/**
 * Duplicates (ADM-11): every file of every copy side by side, the better value in each
 * comparable row highlighted, each with Delete… (to the trash) when the server allows it.
 * The last file left can't be deleted from here.
 */
@Composable
private fun DuplicateComparison(files: List<IssueFile>, allowDeletion: Boolean, onDelete: (IssueFile) -> Unit) {
    val all = files.map { facts(it) }
    // Per label, the best score when the files differ.
    val best = all.flatten().filter { it.score != null }.groupBy { it.label }
        .mapValues { (_, fs) -> fs.mapNotNull { it.score }.let { sc -> if (sc.distinct().size > 1) sc.max() else null } }
    Column(Modifier.fillMaxWidth().padding(bottom = 12.dp).testTag("duplicateComparison")) {
        Text("Compare files", Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold)
        if (!allowDeletion) Text("Turn on 'Allow deleting media files' in Settings → Libraries to delete from here.",
            Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            files.forEachIndexed { idx, f ->
                val name = splitPath(f.file.path ?: f.itemTitle).first
                Column(Modifier.width(260.dp).clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surface).padding(12.dp)
                    .semantics { contentDescription = "Duplicate file $name" }, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(f.itemTitle, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    f.file.path?.let { FileName(it) } ?: Text(name, fontWeight = FontWeight.Medium)
                    all[idx].forEach { fact ->
                        val better = fact.score != null && best[fact.label] != null && fact.score == best[fact.label]
                        Row(Modifier.fillMaxWidth().semantics { if (better) contentDescription = "Better ${fact.label}: ${fact.text}" }) {
                            Text(fact.label, Modifier.width(84.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            Text(fact.text, style = MaterialTheme.typography.bodySmall, color = if (better) Gold else MaterialTheme.colorScheme.onSurface,
                                fontWeight = if (better) FontWeight.Bold else FontWeight.Normal)
                        }
                    }
                    if (allowDeletion) OutlinedButton({ onDelete(f) }, Modifier.padding(top = 6.dp).focusRing().semantics { contentDescription = "Delete $name" },
                        enabled = files.size > 1) { Text("Delete…", color = if (files.size > 1) MaterialTheme.colorScheme.error else Color.Unspecified) }
                }
            }
        }
    }
}
