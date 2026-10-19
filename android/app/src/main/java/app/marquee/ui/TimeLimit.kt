package app.marquee.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties

/**
 * Kids' time limits: when video stopped (or couldn't start) because the time is up or it's
 * outside the viewing hours, the reason fills the screen until OK.
 */
@Composable
fun TimeLimitHost() {
    val marquee = LocalMarquee.current
    val reason by marquee.timeLimits.blocked.collectAsState()
    val r = reason ?: return
    Dialog(onDismissRequest = marquee.timeLimits::dismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Column(
            Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background).padding(32.dp).semantics { contentDescription = "Time limit" },
            verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(Icons.Filled.Timer, null, Modifier.size(64.dp), tint = Gold)
            Text("Time's up", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
            Text(r, Modifier.widthIn(max = 520.dp), style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
            Button(marquee.timeLimits::dismiss, Modifier.padding(top = 8.dp).focusRing().initialFocus(marquee.isTv)) { Text("OK") }
        }
    }
}
