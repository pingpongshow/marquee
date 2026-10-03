package app.marquee.music

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Album
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Category
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.apis.ItemsApi.SortListLibraryItems
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.Library
import app.marquee.api.models.Playlist
import app.marquee.api.models.PlaylistKind
import app.marquee.api.models.RadioRequest
import app.marquee.ui.Artwork
import app.marquee.ui.LocalMarquee
import app.marquee.ui.LocalMusic
import app.marquee.ui.PosterCard
import app.marquee.ui.Shape
import app.marquee.ui.focusCard
import app.marquee.ui.focusRing
import app.marquee.ui.initialFocus
import app.marquee.ui.openItem
import app.marquee.ui.sidePadding
import app.marquee.ui.subtitleFor
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The music library's own pages, opened from the home's browse row. */
enum class MusicBrowse(val route: String, val title: String) {
    Artists("artists", "Artists"), Albums("albums", "Albums"), Songs("songs", "Songs"), Genres("genres", "Genres");

    companion object { fun of(route: String) = entries.firstOrNull { it.route == route } ?: Artists }
}

/** A sort for a music browse page; the key goes in the route (?sort=…). */
private enum class MusicSort(val key: String, val label: String, val api: SortListLibraryItems) {
    Name("name", "Name", SortListLibraryItems.TITLE),
    Added("added", "Recently added", SortListLibraryItems._ADDED),
    Played("played", "Recently played", SortListLibraryItems._VIEWED),
    Year("year", "Year", SortListLibraryItems._YEAR),
    Random("random", "Random", SortListLibraryItems.RANDOM),
}

private fun sortsFor(kind: MusicBrowse) = when (kind) {
    MusicBrowse.Albums -> listOf(MusicSort.Name, MusicSort.Added, MusicSort.Played, MusicSort.Year)
    else -> listOf(MusicSort.Name, MusicSort.Added, MusicSort.Played, MusicSort.Random)
}

private class MusicShelves(val recentlyPlayed: List<ItemSummary>, val recentlyAdded: List<ItemSummary>, val playlists: List<Playlist>, val topArtists: List<ItemSummary>)

