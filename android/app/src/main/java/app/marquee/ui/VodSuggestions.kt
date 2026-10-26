package app.marquee.ui

import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.saveable.rememberSaveable
import app.marquee.api.models.VodRecommendationRow
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.Tv
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import app.marquee.api.apis.LivetvApi
import app.marquee.api.models.VodRef
import app.marquee.api.models.VodSuggestion
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Suggestions from the films and series on demand (LIVE-6, USER-16): Home's Watch on VOD row,
// Also on VOD on a movie's or show's page, the VOD pages' Recommended for you and Muse's On VOD.
// Each tile opens the film's or show's page and shows why it's suggested; its context menu has
// Not interested and Go to film / Go to show.

/** The film's or show's page for a suggestion. */
fun vodRoute(s: VodSuggestion): String? = s.movie?.let { "vod/${it.id}" } ?: s.series?.let { "vodshow/${it.id}" }

/** The film's or show's page for the same title on demand (Discover's Watch now on VOD). */
fun vodRoute(v: VodRef): String = if (v.kind == VodRef.Kind.MOVIE) "vod/${v.id}" else "vodshow/${v.id}"

private fun VodSuggestion.title() = movie?.title ?: series?.title ?: ""

/** The line under a suggestion when there's no reason: the year and runtime, or the year and seasons. */
private fun VodSuggestion.facts(): String {
    movie?.let { m -> return listOfNotNull(m.year?.toString(), m.runtimeMs?.let(::formatRuntime)).joinToString(" · ") }
    series?.let { s ->
        val seasons = s.seasonCount.takeIf { it > 0 }?.let { if (it == 1) "1 season" else "$it seasons" }
        return listOfNotNull(s.year?.toString(), seasons).joinToString(" · ")
    }
    return ""
}

/**
 * Not interested: the server stops offering it. [onHidden] runs at once (the tile goes), and
 * [onFailed] puts it back if the call fails.
 */
fun hideVodSuggestion(marquee: Marquee, context: Context, s: VodSuggestion, onHidden: () -> Unit = {}, onFailed: () -> Unit = {}) {
    val id = s.movie?.id ?: s.series?.id ?: return
    val kind = if (s.kind == VodSuggestion.Kind.MOVIE) LivetvApi.KindHideVodSuggestion.MOVIE else LivetvApi.KindHideVodSuggestion.SERIES
    val title = s.title()
    onHidden()
    marquee.scope.launch {
        withContext(Dispatchers.IO) { runCatching { marquee.livetv.hideVodSuggestion(kind, id) } }
            .onSuccess { withContext(Dispatchers.Main) { toast(context, "You won't be offered “$title” again.") } }
            .onFailure { withContext(Dispatchers.Main) { onFailed(); toast(context, "Couldn't hide “$title”: ${serverMessage(it, "")}") } }
    }
}

/**
 * Takes a film or series on demand off the watchlist (USER-8), from Home's Your Watchlist row.
 * [onRemoved] runs at once (the tile goes), and [onFailed] puts it back if the call fails.
 */
fun removeVodFromWatchlist(marquee: Marquee, context: Context, s: VodSuggestion, onRemoved: () -> Unit = {}, onFailed: () -> Unit = {}) {
    val title = s.title()
    onRemoved()
    marquee.scope.launch {
        withContext(Dispatchers.IO) {
            runCatching {
                s.movie?.let { marquee.livetv.removeVodMovieFromWatchlist(it.id) } ?: s.series?.let { marquee.livetv.removeVodSeriesFromWatchlist(it.id) }
            }
        }
            .onSuccess { VodChanges.bump() }
            .onFailure { withContext(Dispatchers.Main) { onFailed(); toast(context, "Couldn't remove “$title”: ${serverMessage(it, "")}") } }
    }
}

/**
 * The small "VOD" pill on a poster that's on demand rather than in a library (Home's mixed rows):
 * bold caps on a dark backing, legible over any artwork.
 */
@Composable
fun VodBadge(modifier: Modifier = Modifier) {
    Text("VOD", modifier.background(Color.Black.copy(alpha = 0.72f), RoundedCornerShape(4.dp)).padding(horizontal = 5.dp, vertical = 1.dp),
        color = Color.White, style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.Bold, letterSpacing = 0.6.sp)
}

/**
 * One suggestion: the poster, the title (a film or show icon before it) and the reason under it.
 * [badge] marks the poster VOD, for rows that mix it with library titles; [watchlist] is for Your
 * Watchlist, where the menu removes it from the watchlist rather than Not interested.
 */
