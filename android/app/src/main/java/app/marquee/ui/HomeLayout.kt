package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.HomeLayout
import app.marquee.api.models.HomeLayoutRow
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Home rows (USER-12): the person's own order, hidden rows and pinned collections and
 * playlists. The server sends every available row in order; saving sends ids and hidden
 * flags in the new order (an empty list resets to the default).
 */
object HomeRows {
    suspend fun load(marquee: Marquee): List<HomeLayoutRow> = withContext(Dispatchers.IO) { marquee.hubs.getHomeLayout().rows }

    suspend fun save(marquee: Marquee, rows: List<HomeLayoutRow>): List<HomeLayoutRow> = withContext(Dispatchers.IO) {
        marquee.hubs.setHomeLayout(HomeLayout(rows.map { HomeLayoutRow(it.id, hidden = it.hidden == true) })).rows
    }

    /** The page a pinned hub opens: its collection or playlist. */
    fun route(hubId: String): String? = when {
        hubId.startsWith("collection-") -> hubId.removePrefix("collection-").toLongOrNull()?.let { "item/$it" }
        hubId.startsWith("playlist-") -> hubId.removePrefix("playlist-").toLongOrNull()?.let { "playlist/$it" }
        else -> null
    }
}

/** Edit Home: reorder with up/down buttons (D-pad friendly), hide or show, unpin, or reset. */
@Composable
fun EditHomeScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var rows by remember { mutableStateOf<List<HomeLayoutRow>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { runCatching { HomeRows.load(marquee) }.onSuccess { rows = it }.onFailure { error = it.message } }

    // Shows the change at once and saves it; the server's answer is the truth.
    fun commit(next: List<HomeLayoutRow>) {
        rows = next
        scope.launch { runCatching { HomeRows.save(marquee, next) }.onSuccess { rows = it; error = null }.onFailure { error = it.message ?: "Couldn't save" } }
    }
    fun move(i: Int, by: Int) {
        val list = rows?.toMutableList() ?: return
        val j = i + by
        if (j !in list.indices) return
        list.add(j, list.removeAt(i))
        commit(list)
    }

    val list = rows
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item {
            Row(Modifier.padding(horizontal = sidePadding, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("Edit Home", Modifier.weight(1f), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                OutlinedButton({
                    scope.launch {
                        runCatching { withContext(Dispatchers.IO) { marquee.hubs.setHomeLayout(HomeLayout(emptyList())).rows } }
                            .onSuccess { rows = it }.onFailure { error = it.message }
                    }
                }, Modifier.focusRing()) { Text("Reset") }
            }
            Text("Choose the order of your Home rows and hide the ones you don't use. Pin collections and playlists from their pages.",
                Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            error?.let { Text(it, Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error) }
        }
        if (list == null) item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        else itemsIndexed(list, key = { _, r -> r.id }) { i, r ->
            val hidden = r.hidden == true
            val title = r.title ?: r.id
            Row(
                Modifier.fillMaxWidth().padding(horizontal = sidePadding - 4.dp, vertical = 2.dp)
                    .semantics(mergeDescendants = false) { stateDescription = if (hidden) "Hidden" else "Shown" },
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (r.pinned == true) Icon(Icons.Filled.PushPin, "Pinned", Modifier.padding(start = 4.dp, end = 8.dp), tint = Gold)
                Text(title, Modifier.weight(1f).padding(start = 4.dp), fontWeight = FontWeight.Medium,
                    color = if (hidden) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface)
                IconButton({ move(i, -1) }, Modifier.focusRing().initialFocus(marquee.isTv && i == 0), enabled = i > 0) { Icon(Icons.Filled.KeyboardArrowUp, "Move $title up") }
                IconButton({ move(i, 1) }, Modifier.focusRing(), enabled = i < list.size - 1) { Icon(Icons.Filled.KeyboardArrowDown, "Move $title down") }
                IconButton({ commit(list.map { if (it.id == r.id) it.copy(hidden = !hidden) else it }) }, Modifier.focusRing()) {
                    Icon(if (hidden) Icons.Filled.VisibilityOff else Icons.Filled.Visibility, if (hidden) "Show $title" else "Hide $title",
                        tint = if (hidden) MaterialTheme.colorScheme.onSurfaceVariant else Gold)
                }
                if (r.pinned == true) IconButton({ commit(list.filter { it.id != r.id }) }, Modifier.focusRing()) { Icon(Icons.Filled.Close, "Unpin $title") }
            }
            HorizontalDivider()
        }
        item {
            Column(Modifier.padding(sidePadding)) {
                OutlinedButton({ nav.popBackStack() }, Modifier.focusRing()) { Text("Done") }
            }
        }
    }
}

/** Pin to Home / Unpin from Home for a collection or playlist (rowId: collection-<id> or playlist-<id>). */
@Composable
fun PinToHomeButton(rowId: String) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var pinned by remember { mutableStateOf<Boolean?>(null) }
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(rowId) { pinned = runCatching { HomeRows.load(marquee).any { it.id == rowId } }.getOrNull() }
    val on = pinned ?: return
    OutlinedButton(
        {
            if (busy) return@OutlinedButton
            busy = true
            scope.launch {
                // Read the layout fresh, so changes made elsewhere aren't undone.
                runCatching {
                    val rows = HomeRows.load(marquee)
                    val next = if (on) rows.filter { it.id != rowId } else rows + HomeLayoutRow(rowId, hidden = false)
                    HomeRows.save(marquee, next).any { it.id == rowId }
                }.onSuccess { pinned = it }
                busy = false
            }
        },
        Modifier.focusRing().semantics { contentDescription = if (on) "Unpin from Home" else "Pin to Home" },
    ) {
        Icon(if (on) Icons.Filled.PushPin else Icons.Outlined.PushPin, null)
        Text(if (on) "Unpin from Home" else "Pin to Home", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
    }
}
