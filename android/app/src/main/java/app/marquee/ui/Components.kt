package app.marquee.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.StarHalf
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.StarBorder
import androidx.compose.material3.Icon
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.stateDescription
import app.marquee.core.WatchState
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import coil3.compose.AsyncImage

enum class Shape(val ratio: Float) { Poster(2f / 3f), Square(1f), Wide(16f / 9f) }

fun shapeFor(item: ItemSummary): Shape = when (item.type) {
    ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK -> Shape.Square
    ItemType.EPISODE, ItemType.VIDEO -> Shape.Wide
    else -> Shape.Poster
}

/**
 * Clickable and, on TV, visibly focused: scales up with a gold border under the D-pad. With
 * [onLongClick] (a context menu): a long press on phones, and a long press of the D-pad's
 * centre or the menu key on TV.
 */
@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
fun Modifier.focusCard(onClick: () -> Unit, shape: RoundedCornerShape = RoundedCornerShape(8.dp), onLongClick: (() -> Unit)? = null): Modifier {
    var focused by remember { mutableStateOf(false) }
    val s by animateFloatAsState(if (focused) 1.06f else 1f, label = "focus")
    var heldCentre by remember { mutableStateOf(false) }
    val base = this
        .scale(s)
        .onFocusChanged { focused = it.isFocused }
        .clip(shape)
        .border(if (focused) 3.dp else 0.dp, if (focused) Gold else Color.Transparent, shape)
    if (onLongClick == null) return base.clickable(onClick = onClick)
    return base
        .onPreviewKeyEvent { e ->
            val k = e.nativeKeyEvent
            val centre = k.keyCode == android.view.KeyEvent.KEYCODE_DPAD_CENTER || k.keyCode == android.view.KeyEvent.KEYCODE_ENTER
            when {
                k.keyCode == android.view.KeyEvent.KEYCODE_MENU -> { if (e.type == KeyEventType.KeyUp) onLongClick(); true }
                // Held: the menu opens once, and letting go doesn't also open the item.
                centre && e.type == KeyEventType.KeyDown && k.repeatCount > 0 -> { if (!heldCentre) { heldCentre = true; onLongClick() }; true }
                centre && e.type == KeyEventType.KeyUp && heldCentre -> { heldCentre = false; true }
                else -> false
            }
        }
        .combinedClickable(onClick = onClick, onLongClick = onLongClick)
}

/** A gold ring around a focused button or chip, for the D-pad (Material's own focus state is too faint on a TV). */
@Composable
fun Modifier.focusRing(shape: androidx.compose.ui.graphics.Shape = RoundedCornerShape(50)): Modifier {
    var focused by remember { mutableStateOf(false) }
    val s by animateFloatAsState(if (focused) 1.06f else 1f, label = "focus")
    return this
        .scale(s)
        .border(if (focused) 2.dp else 0.dp, if (focused) Gold else Color.Transparent, shape)
        .onFocusChanged { focused = it.hasFocus }
}

/** On TV, focuses this element when it first appears: the D-pad needs a starting point. */
@Composable
fun Modifier.initialFocus(enabled: Boolean): Modifier {
    if (!enabled) return this
    val r = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { r.requestFocus() } }
    return focusRequester(r)
}

/** Artwork with a titled placeholder when there's none. */
@Composable
fun Artwork(url: String?, title: String, shape: Shape, modifier: Modifier = Modifier) {
    Box(
        modifier
            .aspectRatio(shape.ratio)
            .clip(RoundedCornerShape(8.dp))
            .background(Brush.linearGradient(listOf(Color(0xFF2A2A36), Color(0xFF15151B)))),
        contentAlignment = Alignment.BottomStart,
    ) {
        Text(title, Modifier.padding(8.dp), style = MaterialTheme.typography.labelMedium, color = Color.White.copy(alpha = 0.8f), maxLines = 3)
        if (url != null) AsyncImage(url, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
    }
}

fun subtitleFor(item: ItemSummary): String = when (item.type) {
    ItemType.SHOW -> if (item.childCount == 1) "1 season" else "${item.childCount} seasons"
    ItemType.SEASON -> if (item.leafCount == 1) "1 episode" else "${item.leafCount} episodes"
    ItemType.ARTIST -> if (item.childCount == 1) "1 album" else "${item.childCount} albums"
    ItemType.EPISODE -> listOfNotNull(item.grandparentTitle, item.index?.let { "E$it" }).joinToString(" · ")
    ItemType.TRACK -> item.artistCredit ?: item.grandparentTitle ?: ""
    ItemType.COLLECTION -> "${item.childCount} titles"
    else -> item.year?.toString() ?: ""
}

/** "4.5" for 9, "4" for 8: a 0–10 rating as stars. */
fun starsLabel(rating: Double): String = "%.1f".format(rating / 2).removeSuffix(".0")

/**
 * The person's rating as five small read-only stars (halves included), for track rows and
 * cards. Nothing when unrated, so rows stay tidy.
 */
@Composable
fun RatingBadge(rating: Double?, modifier: Modifier = Modifier, size: Dp = 12.dp) {
    if (rating == null || rating <= 0) return
    val stars = rating / 2
    Row(modifier.semantics { contentDescription = "Rated ${starsLabel(rating)} stars" }, verticalAlignment = Alignment.CenterVertically) {
        for (star in 1..5) {
            val icon = when {
                stars >= star -> Icons.Filled.Star
                stars >= star - 0.5 -> Icons.AutoMirrored.Filled.StarHalf
                else -> Icons.Filled.StarBorder
            }
            Icon(icon, null, Modifier.size(size), tint = if (stars >= star - 0.5) Gold else MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f))
        }
    }
}

/** One entry of a tile's context menu. */
data class TileAction(val label: String, val run: () -> Unit)

