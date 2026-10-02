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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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

/** Clickable and, on TV, visibly focused: scales up with a gold border under the D-pad. */
@Composable
fun Modifier.focusCard(onClick: () -> Unit, shape: RoundedCornerShape = RoundedCornerShape(8.dp)): Modifier {
    var focused by remember { mutableStateOf(false) }
    val s by animateFloatAsState(if (focused) 1.06f else 1f, label = "focus")
    return this
        .scale(s)
        .onFocusChanged { focused = it.isFocused }
        .clip(shape)
        .border(if (focused) 3.dp else 0.dp, if (focused) Gold else Color.Transparent, shape)
        .clickable(onClick = onClick)
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

/** A poster with its title underneath. */
@Composable
fun PosterCard(item: ItemSummary, imageUrl: String?, width: Dp, onClick: () -> Unit, shape: Shape = shapeFor(item), autoFocus: Boolean = false) {
    Column(Modifier.width(width)) {
        Artwork(imageUrl, item.title, shape, Modifier.fillMaxWidth().initialFocus(autoFocus).focusCard(onClick).semantics { contentDescription = item.title })
        Text(item.title, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
        Text(subtitleFor(item), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** A titled horizontal row of cards (Home hubs, cast, related). */
@Composable
fun <T> Shelf(title: String, items: List<T>, sidePadding: Dp, content: @Composable (index: Int, item: T) -> Unit) {
    Column {
        Text(title, Modifier.padding(start = sidePadding, bottom = 4.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
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
