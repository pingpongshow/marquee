package app.marquee.music

import android.widget.Toast
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import app.marquee.api.models.ListeningRecap
import app.marquee.api.models.RecapEntry
import app.marquee.ui.Gold
import app.marquee.ui.LocalMarquee
import app.marquee.ui.LocalMusic
import app.marquee.ui.focusRing
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * "Your Year in Music" (MUSIC-22): a card on the Music page and in Your Stats, shown once
 * there's a year of listening, with a year picker when there's more than one.
 */
@Composable
fun YearInMusicCard(nav: NavHostController, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    val years by produceState(emptyList<Int>()) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.music.recapYears() }.getOrDefault(emptyList()) }.sortedDescending()
    }
    if (years.isEmpty()) return
    var year by remember(years) { mutableIntStateOf(years.first()) }
    Column(
        modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp))
            .background(Brush.linearGradient(listOf(Color(0xFF7C3AED), Color(0xFFDB2777), Color(0xFFF59E0B))))
            .focusRing(RoundedCornerShape(16.dp))
            .clickable { nav.navigate("recap/$year") }
            .semantics { contentDescription = "Your Year in Music $year" }
            .padding(18.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.AutoAwesome, null, tint = Color.White)
            Text("YOUR YEAR IN MUSIC", Modifier.padding(start = 8.dp), color = Color.White.copy(alpha = 0.85f), style = MaterialTheme.typography.labelLarge,
                fontWeight = FontWeight.Bold)
        }
        Text("$year, wrapped", color = Color.White, fontSize = 30.sp, fontWeight = FontWeight.Black)
        Text("Your top artists, songs and listening habits.", color = Color.White.copy(alpha = 0.85f), style = MaterialTheme.typography.bodyMedium)
        if (years.size > 1) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            years.forEach { y ->
                FilterChip(y == year, { year = y }, { Text("$y", color = Color.White) }, Modifier.focusRing(RoundedCornerShape(8.dp)))
            }
        }
    }
}

/** The story's pages and their colours. */
private val pageColours = listOf(
    listOf(Color(0xFF1E1B4B), Color(0xFF7C3AED)),
    listOf(Color(0xFF831843), Color(0xFFF43F5E)),
    listOf(Color(0xFF0C4A6E), Color(0xFF06B6D4)),
    listOf(Color(0xFF14532D), Color(0xFF22C55E)),
    listOf(Color(0xFF7C2D12), Color(0xFFF97316)),
    listOf(Color(0xFF4A044E), Color(0xFFD946EF)),
    listOf(Color(0xFF172554), Color(0xFF3B82F6)),
    listOf(Color(0xFF713F12), Color(0xFFEAB308)),
    listOf(Color(0xFF134E4A), Color(0xFF14B8A6)),
    listOf(Color(0xFF0B0B0F), Color(0xFF3F3F46)),
)
private const val PAGES = 10

/**
 * The recap as a story (MUSIC-22): full-screen pages, tapped (left or right side), swiped or,
 * on TV, moved through with left and right. Ends with a summary, Save as playlist and Play
 * your top songs.
 */
@Composable
fun RecapScreen(nav: NavHostController, year: Int) {
    val marquee = LocalMarquee.current
    val recap by produceState<Result<ListeningRecap>?>(null, year) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.music.listeningRecap(year) } }
    }
    val r = recap
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        when {
            r == null -> CircularProgressIndicator(Modifier.align(Alignment.Center))
            r.isFailure -> Text("Couldn't load your year: ${r.exceptionOrNull()?.message}", Modifier.align(Alignment.Center).padding(24.dp), color = MaterialTheme.colorScheme.error)
            r.getOrThrow().plays == 0 -> Text("Nothing played in $year yet.", Modifier.align(Alignment.Center), color = Color.White)
            else -> Story(nav, r.getOrThrow())
        }
        IconButton({ nav.popBackStack() }, Modifier.align(Alignment.TopEnd).systemBarsPadding().padding(top = 18.dp, end = 8.dp).focusRing()) {
            Icon(Icons.Filled.Close, "Close recap", tint = Color.White)
        }
    }
}

