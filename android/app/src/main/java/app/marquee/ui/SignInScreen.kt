package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.platform.testTag
import app.marquee.api.infrastructure.ClientError
import app.marquee.api.infrastructure.ClientException
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.marquee.api.models.Profile
import app.marquee.api.models.QuickConnectState
import app.marquee.api.models.StartQuickConnectRequest
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** "Who's watching?": profiles (PIN or password when set), a username sign-in and Quick Connect. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SignInScreen() {
    val marquee = LocalMarquee.current
    val profiles by produceState<List<Profile>?>(null) { value = withContext(Dispatchers.IO) { runCatching { marquee.auth.listSignInProfiles() }.getOrDefault(emptyList()) } }
    var asking by remember { mutableStateOf<Profile?>(null) }
    var manual by remember { mutableStateOf(false) }
    var quick by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Spacer(Modifier.height(40.dp))
        Text("Who's watching?", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Text(marquee.server?.name ?: "", color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(bottom = 28.dp))
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(bottom = 12.dp)) }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(24.dp, Alignment.CenterHorizontally), verticalArrangement = Arrangement.spacedBy(24.dp)) {
            profiles?.forEach { p ->
                Column(Modifier.width(110.dp).focusCard({
                    error = null
                    if (p.requires == Profile.Requires.NONE) scope.launch {
                        runCatching { marquee.signInProfile(p.id, null, null) }.onFailure { error = it.message }
                    } else asking = p
                }), horizontalAlignment = Alignment.CenterHorizontally) {
                    Avatar(p.displayName, marquee.absolute(p.avatarUrl), 96)
                    Text(p.displayName, Modifier.padding(top = 8.dp, bottom = 4.dp), maxLines = 1)
                }
            }
        }
        Spacer(Modifier.height(32.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            TextButton(onClick = { manual = true }) { Text("Sign in with a username") }
            TextButton(onClick = { quick = true }) { Text("Use Quick Connect") }
        }
        TextButton(onClick = { marquee.forgetServer() }) { Text("Use a different server", color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }

    asking?.let { p ->
        SecretDialog(
            title = p.displayName,
            label = if (p.requires == Profile.Requires.PIN) "PIN" else "Password",
            numeric = p.requires == Profile.Requires.PIN,
            onDismiss = { asking = null },
        ) { secret, code ->
            runCatching {
                if (p.requires == Profile.Requires.PIN) marquee.signInProfile(p.id, secret, null) else marquee.signInProfile(p.id, null, secret, code)
            }.exceptionOrNull()?.let { signInError(it, "That didn't work. Try again.") }
        }
    }
    if (manual) UsernameDialog(onDismiss = { manual = false })
    if (quick) QuickConnectDialog(onDismiss = { quick = false })
}

@Composable
fun Avatar(name: String, url: String?, size: Int) {
    Box(Modifier.size(size.dp).clip(CircleShape).background(Surface2), contentAlignment = Alignment.Center) {
        Text(name.take(1).uppercase(), fontSize = (size / 2.5).sp, fontWeight = FontWeight.Bold, color = Gold)
        if (url != null) AsyncImage(url, null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
    }
}

/** Returned by a sign-in when the account also needs an authenticator code (USER-9). */
private const val NEEDS_CODE = "Enter the code from your authenticator app, or a recovery code."

/** The message for a failed sign-in: [NEEDS_CODE] when two-factor is on, else [fallback]. */
private fun signInError(e: Throwable, fallback: String): String {
    val body = ((e as? ClientException)?.response as? ClientError<*>)?.body?.toString() ?: ""
    return when {
        "totp_required" in body -> NEEDS_CODE
        "invalid_totp" in body -> "That code didn't work. Try the newest one."
        else -> fallback
    }
}

@Composable
private fun CodeField(code: String, onChange: (String) -> Unit) {
    OutlinedTextField(code, onChange, label = { Text("Authenticator code") }, singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii), modifier = Modifier.testTag("totpCode"))
}

/** Asks for a PIN or password (and an authenticator code when needed); submit returns an error message, or null when it worked. */
@Composable
private fun SecretDialog(title: String, label: String, numeric: Boolean, onDismiss: () -> Unit, submit: suspend (String, String?) -> String?) {
    var value by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var needsCode by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column {
                OutlinedTextField(value, { value = it }, label = { Text(label) }, singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = if (numeric) KeyboardType.NumberPassword else KeyboardType.Password))
                if (needsCode) CodeField(code) { code = it }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            Button(onClick = { scope.launch { error = submit(value, code.takeIf { needsCode }); if (error == NEEDS_CODE) needsCode = true } },
                enabled = value.isNotEmpty() && (!needsCode || code.isNotBlank())) { Text("Sign In") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun UsernameDialog(onDismiss: () -> Unit) {
    val marquee = LocalMarquee.current
    var user by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var needsCode by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Sign in") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(user, { user = it }, label = { Text("Username") }, singleLine = true)
                OutlinedTextField(pass, { pass = it }, label = { Text("Password") }, singleLine = true, visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
                if (needsCode) CodeField(code) { code = it }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            Button(onClick = {
                scope.launch {
                    runCatching { marquee.signIn(user, pass, code.takeIf { needsCode }) }.onFailure {
                        error = signInError(it, "Wrong username or password.")
                        if (error == NEEDS_CODE) needsCode = true
                    }
                }
            }, enabled = user.isNotBlank() && pass.isNotEmpty() && (!needsCode || code.isNotBlank())) { Text("Sign In") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** Shows a code to approve from a signed-in phone or the web (D51). */
@Composable
private fun QuickConnectDialog(onDismiss: () -> Unit) {
    val marquee = LocalMarquee.current
    var code by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        runCatching {
            val start = withContext(Dispatchers.IO) { marquee.auth.startQuickConnect(StartQuickConnectRequest(marquee.device)) }
            code = start.code.chunked(1).joinToString(" ")
            while (true) {
                delay(start.pollIntervalMs.toLong())
                val st = withContext(Dispatchers.IO) { marquee.auth.pollQuickConnect(start.secret) }
                when (st.status) {
                    QuickConnectState.Status.APPROVED -> { st.auth?.let { marquee.finish(it) }; return@runCatching }
                    QuickConnectState.Status.EXPIRED -> { error = "The code expired. Start again."; return@runCatching }
                    else -> {}
                }
            }
        }.onFailure { error = it.message }
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Quick Connect") },
        text = {
            Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.fillMaxWidth()) {
                Text("On a signed-in phone or the web, open Settings → Link a TV and enter:", color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(code ?: "…", fontSize = 40.sp, fontWeight = FontWeight.Bold, color = Gold, modifier = Modifier.padding(vertical = 16.dp))
                error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
