package app.marquee.ui

import androidx.compose.ui.semantics.CustomAccessibilityAction
import app.marquee.core.WatchChoices
import app.marquee.core.WatchState
import androidx.compose.ui.semantics.customActions
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.VodEpisode
import app.marquee.api.models.VodSeries
import app.marquee.api.models.VodSeriesDetail
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// VOD Shows (LIVE-6): series from Xtream Codes sources. A show's seasons and episodes are fetched
// from the source the first time it's opened; viewing order and what's next come from the server.

private fun VodSeries.meta() = listOfNotNull(
    year?.toString(),
    seasonCount.takeIf { it > 0 }?.let { "$it ${if (it == 1) "season" else "seasons"}" },
).joinToString(" · ")

/** How far through an episode, while it's partly watched. */
internal fun VodEpisode.progress(): Float? {
    if (watched) return null
    val pos = positionMs?.takeIf { it > 0 } ?: return null
    val dur = runtimeMs?.takeIf { it > 0 } ?: return null
    return (pos.toFloat() / dur).coerceIn(0f, 1f)
}

/** A show's poster with its title underneath; a watched tick once every episode is watched, or how many are. */
@Composable
internal fun VodShowTile(s: VodSeries, width: Dp?, onOpen: () -> Unit) {
    val marquee = LocalMarquee.current
    Column(if (width != null) Modifier.width(width) else Modifier.fillMaxWidth()) {
        Box {
            Artwork(marquee.absolute(s.posterUrl), s.title, Shape.Poster, Modifier.fillMaxWidth().focusCard(onOpen).inLibraryBorder(s.inLibrary)
                .semantics {
                    contentDescription = s.title
                    vodTileState(s.watched, s.inLibrary)?.let { stateDescription = it }
                })
            if (s.watched) Icon(Icons.Filled.CheckCircle, null, Modifier.align(Alignment.TopEnd).padding(6.dp).size(20.dp)
                .background(Color.Black.copy(alpha = 0.55f), RoundedCornerShape(50)), tint = Gold)
            else if (s.watchedCount > 0 && s.episodeCount > 0) LinearProgressIndicator(progress = { s.watchedCount.toFloat() / s.episodeCount },
                Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(6.dp).height(3.dp),
                color = Gold, trackColor = Color.White.copy(alpha = 0.3f), drawStopIndicator = {})
        }
        Text(s.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text(s.meta(), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Continue Watching for shows: the next episode's still, with the show and episode under it; opening it plays that episode. */
@Composable
internal fun VodNextEpisodeTile(s: VodSeries, width: Dp, actions: List<TileAction> = emptyList(), onOpen: () -> Unit) {
    val marquee = LocalMarquee.current
    val e = s.nextEpisode
    var menu by remember { mutableStateOf(false) }
    Column(Modifier.width(width)) {
        Box {
            Artwork(marquee.absolute(e?.stillUrl ?: s.posterUrl), s.title, Shape.Wide, Modifier.fillMaxWidth()
                .focusCard(onOpen, onLongClick = if (actions.isEmpty()) null else ({ menu = true }))
                .inLibraryBorder(s.inLibrary)
                .semantics {
                    contentDescription = listOfNotNull(s.title, e?.label()).joinToString(", ")
                    vodTileState(false, s.inLibrary)?.let { stateDescription = it }
                    if (actions.isNotEmpty()) customActions = actions.map { a -> CustomAccessibilityAction(a.label) { a.run(); true } }
                })
            TileMenu(actions, menu) { menu = false }
            e?.progress()?.let {
                LinearProgressIndicator(progress = { it }, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(6.dp).height(3.dp),
                    color = Gold, trackColor = Color.White.copy(alpha = 0.3f), drawStopIndicator = {})
            }
        }
        Text(s.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        if (e != null) Text("${e.label()} · ${e.title}", maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * A show's page: backdrop, poster, details, Play (or Resume) for the next episode, watched for the
 * show and a season, a season picker and the season's episodes, each played from here.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun VodShowScreen(nav: NavHostController, seriesId: Long) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val changed by VodChanges.version.collectAsState()
    var show by remember { mutableStateOf<VodSeriesDetail?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var season by rememberSaveable { mutableStateOf<Int?>(null) }
    // Watched set here shows at once; the reload after the change replaces it.
    val episodeOverride = remember { mutableStateMapOf<Long, Boolean>() }
    var showOverride by remember { mutableStateOf<Boolean?>(null) }
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(seriesId, changed) {
        withContext(Dispatchers.IO) { runCatching { marquee.livetv.getVodSeries(seriesId) } }
            .onSuccess {
                show = it; error = null; episodeOverride.clear(); showOverride = null
                // Opens on the next episode's season, then stays where the person put it (marking watched moves "next").
                if (season == null) season = it.nextEpisode?.season ?: it.seasons.firstOrNull()?.season
            }
            .onFailure { if (show == null) error = it.message ?: "Couldn't load the show" }
    }
    val s = show
    if (s == null) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            error?.let { Text(it, Modifier.padding(24.dp), color = MaterialTheme.colorScheme.error) } ?: Column(horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator()
                // The first open fetches the episodes from the source, which can take a few seconds.
                Text("Loading the episodes…", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (!marquee.isTv) IconButton({ nav.popBackStack() }, Modifier.align(Alignment.TopStart).padding(4.dp)) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
        }
        return
    }
    val next = s.nextEpisode
    val shown = season?.takeIf { n -> s.seasons.any { it.season == n } } ?: next?.season ?: s.seasons.firstOrNull()?.season
    val seasonInfo = s.seasons.firstOrNull { it.season == shown }
    val episodes = s.episodes.filter { it.season == shown }
    fun watched(e: VodEpisode) = episodeOverride[e.id] ?: showOverride ?: e.watched
    val showWatched = showOverride ?: s.watched
    val seasonWatched = episodes.isNotEmpty() && episodes.all { watched(it) }
    fun play(e: VodEpisode, from: Long? = null) = nav.navigate("vodepplay/${s.id}/${e.id}" + (from?.let { "?start=$it" } ?: ""))
    fun change(apply: () -> Unit, undo: () -> Unit, call: suspend () -> Unit) {
        apply()
        busy = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { call() } }.onFailure { undo() }
            busy = false
            VodChanges.bump()
        }
    }
    fun setEpisode(e: VodEpisode, on: Boolean) = change({ episodeOverride[e.id] = on }, { episodeOverride.remove(e.id) }) {
        if (on) marquee.livetv.markVodEpisodeWatched(e.id) else marquee.livetv.markVodEpisodeUnwatched(e.id)
    }
    fun setSeason(n: Int, on: Boolean) = change({ episodes.forEach { episodeOverride[it.id] = on } }, { episodes.forEach { episodeOverride.remove(it.id) } }) {
        if (on) marquee.livetv.markVodSeriesWatched(s.id, n) else marquee.livetv.markVodSeriesUnwatched(s.id, n)
    }
    fun setShow(on: Boolean) = change({ episodeOverride.clear(); showOverride = on }, { showOverride = null }) {
        if (on) marquee.livetv.markVodSeriesWatched(s.id) else marquee.livetv.markVodSeriesUnwatched(s.id)
    }
    val meta = listOfNotNull(s.year?.toString(), s.contentRating,
        s.seasonCount.takeIf { it > 0 }?.let { "$it ${if (it == 1) "season" else "seasons"}" })

    BoxWithConstraints(Modifier.fillMaxSize()) {
        val wide = maxWidth > 600.dp
        s.backdropUrl?.let { url ->
            Box(Modifier.fillMaxWidth().height(if (wide) 420.dp else 260.dp)) {
                AsyncImage(marquee.absolute(url), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop, alpha = 0.4f)
                Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Transparent, MaterialTheme.colorScheme.background))))
            }
        }
        LazyColumn(Modifier.fillMaxSize().semantics { contentDescription = "Show" }, // Phones leave room for the back button above the poster.
            contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = if (marquee.isTv) 24.dp else 56.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)) {
            item(key = "top") {
                val poster = @Composable { w: Dp -> Artwork(marquee.absolute(s.posterUrl), s.title, Shape.Poster, Modifier.width(w)) }
                val details = @Composable {
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        s.category?.let { Text("VOD Shows · $it", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelLarge) }
                        Text(s.title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                        if (s.inLibrary == true) InLibraryLabel()
                        if (meta.isNotEmpty()) Text(meta.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (s.genres.isNotEmpty()) Text(s.genres.joinToString(", "), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                        s.rating?.let {
                            Text("${if (s.matched) "TMDB " else ""}${"%.1f".format(it)}", Modifier.clip(RoundedCornerShape(6.dp)).background(Surface2).padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold)
                        }
                        FlowRow(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            if (next != null) {
                                val pos = next.positionMs ?: 0
                                val resume = pos > 0 && !watched(next) && (episodeOverride[next.id] ?: showOverride) == null
                                Button({ play(next) }, Modifier.focusRing().initialFocus(marquee.isTv)) {
                                    Icon(Icons.Filled.PlayArrow, null)
                                    Text(if (resume) "Resume ${next.label()} from ${formatTime(pos)}" else "Play ${next.label()}", Modifier.padding(start = 6.dp))
                                }
                                if (resume) OutlinedButton({ play(next, 0) }, Modifier.focusRing()) {
                                    Icon(Icons.Filled.Replay, null)
                                    Text("Play from start", Modifier.padding(start = 6.dp))
                                }
                            }
                            if (s.episodes.isNotEmpty()) OutlinedButton({ setShow(!showWatched) }, Modifier.focusRing().initialFocus(marquee.isTv && next == null).semantics {
                                contentDescription = if (showWatched) "Mark the show unwatched" else "Mark the show watched"
                                stateDescription = if (showWatched) "Watched" else "Not watched"
                            }, enabled = !busy) {
                                Icon(Icons.Filled.Check, null, tint = if (showWatched) Gold else MaterialTheme.colorScheme.onSurface)
                                Text(if (showWatched) "Watched" else "Mark watched", Modifier.padding(start = 6.dp))
                            }
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
            s.overview?.let { item(key = "overview") { Text(it, Modifier.widthIn(max = 720.dp)) } }
            if (!s.matched) item(key = "unmatched") {
                Text("Details from the source (not matched to TMDB).", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            }
            s.episodesError?.let { item(key = "episodesError") { Text("Couldn't get the episodes: $it", color = MaterialTheme.colorScheme.error) } }
            if (s.seasons.isNotEmpty()) item(key = "seasons") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    LazyRow(Modifier.semantics { contentDescription = "Seasons" }, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(s.seasons, key = { it.season }) { x ->
                            FilterChip(x.season == shown, { season = x.season }, { Text(x.name) }, Modifier.focusRing(RoundedCornerShape(8.dp)))
                        }
                    }
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        seasonInfo?.let {
                            Text("${it.episodeCount} ${if (it.episodeCount == 1) "episode" else "episodes"}", color = MaterialTheme.colorScheme.onSurfaceVariant,
                                style = MaterialTheme.typography.bodySmall)
                        }
                        if (shown != null && episodes.isNotEmpty()) OutlinedButton({ setSeason(shown, !seasonWatched) }, Modifier.focusRing().semantics {
                            contentDescription = if (seasonWatched) "Mark the season unwatched" else "Mark the season watched"
                            stateDescription = if (seasonWatched) "Watched" else "Not watched"
                        }, enabled = !busy) {
                            Icon(Icons.Filled.Check, null, tint = if (seasonWatched) Gold else MaterialTheme.colorScheme.onSurface)
                            Text(if (seasonWatched) "Season watched" else "Mark season watched", Modifier.padding(start = 6.dp))
                        }
                    }
                }
            }
            items(episodes, key = { it.id }) { e ->
                // Marked here: what the mark leaves (PLAY-5), and no progress until the reload.
                val marked = episodeOverride[e.id] ?: showOverride
                VodEpisodeRow(e, watched(e), marked?.let(WatchState::choicesAfterMark) ?: WatchState.choices(e.watched, (e.positionMs ?: 0) > 0),
                    showProgress = marked == null, wide = wide, onPlay = { play(e) }, onWatched = { setEpisode(e, it) })
            }
            if (s.episodes.isEmpty() && s.episodesError == null) item(key = "none") {
                Text("No episodes listed yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (s.cast.isNotEmpty()) {
                item(key = "castTitle") { Text("Cast", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
                item(key = "cast") {
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                        items(s.cast) { c ->
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
 * An episode: its still (with how far it was watched), number, title, runtime and overview; opening it plays it. The tick marks it
 * watched or unwatched; a long press (TV: hold OK, or the menu key) offers Mark as watched and/or unwatched, both while part-way through (PLAY-5).
 */
@Composable
private fun VodEpisodeRow(e: VodEpisode, watched: Boolean, choices: WatchChoices, showProgress: Boolean, wide: Boolean, onPlay: () -> Unit, onWatched: (Boolean) -> Unit) {
    val marquee = LocalMarquee.current
    val progress = if (watched || !showProgress) null else e.progress()
    val actions = buildList {
        if (choices.markWatched) add(TileAction("Mark as watched") { onWatched(true) })
        if (choices.markUnwatched) add(TileAction("Mark as unwatched") { onWatched(false) })
    }
    var menu by remember { mutableStateOf(false) }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        Box(Modifier.weight(1f)) {
            Row(Modifier.fillMaxWidth().focusCard(onPlay, onLongClick = { menu = true }).semantics(mergeDescendants = true) {
                contentDescription = "${e.label()}, ${e.title}"
                stateDescription = if (watched) "Watched" else progress?.let { "${(it * 100).toInt()}% watched" } ?: "Not watched"
                customActions = actions.map { a -> CustomAccessibilityAction(a.label) { a.run(); true } }
            }, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                Box(Modifier.width(if (wide) 200.dp else 140.dp)) {
                    Artwork(marquee.absolute(e.stillUrl), "E${e.episode}", Shape.Wide, Modifier.fillMaxWidth())
                    progress?.let {
                        LinearProgressIndicator(progress = { it }, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(6.dp).height(3.dp),
                            color = Gold, trackColor = Color.White.copy(alpha = 0.3f), drawStopIndicator = {})
                    }
                }
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text("${e.episode}. ${e.title}", fontWeight = FontWeight.Medium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    val meta = listOfNotNull(e.runtimeMs?.let(::formatRuntime), e.airDate)
                    if (meta.isNotEmpty()) Text(meta.joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                    if (wide) e.overview?.let { Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis) }
                }
            }
            TileMenu(actions, menu) { menu = false }
        }
        IconButton({ onWatched(!watched) }, Modifier.focusRing().semantics {
            contentDescription = if (watched) "Mark ${e.label()} unwatched" else "Mark ${e.label()} watched"
            stateDescription = if (watched) "Watched" else "Not watched"
        }) {
            Icon(if (watched) Icons.Filled.CheckCircle else Icons.Filled.RadioButtonUnchecked, null, tint = if (watched) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
