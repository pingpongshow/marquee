package app.marquee.ui

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.LocalContext
import app.marquee.api.models.ItemSummary
import app.marquee.core.Marquee
import app.marquee.core.PendingChange
import app.marquee.core.WatchChoices
import app.marquee.core.WatchState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** A toast from any thread (Toast needs the main looper). */
fun toast(context: Context, text: String, long: Boolean = false) {
    val show = { Toast.makeText(context.applicationContext, text, if (long) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show() }
    if (Looper.myLooper() == Looper.getMainLooper()) show() else Handler(Looper.getMainLooper()).post(show)
}

/** Whether a video is watched, with this device's marks and queued changes over the server's value. */
@Composable
fun isWatched(item: ItemSummary): Boolean {
    val marquee = LocalMarquee.current
    val marks by marquee.watch.marks.collectAsState()
    return watchedNow(marquee, item, marks)
}

fun watchedNow(marquee: Marquee, item: ItemSummary, marks: Map<Long, WatchState.Mark> = marquee.watch.marks.value): Boolean =
    marquee.watch.markFor(item, marks)?.on ?: marquee.sync.queued(item.id, PendingChange.Kind.Watched)?.on ?: WatchState.serverWatched(item)

/** What a menu offers for a video, show or season (PLAY-5), with this device's marks and queued changes over the server's values. */
@Composable
fun watchChoices(item: ItemSummary): WatchChoices {
    val marquee = LocalMarquee.current
    val marks by marquee.watch.marks.collectAsState()
    return WatchState.choices(item, marquee.watch.markFor(item, marks)?.on ?: marquee.sync.queued(item.id, PendingChange.Kind.Watched)?.on)
}

/**
 * A tile's context menu (USER-6): Mark as watched and/or unwatched for videos, shows and seasons (both while part-way through),
 * Add to / Remove from favourites (USER-12), and Remove from Continue Watching on that row's tiles.
 */
@Composable
fun tileActions(item: ItemSummary, continueWatching: Boolean = false, onRemoved: (Long) -> Unit = {}): List<TileAction> {
    val marquee = LocalMarquee.current
    val context = LocalContext.current
    // The app's scope: the tile may leave the screen (its row reloads) before the request ends.
    val scope = marquee.scope
    val choices = watchChoices(item)
    val favorite = favoriteAction(item)
    // Either way it leaves Continue Watching: its row hides marked items until it reloads.
    fun setWatched(on: Boolean) = scope.launch { if (!marquee.sync.setWatched(item.id, on)) toast(context, "Couldn't update “${item.title}”") }
    return buildList {
        if (item.type in WatchState.videoTypes) {
            if (choices.markWatched) add(TileAction("Mark as watched") { setWatched(true) })
            if (choices.markUnwatched) add(TileAction("Mark as unwatched") { setWatched(false) })
        }
        favorite?.let(::add)
        if (continueWatching && !marquee.isOffline) add(TileAction("Remove from Continue Watching") {
            scope.launch {
                withContext(Dispatchers.IO) { runCatching { marquee.hubs.removeFromContinueWatching(item.id) } }
                    .onSuccess { withContext(Dispatchers.Main) { onRemoved(item.id) }; marquee.watch.changed() }
                    .onFailure { toast(context, "Couldn't remove “${item.title}”: ${it.message ?: ""}") }
            }
        })
    }
}
