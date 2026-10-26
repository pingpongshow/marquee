package app.marquee.ui

import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.background
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.CloudUpload
import androidx.compose.material.icons.filled.LiveTv
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.RemoveCircleOutline
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.TextButton
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Animation
import androidx.compose.material.icons.filled.LibraryMusic
import androidx.compose.material.icons.filled.LocalMovies
import androidx.compose.material.icons.filled.OndemandVideo
import androidx.compose.material.icons.filled.Photo
import androidx.compose.material.icons.filled.VideoLibrary
import androidx.compose.material.icons.filled.Equalizer
import androidx.compose.material.icons.filled.Explore
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import app.marquee.api.models.LibraryType
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.FilterChip
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.foundation.layout.size
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.HomeEntry
import app.marquee.api.models.Hub
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.Library
import app.marquee.api.models.PlaylistKind
import app.marquee.api.models.SearchResults
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Opens an item: videos in progress play, everything else shows its page. */
fun openItem(nav: NavHostController, item: ItemSummary, play: Boolean = false) {
    if (play && (item.type == ItemType.MOVIE || item.type == ItemType.EPISODE || item.type == ItemType.VIDEO)) nav.navigate("player/${item.id}")
    else nav.navigate("item/${item.id}")
}

@Composable
fun HomeScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val connection by marquee.connection.collectAsState()
    val me by marquee.me.collectAsState()
    var groups by remember { mutableStateOf(emptyList<app.marquee.api.models.WatchGroup>()) }
    LaunchedEffect(connection) {
        while (true) {
            groups = withContext(Dispatchers.IO) { runCatching { marquee.syncplay.listWatchGroups() }.getOrDefault(emptyList()) }
                .filter { g -> g.members.none { it.userId == me?.id } }
            kotlinx.coroutines.delay(15_000)
        }
    }
    // Reloads after watched marks or a removal from Continue Watching (USER-6), keeping the rows shown meanwhile.
    val changes by marquee.watch.changes.collectAsState()
    // And after a favourite changed, for the Your Favourites row (USER-12).
    val favoriteChanges by marquee.favorites.changes.collectAsState()
    // Removed from Continue Watching: gone at once, until the row reloads.
    val removed = remember { mutableStateListOf<String>() }
    // Kept while Home is on the back stack: coming back shows the same rows at once (refreshed in place), so it keeps its place.
    val hubsState = rememberCached("home-hubs") { mutableStateOf<Result<List<Hub>>?>(null) }
    val hubs by hubsState
    var opened by rememberLastOpened("home")
    // A Continue Watching tile opened last (its key: films and episodes on demand have no item id), for the D-pad's return.
    var openedKey by rememberCached("home:openedKey") { mutableStateOf<String?>(null) }
    // Films and episodes on demand report where they stopped as their player closes.
    val vodChanged by VodChanges.version.collectAsState()
    // Watch on VOD's title opens VOD Movies while it's listed.
    val vodMovies by marquee.vodMovies.collectAsState()
    LaunchedEffect(connection, changes, vodChanged, favoriteChanges) {
        val fresh = withContext(Dispatchers.IO) { runCatching { marquee.hubs.homeHubs(vod = true, mixed = true) } }
        if (fresh.isSuccess || hubsState.value?.isSuccess != true) { hubsState.value = fresh; if (fresh.isSuccess) removed.clear() }
    }
    val cardWidth = if (marquee.isTv) 130.dp else 120.dp
    when (val h = hubs) {
        null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        else -> LazyColumn(contentPadding = PaddingValues(vertical = 20.dp), verticalArrangement = Arrangement.spacedBy(26.dp)) {
            item {
                Column {
                    // Phones and tablets: who's watching, top left (TV has it at the top of the side menu).
                    if (!marquee.isTv) ProfileButton(nav, modifier = Modifier.padding(start = sidePadding - 4.dp, bottom = 8.dp))
                    Row(Modifier.padding(horizontal = sidePadding).fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text("Home", Modifier.weight(1f), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                        // Muse: describe what to watch (USER-15).
                        if (!marquee.isOffline) IconButton({ nav.navigate("muse") }, Modifier.focusRing()) { Icon(Icons.Filled.AutoAwesome, "Muse", tint = Gold) }
                        // Rows: order, hide and pinned collections and playlists (USER-12).
                        if (!marquee.isOffline) androidx.compose.material3.TextButton({ nav.navigate("edithome") }, Modifier.focusRing()) {
                            Icon(Icons.Filled.Edit, null)
                            Text("Edit Home", Modifier.padding(start = 6.dp))
                        }
                    }
                }
            }
            // Others watching together, to join (SYNC-1).
            items(groups, key = { "g" + it.id }) { g ->
                Row(Modifier.padding(horizontal = sidePadding).fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(Gold.copy(alpha = 0.12f))
                    .focusCard({ nav.navigate("player/${g.itemId}?group=${g.id}") }, RoundedCornerShape(12.dp)).semantics { contentDescription = "Join ${g.title}" }
                    .padding(14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Icon(Icons.Filled.Groups, null, tint = Gold)
                    Column(Modifier.weight(1f)) {
                        Text(g.title, fontWeight = FontWeight.SemiBold, maxLines = 1)
                        Text("${g.members.joinToString { it.name }} ${if (g.members.size == 1) "is" else "are"} watching together", style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                    }
                    Text("Join", color = Gold, fontWeight = FontWeight.Bold)
                }
            }
            marquee.lastError?.let { e ->
                item {
                    Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(e, color = MaterialTheme.colorScheme.error)
                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            // Back on the network (or Tailscale) without waiting for the app to notice.
                            var retrying by remember { mutableStateOf(false) }
                            Button({ retrying = true; marquee.scope.launch { runCatching { marquee.reconnect() }; retrying = false } },
                                Modifier.focusRing().initialFocus(marquee.isTv), enabled = !retrying) { Text(if (retrying) "Trying…" else "Try again") }
                            if (!marquee.isTv) OutlinedButton({ nav.navigate("downloads") }, Modifier.focusRing()) { Text("Open Downloads") }
                        }
                    }
                }
            }
            if (!marquee.isOffline) h.exceptionOrNull()?.let { e -> item { Text("Couldn't load: ${serverMessage(e)}", Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error) } }
            itemsIndexed(h.getOrDefault(emptyList()), key = { _, it -> it.id }) { hi, hub ->
                val first = opened == null && openedKey == null && hi == 0
                // Every row's See all (USER-12): its page; on TV the D-pad comes back to its tile.
                val seeAllKey = "seeall:${hub.id}"
                val seeAllFocus = openedKey == seeAllKey
                val seeAll = { openedKey = seeAllKey; opened = null; nav.navigate(HomeRows.seeAllRoute(hub.id)) }
                // Continue Watching: library titles and films and episodes on demand together, with See all (LIVE-6).
                if (hub.id == CONTINUE_HUB) {
                    ContinueRow(nav, hub, removed, cardWidth * 1.6f, focusFirst = first, openedKey = openedKey?.takeIf { it != seeAllKey },
                        onOpen = { k -> openedKey = k; opened = null }, onRemoved = { k -> removed.add(k) },
                        seeAllFocus = seeAllFocus, onSeeAll = { openedKey = seeAllKey; opened = null })
                    return@itemsIndexed
                }
                // Suggestion rows with films and series on demand mixed in (Recommended, holidays, Top Rated, genres…).
                hub.mixedEntries?.let { entries ->
                    MixedRow(nav, hub, entries, cardWidth, focusFirst = first, opened = opened, openedKey = openedKey,
                        onOpenItem = { id -> opened = id; openedKey = null }, onOpenVod = { k -> openedKey = "vodsug:$k"; opened = null },
                        onSeeAll = seeAll, seeAllFocus = seeAllFocus)
                    return@itemsIndexed
                }
                // Watch on VOD: films and series on demand picked for this person (LIVE-6, USER-16), hidden when empty.
                // Older servers only: asking for mixed rows leaves it out.
                hub.vodEntries?.let { vod ->
                    VodSuggestionRow(nav, hub.title, vod, autoFocusKey = openedKey?.takeIf { it.startsWith("vodsug:") }?.removePrefix("vodsug:")
                        ?: if (first) vod.firstOrNull()?.key else null,
                        onTitle = if (vodMovies) ({ nav.navigate("vod") }) else null,
                        onOpen = { k -> openedKey = "vodsug:$k"; opened = null })
                    return@itemsIndexed
                }
                if (hub.items.isEmpty()) return@itemsIndexed
                Shelf(hub.title, hub.items, sidePadding, onSeeAll = seeAll, seeAllTileHeight = cardWidth / shapeFor(hub.items.first()).ratio,
                    seeAllFocus = seeAllFocus) { i, it ->
                    PosterCard(it, marquee.imageUrl(it.images?.poster, 300), cardWidth, { opened = it.id; openedKey = null; openItem(nav, it) },
                        shape = shapeFor(it), autoFocus = marquee.isTv && (if (opened != null) it.id == opened else first && i == 0),
                        actions = tileActions(it, nav = nav))
                }
            }
        }
    }
}

