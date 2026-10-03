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
    LaunchedEffect(checkId) {
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
                    onBazarr = { bazarr = issue }, onRelated = { nav.navigate("item/${it}") })
                HorizontalDivider()
            }
            if (loading) item { CircularProgressIndicator(Modifier.padding(sidePadding)) }
        }
    }
    bazarr?.let { b -> BazarrDialog(b.item.id, b.item.title) { bazarr = null } }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun IssueRow(issue: HealthIssue, checkId: String, onOpen: () -> Unit, onRefresh: () -> Unit, onIgnore: () -> Unit, onBazarr: () -> Unit, onRelated: (Long) -> Unit) {
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
                if (checkId == HealthCheck.Id.MISSING_SUBTITLES.value) TextButton(onBazarr, Modifier.focusRing()) { Text("Download with Bazarr") }
                TextButton(onIgnore, Modifier.focusRing().semantics { contentDescription = "Ignore ${it.title}" }) { Text("Ignore") }
            }
        }
    }
}
