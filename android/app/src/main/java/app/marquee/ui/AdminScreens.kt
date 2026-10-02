package app.marquee.ui

import android.content.Intent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.PersonAdd
import androidx.compose.material.icons.filled.Remove
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.marquee.api.models.CinemaSettings
import app.marquee.api.models.Invite
import app.marquee.api.models.InviteCreate
import app.marquee.api.models.InviteCreated
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.Library
import app.marquee.api.models.MeUpdate
import app.marquee.api.models.ServerSettingsUpdate
import app.marquee.api.models.User
import app.marquee.api.models.UserRestrictions
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/** "Play trailers before movies" (PLAY-18): the signed-in person's own preference. */
@Composable
fun TrailersPreference() {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    val scope = rememberCoroutineScope()
    val u = me ?: return
    val on = u.preferences.cinemaTrailers != false
    var error by remember { mutableStateOf<String?>(null) }
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text("Play trailers before movies")
            Text("When the server has cinema trailers on", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
        }
        Switch(on, { v ->
            scope.launch {
                withContext(Dispatchers.IO) { runCatching { marquee.auth.updateMe(MeUpdate(preferences = u.preferences.copy(cinemaTrailers = v))) } }
                    .onSuccess { marquee.updated(it); error = null }.onFailure { error = it.message ?: "Couldn't save" }
            }
        }, Modifier.focusRing().semantics { contentDescription = "Play trailers before movies" })
    }
}

@Composable
private fun SectionTitle(t: String) =
    Text(t, Modifier.padding(horizontal = sidePadding).padding(top = 20.dp, bottom = 6.dp), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold, color = Gold)

/** Admin server settings on Android: cinema trailers (PLAY-18). The full set lives on the web. */
@Composable
fun ServerSettingsScreen() {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var loaded by remember { mutableStateOf(false) }
    var trailers by remember { mutableIntStateOf(0) }
    var preroll by remember { mutableStateOf<ItemSummary?>(null) }
    var prerollId by remember { mutableStateOf<Long?>(null) }
    var query by remember { mutableStateOf("") }
    var idText by remember { mutableStateOf("") }
    var results by remember { mutableStateOf(emptyList<ItemSummary>()) }
    var status by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) { runCatching { marquee.settings.getSettings().cinema } }
            .onSuccess { c ->
                trailers = c?.trailers ?: 0
                prerollId = c?.prerollItemId
                loaded = true
            }.onFailure { status = "Couldn't load the settings: ${it.message}" }
    }
    // The picked pre-roll's title.
    LaunchedEffect(prerollId) {
        val id = prerollId
        preroll = if (id == null) null else withContext(Dispatchers.IO) { runCatching { marquee.items.getItem(id).summary() }.getOrNull() }
    }
    LaunchedEffect(query) {
        if (query.trim().length < 2) { results = emptyList(); return@LaunchedEffect }
        delay(250)
        results = withContext(Dispatchers.IO) {
            runCatching { marquee.search.search(query.trim(), 10).groups.flatMap { it.items } }.getOrDefault(emptyList())
        }.filter { it.type in listOf(ItemType.MOVIE, ItemType.VIDEO, ItemType.EPISODE) }
    }
    fun save() {
        scope.launch {
            status = withContext(Dispatchers.IO) {
                runCatching { marquee.settings.updateSettings(ServerSettingsUpdate(cinema = CinemaSettings(trailers, prerollId ?: 0))) }
            }.fold({ "Saved." }, { "Couldn't save: ${it.message}" })
        }
    }

    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Server settings", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        item { SectionTitle("Cinema trailers") }
        item {
            Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Before a movie played from the start, play trailers of other movies in the library, then an optional pre-roll video. Each person can turn this off in their settings.",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Trailers", Modifier.weight(1f))
                    IconButton({ trailers = (trailers - 1).coerceAtLeast(0) }, Modifier.focusRing().initialFocus(marquee.isTv), enabled = loaded && trailers > 0) {
                        Icon(Icons.Filled.Remove, "Fewer trailers")
                    }
                    Text(if (trailers == 0) "Off" else "$trailers", Modifier.semantics { contentDescription = "Trailers: ${if (trailers == 0) "Off" else trailers}" },
                        fontWeight = FontWeight.SemiBold)
                    IconButton({ trailers = (trailers + 1).coerceAtMost(5) }, Modifier.focusRing(), enabled = loaded && trailers < 5) { Icon(Icons.Filled.Add, "More trailers") }
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("Pre-roll")
                        Text(prerollId?.let { id -> preroll?.let { "${it.title} (#$id)" } ?: "#$id" } ?: "None",
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    if (prerollId != null) TextButton({ prerollId = null }, Modifier.focusRing()) { Text("Clear") }
                }
                OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth(), singleLine = true, label = { Text("Find a video for the pre-roll") })
            }
        }
        items(results, key = { "r" + it.id }) { r ->
            ListItem(
                headlineContent = { Text(r.title) },
                supportingContent = { Text(subtitleFor(r)) },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
                modifier = Modifier.focusCard({ prerollId = r.id; query = "" }).semantics { contentDescription = "Use ${r.title} as the pre-roll" },
            )
        }
        item {
            Column(Modifier.padding(horizontal = sidePadding), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(idText, { idText = it.filter(Char::isDigit) }, Modifier.weight(1f), singleLine = true, label = { Text("Or an item id") })
                    OutlinedButton({ idText.toLongOrNull()?.let { prerollId = it; idText = "" } }, Modifier.focusRing(), enabled = idText.isNotEmpty()) { Text("Use") }
                }
                Button(::save, Modifier.focusRing(), enabled = loaded) { Text("Save") }
                status?.let { Text(it, color = if (it == "Saved.") Gold else MaterialTheme.colorScheme.error) }
            }
        }
    }
}

