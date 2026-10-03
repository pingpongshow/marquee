package app.marquee.ui

import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.background
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.LibraryMusic
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
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.FilterChip
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
    val hubs by produceState<Result<List<Hub>>?>(null, connection) { value = withContext(Dispatchers.IO) { runCatching { marquee.hubs.homeHubs() } } }
    val cardWidth = if (marquee.isTv) 130.dp else 120.dp
    when (val h = hubs) {
        null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        else -> LazyColumn(contentPadding = PaddingValues(vertical = 20.dp), verticalArrangement = Arrangement.spacedBy(26.dp)) {
            item {
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
                        if (!marquee.isTv) OutlinedButton({ nav.navigate("downloads") }) { Text("Open Downloads") }
                    }
                }
            }
            if (!marquee.isOffline) h.exceptionOrNull()?.let { e -> item { Text("Couldn't load: ${e.message}", Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error) } }
            itemsIndexed(h.getOrDefault(emptyList()), key = { _, it -> it.id }) { hi, hub ->
                val wide = hub.id == "continue-watching"
                Shelf(hub.title, hub.items, sidePadding, onTitle = HomeRows.route(hub.id)?.let { r -> { nav.navigate(r) } }) { i, it ->
                    val art = if (wide) it.images?.thumb ?: it.images?.backdrop else it.images?.poster
                    PosterCard(it, marquee.imageUrl(art, 300), if (wide) cardWidth * 1.6f else cardWidth, { openItem(nav, it, play = wide) },
                        shape = if (wide) Shape.Wide else shapeFor(it), autoFocus = marquee.isTv && hi == 0 && i == 0)
                }
            }
        }
    }
}

@Composable
fun LibrariesScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val liveOn by produceState(false) { value = withContext(Dispatchers.IO) { runCatching { marquee.livetv.liveTvStatus().enabled }.getOrDefault(false) } }
    val requests by produceState<app.marquee.api.models.RequestsStatus?>(null) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.requests.requestsStatus() }.getOrNull() }
    }
    val libs by produceState<List<Library>?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.libraries.listLibraries() }.getOrDefault(emptyList()) } }
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
                modifier = Modifier.focusCard({ nav.navigate("discover") }),
            )
            HorizontalDivider()
        }
        // Phones with Live TV list Playlists here instead of in the tab bar.
        if (!marquee.isTv && liveOn) item {
            ListItem(
                headlineContent = { Text("Playlists") },
                leadingContent = { Icon(Icons.Filled.LibraryMusic, null) },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ nav.navigate("playlists") }),
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
                modifier = Modifier.focusCard({ nav.navigate("downloads") }),
            )
            HorizontalDivider()
        }
        itemsIndexed(libs ?: emptyList(), key = { _, it -> it.id }) { i, lib ->
            ListItem(
                headlineContent = { Text(lib.name) },
                supportingContent = { Text("${lib.itemCount} items") },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.initialFocus(marquee.isTv && i == 0).focusCard({ nav.navigate("library/${lib.id}") }),
            )
            HorizontalDivider()
        }
    }
}

/** A library's grid, a page at a time as you scroll. */
@Composable
fun LibraryScreen(nav: NavHostController, libraryId: Long) {
    val marquee = LocalMarquee.current
    val items = remember { mutableStateListOf<ItemSummary>() }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var library by remember { mutableStateOf<Library?>(null) }
    // Movie libraries also browse by collection (META-7).
    var collections by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    fun loadMore() {
        if (loading || (total >= 0 && items.size >= total)) return
        loading = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.listLibraryItems(libraryId, if (collections) ItemType.COLLECTION else null, offset = items.size, limit = 120) } }
                .onSuccess { page -> items.addAll(page.items.filter { n -> items.none { it.id == n.id } }); total = page.total }
            loading = false
        }
    }
    LaunchedEffect(libraryId) {
        library = withContext(Dispatchers.IO) { runCatching { marquee.libraries.getLibrary(libraryId) }.getOrNull() }
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
                    if (collections != c) { collections = c; items.clear(); total = -1; loadMore() }
                }, { Text(label) }, Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)))
            }
        }
        LazyVerticalGrid(GridCells.Adaptive(min), contentPadding = PaddingValues(sidePadding), horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            itemsIndexed(items, key = { _, it -> it.id }) { i, it ->
                if (i >= items.size - 30) LaunchedEffect(i) { loadMore() }
                PosterCard(it, marquee.imageUrl(it.images?.poster, 240), min, { openItem(nav, it) }, autoFocus = marquee.isTv && i == 0)
            }
        }
    }
}

