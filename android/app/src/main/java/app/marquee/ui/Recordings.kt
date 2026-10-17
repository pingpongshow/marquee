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
import androidx.compose.foundation.border
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
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
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.apis.LivetvApi
import app.marquee.api.models.DvrUsage
import app.marquee.api.models.LiveChannel
import app.marquee.api.models.LiveProgramme
import app.marquee.api.models.Recording
import app.marquee.api.models.RecordingRule
import app.marquee.api.models.ScheduleRecordingRequest
import app.marquee.api.models.StopRecordingRequest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.Duration
import java.time.OffsetDateTime
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
        // Recording now: Stop and keep / Stop and delete, for whoever may.
        if (!theirs && e != null && e.status == Recording.Status.RECORDING) StopButtons(e, onDone) { error = it }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (theirs || e?.status == Recording.Status.RECORDING) Unit
            else if (e != null) {
                OutlinedButton({ act { marquee.livetv.cancelRecording(e.id) } }, Modifier.focusRing()) { Text("Don't record") }
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

/** "Stop recording X and delete what has been recorded so far?" */
private fun stopDeleteConfirm(title: String) = "Stop recording $title and delete what has been recorded so far?"

/** "12 min in · 48 min left" for a recording in progress. */
fun recordingProgress(r: Recording, now: OffsetDateTime = OffsetDateTime.now()): String {
    fun fmt(d: Duration): String {
        val m = Math.round(d.toMillis() / 60_000.0).coerceAtLeast(0)
        return if (m >= 60) "${m / 60} h ${m % 60} min" else "$m min"
    }
    val left = Duration.between(now, r.end)
    return "${fmt(Duration.between(r.start, now))} in · " + if (!left.isNegative && !left.isZero) "${fmt(left)} left" else "finishing"
}

/**
 * Stop and keep / Stop and delete for a recording in progress (owner or admin; LIVE-5). Stop and
 * delete asks first. [onDone] after it stopped; [onError] with the server's message.
 */
@Composable
fun StopButtons(r: Recording, onDone: () -> Unit, onError: (String) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var busy by remember(r.id) { mutableStateOf(false) }
    var confirming by remember(r.id) { mutableStateOf(false) }
    fun stop(keep: Boolean) {
        if (busy) return
        busy = true
        scope.launch {
            runCatching { withContext(Dispatchers.IO) { marquee.livetv.stopRecording(r.id, StopRecordingRequest(keep)) } }
                .onSuccess { onDone() }.onFailure { onError(serverMessage(it)) }
            busy = false
        }
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedButton({ stop(true) }, Modifier.focusRing(), enabled = !busy) { Text("Stop and keep") }
        OutlinedButton({ confirming = true }, Modifier.focusRing(), enabled = !busy) { Text("Stop and delete", color = MaterialTheme.colorScheme.error) }
    }
    if (confirming) AlertDialog(
        onDismissRequest = { confirming = false },
        title = { Text("Stop and delete?") },
        text = { Text(stopDeleteConfirm(r.title)) },
        confirmButton = { TextButton({ confirming = false; stop(false) }, Modifier.focusRing()) { Text("Stop and delete", color = MaterialTheme.colorScheme.error) } },
        dismissButton = { TextButton({ confirming = false }, Modifier.focusRing()) { Text("Cancel") } },
    )
}

/**
 * Recordings in progress, at the top of every Live TV tab: channel, title, time in and left, and
 * Stop and keep / Stop and delete for whoever may. Reloads when [reload] changes and every 30
 * seconds; [onActive] with how many are recording, after each load; [onChanged] after a recording was stopped.
 */
@Composable
fun RecordingNowStrip(reload: Int, onActive: (Int) -> Unit = {}, onChanged: () -> Unit) {
    val marquee = LocalMarquee.current
    var active by remember { mutableStateOf<List<Recording>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var now by remember { mutableStateOf(OffsetDateTime.now()) }
    LaunchedEffect(reload) {
        while (true) {
            withContext(Dispatchers.IO) { runCatching { marquee.livetv.listRecordings() } }
                .onSuccess { l -> active = l.filter { it.status == Recording.Status.RECORDING }; onActive(active.size) }
            now = OffsetDateTime.now()
            delay(30_000)
        }
    }
    if (active.isEmpty()) return
    Column(Modifier.padding(horizontal = sidePadding).fillMaxWidth().clip(RoundedCornerShape(10.dp))
        .border(1.dp, Color(0xFFE53935).copy(alpha = 0.4f), RoundedCornerShape(10.dp)).background(Surface2).padding(12.dp)
        .semantics { contentDescription = "Recording now" },
        verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.padding(end = 8.dp).size(9.dp).clip(CircleShape).background(Color(0xFFE53935)))
            Text("Recording now", style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold)
        }
        active.forEach { r ->
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(r.title, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text((listOf(r.channelName, recordingProgress(r, now)) + listOfNotNull(r.ownerName?.takeIf { it.isNotBlank() })).joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
                if (r.canDelete != false) StopButtons(r, onDone = { error = null; onChanged() }) { error = it }
            }
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
    }
}

/** "1:02:03" or "4:05". */
private fun clockTime(ms: Long): String {
    val t = ms / 1000
    return if (t >= 3600) String.format(java.util.Locale.US, "%d:%02d:%02d", t / 3600, t / 60 % 60, t % 60)
    else String.format(java.util.Locale.US, "%d:%02d", t / 60, t % 60)
}

/** A finished recording's progress: a bar while part-watched, or "Watched". */
@Composable
private fun WatchProgress(r: Recording) {
    val offset = r.viewOffsetMs ?: 0
    val total = r.durationMs ?: 0
    if (offset > 0 && total > 0) Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Box(Modifier.weight(1f).height(4.dp).clip(CircleShape).background(Color.White.copy(alpha = 0.12f)).semantics { contentDescription = "Watched so far" }) {
            Box(Modifier.fillMaxWidth((offset.toFloat() / total).coerceIn(0f, 1f)).height(4.dp).clip(CircleShape).background(Gold))
        }
        Text("${clockTime(offset)} of ${clockTime(total)}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    } else if (r.watched == true) Text("Watched", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** All recordings or only the signed-in person's, remembered on this device per profile. */
private fun ownerKey(userId: Long?) = "recordings.owner.${userId ?: ""}"

/** Recording now, upcoming, series and finished recordings: everyone's (or only mine), as the server allows. */
@Composable
fun RecordingsList(nav: NavHostController, canRecord: Boolean, reloadKey: Int = 0, onChanged: () -> Unit = {}) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = remember { context.getSharedPreferences("marquee.livetv", android.content.Context.MODE_PRIVATE) }
    val me = marquee.userId
    var onlyMine by remember(me) { mutableStateOf(runCatching { prefs.getString(ownerKey(me), "all") == "me" }.getOrDefault(false)) }
    var recordings by remember { mutableStateOf<List<Recording>?>(null) }
    var rules by remember { mutableStateOf<List<RecordingRule>>(emptyList()) }
    var reload by remember { mutableIntStateOf(0) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Recording?>(null) }
    var usage by remember { mutableStateOf<DvrUsage?>(null) }
    LaunchedEffect(reload, reloadKey, onlyMine) {
        withContext(Dispatchers.IO) {
            val r = if (onlyMine) LivetvApi.OwnerListRecordings.ME else LivetvApi.OwnerListRecordings.ALL
            val s = if (onlyMine) LivetvApi.OwnerListRecordingRules.ME else LivetvApi.OwnerListRecordingRules.ALL
            runCatching { marquee.livetv.listRecordings(r) to marquee.livetv.listRecordingRules(s) }
                .onSuccess { (r, s) -> recordings = r; rules = s; error = null }
                .onFailure { error = serverMessage(it); if (recordings == null) recordings = emptyList() }
            if (canRecord) usage = runCatching { marquee.livetv.getRecordingUsage() }.getOrNull()
        }
    }
    fun act(f: suspend () -> Unit) = scope.launch {
        runCatching { withContext(Dispatchers.IO) { f() } }.onFailure { error = serverMessage(it) }
        reload++
    }
    fun setOnlyMine(on: Boolean) {
        onlyMine = on
        runCatching { prefs.edit().putString(ownerKey(me), if (on) "me" else "all").apply() }
    }
    // Only whoever scheduled a recording or series, or an admin, may cancel or delete it.
    fun mine(canDelete: Boolean?) = canDelete != false
    val list = recordings ?: return
    Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(!onlyMine, { setOnlyMine(false) }, { Text("All recordings") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
            FilterChip(onlyMine, { setOnlyMine(true) }, { Text("Only mine") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
        }
        usage?.takeIf { canRecord }?.let { UsageLine(it) }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (list.isEmpty() && rules.isEmpty()) Text(when {
            onlyMine -> "You haven't recorded anything yet."
            canRecord -> "Nothing recorded yet. Pick a programme in the guide and choose Record."
            else -> "Nothing recorded yet."
        }, color = MaterialTheme.colorScheme.onSurfaceVariant)

        @Composable
        fun section(title: String, items: List<Recording>, below: @Composable (Recording) -> Unit = {}, action: @Composable (Recording) -> Unit) {
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
                                listOfNotNull(r.sizeBytes?.takeIf { it > 0 }?.let { Formatter.formatShortFileSize(context, it) },
                                    "Stopped early".takeIf { r.stoppedEarly == true })
                            Text(bits.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            recordedBy(r.userId, r.ownerName, me)?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            if (r.status == Recording.Status.RECORDING) Text(recordingProgress(r), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            if (r.status == Recording.Status.FAILED) Text("Failed: ${r.error ?: "unknown error"}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                            if (r.status == Recording.Status.COMPLETED && r.itemId != null) WatchProgress(r)
                            below(r)
                        }
                        action(r)
                    }
                }
            }
        }

        section("Recording now", list.filter { it.status == Recording.Status.RECORDING }, below = { r ->
            if (mine(r.canDelete)) Box(Modifier.padding(top = 8.dp)) { StopButtons(r, onDone = { reload++; onChanged() }) { error = it } }
        }) { }
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
        section("Recorded", list.filter { it.status == Recording.Status.COMPLETED || it.status == Recording.Status.FAILED }, below = { r ->
            // The player picks up where this profile left off.
            val id = r.itemId
            if (r.status == Recording.Status.COMPLETED && id != null) Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button({ nav.navigate("player/$id") }, Modifier.focusRing()) { Text(if ((r.viewOffsetMs ?: 0) > 0) "Resume" else "Play") }
                if (mine(r.canDelete)) TextButton({ deleting = r }, Modifier.focusRing()) { Text("Delete", color = MaterialTheme.colorScheme.error) }
            }
        }) { r ->
            if (!mine(r.canDelete)) Unit
            else if (r.status == Recording.Status.FAILED) TextButton({ act { marquee.livetv.cancelRecording(r.id) } }, Modifier.focusRing()) { Text("Dismiss") }
            else if (r.itemId == null) TextButton({ deleting = r }, Modifier.focusRing()) { Text("Delete", color = MaterialTheme.colorScheme.error) }
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
