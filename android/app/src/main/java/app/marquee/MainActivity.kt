package app.marquee

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import app.marquee.ui.MarqueeRoot
import app.marquee.ui.MarqueeTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Dark UI everywhere: light status and navigation bar icons.
        enableEdgeToEdge(SystemBarStyle.dark(android.graphics.Color.TRANSPARENT), SystemBarStyle.dark(android.graphics.Color.TRANSPARENT))
        val app = application as MarqueeApplication
        val marquee = app.marquee
        // UI tests start from a clean slate.
        if (intent.getBooleanExtra("marquee-reset", false)) {
            app.music.stop()
            marquee.reset()
        }
        if (!marquee.isTv) marquee.cast.init() // Chromecast sessions (D82)
        setContent { MarqueeTheme { MarqueeRoot(marquee, app.music, app.downloads, app.remote) } }
        playFromSearch(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        playFromSearch(intent)
    }

    /** "Play … on Marquee" from Assistant: the music service finds and plays the best match. */
    private fun playFromSearch(intent: Intent?) {
        if (intent?.action != android.provider.MediaStore.INTENT_ACTION_MEDIA_PLAY_FROM_SEARCH) return
        (application as MarqueeApplication).music.playFromSearch(intent.getStringExtra(android.app.SearchManager.QUERY).orEmpty())
    }
}
