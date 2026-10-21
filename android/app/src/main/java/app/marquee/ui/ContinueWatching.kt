package app.marquee.ui

import android.content.Context
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.apis.HubsApi
import app.marquee.api.models.ContinueEntry
import app.marquee.api.models.Hub
import app.marquee.api.models.ItemSummary
import app.marquee.core.Marquee
import app.marquee.core.WatchChoices
import app.marquee.core.WatchState
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Continue Watching (USER-12, LIVE-6): library titles and films and episodes on demand together,
// newest first: Home's row (the first 20, ending in View more when there are others) and a page
// with all of them.

const val CONTINUE_HUB = "continue-watching"
const val CONTINUE_ROUTE = "continue"

/** The row's entries: the hub's own when the server sends them, else its library items (an older server). */
fun continueEntries(hub: Hub): List<ContinueEntry> = hub.propertyEntries ?: hub.items.map(::itemEntry)

/** A library item as an entry. */
fun itemEntry(item: ItemSummary) = ContinueEntry(
    kind = ContinueEntry.Kind.ITEM, key = "item:${item.id}", positionMs = item.viewOffsetMs ?: 0, durationMs = item.durationMs ?: 0,
    lastWatchedAt = item.lastViewedAt ?: java.time.OffsetDateTime.MIN, item = item,
)

/** Whether the row ends in View more: there are more entries than it holds. */
fun continueHasMore(hub: Hub): Boolean = (hub.total ?: 0) > (hub.propertyEntries?.size ?: Int.MAX_VALUE)

/** What removing a VOD entry from Continue Watching sends: the film, or the episode (whose series goes). */
fun vodRemoval(e: ContinueEntry): Pair<HubsApi.KindRemoveVodFromContinueWatching, Long>? = when {
    e.vodMovie != null -> HubsApi.KindRemoveVodFromContinueWatching.MOVIE to e.vodMovie!!.id
    e.vodEpisode != null -> HubsApi.KindRemoveVodFromContinueWatching.EPISODE to e.vodEpisode!!.id
    else -> null
}

/**
 * Removes a VOD film or show (its next episode's id: the whole show goes) from Continue Watching:
 * [onRemoved] once it's gone, then Home's row and the VOD pages' rows refresh; if it fails, a toast
 * and [onFailed] (to put the tile back when it was taken out at once).
 */
fun removeVodContinue(marquee: Marquee, context: Context, kind: HubsApi.KindRemoveVodFromContinueWatching, id: Long, title: String,
    onRemoved: () -> Unit = {}, onFailed: () -> Unit = {}) {
    marquee.scope.launch {
        withContext(Dispatchers.IO) { runCatching { marquee.hubs.removeVodFromContinueWatching(kind, id) } }
            .onSuccess { withContext(Dispatchers.Main) { onRemoved() }; marquee.watch.changed(); VodChanges.bump() }
            .onFailure { withContext(Dispatchers.Main) { onFailed(); toast(context, "Couldn't remove “$title”: ${it.message ?: ""}") } }
    }
}

/** What a VOD entry's menu offers (PLAY-5): watched unless it is, unwatched whenever it's played or part-way through. */
fun vodWatchChoices(e: ContinueEntry): WatchChoices =
    WatchState.choices(e.vodMovie?.watched ?: e.vodEpisode?.watched ?: false, e.positionMs > 0)

/**
 * Marks a film or episode on demand watched or unwatched (unwatched forgets where it stopped,
 * PLAY-5): [onDone] once saved, then Home's row, the Continue Watching page and the VOD pages
 * refresh; a toast if it fails.
 */
