package app.marquee.music

import androidx.compose.ui.text.style.TextOverflow

import androidx.compose.material3.TextButton

import androidx.compose.material3.OutlinedButton

import androidx.compose.material.icons.automirrored.filled.PlaylistAdd

import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.Key
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.focusable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.RecordVoiceOver
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Bedtime
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material.icons.filled.StarBorder
import androidx.compose.material.icons.automirrored.filled.StarHalf
import androidx.compose.material.icons.filled.Waves
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.models.ItemType
import app.marquee.api.models.Lyrics
import app.marquee.api.models.MusicMuseRequest
import app.marquee.api.models.MusicStatus
import app.marquee.api.models.RadioRequest
import app.marquee.api.models.Station
import app.marquee.ui.Gold
import app.marquee.ui.LocalMarquee
import app.marquee.ui.LocalMusic
import app.marquee.ui.focusRing
import app.marquee.ui.initialFocus
import app.marquee.ui.sidePadding
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The moods offered as stations (same as the web and Apple apps). */
val musicMoods = listOf("Chill", "Energetic", "Focus", "Melancholy", "Party", "Romantic", "Dreamy", "Aggressive")

/** Example Muse prompts. */
val museSuggestions = listOf("Rainy Sunday jazz", "Late-night drive synthwave", "Upbeat 80s pop for cleaning the house", "Acoustic songs for a quiet evening")

/** What the music home and the Muse page need from the server: analysis status, mixes and the library's decades and styles. */
class MusicDiscoverData(val status: MusicStatus?, val mixes: List<Station>, val decades: List<String>, val styles: List<String> = emptyList()) {
    /** Soundprint is on, so radios, Muse, moods and mixes work. */
    val enabled get() = status?.enabled == true
}

@Composable
fun rememberMusicDiscoverData(libraryId: Long): MusicDiscoverData? {
    val marquee = LocalMarquee.current
    val data by produceState<MusicDiscoverData?>(null, libraryId) {
        value = withContext(Dispatchers.IO) {
            val st = runCatching { marquee.music.musicStatus() }.getOrNull()
            if (st?.enabled != true) MusicDiscoverData(st, emptyList(), emptyList())
            else {
                val f = runCatching { marquee.items.libraryFilters(libraryId, ItemType.ALBUM) }.getOrNull()
                MusicDiscoverData(
                    st,
                    runCatching { marquee.music.musicMixes(libraryId) }.getOrDefault(emptyList()),
                    f?.decades?.map { it.value }?.take(6).orEmpty(),
                    f?.genres?.sortedByDescending { it.count }?.map { it.value }?.take(18).orEmpty(),
                )
            }
        }
    }
    return data
}