@Composable
private fun Story(nav: NavHostController, r: ListeningRecap) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val pager = rememberPagerState { PAGES }
    val snackbar = remember { SnackbarHostState() }
    var saving by remember { mutableStateOf(false) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }

    fun go(page: Int) { scope.launch { pager.animateScrollToPage(page.coerceIn(0, PAGES - 1)) } }
    fun playTop(from: Int = 0) {
        val tracks = r.topTracks.map { it.item }
        if (tracks.isEmpty()) return
        music.play(tracks, from, source = "Your top songs of ${r.year}")
        Toast.makeText(context, "Playing your top songs", Toast.LENGTH_SHORT).show()
    }
    fun save() {
        if (saving) return
        saving = true
        scope.launch {
            val p = withContext(Dispatchers.IO) { runCatching { marquee.music.recapPlaylist(r.year) } }
            saving = false
            p.onSuccess { pl ->
                if (snackbar.showSnackbar("Saved “${pl.title}”", actionLabel = "Open") == SnackbarResult.ActionPerformed) nav.navigate("playlist/${pl.id}")
            }.onFailure { snackbar.showSnackbar("Couldn't save the playlist: ${it.message}") }
        }
    }

    Box(
        Modifier.fillMaxSize()
            .focusRequester(focus)
            .onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionRight -> { go(pager.currentPage + 1); true }
                    Key.DirectionLeft -> { go(pager.currentPage - 1); true }
                    else -> false
                }
            }
            .focusable()
            .semantics { contentDescription = "Year in Music"; stateDescription = "Page ${pager.currentPage + 1} of $PAGES" },
    ) {
        HorizontalPager(pager, Modifier.fillMaxSize()) { page ->
            val (top, bottom) = pageColours[page].let { it[0] to it[1] }
            Box(Modifier.fillMaxSize().background(Brush.linearGradient(listOf(top, bottom)))) {
                // Taps: the left third goes back, the rest forward.
                BoxWithConstraints(Modifier.fillMaxSize()) {
                    Row(Modifier.fillMaxSize()) {
                        Box(Modifier.weight(1f).fillMaxHeight().clickable(remember { MutableInteractionSource() }, null) { go(page - 1) }
                            .semantics { contentDescription = "Previous page" })
                        Box(Modifier.weight(2f).fillMaxHeight().clickable(remember { MutableInteractionSource() }, null) { go(page + 1) }
                            .semantics { contentDescription = "Next page" })
                    }
                }
                Column(
                    Modifier.fillMaxSize().systemBarsPadding().padding(horizontal = if (marquee.isTv) 96.dp else 28.dp, vertical = 64.dp),
                    verticalArrangement = Arrangement.Center,
                ) {
                    val shown = pager.currentPage == page
                    val appear by animateFloatAsState(if (shown) 1f else 0f, tween(600), label = "appear")
                    Column(Modifier.fillMaxWidth().padding(top = ((1f - appear) * 40).dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                        StoryPage(page, r, onPlay = ::playTop, onSave = ::save, saving = saving, onDone = { nav.popBackStack() })
                    }
                }
            }
        }
        // Progress along the top.
        Row(Modifier.fillMaxWidth().systemBarsPadding().padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            repeat(PAGES) { i ->
                Box(Modifier.weight(1f).height(3.dp).clip(RoundedCornerShape(2.dp))
                    .background(if (i <= pager.currentPage) Color.White else Color.White.copy(alpha = 0.3f)))
            }
        }
        SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter).systemBarsPadding())
    }
}

@Composable
private fun Kicker(t: String) = Text(t.uppercase(), color = Color.White.copy(alpha = 0.8f), style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.Bold,
    letterSpacing = 2.sp)

@Composable
private fun Huge(t: String, size: Int = 64) = Text(t, color = Color.White, fontSize = size.sp, lineHeight = (size * 1.05).sp, fontWeight = FontWeight.Black)

