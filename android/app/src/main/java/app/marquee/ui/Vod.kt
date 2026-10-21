package app.marquee.ui

import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.runtime.mutableStateListOf
import app.marquee.api.apis.HubsApi
import androidx.annotation.OptIn
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.FilterList
import androidx.compose.material.icons.filled.Forward30
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.RemoveDone
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import app.marquee.api.apis.LivetvApi
import app.marquee.api.apis.LivetvApi.SortListVodMovies
import app.marquee.api.apis.LivetvApi.SortListVodSeries
import app.marquee.api.apis.LivetvApi.WatchedListVodMovies
import app.marquee.api.apis.LivetvApi.WatchedListVodSeries
import app.marquee.api.models.PlayVodEpisodeRequest
import app.marquee.api.models.PlayVodMovieRequest
import app.marquee.api.models.VodEpisode
import app.marquee.api.models.VodSeries
import app.marquee.core.AndroidProfile
import app.marquee.core.WatchState
import app.marquee.api.models.ReportVodPlaybackRequest
import app.marquee.api.models.VodCategory
import app.marquee.api.models.VodMovie
import app.marquee.api.models.VodMovieDetail
import app.marquee.api.models.VodSession
import app.marquee.api.models.VodStatus
import coil3.compose.AsyncImage
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Movies on demand under Live TV (LIVE-6): browse by category, a film's page, and the player.
// The films come from M3U sources and play as the source's own HLS through the server.

/**
 * Bumped once a film's position, watched state or session has reached the server, so the
 * pages showing it reload (the player's last report lands after the page below reappears).
 */
internal object VodChanges {
    val version = MutableStateFlow(0)
    fun bump() { version.value++ }
}

/** The sorts both VOD pages offer, by the API's value. */
private val vodSorts = listOf(
    "title" to "Title",
    "added" to "Recently added",
    "year" to "Release year",
    "rating" to "Rating",
)

private const val PAGE = 120

private fun VodMovie.meta() = listOfNotNull(year?.toString(), runtimeMs?.let(::formatRuntime)).joinToString(" · ")

/** "1h 52m", or "48m". */
internal fun formatRuntime(ms: Long): String? {
    val m = ms / 60_000
    if (m <= 0) return null
    return if (m >= 60) "${m / 60}h ${m % 60}m" else "${m}m"
}

private fun VodMovie.progress(): Float? {
    if (watched) return null
    val pos = positionMs?.takeIf { it > 0 } ?: return null
    val dur = runtimeMs?.takeIf { it > 0 } ?: return null
    return (pos.toFloat() / dur).coerceIn(0f, 1f)
}