fun setVodWatched(marquee: Marquee, context: Context, kind: HubsApi.KindRemoveVodFromContinueWatching, id: Long, on: Boolean, title: String,
    onDone: () -> Unit = {}) {
    marquee.scope.launch {
        withContext(Dispatchers.IO) {
            runCatching {
                when (kind) {
                    HubsApi.KindRemoveVodFromContinueWatching.MOVIE -> if (on) marquee.livetv.markVodWatched(id) else marquee.livetv.markVodUnwatched(id)
                    HubsApi.KindRemoveVodFromContinueWatching.EPISODE -> if (on) marquee.livetv.markVodEpisodeWatched(id) else marquee.livetv.markVodEpisodeUnwatched(id)
                }
            }
        }
            .onSuccess { withContext(Dispatchers.Main) { onDone() }; marquee.watch.changed(); VodChanges.bump() }
            .onFailure { withContext(Dispatchers.Main) { toast(context, "Couldn't update “$title”: ${it.message ?: ""}") } }
    }
}

/** Mark as watched and/or unwatched for a film or episode on demand, as [choices] say; [onDone] once saved. */
fun vodWatchActions(marquee: Marquee, context: Context, kind: HubsApi.KindRemoveVodFromContinueWatching, id: Long, choices: WatchChoices, title: String,
    onDone: () -> Unit = {}): List<TileAction> = if (marquee.isOffline) emptyList() else buildList {
    if (choices.markWatched) add(TileAction("Mark as watched") { setVodWatched(marquee, context, kind, id, true, title, onDone) })
    if (choices.markUnwatched) add(TileAction("Mark as unwatched") { setVodWatched(marquee, context, kind, id, false, title, onDone) })
}

/** Where opening an entry goes: library videos play (as Home always did), films and episodes on demand resume in their player. */
fun continueRoute(e: ContinueEntry): String? {
    e.item?.let { return null }
    e.vodMovie?.let { return "vodplay/${it.id}" }
    e.vodEpisode?.let { return "vodepplay/${it.seriesId}/${it.id}" }
    return null
}

/** How far through an entry, while it's partly watched (an episode's duration may be unknown: then its runtime). */
fun continueProgress(e: ContinueEntry): Float? {
    if (e.positionMs <= 0) return null
    val dur = e.durationMs.takeIf { it > 0 } ?: e.vodMovie?.runtimeMs ?: e.vodEpisode?.runtimeMs ?: return null
    if (dur <= 0) return null
    return (e.positionMs.toFloat() / dur).coerceIn(0f, 1f)
}

/** A VOD entry's title and the line under it: the film and its year, or the show and "S1 · E3 · Title". */
fun vodTitles(e: ContinueEntry): Pair<String, String> {
    e.vodMovie?.let { m -> return m.title to listOfNotNull("VOD", m.year?.toString()).joinToString(" · ") }
    e.vodEpisode?.let { ep -> return ep.seriesTitle to listOf("VOD", ep.label(), ep.title).filter { it.isNotBlank() }.joinToString(" · ") }
    return "" to ""
}

/**
 * One Continue Watching tile: a library item as Home has always shown it, or a film or episode
 * on demand. The context menu removes it from Continue Watching ([onRemoved] gets its key).
 */
@Composable
fun ContinueTile(nav: NavHostController, e: ContinueEntry, width: Dp, autoFocus: Boolean, onOpen: () -> Unit, onRemoved: (String) -> Unit) {
    val marquee = LocalMarquee.current
    val item = e.item
    if (item != null) {
        PosterCard(item, marquee.imageUrl(item.images?.thumb ?: item.images?.backdrop, 300), width, { onOpen(); openItem(nav, item, play = true) },
            shape = Shape.Wide, autoFocus = autoFocus, actions = tileActions(item, continueWatching = true, nav = nav) { id -> onRemoved("item:$id") })
        return
    }
    VodContinueTile(nav, e, width, autoFocus, onOpen, onRemoved)
}

