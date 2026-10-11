package app.marquee.ui

import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.webkit.JavascriptInterface
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import app.marquee.api.models.ItemTrailer

/**
 * Trailers for movies and shows (PLAY-22): a local trailer plays in the video player; a YouTube one
 * plays in Marquee, in YouTube's official embedded player shown full screen (nothing is downloaded
 * or extracted). The YouTube app is only opened from the error fallback, or on a TV without WebView.
 */
object Trailers {
    /** UI tests: a canned answer in place of the server's (the test server has no TMDB key, so no YouTube trailers). */
    @androidx.annotation.VisibleForTesting
    @Volatile var fake: ItemTrailer? = null

    /** Whether this device has a working WebView (some TVs don't). */
    fun webViewAvailable(): Boolean = runCatching { WebView.getCurrentWebViewPackage() != null }.getOrDefault(false)

    /** The YouTube app when it's installed, else the browser; a message when neither can open it (some TVs). */
    fun openYouTube(context: Context, key: String, title: String) {
        val app = Intent(Intent.ACTION_VIEW, Uri.parse("vnd.youtube:$key"))
        val web = Intent(Intent.ACTION_VIEW, Uri.parse("https://www.youtube.com/watch?v=$key"))
        if (context !is android.app.Activity) { app.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK); web.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) }
        try { context.startActivity(app) } catch (_: ActivityNotFoundException) {
            try { context.startActivity(web) } catch (_: ActivityNotFoundException) {
                Toast.makeText(context, "Watch the trailer on YouTube: $title", Toast.LENGTH_LONG).show()
            }
        }
    }

    /** The page the web view loads: YouTube's IFrame player on youtube-nocookie.com, reporting errors back. */
    internal fun embedHtml(key: String): String {
        val k = key.filter { it.isLetterOrDigit() || it == '-' || it == '_' }
        return """
<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
<style>html,body{margin:0;height:100%;background:#000;overflow:hidden}#p{position:absolute;top:0;left:0;width:100%;height:100%;border:0}</style>
</head><body><div id="p"></div>
<script>
var player;
function fail(c){try{Marquee.error(c)}catch(e){}}
function onYouTubeIframeAPIReady(){
  player=new YT.Player('p',{host:'https://www.youtube-nocookie.com',videoId:'$k',
    playerVars:{autoplay:1,playsinline:1,rel:0,modestbranding:1,fs:1,origin:'https://marquee.app'},
    events:{onReady:function(e){e.target.playVideo()},onStateChange:function(e){try{Marquee.state(e.data)}catch(x){}},onError:function(e){fail(e.data)}}});
}
function toggle(){if(!player||!player.getPlayerState)return;if(player.getPlayerState()==1)player.pauseVideo();else player.playVideo();}
</script>
<script src="https://www.youtube.com/iframe_api" onerror="fail(-1)"></script>
</body></html>
""".trimIndent()
    }

    /** What to say when the embed can't play: 101/150/152/153 mean the owner doesn't allow embedding. */
    internal fun errorMessage(code: Int): String = when (code) {
        101, 150, 152, 153 -> "This trailer's owner doesn't allow it to play outside YouTube."
        -1 -> "Couldn't reach YouTube to play this trailer."
        else -> "This trailer can't be played here."
    }
}

/** A web view for the trailer: OK and play/pause on a remote toggle playback (Android TV). */
@SuppressLint("ViewConstructor")
private class TrailerWebView(context: Context) : WebView(context) {
    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        when (event.keyCode) {
            KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER,
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_MEDIA_PLAY, KeyEvent.KEYCODE_MEDIA_PAUSE -> {
                if (event.action == KeyEvent.ACTION_UP) evaluateJavascript("toggle()", null)
                return true
            }
        }
        return super.dispatchKeyEvent(event)
    }
}

/**
 * A YouTube trailer, full screen inside Marquee (PLAY-22): black, the embedded player, a close
 * button top-left; Back closes. If the embed fails, a message and a "Watch on YouTube" fallback.
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
fun YouTubeTrailerPlayer(key: String, title: String, onClose: () -> Unit) {
    val context = LocalContext.current
    var error by remember(key) { mutableStateOf<Int?>(null) }
    var playing by remember(key) { mutableStateOf(false) } // YouTube's player state, for TalkBack and the UI tests
    Dialog(onClose, DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        // Full screen: no system bars over the trailer (a swipe brings them back for a moment).
        val window = (LocalView.current.parent as? DialogWindowProvider)?.window
        DisposableEffect(window) {
            window?.let {
                it.setLayout(android.view.ViewGroup.LayoutParams.MATCH_PARENT, android.view.ViewGroup.LayoutParams.MATCH_PARENT)
                it.setDimAmount(0f)
                WindowCompat.setDecorFitsSystemWindows(it, false)
                if (android.os.Build.VERSION.SDK_INT >= 28) it.attributes = it.attributes.apply {
                    layoutInDisplayCutoutMode = android.view.WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
                }
                val bars = WindowCompat.getInsetsController(it, it.decorView)
                bars.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
                bars.hide(WindowInsetsCompat.Type.systemBars())
            }
            onDispose { }
        }
        Box(Modifier.fillMaxSize().background(Color.Black).testTag("trailerPlayer")) {
            val e = error
            if (e == null) {
                val main = remember { Handler(Looper.getMainLooper()) }
                AndroidView(
                    factory = { ctx ->
                        TrailerWebView(ctx).apply {
                            setBackgroundColor(android.graphics.Color.BLACK)
                            settings.javaScriptEnabled = true
                            settings.domStorageEnabled = true
                            settings.mediaPlaybackRequiresUserGesture = false
                            settings.setSupportMultipleWindows(false)
                            addJavascriptInterface(object {
                                @JavascriptInterface fun error(code: Int) { main.post { error = code } }
                                @JavascriptInterface fun state(code: Int) { main.post { playing = code == 1 } }
                            }, "Marquee")
                            webViewClient = object : WebViewClient() {
                                // YouTube's pages stay in this web view; app links (vnd.youtube:, intent:) go nowhere.
                                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean =
                                    request.url.scheme !in setOf("http", "https")
                            }
                            loadDataWithBaseURL("https://marquee.app", Trailers.embedHtml(key), "text/html", "utf-8", null)
                            isFocusable = true
                            post { requestFocus() }
                        }
                    },
                    modifier = Modifier.fillMaxSize().testTag("trailerWebView")
                        .semantics { contentDescription = "Trailer"; stateDescription = if (playing) "Playing" else "Paused" },
                    onRelease = { it.stopLoading(); it.loadUrl("about:blank"); it.destroy() },
                )
            } else {
                Column(Modifier.align(Alignment.Center).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(Trailers.errorMessage(e), color = Color.White, textAlign = TextAlign.Center)
                    Button({ Trailers.openYouTube(context, key, title) }, Modifier.padding(top = 16.dp).focusRing().initialFocus(true)) {
                        Text("Watch on YouTube")
                    }
                }
            }
            // Same look as the video player's close: one plain icon button, no extra ring.
            IconButton(onClose, Modifier.align(Alignment.TopStart).safeDrawingPadding().padding(12.dp).size(48.dp)
                .background(Color.Black.copy(alpha = 0.45f), CircleShape).focusRing()) {
                Icon(Icons.Filled.Close, "Close trailer", tint = Color.White)
            }
        }
    }
}