@Composable
fun VodSuggestionTile(nav: NavHostController, s: VodSuggestion, width: Dp?, autoFocus: Boolean = false, badge: Boolean = false,
    watchlist: Boolean = false, onOpen: () -> Unit = {}, onHide: () -> Unit) {
    val marquee = LocalMarquee.current
    var menu by remember { mutableStateOf(false) }
    val m = s.movie
    val sh = s.series
    val title = s.title()
    val sub = s.reason?.takeIf { it.isNotBlank() } ?: s.facts().ifEmpty { "VOD" }
    val watched = m?.watched ?: sh?.watched ?: false
    val inLibrary = m?.inLibrary ?: sh?.inLibrary
    val open = { onOpen(); vodRoute(s)?.let { nav.navigate(it) }; Unit }
    val actions = buildList {
        // In Your Watchlist (USER-8): [onHide] takes it off the watchlist instead.
        if (!marquee.isOffline) add(TileAction(if (watchlist) "Remove from watchlist" else "Not interested", onHide))
        add(TileAction(if (m != null) "Go to film" else "Go to show") { vodRoute(s)?.let { nav.navigate(it) } })
    }
    Column(if (width != null) Modifier.width(width) else Modifier.fillMaxWidth()) {
        Box {
            Artwork(marquee.absolute(m?.posterUrl ?: sh?.posterUrl), title, Shape.Poster, Modifier.fillMaxWidth().initialFocus(autoFocus)
                .focusCard(open, onLongClick = { menu = true })
                .inLibraryBorder(inLibrary)
                .semantics {
                    contentDescription = listOf(title, sub).filter { it.isNotBlank() }.joinToString(", ")
                    vodTileState(watched, inLibrary)?.let { stateDescription = it }
                    customActions = actions.map { a -> CustomAccessibilityAction(a.label) { a.run(); true } }
                })
            TileMenu(actions, menu) { menu = false }
            if (badge) VodBadge(Modifier.align(Alignment.TopStart).padding(6.dp))
            if (watched) Icon(Icons.Filled.CheckCircle, null, Modifier.align(Alignment.TopEnd).padding(6.dp).size(20.dp)
                .background(Color.Black.copy(alpha = 0.55f), RoundedCornerShape(50)), tint = Gold)
        }
        Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(if (m != null) Icons.Filled.Movie else Icons.Filled.Tv, null, Modifier.padding(end = 4.dp).size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        }
        Text(sub, maxLines = 2, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * A titled row of suggestions, less those just marked Not interested (nothing when empty).
 * [onTitle] makes the title open a page; [autoFocusFirst] focuses the first tile on TV.
 */
@Composable
fun VodSuggestionRow(nav: NavHostController, title: String, list: List<VodSuggestion>?, padding: Dp = sidePadding, autoFocusKey: String? = null,
    onTitle: (() -> Unit)? = null, onSeeAll: (() -> Unit)? = null, onOpen: (String) -> Unit = {}) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val hidden = remember { mutableStateListOf<String>() }
    val shown = list.orEmpty().filter { it.key !in hidden && vodRoute(it) != null }
    if (shown.isEmpty()) return
    Box(Modifier.semantics { contentDescription = title }) {
        Shelf(title, shown, padding, onTitle = onTitle, onSeeAll = onSeeAll) { _, s ->
            VodSuggestionTile(nav, s, if (marquee.isTv) 140.dp else 116.dp, autoFocus = marquee.isTv && s.key == autoFocusKey, onOpen = { onOpen(s.key) }) {
                hideVodSuggestion(marquee, context, s, onHidden = { hidden.add(s.key) }, onFailed = { hidden.remove(s.key) })
            }
        }
    }
}

/** The VOD pages' Recommended for you in full: films ([shows] false) or series. */
fun vodRecommendedRoute(shows: Boolean): String = "vodrecommended?kind=" + if (shows) "series" else "movie"

/**
 * Recommended on VOD: rows of films or series picked from this person's own taste (Top picks,
 * Because you love …), with a Movies/Shows switch. Tiles open the title; Not interested is in
 * each tile's long-press menu.
 */
@Composable
fun VodRecommendedScreen(nav: NavHostController, initialShows: Boolean) {
    val marquee = LocalMarquee.current
    var shows by rememberSaveable { mutableStateOf(initialShows) }
    // Switched by hand: the D-pad stays on the switch rather than jumping into the new rows.
    var switched by rememberSaveable { mutableStateOf(false) }
    val changed by VodChanges.version.collectAsState()
    val error = remember(shows) { mutableStateOf<String?>(null) }
    val rows by cachedLoad<List<VodRecommendationRow>>("vodrecommended:$shows", changed) {
        withContext(Dispatchers.IO) {
            runCatching { marquee.livetv.vodRecommendations(if (shows) LivetvApi.KindVodRecommendations.SERIES else LivetvApi.KindVodRecommendations.MOVIE, 30) }
                .onFailure { error.value = serverMessage(it, "Couldn't load recommendations") }.getOrNull()
        }
    }
    val list = rows
    val chipShape = RoundedCornerShape(8.dp)
    LazyColumn(Modifier.fillMaxSize().semantics { contentDescription = "Recommended on VOD" }, contentPadding = PaddingValues(top = 16.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(key = "header") {
            Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Recommended ${if (shows) "shows" else "movies"} on VOD", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                Row(Modifier.semantics { contentDescription = "Movies or shows" }, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(!shows, { if (shows) { shows = false; switched = true } }, { Text("Movies") }, Modifier.focusRing(chipShape))
                    FilterChip(shows, { if (!shows) { shows = true; switched = true } }, { Text("Shows") }, Modifier.focusRing(chipShape))
                }
                Text("Picked from your favourites, ratings and what you watch. Nothing here comes from what other people watch.",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        when {
            list == null && error.value != null -> item(key = "error") {
                Text(error.value ?: "", Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error)
            }
            list == null -> item(key = "loading") { CircularProgressIndicator(Modifier.padding(horizontal = sidePadding)) }
            list.none { r -> r.propertyEntries.any { vodRoute(it) != null } } -> item(key = "empty") {
                Text("Nothing to recommend yet. Watch, rate or heart a few titles and come back.", Modifier.padding(horizontal = sidePadding),
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            else -> {
                val first = list.firstOrNull { r -> r.propertyEntries.any { vodRoute(it) != null } }?.propertyEntries?.firstOrNull { vodRoute(it) != null }?.key
                items(list, key = { "${if (shows) "s" else "m"}:${it.id}" }) { r ->
                    VodSuggestionRow(nav, r.title, r.propertyEntries, autoFocusKey = first.takeIf { !switched })
                }
            }
        }
    }
}
