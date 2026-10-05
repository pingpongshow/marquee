package app.marquee.core

import android.content.Context
import android.util.AtomicFile
import android.widget.Toast
import app.marquee.api.models.RateItemRequest
import app.marquee.api.models.SetReviewRequest
import app.marquee.api.models.SyncProgressRequest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import java.io.File
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneOffset

/**
 * Offline sync (USER-18): ratings, watched, watchlist, comments and the progress of plays made
 * while the server can't be reached are queued on the device (per server and person) and sent,
 * with when they were made, once it's back: on signing in, when the network returns, when the
 * app comes to the foreground, and with backoff while any remain.
 */
class OfflineSync(context: Context, private val marquee: Marquee) {
    enum class Result { Saved, Queued }

    private val file = File(context.filesDir, "pending-changes.json")
    private val legacyFile = File(context.filesDir, "downloads-pending.json")
    private val prefs = context.getSharedPreferences("marquee.sync", Context.MODE_PRIVATE)
    private val appContext = context.applicationContext

    val queue = PendingQueue(object : PendingQueue.Storage {
        override fun read(): String? = if (file.exists()) String(AtomicFile(file).readFully()) else null
        override fun write(text: String) {
            val a = AtomicFile(file)
            val out = a.startWrite()
            runCatching { out.write(text.toByteArray()); a.finishWrite(out) }.onFailure { a.failWrite(out) }
        }
    })
    private val mutex = Mutex()
    private var retry: Job? = null
    private var backoffMs = BACKOFF_START

    private val _lastSync = MutableStateFlow(prefs.getLong("last", 0L).takeIf { it > 0 })
    /** When queued changes last reached the server (epoch ms). */
    val lastSync: StateFlow<Long?> = _lastSync
    private val _syncs = MutableStateFlow(0)
    /** Bumped after queued changes were sent, so Now Playing and pages show the server's truth. */
    val syncs: StateFlow<Int> = _syncs

    /** The signed-in person's queued changes. */
    fun count(changes: List<PendingChange>): Int {
        val s = marquee.server?.id ?: return 0
        val u = marquee.userId ?: return 0
        return changes.count { it.server == s && it.user == u }
    }

    /** A queued change for an item, for screens to show over the server's older value. */
    fun queued(item: Long, kind: PendingChange.Kind): PendingChange? = queue.latest(marquee.server?.id, marquee.userId, item, kind)

    // Making changes

    fun rating(item: Long, rating: Double?) = change(item, PendingChange.Kind.Rating) { it.copy(rating = rating) }
    fun watched(item: Long, on: Boolean) = change(item, PendingChange.Kind.Watched) { it.copy(on = on) }
    fun watchlist(item: Long, on: Boolean) = change(item, PendingChange.Kind.Watchlist) { it.copy(on = on) }
    /** Your own comment; empty deletes it. */
    fun comment(item: Long, text: String) = change(item, PendingChange.Kind.Comment) { it.copy(comment = text) }
    /** Someone else's comment (admins). */
    fun deleteComment(item: Long, user: Long) = change(item, PendingChange.Kind.Comment) { it.copy(reviewUser = user) }

    private fun change(item: Long, kind: PendingChange.Kind, f: (PendingChange) -> PendingChange) =
        f(PendingChange(marquee.server?.id ?: "", marquee.userId ?: -1, item, kind, System.currentTimeMillis()))

    /**
     * Saves a change: straight to the server when it can be reached; queued when it can't
     * (offline, a timeout, a 5xx) so it's sent later. Throws on a refusal (4xx), which the
     * caller shows as before.
     */
    suspend fun save(c: PendingChange): Result {
        val scoped = c.server.isNotEmpty() && c.user >= 0
        if (marquee.baseUrl == null && scoped) return enqueue(c)
        val error = withContext(Dispatchers.IO) { runCatching { send(c) }.exceptionOrNull() }
        return when (PendingQueue.outcome(error)) {
            PendingQueue.Outcome.Sent -> { queue.superseded(c); Result.Saved }
            PendingQueue.Outcome.Drop -> throw error!!
            else -> if (scoped) enqueue(c) else throw error!!
        }
    }

    /** [save] from a screen: a short "Saved offline" note when queued; false when refused. */
    suspend fun saveShowing(c: PendingChange, context: Context = appContext): Boolean = try {
        if (save(c) == Result.Queued) withContext(Dispatchers.Main) {
            Toast.makeText(context, "Saved offline, will sync", Toast.LENGTH_SHORT).show()
        }
        true
    } catch (e: kotlinx.coroutines.CancellationException) { throw e } catch (e: Throwable) { false }

