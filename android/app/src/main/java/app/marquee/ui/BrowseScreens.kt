package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
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
    val hubs by produceState<Result<List<Hub>>?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.hubs.homeHubs() } } }
    val cardWidth = if (marquee.isTv) 130.dp else 120.dp
    when (val h = hubs) {
        null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        else -> LazyColumn(contentPadding = PaddingValues(vertical = 20.dp), verticalArrangement = Arrangement.spacedBy(26.dp)) {
            item { Text("Home", Modifier.padding(horizontal = sidePadding), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
            marquee.lastError?.let { e -> item { Text(e, Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error) } }
            h.exceptionOrNull()?.let { e -> item { Text("Couldn't load: ${e.message}", Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error) } }
            itemsIndexed(h.getOrDefault(emptyList()), key = { _, it -> it.id }) { hi, hub ->
                val wide = hub.id == "continue-watching"
                Shelf(hub.title, hub.items, sidePadding) { i, it ->
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
    val libs by produceState<List<Library>?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.libraries.listLibraries() }.getOrDefault(emptyList()) } }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Libraries", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
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
    var name by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    fun loadMore() {
        if (loading || (total >= 0 && items.size >= total)) return
        loading = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.listLibraryItems(libraryId, offset = items.size, limit = 120) } }
                .onSuccess { page -> items.addAll(page.items.filter { n -> items.none { it.id == n.id } }); total = page.total }
            loading = false
        }
    }
    LaunchedEffect(libraryId) {
        name = withContext(Dispatchers.IO) { runCatching { marquee.libraries.getLibrary(libraryId).name }.getOrDefault("") }
        loadMore()
    }
    val min = if (marquee.isTv) 130.dp else 110.dp
    Column {
        Text(name, Modifier.padding(horizontal = sidePadding, vertical = 12.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
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
                if (tracks.any { it.type == ItemType.TRACK }) Row(Modifier.padding(vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Button(onClick = { music.play(tracks, 0, source = title) }) { Text("Play") }
                    OutlinedButton(onClick = { music.play(tracks.shuffled(), 0, source = title) }) { Text("Shuffle") }
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
    Column {
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

@Composable
fun SettingsScreen() {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    val scope = rememberCoroutineScope()
    Column(Modifier.padding(sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
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
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedButton(onClick = { scope.launch { marquee.signOut() } }) { Text("Switch profile") }
            OutlinedButton(onClick = { marquee.forgetServer() }) { Text("Use a different server") }
        }
        Box(Modifier.width(1.dp))
    }
}
