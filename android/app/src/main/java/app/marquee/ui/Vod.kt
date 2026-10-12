package app.marquee.ui

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
import androidx.compose.foundation.layout.aspectRatio
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
import androidx.compose.material.icons.filled.Forward30
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Refresh
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
import app.marquee.api.apis.LivetvApi.SortListVodMovies
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

private val vodSorts = listOf(
    SortListVodMovies.TITLE to "Title",
    SortListVodMovies.ADDED to "Recently added",
    SortListVodMovies.YEAR to "Release year",
    SortListVodMovies.RATING to "Rating",
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
private fun VodTile(m: VodMovie, width: Dp?, onOpen: () -> Unit) {
    val marquee = LocalMarquee.current
    Column(if (width != null) Modifier.width(width) else Modifier.fillMaxWidth()) {
        Box {
            Artwork(marquee.absolute(m.posterUrl), m.title, Shape.Poster, Modifier.fillMaxWidth().focusCard(onOpen)
                .semantics {
                    contentDescription = m.title
                    if (m.watched) stateDescription = "Watched"
                })
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

/** What the Movies tab has loaded, kept while Live TV is on the back stack (so Back returns to the same place). */
private class VodList {
    val movies = mutableStateOf<List<VodMovie>?>(null)
    val total = mutableIntStateOf(0)
    val error = mutableStateOf<String?>(null)
    val loadingMore = mutableStateOf(false)
    var loadedFor: Triple<String, String, SortListVodMovies>? = null
    val grid = LazyGridState()
}

/**
 * The VOD tab of Live TV (movies on demand): categories, search, sort, Continue Watching and a poster grid that
 * loads a page at a time. [header] is Live TV's title and tabs, scrolled with the grid.
 */
@Composable
fun VodBrowse(nav: NavHostController, header: @Composable () -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var category by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf(SortListVodMovies.TITLE) }
    var text by rememberSaveable { mutableStateOf("") }
    var q by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(text) { delay(300); q = text.trim() }
    val changed by VodChanges.version.collectAsState()
    val categories by cachedLoad<List<VodCategory>>("vod:categories") {
        withContext(Dispatchers.IO) { runCatching { marquee.livetv.listVodCategories() }.getOrNull() }
    }
    val resume by cachedLoad<List<VodMovie>>("vod:continue", changed) {
        withContext(Dispatchers.IO) { runCatching { marquee.livetv.vodContinueWatching() }.getOrNull() }
    }
    val state = rememberCached("vod:list") { VodList() }
    var movies by state.movies

    suspend fun fetch(offset: Int, limit: Int) = withContext(Dispatchers.IO) {
        runCatching { marquee.livetv.listVodMovies(category.ifEmpty { null }, q.ifEmpty { null }, sort, limit, offset) }
    }
    // A new filter starts again from the top; coming back (or a change from the film's page)
    // refreshes what's loaded in place, keeping the place in the grid.
    LaunchedEffect(category, q, sort, changed) {
        val key = Triple(category, q, sort)
        val have = movies
        if (state.loadedFor == key && have != null) {
            fetch(0, have.size.coerceAtLeast(PAGE)).onSuccess { movies = it.movies; state.total.intValue = it.total }
            return@LaunchedEffect
        }
        movies = null
        state.error.value = null
        state.loadedFor = key
        fetch(0, PAGE).onSuccess { movies = it.movies; state.total.intValue = it.total }
            .onFailure { state.error.value = it.message ?: "Couldn't load the movies"; movies = emptyList() }
        state.grid.scrollToItem(0)
    }
    fun loadMore() {
        val have = movies ?: return
        if (state.loadingMore.value || have.size >= state.total.intValue) return
        state.loadingMore.value = true
        val key = state.loadedFor
        scope.launch {
            fetch(have.size, PAGE).onSuccess { page -> if (state.loadedFor == key) movies = (movies ?: emptyList()) + page.movies }
            state.loadingMore.value = false
        }
    }
    // The next page as the end of the grid comes into view.
    LaunchedEffect(state) {
        snapshotFlow { state.grid.layoutInfo.let { (it.visibleItemsInfo.lastOrNull()?.index ?: 0) >= it.totalItemsCount - 12 } }
            .distinctUntilChanged().filter { it }.collect { loadMore() }
    }
    fun open(m: VodMovie) = nav.navigate("vod/${m.id}")

    val all = categories?.sumOf { it.count }
    val list = movies
    val full: androidx.compose.foundation.lazy.grid.LazyGridItemSpanScope.() -> GridItemSpan = { GridItemSpan(maxLineSpan) }
    LazyVerticalGrid(
        GridCells.Adaptive(if (marquee.isTv) 150.dp else 108.dp), Modifier.fillMaxSize().semantics { contentDescription = "VOD" }, state.grid,
        contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = 16.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        // The header and filters span the grid; the side padding is the grid's, so they undo theirs.
        item(span = full, key = "header") { HeaderNoPadding(header) }
        item(span = full, key = "filters") {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(text, { text = it }, Modifier.fillMaxWidth().widthIn(max = 480.dp).semantics { contentDescription = "Search movies" }, singleLine = true,
                    leadingIcon = { Icon(Icons.Filled.Search, null) }, placeholder = { Text("Search movies") })
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    item { FilterChip(category.isEmpty(), { category = "" }, { Text("All movies" + (all?.let { " ($it)" } ?: "")) }, Modifier.focusRing(RoundedCornerShape(8.dp))) }
                    items(categories ?: emptyList(), key = { it.name }) { c ->
                        FilterChip(category == c.name, { category = c.name }, { Text("${c.name} (${c.count})") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
                    }
                }
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    item { Text("Sort", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelLarge) }
                    items(vodSorts) { (s, label) -> FilterChip(sort == s, { sort = s }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp))) }
                }
            }
        }
        val r = resume
        if (category.isEmpty() && q.isEmpty() && !r.isNullOrEmpty()) item(span = full, key = "continue") {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("Continue Watching", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                LazyRow(Modifier.semantics { contentDescription = "Continue Watching" }, contentPadding = PaddingValues(vertical = 6.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    items(r, key = { it.id }) { m -> VodTile(m, if (marquee.isTv) 150.dp else 112.dp) { open(m) } }
                }
            }
        }
        if ((category.isNotEmpty() || q.isNotEmpty()) && list != null) item(span = full, key = "count") {
            val n = state.total.intValue
            Text("$n ${if (n == 1) "movie" else "movies"}", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        }
        when {
            list == null -> item(span = full, key = "loading") { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            state.error.value != null -> item(span = full, key = "error") { Text(state.error.value!!, color = MaterialTheme.colorScheme.error) }
            list.isEmpty() -> item(span = full, key = "empty") {
                Text(if (q.isNotEmpty()) "No movies match “$q”." else "No movies here yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            else -> items(list, key = { it.id }) { m -> VodTile(m, null) { open(m) } }
        }
        if (state.loadingMore.value) item(span = full, key = "more") { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
    }
}

/** Live TV's header pads itself; inside the grid (which pads) it's pulled back to the edge. */
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
    val position = m.positionMs ?: 0
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
 * watched near the end); the session stops when the player closes. No trailers, Up Next,
 * rating request, watch together or casting: those are for library items.
 */
@OptIn(UnstableApi::class)
@Composable
fun VodPlayerScreen(nav: NavHostController, movieId: Long, startMs: Long?) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val player = remember { videoPlayer(context, seekIncrements = true) }
    var movie by remember { mutableStateOf<VodMovieDetail?>(null) }
    var session by remember { mutableStateOf<VodSession?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var position by remember { mutableLongStateOf(0L) }
    var duration by remember { mutableLongStateOf(0L) }
    var playing by remember { mutableStateOf(false) }
    var buffering by remember { mutableStateOf(true) }
    var controls by remember { mutableStateOf(true) }
    var lastTouch by remember { mutableIntStateOf(0) }
    var scrub by remember { mutableStateOf<Long?>(null) }
    val ownEntry = remember { nav.currentBackStackEntry }
    fun stillShowing() = ownEntry != null && nav.currentBackStackEntry === ownEntry

    suspend fun send(id: String, pos: Long, dur: Long) {
        if (pos <= 0) return
        runCatching { marquee.livetv.reportVodPlayback(id, ReportVodPlaybackRequest(pos, dur.takeIf { it > 0 })) }
    }
    fun report() {
        val id = session?.id ?: return
        val pos = player.currentPosition
        val dur = player.duration
        marquee.scope.launch { send(id, pos, dur) }
    }

    LaunchedEffect(movieId) {
        launch { movie = withContext(Dispatchers.IO) { runCatching { marquee.livetv.getVodMovie(movieId) }.getOrNull() } }
        // Started on the session's own scope: if the player closes while the server is starting
        // it, the session is stopped as soon as it arrives rather than left running.
        val pending = marquee.scope.async { runCatching { marquee.livetv.playVodMovie(movieId) } }
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
            }.onFailure { error = it.message ?: "Couldn't start the movie" }
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
    VideoWindow()
    DisposableEffect(Unit) {
        val listener = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                playing = isPlaying
                if (!isPlaying && player.playbackState != Player.STATE_ENDED) report()
            }
            override fun onPlaybackStateChanged(state: Int) {
                buffering = state == Player.STATE_BUFFERING || state == Player.STATE_IDLE
                // The end: closing reports it (so it counts as watched) and stops the session.
                if (state == Player.STATE_ENDED && stillShowing()) nav.popBackStack()
            }
            override fun onPlayerError(e: androidx.media3.common.PlaybackException) {
                error = "The movie stopped playing (${e.errorCodeName})."
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
    LaunchedEffect(controls) { if (!controls) runCatching { rootFocus.requestFocus() } }

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
                if (e.type != KeyEventType.KeyDown || (controls && scrub == null)) return@onPreviewKeyEvent false
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
        if (buffering && error == null) CircularProgressIndicator(Modifier.align(Alignment.Center))
        error?.let { Text(it, Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error) }
        if (!controls && scrub != null) Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 48.dp, vertical = 32.dp)) {
            ScrubBar(scrub!!, duration, null, onChange = { scrub = it }, onDone = ::commitScrub)
        }
        AnimatedVisibility(controls, Modifier.fillMaxSize(), enter = fadeIn(), exit = fadeOut()) {
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color.Black.copy(alpha = 0.7f), 0.3f to Color.Transparent, 0.65f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.8f)))) {
                Row(Modifier.align(Alignment.TopStart).fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton({ nav.popBackStack() }, Modifier.focusRing()) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Close player", tint = Color.White) }
                    Column(Modifier.weight(1f).padding(start = 8.dp)) {
                        Text(movie?.title ?: "", color = Color.White, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        movie?.let { d ->
                            val meta = listOfNotNull(d.year?.toString(), d.runtimeMs?.let(::formatRuntime)).joinToString(" · ")
                            if (meta.isNotEmpty()) Text(meta, color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.bodySmall)
                        }
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
