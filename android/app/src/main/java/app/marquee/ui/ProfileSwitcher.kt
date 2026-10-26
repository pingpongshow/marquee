package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
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
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.navigation.NavHostController
import app.marquee.api.infrastructure.ClientException
import app.marquee.api.models.Profile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Who's watching, like the web and Apple apps: the signed-in profile at the top of the
// navigation (the TV side menu, or the top of Home on phones and tablets) opens a profile
// picker that switches this device directly (a PIN or password when the profile has one),
// with Account settings and Sign out.

/** What switching to [p] asks for: an admin opens a managed profile directly. */
internal fun profileNeeds(p: Profile, isAdmin: Boolean): Profile.Requires =
    if (isAdmin && p.isManaged) Profile.Requires.NONE else p.requires

/** The message for a switch that didn't work. */
private fun switchError(e: Throwable): String = when ((e as? ClientException)?.statusCode) {
    429 -> "Too many tries. Wait a minute and try again."
    401, 403 -> "That didn't work. Try again."
    else -> serverMessage(e, "Couldn't switch profile")
}

/**
 * The signed-in profile: avatar and name. Opens the profile picker, or Settings for a friend
 * (who has no household profiles to switch to). [tv] is the side menu's form, avatar over name.
 */
@Composable
fun ProfileButton(nav: NavHostController, tv: Boolean = false, modifier: Modifier = Modifier) {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    val u = me ?: return
    var open by remember { mutableStateOf(false) }
    val friend = u.restrictions.friend == true
    val onClick = { if (friend) nav.navigate("settings") { launchSingleTop = true } else open = true }
    val label = if (friend) "Account: ${u.displayName}" else "Profile: ${u.displayName}. Switch profile"
    if (tv) {
        var focused by remember { mutableStateOf(false) }
        Column(
            modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(if (focused) Gold else Color.Transparent)
                .onFocusChanged { focused = it.isFocused }.clickable(onClick = onClick)
                .semantics(mergeDescendants = true) { contentDescription = label }.padding(vertical = 8.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Avatar(u.displayName, marquee.absolute(u.avatarUrl), 40)
            Text(u.displayName, Modifier.padding(top = 4.dp, start = 4.dp, end = 4.dp), style = MaterialTheme.typography.labelMedium,
                color = if (focused) MaterialTheme.colorScheme.background else MaterialTheme.colorScheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    } else {
        Row(
            modifier.clip(RoundedCornerShape(50)).focusRing().clickable(onClick = onClick)
                .semantics(mergeDescendants = true) { contentDescription = label }.padding(start = 4.dp, end = 12.dp, top = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Avatar(u.displayName, marquee.absolute(u.avatarUrl), 32)
            Text(u.displayName, Modifier.widthIn(max = 200.dp), fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!friend) Icon(Icons.Filled.Groups, null, Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    if (open) ProfileSwitcher(nav, onDismiss = { open = false })
}

/**
 * "Who's watching?": this household's profiles (the current one marked, a lock on those with a
 * PIN or password). Picking one switches this device to it; Account settings and Sign out below.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ProfileSwitcher(nav: NavHostController, onDismiss: () -> Unit) {
    val marquee = LocalMarquee.current
    val music = LocalMusic.current
    val me by marquee.me.collectAsState()
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var asking by remember { mutableStateOf<Profile?>(null) }
    var confirmSignOut by remember { mutableStateOf(false) }
    val profiles by produceState<Result<List<Profile>>?>(null) {
        value = withContext(Dispatchers.IO) { runCatching { marquee.users.listProfiles() } }
    }
    val isAdmin = me?.isAdmin == true
    // A profile with a time limit sees what's left today ("45 min left today").
    val timeLeft by produceState<String?>(null, me?.id) {
        value = app.marquee.core.TimeLimitRules.leftLabel(marquee.timeLimits.refresh())
    }

    /** The switch itself; the previous profile's music doesn't carry over. */
    suspend fun switchTo(p: Profile, pin: String?, password: String?) =
        marquee.switchProfile(p.id, pin, password, onSwitched = { music.stop() })

    fun choose(p: Profile) {
        if (busy) return
        error = null
        if (p.id == me?.id) { onDismiss(); return }
        if (profileNeeds(p, isAdmin) != Profile.Requires.NONE) { asking = p; return }
        busy = true
        scope.launch { runCatching { switchTo(p, null, null) }.onFailure { error = switchError(it); busy = false } }
    }

    Dialog(onDismissRequest = onDismiss) {
        Surface(shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.surface) {
            Column(
                Modifier.verticalScroll(rememberScrollState()).padding(24.dp).semantics { contentDescription = "Who's watching?" },
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text("Who's watching?", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                timeLeft?.let { Text(it, Modifier.padding(top = 4.dp), color = Gold, style = MaterialTheme.typography.bodyMedium) }
                error?.let { Text(it, Modifier.padding(top = 8.dp), color = MaterialTheme.colorScheme.error) }
                val list = profiles
                when {
                    list == null || busy -> CircularProgressIndicator(Modifier.padding(24.dp))
                    list.isFailure -> Text(list.exceptionOrNull()?.let { serverMessage(it) } ?: "Couldn't load profiles", Modifier.padding(16.dp), color = MaterialTheme.colorScheme.error)
                    else -> FlowRow(
                        Modifier.padding(vertical = 20.dp),
                        horizontalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterHorizontally), verticalArrangement = Arrangement.spacedBy(16.dp),
                    ) {
                        list.getOrDefault(emptyList()).forEachIndexed { i, p ->
                            val current = p.id == me?.id
                            val locked = profileNeeds(p, isAdmin) != Profile.Requires.NONE
                            Column(
                                Modifier.width(96.dp).initialFocus(marquee.isTv && i == 0).focusCard({ choose(p) }, RoundedCornerShape(12.dp))
                                    .semantics(mergeDescendants = true) {
                                        contentDescription = p.displayName
                                        stateDescription = listOfNotNull("Current".takeIf { current }, "Locked".takeIf { locked }).joinToString(", ")
                                    }.padding(6.dp),
                                horizontalAlignment = Alignment.CenterHorizontally,
                            ) {
                                Box {
                                    Avatar(p.displayName, marquee.absolute(p.avatarUrl), 72)
                                    if (locked) Icon(Icons.Filled.Lock, null, Modifier.align(Alignment.BottomEnd).size(22.dp)
                                        .background(MaterialTheme.colorScheme.surface, CircleShape).padding(4.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                                Text(p.displayName, Modifier.padding(top = 6.dp), maxLines = 1, overflow = TextOverflow.Ellipsis,
                                    fontWeight = if (current) FontWeight.Bold else FontWeight.Normal)
                                Text(if (current) "Current" else " ", style = MaterialTheme.typography.labelSmall, color = Gold)
                            }
                        }
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton({ onDismiss(); nav.navigate("settings") { launchSingleTop = true } }, Modifier.focusRing()) { Text("Account settings") }
                    TextButton({ confirmSignOut = true }, Modifier.focusRing()) { Text("Sign out") }
                }
            }
        }
    }

    if (confirmSignOut) SignOutDialog(onDismiss = { confirmSignOut = false }, onSignedOut = onDismiss)
    asking?.let { p ->
        val pin = profileNeeds(p, isAdmin) == Profile.Requires.PIN
        SecretDialog(title = p.displayName, label = if (pin) "PIN" else "Password", numeric = pin, onDismiss = { asking = null }) { secret, _ ->
            runCatching { if (pin) switchTo(p, secret, null) else switchTo(p, null, secret) }.exceptionOrNull()?.let(::switchError)
        }
    }
}

/**
 * Sign out, once confirmed: on a TV remote it's one press away from Switch profile and the
 * page's other buttons, and signing in again needs the password (or Quick Connect).
 */
@Composable
internal fun SignOutDialog(onDismiss: () -> Unit, onSignedOut: () -> Unit = {}) {
    val marquee = LocalMarquee.current
    val me by marquee.me.collectAsState()
    androidx.compose.material3.AlertDialog(onDismissRequest = onDismiss,
        title = { Text("Sign out${me?.displayName?.let { " $it" } ?: ""}?") },
        text = { Text("This device goes back to Who's watching." + if (marquee.isTv) "" else " Downloads stay on it.") },
        confirmButton = { TextButton({ onDismiss(); onSignedOut(); marquee.scope.launch { marquee.signOut() } }, Modifier.focusRing()) { Text("Sign out") } },
        // Cancel first under the D-pad, so a stray press of OK doesn't sign out.
        dismissButton = { TextButton(onDismiss, Modifier.focusRing().initialFocus(marquee.isTv)) { Text("Cancel") } })
}
