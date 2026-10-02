package app.marquee.music

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.apis.ItemsApi
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.RadioRequest
import app.marquee.ui.Gold
import app.marquee.ui.LocalMarquee
import app.marquee.ui.LocalMusic
import app.marquee.ui.PosterCard
import app.marquee.ui.Shelf
import app.marquee.ui.focusCard
import app.marquee.ui.focusRing
import app.marquee.ui.initialFocus
import app.marquee.ui.openItem
import app.marquee.ui.sidePadding
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** A mood or style page (MUSIC-18): its radio, albums and a track sampler. */
@Composable
fun MoodStyleScreen(nav: NavHostController, libraryId: Long, kind: String, name: String) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val scope = rememberCoroutineScope()
    val isMood = kind == "mood"
    val req = if (isMood) RadioRequest(RadioRequest.Seed.MOOD, value = name.lowercase(), libraryId = libraryId, limit = 30)
    else RadioRequest(RadioRequest.Seed.GENRE, value = name, libraryId = libraryId, limit = 30)
    var error by remember { mutableStateOf<String?>(null) }
    val tracks by produceState<List<ItemSummary>?>(null) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.music.musicRadio(req).items }.onFailure { error = it.message }.getOrDefault(emptyList()) }
    }
    val albums by produceState(emptyList<ItemSummary>(), tracks) {
        value = if (!isMood) withContext(Dispatchers.IO) {
            runCatching { marquee.items.listLibraryItems(libraryId, ItemType.ALBUM, ItemsApi.SortListLibraryItems._RATING, limit = 40, genre = name).items }.getOrDefault(emptyList())
        } else tracks.orEmpty().filter { it.parentId != null }.distinctBy { it.parentId }
            .map { it.copy(id = it.parentId!!, type = ItemType.ALBUM, title = it.parentTitle ?: it.title, parentTitle = it.artistCredit ?: it.grandparentTitle) }
    }
    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(if (isMood) "MOOD" else "STYLE", style = MaterialTheme.typography.labelMedium, color = Gold)
                Text(name, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                Button({
                    scope.launch {
                        runCatching { music.startRadio(req) }.onFailure { error = it.message }
                    }
                }, Modifier.focusRing().initialFocus(marquee.isTv)) { Icon(Icons.Filled.Radio, null); Text("Play $name Radio") }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        }
        if (albums.isNotEmpty()) item {
            Shelf(if (isMood) "Albums with this feel" else "Albums", albums, sidePadding) { _, a ->
                PosterCard(a, marquee.imageUrl(a.images?.poster, 240), if (marquee.isTv) 130.dp else 110.dp, { openItem(nav, a) })
            }
        }
        val list = tracks
        if (list == null) item { CircularProgressIndicator(Modifier.padding(sidePadding)) }
        else {
            item { Text("Tracks", Modifier.padding(horizontal = sidePadding), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold) }
            itemsIndexed(list, key = { _, t -> t.id }) { i, t ->
                Row(Modifier.fillMaxWidth().focusCard({ music.play(list, i, source = name) }).padding(horizontal = sidePadding, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically) {
                    Text("${i + 1}", Modifier.width(28.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                    Column(Modifier.weight(1f)) {
                        Text(t.title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(listOfNotNull(t.artistCredit ?: t.grandparentTitle, t.parentTitle).joinToString(" · "), maxLines = 1,
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
}
