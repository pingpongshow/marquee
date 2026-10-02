package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
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
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.apis.RequestsApi
import app.marquee.api.models.Availability
import app.marquee.api.models.DeclineRequestRequest
import app.marquee.api.models.DiscoverItem
import app.marquee.api.models.MediaRequest
import app.marquee.api.models.MediaRequestCreate
import app.marquee.api.models.RequestState
import app.marquee.api.models.RequestableShow
import app.marquee.api.models.RequestsStatus
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

val Availability.label: String
    get() = when (this) {
        Availability.NONE -> ""
        Availability.PENDING -> "Waiting for approval"
        Availability.REQUESTED -> "Requested"
        Availability.PROCESSING -> "On its way"
        Availability.PARTIAL -> "Partly available"
        Availability.AVAILABLE -> "In library"
    }

val RequestState.label: String
    get() = when (this) {
        RequestState.PENDING -> "Waiting for approval"
        RequestState.APPROVED -> "Approved"
        RequestState.DECLINED -> "Declined"
        RequestState.FAILED -> "Failed"
        RequestState.AVAILABLE -> "Available"
    }

private val categories = listOf(
    RequestsApi.CategoryDiscoverRequestable.TRENDING to "Trending",
    RequestsApi.CategoryDiscoverRequestable.MOVIES to "Movies",
    RequestsApi.CategoryDiscoverRequestable.TV to "Shows",
    RequestsApi.CategoryDiscoverRequestable.UPCOMING to "Coming Soon",
)

private fun DiscoverItem.requestable() =
    availability == Availability.NONE || (mediaType == DiscoverItem.MediaType.TV && availability == Availability.PARTIAL)