/** Muse and the stations (M6.5): the Muse page opened from the music home. */
@Composable
fun MusicDiscover(libraryId: Long, d: MusicDiscoverData, onOpenPlaylist: ((Long) -> Unit)? = null) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val scope = rememberCoroutineScope()
    var prompt by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    // The last Muse mix started here, to save as a playlist; then the saved playlist.
    var mix by remember { mutableStateOf<Station?>(null) }
    var savingMix by remember { mutableStateOf(false) }
    var savedMix by remember { mutableStateOf<app.marquee.api.models.Playlist?>(null) }
    val st = d.status
    if (st == null || !st.enabled) {
        Text("Muse and stations need the Soundprint analysis service, which isn't turned on.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        return
    }

    fun run(work: suspend () -> Unit) {
        if (busy) return
        busy = true
        error = null
        scope.launch {
            runCatching { work() }.onFailure { error = it.message ?: "Couldn't start that" }
            busy = false
        }
    }
    fun muse(text: String) {
        val p = text.trim()
        if (p.length < 2) return
        run {
            val station = withContext(Dispatchers.IO) { marquee.music.musicMuse(MusicMuseRequest(p, 40, libraryId)) }
            if (station.items.isEmpty()) throw IllegalStateException("Muse found nothing for that.")
            music.playStation(station)
            mix = station
            savedMix = null
        }
    }
    fun radio(req: RadioRequest) = run { music.startRadio(req) }

    Column(Modifier.padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
        AnalysisNote(st)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }

        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Filled.AutoAwesome, null, tint = Gold)
                Text("Muse", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                OutlinedTextField(
                    prompt, { prompt = it }, Modifier.weight(1f).initialFocus(marquee.isTv), singleLine = true,
                    placeholder = { Text("Describe what you want to hear…") },
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                    keyboardActions = KeyboardActions(onGo = { muse(prompt) }),
                )
                Button({ muse(prompt) }, Modifier.focusRing(), enabled = prompt.trim().length >= 2 && !busy) {
                    if (busy) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp) else Text("Play")
                }
            }
            ChipRow(museSuggestions.map { s -> Chip(s, null) { prompt = s; muse(s) } })
            mix?.let { m ->
                val sv = savedMix
                if (sv == null) OutlinedButton({ savingMix = true }, Modifier.focusRing()) {
                    Icon(Icons.AutoMirrored.Filled.PlaylistAdd, null)
                    Text("Save “${m.title}” as playlist", Modifier.padding(start = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis)
                } else TextButton({ onOpenPlaylist?.invoke(sv.id) }, Modifier.focusRing(), enabled = onOpenPlaylist != null) { Text("Saved · Open “${sv.title}”") }
                if (savingMix) SaveAsPlaylistDialog(defaultPlaylistTitle(m.title), m.items.map { it.id }) { pl ->
                    savingMix = false
                    if (pl != null) savedMix = pl
                }
            }
        }

        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("Stations", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            ChipRow(
                listOf(
                    Chip("Library Radio", Icons.Filled.Radio) { radio(RadioRequest(RadioRequest.Seed.LIBRARY, libraryId = libraryId)) },
                    Chip("Favourites Radio", Icons.Filled.Favorite) { radio(RadioRequest(RadioRequest.Seed.FAVOURITES, libraryId = libraryId)) },
                ) + musicMoods.map { m -> Chip(m, Icons.Filled.Waves) { radio(RadioRequest(RadioRequest.Seed.MOOD, value = m.lowercase(), libraryId = libraryId)) } } +
                    d.decades.map { dec -> Chip("${dec}s", Icons.Filled.Radio) { radio(RadioRequest(RadioRequest.Seed.DECADE, value = dec, libraryId = libraryId)) } },
            )
        }
    }
}

/** How far Soundprint has got, while it's still listening (or that it isn't running). */
@Composable
fun AnalysisNote(st: MusicStatus) {
    if (st.analyzed < st.total) Text(
        if (st.available) "Listening to your music: ${st.analyzed} of ${st.total} tracks analysed. Radios and mixes improve as it goes."
        else "The Soundprint analysis service isn't running, so radios and Muse are unavailable.",
        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** The daily mixes as one row; each plays its mix. */
@Composable
fun MixesRow(mixes: List<Station>, contentPadding: PaddingValues = PaddingValues(vertical = 6.dp)) {
    val music = LocalMusic.current
    LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp), contentPadding = contentPadding) {
        itemsIndexed(mixes) { i, m -> MixCard(m, i) { music.playStation(m) } }
    }
}

/** Moods and styles (MUSIC-18): each opens a page with its radio and music. */
@Composable
fun MoodStyleTiles(styles: List<String>, contentPadding: PaddingValues, onBrowse: (kind: String, name: String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
        TileRow("Moods", musicMoods, contentPadding) { onBrowse("mood", it) }
        if (styles.isNotEmpty()) TileRow("Styles", styles, contentPadding) { onBrowse("style", it) }
    }
}

private class Chip(val label: String, val icon: ImageVector?, val focus: Boolean = false, val onClick: () -> Unit)

