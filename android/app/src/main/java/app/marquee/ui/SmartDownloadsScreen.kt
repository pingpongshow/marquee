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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoMode
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.core.Downloads
import app.marquee.core.SmartDownloads
import app.marquee.core.SmartSelection

/** Settings → Downloads: the Smart downloads row (phones and tablets), opening its settings. */
@Composable
fun SmartDownloadsRow(nav: NavHostController) {
    val smart = LocalDownloads.current.smart
    val s by smart.settings.collectAsState()
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("Downloads", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        Row(Modifier.fillMaxWidth().focusCard({ nav.navigate("smartdownloads") }).semantics(mergeDescendants = true) { contentDescription = "Smart downloads" },
            verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.AutoMode, null, Modifier.padding(end = 12.dp), tint = if (s.on) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.weight(1f).padding(end = 12.dp)) {
                Text("Smart downloads")
                Text(summary(s), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Icon(Icons.Filled.ChevronRight, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

private fun summary(s: SmartDownloads.Settings): String = when {
    s.showsOn && s.musicOn -> "Shows and music"
    s.showsOn -> "Shows: the next ${s.episodes} ${if (s.episodes == 1) "episode" else "episodes"}"
    s.musicOn -> "Music: favourites and most played"
    else -> "Off"
}

/**
 * Smart downloads' settings: shows (the next episodes of what you're watching) and music
 * (favourites, then most played), each with its own storage budget; Wi-Fi only and quality.
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
        Text("Video quality", style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Downloads.Quality.entries.forEach { q ->
                FilterChip(s.quality == q, { smart.update { it.copy(quality = q) } }, { Text(q.label) }, Modifier.focusRing(chip))
            }
        }
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
