package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.MuseVideoRequest
import app.marquee.api.models.PlaylistCreate
import app.marquee.api.models.PlaylistKind
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Example Muse prompts for movies and shows (USER-15). */
val museVideoSuggestions = listOf("90s sci-fi with time travel", "feel-good comedy under 90 minutes", "acclaimed war films I haven't seen", "family animation")

/** Muse for movies and shows, as its own page (from Home or a library's sparkle button). */
@Composable
fun MuseVideoScreen(nav: NavHostController, libraryId: Long?) {
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.padding(horizontal = sidePadding, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.AutoAwesome, null, tint = Gold)
            Text("Muse", Modifier.padding(start = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        }
        MuseVideoPanel(nav, libraryId, focusPrompt = true)
    }
}

/**
 * Describe what you want to watch (USER-15): a prompt with example chips; the results grid
 * with how Muse read it, a note while it's still learning the library, and Save as playlist.
 */
@Composable
fun MuseVideoPanel(nav: NavHostController, libraryId: Long?, focusPrompt: Boolean = false) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val keyboard = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    var prompt by rememberSaveable { mutableStateOf("") }
    var asked by rememberSaveable { mutableStateOf("") }
    var busy by rememberSaveable { mutableStateOf(false) }
    var error by rememberSaveable { mutableStateOf<String?>(null) }
    var understood by rememberSaveable { mutableStateOf<String?>(null) }
    var analysed by rememberSaveable { mutableStateOf<Double?>(null) }
    var results by androidx.compose.runtime.remember { mutableStateOf<List<ItemSummary>?>(null) }
    var saved by rememberSaveable { mutableStateOf<Pair<Long, String>?>(null) }
    var saving by rememberSaveable { mutableStateOf(false) }

    fun ask(text: String) {
        val p = text.trim()
        if (p.length < 2 || busy) return
        keyboard?.hide()
        busy = true
        error = null
        saved = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.museVideo(MuseVideoRequest(p, libraryId = libraryId)) } }
                .onSuccess { r -> results = r.items; understood = r.understood; analysed = r.analysed; asked = p }
                .onFailure { error = it.message ?: "Muse couldn't answer that" }
            busy = false
        }
    }
    fun save() {
        val list = results ?: return
        if (saving || list.isEmpty()) return
        saving = true
        scope.launch {
            withContext(Dispatchers.IO) {
                runCatching { marquee.playlists.createPlaylist(PlaylistCreate("Muse: ${asked.take(60)}", PlaylistKind.VIDEO, itemIds = list.map { it.id })) }
            }.onSuccess { saved = it.id to it.title }.onFailure { error = "Couldn't save the playlist: ${it.message}" }
            saving = false
        }
    }

    val min = if (marquee.isTv) 130.dp else 110.dp
    LazyVerticalGrid(GridCells.Adaptive(min), Modifier.fillMaxSize(), contentPadding = PaddingValues(sidePadding),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    OutlinedTextField(
                        prompt, { prompt = it }, Modifier.weight(1f).initialFocus(focusPrompt && marquee.isTv).semantics { testTag = "musePrompt" }, singleLine = true,
                        placeholder = { Text("Describe what you want to watch…") },
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                        keyboardActions = KeyboardActions(onGo = { ask(prompt) }),
                    )
                    Button({ ask(prompt) }, Modifier.focusRing(), enabled = prompt.trim().length >= 2 && !busy) {
                        if (busy) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp) else Text("Ask Muse")
                    }
                }
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp), contentPadding = PaddingValues(vertical = 4.dp)) {
                    items(museVideoSuggestions) { s ->
                        AssistChip({ prompt = s; ask(s) }, { Text(s) }, Modifier.focusRing(RoundedCornerShape(8.dp)))
                    }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                understood?.let { u ->
                    Text(u, Modifier.semantics { contentDescription = "Muse understood: $u" }, style = MaterialTheme.typography.titleSmall, color = Gold)
                }
                analysed?.takeIf { it < 1 }?.let { a ->
                    Text("Muse is still learning your library (${(a * 100).toInt()}%)", style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                val list = results
                if (list != null && list.isEmpty()) Text("Nothing in your library fits that. Try saying it another way.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (!list.isNullOrEmpty()) Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    Text(if (list.size == 1) "1 title" else "${list.size} titles", Modifier.weight(1f), fontWeight = FontWeight.SemiBold)
                    val sv = saved
                    if (sv == null) OutlinedButton(::save, Modifier.focusRing(), enabled = !saving) {
                        Icon(Icons.AutoMirrored.Filled.PlaylistAdd, null)
                        Text("Save as playlist", Modifier.padding(start = 6.dp))
                    } else TextButton({ nav.navigate("playlist/${sv.first}") }, Modifier.focusRing()) { Text("Saved · Open “${sv.second}”") }
                }
            }
        }
        itemsIndexed(results.orEmpty(), key = { _, it -> it.id }) { _, it ->
            PosterCard(it, marquee.imageUrl(it.images?.poster ?: it.images?.thumb, 240), min, { openItem(nav, it) })
        }
    }
}
