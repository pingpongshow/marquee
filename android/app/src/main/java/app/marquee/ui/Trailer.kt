package app.marquee.ui

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import app.marquee.api.models.ItemTrailer

/** Trailers for movies and shows (PLAY-22): a local trailer plays here; a YouTube one opens in YouTube. Nothing is downloaded. */
object Trailers {
    /** UI tests: a canned answer in place of the server's (the test server has no TMDB key, so no YouTube trailers). */
    @androidx.annotation.VisibleForTesting
    @Volatile var fake: ItemTrailer? = null

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
}