@Composable
fun PlaylistsScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val lists by produceState(emptyList<app.marquee.api.models.Playlist>()) { value = withContext(Dispatchers.IO) { runCatching { marquee.playlists.listPlaylists() }.getOrDefault(emptyList()) } }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Playlists", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        if (lists.isEmpty()) item { Text("No playlists yet.", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(lists, key = { it.id }) { p ->
            ListItem(
                headlineContent = { Text(p.title) },
                supportingContent = { Text("${p.itemCount} ${if (p.kind == PlaylistKind.AUDIO) "tracks" else "videos"}") },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ nav.navigate("playlist/${p.id}") }),
            )
        }
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun PlaylistScreen(nav: NavHostController, playlistId: Long) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val entries by produceState(emptyList<app.marquee.api.models.PlaylistEntry>()) { value = withContext(Dispatchers.IO) { runCatching { marquee.playlists.listPlaylistItems(playlistId).items }.getOrDefault(emptyList()) } }
    val title by produceState("") { value = withContext(Dispatchers.IO) { runCatching { marquee.playlists.getPlaylist(playlistId).title }.getOrDefault("") } }
    val tracks = entries.map { it.item }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item {
            Column(Modifier.padding(horizontal = sidePadding)) {
                Text(title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                // Full width, wrapping onto another line rather than squeezing a button's label.
                FlowRow(Modifier.fillMaxWidth().padding(vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (tracks.any { it.type == ItemType.TRACK }) {
                        Button(onClick = { music.play(tracks, 0, source = title) }, modifier = Modifier.focusRing().initialFocus(marquee.isTv)) {
                            Icon(Icons.Filled.PlayArrow, null); Text("Play", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                        OutlinedButton(onClick = { music.play(tracks.shuffled(), 0, source = title) }, modifier = Modifier.focusRing()) {
                            Icon(Icons.Filled.Shuffle, null); Text("Shuffle", Modifier.padding(start = 6.dp), maxLines = 1, softWrap = false)
                        }
                        if (!marquee.isTv) PlaylistDownloadButton(playlistId)
                    }
                    PinToHomeButton("playlist-$playlistId")
                }
            }
        }
        items(entries.size) { i ->
            val it = entries[i].item
            ListItem(
                headlineContent = { Text(it.title) },
                supportingContent = { Text(subtitleFor(it)) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ if (it.type == ItemType.TRACK) music.play(tracks, i, source = title) else openItem(nav, it, play = true) }),
            )
        }
    }
}

@Composable
fun SearchScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    var q by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<SearchResults?>(null) }
    LaunchedEffect(q) {
        if (q.trim().length < 2) { results = null; return@LaunchedEffect }
        delay(250)
        results = withContext(Dispatchers.IO) { runCatching { marquee.search.search(q.trim(), 20) }.getOrNull() }
    }
    // Muse (USER-15): describe what you want to watch instead of searching by name.
    var muse by rememberSaveable { mutableStateOf(false) }
    Column {
        Row(Modifier.padding(start = sidePadding, end = sidePadding, top = sidePadding), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(!muse, { muse = false }, { Text("Search") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
            FilterChip(muse, { muse = true }, { Text("Muse") }, Modifier.focusRing(RoundedCornerShape(8.dp)),
                leadingIcon = { Icon(Icons.Filled.AutoAwesome, null, Modifier.size(18.dp)) })
        }
        if (muse) { MuseVideoPanel(nav, null); return@Column }
        OutlinedTextField(q, { q = it }, label = { Text("Search movies, shows, music…") }, singleLine = true, modifier = Modifier.fillMaxWidth().padding(sidePadding))
        LazyColumn(contentPadding = PaddingValues(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
            items(results?.groups ?: emptyList()) { g ->
                Shelf(g.type.value.replaceFirstChar { it.uppercase() } + "s", g.items, sidePadding) { _, it ->
                    PosterCard(it, marquee.imageUrl(it.images?.poster ?: it.images?.thumb, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) })
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
        TrailersPreference()
        ShowAudioQualityPreference()
        // Buttons wrap onto more lines rather than running off a phone's edge.
        androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { nav.navigate("stats") }) { Text("Your Stats") }
            OutlinedButton(onClick = { nav.navigate("subtitles") }) { Text("Subtitle appearance") }
            // Control Marquee on another screen (USER-14); TVs are players only.
            if (!marquee.isTv) OutlinedButton(onClick = { nav.navigate("remote") }) { Text("Remote") }
            if (me?.isAdmin == true && !marquee.isTv) OutlinedButton(onClick = { nav.navigate("health") }) { Text("Library Health") }
            if (me?.isAdmin == true) OutlinedButton(onClick = { nav.navigate("approvals") }) { Text("Requests") }
            if (me?.isAdmin == true) OutlinedButton(onClick = { nav.navigate("users") }) { Text("Users & sharing") }
            if (me?.isAdmin == true) OutlinedButton(onClick = { nav.navigate("serversettings") }) { Text("Server settings") }
            OutlinedButton(onClick = { scope.launch { marquee.signOut() } }) { Text("Switch profile") }
            OutlinedButton(onClick = { marquee.forgetServer() }) { Text("Use a different server") }
        }
        Box(Modifier.width(1.dp))
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