/** A film's poster with its title underneath, a watched tick or how far it was watched. */
@Composable
private fun VodTile(m: VodMovie, width: Dp?, actions: List<TileAction> = emptyList(), onOpen: () -> Unit) {
    val marquee = LocalMarquee.current
    var menu by remember { mutableStateOf(false) }
    Column(if (width != null) Modifier.width(width) else Modifier.fillMaxWidth()) {
        Box {
            Artwork(marquee.absolute(m.posterUrl), m.title, Shape.Poster, Modifier.fillMaxWidth()
                .focusCard(onOpen, onLongClick = if (actions.isEmpty()) null else ({ menu = true }))
                .inLibraryBorder(m.inLibrary)
                .semantics {
                    contentDescription = m.title
                    vodTileState(m.watched, m.inLibrary)?.let { stateDescription = it }
                    if (actions.isNotEmpty()) customActions = actions.map { a -> CustomAccessibilityAction(a.label) { a.run(); true } }
                })
            TileMenu(actions, menu) { menu = false }
            if (m.watched) Icon(Icons.Filled.CheckCircle, null, Modifier.align(Alignment.TopEnd).padding(6.dp).size(20.dp)
                .background(Color.Black.copy(alpha = 0.55f), RoundedCornerShape(50)), tint = Gold)
            m.progress()?.let {
                LinearProgressIndicator(progress = { it }, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(6.dp).height(3.dp),
                    color = Gold, trackColor = Color.White.copy(alpha = 0.3f), drawStopIndicator = {})
            }
        }
        Text(m.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text(m.meta(), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** What a VOD page has loaded (films or shows), kept while it's on the back stack (so Back returns to the same place). */
private class VodList {
    val items = mutableStateOf<List<Any>?>(null)
    val total = mutableIntStateOf(0)
    val error = mutableStateOf<String?>(null)
    val loadingMore = mutableStateOf(false)
    var loadedFor: VodQuery? = null
    val grid = LazyGridState()
}

/** [current] then those of [page] whose [key] isn't already in it (a list's next page, after the list moved). */
internal fun <T> appendUnseen(current: List<T>, page: List<T>, key: (T) -> Any): List<T> {
    val seen = current.mapTo(HashSet(), key)
    return current + page.filter { seen.add(key(it)) }
}

/** A film's or show's key in a VOD page's grid. */
private fun vodKey(x: Any): String = if (x is VodSeries) "s${x.id}" else "m${(x as VodMovie).id}"

/** What a VOD page's list is for. */
private data class VodQuery(val category: String, val q: String, val sort: String, val filters: VodFilters)

/** VOD Movies (LIVE-6): a page of its own, opened from Libraries' VOD Movies entry. */
@Composable
fun VodPage(nav: NavHostController) {
    VodBrowse(nav, shows = false) {
        Text("VOD Movies", Modifier.padding(horizontal = sidePadding), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
    }
}

/** VOD Shows (LIVE-6): series from Xtream Codes sources, opened from Libraries' VOD Shows entry. */
@Composable
fun VodShowsPage(nav: NavHostController) {
    VodBrowse(nav, shows = true) {
        Row(Modifier.fillMaxWidth().padding(horizontal = sidePadding), verticalAlignment = Alignment.CenterVertically) {
            Text("VOD Shows", Modifier.weight(1f), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            // The series you follow (new episodes saved automatically).
            VodFollowingButton(nav)
        }
    }
}

/**
 * A VOD page's browse, films or [shows]: categories, search, sort, filters, Continue Watching and a
 * poster grid that loads a page at a time. Category, sort and filters are remembered per page.
 * [header] is the page's title, scrolled with the grid.
 */
@Composable
fun VodBrowse(nav: NavHostController, shows: Boolean, header: @Composable () -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = remember(shows) { VodPagePrefs(context, marquee.server?.id, shows) }
    val kind = if (shows) "vodshows" else "vod"
    val noun = if (shows) "shows" else "movies"
    var category by rememberSaveable { mutableStateOf(prefs.category) }
    var sort by rememberSaveable { mutableStateOf(prefs.sort.takeIf { s -> vodSorts.any { it.first == s } } ?: "title") }
    var filtersText by rememberSaveable { mutableStateOf(prefs.filters.encode()) }
    val filters = remember(filtersText) { VodFilters.decode(filtersText) }
    fun setFilters(f: VodFilters) { filtersText = f.encode(); prefs.filters = f }
    LaunchedEffect(category) { prefs.category = category }
    LaunchedEffect(sort) { prefs.sort = sort }
    var filtersOpen by remember { mutableStateOf(false) }
    var text by rememberSaveable { mutableStateOf("") }
    var q by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(text) { delay(300); q = text.trim() }
    val changed by VodChanges.version.collectAsState()
    val categories by cachedLoad<List<VodCategory>>("$kind:categories") {
        withContext(Dispatchers.IO) { runCatching { if (shows) marquee.livetv.listVodSeriesCategories() else marquee.livetv.listVodCategories() }.getOrNull() }
    }
    val genres by cachedLoad<List<VodCategory>>("$kind:genres") {
        withContext(Dispatchers.IO) { runCatching { if (shows) marquee.livetv.listVodSeriesGenres() else marquee.livetv.listVodGenres() }.getOrNull() }
    }
    val status by cachedLoad<VodStatus>("$kind:status") { withContext(Dispatchers.IO) { runCatching { marquee.livetv.vodStatus() }.getOrNull() } }
    val sources = status?.sources?.filter { if (shows) it.series > 0 else it.movies > 0 } ?: emptyList()
    val resumeMovies by cachedLoad<List<VodMovie>>("vod:continue", changed) {
        if (shows) null else withContext(Dispatchers.IO) { runCatching { marquee.livetv.vodContinueWatching() }.getOrNull() }
    }
    val resumeShows by cachedLoad<List<VodSeries>>("vodshows:continue", changed) {
        if (!shows) null else withContext(Dispatchers.IO) { runCatching { marquee.livetv.vodSeriesContinueWatching() }.getOrNull() }
    }
    // Recommended for you (USER-16): films or series on demand picked from this person's taste.
    val recommended by cachedLoad<List<app.marquee.api.models.VodSuggestion>>("$kind:suggestions", changed) {
        withContext(Dispatchers.IO) {
            runCatching { marquee.livetv.vodSuggestions(if (shows) LivetvApi.KindVodSuggestions.SERIES else LivetvApi.KindVodSuggestions.MOVIE, 20) }.getOrNull()
        }
    }
    val state = rememberCached("$kind:list") { VodList() }
    var items by state.items

    suspend fun fetch(k: VodQuery, offset: Int, limit: Int): Result<Pair<List<Any>, Int>> = withContext(Dispatchers.IO) {
        val f = k.filters
        runCatching {
            if (shows) marquee.livetv.listVodSeries(
                category = k.category.ifEmpty { null }, q = k.q.ifEmpty { null }, sort = SortListVodSeries.entries.first { it.value == k.sort },
                limit = limit, genre = f.genre, yearFrom = f.yearFrom, yearTo = f.yearTo, minRating = f.minRating?.toDouble(),
                watched = f.watched?.let { w -> WatchedListVodSeries.entries.first { it.value == w } }, sourceId = f.sourceId, inLibrary = f.inLibrary, offset = offset,
            ).let { it.series to it.total }
            else marquee.livetv.listVodMovies(
                category = k.category.ifEmpty { null }, q = k.q.ifEmpty { null }, sort = SortListVodMovies.entries.first { it.value == k.sort },
                limit = limit, genre = f.genre, yearFrom = f.yearFrom, yearTo = f.yearTo, minRating = f.minRating?.toDouble(),
                watched = f.watched?.let { w -> WatchedListVodMovies.entries.first { it.value == w } }, sourceId = f.sourceId, inLibrary = f.inLibrary, offset = offset,
            ).let { it.movies to it.total }
        }
    }
    // A new filter starts again from the top; coming back (or a change from the film's or show's
    // page) refreshes what's loaded in place, keeping the place in the grid.
    LaunchedEffect(category, q, sort, filters, changed) {
        val key = VodQuery(category, q, sort, filters)
        val have = items
        if (state.loadedFor == key && have != null) {
            fetch(key, 0, have.size.coerceAtLeast(PAGE)).onSuccess { (list, total) -> items = list; state.total.intValue = total }
            return@LaunchedEffect
        }
        items = null
        state.error.value = null
        state.loadedFor = key
        fetch(key, 0, PAGE).onSuccess { (list, total) -> items = list; state.total.intValue = total }
            .onFailure { state.error.value = it.message ?: "Couldn't load the $noun"; items = emptyList() }
        state.grid.scrollToItem(0)
    }
    fun loadMore() {
        val have = items ?: return
        val key = state.loadedFor ?: return
        if (state.loadingMore.value || have.size >= state.total.intValue) return
        state.loadingMore.value = true
        scope.launch {
            // Only titles not already shown: the list may have moved meanwhile (new titles, a refresh in place),
            // and a repeated key would bring the grid down.
            fetch(key, have.size, PAGE).onSuccess { (page, _) ->
                if (state.loadedFor != key) return@onSuccess
                items = appendUnseen(items ?: emptyList(), page, ::vodKey)
            }
            state.loadingMore.value = false
        }
    }
    // The next page as the end of the grid comes into view.
    LaunchedEffect(state) {
        snapshotFlow { state.grid.layoutInfo.let { (it.visibleItemsInfo.lastOrNull()?.index ?: 0) >= it.totalItemsCount - 12 } }
            .distinctUntilChanged().filter { it }.collect { loadMore() }
    }
    fun openMovie(m: VodMovie) = nav.navigate("vod/${m.id}")
    fun openShow(s: VodSeries) = nav.navigate("vodshow/${s.id}")
    // Continue Watching's removals (USER-12): a tile goes at once ("m<id>" or "s<id>") and comes back
    // if the call fails; once a reloaded row no longer has it, the key is forgotten (so a title
    // watched again shows again).
    val removed = remember { mutableStateListOf<String>() }
    LaunchedEffect(resumeMovies, resumeShows) {
        val keys = (resumeMovies?.map { "m${it.id}" } ?: emptyList()) + (resumeShows?.map { "s${it.id}" } ?: emptyList())
        removed.retainAll { it in keys }
    }
    fun continueActions(removal: Pair<HubsApi.KindRemoveVodFromContinueWatching, Long>?, key: String, title: String, more: List<TileAction>) = buildList {
        if (removal != null && !marquee.isOffline) add(TileAction("Remove from Continue Watching") {
            removed.add(key)
            removeVodContinue(marquee, context, removal.first, removal.second, title, onFailed = { removed.remove(key) })
        })
        addAll(more)
    }

    val all = categories?.sumOf { it.count }
    val list = items
    val searched = category.isNotEmpty() || q.isNotEmpty() || filters.active
    val full: androidx.compose.foundation.lazy.grid.LazyGridItemSpanScope.() -> GridItemSpan = { GridItemSpan(maxLineSpan) }
    val chipShape = RoundedCornerShape(8.dp)
    LazyVerticalGrid(
        GridCells.Adaptive(if (marquee.isTv) 150.dp else 108.dp), Modifier.fillMaxSize().semantics { contentDescription = if (shows) "VOD Shows" else "VOD Movies" }, state.grid,
        contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = 16.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        // The header and filters span the grid; the side padding is the grid's, so they undo theirs.
        item(span = full, key = "header") { HeaderNoPadding(header) }
        item(span = full, key = "filters") {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(text, { text = it }, Modifier.fillMaxWidth().widthIn(max = 480.dp).semantics { contentDescription = "Search $noun" }, singleLine = true,
                    leadingIcon = { Icon(Icons.Filled.Search, null) }, placeholder = { Text("Search $noun") })
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    item { FilterChip(category.isEmpty(), { category = "" }, { Text("All $noun" + (all?.let { " ($it)" } ?: "")) }, Modifier.focusRing(chipShape)) }
                    items(categories ?: emptyList(), key = { it.name }) { c ->
                        FilterChip(category == c.name, { category = c.name }, { Text("${c.name} (${c.count})") }, Modifier.focusRing(chipShape))
                    }
                }
                LazyRow(Modifier.semantics { contentDescription = "Sort and filter" }, horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    item {
                        FilterChip(filters.active, { filtersOpen = true }, { Text("Filter", maxLines = 1) }, Modifier.focusRing(chipShape),
                            leadingIcon = { Icon(Icons.Filled.FilterList, null, Modifier.size(18.dp)) })
                    }
                    item { Text("Sort", Modifier.padding(start = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelLarge) }
                    items(vodSorts) { (s, label) -> FilterChip(sort == s, { sort = s }, { Text(label) }, Modifier.focusRing(chipShape)) }
                }
                VodActiveFilters(filters, shows, sources, ::setFilters)
            }
        }
        if (!searched && !recommended.isNullOrEmpty()) item(span = full, key = "recommended") {
            HeaderNoPadding {
                VodSuggestionRow(nav, "Recommended for you", recommended, onSeeAll = { nav.navigate(vodRecommendedRoute(shows)) })
            }
        }
        val rm = resumeMovies
        val rs = resumeShows
        if (!searched && (if (shows) !rs.isNullOrEmpty() else !rm.isNullOrEmpty())) item(span = full, key = "continue") {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("Continue Watching", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                LazyRow(Modifier.semantics { contentDescription = "Continue Watching" }, contentPadding = PaddingValues(vertical = 6.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    if (shows) items(rs?.filter { "s${it.id}" !in removed } ?: emptyList(), key = { it.id }) { s ->
                        val ep = s.nextEpisode
                        // Watched or unwatched (PLAY-5): the next episode, and the tile leaves the row at once.
                        val watch = ep?.let { vodWatchActions(marquee, context, HubsApi.KindRemoveVodFromContinueWatching.EPISODE, it.id,
                            WatchState.choices(it.watched, (it.positionMs ?: 0) > 0), s.title) { removed.add("s${s.id}") } } ?: emptyList()
                        VodNextEpisodeTile(s, if (marquee.isTv) 260.dp else 200.dp, watch + continueActions(ep?.let { HubsApi.KindRemoveVodFromContinueWatching.EPISODE to it.id },
                            "s${s.id}", s.title, listOf(TileAction("Go to show") { openShow(s) }))) {
                            val e = s.nextEpisode
                            if (e != null) nav.navigate("vodepplay/${s.id}/${e.id}") else openShow(s)
                        }
                    } else items(rm?.filter { "m${it.id}" !in removed } ?: emptyList(), key = { it.id }) { m ->
                        val watch = vodWatchActions(marquee, context, HubsApi.KindRemoveVodFromContinueWatching.MOVIE, m.id,
                            WatchState.choices(m.watched, (m.positionMs ?: 0) > 0), m.title) { removed.add("m${m.id}") }
                        VodTile(m, if (marquee.isTv) 150.dp else 112.dp, watch + continueActions(HubsApi.KindRemoveVodFromContinueWatching.MOVIE to m.id, "m${m.id}", m.title,
                            listOf(TileAction("Go to film") { openMovie(m) }))) { openMovie(m) }
                    }
                }
            }
        }
        if (searched && list != null) item(span = full, key = "count") {
            val n = state.total.intValue
            Text("$n ${if (n == 1) noun.dropLast(1) else noun}", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        }
        when {
            list == null -> item(span = full, key = "loading") { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            state.error.value != null -> item(span = full, key = "error") { Text(state.error.value!!, color = MaterialTheme.colorScheme.error) }
            list.isEmpty() -> item(span = full, key = "empty") {
                Text(when {
                    q.isNotEmpty() -> "No $noun match “$q”."
                    filters.active -> "Nothing matches these filters."
                    else -> "No $noun here yet."
                }, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            else -> items(list, key = ::vodKey) { x ->
                when (x) {
                    is VodSeries -> VodShowTile(x, null) { openShow(x) }
                    is VodMovie -> VodTile(x, null) { openMovie(x) }
                }
            }
        }
        if (state.loadingMore.value) item(span = full, key = "more") { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
    }
    if (filtersOpen) VodFilterDialog(filters, shows, genres, sources, ::setFilters) { filtersOpen = false }
}

/** The page's header pads itself; inside the grid (which pads) it's pulled back to the edge. */
@Composable
private fun HeaderNoPadding(header: @Composable () -> Unit) {
    Box(Modifier.layoutOffset(-sidePadding)) { header() }
}

/** Draws the content wider by [inset] on both sides (so a padded child lines up with the grid's padding). */
private fun Modifier.layoutOffset(inset: Dp): Modifier = this.then(
    Modifier.layout { measurable, constraints ->
        val extra = (-inset).roundToPx() * 2
        val p = measurable.measure(constraints.copy(minWidth = (constraints.minWidth + extra).coerceAtLeast(0), maxWidth = constraints.maxWidth + extra))
        layout(constraints.maxWidth, p.height) { p.place(inset.roundToPx(), 0) }
    },
)

/** A film's page: backdrop, poster, details from TMDB (or the source), Play, and watched. */
@Composable
fun VodMovieScreen(nav: NavHostController, movieId: Long) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val changed by VodChanges.version.collectAsState()
    var movie by remember { mutableStateOf<VodMovieDetail?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var watchedOverride by remember { mutableStateOf<Boolean?>(null) }
    LaunchedEffect(movieId, changed) {
        withContext(Dispatchers.IO) { runCatching { marquee.livetv.getVodMovie(movieId) } }
            .onSuccess { movie = it; error = null; watchedOverride = null }
            .onFailure { if (movie == null) error = it.message ?: "Couldn't load the movie" }
    }
    val m = movie
    if (m == null) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            error?.let { Text(it, color = MaterialTheme.colorScheme.error) } ?: CircularProgressIndicator()
        }
        return
    }
    val watched = watchedOverride ?: m.watched
    // Marked here (watched, or unwatched, which forgets where it stopped): no resume point until the reload.
    val position = if (watchedOverride != null) 0 else m.positionMs ?: 0
    val resume = position > 0 && !watched
    fun play(from: Long?) = nav.navigate("vodplay/${m.id}" + (from?.let { "?start=$it" } ?: ""))
    fun setWatched(on: Boolean) {
        watchedOverride = on
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { if (on) marquee.livetv.markVodWatched(m.id) else marquee.livetv.markVodUnwatched(m.id) } }
                .onFailure { watchedOverride = null }
            VodChanges.bump()
        }
    }
    val meta = listOfNotNull(m.year?.toString(), m.contentRating, m.runtimeMs?.let(::formatRuntime))

    BoxWithConstraints(Modifier.fillMaxSize()) {
        val wide = maxWidth > 600.dp
        m.backdropUrl?.let { url ->
            Box(Modifier.fillMaxWidth().height(if (wide) 420.dp else 260.dp)) {
                AsyncImage(marquee.absolute(url), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop, alpha = 0.4f)
                Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, MaterialTheme.colorScheme.background))))
            }
        }
        LazyColumn(Modifier.fillMaxSize(), // Phones leave room for the back button above the poster.
            contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = if (marquee.isTv) 24.dp else 56.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)) {
            item {
                val poster = @Composable { w: Dp -> Artwork(marquee.absolute(m.posterUrl), m.title, Shape.Poster, Modifier.width(w)) }
                val details = @Composable {
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        m.category?.let { Text("VOD · $it", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelLarge) }
                        Text(m.title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                        if (m.inLibrary == true) InLibraryLabel()
                        if (meta.isNotEmpty()) Text(meta.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (m.genres.isNotEmpty()) Text(m.genres.joinToString(", "), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                        m.rating?.let {
                            Text("TMDB ${"%.1f".format(it)}", Modifier.clip(RoundedCornerShape(6.dp)).background(Surface2).padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold)
                        }
                        Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                            Button({ play(null) }, Modifier.focusRing().initialFocus(marquee.isTv)) {
                                Icon(Icons.Filled.PlayArrow, null)
                                Text(if (resume) "Resume from ${formatTime(position)}" else "Play", Modifier.padding(start = 6.dp))
                            }
                            if (resume) OutlinedButton({ play(0) }, Modifier.focusRing()) {
                                Icon(Icons.Filled.Replay, null)
                                Text("Play from start", Modifier.padding(start = 6.dp))
                            }
                        }
                        OutlinedButton({ setWatched(!watched) }, Modifier.focusRing().semantics {
                            contentDescription = if (watched) "Mark as unwatched" else "Mark as watched"
                            stateDescription = if (watched) "Watched" else "Not watched"
                        }) {
                            Icon(Icons.Filled.Check, null, tint = if (watched) Gold else MaterialTheme.colorScheme.onSurface)
                            Text(if (watched) "Watched" else "Mark watched", Modifier.padding(start = 6.dp))
                        }
                        // Part-way through: unwatched too, which forgets where it stopped (PLAY-5).
                        if (!watched && position > 0) OutlinedButton({ setWatched(false) }, Modifier.focusRing().semantics { contentDescription = "Mark as unwatched" }) {
                            Icon(Icons.Filled.RemoveDone, null)
                            Text("Mark unwatched", Modifier.padding(start = 6.dp))
                        }
                    }
                }
                if (wide) Row(horizontalArrangement = Arrangement.spacedBy(28.dp)) {
                    poster(200.dp)
                    Box(Modifier.weight(1f)) { details() }
                } else Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                    poster(150.dp)
                    details()
                }
            }
            m.tagline?.let { item { Text(it, fontStyle = FontStyle.Italic, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.titleMedium) } }
            m.overview?.let { item { Text(it, Modifier.widthIn(max = 720.dp)) } }
            if (!m.matched) item {
                Text("Details from the source (not matched to TMDB).", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            }
            m.director?.let { item { Text("Directed by $it", fontWeight = FontWeight.Medium) } }
            if (m.cast.isNotEmpty()) {
                item { Text("Cast", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
                item {
                    // Focusable on TV so the D-pad can scroll down to it.
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                        items(m.cast) { c ->
                            Column(Modifier.width(140.dp).focusCard({})) {
                                Text(c.name, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                c.character?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                            }
                        }
                    }
                }
            }
        }
        if (!marquee.isTv) IconButton({ nav.popBackStack() }, Modifier.padding(4.dp)) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
    }
}

/**
 * Full-screen playback of a film: the source's HLS through the server, seekable. The position
 * is reported every 10 s while playing, on pause and on closing (so it resumes, and counts as
 * watched near the end); the session stops when the player closes. No trailers, rating
 * request, watch together or casting: those are for library items.
 */
@Composable
fun VodPlayerScreen(nav: NavHostController, movieId: Long, startMs: Long?) = VodVideo(nav, movieId, null, null, startMs)

/** Full-screen playback of a show's episode (LIVE-6), with Up Next at the end: the next episode in viewing order. */
@Composable
fun VodEpisodePlayerScreen(nav: NavHostController, seriesId: Long, episodeId: Long, startMs: Long?) = VodVideo(nav, null, seriesId, episodeId, startMs)

/** "S1 · E2", or "Special 3" for season 0. */
internal fun VodEpisode.label(): String = if (season == 0) "Special $episode" else "S$season · E$episode"

/** The player for a film ([movieId]) or an episode ([seriesId], [episodeId]). */
@OptIn(UnstableApi::class)
@Composable
private fun VodVideo(nav: NavHostController, movieId: Long?, seriesId: Long?, episodeId: Long?, startMs: Long?) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val player = remember { videoPlayer(context, seekIncrements = true) }
    var movie by remember { mutableStateOf<VodMovieDetail?>(null) }
    var episode by remember { mutableStateOf<VodEpisode?>(null) }
    var series by remember { mutableStateOf<app.marquee.api.models.VodSeriesDetail?>(null) }
    var session by remember { mutableStateOf<VodSession?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var position by remember { mutableLongStateOf(0L) }
    var duration by remember { mutableLongStateOf(0L) }
    var playing by remember { mutableStateOf(false) }
    var buffering by remember { mutableStateOf(true) }
    var controls by remember { mutableStateOf(true) }
    var lastTouch by remember { mutableIntStateOf(0) }
    var scrub by remember { mutableStateOf<Long?>(null) }
    // Up Next (episodes): the next one in viewing order, fetched as playback starts; at the end its screen counts down to it.
    var next by remember { mutableStateOf<VodEpisode?>(null) }
    var upNext by remember { mutableStateOf(false) }
    var countdown by remember { mutableIntStateOf(UP_NEXT_SECONDS) }
    val ownEntry = remember { nav.currentBackStackEntry }
    fun stillShowing() = ownEntry != null && nav.currentBackStackEntry === ownEntry
    val what = if (episodeId != null) "episode" else "movie"

    /** The time limit was reached (kids' profiles): the player closes and the reason shows full screen. */
    fun timeUp(reason: String) {
        player.pause()
        marquee.timeLimits.block(reason)
        if (stillShowing()) nav.popBackStack()
    }
    suspend fun send(id: String, pos: Long, dur: Long): Throwable? {
        if (pos <= 0) return null
        return runCatching { marquee.livetv.reportVodPlayback(id, ReportVodPlaybackRequest(pos, dur.takeIf { it > 0 })) }.exceptionOrNull()
    }
    fun report() {
        val id = session?.id ?: return
        val pos = player.currentPosition
        val dur = player.duration
        marquee.scope.launch {
            // The time ran out while playing: the server ended the session.
            app.marquee.core.TimeLimitRules.reason(send(id, pos, dur))?.let { r ->
                withContext(Dispatchers.Main) { if (session?.id == id) { session = null; timeUp(r) } }
            }
        }
    }
    fun playNext() {
        val n = next ?: return
        if (stillShowing()) nav.navigate("vodepplay/${n.seriesId}/${n.id}") { popUpTo(ownEntry!!.destination.id) { inclusive = true } }
    }
    /** Up Next's Previous: this episode again, from the start. */
    fun replay() {
        if (stillShowing() && seriesId != null && episodeId != null)
            nav.navigate("vodepplay/$seriesId/$episodeId?start=0") { popUpTo(ownEntry!!.destination.id) { inclusive = true } }
    }

    LaunchedEffect(movieId, episodeId) {
        if (movieId != null) launch { movie = withContext(Dispatchers.IO) { runCatching { marquee.livetv.getVodMovie(movieId) }.getOrNull() } }
        if (seriesId != null && episodeId != null) {
            launch {
                series = withContext(Dispatchers.IO) { runCatching { marquee.livetv.getVodSeries(seriesId) }.getOrNull() }
                episode = series?.episodes?.firstOrNull { it.id == episodeId }
            }
            launch { next = withContext(Dispatchers.IO) { runCatching { marquee.livetv.vodNextEpisode(episodeId) }.getOrNull() }?.episode }
        }
        // Started on the session's own scope: if the player closes while the server is starting
        // it, the session is stopped as soon as it arrives rather than left running. The device's
        // profile lets the server copy what it plays (HEVC, AC3) rather than convert it.
        val pending = marquee.scope.async {
            runCatching {
                if (episodeId != null) marquee.livetv.playVodEpisode(episodeId, PlayVodEpisodeRequest(AndroidProfile.profile, startMs = startMs))
                else marquee.livetv.playVodMovie(movieId!!, PlayVodMovieRequest(AndroidProfile.profile))
            }
        }
        val result = try {
            pending.await()
        } catch (e: CancellationException) {
            marquee.scope.launch { pending.await().getOrNull()?.let { runCatching { marquee.livetv.stopVodSession(it.id) } } }
            throw e
        }
        withContext(Dispatchers.Main) {
            result.onSuccess { s ->
                session = s
                player.setMediaItem(MediaItem.fromUri(marquee.absolute(s.url)!!))
                player.prepare()
                val at = startMs ?: s.startMs ?: 0
                if (at > 0) player.seekTo(at)
                player.playWhenReady = true
            }.onFailure { e ->
                val limit = app.marquee.core.TimeLimitRules.reason(e)
                if (limit != null) timeUp(limit) else { error = vodStartError(e, what); buffering = false }
            }
        }
    }
    LaunchedEffect(Unit) {
        withContext(Dispatchers.Main) {
            var ticks = 0
            while (true) {
                position = player.currentPosition
                duration = player.duration.coerceAtLeast(0)
                if (player.isPlaying && ++ticks % 20 == 0) report()
                delay(500)
            }
        }
    }
    LaunchedEffect(controls, lastTouch, playing, scrub) {
        if (controls && playing && scrub == null) { delay(3000); controls = false }
    }
    LaunchedEffect(upNext) {
        if (!upNext) return@LaunchedEffect
        while (countdown > 0) { delay(1000); countdown-- }
        playNext()
    }
    VideoWindow()
    PauseInBackground(player)
    DisposableEffect(Unit) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                playing = isPlaying
                if (!isPlaying && player.playbackState != Player.STATE_ENDED) report()
            }
            override fun onPlaybackStateChanged(state: Int) {
                buffering = state == Player.STATE_BUFFERING || state == Player.STATE_IDLE
                if (state != Player.STATE_ENDED || !stillShowing()) return
                // The end: an episode with one after it counts down to that; otherwise closing
                // reports it (so it counts as watched) and stops the session.
                if (next != null) {
                    report()
                    controls = false
                    countdown = UP_NEXT_SECONDS
                    upNext = true
                } else nav.popBackStack()
            }
            override fun onPlayerError(e: androidx.media3.common.PlaybackException) {
                error = "The $what stopped playing (${e.errorCodeName})."
            }
        }
        player.addListener(listener)
        onDispose {
            val id = session?.id
            val pos = player.currentPosition
            val dur = player.duration
            player.removeListener(listener)
            player.release()
            // Where they stopped is recorded before the session ends (it's reported against it).
            marquee.scope.launch {
                if (id != null) {
                    send(id, pos, dur)
                    runCatching { marquee.livetv.stopVodSession(id) }
                }
                VodChanges.bump()
            }
        }
    }

    fun poke() { controls = true; lastTouch++ }
    fun seekBy(ms: Long) {
        scrub = ((scrub ?: player.currentPosition) + ms).coerceIn(0, duration.coerceAtLeast(1))
        poke()
    }
    fun commitScrub() { scrub?.let { player.seekTo(it) }; scrub = null; poke() }
    val rootFocus = remember { FocusRequester() }
    LaunchedEffect(controls) { if (!controls && !upNext) runCatching { rootFocus.requestFocus() } }
    val title = movie?.title ?: episode?.let { "${it.seriesTitle} · ${it.label()}" } ?: ""
    val subtitle = movie?.let { d -> listOfNotNull(d.year?.toString(), d.runtimeMs?.let(::formatRuntime)).joinToString(" · ") }
        ?: episode?.let { e -> listOfNotNull(e.title, e.runtimeMs?.let(::formatRuntime)).joinToString(" · ") } ?: ""

    Box(
        Modifier.fillMaxSize().background(Color.Black)
            .semantics {
                contentDescription = "Video player"
                stateDescription = if (playing) "Playing" else "Paused"
                progressBarRangeInfo = ProgressBarRangeInfo(position / 1000f, 0f..(duration / 1000f).coerceAtLeast(1f))
            }
            .focusRequester(rootFocus)
            // TV remote with the controls hidden, or mid-scrub: left/right scrub, select jumps there (or plays/pauses).
            .onPreviewKeyEvent { e ->
                if (upNext || e.type != KeyEventType.KeyDown || (controls && scrub == null)) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionLeft -> { seekBy(-10_000); true }
                    Key.DirectionRight -> { seekBy(10_000); true }
                    Key.DirectionCenter, Key.Enter, Key.MediaPlayPause -> {
                        if (scrub != null) commitScrub() else { if (player.isPlaying) player.pause() else player.play(); poke() }
                        true
                    }
                    Key.DirectionUp, Key.DirectionDown -> { scrub = null; poke(); true }
                    Key.Back, Key.Escape -> if (scrub != null) { scrub = null; true } else false
                    else -> false
                }
            }
            .focusable()
            .clickable(remember { MutableInteractionSource() }, indication = null) { if (controls) controls = false else poke() },
    ) {
        AndroidView({
            PlayerView(it).apply {
                this.player = player
                useController = false
                keepScreenOn = true
                resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            }
        }, Modifier.fillMaxSize())
        if (buffering && error == null && !upNext) CircularProgressIndicator(Modifier.align(Alignment.Center))
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp).semantics { contentDescription = "Playback error" }, color = MaterialTheme.colorScheme.error) }
        if (!controls && scrub != null) Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 48.dp, vertical = 32.dp)) {
            ScrubBar(scrub!!, duration, null, onChange = { scrub = it }, onDone = ::commitScrub)
        }
        AnimatedVisibility(controls && !upNext, Modifier.fillMaxSize(), enter = fadeIn(), exit = fadeOut()) {
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color.Black.copy(alpha = 0.7f), 0.3f to Color.Transparent, 0.65f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.8f)))) {
                Row(Modifier.align(Alignment.TopStart).fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton({ nav.popBackStack() }, Modifier.focusRing()) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Close player", tint = Color.White) }
                    Column(Modifier.weight(1f).padding(start = 8.dp)) {
                        Text(title, color = Color.White, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (subtitle.isNotEmpty()) Text(subtitle, color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
                Row(Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(36.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton({ player.seekBack(); poke() }, Modifier.focusRing().size(56.dp)) { Icon(Icons.Filled.Replay10, "Back 10 seconds", Modifier.size(36.dp), tint = Color.White) }
                    IconButton({ if (player.isPlaying) player.pause() else player.play(); poke() }, Modifier.focusRing().size(76.dp).initialFocus(marquee.isTv)) {
                        Icon(if (playing) Icons.Filled.Pause else Icons.Filled.PlayArrow, if (playing) "Pause" else "Play", Modifier.size(56.dp), tint = Color.White)
                    }
                    IconButton({ player.seekForward(); poke() }, Modifier.focusRing().size(56.dp)) { Icon(Icons.Filled.Forward30, "Forward 30 seconds", Modifier.size(36.dp), tint = Color.White) }
                }
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 24.dp, vertical = 16.dp)) {
                    ScrubBar(scrub ?: position, duration, null, scrubbing = scrub != null, onChange = { scrub = it; poke() }, onDone = ::commitScrub)
                }
            }
        }
        // The end of an episode: the Up Next screen, as library shows have it.
        val n = next
        if (upNext && n != null) {
            fun entry(e: VodEpisode) = UpNextEntry(e.seriesTitle, e.title, episodeLabel(e.season, e.episode),
                marquee.absolute(e.stillUrl ?: series?.backdropUrl), e.overview)
            val cur = episode
            UpNextScreen(
                previous = cur?.let(::entry) ?: UpNextEntry(n.seriesTitle, "", null, null),
                next = entry(n),
                backdropUrl = marquee.absolute(series?.backdropUrl ?: n.stillUrl),
                seconds = countdown, held = false,
                onPlayNext = { playNext() }, onReplay = { replay() },
                onCancel = { if (stillShowing()) nav.popBackStack() },
            )
        }
    }
}

