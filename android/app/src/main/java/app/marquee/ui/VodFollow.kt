package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.BookmarkAdd
import androidx.compose.material.icons.filled.BookmarkAdded
import androidx.compose.material.icons.filled.Bookmarks
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.models.VodFollow
import app.marquee.api.models.VodFollowRequest
import app.marquee.api.models.VodSeriesDetail
import app.marquee.core.Marquee
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Following a series on demand: its new episodes are saved to the library as they appear
// (within the follow's size limit and the person's DVR quota). The server must have following on.

/** Whether following is known to be off on a server (a 409 from Follow, or the admin's settings), for this run of the app. */
internal object VodFollowAvailability {
    private val off = mutableStateMapOf<String, Boolean>()
    fun isOff(marquee: Marquee): Boolean = off[marquee.server?.id ?: ""] == true
    fun set(marquee: Marquee, isOff: Boolean) { off[marquee.server?.id ?: ""] = isOff }

    /** Whether this person may follow at all: online, following not known to be off, and allowed to record (admins always are). */
    fun canFollow(marquee: Marquee): Boolean {
        if (marquee.isOffline || isOff(marquee)) return false
        val me = marquee.me.value ?: return true
        if (me.isAdmin) return true
        return (me.restrictions.canRecordDVR ?: me.restrictions.canRecord) != false
    }

    /** Admins can read the server's switch (and default size limit); others learn it from a 409. */
    suspend fun check(marquee: Marquee): Double? {
        if (marquee.me.value?.isAdmin != true) return null
        val vs = withContext(Dispatchers.IO) { runCatching { marquee.settings.getSettings().vodSave }.getOrNull() } ?: return null
        vs.followEnabled?.let { set(marquee, !it) }
        return vs.followDefaultMaxGb
    }
}

/** The message for a failed follow; a 409 means following is off on the server. */
private fun followError(e: Throwable): String = when ((e as? ClientException)?.statusCode) {
    409 -> "Following series isn't turned on on this server."
    else -> serverMessage(e)
}

private fun followLine(f: VodFollow, context: android.content.Context): String = listOfNotNull(
    "${f.episodesSaved} ${if (f.episodesSaved == 1) "episode" else "episodes"} saved",
    f.episodesQueued?.takeIf { it > 0 }?.let { "$it queued" },
    android.text.format.Formatter.formatShortFileSize(context, f.usedBytes) + (f.maxGb?.takeIf { it > 0 }?.let { " of ${gbLabel(it)}" } ?: ""),
).joinToString(" · ")

private fun gbLabel(gb: Double): String = if (gb == gb.toLong().toDouble()) "${gb.toLong()} GB" else "$gb GB"

/**
 * The show page's Follow button: Follow opens the options (size limit, also save the existing
 * episodes, where to save); once following, it shows that and opens the follow's state with
 * Unfollow. Hidden when the server has following off, or the person may not record.
 */
@Composable
fun VodFollowButton(s: VodSeriesDetail) {
    val marquee = LocalMarquee.current
    var following by remember(s.id, s.following) { mutableStateOf(s.following == true) }
    var open by remember { mutableStateOf(false) }
    var defaultGb by remember { mutableStateOf<Double?>(null) }
    LaunchedEffect(Unit) { defaultGb = VodFollowAvailability.check(marquee) }
    if (!VodFollowAvailability.canFollow(marquee) && !following) return
    OutlinedButton({ open = true }, Modifier.focusRing().semantics {
        contentDescription = if (following) "Following ${s.title}" else "Follow ${s.title}"
        stateDescription = if (following) "Following" else "Not following"
    }) {
        Icon(if (following) Icons.Filled.BookmarkAdded else Icons.Filled.BookmarkAdd, null, tint = if (following) Gold else MaterialTheme.colorScheme.onSurface)
        Text(if (following) "Following" else "Follow", Modifier.padding(start = 6.dp))
    }
    if (open) FollowDialog(s, following, defaultGb, onDismiss = { open = false }, onChanged = { now ->
        following = now
        VodChanges.bump()
    })
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FollowDialog(s: VodSeriesDetail, following: Boolean, defaultGb: Double?, onDismiss: () -> Unit, onChanged: (Boolean) -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var follow by remember { mutableStateOf<VodFollow?>(null) }
    var loading by remember { mutableStateOf(following) }
    // null = the server's default (a new follow) or unchanged (an existing one); 0 = no limit.
    var maxGb by remember { mutableStateOf<Double?>(null) }
    var includeExisting by remember { mutableStateOf(false) }
    var destination by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(following) {
        if (!following) return@LaunchedEffect
        follow = withContext(Dispatchers.IO) { runCatching { marquee.livetv.listVodFollows() }.getOrNull() }?.firstOrNull { it.seriesId == s.id }
        loading = false
    }
    val destinations = s.saveDestinations.orEmpty()
    val chip = RoundedCornerShape(8.dp)

    fun save() {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) {
                runCatching { marquee.livetv.followVodSeries(s.id, VodFollowRequest(maxGb = maxGb, includeExisting = includeExisting.takeIf { !following }, destination = destination)) }
            }.onSuccess { onChanged(true); onDismiss() }
                .onFailure { e ->
                    if ((e as? ClientException)?.statusCode == 409) VodFollowAvailability.set(marquee, true)
                    error = followError(e)
                    busy = false
                }
        }
    }
    fun unfollow() {
        busy = true
        error = null
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.livetv.unfollowVodSeries(s.id) } }
                .onSuccess { onChanged(false); onDismiss() }
                .onFailure { error = serverMessage(it); busy = false }
        }
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (following) "Following ${s.title}" else "Follow ${s.title}") },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (following) {
                    val f = follow
                    when {
                        loading -> CircularProgressIndicator()
                        f != null -> Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(followLine(f, context))
                            (f.statusText ?: f.status).takeIf { it.isNotBlank() }?.let {
                                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                    Text("Change the follow", style = MaterialTheme.typography.labelLarge)
                } else {
                    Text("New episodes are saved to the library as they appear. Saved episodes count towards your recording space.",
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Text("Size limit", style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    val first = if (following) "Unchanged" else "Default" + (defaultGb?.let { if (it > 0) " (${gbLabel(it)})" else " (none)" } ?: "")
                    FilterChip(maxGb == null, { maxGb = null }, { Text(first) }, Modifier.focusRing(chip))
                    FilterChip(maxGb == 0.0, { maxGb = 0.0 }, { Text("No limit") }, Modifier.focusRing(chip))
                    listOf(5.0, 10.0, 20.0, 50.0, 100.0).forEach { gb ->
                        FilterChip(maxGb == gb, { maxGb = gb }, { Text(gbLabel(gb)) }, Modifier.focusRing(chip))
                    }
                }
                if (!following) Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f).padding(end = 12.dp)) {
                        Text("Also save existing episodes")
                        Text("From the start, not only new ones", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Switch(includeExisting, { includeExisting = it }, Modifier.focusRing().semantics { contentDescription = "Also save existing episodes" })
                }
                if (destinations.isNotEmpty()) {
                    Text("Save to", style = MaterialTheme.typography.labelLarge)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        if (s.saveDefault != false) FilterChip(destination == null, { destination = null }, { Text(if (following) "Unchanged" else "Shows folder") }, Modifier.focusRing(chip))
                        destinations.forEach { d -> FilterChip(destination == d.id, { destination = d.id }, { Text(d.name) }, Modifier.focusRing(chip)) }
                    }
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            TextButton(::save, Modifier.focusRing().initialFocus(marquee.isTv), enabled = !busy && !(destinations.isNotEmpty() && s.saveDefault == false && destination == null && !following)) {
                Text(if (following) "Save" else "Follow")
            }
        },
        dismissButton = {
            Row {
                if (following) TextButton(::unfollow, Modifier.focusRing(), enabled = !busy) { Text("Unfollow", color = MaterialTheme.colorScheme.error) }
                TextButton(onDismiss, Modifier.focusRing()) { Text("Cancel") }
            }
        },
    )
}

