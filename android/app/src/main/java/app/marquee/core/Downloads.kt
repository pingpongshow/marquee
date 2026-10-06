package app.marquee.core

import android.app.DownloadManager
import android.content.Context
import android.net.Uri
import android.util.AtomicFile
import app.marquee.api.infrastructure.Serializer
import app.marquee.api.models.CreateDownloadRequest
import app.marquee.api.models.Download
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import okhttp3.Request
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Offline downloads on phones and tablets (M7, MUSIC-14), as on the iPhone (D64): videos as
 * the original file or converted on the server, music tracks as they are. The system's
 * DownloadManager fetches them into app storage (background, resumable, with a notification).
 * Plays made offline are kept and sent when the server is reachable again (with everything
 * else changed offline, in [OfflineSync]).
 */
class Downloads(private val context: Context, private val marquee: Marquee) {
    enum class Quality(val label: String, val api: CreateDownloadRequest.Quality) {
        Original("Original", CreateDownloadRequest.Quality.ORIGINAL),
        High("High (1080p)", CreateDownloadRequest.Quality.HIGH),
        Medium("Medium (720p)", CreateDownloadRequest.Quality.MEDIUM),
        Low("Low (480p)", CreateDownloadRequest.Quality.LOW),
    }

    enum class State { Preparing, Downloading, Done, Failed }

    @Serializable
    data class Entry(
        val item: ItemSummary,
        val quality: Quality,
        val state: State,
        val progress: Double = 0.0,
        val error: String? = null,
        val file: String? = null,
        val poster: String? = null,
        val size: Long = 0,
        val jobId: String? = null,
        val url: String? = null,
        val transfer: Long? = null, // DownloadManager id
        val addedMs: Long = System.currentTimeMillis(),
        val resumeMs: Long? = null,
        /** Downloaded because a synced playlist has it (removed when no synced playlist does). */
        val viaPlaylist: Boolean = false,
    )