/** Discover (REQ-1, REQ-2): titles from Seerr, search, and requests. */
@Composable
fun DiscoverScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val status by produceState<RequestsStatus?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.requests.requestsStatus() }.getOrNull() } }
    var category by remember { mutableStateOf(RequestsApi.CategoryDiscoverRequestable.TRENDING) }
    var query by remember { mutableStateOf("") }
    var items by remember { mutableStateOf<List<DiscoverItem>?>(null) }
    var mine by remember { mutableStateOf<List<MediaRequest>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var picked by remember { mutableStateOf<DiscoverItem?>(null) }
    var reload by remember { mutableIntStateOf(0) }

    LaunchedEffect(status, category, query, reload) {
        val s = status ?: return@LaunchedEffect
        if (!s.enabled || !s.canRequest) return@LaunchedEffect
        val q = query.trim()
        if (q.isNotEmpty()) delay(350)
        withContext(Dispatchers.IO) {
            runCatching { if (q.isEmpty()) marquee.requests.discoverRequestable(category).results else marquee.requests.searchRequestable(q).results }
                .onSuccess { items = it; error = null }
                .onFailure { error = it.message }
            mine = runCatching { marquee.requests.listRequests() }.getOrDefault(emptyList())
        }
    }

    val s = status
    when {
        s == null -> Box(Modifier.fillMaxWidth().padding(48.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        !s.enabled -> Message("Requests aren't set up", "An admin can connect Seerr in the web app's Settings → Requests.")
        !s.canRequest -> Message("Ask an admin", "An admin can let you request movies and shows.")
        else -> LazyVerticalGrid(
            GridCells.Adaptive(if (marquee.isTv) 130.dp else 110.dp),
            contentPadding = PaddingValues(sidePadding),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Discover", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                    OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth(), singleLine = true, placeholder = { Text("Find a movie or show to request") })
                    if (query.isBlank()) LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(categories) { (c, label) ->
                            FilterChip(category == c, { category = c }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(marquee.isTv && c == category))
                        }
                    }
                    error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                }
            }
            val list = items
            if (list == null) item(span = { GridItemSpan(maxLineSpan) }) { Box(Modifier.padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            else items(list, key = { "${it.mediaType}-${it.tmdbId}" }) { it ->
                DiscoverCard(it) {
                    when {
                        it.itemId != null -> nav.navigate("item/${it.itemId}")
                        it.requestable() -> picked = it
                    }
                }
            }
            if (mine.isNotEmpty()) {
                item(span = { GridItemSpan(maxLineSpan) }) { Text("My Requests", Modifier.padding(top = 16.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
                items(mine, key = { "r${it.id}" }, span = { GridItemSpan(maxLineSpan) }) { r ->
                    RequestRow(r, action = if (r.status == RequestState.PENDING) "Withdraw" else null) {
                        scope.launch {
                            withContext(Dispatchers.IO) { runCatching { marquee.requests.cancelRequest(r.id) } }
                            reload++
                        }
                    }
                }
            }
        }
    }
    picked?.let { RequestDialog(it) { done -> picked = null; if (done) reload++ } }
}

@Composable
private fun Message(title: String, body: String) {
    Column(Modifier.fillMaxWidth().padding(48.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        Text(body, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun DiscoverCard(it: DiscoverItem, onClick: () -> Unit) {
    Column {
        Box(
            Modifier.fillMaxWidth().aspectRatio(2f / 3f).focusCard(onClick)
                .semantics { contentDescription = if (it.requestable()) "Request ${it.title}" else listOf(it.title, it.availability.label).filter { s -> s.isNotBlank() }.joinToString(", ") },
        ) {
            Box(Modifier.matchParentSize().background(Surface2)) {
                Text(it.title, Modifier.align(Alignment.BottomStart).padding(8.dp), style = MaterialTheme.typography.labelMedium)
            }
            it.posterUrl?.let { u -> AsyncImage(u, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize()) }
            if (it.availability != Availability.NONE) Text(
                it.availability.label,
                Modifier.padding(6.dp).background(if (it.availability == Availability.AVAILABLE) Color(0xFF2E7D32) else Color.Black.copy(alpha = 0.7f), RoundedCornerShape(4.dp))
                    .padding(horizontal = 6.dp, vertical = 2.dp),
                color = Color.White, style = MaterialTheme.typography.labelSmall,
            )
        }
        Text(it.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text(listOfNotNull(it.year?.toString(), if (it.mediaType == DiscoverItem.MediaType.TV) "Series" else "Movie").joinToString(" · "),
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun RequestRow(r: MediaRequest, action: String?, secondary: String? = null, onSecondary: () -> Unit = {}, onAction: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        AsyncImage(r.posterUrl, null, contentScale = ContentScale.Crop, modifier = Modifier.width(40.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(4.dp)).background(Surface2))
        Column(Modifier.weight(1f)) {
            Text(r.year?.let { "${r.title} ($it)" } ?: r.title, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.Medium)
            Text(listOfNotNull(r.userName.takeIf { secondary != null }, r.status.label, r.reason).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (action != null) Button(onAction, Modifier.focusRing()) { Text(action) }
        if (secondary != null) OutlinedButton(onSecondary, Modifier.focusRing()) { Text(secondary) }
    }
}

/** Ask for a movie, or chosen seasons of a show. */
@Composable
private fun RequestDialog(item: DiscoverItem, onDone: (Boolean) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val isShow = item.mediaType == DiscoverItem.MediaType.TV
    val show by produceState<RequestableShow?>(null) { if (isShow) value = withContext(Dispatchers.IO) { runCatching { marquee.requests.requestableShow(item.tmdbId) }.getOrNull() } }
    var chosen by remember { mutableStateOf<Set<Int>?>(null) }
    val open = show?.seasons?.filter { it.availability == Availability.NONE }?.map { it.number }?.toSet() ?: emptySet()
    val seasons = chosen ?: open
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = { onDone(false) },
        title = { Text("Request ${item.title}") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    AsyncImage(item.posterUrl, null, contentScale = ContentScale.Crop, modifier = Modifier.width(80.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)))
                    Text(item.overview ?: "", maxLines = 7, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall)
                }
                Text("An admin approves requests before they're downloaded.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (isShow) {
                    val sh = show
                    if (sh == null) CircularProgressIndicator(Modifier.size(24.dp))
                    else sh.seasons.forEach { s ->
                        val can = s.availability == Availability.NONE
                        Row(Modifier.fillMaxWidth().clickable(enabled = can) { chosen = if (s.number in seasons) seasons - s.number else seasons + s.number },
                            verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(can && s.number in seasons, { on -> chosen = if (on) seasons + s.number else seasons - s.number }, enabled = can)
                            Text(s.name ?: "Season ${s.number}", Modifier.weight(1f))
                            Text(if (can) "${s.episodeCount ?: "?"} episodes" else s.availability.label, style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            Button(
                {
                    busy = true
                    scope.launch {
                        val mt = if (isShow) MediaRequestCreate.MediaType.TV else MediaRequestCreate.MediaType.MOVIE
                        withContext(Dispatchers.IO) { runCatching { marquee.requests.createRequest(MediaRequestCreate(item.tmdbId, mt, if (isShow) seasons.sorted() else null)) } }
                            .onSuccess { onDone(true) }
                            .onFailure { error = it.message }
                        busy = false
                    }
                },
                Modifier.focusRing().initialFocus(marquee.isTv),
                enabled = !busy && (!isShow || seasons.isNotEmpty()),
            ) { Text("Request") }
        },
        dismissButton = { TextButton({ onDone(false) }, Modifier.focusRing()) { Text("Cancel") } },
    )
}

/** Admins: approve or decline requests. */
@Composable
fun ApprovalsScreen() {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var list by remember { mutableStateOf<List<MediaRequest>?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(reload) {
        list = withContext(Dispatchers.IO) { runCatching { marquee.requests.listRequests(RequestsApi.ScopeListRequests.ALL) }.onFailure { error = it.message }.getOrNull() }
    }
    fun act(work: suspend () -> Unit) = scope.launch {
        withContext(Dispatchers.IO) { runCatching { work() }.onFailure { error = it.message } }
        reload++
    }
    androidx.compose.foundation.lazy.LazyColumn(contentPadding = PaddingValues(sidePadding), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        item { Text("Requests", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        error?.let { e -> item { Text(e, color = MaterialTheme.colorScheme.error) } }
        val all = list ?: emptyList()
        val pending = all.filter { it.status == RequestState.PENDING }
        item { Text("Waiting for approval", Modifier.padding(top = 12.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
        if (list != null && pending.isEmpty()) item { Text("Nothing to approve.", color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(pending, key = { it.id }) { r ->
            RequestRow(r, action = "Approve", secondary = "Decline",
                onSecondary = { act { marquee.requests.declineRequest(r.id, DeclineRequestRequest()) } },
                onAction = { act { marquee.requests.approveRequest(r.id) } })
        }
        val done = all.filter { it.status != RequestState.PENDING }.take(30)
        if (done.isNotEmpty()) {
            item { Text("Recent", Modifier.padding(top = 12.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
            items(done, key = { it.id }) { r -> RequestRow(r, action = null) {} }
        }
    }
}
