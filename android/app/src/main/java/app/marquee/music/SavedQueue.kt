package app.marquee.music

import android.util.AtomicFile
import app.marquee.api.infrastructure.Serializer
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.RadioRequest
import kotlinx.serialization.Serializable
import java.io.File

/**
 * The music queue as saved on the device, so it can carry on where it stopped after the system
 * ended the app in the background: the tracks, which one was playing and how far in, and what
 * they were playing from. It's one person's on one server ([owner]); someone else's is ignored.
 */
@Serializable
data class SavedQueue(
    val owner: String,
    /** Each track in the queue once, with everything needed to play it again. */
    val tracks: List<ItemSummary>,
    /** The queue in order (a track can be in it more than once). */
    val entries: List<Entry>,
    val index: Int,
    val positionMs: Long = 0,
    val source: String? = null,
    val radio: RadioRequest? = null,
    val context: String? = null,
    val generated: Boolean = false,
    val shuffle: Boolean = false,
    val repeat: Int = 0,
) {
    @Serializable
    data class Entry(val id: Long, val dj: String? = null, val context: String? = null, val radio: Boolean = false)

    /** The entries whose tracks were saved, and where the current one is among them (null when nothing's left). */
    fun playable(): Pair<List<Entry>, Int>? {
        val have = tracks.mapTo(HashSet()) { it.id }
        val keep = entries.withIndex().filter { it.value.id in have }
        if (keep.isEmpty()) return null
        val at = keep.indexOfFirst { it.index == index }.takeIf { it >= 0 } ?: keep.indexOfFirst { it.index > index }.takeIf { it >= 0 } ?: 0
        return keep.map { it.value } to at
    }

    companion object {
        /** Whose music it is: the server and the person signed in. */
        fun owner(server: String?, user: Long?): String? = if (server == null || user == null) null else "$server|$user"

        private val json get() = Serializer.kotlinxSerializationJson

        fun encode(q: SavedQueue): String = json.encodeToString(serializer(), q)
        fun decode(text: String): SavedQueue? = runCatching { json.decodeFromString(serializer(), text) }.getOrNull()

        fun read(file: File): SavedQueue? =
            runCatching { if (file.exists()) decode(String(AtomicFile(file).readFully())) else null }.getOrNull()

        fun write(file: File, q: SavedQueue) {
            val a = AtomicFile(file)
            val out = a.startWrite()
            runCatching { out.write(encode(q).toByteArray()); a.finishWrite(out) }.onFailure { a.failWrite(out) }
        }
    }
}