@Composable
private fun ChipRow(chips: List<Chip>) {
    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp), contentPadding = PaddingValues(vertical = 4.dp)) {
        items(chips) { c ->
            AssistChip(
                onClick = c.onClick,
                label = { Text(c.label) },
                leadingIcon = c.icon?.let { { Icon(it, null, Modifier.size(AssistChipDefaults.IconSize)) } },
                modifier = Modifier.focusRing(RoundedCornerShape(8.dp)).initialFocus(c.focus),
            )
        }
    }
}

private val mixColors = listOf(Color(0xFF7C4DFF), Color(0xFF00897B), Color(0xFFE65100), Color(0xFF1E88E5), Color(0xFFC2185B), Color(0xFF558B2F))

/** A daily mix: a tinted square with its name; plays the mix. */
@Composable
private fun MixCard(station: Station, index: Int, onPlay: () -> Unit) {
    val tint = mixColors[index % mixColors.size]
    Column(Modifier.width(140.dp)) {
        Box(
            Modifier.fillMaxWidth().aspectRatio(1f).focusRing(RoundedCornerShape(10.dp)).clip(RoundedCornerShape(10.dp))
                .background(Brush.linearGradient(listOf(tint, tint.copy(alpha = 0.35f))))
                .semantics { contentDescription = "Play ${station.title}" }
                .clickable(onClick = onPlay),
            contentAlignment = Alignment.BottomStart,
        ) {
            Icon(Icons.Filled.Headphones, null, Modifier.align(Alignment.TopEnd).padding(10.dp), tint = Color.White.copy(alpha = 0.8f))
            Text(station.title, Modifier.padding(10.dp), color = Color.White, fontWeight = FontWeight.Bold, maxLines = 2)
        }
        station.description?.let { Text(it, Modifier.padding(top = 6.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2) }
    }
}

/**
 * Star rating in half stars (MUSIC-11): the left half of a star gives a half star; choosing
 * the current value again clears it. With a D-pad, left/right on the stars move in halves.
 */
@Composable
fun RatingStars(rating: Double?, onRate: (Double?) -> Unit, size: Int = 26) {
    val stars = (rating ?: 0.0) / 2
    val state = if (rating == null) "Not rated" else "${"%.1f".format(stars).removeSuffix(".0")} of 5 stars"
    fun pick(v: Double) = onRate(if (v == rating || v <= 0) null else v.coerceAtMost(10.0))
    Row(
        Modifier.semantics { contentDescription = "Rating"; stateDescription = state }
            .focusRing(RoundedCornerShape(8.dp))
            .onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionRight -> { pick((rating ?: 0.0) + 1); true }
                    Key.DirectionLeft -> { if ((rating ?: 0.0) > 0) { pick((rating ?: 0.0) - 1); true } else false }
                    else -> false
                }
            }
            .focusable(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        for (star in 1..5) {
            val icon = when {
                stars >= star -> Icons.Filled.Star
                stars >= star - 0.5 -> Icons.AutoMirrored.Filled.StarHalf
                else -> Icons.Filled.StarBorder
            }
            Box(Modifier.size((size + 14).dp), contentAlignment = Alignment.Center) {
                Icon(icon, null, Modifier.size(size.dp), tint = if (stars >= star - 0.5) Gold else MaterialTheme.colorScheme.onSurfaceVariant)
                Row(Modifier.matchParentSize()) {
                    Box(Modifier.weight(1f).fillMaxHeight().semantics { contentDescription = "${star - 1}.5 stars" }.clickable { pick(star * 2.0 - 1) })
                    Box(Modifier.weight(1f).fillMaxHeight().semantics { contentDescription = "$star star${if (star == 1) "" else "s"}" }.clickable { pick(star * 2.0) })
                }
            }
        }
    }
}

/**
 * A track row's rating you can change right in the row: the small stars when rated, or a faint
 * outline star when not. Tapping opens a small popover of half-star stars; choosing shows at once,
 * saves, and reverts if the server refuses (choosing the current value again clears it).
 */