/** Admins: the movies-on-demand sources' status and Refresh now. Sources are edited on the web. */
@Composable
fun VodAdminSection() {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var status by remember { mutableStateOf<VodStatus?>(null) }
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { status = withContext(Dispatchers.IO) { runCatching { marquee.livetv.vodStatus() }.getOrNull() } }
    Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("Films from M3U sources under Live TV. Add or change sources in the web app's Settings → VOD.",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        val s = status
        if (s == null) Text("Not available on this server.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        else {
            Text(if (s.sources.isEmpty()) "No sources" else "${s.movies} movies · ${s.matched} matched to TMDB" + if (s.matching) " · matching now" else "",
                Modifier.semantics { contentDescription = "Movies on demand status" })
            s.sources.forEach { src ->
                Text("${src.name}: ${src.error ?: "${src.movies} movies"}", style = MaterialTheme.typography.bodySmall,
                    color = if (src.error != null) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
            }
            OutlinedButton({
                busy = true
                scope.launch {
                    withContext(Dispatchers.IO) { runCatching { marquee.livetv.refreshVod() } }
                        .onSuccess { status = it; message = "Refreshed."; marquee.refreshLiveTv(); VodChanges.bump() }
                        .onFailure { message = "Couldn't refresh: ${it.message}" }
                    busy = false
                }
            }, Modifier.focusRing(), enabled = !busy) {
                Icon(Icons.Filled.Refresh, null)
                Text(if (busy) "Refreshing…" else "Refresh now", Modifier.padding(start = 6.dp))
            }
            message?.let { Text(it, color = if (it == "Refreshed.") Gold else MaterialTheme.colorScheme.error) }
        }
    }
}
