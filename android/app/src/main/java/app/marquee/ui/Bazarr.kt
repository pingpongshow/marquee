package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.marquee.api.models.BazarrCandidate
import app.marquee.api.models.BazarrDownloadRequest
import app.marquee.api.models.BazarrPick
import app.marquee.api.models.BazarrStatus
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Languages offered for "Download another language" (ISO 639-1). */
private val commonLanguages = listOf(
    "en" to "English", "es" to "Spanish", "fr" to "French", "de" to "German", "it" to "Italian", "pt" to "Portuguese",
    "nl" to "Dutch", "sv" to "Swedish", "da" to "Danish", "no" to "Norwegian", "fi" to "Finnish", "pl" to "Polish",
    "ru" to "Russian", "tr" to "Turkish", "ar" to "Arabic", "he" to "Hebrew", "hi" to "Hindi", "ja" to "Japanese",
    "ko" to "Korean", "zh" to "Chinese",
)

/**
 * Bazarr subtitles for a movie or episode (META-12): the languages its profile has and still
 * wants, a download for each wanted one or any other language, and a search of every provider.
 * Downloads run in Bazarr's background; onQueued lets the caller refresh the tracks later.
 * Shows nothing unless Bazarr is set up and manages this title.
 */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun BazarrSection(itemId: Long, modifier: Modifier = Modifier, onQueued: () -> Unit = {}, heading: @Composable (String) -> Unit = { BazarrHeading(it) }) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val status by produceState<BazarrStatus?>(null, itemId) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.items.bazarrStatus(itemId) }.getOrNull() }
    }
    var message by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var searching by remember { mutableStateOf(false) }
    var candidates by remember { mutableStateOf<List<BazarrCandidate>?>(null) }
    var languageMenu by remember { mutableStateOf(false) }
    val s = status ?: return
    if (!s.configured || !s.managed) return

    fun queued(what: String) {
        message = "$what is on its way from Bazarr. It appears in the subtitle list in a moment."
        error = null
        onQueued()
    }
    fun download(code: String, name: String, forced: Boolean = false, hi: Boolean = false) {
        if (busy) return
        busy = true
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.items.bazarrDownload(itemId, BazarrDownloadRequest(code, forced, hi)) } }
                .onSuccess { queued("$name${if (forced) " (forced)" else ""}") }.onFailure { error = it.message ?: "Couldn't ask Bazarr" }
            busy = false
        }
    }

    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        heading("Bazarr")
        s.error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
        if (s.languages.isEmpty()) Text("No languages in this title's Bazarr profile.", style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant)
        s.languages.forEach { l ->
            val name = if (l.name.equals(l.code2, ignoreCase = true)) java.util.Locale.forLanguageTag(l.code2).displayLanguage.ifBlank { l.name } else l.name
            val label = name + listOfNotNull(" (forced)".takeIf { l.forced }, " (SDH)".takeIf { l.hi }).joinToString("")
            Row(Modifier.fillMaxWidth().heightIn(min = 44.dp), verticalAlignment = Alignment.CenterVertically) {
                if (l.have) Icon(Icons.Filled.CheckCircle, "Have", Modifier.size(20.dp), tint = Gold)
                else Icon(Icons.Filled.Download, null, Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Column(Modifier.weight(1f).padding(start = 10.dp)) {
                    Text(label)
                    Text(if (l.have) "Downloaded" else "Wanted", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (!l.have) TextButton({ download(l.code2, name, l.forced, l.hi) }, Modifier.focusRing().semantics { contentDescription = "Download $label" },
                    enabled = !busy) { Text("Download") }
            }
        }
        // Wraps in narrow places (a dialog, the player's side panel).
        androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            OutlinedButton({
                if (searching) return@OutlinedButton
                searching = true
                candidates = null
                error = null
                scope.launch {
                    // Bazarr asks every provider: this can take up to a minute.
                    withContext(Dispatchers.IO) { runCatching { marquee.items.bazarrSearch(itemId) } }
                        .onSuccess { candidates = it }.onFailure { error = it.message ?: "The search failed" }
                    searching = false
                }
            }, Modifier.focusRing(), enabled = !searching) {
                Icon(Icons.Filled.Search, null)
                Text("Search all providers", Modifier.padding(start = 6.dp))
            }
            TextButton({ languageMenu = !languageMenu }, Modifier.focusRing(), enabled = !busy) { Text("Another language…") }
        }
        // Any other language, inline (a menu's popup is awkward over the player and on a TV).
        if (languageMenu) androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            commonLanguages.forEach { (code, name) ->
                androidx.compose.material3.AssistChip({ languageMenu = false; download(code, name) }, { Text(name) },
                    Modifier.focusRing(androidx.compose.foundation.shape.RoundedCornerShape(8.dp)), enabled = !busy)
            }
        }
        if (searching) Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            LinearProgressIndicator(Modifier.fillMaxWidth())
            Text("Searching every provider. This can take up to a minute…", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        candidates?.let { list ->
            if (list.isEmpty()) Text("No providers found anything.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            list.forEach { c ->
                Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(c.release?.takeIf { it.isNotBlank() } ?: c.provider, maxLines = 2, fontWeight = FontWeight.Medium)
                        Text(listOfNotNull(c.provider, c.language.uppercase(), "Score ${c.score}", "SDH".takeIf { c.hi == true }, "Forced".takeIf { c.forced == true })
                            .joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    TextButton({
                        if (busy) return@TextButton
                        busy = true
                        scope.launch {
                            withContext(Dispatchers.IO) { runCatching { marquee.items.bazarrPick(itemId, BazarrPick(c.provider, c.subtitle, c.hi, c.forced, c.originalFormat)) } }
                                .onSuccess { queued("The ${c.provider} subtitle") }.onFailure { error = it.message ?: "Couldn't download it" }
                            busy = false
                        }
                    }, Modifier.focusRing().semantics { contentDescription = "Download from ${c.provider}: ${c.release ?: c.subtitle}" }, enabled = !busy) { Text("Download") }
                }
            }
        }
        if (busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        message?.let { Text(it, color = Gold, style = MaterialTheme.typography.bodyMedium) }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
    }
}

@Composable
private fun BazarrHeading(t: String) = Text(t, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)

/** Bazarr for one title in a dialog (Library Health's "Download with Bazarr"). */
@Composable
fun BazarrDialog(itemId: Long, title: String, onClose: () -> Unit) {
    val marquee = LocalMarquee.current
    val status by produceState<BazarrStatus?>(null, itemId) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.items.bazarrStatus(itemId) }.getOrNull() }
    }
    AlertDialog(
        onDismissRequest = onClose,
        title = { Text(title) },
        text = {
            Column(Modifier.heightIn(max = 480.dp).verticalScroll(rememberScrollState())) {
                val s = status
                when {
                    s == null -> CircularProgressIndicator()
                    !s.configured -> Text("Bazarr isn't set up. Add it in Server settings.")
                    !s.managed -> Text(s.error ?: "Bazarr doesn't manage this title.")
                    else -> BazarrSection(itemId)
                }
            }
        },
        confirmButton = { TextButton(onClose, Modifier.focusRing()) { Text("Done") } },
    )
}
