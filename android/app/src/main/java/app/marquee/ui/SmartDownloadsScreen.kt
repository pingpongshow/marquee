package app.marquee.ui

import android.text.format.Formatter
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoMode
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Movie
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.core.DownloadQuality
import app.marquee.core.Downloads
import app.marquee.core.SmartDownloads
import app.marquee.core.SmartSelection

/**
 * Settings → Downloads (phones and tablets): what's on the device, opening Downloads; the song and
 * video quality new downloads use; and the Smart downloads row, opening its settings.
 */
@Composable
fun DownloadsSettings(nav: NavHostController) {
    val downloads = LocalDownloads.current
    val smart = downloads.smart
    val context = LocalContext.current
    val s by smart.settings.collectAsState()
    val entries by downloads.entries.collectAsState()
    val done = entries.values.count { it.state == Downloads.State.Done }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("Downloads", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        SettingRow(Icons.Filled.DownloadDone, "Downloaded", "$done ${if (done == 1) "item" else "items"} · ${Formatter.formatShortFileSize(context, downloads.totalBytes)}") { nav.navigate("downloads") }
        QualityPicker(DownloadQuality.Kind.Songs, "Song quality", Icons.Filled.MusicNote)
        QualityPicker(DownloadQuality.Kind.Videos, "Video quality", Icons.Filled.Movie)
        SettingRow(Icons.Filled.AutoMode, "Smart downloads", summary(s), if (s.on) Gold else MaterialTheme.colorScheme.onSurfaceVariant) { nav.navigate("smartdownloads") }
    }
}

