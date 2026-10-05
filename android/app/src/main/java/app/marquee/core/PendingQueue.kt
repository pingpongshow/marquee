package app.marquee.core

import app.marquee.api.infrastructure.ClientException
import app.marquee.api.infrastructure.ServerException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import java.io.IOException

/**
 * One change a person made that the server hasn't got yet (USER-18): a rating, watched or
 * unwatched, a watchlist add or remove, a comment set or deleted, or the progress of a play.
 * Scoped to a server and a person, and stamped with when it was made ([atMs]), which the
 * server uses to ignore it if something newer happened elsewhere meanwhile.
 */
@Serializable
data class PendingChange(
    val server: String,
    val user: Long,
    val item: Long,
    val kind: Kind,
    val atMs: Long,
    /** Rating: 0–10, or null to clear. */
    val rating: Double? = null,
    /** Watched and Watchlist: on (watched, added) or off. */
    val on: Boolean = true,
    /** Comment: the text; empty deletes your own. */
    val comment: String? = null,
    /** Comment: someone else's to delete (admins). */
    val reviewUser: Long? = null,
    /** Progress. */
    val positionMs: Long = 0,
    val watched: Boolean = false,
    /** Order the changes were made in; set by the queue. */
    val seq: Long = 0,
) {
    enum class Kind { Rating, Watched, Watchlist, Comment, Progress }

    /** Only the latest change per key is kept. */
    data class Key(val server: String, val user: Long, val item: Long, val kind: Kind, val reviewUser: Long?)
    val key: Key get() = Key(server, user, item, kind, reviewUser)
}

/**
 * The device's queue of [PendingChange]s, kept in a file so it survives restarts. Pure Kotlin
 * (the storage is passed in), so it's unit tested on the JVM.
 */
class PendingQueue(private val storage: Storage) {
    interface Storage {
        fun read(): String?
        fun write(text: String)
    }

    /** What to do with a change after trying to send it. */
    enum class Outcome { Sent, Drop, KeepSignedOut, KeepOffline }

    data class FlushResult(val sent: Int, val dropped: Int, val stopped: Outcome?)

    private val json = Json { ignoreUnknownKeys = true }
    private val serializer = ListSerializer(PendingChange.serializer())
    private val lock = Any()
    private val _changes = MutableStateFlow(load())
    /** Every queued change, for every server and person. */
    val changes: StateFlow<List<PendingChange>> = _changes

    private fun load(): List<PendingChange> =
        runCatching { storage.read()?.let { json.decodeFromString(serializer, it) } }.getOrNull().orEmpty().sortedBy { it.seq }

    private fun update(f: (List<PendingChange>) -> List<PendingChange>) = synchronized(lock) {
        val next = f(_changes.value)
        if (next == _changes.value) return@synchronized
        _changes.value = next
        runCatching { storage.write(json.encodeToString(serializer, next)) }
    }

    /**
     * Queues a change, replacing any earlier one for the same item and kind. An older change
     * than the one queued is ignored. Progress keeps the furthest play: a finished play isn't
     * replaced by a later partial one (only its time moves on).
     */
    fun add(c: PendingChange): PendingChange {
        var result = c
        update { list ->
            val old = list.firstOrNull { it.key == c.key }
            var merged = c
            if (old != null) {
                if (c.kind == PendingChange.Kind.Progress) {
                    if (old.watched && !c.watched) merged = old.copy(atMs = maxOf(old.atMs, c.atMs))
                } else if (old.atMs > c.atMs) merged = old
            }
            merged = merged.copy(seq = (list.maxOfOrNull { it.seq } ?: 0) + 1)
            result = merged
            list.filterNot { it.key == c.key } + merged
        }
        return result
    }

    /** One person's changes on one server, in the order they were made. */
    fun pending(server: String, user: Long): List<PendingChange> =
        _changes.value.filter { it.server == server && it.user == user }.sortedBy { it.seq }

    fun count(server: String?, user: Long?): Int =
        if (server == null || user == null) 0 else _changes.value.count { it.server == server && it.user == user }

    /** The queued change for an item, if any (screens show it rather than the server's older value). */
    fun latest(server: String?, user: Long?, item: Long, kind: PendingChange.Kind): PendingChange? =
        if (server == null || user == null) null
        else _changes.value.firstOrNull { it.server == server && it.user == user && it.item == item && it.kind == kind && it.reviewUser == null }

    /** Removes exactly this entry; one queued after it (for the same item) stays. */
    fun remove(c: PendingChange) = update { list -> list.filterNot { it == c } }

    /** A newer change reached the server directly: what was queued for the same item and kind is moot. */
    fun superseded(c: PendingChange) = update { list -> list.filterNot { it.key == c.key && it.atMs <= c.atMs } }

    /**
     * Sends one person's queue in order. Sent and refused changes leave the queue; on 401 or a
     * network problem it stops and keeps the rest for later.
     */
    suspend fun flush(server: String, user: Long, send: suspend (PendingChange) -> Unit): FlushResult {
        var sent = 0
        var dropped = 0
        for (c in pending(server, user)) {
            val error = try { send(c); null } catch (e: kotlinx.coroutines.CancellationException) { throw e } catch (e: Throwable) { e }
            when (val o = outcome(error)) {
                Outcome.Sent -> { remove(c); sent++ }
                Outcome.Drop -> { remove(c); dropped++ }
                else -> return FlushResult(sent, dropped, o)
            }
        }
        return FlushResult(sent, dropped, null)
    }

    companion object {
        /** 2xx: sent. 401: keep until signed in. Other 4xx: drop. Network trouble and 5xx: keep, retry later. */
        fun outcome(error: Throwable?): Outcome = when {
            error == null -> Outcome.Sent
            error is ClientException && error.statusCode == 401 -> Outcome.KeepSignedOut
            error is ClientException -> Outcome.Drop
            error is ServerException -> Outcome.KeepOffline
            error is IOException -> Outcome.KeepOffline
            else -> Outcome.Drop
        }
    }
}
