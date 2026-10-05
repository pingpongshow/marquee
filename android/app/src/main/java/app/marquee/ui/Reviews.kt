package app.marquee.ui

import android.text.format.DateUtils
import android.widget.Toast
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.marquee.api.models.CommunityRating
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.ItemReviews
import app.marquee.api.models.ItemType
import app.marquee.music.RatingStars
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Item types people rate and comment on (USER-17). */
fun reviewable(t: ItemType) = t in listOf(ItemType.MOVIE, ItemType.SHOW, ItemType.EPISODE, ItemType.VIDEO, ItemType.ALBUM, ItemType.ARTIST, ItemType.TRACK)

/** "4.2" for an average of 8.4 out of 10. */
fun averageLabel(average: Double) = "%.1f".format(average / 2)

/** Everyone's ratings combined, compact: "★ 4.2 (7)". Nothing when nobody rated it. */
@Composable
fun CommunityScore(rating: CommunityRating?, modifier: Modifier = Modifier) {
    if (rating == null || rating.count <= 0) return
    Row(modifier.semantics(mergeDescendants = true) {
        contentDescription = "Community rating ${averageLabel(rating.average)} from ${rating.count} rating${if (rating.count == 1) "" else "s"}"
    }, verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.Star, null, Modifier.size(12.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(" ${averageLabel(rating.average)} (${rating.count})", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * The item page's ratings: yours (tap to rate, half stars) and the community's (read-only),
 * which opens Ratings & comments.
 */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun ItemRatings(d: ItemDetail, community: CommunityRating?, onRated: () -> Unit, onOpen: () -> Unit) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var rating by remember(d.id, d.userRating) {
        mutableStateOf(marquee.sync.queued(d.id, app.marquee.core.PendingChange.Kind.Rating)?.let { it.rating } ?: d.userRating?.takeIf { it > 0 })
    }
    fun rate(r: Double?) {
        val before = rating
        rating = r
        music.ratingChanged(d.id, r)
        scope.launch {
            val ok = marquee.sync.saveShowing(marquee.sync.rating(d.id, r), context)
            if (!ok) {
                rating = before
                music.ratingChanged(d.id, before)
                Toast.makeText(context, "Couldn't save the rating", Toast.LENGTH_SHORT).show()
            } else onRated()
        }
    }
    val label = MaterialTheme.typography.labelSmall
    FlowRow(horizontalArrangement = Arrangement.spacedBy(16.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("YOUR RATING", style = label, color = MaterialTheme.colorScheme.onSurfaceVariant)
            RatingStars(rating, ::rate, size = if (marquee.isTv) 20 else 22)
        }
        if (reviewable(d.type)) Row(
            Modifier.clip(RoundedCornerShape(8.dp)).focusRing(RoundedCornerShape(8.dp)).clickable(onClick = onOpen)
                .semantics(mergeDescendants = true) {
                    role = Role.Button
                    contentDescription = if (community != null && community.count > 0)
                        "Community rating ${averageLabel(community.average)}, ${community.count} rating${if (community.count == 1) "" else "s"}. Ratings & comments"
                    else "Not rated by others yet. Ratings & comments"
                }
                .padding(vertical = 8.dp, horizontal = 4.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text("COMMUNITY", style = label, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (community != null && community.count > 0) {
                RatingBadge(Math.round(community.average).toDouble(), size = 14.dp)
                Text("${averageLabel(community.average)} · ${community.count} rating${if (community.count == 1) "" else "s"}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else Text("Not rated yet", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** Everyone's ratings and comments, and a box for yours. Admins can remove anyone's comment. */
@Composable
fun ReviewsSection(d: ItemDetail, reviews: ItemReviews?, isAdmin: Boolean, onChanged: () -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val mine = reviews?.reviews?.firstOrNull { it.mine }
    var draft by remember(d.id, mine?.comment) { mutableStateOf(mine?.comment ?: "") }
    var busy by remember { mutableStateOf(false) }
    // Saved straight away, or kept on the device and sent later when the server can't be reached (USER-18).
    fun run(what: String, change: app.marquee.core.PendingChange) {
        busy = true
        scope.launch {
            val r = runCatching { marquee.sync.save(change) }
            busy = false
            r.onSuccess {
                if (it != app.marquee.core.OfflineSync.Result.Queued) onChanged() // a queued comment syncs later, quietly
            }.onFailure { Toast.makeText(context, it.message ?: "Couldn't $what", Toast.LENGTH_LONG).show() }
        }
    }
    Column(Modifier.padding(horizontal = sidePadding).semantics { contentDescription = "Ratings & comments" }, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Ratings & comments", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            reviews?.average?.let { avg ->
                Text("  ★ ${averageLabel(avg)} · ${reviews.count} rating${if (reviews.count == 1) "" else "s"}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        OutlinedTextField(draft, { if (it.length <= 2000) draft = it }, Modifier.fillMaxWidth().semantics { contentDescription = "Your comment" },
            label = { Text("Your comment") }, placeholder = { Text("What did you think?") }, minLines = 2, maxLines = 6)
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            val changed = draft.trim() != (mine?.comment ?: "")
            Button({ run("save the comment", marquee.sync.comment(d.id, draft.trim())) }, Modifier.focusRing(),
                enabled = !busy && changed && draft.isNotBlank()) { Text(if (mine?.comment != null) "Save" else "Post") }
            if (mine?.comment != null) OutlinedButton({ run("delete the comment", marquee.sync.comment(d.id, "")) }, Modifier.focusRing(), enabled = !busy) {
                Icon(Icons.Filled.Delete, null); Text("Delete", Modifier.padding(start = 6.dp))
            }
        }
        val all = reviews?.reviews.orEmpty()
        if (reviews != null && all.none { !it.mine }) Text("No one else has rated this yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        all.forEach { r ->
            HorizontalDivider()
            Row(Modifier.fillMaxWidth().semantics(mergeDescendants = true) {}, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Avatar(r.userName, marquee.absolute(r.avatarUrl), 36)
                Column(Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(r.userName, fontWeight = FontWeight.Medium)
                        if (r.mine) Text("You", style = MaterialTheme.typography.labelSmall, color = Gold)
                        RatingBadge(r.rating)
                    }
                    Text(DateUtils.getRelativeTimeSpanString(r.updatedAt.toInstant().toEpochMilli()).toString(),
                        style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    r.comment?.let { Text(it, Modifier.padding(top = 4.dp), style = MaterialTheme.typography.bodyMedium) }
                }
                if (isAdmin && !r.mine && r.comment != null) Box {
                    IconButton({ run("delete the comment", marquee.sync.deleteComment(d.id, r.userId)) }, Modifier.focusRing()) {
                        Icon(Icons.Filled.Delete, "Delete ${r.userName}'s comment")
                    }
                }
            }
        }
    }
}