@Composable
private fun Line(t: String) = Text(t, color = Color.White.copy(alpha = 0.9f), style = MaterialTheme.typography.titleMedium)

private fun minutes(m: Int) = "%,d".format(m)

private val months = listOf("J", "F", "M", "A", "M", "J", "J", "A", "S", "O", "N", "D")

@Composable
private fun StoryPage(page: Int, r: ListeningRecap, onPlay: (Int) -> Unit, onSave: () -> Unit, saving: Boolean, onDone: () -> Unit) {
    val marquee = LocalMarquee.current
    when (page) {
        0 -> {
            Kicker("${r.year} in music")
            Line("You listened for")
            Huge(minutes(r.minutes), 88)
            Huge(if (r.minutes == 1) "minute" else "minutes", 40)
            Line("${r.plays} plays · ${r.tracks} songs · ${r.artists} artists")
        }
        1 -> {
            val a = r.topArtists.firstOrNull()
            Kicker("Your top artist")
            if (a != null) {
                Art(marquee.imageUrl(a.item.images?.poster, 400), Modifier.size(if (marquee.isTv) 220.dp else 240.dp).clip(CircleShape))
                Huge(a.item.title, 48)
                Line("${if (a.plays == 1) "1 play" else "${a.plays} plays"} · ${minutes(a.minutes)} ${if (a.minutes == 1) "minute" else "minutes"} together")
            } else Line("No artists yet.")
        }
        2 -> {
            Kicker("Your top 5 artists")
            r.topArtists.take(5).forEachIndexed { i, a -> Ranked(i, a, CircleShape) }
        }
        3 -> {
            Kicker("Your top songs")
            r.topTracks.take(5).forEachIndexed { i, t ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Ranked(i, t, RoundedCornerShape(6.dp), Modifier.weight(1f))
                    IconButton({ onPlay(i) }, Modifier.focusRing()) { Icon(Icons.Filled.PlayArrow, "Play ${t.item.title}", tint = Color.White) }
                }
            }
            PlayTop(onPlay)
        }
        4 -> {
            Kicker("Your top albums")
            r.topAlbums.take(5).forEachIndexed { i, a -> Ranked(i, a, RoundedCornerShape(6.dp)) }
        }
        5 -> {
            Kicker("Your genres")
            if (r.topGenres.isEmpty()) Line("Not enough genre tags to tell.")
            val most = r.topGenres.maxOfOrNull { it.plays }?.coerceAtLeast(1) ?: 1
            r.topGenres.take(6).forEachIndexed { i, g ->
                Column {
                    Text(g.name, color = Color.White, fontSize = if (i == 0) 40.sp else 24.sp, fontWeight = FontWeight.Black)
                    Box(Modifier.fillMaxWidth(g.plays.toFloat() / most).height(6.dp).clip(RoundedCornerShape(3.dp)).background(Color.White.copy(alpha = 0.7f)))
                }
            }
        }
        6 -> {
            Kicker("When you listen")
            val peak = r.byHour.indices.maxByOrNull { r.byHour[it] } ?: 0
            Huge(when (peak) { in 5..11 -> "Morning person"; in 12..16 -> "Afternoon listener"; in 17..21 -> "Evening listener"; else -> "Night owl" }, 40)
            Line("Most of your plays start around ${"%02d:00".format(peak)}.")
            Bars(r.byHour, labels = (0..23).map { if (it % 6 == 0) "$it" else "" }, height = 110)
            Line("By month")
            Bars(r.byMonth, labels = months, height = 90)
        }
        7 -> {
            Kicker("Your biggest day")
            val d = r.topDay
            if (d != null) {
                Huge(d.date.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.LONG)), 40)
                Line("${minutes(d.minutes)} minutes of music in one day.")
            } else Line("Every day was a good day.")
            Spacer(Modifier.height(24.dp))
            Kicker("Longest streak")
            Huge(if (r.longestStreakDays == 1) "1 day" else "${r.longestStreakDays} days", 56)
            Line("in a row with music.")
        }
        8 -> {
            Kicker("New discoveries")
            Huge("${r.newArtists}", 96)
            Huge(if (r.newArtists == 1) "new artist" else "new artists", 40)
            Line("you played for the first time this year.")
        }
        else -> {
            Kicker("${r.year} wrapped")
            Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(18.dp)).background(Color.White.copy(alpha = 0.08f)).padding(20.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Summary("Minutes", minutes(r.minutes))
                r.topArtists.firstOrNull()?.let { Summary("Top artist", it.item.title) }
                r.topTracks.firstOrNull()?.let { Summary("Top song", it.item.title) }
                r.topAlbums.firstOrNull()?.let { Summary("Top album", it.item.title) }
                r.topGenres.firstOrNull()?.let { Summary("Top genre", it.name) }
                Summary("Longest streak", if (r.longestStreakDays == 1) "1 day" else "${r.longestStreakDays} days")
            }
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Button(onSave, Modifier.focusRing(), enabled = !saving,
                    colors = ButtonDefaults.buttonColors(containerColor = Gold, contentColor = Color.Black)) {
                    Icon(Icons.AutoMirrored.Filled.PlaylistAdd, null)
                    Text("Save as playlist", Modifier.padding(start = 6.dp))
                }
                PlayTop(onPlay)
            }
            OutlinedButton(onDone, Modifier.focusRing()) { Text("Done", color = Color.White) }
        }
    }
}

