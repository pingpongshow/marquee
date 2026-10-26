package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridItemSpanScope
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.HomeEntry
import app.marquee.api.models.Hub
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

// A Home row in full (USER-12): its See all page, for rows without a page of their own.

/** How many titles the page asks for. */
private const val HOME_HUB_LIMIT = 100

/** What the page has loaded, kept while it's on the back stack (so Back returns to the same place). */
private class HomeHubPage {
    var hub by mutableStateOf<Hub?>(null)
    var error by mutableStateOf<String?>(null)
    var opened by mutableStateOf<String?>(null)
}

/** A row's entries: library titles and films and series on demand together when it mixes them, else its items. */
internal fun homeHubEntries(hub: Hub): List<HomeEntry> =
    hub.mixedEntries ?: hub.items.map { HomeEntry(HomeEntry.Kind.ITEM, "item:${it.id}", item = it) }

/**
 * See all for a Home row: the row's titles in a grid, with films and series on demand marked VOD
 * as on Home. Library titles open their page, VOD ones the film's or show's page (Not interested
 * in their menu). Reloads after playback or a change; on TV, Back focuses the tile opened.
 */
@Composable
fun HomeHubScreen(nav: NavHostController, hubId: String) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val page = rememberCached("homehub:$hubId") { HomeHubPage() }
    val changes by marquee.watch.changes.collectAsState()
    val vodChanged by VodChanges.version.collectAsState()
    val hidden = remember { mutableStateListOf<String>() }
    LaunchedEffect(changes, vodChanged) {
        withContext(Dispatchers.IO) { runCatching { marquee.hubs.homeHub(hubId, mixed = true, limit = HOME_HUB_LIMIT) } }
            .onSuccess { page.hub = it; page.error = null }
            // A failed refresh keeps what's shown.
            .onFailure { if (page.hub == null) page.error = serverMessage(it, "Couldn't load") }
    }
    val hub = page.hub
    val entries = hub?.let(::homeHubEntries).orEmpty().filter { e ->
        e.key !in hidden && when (e.kind) {
            HomeEntry.Kind.ITEM -> e.item != null
            HomeEntry.Kind.VOD -> e.vod?.let(::vodRoute) != null
        }
    }
    val full: LazyGridItemSpanScope.() -> GridItemSpan = { GridItemSpan(maxLineSpan) }
    LazyVerticalGrid(GridCells.Adaptive(if (marquee.isTv) 140.dp else 112.dp), Modifier.fillMaxSize().semantics { contentDescription = "See all" },
        contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = 16.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = full, key = "header") {
            Text(hub?.title ?: "", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        }
        when {
            hub == null && page.error != null -> item(span = full, key = "error") {
                Text("Couldn't load: ${page.error}", color = MaterialTheme.colorScheme.error)
            }
            hub == null -> item(span = full, key = "loading") {
                Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            }
            entries.isEmpty() -> item(span = full, key = "empty") {
                Text("Nothing here right now.", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            else -> itemsIndexed(entries, key = { _, e -> e.key }) { i, e ->
                val focus = marquee.isTv && (if (page.opened != null) e.key == page.opened else i == 0)
                // The cell's width: a tile fills it.
                BoxWithConstraints {
                    val item = e.item
                    val vod = e.vod
                    if (item != null) PosterCard(item, marquee.imageUrl(item.images?.poster, 300), maxWidth, { page.opened = e.key; openItem(nav, item) },
                        shape = shapeFor(item), autoFocus = focus, actions = tileActions(item, nav = nav))
                    else if (vod != null) VodSuggestionTile(nav, vod, maxWidth, autoFocus = focus, badge = true, watchlist = hubId == "watchlist",
                        onOpen = { page.opened = e.key }) {
                        if (hubId == "watchlist") removeVodFromWatchlist(marquee, context, vod, onRemoved = { hidden.add(e.key) }, onFailed = { hidden.remove(e.key) })
                        else hideVodSuggestion(marquee, context, vod, onHidden = { hidden.add(e.key) }, onFailed = { hidden.remove(e.key) })
                    }
                }
            }
        }
    }
}