/**
 * The music library's home: a browse row (Artists, Albums, Songs, Playlists, Genres), quick
 * actions, then shelves (Year in Music, mixes, recently played and added, moods and styles,
 * playlists, top artists). The full artist grid lives on the Artists page.
 */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun MusicHome(nav: NavHostController, library: Library) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val lib = library.id
    val discover = rememberMusicDiscoverData(lib)
    val shelves by produceState<MusicShelves?>(null, lib) {
        value = withContext(Dispatchers.IO) {
            coroutineScope {
                fun albums(sort: SortListLibraryItems) = async { runCatching { marquee.items.listLibraryItems(lib, ItemType.ALBUM, sort, limit = 20).items }.getOrDefault(emptyList()) }
                val played = albums(SortListLibraryItems._VIEWED)
                val added = albums(SortListLibraryItems._ADDED)
                val lists = async { runCatching { marquee.playlists.listPlaylists(PlaylistKind.AUDIO) }.getOrDefault(emptyList()) }
                // Top artists: this year's most played, else the ones played most recently.
                val top = async {
                    runCatching { marquee.music.listeningRecap(null).topArtists.map { it.item }.filter { it.libraryId == lib } }.getOrDefault(emptyList())
                        .ifEmpty {
                            runCatching { marquee.items.listLibraryItems(lib, ItemType.ARTIST, SortListLibraryItems._VIEWED, limit = 20).items }.getOrDefault(emptyList())
                                .filter { (it.watchedLeafCount ?: 0) > 0 }
                        }
                }
                MusicShelves(played.await().filter { (it.watchedLeafCount ?: 0) > 0 }, added.await(), lists.await(), top.await().take(20))
            }
        }
    }
    fun start(what: String, work: suspend () -> Unit) {
        scope.launch {
            runCatching { work() }.onFailure { Toast.makeText(context, it.message ?: "Couldn't start $what", Toast.LENGTH_LONG).show() }
        }
    }
    val card = if (marquee.isTv) 130.dp else 120.dp
    val pad = PaddingValues(horizontal = sidePadding, vertical = 6.dp)

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
        item { Text(library.name, Modifier.padding(horizontal = sidePadding), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }

        // Browse: each opens a full, sortable page.
        item {
            val browse = listOf(
                Triple("Artists", Icons.Filled.Person) { nav.navigate("musicbrowse/$lib/artists") },
                Triple("Albums", Icons.Filled.Album) { nav.navigate("musicbrowse/$lib/albums") },
                Triple("Songs", Icons.Filled.MusicNote) { nav.navigate("musicbrowse/$lib/songs") },
                Triple("Playlists", Icons.AutoMirrored.Filled.QueueMusic) { nav.navigate("playlists") },
                Triple<String, ImageVector, () -> Unit>("Genres", Icons.Filled.Category) { nav.navigate("musicbrowse/$lib/genres") },
            )
            // Wraps, so every page is in view without scrolling sideways.
            FlowRow(Modifier.fillMaxWidth().padding(horizontal = sidePadding).semantics { contentDescription = "Browse music" },
                horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                browse.forEachIndexed { i, (label, icon, go) -> Pill(label, icon, go, focus = marquee.isTv && i == 0) }
            }
        }

        // Quick actions.
        item {
            val enabled = discover?.enabled == true
            val actions = buildList<Triple<String, ImageVector, () -> Unit>> {
                if (enabled) add(Triple("Library Radio", Icons.Filled.Radio) { start("the radio") { music.startRadio(RadioRequest(RadioRequest.Seed.LIBRARY, libraryId = lib)) } })
                add(Triple("Shuffle All", Icons.Filled.Shuffle) {
                    start("shuffle") {
                        val tracks = withContext(Dispatchers.IO) { marquee.items.listLibraryItems(lib, ItemType.TRACK, SortListLibraryItems.RANDOM, limit = 200).items }
                        if (tracks.isEmpty()) throw IllegalStateException("No songs to shuffle")
                        music.play(tracks, 0, source = "Shuffle All")
                    }
                })
                if (enabled) add(Triple("Muse", Icons.Filled.AutoAwesome) { nav.navigate("musicmuse/$lib") })
                if (!marquee.isTv) add(Triple("Car mode", Icons.Filled.DirectionsCar) { nav.navigate("carmode?lib=$lib") })
            }
            FlowRow(Modifier.fillMaxWidth().padding(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                actions.forEach { (label, icon, go) -> Pill(label, icon, go) }
            }
        }

        discover?.status?.let { st -> if (st.enabled && st.analyzed < st.total) item { Box(Modifier.padding(horizontal = sidePadding)) { AnalysisNote(st) } } }

        // Your Year in Music (MUSIC-22), when there's a year to show.
        item { YearInMusicCard(nav, Modifier.padding(horizontal = sidePadding)) }

        discover?.mixes?.takeIf { it.isNotEmpty() }?.let { mixes ->
            item { Section("Mixes for you", null) { MixesRow(mixes, pad) } }
        }

        val s = shelves
        if (s == null) item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        else {
            if (s.recentlyPlayed.isNotEmpty()) item {
                Section("Recently played", { nav.navigate("musicbrowse/$lib/albums?sort=played") }) {
                    CardRow(s.recentlyPlayed, pad) { PosterCard(it, marquee.imageUrl(it.images?.poster, 300), card, { openItem(nav, it) }) }
                }
            }
            if (s.recentlyAdded.isNotEmpty()) item {
                Section("Recently added", { nav.navigate("musicbrowse/$lib/albums?sort=added") }) {
                    CardRow(s.recentlyAdded, pad) { PosterCard(it, marquee.imageUrl(it.images?.poster, 300), card, { openItem(nav, it) }) }
                }
            }
        }

        // Moods and styles (MUSIC-18).
        if (discover?.enabled == true) item {
            MoodStyleTiles(discover.styles, pad) { kind, name -> nav.navigate("browse/$lib/$kind/${android.net.Uri.encode(name)}") }
        }

        if (s != null) {
            if (s.playlists.isNotEmpty()) item {
                Section("Your playlists", { nav.navigate("playlists") }) {
                    CardRow(s.playlists, pad) { PlaylistCard(it, card) { nav.navigate("playlist/${it.id}") } }
                }
            }
            if (s.topArtists.isNotEmpty()) item {
                Section("Top artists", { nav.navigate("musicbrowse/$lib/artists") }) {
                    CardRow(s.topArtists, pad) { PosterCard(it, marquee.imageUrl(it.images?.poster, 300), card, { openItem(nav, it) }) }
                }
            }
        }
    }
}

