package app.marquee.ui

import android.widget.Toast
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.StarHalf
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.StarBorder
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.marquee.api.models.ItemDetail
import app.marquee.api.models.ItemType
import app.marquee.api.models.MeUpdate
import app.marquee.core.Marquee
import app.marquee.core.RatingRequest
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** "Show · S01E02 · Title" for an episode, the title for a movie (USER-19). */
fun ratingTitle(d: ItemDetail): String {
    if (d.type != ItemType.EPISODE) return d.title
    val season = d.parentTitle?.let { Regex("\\d+").find(it)?.value?.toIntOrNull() }
    val code = when {
        season != null && d.index != null -> "S%02dE%02d".format(season, d.index)
        d.index != null -> "E%02d".format(d.index)
        else -> null
    }
    return listOfNotNull(d.grandparentTitle, code, d.title).joinToString(" · ")
}

fun ratingRequestFor(marquee: Marquee, d: ItemDetail) = RatingRequest(
    d.id, ratingTitle(d),
    marquee.imageUrl(if (d.type == ItemType.EPISODE) d.images?.thumb ?: d.images?.poster else d.images?.poster ?: d.images?.thumb, 120),
)

/**
 * The small card asking for a rating once a movie or episode has been watched (USER-19):
 * artwork, title, five stars with halves, Not now and Don't ask again. Stars save at once,
 * thank the person and close the card. On a TV, left/right choose and OK saves.
 * [onInteract] says the person is using it (the Up Next countdown waits).
 */
@Composable
fun RatingPromptCard(req: RatingRequest, onDone: () -> Unit, modifier: Modifier = Modifier, onInteract: () -> Unit = {}) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var thanks by remember(req.itemId) { mutableStateOf(false) }
    var busy by remember(req.itemId) { mutableStateOf(false) }
    LaunchedEffect(thanks) { if (thanks) { delay(1500); onDone() } }
    BackHandler(onBack = onDone)
    fun rate(v: Double) {
        if (busy || thanks) return
        onInteract()
        busy = true
        scope.launch {
            val ok = marquee.sync.saveShowing(marquee.sync.rating(req.itemId, v), context)
            busy = false
            if (ok) thanks = true else Toast.makeText(context, "Couldn't save the rating", Toast.LENGTH_SHORT).show()
        }
    }
    fun dontAsk() {
        val u = marquee.me.value
        onDone()
        if (u == null) return
        marquee.scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.auth.updateMe(MeUpdate(preferences = u.preferences.copy(ratingPrompt = false))) } }
                .onSuccess { withContext(Dispatchers.Main) { marquee.updated(it) } }
        }
    }
    Row(
        modifier.widthIn(max = 520.dp).clip(RoundedCornerShape(16.dp)).background(Surface2.copy(alpha = 0.97f))
            .semantics { contentDescription = "Rating request" }.padding(12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(56.dp).height(84.dp).clip(RoundedCornerShape(8.dp)).background(Surface1)) {
            if (req.imageUrl != null) AsyncImage(req.imageUrl, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
        }
        Column(Modifier.weight(1f, fill = false)) {
            Text(if (thanks) "Thanks!" else "How was it?", style = MaterialTheme.typography.labelLarge, color = Gold)
            Text(req.title, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis, color = Color.White)
            if (!thanks) {
                PromptStars(onPreview = onInteract, onPick = ::rate)
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    TextButton(onDone, Modifier.focusRing()) { Text("Not now") }
                    TextButton(::dontAsk, Modifier.focusRing()) { Text("Don't ask again") }
                }
            }
        }
    }
}

/**
 * Five stars with halves. Touch: tap a half to rate. TV: left/right move the choice and OK
 * saves it. Values are 0–10 (half-stars odd), as the rest of the app uses.
 */
@Composable
private fun PromptStars(onPreview: () -> Unit, onPick: (Double) -> Unit) {
    val marquee = LocalMarquee.current
    var pending by remember { mutableStateOf(0.0) }
    val stars = pending / 2
    val size = if (marquee.isTv) 30 else 28
    Row(
        Modifier.semantics {
            contentDescription = "Your rating"
            stateDescription = if (pending <= 0) "Not rated" else "${"%.1f".format(stars).removeSuffix(".0")} of 5 stars"
        }
            .focusRing(RoundedCornerShape(8.dp))
            .onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionRight -> { pending = (pending + 1).coerceAtMost(10.0); onPreview(); true }
                    Key.DirectionLeft -> if (pending > 0) { pending -= 1; onPreview(); true } else false
                    Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> { if (pending > 0) onPick(pending); true }
                    else -> false
                }
            }
            .initialFocus(marquee.isTv)
            .focusable(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        for (star in 1..5) {
            val icon = when {
                stars >= star -> Icons.Filled.Star
                stars >= star - 0.5 -> Icons.AutoMirrored.Filled.StarHalf
                else -> Icons.Filled.StarBorder
            }
            Box(Modifier.size((size + 12).dp), contentAlignment = Alignment.Center) {
                Icon(icon, null, Modifier.size(size.dp), tint = if (stars >= star - 0.5) Gold else Color.White.copy(alpha = 0.7f))
                Row(Modifier.matchParentSize()) {
                    for ((half, value) in listOf("${star - 1}.5 stars" to star * 2.0 - 1, "$star star${if (star == 1) "" else "s"}" to star * 2.0)) {
                        Box(Modifier.weight(1f).fillMaxHeight().focusProperties { canFocus = false }
                            .semantics { contentDescription = half }
                            .clickable(remember { MutableInteractionSource() }, indication = null) { pending = value; onPick(value) })
                    }
                }
            }
        }
    }
}

/**
 * Shows a rating request over the item page or home after the player closed at the end
 * (USER-19), for a few seconds or until dismissed; longer once the person starts using it.
 */
@Composable
fun RatingPromptHost(visible: Boolean, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    val req by marquee.ratingRequest.collectAsState()
    val r = req ?: return
    if (!visible) return
    var interacted by remember(r) { mutableStateOf(false) }
    LaunchedEffect(r, interacted) {
        delay(if (interacted) 30_000 else if (marquee.isTv) 15_000 else 10_000)
        if (marquee.ratingRequest.value == r) marquee.requestRating(null)
    }
    RatingPromptCard(r, onDone = { if (marquee.ratingRequest.value == r) marquee.requestRating(null) }, modifier = modifier.padding(16.dp),
        onInteract = { interacted = true })
}
