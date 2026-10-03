package app.marquee.music

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Waves
import androidx.compose.foundation.background
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.foundation.lazy.rememberLazyListState
import app.marquee.ui.Gold
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.DirectionsCar
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.Shuffle
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
    Artists("artists", "Artists"), Albums("albums", "Albums"), Songs("songs", "Songs"), Genres("genres", "Genres"),
    Moods("moods", "Moods & Styles"), Decades("decades", "Decades");

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
 * The music library's home, organised like Plexamp: compact quick actions, shelves (recently
 * played, mixes, Year in Music, recently added, playlists, top artists) and then the Library
 * list (Artists, Albums, Songs, Playlists, Genres, Moods & Styles, Decades, Muse & Stations),
 * each opening its own page. The full artist grid lives only behind Artists.
 */
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
                MusicShelves(played.await().filter { (it.watchedLeafCount ?: 0) > 0 }, added.await(), lists.await(), top.await().take(MAX_TOP_ARTISTS))
            }
        }
    }
    // The Library list's counts, loaded after the shelves.
    val counts by produceState(emptyMap<ItemType, Int>(), lib) {
        value = withContext(Dispatchers.IO) {
            listOf(ItemType.ARTIST, ItemType.ALBUM, ItemType.TRACK).mapNotNull { t ->
                runCatching { t to marquee.items.listLibraryItems(lib, t, limit = 1).total }.getOrNull()
            }.toMap()
        }
    }
    fun start(what: String, work: suspend () -> Unit) {
        scope.launch {
            runCatching { work() }.onFailure { Toast.makeText(context, it.message ?: "Couldn't start $what", Toast.LENGTH_LONG).show() }
        }
    }
    val card = if (marquee.isTv) 130.dp else 120.dp
    val pad = PaddingValues(horizontal = sidePadding, vertical = 6.dp)
    val state = rememberLazyListState()
    val enabled = discover?.enabled == true

    LazyColumn(Modifier.fillMaxSize().semantics { contentDescription = "Music home" }, state = state,
        contentPadding = PaddingValues(vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
        item {
            Row(Modifier.fillMaxWidth().padding(start = sidePadding, end = sidePadding - 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(library.name, Modifier.weight(1f), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                // Phones: a shortcut down to the Library list.
                if (!marquee.isTv) TextButton({ scope.launch { state.animateScrollToItem((state.layoutInfo.totalItemsCount - 1).coerceAtLeast(0)) } }, Modifier.focusRing()) {
                    Text("Library", maxLines = 1)
                    Icon(Icons.Filled.KeyboardArrowDown, null)
                }
            }
        }

        // Quick actions: round icons with small labels.
        item {
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
            Row(Modifier.fillMaxWidth().padding(horizontal = sidePadding).semantics { contentDescription = "Quick actions" },
                horizontalArrangement = Arrangement.spacedBy(if (marquee.isTv) 28.dp else 8.dp, if (marquee.isTv) Alignment.Start else Alignment.CenterHorizontally)) {
                actions.forEachIndexed { i, (label, icon, go) -> QuickAction(label, icon, go, Modifier.weight(1f, fill = !marquee.isTv), focus = marquee.isTv && i == 0) }
            }
        }

        discover?.status?.let { st -> if (st.enabled && st.analyzed < st.total) item { Box(Modifier.padding(horizontal = sidePadding)) { AnalysisNote(st) } } }

        val s = shelves
        if (s == null) item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        else if (s.recentlyPlayed.isNotEmpty()) item {
            Section("Recently Played", { nav.navigate("musicbrowse/$lib/albums?sort=played") }) {
                CardRow(s.recentlyPlayed, pad) { PosterCard(it, marquee.imageUrl(it.images?.poster, 300), card, { openItem(nav, it) }) }
            }
        }
        discover?.mixes?.takeIf { it.isNotEmpty() }?.let { mixes ->
            item { Section("Mixes for You", null) { MixesRow(mixes, pad) } }
        }
        // Your Year in Music (MUSIC-22), when there's a year to show.
        item { YearInMusicCard(nav, Modifier.padding(horizontal = sidePadding)) }
        if (s != null) {
            if (s.recentlyAdded.isNotEmpty()) item {
                Section("Recently Added", { nav.navigate("musicbrowse/$lib/albums?sort=added") }) {
                    CardRow(s.recentlyAdded, pad) { PosterCard(it, marquee.imageUrl(it.images?.poster, 300), card, { openItem(nav, it) }) }
                }
            }
            if (s.playlists.isNotEmpty()) item {
                Section("Your Playlists", { nav.navigate("playlists") }) {
                    CardRow(s.playlists, pad) { PlaylistCard(it, card) { nav.navigate("playlist/${it.id}") } }
                }
            }
            if (s.topArtists.isNotEmpty()) item {
                Section("Top Artists", { nav.navigate("musicbrowse/$lib/artists") }) {
                    CardRow(s.topArtists, pad, Modifier.semantics { contentDescription = "Top Artists shelf" }) {
                        PosterCard(it, marquee.imageUrl(it.images?.poster, 300), card, { openItem(nav, it) })
                    }
                }
            }
        }

        // Library: every page of the library, Plexamp style.
        item {
            Column(Modifier.fillMaxWidth().semantics { contentDescription = "Library" }) {
                Text("Library", Modifier.padding(horizontal = sidePadding).padding(bottom = 4.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                LibraryRow(Icons.Filled.Person, "Artists", counts[ItemType.ARTIST]) { nav.navigate("musicbrowse/$lib/artists") }
                LibraryRow(Icons.Filled.Album, "Albums", counts[ItemType.ALBUM]) { nav.navigate("musicbrowse/$lib/albums") }
                LibraryRow(Icons.Filled.MusicNote, "Songs", counts[ItemType.TRACK]) { nav.navigate("musicbrowse/$lib/songs") }
                LibraryRow(Icons.AutoMirrored.Filled.QueueMusic, "Playlists", s?.playlists?.size) { nav.navigate("playlists") }
                LibraryRow(Icons.Filled.Category, "Genres", null) { nav.navigate("musicbrowse/$lib/genres") }
                if (enabled) LibraryRow(Icons.Filled.Waves, "Moods & Styles", null) { nav.navigate("musicbrowse/$lib/moods") }
                if (enabled && discover?.decades?.isNotEmpty() == true) LibraryRow(Icons.Filled.CalendarMonth, "Decades", discover.decades.size) { nav.navigate("musicbrowse/$lib/decades") }
                if (enabled) LibraryRow(Icons.Filled.AutoAwesome, "Muse & Stations", null) { nav.navigate("musicmuse/$lib") }
            }
        }
    }
}

/** At most this many artists on the home; the rest are behind Artists. */
const val MAX_TOP_ARTISTS = 20

/** A round icon with a small label under it; the whole thing is one button. */
@Composable
private fun QuickAction(label: String, icon: ImageVector, onClick: () -> Unit, modifier: Modifier = Modifier, focus: Boolean = false) {
    Column(
        modifier.initialFocus(focus).focusCard(onClick, RoundedCornerShape(12.dp)).semantics(mergeDescendants = true) { role = Role.Button }.padding(vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(Modifier.size(48.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceVariant), contentAlignment = Alignment.Center) {
            Icon(icon, null, tint = Gold)
        }
        Text(label, Modifier.padding(top = 6.dp), style = MaterialTheme.typography.labelMedium, maxLines = 1, softWrap = false)
    }
}

/** A Library row: icon, title, count (muted) and a chevron. */
@Composable
private fun LibraryRow(icon: ImageVector, title: String, count: Int?, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = sidePadding - 8.dp).focusCard(onClick, RoundedCornerShape(10.dp))
            .semantics(mergeDescendants = true) { role = Role.Button }.padding(horizontal = 8.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, null, tint = Gold)
        Text(title, Modifier.weight(1f).padding(start = 16.dp), style = MaterialTheme.typography.bodyLarge, maxLines = 1)
        count?.let { Text("$it", Modifier.padding(horizontal = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        Icon(Icons.Filled.ChevronRight, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
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
private fun <T> CardRow(items: List<T>, pad: PaddingValues, modifier: Modifier = Modifier, card: @Composable (T) -> Unit) {
    LazyRow(modifier, contentPadding = pad, horizontalArrangement = Arrangement.spacedBy(14.dp)) { items(items) { card(it) } }
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
        when (kind) {
            MusicBrowse.Genres -> { GenreList(nav, libraryId); return@Column }
            MusicBrowse.Moods -> { MoodsPage(nav, libraryId); return@Column }
            MusicBrowse.Decades -> { DecadesPage(libraryId); return@Column }
            else -> {}
        }
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

/** Moods and styles (MUSIC-18), each opening its page. */
@Composable
private fun MoodsPage(nav: NavHostController, libraryId: Long) {
    val d = rememberMusicDiscoverData(libraryId) ?: return Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    LazyColumn(contentPadding = PaddingValues(vertical = 12.dp)) {
        item { MoodStyleTiles(d.styles, PaddingValues(horizontal = sidePadding, vertical = 6.dp)) { kind, name -> nav.navigate("browse/$libraryId/$kind/${android.net.Uri.encode(name)}") } }
    }
}

/** A radio for each decade in the library. */
@Composable
private fun DecadesPage(libraryId: Long) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val d = rememberMusicDiscoverData(libraryId) ?: return Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    LazyColumn(contentPadding = PaddingValues(vertical = 8.dp)) {
        itemsIndexed(d.decades) { i, dec ->
            ListItem(
                headlineContent = { Text("${dec}s") },
                supportingContent = { Text("Radio") },
                leadingContent = { Icon(Icons.Filled.Radio, null, tint = Gold) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.initialFocus(marquee.isTv && i == 0).focusCard({
                    scope.launch {
                        runCatching { music.startRadio(RadioRequest(RadioRequest.Seed.DECADE, value = dec, libraryId = libraryId)) }
                            .onFailure { Toast.makeText(context, it.message ?: "Couldn't start the radio", Toast.LENGTH_LONG).show() }
                    }
                }),
            )
            HorizontalDivider()
        }
    }
}
