package app.marquee.music

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.MusicJourneyRequest
import app.marquee.ui.Artwork
import app.marquee.ui.LocalMarquee
import app.marquee.ui.LocalMusic
import app.marquee.ui.Shape
import app.marquee.ui.focusRing
import app.marquee.ui.serverMessage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Sound Journey (MUSIC-4): pick a destination track and travel there through music that sounds in between. */
@Composable
fun SoundJourneyDialog(fromId: Long, fromTitle: String, onDone: () -> Unit) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val scope = rememberCoroutineScope()
    var query by remember { mutableStateOf("") }
    var tracks by remember { mutableStateOf(emptyList<ItemSummary>()) }
    var searching by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(query) {
        if (query.trim().length < 2) { tracks = emptyList(); return@LaunchedEffect }
        delay(250)
        searching = true
        tracks = withContext(Dispatchers.IO) {
            runCatching { marquee.search.search(query.trim(), 15).groups.firstOrNull { it.type == ItemType.TRACK }?.items.orEmpty() }.getOrDefault(emptyList())
        }.filter { it.id != fromId }
        searching = false
    }
    fun go(to: ItemSummary) {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.music.musicJourney(MusicJourneyRequest(fromId, to.id, 15)) } }
                .onSuccess { st ->
                    if (st.items.isEmpty()) error = "No journey between those two yet."
                    else { music.playStation(st); onDone() }
                }
                .onFailure { error = serverMessage(it) }
            busy = false
        }
    }
    AlertDialog(
        onDismissRequest = onDone,
        title = { Text("Sound Journey") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("From “$fromTitle”, travel to any track through music that sounds in between. Where to?",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth(), singleLine = true, label = { Text("Destination track") })
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (searching || busy) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                Column(Modifier.heightIn(max = 320.dp).verticalScroll(rememberScrollState())) {
                    tracks.forEach { t ->
                        Row(Modifier.fillMaxWidth().clickable(enabled = !busy) { go(t) }.focusRing().padding(vertical = 8.dp)
                            .semantics { contentDescription = "Journey to ${t.title}" },
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            Artwork(marquee.imageUrl(t.images?.poster, 80), t.title, Shape.Square, Modifier.width(40.dp))
                            Column(Modifier.weight(1f)) {
                                Text(t.title, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.Medium)
                                (t.artistCredit ?: t.grandparentTitle)?.let {
                                    Text(it, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodySmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                            }
                        }
                    }
                }
            }
        },
        confirmButton = { TextButton(onDone, Modifier.focusRing()) { Text("Cancel") } },
    )
}