@Composable
private fun SettingRow(icon: androidx.compose.ui.graphics.vector.ImageVector, title: String, detail: String,
    tint: androidx.compose.ui.graphics.Color = MaterialTheme.colorScheme.onSurfaceVariant, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().focusCard(onClick).semantics(mergeDescendants = true) { contentDescription = title }, verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, null, Modifier.padding(end = 12.dp), tint = tint)
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(title)
            Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Icon(Icons.Filled.ChevronRight, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The quality new downloads of a kind use: Original to Low with a size guide, one marked Recommended (and used until another is chosen). */
@Composable
private fun QualityPicker(kind: DownloadQuality.Kind, title: String, icon: androidx.compose.ui.graphics.vector.ImageVector) {
    val downloads = LocalDownloads.current
    val chosen by (if (kind == DownloadQuality.Kind.Songs) downloads.songQuality else downloads.videoQuality).collectAsState()
    var open by remember { mutableStateOf(false) }
    // Follows the free storage, so it's looked up on opening the page and the picker.
    val recommended = remember(open) { downloads.recommendedQuality(kind) }
    val current = chosen ?: recommended
    SettingRow(icon, title, DownloadQuality.label(kind, current) + if (current == recommended) " · Recommended" else "") { open = true }
    if (open) AlertDialog(onDismissRequest = { open = false },
        title = { Text(title) },
        text = {
            Column(Modifier.selectableGroup()) {
                Downloads.Quality.entries.forEach { q ->
                    Row(Modifier.fillMaxWidth().selectable(current == q, role = Role.RadioButton) {
                        downloads.setQuality(kind, q)
                        open = false
                    }.focusRing(RoundedCornerShape(8.dp)).padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(current == q, null)
                        Column(Modifier.padding(start = 12.dp)) {
                            Text(DownloadQuality.label(kind, q) + if (q == recommended) " · Recommended" else "", fontWeight = if (q == recommended) FontWeight.SemiBold else null)
                            Text(DownloadQuality.sizeGuide(kind, q), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                Text("Applies to new downloads. What's already on this device stays as it is.", Modifier.padding(top = 8.dp),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = { TextButton({ open = false }, Modifier.focusRing()) { Text("Cancel") } })
}

private fun summary(s: SmartDownloads.Settings): String = when {
    s.showsOn && s.musicOn -> "Shows and music"
    s.showsOn -> "Shows: the next ${s.episodes} ${if (s.episodes == 1) "episode" else "episodes"}"
    s.musicOn -> "Music: favourites and most played"
    else -> "Off"
}

/**
 * Smart downloads' settings: shows (the next episodes of what you're watching) and music
 * (favourites, then most played), each with its own storage budget; Wi-Fi only. The quality is
 * the one set for all downloads.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SmartDownloadsScreen(nav: NavHostController) {
    val downloads = LocalDownloads.current
    val smart = downloads.smart
    val context = LocalContext.current
    val s by smart.settings.collectAsState()
    val entries by downloads.entries.collectAsState()
    val running by smart.running.collectAsState()
    val lastRun by smart.lastRun.collectAsState()
    val lastError by smart.lastError.collectAsState()
    fun size(kind: Downloads.SmartKind) = Formatter.formatShortFileSize(context, entries.values.filter { it.smart == kind }.sumOf { it.size })
    fun count(kind: Downloads.SmartKind) = entries.values.count { it.smart == kind }
    val chip = RoundedCornerShape(8.dp)

    Column(Modifier.verticalScroll(rememberScrollState()).padding(sidePadding), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text("Smart downloads", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Text("Keeps what you'll want next on this device, so it plays without the server. Downloads you make yourself are never removed.",
            color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)

        // Shows
        SwitchRow("Shows", "The next unwatched episodes of shows you're watching. Watched episodes are removed. Turning this off removes them.",
            s.showsOn) { on -> smart.update { it.copy(showsOn = on) } }
        if (s.showsOn) {
            Text("Episodes ahead", style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SmartSelection.episodeCounts.forEach { n ->
                    FilterChip(s.episodes == n, { smart.update { it.copy(episodes = n) } }, { Text("$n") }, Modifier.focusRing(chip))
                }
            }
            Budget(s.showsBudgetGb) { gb -> smart.update { it.copy(showsBudgetGb = gb) } }
            Text("${count(Downloads.SmartKind.Shows)} episodes · ${size(Downloads.SmartKind.Shows)}", color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall)
        }
        HorizontalDivider()

        // Music
        SwitchRow("Music", "Your favourite songs, albums and artists, then what you play most. Turning this off removes them.",
            s.musicOn) { on -> smart.update { it.copy(musicOn = on) } }
        if (s.musicOn) {
            Budget(s.musicBudgetGb) { gb -> smart.update { it.copy(musicBudgetGb = gb) } }
            Text("${count(Downloads.SmartKind.Music)} tracks · ${size(Downloads.SmartKind.Music)}", color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall)
        }
        HorizontalDivider()

        // Options
        SwitchRow("Wi-Fi only", "Download only on Wi-Fi (an unmetered network), never on mobile data.", s.wifiOnly) { on -> smart.update { it.copy(wifiOnly = on) } }
        Text("Downloads at the song and video quality set in Settings.", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        if (s.on) {
            Text(when {
                running -> "Updating…"
                lastRun != null -> "Updated " + android.text.format.DateUtils.getRelativeTimeSpanString(lastRun!!)
                else -> "Updates soon, and every few hours" + if (s.wifiOnly) " on Wi-Fi" else ""
            }, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            lastError?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (s.on) OutlinedButton({ smart.runSoon(delaySeconds = 0) }, Modifier.focusRing(), enabled = !running) { Text("Update now") }
            OutlinedButton({ nav.navigate("downloads") }, Modifier.focusRing()) { Text("Open Downloads") }
        }
    }
}

@Composable
private fun SwitchRow(title: String, help: String, on: Boolean, onChange: (Boolean) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(title, fontWeight = FontWeight.Medium)
            Text(help, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Switch(on, onChange, Modifier.focusRing().semantics { contentDescription = title })
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Budget(gb: Int, onChange: (Int) -> Unit) {
    Text("Storage", style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SmartSelection.budgets.forEach { n ->
            FilterChip(gb == n, { onChange(n) }, { Text("$n GB") }, Modifier.focusRing(RoundedCornerShape(8.dp)))
        }
    }
}