    private fun enqueue(c: PendingChange): Result {
        queue.add(c)
        scheduleRetry()
        return Result.Queued
    }

    /** The progress of a play of a download: always queued, then sent when possible. */
    fun recordProgress(item: Long, positionMs: Long, watched: Boolean) {
        val s = marquee.server?.id ?: return
        val u = marquee.userId ?: return
        queue.add(PendingChange(s, u, item, PendingChange.Kind.Progress, System.currentTimeMillis(), positionMs = positionMs, watched = watched))
        marquee.scope.launch { flush() }
    }

    // Sending

    private fun send(c: PendingChange) {
        val at = OffsetDateTime.ofInstant(Instant.ofEpochMilli(c.atMs), ZoneOffset.UTC)
        val items = marquee.items
        when (c.kind) {
            PendingChange.Kind.Rating -> items.rateItem(c.item, RateItemRequest(c.rating, at))
            PendingChange.Kind.Watched -> if (c.on) items.markWatched(c.item, at) else items.markUnwatched(c.item, at)
            PendingChange.Kind.Watchlist -> if (c.on) items.addToWatchlist(c.item, at) else items.removeFromWatchlist(c.item, at)
            PendingChange.Kind.Comment -> c.reviewUser?.let { items.deleteReview(c.item, it, at) }
                ?: items.setReview(c.item, SetReviewRequest(c.comment.orEmpty(), at))
            PendingChange.Kind.Progress -> marquee.playback.syncProgress(c.item, SyncProgressRequest(c.positionMs, c.watched, at))
        }
    }

    /** Sends the signed-in person's queue (reconnecting first if the app is offline). */
    suspend fun syncNow() {
        if (marquee.token == null) return
        if (count(queue.changes.value) == 0 && !legacyFile.exists()) return
        if (marquee.baseUrl == null) marquee.reconnect(quiet = true)
        if (marquee.baseUrl == null) { scheduleRetry(); return }
        flush()
    }

    /** Sends the queue if the server is reachable; keeps what can't go yet. */
    suspend fun flush() = mutex.withLock {
        migrateLegacy()
        val s = marquee.server?.id ?: return@withLock
        val u = marquee.userId ?: return@withLock
        if (marquee.token == null || marquee.baseUrl == null || queue.pending(s, u).isEmpty()) return@withLock
        val r = withContext(Dispatchers.IO) { queue.flush(s, u) { send(it) } }
        if (r.sent > 0 || r.dropped > 0) {
            val now = System.currentTimeMillis()
            _lastSync.value = now
            prefs.edit().putLong("last", now).apply()
            _syncs.value++
            marquee.refresh()
        }
        if (r.stopped == PendingQueue.Outcome.KeepOffline) scheduleRetry() else if (r.stopped == null) backoffMs = BACKOFF_START
    }

    /** Tries again later, waiting longer each time (5 s up to 5 min) while changes remain. */
    private fun scheduleRetry() {
        if (retry?.isActive == true) return
        val wait = backoffMs
        backoffMs = (backoffMs * 2).coerceAtMost(BACKOFF_MAX)
        retry = marquee.scope.launch {
            delay(wait)
            retry = null
            syncNow()
        }
    }

    // Plays of downloads queued before offline sync (no person recorded): they belong to whoever
    // is signed in on this device now.
    @Serializable
    private data class LegacyPending(val itemId: Long, val positionMs: Long, val watched: Boolean, val atMs: Long)

    private fun migrateLegacy() {
        if (!legacyFile.exists()) return
        val s = marquee.server?.id ?: return
        val u = marquee.userId ?: return
        runCatching {
            Json { ignoreUnknownKeys = true }.decodeFromString(ListSerializer(LegacyPending.serializer()), String(AtomicFile(legacyFile).readFully()))
        }.getOrNull().orEmpty().forEach {
            queue.add(PendingChange(s, u, it.itemId, PendingChange.Kind.Progress, it.atMs, positionMs = it.positionMs, watched = it.watched))
        }
        AtomicFile(legacyFile).delete()
    }

    private companion object {
        const val BACKOFF_START = 5_000L
        const val BACKOFF_MAX = 300_000L
    }
}