private val dateFormat: DateTimeFormatter = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
private fun OffsetDateTime.date(): String = atZoneSameInstant(ZoneId.systemDefault()).format(dateFormat)

/** Where an invite stands: pending, used or expired. */
fun inviteStatus(i: Invite): String = when {
    i.usedAt != null -> "Used by ${i.usedBy ?: "someone"}"
    i.expiresAt?.isBefore(OffsetDateTime.now()) == true -> "Expired"
    else -> "Pending" + (i.expiresAt?.let { " · expires ${it.date()}" } ?: "")
}

/**
 * Users and sharing (USER-13), for admins: the household, friends who joined through an
 * invite, and the invites themselves. A new invite is a link to the server's join page,
 * sent with the system share sheet.
 */
@Composable
fun UsersScreen() {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    var users by remember { mutableStateOf<List<User>?>(null) }
    var invites by remember { mutableStateOf(emptyList<Invite>()) }
    var error by remember { mutableStateOf<String?>(null) }
    var inviting by remember { mutableStateOf(false) }
    var created by remember { mutableStateOf<InviteCreated?>(null) }
    var reload by remember { mutableIntStateOf(0) }
    LaunchedEffect(reload) {
        withContext(Dispatchers.IO) { runCatching { marquee.users.listUsers() to marquee.users.listInvites() } }
            .onSuccess { (u, i) -> users = u; invites = i.sortedByDescending { it.createdAt }; error = null }
            .onFailure { error = it.message }
    }
    val (friends, household) = (users ?: emptyList()).partition { it.restrictions.friend == true }

    @Composable
    fun UserRow(u: User) = ListItem(
        headlineContent = { Text(u.displayName) },
        supportingContent = { Text(listOfNotNull("@${u.username}", "Admin".takeIf { u.isAdmin }, "Managed".takeIf { u.isManaged }).joinToString(" · ")) },
        leadingContent = { Avatar(u.displayName, marquee.absolute(u.avatarUrl), 40) },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
    )

    LazyColumn(contentPadding = PaddingValues(vertical = 16.dp)) {
        item { Text("Users", Modifier.padding(horizontal = sidePadding, vertical = 8.dp), style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
        error?.let { e -> item { Text(e, Modifier.padding(horizontal = sidePadding), color = MaterialTheme.colorScheme.error) } }
        item { SectionTitle("Household") }
        items(household, key = { "u" + it.id }) { UserRow(it) }
        item { SectionTitle("Friends") }
        if (users != null && friends.isEmpty()) item {
            Text("No friends yet. Invite someone to share your libraries with them.", Modifier.padding(horizontal = sidePadding),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        items(friends, key = { "f" + it.id }) { UserRow(it) }
        item {
            Button({ inviting = true }, Modifier.padding(horizontal = sidePadding, vertical = 12.dp).focusRing().initialFocus(marquee.isTv)) {
                Icon(Icons.Filled.PersonAdd, null)
                Text("Invite a friend", Modifier.padding(start = 8.dp))
            }
        }
        if (invites.isNotEmpty()) item { SectionTitle("Invites") }
        items(invites, key = { "i" + it.id }) { i ->
            val name = i.note?.takeIf { it.isNotBlank() } ?: "Invite"
            ListItem(
                headlineContent = { Text(name) },
                supportingContent = { Text("${inviteStatus(i)} · created ${i.createdAt.date()}") },
                trailingContent = {
                    IconButton({
                        scope.launch {
                            withContext(Dispatchers.IO) { runCatching { marquee.users.deleteInvite(i.id) } }
                                .onSuccess { reload++ }.onFailure { error = it.message }
                        }
                    }, Modifier.focusRing()) { Icon(Icons.Filled.Delete, "Delete invite $name") }
                },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.background),
            )
            HorizontalDivider()
        }
    }
    if (inviting) InviteDialog(onDismiss = { inviting = false }) { c -> inviting = false; created = c; reload++ }
    created?.let { c -> InviteLinkDialog(c) { created = null } }
}

/** Ratings offered as a friend's limit. */
private val ratingLimits = UserRestrictions.MaxContentRating.entries

/** Invite a friend: who it's for, how long the link lasts and what they may watch. */
@Composable
private fun InviteDialog(onDismiss: () -> Unit, onCreated: (InviteCreated) -> Unit) {
    val marquee = LocalMarquee.current
    val scope = rememberCoroutineScope()
    val libraries by produceState(emptyList<Library>()) { value = withContext(Dispatchers.IO) { runCatching { marquee.libraries.listLibraries() }.getOrDefault(emptyList()) } }
    var note by remember { mutableStateOf("") }
    var days by remember { mutableIntStateOf(7) }
    var allLibraries by remember { mutableStateOf(true) }
    var picked by remember { mutableStateOf(emptySet<Long>()) }
    var rating by remember { mutableStateOf<UserRestrictions.MaxContentRating?>(null) }
    var ratingMenu by remember { mutableStateOf(false) }
    var remote by remember { mutableStateOf(true) }
    var canRequest by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Invite a friend") },
        text = {
            Column(Modifier.heightIn(max = 460.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(note, { note = it }, Modifier.fillMaxWidth().semantics { testTag = "inviteNote" }, singleLine = true, label = { Text("Who it's for") })
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Link lasts", Modifier.weight(1f))
                    IconButton({ days = (days - 1).coerceAtLeast(1) }, Modifier.focusRing()) { Icon(Icons.Filled.Remove, "Fewer days") }
                    Text(if (days == 1) "1 day" else "$days days", fontWeight = FontWeight.SemiBold)
                    IconButton({ days = (days + 1).coerceAtMost(90) }, Modifier.focusRing()) { Icon(Icons.Filled.Add, "More days") }
                }
                Text("Libraries", fontWeight = FontWeight.SemiBold)
                CheckRow("All libraries", allLibraries) { allLibraries = it }
                if (!allLibraries) libraries.forEach { l ->
                    CheckRow(l.name, l.id in picked) { on -> picked = if (on) picked + l.id else picked - l.id }
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("Highest rating", Modifier.weight(1f))
                    Box {
                        OutlinedButton({ ratingMenu = true }, Modifier.focusRing()) { Text(rating?.value ?: "No limit") }
                        DropdownMenu(ratingMenu, { ratingMenu = false }) {
                            DropdownMenuItem({ Text("No limit") }, { rating = null; ratingMenu = false })
                            ratingLimits.forEach { r -> DropdownMenuItem({ Text(r.value) }, { rating = r; ratingMenu = false }) }
                        }
                    }
                }
                SwitchRow("Can stream away from home", remote) { remote = it }
                SwitchRow("Can request titles", canRequest) { canRequest = it }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            TextButton({
                if (busy) return@TextButton
                busy = true
                error = null
                val restrictions = UserRestrictions(
                    libraryIds = if (allLibraries) null else picked.toList(),
                    maxContentRating = rating, allowRemote = remote, canRequest = canRequest, friend = true,
                )
                scope.launch {
                    withContext(Dispatchers.IO) {
                        runCatching { marquee.users.createInvite(InviteCreate(note.trim().ifBlank { null }, days, restrictions)) }
                    }.onSuccess(onCreated).onFailure { error = it.message ?: "Couldn't create the invite" }
                    busy = false
                }
            }, Modifier.focusRing(), enabled = !busy && (allLibraries || picked.isNotEmpty())) { Text("Create invite") }
        },
        dismissButton = { TextButton(onDismiss, Modifier.focusRing()) { Text("Cancel") } },
    )
}

@Composable
private fun CheckRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().focusCard({ onChange(!checked) }), verticalAlignment = Alignment.CenterVertically) {
        Checkbox(checked, onChange)
        Text(label)
    }
}

@Composable
private fun SwitchRow(label: String, on: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f))
        Switch(on, onChange, Modifier.focusRing().semantics { contentDescription = label })
    }
}

