package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.marquee.api.models.Stats
import app.marquee.api.models.StatsCount
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Your Stats (ADM-4, the "year in music"): what you've watched and listened to. */
@Composable
fun StatsScreen() {
    val marquee = LocalMarquee.current
    var days by remember { mutableIntStateOf(30) }
    val stats by produceState<Result<Stats>?>(null, days) {
        value = null
        value = withContext(Dispatchers.IO) { runCatching { marquee.activity.getStats(days, null, 10) } }
    }
    LazyColumn(contentPadding = PaddingValues(sidePadding), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item { Text("Your Stats", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        item {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(7 to "7 days", 30 to "30 days", 365 to "Year", 0 to "All time").forEach { (d, label) ->
                    FilterChip(days == d, { days = d }, { Text(label) }, Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(marquee.isTv && d == 30))
                }
            }
        }
        val s = stats
        when {
            s == null -> item { CircularProgressIndicator() }
            s.isFailure -> item { Text(s.exceptionOrNull()?.message ?: "Couldn't load", color = MaterialTheme.colorScheme.error) }
            else -> {
                val st = s.getOrThrow()
                if (st.plays == 0) item { Text("Nothing played in this period.", color = MaterialTheme.colorScheme.onSurfaceVariant) }
                else {
                    item {
                        Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                            Figure("Plays", st.plays.toString())
                            Figure("Watched", hours(st.videoHours))
                            Figure("Listened", hours(st.musicHours))
                        }
                    }
                    top("Top artists", st.artists)
                    top("Top albums", st.albums)
                    top("Top tracks", st.tracks)
                    top("Top movies", st.movies)
                    top("Top shows", st.shows)
                }
            }
        }
    }
}

private fun hours(h: Double) = if (h < 1) "${(h * 60).toInt()} min" else "%.1f h".format(h)

@Composable
private fun Figure(label: String, value: String) {
    androidx.compose.foundation.layout.Column {
        Text(value, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold, color = Gold)
        Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.top(title: String, list: List<StatsCount>) {
    if (list.isEmpty()) return
    item { Text(title, Modifier.padding(top = 12.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
    items(list.withIndex().toList()) { (i, c) ->
        Row(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
            Text("${i + 1}", Modifier.width(28.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(c.title, Modifier.weight(1f), maxLines = 1)
            Text(if (c.plays == 1) "1 play" else "${c.plays} plays", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
