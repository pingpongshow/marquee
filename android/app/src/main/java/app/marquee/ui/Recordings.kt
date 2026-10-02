package app.marquee.ui

import android.text.format.Formatter
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.models.LiveChannel
import app.marquee.api.models.LiveProgramme
import app.marquee.api.models.Recording
import app.marquee.api.models.RecordingRule
import app.marquee.api.models.ScheduleRecordingRequest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.ZoneId
import java.time.format.DateTimeFormatter

// DVR (LIVE-5): Record buttons for a guide programme, and the Recordings tab.

/** Record, record the series, or cancel; [onDone] after a change. */
@Composable
fun RecordActions(p: LiveProgramme, c: LiveChannel, onDone: () -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var existing by remember { mutableStateOf<Recording?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(p.id) {
        existing = withContext(Dispatchers.IO) {
            runCatching { marquee.livetv.listRecordings() }.getOrDefault(emptyList()).firstOrNull {
                it.channelId == c.id && it.start.isEqual(p.start) && (it.status == Recording.Status.SCHEDULED || it.status == Recording.Status.RECORDING)
            }
        }
        loaded = true
    }
    fun act(f: suspend () -> Unit) = scope.launch {
        runCatching { withContext(Dispatchers.IO) { f() } }.onSuccess { onDone() }.onFailure { error = it.message ?: "That didn't work" }
    }
    if (!loaded) return
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (p.series == true) Text("A series recording covers this title.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val e = existing
            if (e != null) {
                OutlinedButton({ act { marquee.livetv.cancelRecording(e.id) } }, Modifier.focusRing()) {
                    Text(if (e.status == Recording.Status.RECORDING) "Stop recording" else "Don't record")
                }
            } else {
                OutlinedButton({ act { marquee.livetv.scheduleRecording(ScheduleRecordingRequest(c.id, p.start)) } }, Modifier.focusRing()) { Text("Record") }
            }
            if (p.series != true) OutlinedButton({ act { marquee.livetv.scheduleRecording(ScheduleRecordingRequest(c.id, p.start, series = true, anyChannel = true)) } },
                Modifier.focusRing()) { Text("Record series") }
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
    }
}

private val whenFormat = DateTimeFormatter.ofPattern("EEE MMM d, h:mm a")
private val clockFormat = DateTimeFormatter.ofPattern("h:mm a")

/** Recording now, upcoming, series and finished recordings. */
@Composable
fun RecordingsList(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var recordings by remember { mutableStateOf<List<Recording>?>(null) }
    var rules by remember { mutableStateOf<List<RecordingRule>>(emptyList()) }
    var reload by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Recording?>(null) }
    LaunchedEffect(reload) {
        withContext(Dispatchers.IO) {
            runCatching { marquee.livetv.listRecordings() to marquee.livetv.listRecordingRules() }
                .onSuccess { (r, s) -> recordings = r; rules = s; error = null }
                .onFailure { error = it.message; if (recordings == null) recordings = emptyList() }
        }
    }
    fun act(f: suspend () -> Unit) = scope.launch {
        runCatching { withContext(Dispatchers.IO) { f() } }.onFailure { error = it.message }
        reload++
    }
    val list = recordings ?: return
    Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (list.isEmpty() && rules.isEmpty()) Text("Nothing recorded yet. Pick a programme in the guide and choose Record.", color = MaterialTheme.colorScheme.onSurfaceVariant)

        @Composable
        fun section(title: String, items: List<Recording>, action: @Composable (Recording) -> Unit) {
            if (items.isEmpty()) return
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                items.forEach { r ->
                    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Surface2).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        if (r.status == Recording.Status.RECORDING) Box(Modifier.padding(end = 10.dp).size(9.dp).clip(CircleShape).background(Color(0xFFE53935)))
                        Column(Modifier.weight(1f)) {
                            val open = r.itemId?.let { id -> { nav.navigate("item/$id") } }
                            Text(r.title, fontWeight = FontWeight.Medium, color = if (open != null) Gold else Color.Unspecified,
                                modifier = if (open != null) Modifier.focusCard(open) else Modifier)
                            val bits = listOfNotNull(r.episode, r.subtitle) + r.channelName +
                                "${r.start.atZoneSameInstant(ZoneId.systemDefault()).format(whenFormat)}–${r.end.atZoneSameInstant(ZoneId.systemDefault()).format(clockFormat)}" +
                                listOfNotNull(r.sizeBytes?.takeIf { it > 0 }?.let { Formatter.formatShortFileSize(context, it) })
                            Text(bits.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            if (r.status == Recording.Status.FAILED) Text("Failed: ${r.error ?: "unknown error"}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                        }
                        action(r)
                    }
                }
            }
        }

        section("Recording now", list.filter { it.status == Recording.Status.RECORDING }) { r ->
            TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Stop") }
        }
        section("Upcoming", list.filter { it.status == Recording.Status.SCHEDULED }) { r ->
            TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Cancel") }
        }
        if (rules.isNotEmpty()) Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Series", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            rules.forEach { s ->
                Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Surface2).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(s.title, fontWeight = FontWeight.Medium)
                        Text("${s.channelName ?: "Any channel"} · ${s.upcoming} upcoming", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    TextButton({ act { marquee.livetv.deleteRecordingRule(s.id) } }, Modifier.focusRing()) { Text("Stop series") }
                }
            }
        }
        section("Recorded", list.filter { it.status == Recording.Status.COMPLETED || it.status == Recording.Status.FAILED }) { r ->
            if (r.status == Recording.Status.FAILED) TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Dismiss") }
            else TextButton({ deleting = r }, Modifier.focusRing()) { Text("Delete", color = MaterialTheme.colorScheme.error) }
        }
    }
    deleting?.let { r ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text("Delete this recording?") },
            text = { Text("${r.title} will be removed from the library.") },
            confirmButton = { TextButton({ deleting = null; act { marquee.livetv.cancelRecording(r.id, deleteFile = true) } }, Modifier.focusRing()) { Text("Delete") } },
            dismissButton = { TextButton({ deleting = null }, Modifier.focusRing()) { Text("Cancel") } },
        )
    }
}
