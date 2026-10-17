package app.marquee.ui

import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import app.marquee.api.models.ItemSummary
import app.marquee.core.FavoriteState
import app.marquee.core.Marquee
import app.marquee.core.PendingChange
import kotlinx.coroutines.launch

/** Whether an item is a favourite: this device's latest change, then one queued offline, then the server's value. */
fun favoriteNow(marquee: Marquee, id: Long, server: Boolean?, marks: Map<Long, Boolean> = marquee.favorites.marks.value): Boolean =
    marks[id] ?: marquee.sync.queued(id, PendingChange.Kind.Favorite)?.on ?: (server == true)

@Composable
fun isFavorite(id: Long, server: Boolean?): Boolean {
    val marquee = LocalMarquee.current
    val marks by marquee.favorites.marks.collectAsState()
    return favoriteNow(marquee, id, server, marks)
}

/** Sets or clears a favourite in the app's scope (leaving the screen doesn't cancel it); a refusal reverts with a toast. */
fun toggleFavorite(marquee: Marquee, context: android.content.Context, id: Long, title: String, on: Boolean) {
    marquee.scope.launch {
        if (!marquee.sync.setFavorite(id, on)) toast(context, if (on) "Couldn't add “$title” to favourites" else "Couldn't remove “$title” from favourites")
    }
}

/**
 * The heart (USER-12): filled when the item is one of the person's favourites. The change shows
 * at once everywhere and reverts if the server refuses it; offline it's queued and sent later.
 * [small] is for track and episode rows.
 */
@Composable
fun FavoriteButton(id: Long, title: String, server: Boolean?, modifier: Modifier = Modifier, small: Boolean = false) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val on = isFavorite(id, server)
    IconButton({ toggleFavorite(marquee, context, id, title, !on) },
        modifier.focusRing().then(if (small) Modifier.size(36.dp) else Modifier).semantics { stateDescription = if (on) "Favourite" else "Not a favourite" }) {
        Icon(if (on) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
            if (on) "Remove $title from favourites" else "Add $title to favourites",
            Modifier.size(if (small) 18.dp else 24.dp),
            tint = if (on) Gold else MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = if (small) 0.6f else 1f))
    }
}

@Composable
fun FavoriteButton(item: ItemSummary, modifier: Modifier = Modifier, small: Boolean = false) =
    FavoriteButton(item.id, item.title, item.favorite, modifier, small)

/** "Add to favourites" / "Remove from favourites" for a menu, or null when the item can't be a favourite. */
@Composable
fun favoriteAction(item: ItemSummary): TileAction? {
    if (item.type !in FavoriteState.types) return null
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val on = isFavorite(item.id, item.favorite)
    return TileAction(if (on) "Remove from favourites" else "Add to favourites") { toggleFavorite(marquee, context, item.id, item.title, !on) }
}