/** VOD Shows' Following entry: opens the list of followed series. Hidden while following is known to be off. */
@Composable
fun VodFollowingButton(nav: NavHostController) {
    val marquee = LocalMarquee.current
    LaunchedEffect(Unit) { VodFollowAvailability.check(marquee) }
    if (marquee.isOffline || VodFollowAvailability.isOff(marquee)) return
    OutlinedButton({ nav.navigate("vodfollows") }, Modifier.focusRing()) {
        Icon(Icons.Filled.Bookmarks, null)
        Text("Following", Modifier.padding(start = 6.dp))
    }
}

/** The series you follow: what each has saved, opening its page; Unfollow on each. */
@Composable
fun VodFollowsScreen(nav: NavHostController) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val changed by VodChanges.version.collectAsState()
    var follows by remember { mutableStateOf<List<VodFollow>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(changed) {
        withContext(Dispatchers.IO) { runCatching { marquee.livetv.listVodFollows() } }
            .onSuccess { follows = it.sortedBy { f -> f.title.lowercase() }; error = null }
            .onFailure { error = serverMessage(it); if (follows == null) follows = emptyList() }
    }
    fun unfollow(f: VodFollow) {
        follows = follows?.filter { it.seriesId != f.seriesId }
        scope.launch {
            withContext(Dispatchers.IO) { runCatching { marquee.livetv.unfollowVodSeries(f.seriesId) } }
                .onFailure { error = serverMessage(it) }
            VodChanges.bump()
        }
    }
    Box(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize().semantics { contentDescription = "Following" },
            contentPadding = PaddingValues(start = sidePadding, end = sidePadding, top = if (marquee.isTv) 24.dp else 56.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)) {
            item { Text("Following", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
            item {
                Text("New episodes of these series are saved to the library as they appear.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            }
            error?.let { e -> item { Text(e, color = MaterialTheme.colorScheme.error) } }
            val list = follows
            when {
                list == null -> item { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
                list.isEmpty() -> item {
                    Text("You're not following any series. Follow a show from its page to save its new episodes automatically.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                else -> items(list, key = { it.seriesId }) { f ->
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Row(Modifier.weight(1f).focusCard({ nav.navigate("vodshow/${f.seriesId}") }).semantics(mergeDescendants = true) { contentDescription = f.title },
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            Artwork(marquee.absolute(f.posterUrl), f.title, Shape.Poster, Modifier.width(if (marquee.isTv) 80.dp else 60.dp))
                            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                                Text(f.title, fontWeight = FontWeight.Medium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                Text(followLine(f, context), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                (f.statusText ?: f.status).takeIf { it.isNotBlank() }?.let {
                                    Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                }
                            }
                        }
                        TextButton({ unfollow(f) }, Modifier.focusRing()) { Text("Unfollow") }
                    }
                }
            }
        }
        if (!marquee.isTv) FloatingBack({ nav.popBackStack() })
    }
}
