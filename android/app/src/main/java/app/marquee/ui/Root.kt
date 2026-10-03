package app.marquee.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import app.marquee.core.Marquee

val LocalMarquee = staticCompositionLocalOf<Marquee> { error("no Marquee") }
val LocalMusic = staticCompositionLocalOf<app.marquee.music.MusicController> { error("no music") }
val LocalDownloads = staticCompositionLocalOf<app.marquee.core.Downloads> { error("no downloads") }
val LocalRemote = staticCompositionLocalOf<app.marquee.core.RemoteReceiver> { error("no remote") }

/** Server → sign-in → app. */
@Composable
fun MarqueeRoot(marquee: Marquee, music: app.marquee.music.MusicController, downloads: app.marquee.core.Downloads, remote: app.marquee.core.RemoteReceiver) {
    val state by marquee.state.collectAsState()
    LaunchedEffect(Unit) { if (marquee.server != null) marquee.reconnect() }
    // Signed in (even offline): resume downloads and send plays made offline.
    LaunchedEffect(state) { if (state == Marquee.State.SignedIn) downloads.attach() }
    CompositionLocalProvider(LocalMarquee provides marquee, LocalMusic provides music, LocalDownloads provides downloads, LocalRemote provides remote) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
            when (state) {
                Marquee.State.NoServer -> ConnectScreen()
                Marquee.State.Connecting -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                        CircularProgressIndicator()
                        Text("Connecting to ${marquee.server?.name ?: "your server"}…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                Marquee.State.SignedOut -> SignInScreen()
                Marquee.State.SignedIn -> MainScreen()
            }
        }
    }
}