@Composable
fun TrackRating(track: app.marquee.api.models.ItemSummary, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    val context = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    // A rating made offline and not yet sent shows over the list's older value (USER-18).
    var rating by remember(track.id, track.userRating) {
        mutableStateOf(marquee.sync.queued(track.id, app.marquee.core.PendingChange.Kind.Rating)?.let { it.rating } ?: track.userRating?.takeIf { it > 0 })
    }
    var open by remember { mutableStateOf(false) }
    val music = LocalMusic.current
    fun rate(r: Double?) {
        val before = rating
        rating = r
        open = false
        music.ratingChanged(track.id, r)
        scope.launch {
            val ok = marquee.sync.saveShowing(marquee.sync.rating(track.id, r), context)
            if (!ok) {
                rating = before
                music.ratingChanged(track.id, before)
                android.widget.Toast.makeText(context, "Couldn't save the rating", android.widget.Toast.LENGTH_SHORT).show()
            }
        }
    }
    Box(modifier) {
        val r = rating
        Box(
            Modifier.clip(RoundedCornerShape(6.dp)).focusRing(RoundedCornerShape(6.dp)).clickable { open = true }
                .semantics(mergeDescendants = true) {
                    contentDescription = if (r != null) "Rated ${app.marquee.ui.starsLabel(r)} stars" else "Rate ${track.title}"
                    role = androidx.compose.ui.semantics.Role.Button
                }
                .padding(horizontal = 6.dp, vertical = 10.dp),
            contentAlignment = Alignment.Center,
        ) {
            if (r != null) app.marquee.ui.RatingBadge(r)
            else Icon(Icons.Filled.StarBorder, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.45f))
        }
        DropdownMenu(open, { open = false }) {
            Box(Modifier.padding(horizontal = 8.dp).semantics { contentDescription = "Rate ${track.title}" }) {
                RatingStars(rating, ::rate, size = 24)
            }
        }
    }
}

/** Lyrics for a track, following along when they're synced. */
@Composable
fun LyricsPanel(trackId: Long, positionMs: Long, onSeek: (Long) -> Unit, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    val lyrics by produceState<Result<Lyrics?>?>(null, trackId) {
        value = withContext(Dispatchers.IO) {
            // 404: the track has no lyrics.
            runCatching<Lyrics?> { marquee.music.getLyrics(trackId) }.recoverCatching { e -> if ((e as? ClientException)?.statusCode == 404) null else throw e }
        }
    }
    val l = lyrics
    when {
        l == null -> Box(modifier, contentAlignment = Alignment.Center) { CircularProgressIndicator() }
        l.getOrNull() == null -> Box(modifier, contentAlignment = Alignment.Center) {
            Text("No lyrics for this track.", color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        else -> {
            val ly = l.getOrNull()!!
            val active = if (ly.synced) ly.lines.indexOfLast { it.timeMs != null && it.timeMs!! <= positionMs + 150 } else -1
            val state = rememberLazyListState()
            LaunchedEffect(active) { if (active >= 0) state.animateScrollToItem((active - 2).coerceAtLeast(0)) }
            LazyColumn(modifier, state = state, verticalArrangement = Arrangement.spacedBy(14.dp), contentPadding = PaddingValues(vertical = 24.dp)) {
                itemsIndexed(ly.lines) { i, line ->
                    val alpha by animateFloatAsState(if (!ly.synced || i == active) 1f else 0.45f, label = "lyric")
                    Text(
                        line.text.ifBlank { "♪" },
                        Modifier.fillMaxWidth().then(if (line.timeMs != null) Modifier.focusRing(RoundedCornerShape(6.dp)).clickable { onSeek(line.timeMs!!) } else Modifier),
                        style = if (ly.synced) MaterialTheme.typography.headlineSmall else MaterialTheme.typography.bodyLarge,
                        fontWeight = if (i == active) FontWeight.Bold else FontWeight.Medium,
                        color = Color.White.copy(alpha = alpha),
                    )
                }
            }
        }
    }
}

private val sleepMinutes = listOf(15, 30, 45, 60, 90)

/** Sleep timer and DJ choices for Now Playing. */
@Composable
fun SleepButton() {
    val music = LocalMusic.current
    val sleep by music.sleep.collectAsState()
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton({ open = true }, Modifier.focusRing()) {
            Icon(Icons.Filled.Bedtime, if (sleep == null) "Sleep timer" else "Sleep timer on", tint = if (sleep == null) MaterialTheme.colorScheme.onSurfaceVariant else Gold)
        }
        DropdownMenu(open, { open = false }) {
            sleepMinutes.forEach { m ->
                DropdownMenuItem({ Text("$m minutes") }, { music.setSleep(MusicController.Sleep.At(System.currentTimeMillis() + m * 60_000L)); open = false })
            }
            DropdownMenuItem({ Text("End of track") }, { music.setSleep(MusicController.Sleep.EndOfTrack); open = false })
            if (sleep != null) DropdownMenuItem({ Text("Turn off", color = MaterialTheme.colorScheme.error) }, { music.setSleep(null); open = false })
        }
    }
}

@Composable
fun DJButton() {
    val music = LocalMusic.current
    val dj by music.dj.collectAsState()
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton({ open = true }, Modifier.focusRing()) {
            Icon(Icons.Filled.RecordVoiceOver, dj?.label ?: "DJ", tint = if (dj == null) MaterialTheme.colorScheme.onSurfaceVariant else Gold)
        }
        DropdownMenu(open, { open = false }) {
            DropdownMenuItem({ Text("DJ off") }, { music.setDJ(null); open = false })
            MusicController.DJ.entries.forEach { d ->
                DropdownMenuItem(
                    { Column { Text(d.label, fontWeight = if (d == dj) FontWeight.Bold else FontWeight.Normal); Text(d.blurb, style = MaterialTheme.typography.bodySmall) } },
                    { music.setDJ(d); open = false },
                )
            }
        }
    }
}