/** A tile's context menu of [actions], while [open]. */
@Composable
fun TileMenu(actions: List<TileAction>, open: Boolean, onDismiss: () -> Unit) {
    if (actions.isEmpty()) return
    DropdownMenu(open, onDismiss) {
        actions.forEach { a -> DropdownMenuItem({ Text(a.label) }, { onDismiss(); a.run() }) }
    }
}

/**
 * A poster with its title underneath. Videos show a watched check or how far they were
 * watched, with this device's marks shown at once (USER-6). [actions] open as a context
 * menu on a long press (TV: hold the D-pad's centre, or the menu key).
 */
@Composable
fun PosterCard(item: ItemSummary, imageUrl: String?, width: Dp, onClick: () -> Unit, shape: Shape = shapeFor(item), autoFocus: Boolean = false,
    showRating: Boolean = item.type == ItemType.TRACK, actions: List<TileAction> = emptyList()) {
    val marquee = LocalMarquee.current
    val marks by marquee.watch.marks.collectAsState()
    var menu by remember { mutableStateOf(false) }
    val video = item.type in WatchState.videoTypes
    val mark = if (video) marquee.watch.markFor(item, marks) else null
    val watched = video && watchedNow(marquee, item, marks)
    val progress = if (!video || watched || mark != null) null
        else item.viewOffsetMs?.takeIf { it > 0 }?.let { off -> item.durationMs?.takeIf { it > 0 }?.let { (off.toFloat() / it).coerceIn(0f, 1f) } }
    Column(Modifier.width(width)) {
        Box {
            Artwork(imageUrl, item.title, shape, Modifier.fillMaxWidth().initialFocus(autoFocus)
                .focusCard(onClick, onLongClick = if (actions.isEmpty()) null else ({ menu = true }))
                .semantics {
                    contentDescription = item.title
                    if (watched) stateDescription = "Watched"
                    if (actions.isNotEmpty()) customActions = actions.map { a -> CustomAccessibilityAction(a.label) { a.run(); true } }
                })
            if (watched) Icon(Icons.Filled.CheckCircle, null, Modifier.align(Alignment.TopEnd).padding(6.dp).size(20.dp)
                .background(Color.Black.copy(alpha = 0.55f), RoundedCornerShape(50)), tint = Gold)
            progress?.let {
                LinearProgressIndicator(progress = { it }, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 6.dp, vertical = 6.dp).height(3.dp),
                    color = Gold, trackColor = Color.White.copy(alpha = 0.3f), drawStopIndicator = {})
            }
            DropdownMenu(menu, { menu = false }) {
                actions.forEach { a -> DropdownMenuItem({ Text(a.label) }, { menu = false; a.run() }) }
            }
        }
        Text(item.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text(subtitleFor(item), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (showRating) RatingBadge(item.userRating, Modifier.padding(top = 2.dp))
    }
}

/** A titled horizontal row of cards (Home hubs, cast, related). */
@Composable
fun <T> Shelf(title: String, items: List<T>, sidePadding: Dp, onTitle: (() -> Unit)? = null, content: @Composable (index: Int, item: T) -> Unit) {
    Column {
        // A title that opens the whole list (a pinned collection or playlist) reads "Title ›".
        if (onTitle != null) Text("$title  ›", Modifier.padding(start = sidePadding - 4.dp, bottom = 4.dp).focusRing(RoundedCornerShape(6.dp))
            .clip(RoundedCornerShape(6.dp)).clickable(onClick = onTitle).padding(horizontal = 4.dp),
            style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        else Text(title, Modifier.padding(start = sidePadding, bottom = 4.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        // Vertical padding leaves room for a focused card's scale and border.
        LazyRow(contentPadding = PaddingValues(horizontal = sidePadding, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            itemsIndexed(items) { i, it -> content(i, it) }
        }
    }
}

fun formatTime(ms: Long): String {
    val s = ms / 1000
    return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
}

/** The summary form of a detail (what lists, queues and downloads keep). */
fun app.marquee.api.models.ItemDetail.summary() = ItemSummary(
    id = id, libraryId = libraryId, type = type, title = title, childCount = childCount, leafCount = leafCount, available = available,
    matchState = ItemSummary.MatchState.entries.firstOrNull { it.value == matchState.value } ?: ItemSummary.MatchState.entries.first(),
    addedAt = addedAt, originalTitle = originalTitle, year = year, index = index, absoluteIndex = absoluteIndex, disc = disc,
    parentId = parentId, grandparentId = grandparentId, parentTitle = parentTitle, grandparentTitle = grandparentTitle,
    artistCredit = artistCredit, durationMs = durationMs, originallyAvailableAt = originallyAvailableAt, images = images,
    viewOffsetMs = viewOffsetMs, viewCount = viewCount, lastViewedAt = lastViewedAt, watchedLeafCount = watchedLeafCount,
    userRating = userRating, watchlisted = watchlisted, communityRating = communityRating, favorite = favorite,
)

/** A playlist's poster mosaic: up to four of its items' posters, or its one poster. */
@Composable
fun PlaylistMosaic(imageIds: List<Long>, title: String, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    if (imageIds.size < 4) { Artwork(marquee.imageUrl(imageIds.firstOrNull(), 200), title, Shape.Square, modifier); return }
    Column(modifier.aspectRatio(1f).clip(RoundedCornerShape(8.dp)).background(Color(0xFF15151B))) {
        imageIds.take(4).chunked(2).forEach { row ->
            Row(Modifier.weight(1f)) {
                row.forEach { id ->
                    AsyncImage(marquee.imageUrl(id, 100), null, contentScale = ContentScale.Crop, modifier = Modifier.weight(1f).fillMaxSize())
                }
            }
        }
    }
}