/** The new invite's link (this server's address + /join/…), to send with the share sheet. */
@Composable
private fun InviteLinkDialog(c: InviteCreated, onDone: () -> Unit) {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    val link = (marquee.baseUrl ?: "") + c.path
    AlertDialog(
        onDismissRequest = onDone,
        title = { Text("Invite ready") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text("Send this link${c.invite.note?.takeIf { it.isNotBlank() }?.let { " to $it" } ?: ""}. They choose a username and password, then sign in with them in any Marquee app.")
                SelectionContainer { Text(link, Modifier.semantics { testTag = "inviteLink" }, color = Gold, fontWeight = FontWeight.SemiBold) }
                Text("The link must use an address your friend can reach (for example your Tailscale or public address).",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = {
            // TVs have no share sheet: the link is shown to type in elsewhere.
            if (!marquee.isTv) TextButton({
                val send = Intent(Intent.ACTION_SEND).setType("text/plain")
                    .putExtra(Intent.EXTRA_SUBJECT, "Join me on ${marquee.server?.name ?: "Marquee"}").putExtra(Intent.EXTRA_TEXT, link)
                context.startActivity(Intent.createChooser(send, "Send invite"))
            }, Modifier.focusRing()) { Text("Share") }
        },
        dismissButton = { TextButton(onDone, Modifier.focusRing()) { Text("Done") } },
    )
}