@Composable
private fun TileRow(title: String, names: List<String>, contentPadding: PaddingValues, onOpen: (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text(title, Modifier.padding(start = contentPadding.calculateLeftPadding(androidx.compose.ui.unit.LayoutDirection.Ltr)),
            style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp), contentPadding = contentPadding) {
            items(names) { n ->
                val h = (n.fold(7) { a, c -> (a * 37 + c.code) % 360 }).toFloat()
                val tint = Color.hsv(h, 0.6f, 0.55f)
                Box(
                    Modifier.size(width = 130.dp, height = 72.dp).focusRing(RoundedCornerShape(10.dp)).clip(RoundedCornerShape(10.dp))
                        .background(Brush.linearGradient(listOf(tint, tint.copy(alpha = 0.4f)))).clickable { onOpen(n) }
                        .semantics { contentDescription = "$n ${title.lowercase().removeSuffix("s")}" }.padding(10.dp),
                    contentAlignment = Alignment.BottomStart,
                ) { Text(n, color = Color.White, fontWeight = FontWeight.SemiBold, maxLines = 2) }
            }
        }
    }
}

/** Crossfade: off, or 2–12 seconds (albums played in order stay gapless). */
@Composable
fun CrossfadeButton() {
    val music = LocalMusic.current
    val secs by music.crossfade.collectAsState()
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton({ open = true }, Modifier.focusRing()) {
            Icon(Icons.Filled.SwapHoriz, if (secs == 0) "Crossfade off" else "Crossfade $secs seconds", tint = if (secs == 0) MaterialTheme.colorScheme.onSurfaceVariant else Gold)
        }
        DropdownMenu(open, { open = false }) {
            listOf(0, 2, 4, 6, 8, 12).forEach { s ->
                DropdownMenuItem({ Text(if (s == 0) "Crossfade off" else "Crossfade $s s", fontWeight = if (s == secs) FontWeight.Bold else FontWeight.Normal) },
                    { music.setCrossfade(s); open = false })
            }
        }
    }
}
