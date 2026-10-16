package app.marquee.ui

import android.text.format.Formatter
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import app.marquee.api.models.DvrUsage
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
    // A 409 quota_exceeded (and other refusals) carry the server's own message.
    fun act(f: suspend () -> Unit) = scope.launch {
        runCatching { withContext(Dispatchers.IO) { f() } }.onSuccess { onDone() }.onFailure { error = serverMessage(it) }
    }
    if (!loaded) return
    val e = existing
    // Someone else's recording: say who, and leave it to them (or an admin) to cancel.
    val theirs = e != null && e.canDelete == false
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (p.series == true) Text("A series recording covers this title.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (theirs) Text((if (e?.status == Recording.Status.RECORDING) "Being recorded" else "Will be recorded") + (e?.ownerName?.let { " by $it" } ?: "") + ".",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (theirs) Unit
            else if (e != null) {
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

/** "Recorded by Alex", or null for the signed-in user's own. */
fun recordedBy(ownerId: Long?, ownerName: String?, me: Long?): String? =
    ownerName?.takeIf { it.isNotBlank() && (ownerId == null || ownerId != me) }?.let { "Recorded by $it" }

/** Bytes as GB for the usage line: "12.4", or "50" when whole. */
private fun gb(bytes: Long): String {
    val v = bytes / 1e9
    return if (v >= 100 || kotlin.math.abs(v - Math.round(v)) < 0.05) Math.round(v).toString() else String.format(java.util.Locale.US, "%.1f", v)
}

/** The signed-in person's recording space: "12.4 of 50 GB used" with a thin bar, and the over-quota note. */
@Composable
private fun UsageLine(u: DvrUsage) {
    val quota = u.quotaBytes
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(if (quota > 0) "${gb(u.usedBytes)} of ${gb(quota)} GB used" else "${gb(u.usedBytes)} GB used",
            style = MaterialTheme.typography.bodyMedium, color = if (u.overQuota) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
        if (quota > 0) {
            val frac = (u.usedBytes.toFloat() / quota).coerceIn(0f, 1f)
            Box(Modifier.fillMaxWidth().height(4.dp).clip(CircleShape).background(Surface2)) {
                Box(Modifier.fillMaxWidth(frac).height(4.dp).clip(CircleShape)
                    .background(if (u.overQuota) MaterialTheme.colorScheme.error else if (frac > 0.85f) Color(0xFFF59E0B) else Gold))
            }
        }
        if (u.overQuota) Text(
            if (u.quotaAction == DvrUsage.QuotaAction.DELETE_OLDEST) "You're over your recording space; your oldest watched recordings will be removed to make room."
            else "You're over your recording space; new recordings won't start until you delete some.",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
    }
}

/** Recording now, upcoming, series and finished recordings: everyone's, as the server allows. */
@Composable
fun RecordingsList(nav: NavHostController, canRecord: Boolean) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var recordings by remember { mutableStateOf<List<Recording>?>(null) }
    var rules by remember { mutableStateOf<List<RecordingRule>>(emptyList()) }
    var reload by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Recording?>(null) }
    var usage by remember { mutableStateOf<DvrUsage?>(null) }
    val me = marquee.userId
    LaunchedEffect(reload) {
        withContext(Dispatchers.IO) {
            runCatching { marquee.livetv.listRecordings() to marquee.livetv.listRecordingRules() }
                .onSuccess { (r, s) -> recordings = r; rules = s; error = null }
                .onFailure { error = serverMessage(it); if (recordings == null) recordings = emptyList() }
            if (canRecord) usage = runCatching { marquee.livetv.getRecordingUsage() }.getOrNull()
        }
    }
    fun act(f: suspend () -> Unit) = scope.launch {
        runCatching { withContext(Dispatchers.IO) { f() } }.onFailure { error = serverMessage(it) }
        reload++
    }
    // Only whoever scheduled a recording or series, or an admin, may cancel or delete it.
    fun mine(canDelete: Boolean?) = canDelete != false
    val list = recordings ?: return
    Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        usage?.takeIf { canRecord }?.let { UsageLine(it) }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (list.isEmpty() && rules.isEmpty()) Text(if (canRecord) "Nothing recorded yet. Pick a programme in the guide and choose Record." else "Nothing recorded yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)

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
                            recordedBy(r.userId, r.ownerName, me)?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            if (r.status == Recording.Status.FAILED) Text("Failed: ${r.error ?: "unknown error"}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                        }
                        action(r)
                    }
                }
            }
        }

        section("Recording now", list.filter { it.status == Recording.Status.RECORDING }) { r ->
            if (mine(r.canDelete)) TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Stop") }
        }
        section("Upcoming", list.filter { it.status == Recording.Status.SCHEDULED }) { r ->
            if (mine(r.canDelete)) TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Cancel") }
        }
        if (rules.isNotEmpty()) Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Series", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            rules.forEach { s ->
                Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Surface2).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(s.title, fontWeight = FontWeight.Medium)
                        Text("${s.channelName ?: "Any channel"} · ${s.upcoming} upcoming", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        recordedBy(s.userId, s.ownerName, me)?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        if (s.paused == true) Text("Paused: ${s.ownerName ?: "its owner"} may no longer record", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                    }
                    if (mine(s.canDelete)) TextButton({ act { marquee.livetv.deleteRecordingRule(s.id) } }, Modifier.focusRing()) { Text("Stop series") }
                }
            }
        }
        section("Recorded", list.filter { it.status == Recording.Status.COMPLETED || it.status == Recording.Status.FAILED }) { r ->
            if (!mine(r.canDelete)) Unit
            else if (r.status == Recording.Status.FAILED) TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Dismiss") }
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