@Composable
private fun VodContinueTile(nav: NavHostController, e: ContinueEntry, width: Dp, autoFocus: Boolean, onOpen: () -> Unit, onRemoved: (String) -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    var menu by remember { mutableStateOf(false) }
    val m = e.vodMovie
    val ep = e.vodEpisode
    val (title, sub) = vodTitles(e)
    // Wide artwork: the film's backdrop over its poster, or the episode's still over the series poster.
    val under = marquee.absolute(m?.posterUrl ?: e.seriesPosterUrl)
    val over = marquee.absolute(if (m != null) "/api/v1/livetv/vod/movies/${m.id}/image?kind=backdrop" else ep?.stillUrl)
    val actions = buildList {
        // Watched or unwatched: either way it leaves Continue Watching.
        vodRemoval(e)?.let { (kind, id) -> addAll(vodWatchActions(marquee, context, kind, id, vodWatchChoices(e), title) { onRemoved(e.key) }) }
        if (!marquee.isOffline) add(TileAction("Remove from Continue Watching") {
            val (kind, id) = vodRemoval(e) ?: return@TileAction
            removeVodContinue(marquee, context, kind, id, title, onRemoved = { onRemoved(e.key) })
        })
        if (m != null) add(TileAction("Go to film") { nav.navigate("vod/${m.id}") })
        if (ep != null) add(TileAction("Go to show") { nav.navigate("vodshow/${ep.seriesId}") })
    }
    Column(Modifier.width(width)) {
        Box {
            Box(Modifier.fillMaxWidth().initialFocus(autoFocus).focusCard({ onOpen(); continueRoute(e)?.let { nav.navigate(it) } }, onLongClick = { menu = true })
                .semantics {
                    contentDescription = listOf(title, sub).filter { it.isNotBlank() }.joinToString(", ")
                    customActions = actions.map { a -> CustomAccessibilityAction(a.label) { a.run(); true } }
                }) {
                Artwork(under, title, Shape.Wide, Modifier.fillMaxWidth())
                if (over != null) AsyncImage(over, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
            }
            continueProgress(e)?.let {
                LinearProgressIndicator(progress = { it }, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(6.dp).height(3.dp),
                    color = Gold, trackColor = Color.White.copy(alpha = 0.3f), drawStopIndicator = {})
            }
            DropdownMenu(menu, { menu = false }) {
                actions.forEach { a -> DropdownMenuItem({ Text(a.label) }, { menu = false; a.run() }) }
            }
        }
        Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(if (m != null) Icons.Filled.Movie else Icons.Filled.Tv, null, Modifier.padding(end = 4.dp).size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        }
        Text(sub, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * Home's Continue Watching row: [hub]'s entries less those removed here ([removed], keys) or just
 * marked watched or unwatched, with See all for the page (on TV a tile at the end, saying how many
 * there are when the row doesn't hold them all; [seeAllFocus] focuses it).
 */
@Composable
fun ContinueRow(nav: NavHostController, hub: Hub, removed: List<String>, width: Dp, focusFirst: Boolean, openedKey: String?,
    onOpen: (String) -> Unit, onRemoved: (String) -> Unit, seeAllFocus: Boolean = false, onSeeAll: () -> Unit = {}) {
    val marquee = LocalMarquee.current
    val marks by marquee.watch.marks.collectAsState()
    val shown = continueEntries(hub).filter { e -> e.key !in removed && e.item?.let { marquee.watch.markFor(it, marks) } == null }
    if (shown.isEmpty()) return
    Shelf(hub.title, shown, sidePadding, onSeeAll = { onSeeAll(); nav.navigate(CONTINUE_ROUTE) }, seeAllTileHeight = width / Shape.Wide.ratio,
        seeAllDetail = hub.total?.takeIf { continueHasMore(hub) }?.let { "$it in all" }, seeAllFocus = seeAllFocus) { i, e ->
        ContinueTile(nav, e, width, autoFocus = marquee.isTv && (if (openedKey != null) e.key == openedKey else focusFirst && i == 0),
            onOpen = { onOpen(e.key) }, onRemoved = onRemoved)
    }
}

/** What the Continue Watching page has loaded, kept while it's on the back stack (so Back returns to the same place). */
private class ContinuePage {
    val entries = mutableStateListOf<ContinueEntry>()
    var total by mutableIntStateOf(-1)
    var loading by mutableStateOf(false)
    var error by mutableStateOf<String?>(null)
    var opened by mutableStateOf<String?>(null)
    var generation = 0
}

private const val CONTINUE_PAGE = 60

/** All of Continue Watching, a page at a time as you scroll: the row's tiles, taps and removal. */
@Composable
fun ContinueWatchingScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val page = rememberCached("continue-page") { ContinuePage() }
    val changes by marquee.watch.changes.collectAsState()
    val vodChanged by VodChanges.version.collectAsState()
    val marks by marquee.watch.marks.collectAsState()
    val removed = remember { mutableStateListOf<String>() }

    fun loadMore() {
        if (page.loading || (page.total >= 0 && page.entries.size >= page.total)) return
        page.loading = true
        val gen = page.generation
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.hubs.continueWatching(page.entries.size, CONTINUE_PAGE) } }
                .onSuccess { p ->
                    if (gen == page.generation) {
                        val have = page.entries.map { it.key }.toSet()
                        page.entries.addAll(p.propertyEntries.filter { it.key !in have }); page.total = p.total; page.error = null
                    }
                }
                .onFailure { if (gen == page.generation) page.error = it.message ?: "Couldn't load" }
            if (gen == page.generation) page.loading = false
        }
    }
    // Opened, and after playback or a change: the first pages again (as many as were loaded), replacing what's shown.
    LaunchedEffect(changes, vodChanged) {
        val gen = ++page.generation
        page.loading = true
        val n = page.entries.size.coerceIn(CONTINUE_PAGE, 200)
        withContext(Dispatchers.IO) { runCatching { marquee.hubs.continueWatching(0, n) } }
            .onSuccess { p ->
                if (gen == page.generation) { page.entries.clear(); page.entries.addAll(p.propertyEntries); page.total = p.total; page.error = null; removed.clear() }
            }
            .onFailure { if (gen == page.generation && page.total < 0) page.error = it.message ?: "Couldn't load" }
        if (gen == page.generation) page.loading = false
    }
    val width = if (marquee.isTv) 240.dp else 168.dp
    val shown = page.entries.filter { e -> e.key !in removed && e.item?.let { marquee.watch.markFor(it, marks) } == null }
    val full: androidx.compose.foundation.lazy.grid.LazyGridItemSpanScope.() -> GridItemSpan = { GridItemSpan(maxLineSpan) }
    LazyVerticalGrid(GridCells.Adaptive(width), Modifier.fillMaxSize().semantics { contentDescription = "Continue Watching" },
        contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = 16.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = full, key = "header") {
            Column {
                Text("Continue Watching", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                if (page.total > 0) Text("${page.total} in all", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        page.error?.takeIf { shown.isEmpty() }?.let { e -> item(span = full, key = "error") { Text(e, color = MaterialTheme.colorScheme.error) } }
        if (page.total == 0 || (page.total > 0 && shown.isEmpty() && !page.loading)) item(span = full, key = "empty") {
            Text("Nothing to continue.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        itemsIndexed(shown, key = { _, e -> e.key }) { i, e ->
            if (i >= shown.size - 12) LaunchedEffect(i, shown.size) { loadMore() }
            // The cell's width: a tile fills it.
            androidx.compose.foundation.layout.BoxWithConstraints {
                ContinueTile(nav, e, maxWidth, autoFocus = marquee.isTv && (if (page.opened != null) e.key == page.opened else i == 0),
                    onOpen = { page.opened = e.key }, onRemoved = { removed.add(it) })
            }
        }
        if (page.loading && page.entries.isEmpty()) item(span = full, key = "loading") {
            Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        }
    }
}