/**
 * A Home row of library titles and films and series on demand together, best first: library
 * tiles open the item, VOD ones (marked with a VOD pill) the film's or show's page, with Not
 * interested in their menu. Nothing when every entry is gone.
 */
@Composable
private fun MixedRow(nav: NavHostController, hub: Hub, entries: List<HomeEntry>, cardWidth: androidx.compose.ui.unit.Dp, focusFirst: Boolean,
    opened: Long?, openedKey: String?, onOpenItem: (Long) -> Unit, onOpenVod: (String) -> Unit, onSeeAll: () -> Unit, seeAllFocus: Boolean) {
    val marquee = LocalMarquee.current
    val context = androidx.compose.ui.platform.LocalContext.current
    val hidden = remember { mutableStateListOf<String>() }
    val shown = entries.filter { e ->
        e.key !in hidden && when (e.kind) {
            HomeEntry.Kind.ITEM -> e.item != null
            HomeEntry.Kind.VOD -> e.vod?.let(::vodRoute) != null
        }
    }
    if (shown.isEmpty()) return
    val vodOpened = openedKey?.takeIf { it.startsWith("vodsug:") }?.removePrefix("vodsug:")
    Shelf(hub.title, shown, sidePadding, onSeeAll = onSeeAll, seeAllTileHeight = cardWidth / Shape.Poster.ratio, seeAllFocus = seeAllFocus) { i, e ->
        val focus = marquee.isTv && when {
            opened != null -> e.item?.id == opened
            vodOpened != null -> e.vod?.key == vodOpened
            else -> focusFirst && i == 0
        }
        val item = e.item
        val vod = e.vod
        if (item != null) PosterCard(item, marquee.imageUrl(item.images?.poster, 300), cardWidth, { onOpenItem(item.id); openItem(nav, item) },
            shape = shapeFor(item), autoFocus = focus, actions = tileActions(item, nav = nav))
        else if (vod != null) VodSuggestionTile(nav, vod, cardWidth, autoFocus = focus, badge = true, watchlist = hub.id == "watchlist",
            onOpen = { onOpenVod(vod.key) }) {
            if (hub.id == "watchlist") removeVodFromWatchlist(marquee, context, vod, onRemoved = { hidden.add(e.key) }, onFailed = { hidden.remove(e.key) })
            else hideVodSuggestion(marquee, context, vod, onHidden = { hidden.add(e.key) }, onFailed = { hidden.remove(e.key) })
        }
    }
}

