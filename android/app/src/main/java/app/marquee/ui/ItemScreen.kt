package app.marquee.ui

import androidx.compose.runtime.remember
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.IconButton
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Explore

import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.BookmarkAdded
import androidx.compose.material.icons.filled.BookmarkAdd
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import app.marquee.api.models.RadioRequest
import androidx.compose.ui.platform.LocalContext
import androidx.compose.material.icons.filled.Radio
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.Credit
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private data class Page(val detail: ItemDetail, val children: List<ItemSummary>, val related: List<ItemSummary>, val similar: List<ItemSummary>,
    val popular: List<ItemSummary> = emptyList())

/** Detail page for any item: header, play buttons, contents, cast and related titles. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun ItemScreen(nav: NavHostController, itemId: Long) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val me by marquee.me.collectAsState()
    var reload by remember { mutableIntStateOf(0) }
    val page by produceState<Result<Page>?>(null, itemId, reload) {
        value = withContext(Dispatchers.IO) {
            runCatching {
                val d = marquee.items.getItem(itemId)
                val children = if (d.childCount > 0) marquee.items.listItemChildren(itemId, limit = 500).items else emptyList()
                val related = if (d.type in listOf(ItemType.MOVIE, ItemType.SHOW, ItemType.ALBUM, ItemType.ARTIST)) runCatching { marquee.items.relatedItems(itemId) }.getOrDefault(emptyList()) else emptyList()
                // Music: what sounds like it, from Soundprint analysis (MUSIC-2).
                val similar = if (d.type in listOf(ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK)) runCatching { marquee.music.soundsLike(itemId, 20) }.getOrDefault(emptyList()) else emptyList()
                // An artist's most-listened tracks in the library (MUSIC-15).
                val popular = if (d.type == ItemType.ARTIST) runCatching { marquee.items.listPopularTracks(itemId).items }.getOrDefault(emptyList()) else emptyList()
                Page(d, children, related, similar, popular)
            }
        }
    }
    val p = page
    if (p == null) { Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }; return }
    val pg = p.getOrElse { Text("Couldn't load: ${it.message}", Modifier.padding(sidePadding), color = MaterialTheme.colorScheme.error); return }
    val d = pg.detail
    fun leaves(shuffle: Boolean = false, unwatched: Boolean = false, then: (List<ItemSummary>) -> Unit) = scope.launch {
        val l = withContext(Dispatchers.IO) { runCatching { marquee.items.itemLeaves(d.id, shuffle, unwatched) }.getOrDefault(emptyList()) }
        then(l)
    }
    val square = d.type in listOf(ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK)
    /** A station seeded from this album, artist or track (MUSIC-3). */
    @Composable fun RadioButton() = OutlinedButton(modifier = Modifier.focusRing(), onClick = {
        scope.launch {
            runCatching { music.startRadio(RadioRequest(RadioRequest.Seed.ITEM, itemId = d.id)) }
                .onFailure { Toast.makeText(context, it.message ?: "Couldn't start a radio", Toast.LENGTH_LONG).show() }
        }
    }) { Icon(Icons.Filled.Radio, null); Text("Radio") }
    /** Watchlist (USER-8) and watched state, for movies and shows. */
    @Composable fun StateButtons() {
        var listed by remember(d.id) { mutableStateOf(d.watchlisted == true) }
        var watched by remember(d.id) { mutableStateOf((d.viewCount ?: 0) > 0 || (d.leafCount > 0 && d.watchedLeafCount == d.leafCount)) }
        if (d.type == ItemType.MOVIE || d.type == ItemType.SHOW) OutlinedButton(modifier = Modifier.focusRing(), onClick = {
            val on = !listed
            listed = on
            scope.launch { withContext(Dispatchers.IO) { runCatching { if (on) marquee.items.addToWatchlist(d.id) else marquee.items.removeFromWatchlist(d.id) } } }
        }) { Icon(if (listed) Icons.Filled.BookmarkAdded else Icons.Filled.BookmarkAdd, null); Text(if (listed) "On Watchlist" else "Watchlist") }
        if (d.type in listOf(ItemType.MOVIE, ItemType.SHOW, ItemType.SEASON, ItemType.EPISODE, ItemType.VIDEO)) OutlinedButton(modifier = Modifier.focusRing(), onClick = {
            val on = !watched
            watched = on
            scope.launch { withContext(Dispatchers.IO) { runCatching { if (on) marquee.items.markWatched(d.id) else marquee.items.markUnwatched(d.id) } } }
        }) { Icon(if (watched) Icons.Filled.CheckCircle else Icons.Filled.RadioButtonUnchecked, null); Text(if (watched) "Watched" else "Mark watched") }
        if (d.type in listOf(ItemType.MOVIE, ItemType.SHOW, ItemType.SEASON, ItemType.EPISODE, ItemType.VIDEO, ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK))
            AddToPlaylistButton(d.id, music = d.type in listOf(ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK))
    }
    @Composable fun Download() {
        if (!marquee.isTv && d.type in listOf(ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO, ItemType.TRACK, ItemType.ALBUM, ItemType.ARTIST, ItemType.SEASON, ItemType.SHOW))
            DownloadButton(d.summary())
    }
    /** The "…" menu: Fix match for admins (movies and shows), Sound Journey for tracks. */
    @Composable fun MoreMenu() {
        val admin = me?.isAdmin == true && matchable(d.type)
        val track = d.type == ItemType.TRACK
        if (!admin && !track) return
        var open by remember { mutableStateOf(false) }
        var match by remember { mutableStateOf(false) }
        var journey by remember { mutableStateOf(false) }
        Box {
            IconButton({ open = true }, Modifier.focusRing()) { Icon(Icons.Filled.MoreVert, "More actions") }
            DropdownMenu(open, { open = false }) {
                if (admin) DropdownMenuItem({ Text("Fix match…") }, { match = true; open = false }, leadingIcon = { Icon(Icons.Filled.Search, null) })
                if (track) DropdownMenuItem({ Text("Sound Journey…") }, { journey = true; open = false }, leadingIcon = { Icon(Icons.Filled.Explore, null) })
            }
        }
        if (match) FixMatchDialog(d.id) { updated ->
            match = false
            if (updated != null) { Toast.makeText(context, "Matched “${updated.title}”", Toast.LENGTH_SHORT).show(); reload++ }
        }
        if (journey) app.marquee.music.SoundJourneyDialog(d.id, d.title) { journey = false }
    }
    @Composable fun Actions() {
            when (d.type) {
                ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO -> {
                    val resume = (d.viewOffsetMs ?: 0) > 0
                    Button(onClick = { nav.navigate("player/${d.id}") }, modifier = Modifier.focusRing().initialFocus(marquee.isTv)) {
                        Icon(Icons.Filled.PlayArrow, null)
                        Text(if (resume) "Resume" else "Play")
                    }
                    if (resume) OutlinedButton(modifier = Modifier.focusRing(), onClick = { nav.navigate("player/${d.id}?start=0") }) {
                        Icon(Icons.Filled.Replay, null)
                        Text("From start")
                    }
                }
                ItemType.SHOW, ItemType.SEASON -> Button(onClick = { leaves(unwatched = true) { l -> l.firstOrNull()?.let { nav.navigate("player/${it.id}") } } }, modifier = Modifier.focusRing().initialFocus(marquee.isTv)) {
                    Icon(Icons.Filled.PlayArrow, null)
                    Text(if ((d.watchedLeafCount ?: 0) > 0) "Continue" else "Play")
                }
                ItemType.ALBUM, ItemType.ARTIST -> {
                    Button(onClick = { leaves { music.play(it, 0, source = d.title) } }, modifier = Modifier.focusRing().initialFocus(marquee.isTv)) { Icon(Icons.Filled.PlayArrow, null); Text("Play") }
                    OutlinedButton(modifier = Modifier.focusRing(), onClick = { leaves(shuffle = true) { music.play(it, 0, source = d.title) } }) { Icon(Icons.Filled.Shuffle, null); Text("Shuffle") }
                    RadioButton()
                }
                ItemType.TRACK -> {
                    Button(onClick = { leaves { music.play(it, 0) } }, modifier = Modifier.focusRing().initialFocus(marquee.isTv)) { Icon(Icons.Filled.PlayArrow, null); Text("Play") }
                    RadioButton()
                }
                ItemType.COLLECTION -> PinToHomeButton("collection-${d.id}")
                else -> {}
            }
        StateButtons()
        Download()
        // Play on another Marquee app (USER-14); it expands albums and shows like its own Play button.
        if (d.type in listOf(ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO, ItemType.SHOW, ItemType.SEASON, ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK))
            PlayOnButton(nav, tint = MaterialTheme.colorScheme.onSurface, handoff = { app.marquee.api.models.RemoteCommand(app.marquee.api.models.RemoteCommand.Type.PLAY, itemIds = listOf(d.id)) })
        MoreMenu()
    }
    LazyColumn(contentPadding = PaddingValues(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
        item {
            if (marquee.isTv) TvHeader(d, square) { Actions() } else {
                // A backdrop when there is one; otherwise just room under the status bar.
                val backdrop = marquee.imageUrl(d.images?.backdrop, 1280)
                Box(Modifier.fillMaxWidth().height(if (backdrop != null) 220.dp else 24.dp)) {
                    backdrop?.let { AsyncImage(it, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize()) }
                    Box(Modifier.matchParentSize().background(Brush.verticalGradient(listOf(Color.Transparent, MaterialTheme.colorScheme.background))))
                }
                Row(Modifier.padding(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(18.dp)) {
                    Artwork(marquee.imageUrl(d.images?.poster ?: d.images?.thumb, 300), d.title, if (square) Shape.Square else Shape.Poster, Modifier.width(120.dp))
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) { Meta(d) }
                }
            }
        }
        if (!marquee.isTv) {
            // Buttons wrap onto more lines rather than hiding off the edge of a phone.
            item {
                FlowRow(Modifier.padding(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp)) { Actions() }
            }
            d.summary?.takeIf { it.isNotBlank() }?.let { s -> item { Text(s, Modifier.padding(horizontal = sidePadding), maxLines = 8, overflow = TextOverflow.Ellipsis) } }
        }
        if (pg.children.isNotEmpty()) item {
            when (d.type) {
                ItemType.SEASON -> Column {
                    Text("Episodes", Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                    pg.children.forEach { ep ->
                        ListItem(
                            headlineContent = { Text("${ep.index ?: ""}. ${ep.title}") },
                            supportingContent = { Text(listOfNotNull(ep.durationMs?.let { formatTime(it) }, if ((ep.viewCount ?: 0) > 0) "Watched" else null).joinToString(" · ")) },
                            leadingContent = { Artwork(marquee.imageUrl(ep.images?.thumb, 200), ep.title, Shape.Wide, Modifier.width(120.dp)) },
                            colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                            modifier = Modifier.focusCard({ nav.navigate("player/${ep.id}") }),
                        )
                    }
                }
                ItemType.ALBUM -> Column {
                    val tracks = pg.children.filter { it.type == ItemType.TRACK }
                    val showQuality by music.showQuality.collectAsState()
                    // Show audio quality (MUSIC-23): the album's format once when every track shares it.
                    val shared = tracks.mapNotNull { it.audioFormat?.let(app.marquee.music.AudioQuality::short) }.distinct()
                        .singleOrNull()?.takeIf { showQuality && tracks.all { it.audioFormat != null } }
                    Row(Modifier.padding(horizontal = sidePadding, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("Tracks", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                        shared?.let { Text(it, Modifier.padding(start = 10.dp).semantics { contentDescription = "Album audio quality: $it" },
                            style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                    tracks.forEachIndexed { i, t ->
                        ListItem(
                            headlineContent = { Text(t.title, maxLines = 1) },
                            supportingContent = t.audioFormat?.takeIf { showQuality }?.let { f -> {
                                Text(app.marquee.music.AudioQuality.short(f), style = MaterialTheme.typography.labelSmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                            } },
                            leadingContent = { Text("${t.index ?: i + 1}", color = MaterialTheme.colorScheme.onSurfaceVariant) },
                            trailingContent = {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Text(t.durationMs?.let { formatTime(it) } ?: "")
                                    if (!marquee.isTv) TrackMenu(t)
                                }
                            },
                            colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                            modifier = Modifier.focusCard({ music.play(tracks, i, source = d.title) }),
                        )
                    }
                }
                ItemType.ARTIST -> Column(verticalArrangement = Arrangement.spacedBy(20.dp)) {
                    if (pg.popular.isNotEmpty()) PopularTracks(d.title, pg.popular)
                    // Sections by release type (MusicBrainz, META-3).
                    releaseSections(pg.children).forEach { (title, list) ->
                        Shelf(title, list, sidePadding) { _, it ->
                            PosterCard(it, marquee.imageUrl(it.images?.poster, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) })
                        }
                    }
                }
                else -> Shelf(when (d.type) { ItemType.SHOW -> "Seasons"; ItemType.ARTIST -> "Albums"; else -> "In this collection" }, pg.children, sidePadding) { _, it ->
                    PosterCard(it, marquee.imageUrl(it.images?.poster, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) })
                }
            }
        }
        val cast = d.credits.filter { it.role == Credit.Role.ACTOR }.take(20)
        if (cast.isNotEmpty()) item {
            Column {
                Text("Cast", Modifier.padding(start = sidePadding, bottom = 10.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                LazyRow(contentPadding = PaddingValues(horizontal = sidePadding), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    items(cast) { c ->
                        Column(Modifier.width(84.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                            Avatar(c.name, if (c.hasPhoto == true) marquee.absolute("/api/v1/people/${c.personId}/photo?w=160") else null, 72)
                            Text(c.name, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall)
                            c.character?.let { Text(it, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                    }
                }
            }
        }
        if (pg.similar.isNotEmpty()) item {
            Shelf(if (d.type == ItemType.ARTIST) "Similar artists" else "Sounds like this", pg.similar, sidePadding) { _, it ->
                PosterCard(it, marquee.imageUrl(it.images?.poster, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) })
            }
        }
        // Extras: trailers, featurettes and more (LIB-8).
        d.extras?.takeIf { it.isNotEmpty() }?.let { extras ->
            item {
                Shelf("Extras", extras, sidePadding) { _, it ->
                    Column(Modifier.width(if (marquee.isTv) 220.dp else 180.dp)) {
                        Artwork(marquee.imageUrl(it.images?.thumb ?: it.images?.poster, 300), it.title, Shape.Wide,
                            Modifier.fillMaxWidth().focusCard({ nav.navigate("player/${it.id}") }).semantics { contentDescription = "Play ${it.title}" })
                        Text(it.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
                        it.extraType?.let { t -> Text(t.value.replace('_', ' ').replaceFirstChar { c -> c.uppercase() }, style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                }
            }
        }
        // Film series this belongs to (META-7).
        d.collections?.takeIf { it.isNotEmpty() }?.let { cols ->
            item {
                Shelf("Part of", cols, sidePadding) { _, it ->
                    PosterCard(it, marquee.imageUrl(it.images?.poster, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) })
                }
            }
        }
        if (pg.related.isNotEmpty()) item {
            Shelf("More like this", pg.related, sidePadding) { _, it ->
                PosterCard(it, marquee.imageUrl(it.images?.poster, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, it) })
            }
        }
    }
}

@Composable
private fun Meta(d: ItemDetail) {
    if (d.type == ItemType.EPISODE) d.grandparentTitle?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    Text(d.title, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold, maxLines = 3)
    // Smart collections (META-7) fill themselves from their rules.
    if (d.type == ItemType.COLLECTION && d.smartRules != null) Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.AutoAwesome, null, Modifier.size(16.dp), tint = Gold)
        Text("Smart collection", Modifier.padding(start = 6.dp), style = MaterialTheme.typography.labelLarge, color = Gold)
    }
    if (d.type != ItemType.ARTIST) d.artistCredit?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    Text(listOfNotNull(d.year?.toString(), d.contentRating, d.durationMs?.takeIf { d.type in listOf(ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO) }?.let { formatTime(it) }).joinToString(" · "),
        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    if (d.genres.isNotEmpty()) Text(d.genres.joinToString(", "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    // Critic and community ratings, where known (AniList for anime).
    d.ratings?.let { r ->
        val parts = listOfNotNull(
            r.imdb?.let { "IMDb %.1f".format(it) },
            r.rottenTomatoes?.let { "RT $it%" },
            r.metacritic?.let { "Metacritic $it" },
            r.anilist?.let { "AniList $it%" },
        )
        if (parts.isNotEmpty()) Text(parts.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** TV: the backdrop fills the screen's top with the details and actions over it, in view from the start. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun TvHeader(d: ItemDetail, square: Boolean, actions: @Composable () -> Unit) {
    val marquee = LocalMarquee.current
    val bg = MaterialTheme.colorScheme.background
    Box(Modifier.fillMaxWidth().height(400.dp)) {
        marquee.imageUrl(d.images?.backdrop, 1280)?.let { AsyncImage(it, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize()) }
        Box(Modifier.matchParentSize().background(Brush.horizontalGradient(0f to bg, 0.65f to bg.copy(alpha = 0.6f), 1f to Color.Transparent)))
        Box(Modifier.matchParentSize().background(Brush.verticalGradient(0.6f to Color.Transparent, 1f to bg)))
        Row(Modifier.align(Alignment.BottomStart).padding(horizontal = 48.dp, vertical = 16.dp), horizontalArrangement = Arrangement.spacedBy(28.dp), verticalAlignment = Alignment.Bottom) {
            Artwork(marquee.imageUrl(d.images?.poster ?: d.images?.thumb, 300), d.title, if (square) Shape.Square else Shape.Poster, Modifier.width(if (square) 200.dp else 160.dp))
            Column(Modifier.widthIn(max = 560.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Meta(d)
                d.summary?.takeIf { it.isNotBlank() }?.let { Text(it, maxLines = 3, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium) }
                // Wraps rather than squeezing labels when an album or artist has many actions.
                FlowRow(Modifier.padding(top = 10.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { actions() }
            }
        }
    }
}

/** An artist's releases by release type: Albums (and unknown), Singles & EPs, Live Albums… */
fun releaseSections(items: List<ItemSummary>): List<Pair<String, List<ItemSummary>>> = listOf(
    "Albums" to setOf("album", ""), "Singles & EPs" to setOf("ep", "single"), "Live Albums" to setOf("live"),
    "Compilations" to setOf("compilation"), "Soundtracks" to setOf("soundtrack"), "Remixes" to setOf("remix"),
    "Demos" to setOf("demo"), "Other" to setOf("other"),
).map { (title, types) -> title to items.filter { (it.releaseType?.value ?: "") in types } }.filter { it.second.isNotEmpty() }

/** An artist's most-listened tracks: five, or all of them. */
@Composable
private fun PopularTracks(artist: String, tracks: List<ItemSummary>) {
    val music = LocalMusic.current
    var all by remember { mutableStateOf(false) }
    Column {
        Text("Popular", Modifier.padding(horizontal = sidePadding, vertical = 4.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        (if (all) tracks else tracks.take(5)).forEachIndexed { i, t ->
            ListItem(
                headlineContent = { Text(t.title, maxLines = 1) },
                supportingContent = { t.parentTitle?.let { Text(it, maxLines = 1) } },
                leadingContent = { Text("${i + 1}", color = MaterialTheme.colorScheme.onSurfaceVariant) },
                trailingContent = { Text(t.durationMs?.let { formatTime(it) } ?: "") },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ music.play(tracks, i, source = "$artist – Popular") }),
            )
        }
        if (tracks.size > 5) TextButton({ all = !all }, Modifier.padding(horizontal = sidePadding).focusRing()) { Text(if (all) "Show fewer" else "Show all ${tracks.size}") }
    }
}