@Composable
private fun Pill(label: String, icon: ImageVector, onClick: () -> Unit, focus: Boolean = false) {
    AssistChip(
        onClick = onClick,
        label = { Text(label, maxLines = 1, softWrap = false) },
        leadingIcon = { Icon(icon, null, Modifier.size(AssistChipDefaults.IconSize)) },
        modifier = Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(focus),
    )
}

/** A titled shelf with "See all" when there's a full page for it. */
@Composable
private fun Section(title: String, onSeeAll: (() -> Unit)?, content: @Composable () -> Unit) {
    Column {
        Row(Modifier.fillMaxWidth().padding(start = sidePadding, end = sidePadding - 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(title, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            if (onSeeAll != null) TextButton(onSeeAll, Modifier.focusRing().semantics { contentDescription = "See all: $title" }) { Text("See all", maxLines = 1) }
        }
        content()
    }
}

@Composable
private fun <T> CardRow(items: List<T>, pad: PaddingValues, card: @Composable (T) -> Unit) {
    LazyRow(contentPadding = pad, horizontalArrangement = Arrangement.spacedBy(14.dp)) { items(items) { card(it) } }
}

@Composable
private fun PlaylistCard(p: Playlist, width: Dp, onClick: () -> Unit) {
    val marquee = LocalMarquee.current
    Column(Modifier.width(width)) {
        Artwork(marquee.imageUrl(p.imageIds.firstOrNull(), 300), p.title, Shape.Square, Modifier.fillMaxWidth().focusCard(onClick).semantics { contentDescription = p.title })
        Text(p.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text("${p.itemCount} tracks", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
    }
}

/** Muse and the stations, opened from the music home's Muse action. */
@Composable
fun MusicMuseScreen(nav: NavHostController, libraryId: Long) {
    val d = rememberMusicDiscoverData(libraryId)
    LazyColumn(contentPadding = PaddingValues(sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        item { Text("Muse & Stations", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        item {
            if (d == null) Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            else MusicDiscover(libraryId, d, onOpenPlaylist = { nav.navigate("playlist/$it") })
        }
    }
}

/** A music library's artists, albums, songs or genres in full, sortable. */
@Composable
fun MusicBrowseScreen(nav: NavHostController, libraryId: Long, kindRoute: String, sortKey: String?) {
    val kind = MusicBrowse.of(kindRoute)
    val sorts = sortsFor(kind)
    var sort by rememberSaveable { mutableStateOf(sorts.firstOrNull { it.key == sortKey }?.key ?: MusicSort.Name.key) }
    Column {
        Text(kind.title, Modifier.padding(start = sidePadding, end = sidePadding, top = 12.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        if (kind == MusicBrowse.Genres) { GenreList(nav, libraryId); return@Column }
        LazyRow(Modifier.semantics { contentDescription = "Sort" }, contentPadding = PaddingValues(horizontal = sidePadding, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(sorts) { s ->
                FilterChip(sort == s.key, { sort = s.key }, { Text(s.label, maxLines = 1) }, Modifier.focusRing(RoundedCornerShape(8.dp)))
            }
        }
        val current = sorts.first { it.key == sort }
        androidx.compose.runtime.key(sort) { PagedMusic(nav, libraryId, kind, current) }
    }
}

@Composable
private fun PagedMusic(nav: NavHostController, libraryId: Long, kind: MusicBrowse, sort: MusicSort) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val scope = rememberCoroutineScope()
    val items = remember { mutableStateListOf<ItemSummary>() }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    val type = when (kind) { MusicBrowse.Albums -> ItemType.ALBUM; MusicBrowse.Songs -> ItemType.TRACK; else -> ItemType.ARTIST }
    fun loadMore() {
        if (loading || (total >= 0 && items.size >= total)) return
        loading = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.listLibraryItems(libraryId, type, sort.api, offset = items.size, limit = 120) } }
                .onSuccess { page -> items.addAll(page.items.filter { n -> items.none { it.id == n.id } }); total = page.total }
                .onFailure { total = items.size }
            loading = false
        }
    }
    LaunchedEffect(Unit) { loadMore() }
    if (total == 0) { Text("Nothing here yet.", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant); return }
    if (kind == MusicBrowse.Songs) {
        LazyColumn(contentPadding = PaddingValues(bottom = 16.dp)) {
            itemsIndexed(items, key = { _, it -> it.id }) { i, t ->
                if (i >= items.size - 30) LaunchedEffect(i) { loadMore() }
                ListItem(
                    headlineContent = { Text(t.title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    supportingContent = { Text(subtitleFor(t), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                    leadingContent = { Artwork(marquee.imageUrl(t.images?.poster, 120), t.title, Shape.Square, Modifier.width(44.dp)) },
                    colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                    modifier = Modifier.initialFocus(marquee.isTv && i == 0).focusCard({ music.play(items.toList(), i, source = "Songs") }),
                )
            }
        }
    } else {
        val min = if (marquee.isTv) 130.dp else 110.dp
        LazyVerticalGrid(GridCells.Adaptive(min), contentPadding = PaddingValues(sidePadding), horizontalArrangement = Arrangement.spacedBy(14.dp),
            verticalArrangement = Arrangement.spacedBy(18.dp)) {
            itemsIndexed(items, key = { _, it -> it.id }) { i, it ->
                if (i >= items.size - 30) LaunchedEffect(i) { loadMore() }
                PosterCard(it, marquee.imageUrl(it.images?.poster, 240), min, { openItem(nav, it) }, autoFocus = marquee.isTv && i == 0)
            }
        }
    }
}

@Composable
private fun GenreList(nav: NavHostController, libraryId: Long) {
    val marquee = LocalMarquee.current
    var byCount by rememberSaveable { mutableStateOf(false) }
    val genres by produceState<List<app.marquee.api.models.Facet>?>(null, libraryId) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.items.libraryFilters(libraryId, ItemType.ALBUM).genres }.getOrDefault(emptyList()) }
    }
    val g = genres ?: return Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    Row(Modifier.padding(horizontal = sidePadding, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(!byCount, { byCount = false }, { Text("Name") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
        FilterChip(byCount, { byCount = true }, { Text("Most albums") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
    }
    if (g.isEmpty()) { Text("No genres yet.", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.onSurfaceVariant); return }
    val sorted = if (byCount) g.sortedByDescending { it.count } else g.sortedBy { it.value.lowercase() }
    LazyColumn(contentPadding = PaddingValues(bottom = 16.dp)) {
        itemsIndexed(sorted, key = { _, it -> it.value }) { i, f ->
            ListItem(
                headlineContent = { Text(f.value) },
                supportingContent = { Text("${f.count} album${if (f.count == 1) "" else "s"}") },
                trailingContent = { Icon(Icons.Filled.ChevronRight, null) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.initialFocus(marquee.isTv && i == 0).focusCard({ nav.navigate("browse/$libraryId/style/${android.net.Uri.encode(f.value)}") }),
            )
            HorizontalDivider()
        }
    }
}