@Composable
fun LibrariesScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    // Only while Live TV has channels this profile may watch (LIVE-1); checked again on opening.
    val liveOn by marquee.liveTv.collectAsState()
    // VOD (LIVE-6): VOD Movies and VOD Shows, listed after Live TV (or in its place).
    val vodMovies by marquee.vodMovies.collectAsState()
    val vodShows by marquee.vodShows.collectAsState()
    LaunchedEffect(Unit) { marquee.refreshLiveTv() }
    val requests by produceState<app.marquee.api.models.RequestsStatus?>(null) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.requests.requestsStatus() }.getOrNull() }
    }
    val libs by produceState<List<Library>?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.libraries.listLibraries() }.getOrDefault(emptyList()) } }
    // TV: the entry last opened has the focus on coming back (the first library before that).
    var opened by rememberCached("libraries:opened") { mutableStateOf<String?>(null) }
    @Composable fun entry(route: String, first: Boolean = false) = Modifier.initialFocus(marquee.isTv && (opened == route || (opened == null && first)))
        .focusCard({ opened = route; nav.navigate(route) })
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Libraries", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        // Discover (REQ-1) when this person may request titles.
        if (requests?.enabled == true && requests?.canRequest == true) item {
            ListItem(
                headlineContent = { Text("Discover") },
                supportingContent = { Text("Find and request movies and shows") },
                leadingContent = { Icon(Icons.Filled.Explore, null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = entry("discover"),
            )
            HorizontalDivider()
        }
        // Phones and tablets keep downloads; TVs stream.
        if (!marquee.isTv) item {
            ListItem(
                headlineContent = { Text("Downloads") },
                supportingContent = { Text("On this device") },
                leadingContent = { Icon(Icons.Filled.DownloadDone, null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = entry("downloads"),
            )
            HorizontalDivider()
        }
        itemsIndexed(libs ?: emptyList(), key = { _, it -> it.id }) { i, lib ->
            ListItem(
                headlineContent = { Text(lib.name) },
                supportingContent = { Text(libraryCount(lib.type, lib.itemCount)) },
                leadingContent = { Icon(libraryIcon(lib.type), null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = entry("library/${lib.id}", first = i == 0),
            )
            HorizontalDivider()
        }
        // Live TV sits with the libraries (not a tab of its own), once it's set up.
        if (liveOn) item(key = "livetv") {
            ListItem(
                headlineContent = { Text("Live TV") },
                supportingContent = { Text("Guide, channels and recordings") },
                leadingContent = { Icon(Icons.Filled.LiveTv, null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = entry("livetv"),
            )
            HorizontalDivider()
        }
        // VOD (LIVE-6): VOD Movies and VOD Shows, pages of their own right after Live TV, each while it has something.
        if (vodMovies) item(key = "vod") {
            ListItem(
                headlineContent = { Text("VOD Movies") },
                supportingContent = { Text("Movies on demand") },
                leadingContent = { Icon(Icons.Filled.Movie, null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = entry("vod"),
            )
            HorizontalDivider()
        }
        if (vodShows) item(key = "vodshows") {
            ListItem(
                headlineContent = { Text("VOD Shows") },
                supportingContent = { Text("Shows on demand") },
                leadingContent = { Icon(Icons.Filled.OndemandVideo, null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = entry("vodshows"),
            )
            HorizontalDivider()
        }
    }
}

/** A library's size in what it holds: "303 movies", "1 show", "10 artists". */
internal fun libraryCount(type: LibraryType, count: Int): String {
    val (one, many) = when (type) {
        LibraryType.MOVIES -> "movie" to "movies"
        LibraryType.SHOWS, LibraryType.ANIME -> "show" to "shows"
        LibraryType.MUSIC -> "artist" to "artists"
        LibraryType.VIDEOS -> "video" to "videos"
        LibraryType.PHOTOS -> "photo" to "photos"
    }
    return "$count ${if (count == 1) one else many}"
}

private fun libraryIcon(type: LibraryType) = when (type) {
    LibraryType.MOVIES -> Icons.Filled.LocalMovies
    LibraryType.SHOWS -> Icons.Filled.Tv
    LibraryType.ANIME -> Icons.Filled.Animation
    LibraryType.MUSIC -> Icons.Filled.LibraryMusic
    LibraryType.VIDEOS -> Icons.Filled.VideoLibrary
    LibraryType.PHOTOS -> Icons.Filled.Photo
}

/** A library grid's sort (the same choices as the Apple apps', less Random). */
enum class LibrarySort(val label: String, val api: app.marquee.api.apis.ItemsApi.SortListLibraryItems, val key: String) {
    Title("Title", app.marquee.api.apis.ItemsApi.SortListLibraryItems.TITLE, "title"),
    Added("Recently added", app.marquee.api.apis.ItemsApi.SortListLibraryItems._ADDED, "added"),
    Released("Release date", app.marquee.api.apis.ItemsApi.SortListLibraryItems._RELEASED, "released"),
    Rating("Rating", app.marquee.api.apis.ItemsApi.SortListLibraryItems._RATING, "rating"),
    MyRating("My rating", app.marquee.api.apis.ItemsApi.SortListLibraryItems._MY_RATING, "myRating"),
    Favorited("Date favourited", app.marquee.api.apis.ItemsApi.SortListLibraryItems._FAVORITED, "favorited"),
    Viewed("Last watched", app.marquee.api.apis.ItemsApi.SortListLibraryItems._VIEWED, "viewed");

    companion object {
        fun of(key: String?) = entries.firstOrNull { it.key == key } ?: Title
    }
}

/** A library's grid, a page at a time as you scroll. */
@Composable
fun LibraryScreen(nav: NavHostController, libraryId: Long, initialSort: String? = null) {
    val marquee = LocalMarquee.current
    // Kept while the library is on the back stack, so coming back keeps its place in the grid.
    val items = rememberCached("library-$libraryId-items") { mutableStateListOf<ItemSummary>() }
    var total by rememberCached("library-$libraryId-total") { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var library by rememberCached("library-$libraryId") { mutableStateOf<Library?>(null) }
    var opened by rememberLastOpened("library-$libraryId")
    // Movie libraries also browse by collection (META-7).
    var collections by rememberSaveable { mutableStateOf(false) }
    // Sort by title, the person's own rating or when they favourited it; only what they rated
    // 4★ or more; only their favourites (the heart, USER-12).
    // Home's Recently Added and Recently Played rows open their library in that order (USER-12).
    var sort by rememberSaveable { mutableStateOf(LibrarySort.of(initialSort)) }
    val byMyRating = sort == LibrarySort.MyRating
    var fav by rememberSaveable { mutableStateOf(false) }
    var hearts by rememberSaveable { mutableStateOf(false) }
    var unwatched by rememberSaveable { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    // Bumped when the sort, filter or collections view changes, so a page still loading for the old one is dropped.
    var generation by rememberCached("library-$libraryId-generation") { mutableIntStateOf(0) }
    fun loadMore() {
        if (loading || (total >= 0 && items.size >= total)) return
        loading = true
        val gen = generation
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.listLibraryItems(libraryId, if (collections) ItemType.COLLECTION else null,
                if (collections) app.marquee.api.apis.ItemsApi.SortListLibraryItems.TITLE else sort.api,
                offset = items.size, limit = 120, minMyRating = if (fav && !collections) app.marquee.music.FAVORITE_RATING else null,
                favorites = if (hearts && !collections) true else null,
                watch = if (unwatched && !collections) app.marquee.api.apis.ItemsApi.WatchListLibraryItems.UNWATCHED else null) } }
                .onSuccess { page -> if (gen == generation) { items.addAll(page.items.filter { n -> items.none { it.id == n.id } }); total = page.total } }
            if (gen == generation) loading = false
        }
    }
    fun reload() { generation++; loading = false; items.clear(); total = -1; loadMore() }
    LaunchedEffect(libraryId) {
        library = withContext(Dispatchers.IO) { runCatching { marquee.libraries.getLibrary(libraryId) }.getOrNull() } ?: library
        if (library?.type != LibraryType.MUSIC) loadMore()
    }
    val min = if (marquee.isTv) 130.dp else 110.dp
    // Music libraries open on an organised home; the full artist grid is its Artists page.
    library?.takeIf { it.type == LibraryType.MUSIC }?.let { app.marquee.music.MusicHome(nav, it); return }
    Column {
        Row(Modifier.padding(horizontal = sidePadding, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(library?.name ?: "", Modifier.weight(1f), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            // Muse for movies and shows (USER-15).
            if (library?.type in listOf(LibraryType.MOVIES, LibraryType.SHOWS, LibraryType.ANIME)) IconButton({ nav.navigate("muse?lib=$libraryId") }, Modifier.focusRing()) {
                Icon(Icons.Filled.AutoAwesome, "Muse", tint = Gold)
            }
            if (library?.type == LibraryType.MOVIES) listOf(false to "All", true to "Collections").forEach { (c, label) ->
                androidx.compose.material3.FilterChip(collections == c, {
                    if (collections != c) { collections = c; reload() }
                }, { Text(label) }, Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)))
            }
        }
        if (!collections) androidx.compose.foundation.lazy.LazyRow(Modifier.semantics { contentDescription = "Sort and filter" },
            contentPadding = PaddingValues(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(LibrarySort.entries) { o ->
                FilterChip(sort == o, { if (sort != o) { sort = o; reload() } }, { Text(o.label, maxLines = 1) }, Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)))
            }
            item {
                FilterChip(hearts, { hearts = !hearts; reload() }, { Text("Favourites", maxLines = 1) }, Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)),
                    leadingIcon = { Icon(Icons.Filled.Favorite, null, Modifier.size(18.dp), tint = Gold) })
            }
            item {
                FilterChip(unwatched, { unwatched = !unwatched; reload() }, { Text("Unwatched", maxLines = 1) }, Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)))
            }
            item {
                FilterChip(fav, { fav = !fav; reload() }, { Text("Rated 4★+", maxLines = 1) }, Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)),
                    leadingIcon = { Icon(Icons.Filled.Star, null, Modifier.size(18.dp), tint = Gold) })
            }
        }
        if (total == 0) Text(when {
                collections -> "Nothing here yet."
                listOf(hearts, fav, unwatched).count { it } > 1 -> "Nothing matches these filters."
                unwatched -> "You've watched everything here."
                hearts -> "No favourites here yet. Tap the heart on a title to add it."
                fav -> "Nothing rated 4★ or more yet."
                else -> "Nothing here yet."
            }, Modifier.padding(sidePadding),
            color = MaterialTheme.colorScheme.onSurfaceVariant)
        LazyVerticalGrid(GridCells.Adaptive(min), Modifier.semantics { contentDescription = "Library items" }, contentPadding = PaddingValues(sidePadding),
            horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            itemsIndexed(items, key = { _, it -> it.id }) { i, it ->
                if (i >= items.size - 30) LaunchedEffect(i) { loadMore() }
                PosterCard(it, marquee.imageUrl(it.images?.poster, 240), min, { opened = it.id; openItem(nav, it) },
                    autoFocus = marquee.isTv && (if (opened != null) it.id == opened else i == 0),
                    showRating = byMyRating || fav, actions = tileActions(it, nav = nav))
            }
        }
    }
}

/** Where entry [i] of [items] is among those that [match] (-1 when it doesn't): a duplicate gets its own place, not the first's. */
internal fun <T> positionAmong(items: List<T>, i: Int, match: (T) -> Boolean): Int =
    if (i !in items.indices || !match(items[i])) -1 else (0 until i).count { match(items[it]) }

@Composable
fun PlaylistsScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    // Kept while on the back stack, so coming back keeps the list's place.
    val loaded by cachedLoad("playlists") { withContext(Dispatchers.IO) { runCatching { marquee.playlists.listPlaylists() } } }
    val lists = loaded?.getOrNull() ?: emptyList()
    var opened by rememberLastOpened("playlists")
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Playlists", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        // Loading, couldn't load, or none yet (not "No playlists yet" while they're on their way).
        when {
            loaded == null -> item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            loaded?.isFailure == true -> item { Text("Couldn't load your playlists: ${serverMessage(loaded!!.exceptionOrNull()!!)}", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.error) }
            lists.isEmpty() -> item {
                Text("No playlists yet. Choose Add to playlist on a movie, episode, album or song to start one.", Modifier.padding(sidePadding),
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        // Video and music playlists, each under its own heading (USER-7), with their poster mosaics.
        val (videos, music) = lists.partition { it.kind == PlaylistKind.VIDEO }
        listOf("Video playlists" to videos, "Music playlists" to music).filter { it.second.isNotEmpty() }.forEach { (heading, group) ->
            if (videos.isNotEmpty()) item(key = heading) {
                Text(heading, Modifier.padding(start = sidePadding, end = sidePadding, top = 16.dp, bottom = 4.dp).semantics { this.heading() },
                    style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            }
            items(group, key = { it.id }) { p ->
                ListItem(
                    headlineContent = { Text(p.title) },
                    supportingContent = { Text("${p.itemCount} ${if (p.kind == PlaylistKind.AUDIO) "tracks" else "videos"}") },
                    leadingContent = { PlaylistMosaic(p.imageIds, p.title, Modifier.size(56.dp)) },
                    trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                    colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                    modifier = Modifier.initialFocus(marquee.isTv && (if (opened != null) opened == p.id else p == lists.first()))
                        .focusCard({ opened = p.id; nav.navigate("playlist/${p.id}") }),
                )
            }
        }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun PlaylistScreen(nav: NavHostController, playlistId: Long) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    // Kept while on the back stack, so coming back keeps the list's place.
    // Bumped after a rename or a removal, to load the playlist again.
    var version by remember { mutableIntStateOf(0) }
    val loadedEntries by cachedLoad("playlist-$playlistId-items", version) { withContext(Dispatchers.IO) { runCatching { marquee.playlists.listPlaylistItems(playlistId).items }.getOrNull() } }
    // Removed here: gone at once, before the playlist comes back from the server.
    val removed = remember { mutableStateListOf<Long>() }
    val entries = (loadedEntries ?: emptyList()).filter { it.entryId !in removed }
    val playlist by cachedLoad("playlist-$playlistId", version) { withContext(Dispatchers.IO) { runCatching { marquee.playlists.getPlaylist(playlistId) }.getOrNull() } }
    val title = playlist?.title ?: ""
    val context = androidx.compose.ui.platform.LocalContext.current
    // A smart playlist fills itself from its rules: nothing to remove by hand.
    val editable = playlist != null && playlist?.rules == null
    fun removeEntry(entryId: Long, what: String) {
        removed.add(entryId)
        marquee.scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.playlists.removePlaylistItem(playlistId, entryId) } }
                .onSuccess { version++ }
                .onFailure { removed.remove(entryId); toast(context, "Couldn't remove “$what”: ${serverMessage(it)}") }
        }
    }
    val tracks = entries.map { it.item }
    var opened by rememberLastOpened("playlist-$playlistId")
    // A video playlist plays in the video player, in order, each video going on to the next (USER-7).
    fun isVideo(x: ItemSummary) = x.type in app.marquee.core.WatchState.videoTypes && x.type != ItemType.SHOW && x.type != ItemType.SEASON
    val videos = tracks.filter(::isVideo)
    fun playVideos(list: List<ItemSummary>) {
        if (list.isEmpty()) return
        marquee.videoQueue = list.map { it.id }
        nav.navigate("player/${list.first().id}")
    }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item {
            Column(Modifier.padding(horizontal = sidePadding)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    playlist?.let { PlaylistMosaic(it.imageIds, it.title, Modifier.size(if (marquee.isTv) 120.dp else 88.dp)) }
                    Text(title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                }
                // Full width, wrapping onto another line rather than squeezing a button's label.
                FlowRow(Modifier.fillMaxWidth().padding(vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (tracks.any { it.type == ItemType.TRACK }) {
                        Button(onClick = { music.play(tracks, 0, source = title) }, modifier = Modifier.focusRing().initialFocus(marquee.isTv && opened == null)) {
                            Icon(Icons.Filled.PlayArrow, null); Text("Play", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                        OutlinedButton(onClick = { music.play(tracks.shuffled(), 0, source = title) }, modifier = Modifier.focusRing()) {
                            Icon(Icons.Filled.Shuffle, null); Text("Shuffle", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                        if (!marquee.isTv) PlaylistDownloadButton(playlistId)
                    } else if (videos.isNotEmpty()) {
                        Button(onClick = { playVideos(videos) }, modifier = Modifier.focusRing().initialFocus(marquee.isTv && opened == null)) {
                            Icon(Icons.Filled.PlayArrow, null); Text("Play", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                        OutlinedButton(onClick = { playVideos(videos.shuffled()) }, modifier = Modifier.focusRing()) {
                            Icon(Icons.Filled.Shuffle, null); Text("Shuffle", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                    }
                    PinToHomeButton("playlist-$playlistId")
                    playlist?.let { p -> PlaylistMenu(p, nav, onRenamed = { version++ }) }
                }
            }
        }
        items(entries.size) { i ->
            val it = entries[i].item
            // Its own place among the videos (a video in the playlist twice plays on from this copy, not the first).
            val vi = positionAmong(entries, i) { e -> isVideo(e.item) }
            var menu by remember { mutableStateOf(false) }
            // TV: Right from the row reaches its heart (and stars), which sit inside it.
            val hearted = it.type in app.marquee.core.FavoriteState.types
            val inRow = rememberInRowFocus()
            ListItem(
                headlineContent = { Text(it.title) },
                supportingContent = { Text(subtitleFor(it)) },
                leadingContent = if (it.type == ItemType.TRACK) null else { { Artwork(marquee.imageUrl(it.images?.thumb ?: it.images?.poster, 160), it.title, Shape.Wide, Modifier.width(96.dp)) } },
                trailingContent = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (hearted) FavoriteButton(it, Modifier.inRowFirst(inRow), small = true)
                        if (it.type == ItemType.TRACK) app.marquee.music.TrackRating(it)
                        if (editable) {
                            Box {
                                // TV: the D-pad can't reach it; holding OK (or Menu) on the row opens the same menu.
                                if (!marquee.isTv) IconButton({ menu = true }, Modifier.focusRing()) { Icon(Icons.Filled.MoreVert, "More for ${it.title}") }
                                DropdownMenu(menu, { menu = false }) {
                                    DropdownMenuItem({ Text("Remove from playlist") }, { menu = false; removeEntry(entries[i].entryId, it.title) },
                                        leadingIcon = { Icon(Icons.Filled.RemoveCircleOutline, null) })
                                }
                            }
                        }
                    }
                },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                // A video plays from here on through the rest of the playlist.
                modifier = Modifier.initialFocus(marquee.isTv && opened == it.id).then(if (hearted) Modifier.rowFocus(inRow) else Modifier)
                    .focusCard({ opened = it.id; if (it.type == ItemType.TRACK) music.play(tracks, i, source = title) else if (vi >= 0) playVideos(videos.drop(vi)) else openItem(nav, it) },
                        onLongClick = if (editable) ({ menu = true }) else null),
            )
        }
    }
}

/** A playlist's "…": Rename and Delete (with a confirmation), for its owner's own playlists. */
@Composable
private fun PlaylistMenu(p: app.marquee.api.models.Playlist, nav: NavHostController, onRenamed: () -> Unit) {
    val marquee = LocalMarquee.current
    val context = androidx.compose.ui.platform.LocalContext.current
    var open by remember { mutableStateOf(false) }
    var renaming by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf(false) }
    Box {
        IconButton({ open = true }, Modifier.focusRing()) { Icon(Icons.Filled.MoreVert, "Playlist actions") }
        DropdownMenu(open, { open = false }) {
            DropdownMenuItem({ Text("Rename…") }, { open = false; renaming = true }, leadingIcon = { Icon(Icons.Filled.Edit, null) })
            DropdownMenuItem({ Text("Delete playlist…") }, { open = false; deleting = true }, leadingIcon = { Icon(Icons.Filled.Delete, null) })
        }
    }
    if (renaming) {
        var name by remember { mutableStateOf(p.title) }
        var busy by remember { mutableStateOf(false) }
        AlertDialog(onDismissRequest = { renaming = false },
            title = { Text("Rename playlist") },
            text = { OutlinedTextField(name, { name = it }, label = { Text("Name") }, singleLine = true) },
            confirmButton = {
                TextButton({
                    busy = true
                    marquee.scope.launch {
                        withContext(Dispatchers.IO) { runCatching { marquee.playlists.updatePlaylist(p.id, app.marquee.api.models.UpdatePlaylistRequest(title = name.trim())) } }
                            .onSuccess { renaming = false; onRenamed() }
                            .onFailure { toast(context, "Couldn't rename it: ${serverMessage(it)}") }
                        busy = false
                    }
                }, Modifier.focusRing(), enabled = !busy && name.isNotBlank() && name.trim() != p.title) { Text("Rename") }
            },
            dismissButton = { TextButton({ renaming = false }, Modifier.focusRing()) { Text("Cancel") } })
    }
    if (deleting) AlertDialog(onDismissRequest = { deleting = false },
        title = { Text("Delete “${p.title}”?") },
        text = { Text("The playlist goes for good. What's in it stays in your library.") },
        confirmButton = {
            TextButton({
                deleting = false
                marquee.scope.launch {
                    withContext(Dispatchers.IO) { runCatching { marquee.playlists.deletePlaylist(p.id) } }
                        .onSuccess { nav.popBackStack() }
                        .onFailure { toast(context, "Couldn't delete it: ${serverMessage(it)}") }
                }
            }, Modifier.focusRing()) { Text("Delete", color = MaterialTheme.colorScheme.error) }
        },
        dismissButton = { TextButton({ deleting = false }, Modifier.focusRing()) { Text("Cancel") } })
}

@Composable
fun SearchScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    // Saved, so coming back from a result (or another tab) keeps the search and runs it again.
    var q by rememberSaveable { mutableStateOf("") }
    var results by remember { mutableStateOf<SearchResults?>(null) }
    // The query the results (or the failure) are for, so "No results" never shows for one still loading.
    var answered by remember { mutableStateOf<String?>(null) }
    var failed by remember { mutableStateOf(false) }
    LaunchedEffect(q) {
        if (q.trim().length < 2) { results = null; answered = null; return@LaunchedEffect }
        delay(250)
        val r = withContext(Dispatchers.IO) { runCatching { marquee.search.search(q.trim(), 20) } }
        results = r.getOrNull()
        failed = r.isFailure
        answered = q.trim()
    }
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    // Muse (USER-15): describe what you want to watch instead of searching by name.
    var muse by rememberSaveable { mutableStateOf(false) }
    Column {
        Row(Modifier.padding(start = sidePadding, end = sidePadding, top = sidePadding), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(!muse, { muse = false }, { Text("Search") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
            FilterChip(muse, { muse = true }, { Text("Muse") }, Modifier.focusRing(RoundedCornerShape(8.dp)),
                leadingIcon = { Icon(Icons.Filled.AutoAwesome, null, Modifier.size(18.dp)) })
        }
        if (muse) { MuseVideoPanel(nav, null); return@Column }
        OutlinedTextField(q, { q = it }, label = { Text("Search movies, shows, music…") }, singleLine = true, modifier = Modifier.fillMaxWidth().padding(sidePadding),
            leadingIcon = { Icon(Icons.Filled.Search, null) },
            trailingIcon = if (q.isEmpty()) null else { { IconButton({ q = "" }, Modifier.focusRing()) { Icon(Icons.Filled.Close, "Clear search") } } },
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
            keyboardActions = androidx.compose.foundation.text.KeyboardActions(onSearch = { keyboard?.hide() }))
        val term = q.trim()
        if (term.length >= 2 && answered == term && results?.groups?.all { it.items.isEmpty() } != false) {
            Text(if (failed) "Couldn't search just now. Check the connection and try again." else "No results for “$term”.",
                Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        LazyColumn(contentPadding = PaddingValues(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
            items(results?.groups ?: emptyList()) { g ->
                Shelf(g.type.value.replaceFirstChar { it.uppercase() } + "s", g.items, sidePadding) { _, it ->
                    PosterCard(it, marquee.imageUrl(it.images?.poster ?: it.images?.thumb, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) },
                        actions = tileActions(it, nav = nav))
                }
            }
        }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun SettingsScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    val scope = rememberCoroutineScope()
    // The rating switch below shows only while the server asks for ratings (USER-19).
    LaunchedEffect(Unit) { marquee.refreshRatingPrompt() }
    var switching by remember { mutableStateOf(false) }
    if (switching) ProfileSwitcher(nav, onDismiss = { switching = false })
    var signingOut by remember { mutableStateOf(false) }
    if (signingOut) SignOutDialog(onDismiss = { signingOut = false })
    var opened by rememberCached("settings:opened") { mutableStateOf<String?>(null) }
    @Composable fun page(route: String) = Modifier.focusRing().initialFocus(marquee.isTv && opened == route)
    fun open(route: String) { opened = route; nav.navigate(route) }
    Column(Modifier.verticalScroll(androidx.compose.foundation.rememberScrollState()).padding(sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text("Settings", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            Avatar(me?.displayName ?: "?", marquee.absolute(me?.avatarUrl), 56)
            Column {
                Text(me?.displayName ?: "", fontWeight = FontWeight.SemiBold)
                Text("@${me?.username ?: ""}", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        Text("Server: ${marquee.server?.name ?: ""} · ${if (marquee.isRemote) "Tailscale (away)" else "home network"}", color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text("Version ${marquee.info?.version ?: ""}", color = MaterialTheme.colorScheme.onSurfaceVariant)
        PendingSyncRow()
        // TV: the page starts on its first setting (not left on the side menu), and coming back from
        // one of its pages focuses that page's button again.
        TrailersPreference(focusFirst = marquee.isTv && opened == null)
        NightModePreference()
        RatingPromptPreference()
        ShowAudioQualityPreference()
        EqualiserSetting()
        // Smart downloads: phones and tablets (a TV doesn't download).
        if (!marquee.isTv) SmartDownloadsRow(nav)
        // Approve a TV's Quick Connect code (the TV's sign-in sends people here); TVs show the code, not this.
        if (!marquee.isTv) LinkTvSetting()
        // Buttons wrap onto more lines rather than running off a phone's edge.
        androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { open("stats") }, page("stats")) { Text("Your Stats") }
            OutlinedButton(onClick = { open("subtitles") }, page("subtitles")) { Text("Subtitle appearance") }
            if (me?.isAdmin == true && !marquee.isTv) OutlinedButton(onClick = { open("health") }, page("health")) { Text("Library Health") }
            if (me?.isAdmin == true) OutlinedButton(onClick = { open("approvals") }, page("approvals")) { Text("Requests") }
            if (me?.isAdmin == true) OutlinedButton(onClick = { open("users") }, page("users")) { Text("Users & sharing") }
            if (me?.isAdmin == true) OutlinedButton(onClick = { open("serversettings") }, page("serversettings")) { Text("Server settings") }
            // Friends have no household profiles to switch to (USER-13).
            if (me?.restrictions?.friend != true) OutlinedButton(onClick = { switching = true }, Modifier.focusRing()) { Text("Switch profile") }
            OutlinedButton(onClick = { signingOut = true }, Modifier.focusRing()) { Text("Sign out") }
            OutlinedButton(onClick = { marquee.forgetServer() }, Modifier.focusRing()) { Text("Use a different server") }
        }
        Box(Modifier.width(1.dp))
    }
}

/** Link a TV: enter the six characters a TV shows under Quick Connect, and it signs in as you. */
@Composable
private fun LinkTvSetting() {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var code by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }
    val clean = code.filter { it.isLetterOrDigit() }.uppercase()
    fun link() {
        if (clean.length != 6 || busy) return
        busy = true
        result = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.auth.authorizeQuickConnect(app.marquee.api.models.AuthorizeQuickConnectRequest(clean)) } }
                .onSuccess { result = "${it.deviceName} is signed in."; code = "" }
                .onFailure { result = if ((it as? app.marquee.api.infrastructure.ClientException)?.statusCode == 404) "That code didn't match a TV. Check it, or start Quick Connect again on the TV."
                    else serverMessage(it, "Couldn't link the TV") }
            busy = false
        }
    }
    Column(Modifier.semantics { contentDescription = "Link a TV" }, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("Link a TV", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.Bold)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            OutlinedTextField(code, { code = it.take(8) }, Modifier.weight(1f), label = { Text("Code shown on the TV") }, singleLine = true,
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(capitalization = androidx.compose.ui.text.input.KeyboardCapitalization.Characters,
                    autoCorrectEnabled = false, imeAction = androidx.compose.ui.text.input.ImeAction.Done),
                keyboardActions = androidx.compose.foundation.text.KeyboardActions(onDone = { link() }))
            Button(::link, Modifier.focusRing(), enabled = clean.length == 6 && !busy) { Text(if (busy) "Linking…" else "Link") }
        }
        Text(result ?: "On the TV, choose Use Quick Connect and enter its code here.", style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Changes made offline that haven't reached the server yet (USER-18), with Sync now. Only while there are some. */
@Composable
private fun PendingSyncRow() {
    val marquee = LocalMarquee.current
    val sync = marquee.sync
    val changes by sync.queue.changes.collectAsState()
    val last by sync.lastSync.collectAsState()
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    val n = sync.count(changes)
    if (n == 0) return
    Row(Modifier.semantics(mergeDescendants = true) {}, verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.CloudUpload, null, Modifier.padding(end = 12.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text("Changes waiting to sync: $n")
            Text(last?.let { "Last synced " + android.text.format.DateUtils.getRelativeTimeSpanString(it) } ?: "Sent when the server can be reached",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        OutlinedButton({
            busy = true
            scope.launch { runCatching { sync.syncNow() }; busy = false }
        }, Modifier.focusRing(), enabled = !busy) { Text(if (busy) "Syncing…" else "Sync now") }
    }
}

/** Keep a playlist on the device, in step with its changes (MUSIC-19). */
@Composable
private fun PlaylistDownloadButton(id: Long) {
    val downloads = LocalDownloads.current
    val synced by downloads.playlists.collectAsState()
    val entries by downloads.entries.collectAsState()
    val ids = synced[id]
    if (ids == null) {
        OutlinedButton({ downloads.scope.launch { runCatching { downloads.syncPlaylist(id) } } }, Modifier.focusRing()) {
            Icon(Icons.Filled.Download, null); Text("Download", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
        }
    } else {
        val done = ids.count { entries[it]?.state == app.marquee.core.Downloads.State.Done }
        OutlinedButton({ downloads.unsyncPlaylist(id) }, Modifier.focusRing().semantics { stateDescription = if (done == ids.size) "Downloaded" else "$done of ${ids.size}" }) {
            Text(if (done == ids.size) "Downloaded · Remove" else "$done/${ids.size} · Remove", maxLines = 1, softWrap = false)
        }
    }
}

/** Music: the equaliser (moved here from Now Playing), on this device only. */
@Composable
private fun EqualiserSetting() {
    val music = LocalMusic.current
    val eq by music.eq.collectAsState()
    var open by remember { mutableStateOf(false) }
    Row(Modifier.focusCard({ open = true }).semantics(mergeDescendants = true) { contentDescription = "Equaliser" }, verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.Equalizer, null, Modifier.padding(end = 12.dp), tint = if (eq.on) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text("Equaliser")
            Text(if (eq.on) "Music: ${eq.preset}, on this device" else "Music: off, on this device", style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Icon(Icons.Filled.ChevronRight, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    if (open) app.marquee.music.EqSheet { open = false }
}

/** Music: Show audio quality (MUSIC-23), on this device only. */
@Composable
private fun ShowAudioQualityPreference() {
    val music = LocalMusic.current
    val on by music.showQuality.collectAsState()
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text("Show audio quality")
            Text("Music: the format in Now Playing and album track lists, on this device", style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        androidx.compose.material3.Switch(on, music::setShowQuality, Modifier.focusRing().semantics { contentDescription = "Show audio quality" })
    }
}