@Composable
private fun PlayTop(onPlay: (Int) -> Unit) = OutlinedButton({ onPlay(0) }, Modifier.focusRing()) {
    Icon(Icons.Filled.PlayArrow, null, tint = Color.White)
    Text("Play your top songs", Modifier.padding(start = 6.dp), color = Color.White)
}

@Composable
private fun Summary(label: String, value: String) = Row(verticalAlignment = Alignment.CenterVertically) {
    Text(label, Modifier.width(130.dp), color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.bodyMedium)
    Text(value, color = Color.White, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
}

@Composable
private fun Art(url: String?, modifier: Modifier) {
    Box(modifier.background(Color.White.copy(alpha = 0.15f)), contentAlignment = Alignment.Center) {
        if (url != null) AsyncImage(url, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
    }
}

@Composable
private fun Ranked(i: Int, e: RecapEntry, shape: androidx.compose.ui.graphics.Shape, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    Row(modifier, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Text("${i + 1}", Modifier.width(30.dp), color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Black)
        Art(marquee.imageUrl(e.item.images?.poster, 160), Modifier.size(52.dp).clip(shape))
        Column(Modifier.weight(1f)) {
            Text(e.item.title, color = Color.White, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis, fontSize = 18.sp)
            Text(listOfNotNull(e.item.artistCredit?.takeIf { e.item.type != app.marquee.api.models.ItemType.ARTIST }, if (e.plays == 1) "1 play" else "${e.plays} plays")
                .joinToString(" · "), color = Color.White.copy(alpha = 0.75f), style = MaterialTheme.typography.bodySmall, maxLines = 1)
        }
    }
}

/** A small bar chart (plays by hour, minutes by month). */
@Composable
private fun Bars(values: List<Int>, labels: List<String>, height: Int) {
    val most = values.maxOrNull()?.coerceAtLeast(1) ?: 1
    Column(Modifier.widthIn(max = 640.dp).fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().height(height.dp), horizontalArrangement = Arrangement.spacedBy(3.dp), verticalAlignment = Alignment.Bottom) {
            values.forEach { v ->
                Box(Modifier.weight(1f).fillMaxHeight((v.toFloat() / most).coerceAtLeast(0.02f)).clip(RoundedCornerShape(topStart = 3.dp, topEnd = 3.dp))
                    .background(if (v == most) Color.White else Color.White.copy(alpha = 0.5f)))
            }
        }
        Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(3.dp)) {
            labels.forEach { l -> Text(l, Modifier.weight(1f), color = Color.White.copy(alpha = 0.7f), fontSize = 10.sp, textAlign = TextAlign.Center,
                softWrap = false, overflow = TextOverflow.Visible) }
        }
    }
}