    private val dir = File(context.getExternalFilesDir(null) ?: context.filesDir, "downloads").apply { mkdirs() }
    private val indexFile = File(context.filesDir, "downloads.json")
    private val playlistsFile = File(context.filesDir, "downloads-playlists.json")
    private val json = Serializer.kotlinxSerializationJson
    private val dm = context.getSystemService(DownloadManager::class.java)
    /** For starting downloads from screens that may go away; Main, for Toasts. */
    val scope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + Dispatchers.Main)

    private val _entries = MutableStateFlow(load())
    val entries: StateFlow<Map<Long, Entry>> = _entries
    private var watcher: Job? = null

    private val playlistsSerializer = kotlinx.serialization.builtins.MapSerializer(Long.serializer(), kotlinx.serialization.builtins.SetSerializer(Long.serializer()))
    /** Playlists kept on the device (MUSIC-19): playlist id -> its items when last synced. */
    private val _playlists = MutableStateFlow<Map<Long, Set<Long>>>(
        runCatching { json.decodeFromString(playlistsSerializer, readFile(playlistsFile)) }.getOrDefault(emptyMap()),
    )
    val playlists: StateFlow<Map<Long, Set<Long>>> = _playlists

    private fun load(): Map<Long, Entry> = runCatching {
        json.decodeFromString(ListSerializer(Entry.serializer()), readFile(indexFile)).associateBy { it.item.id }
    }.getOrDefault(emptyMap())

    // Files are replaced whole (a new copy, then renamed over the old), so a crash or two
    // writers at once can never leave half a list behind.
    private fun readFile(f: File): String = String(AtomicFile(f).readFully())

    @Synchronized
    private fun writeFile(f: File, text: String) {
        val a = AtomicFile(f)
        val out = runCatching { a.startWrite() }.getOrNull() ?: return
        runCatching { out.write(text.toByteArray()); a.finishWrite(out) }.onFailure { a.failWrite(out) }
    }

    /** One writer for the index, so saves never interleave. */
    private val writer = Dispatchers.IO.limitedParallelism(1)
    private val dirty = AtomicBoolean(false)

    /**
     * Writes the index (the latest state, whoever asked) off the main thread. Progress
     * updates wait a few seconds so a busy transfer doesn't rewrite the file every second.
     */
    private fun save(soon: Boolean = false) {
        dirty.set(true)
        marquee.scope.launch(writer) {
            if (soon) delay(3000)
            if (dirty.getAndSet(false)) writeFile(indexFile, json.encodeToString(ListSerializer(Entry.serializer()), _entries.value.values.toList()))
        }
    }

    private fun set(id: Long, progressOnly: Boolean = false, f: (Entry) -> Entry) {
        _entries.update { m -> m[id]?.let { m + (id to f(it)) } ?: m }
        save(soon = progressOnly)
    }

    /** After signing in: resumes conversions and transfers, and sends what was changed offline. */
    fun attach() {
        watch()
        marquee.scope.launch {
            marquee.sync.syncNow()
            // Playlists kept on the device follow their changes.
            _playlists.value.keys.forEach { runCatching { syncPlaylist(it) } }
        }
    }

    // Playlists

    /** Keeps a playlist on the device: downloads what's new and drops what left it (tracks downloaded on their own stay). */
    suspend fun syncPlaylist(id: Long) {
        if (marquee.baseUrl == null) return
        val items = kotlinx.coroutines.withContext(Dispatchers.IO) { marquee.playlists.listPlaylistItems(id, limit = 2000).items.map { it.item } }
            .filter { it.type in listOf(ItemType.TRACK, ItemType.MOVIE, ItemType.EPISODE, ItemType.VIDEO) }
        val now = items.map { it.id }.toSet()
        val before = _playlists.value[id].orEmpty()
        _playlists.update { it + (id to now) }
        savePlaylists()
        for (it in items) if (_entries.value[it.id] == null) {
            runCatching { download(it, if (it.type == ItemType.TRACK) Quality.Original else Quality.Medium) }
            set(it.id) { e -> e.copy(viaPlaylist = true) }
        }
        dropOrphans(before - now)
    }

    fun unsyncPlaylist(id: Long) {
        val ids = _playlists.value[id] ?: return
        _playlists.update { it - id }
        savePlaylists()
        dropOrphans(ids)
    }

    private fun dropOrphans(ids: Set<Long>) {
        val kept = _playlists.value.values.flatten().toSet()
        ids.filter { it !in kept && _entries.value[it]?.viaPlaylist == true }.forEach(::remove)
    }

    private fun savePlaylists() {
        runCatching { writeFile(playlistsFile, json.encodeToString(playlistsSerializer, _playlists.value)) }
    }

    // Starting and removing

    /** Downloads a movie, episode, video or track. Tracks are always the original file. */
    suspend fun download(item: ItemSummary, quality: Quality) {
        val q = if (item.type == ItemType.TRACK) Quality.Original else quality
        val info = kotlinx.coroutines.withContext(Dispatchers.IO) { marquee.downloads.createDownload(CreateDownloadRequest(item.id, q.api)) }
        val entry = Entry(item, q, State.Preparing, info.progress, size = info.propertySize ?: 0,
            jobId = info.id.takeUnless { it.startsWith("original-") }, url = info.url)
        _entries.update { it + (item.id to entry) }
        save()
        if (info.status == Download.Status.READY && info.url != null) fetch(item.id, info.url!!, info.fileName)
        watch()
        marquee.scope.launch(Dispatchers.IO) { savePoster(item) }
        // Tracks keep their volume levelling data, for playing offline (MUSIC-10).
        if (item.type == ItemType.TRACK) marquee.scope.launch(Dispatchers.IO) { app.marquee.music.MusicService.fetchGains(marquee, item.id) }
    }

    /** Everything playable under a season, show, album or artist. */
    suspend fun downloadAll(container: ItemSummary, quality: Quality) {
        val leaves = kotlinx.coroutines.withContext(Dispatchers.IO) { marquee.items.itemLeaves(container.id) }
        for (it in leaves) if (_entries.value[it.id] == null) runCatching { download(it, quality) }
    }

    fun remove(id: Long) {
        val e = _entries.value[id] ?: return
        _entries.update { it - id }
        save()
        e.transfer?.let { runCatching { dm.remove(it) } }
        e.file?.let { File(dir, it).delete() }
        e.poster?.let { File(dir, it).delete() }
        e.jobId?.let { job -> marquee.scope.launch(Dispatchers.IO) { runCatching { marquee.downloads.deleteDownload(job) } } }
    }

    /** The downloaded file for an item, if it's complete. */
    fun localFile(id: Long): File? {
        val e = _entries.value[id] ?: return null
        if (e.state != State.Done || e.file == null) return null
        return File(dir, e.file).takeIf { it.exists() }
    }

    fun posterFile(id: Long): File? = _entries.value[id]?.poster?.let { File(dir, it) }?.takeIf { it.exists() }

    val totalBytes: Long get() = _entries.value.values.filter { it.state == State.Done }.sumOf { it.size }

    // Offline progress

    fun resumePosition(id: Long): Long = _entries.value[id]?.let { it.resumeMs ?: it.item.viewOffsetMs } ?: 0

    /** A play of a downloaded item: kept locally and sent to the server when it's reachable. */
    fun recordProgress(id: Long, positionMs: Long, watched: Boolean) {
        set(id) { it.copy(resumeMs = if (watched) 0 else positionMs) }
        marquee.sync.recordProgress(id, positionMs, watched)
    }

    // Transfers

    private fun fetch(id: Long, path: String, fileName: String? = null) {
        val e = _entries.value[id] ?: return
        val url = marquee.absolute(path) ?: return
        // The server's file name gives the extension; players sniff the format anyway.
        val ext = fileName?.substringAfterLast('.', "")?.takeIf { it.isNotEmpty() && it.length <= 5 }
            ?: if (e.quality == Quality.Original) (if (e.item.type == ItemType.TRACK) "audio" else "mkv") else "mp4"
        val name = "$id.$ext"
        File(dir, name).delete()
        val req = DownloadManager.Request(Uri.parse(url))
            .addRequestHeader("Authorization", "Bearer ${marquee.token}")
            .setTitle(e.item.title)
            .setDescription("Marquee download")
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE)
            .setDestinationUri(Uri.fromFile(File(dir, name)))
            .setAllowedOverMetered(true)
        val transfer = dm.enqueue(req)
        set(id) { it.copy(state = State.Downloading, progress = 0.0, file = name, transfer = transfer, url = path) }
    }

    /** Follows conversions on the server and transfers on the device while any are running. */
    private fun watch() {
        if (watcher?.isActive == true) return
        watcher = marquee.scope.launch {
            while (true) {
                val busy = _entries.value.values.filter { it.state == State.Preparing || it.state == State.Downloading }
                if (busy.isEmpty()) break
                for (e in busy) {
                    if (e.state == State.Preparing) checkConversion(e) else checkTransfer(e)
                }
                delay(1000)
            }
        }
    }

    private suspend fun checkConversion(e: Entry) {
        val job = e.jobId
        if (job == null) { e.url?.let { fetch(e.item.id, it) }; return }
        if (marquee.baseUrl == null) return
        val info = kotlinx.coroutines.withContext(Dispatchers.IO) { runCatching { marquee.downloads.getDownload(job) }.getOrNull() } ?: return
        when (info.status) {
            Download.Status.READY -> { set(e.item.id) { it.copy(size = info.propertySize ?: it.size) }; info.url?.let { fetch(e.item.id, it, info.fileName) } }
            Download.Status.FAILED -> set(e.item.id) { it.copy(state = State.Failed, error = info.error ?: "Conversion failed") }
            else -> set(e.item.id, progressOnly = true) { it.copy(progress = info.progress) }
        }
    }

    private fun checkTransfer(e: Entry) {
        val t = e.transfer ?: return
        dm.query(DownloadManager.Query().setFilterById(t)).use { c ->
            if (!c.moveToFirst()) { set(e.item.id) { it.copy(state = State.Failed, error = "The download was cancelled.") }; return }
            val status = c.getInt(c.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS))
            val done = c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR))
            val total = c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_TOTAL_SIZE_BYTES))
            when (status) {
                DownloadManager.STATUS_SUCCESSFUL -> {
                    val size = e.file?.let { File(dir, it).length() } ?: total
                    set(e.item.id) { it.copy(state = State.Done, progress = 1.0, size = size, transfer = null) }
                    // The converted copy on the server is no longer needed.
                    e.jobId?.let { job -> marquee.scope.launch(Dispatchers.IO) { runCatching { marquee.downloads.deleteDownload(job) } } }
                }
                DownloadManager.STATUS_FAILED -> {
                    val reason = c.getInt(c.getColumnIndexOrThrow(DownloadManager.COLUMN_REASON))
                    set(e.item.id) { it.copy(state = State.Failed, error = "Download failed ($reason)", transfer = null) }
                }
                else -> if (total > 0) set(e.item.id, progressOnly = true) { it.copy(progress = done.toDouble() / total) }
            }
        }
    }

    private fun savePoster(item: ItemSummary) {
        val art = if (item.type == ItemType.EPISODE) item.images?.thumb ?: item.images?.poster else item.images?.poster
        val url = marquee.imageUrl(art, 200) ?: return
        val name = "${item.id}.jpg"
        runCatching {
            marquee.http.newCall(Request.Builder().url(url).build()).execute().use { r ->
                if (!r.isSuccessful) return
                File(dir, name).outputStream().use { out -> r.body!!.byteStream().copyTo(out) }
            }
            set(item.id) { it.copy(poster = name) }
        }
    }
}
