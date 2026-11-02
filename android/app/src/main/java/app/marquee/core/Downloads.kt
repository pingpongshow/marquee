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
 * Offline downloads on phones and tablets (M7, MUSIC-14), as on the iPhone (D64): as the
 * original file or converted on the server, at the song and video quality chosen in Settings
 * (the recommended ones until then). The system's DownloadManager fetches them into app storage
 * (background, resumable, with a notification).
 * Plays made offline are kept and sent when the server is reachable again (with everything
 * else changed offline, in [OfflineSync]).
 */
class Downloads(private val context: Context, private val marquee: Marquee) {
    enum class Quality(val api: CreateDownloadRequest.Quality) {
        Original(CreateDownloadRequest.Quality.ORIGINAL),
        High(CreateDownloadRequest.Quality.HIGH),
        Medium(CreateDownloadRequest.Quality.MEDIUM),
        Low(CreateDownloadRequest.Quality.LOW),
    }

    enum class State { Preparing, Downloading, Done, Failed }

    /** Why smart downloads fetched something (it's removed again when no longer wanted). */
    enum class SmartKind { Shows, Music }

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
        val ownerId: Long? = null, // who asked for it: the server lists only the signed-in person's jobs
        val url: String? = null,
        val fileName: String? = null, // the server's, which gives the extension
        val transfer: Long? = null, // DownloadManager id
        val addedMs: Long = System.currentTimeMillis(),
        val resumeMs: Long? = null,
        /** Downloaded because a synced playlist has it (removed when no synced playlist does). */
        val viaPlaylist: Boolean = false,
        /** Downloaded by smart downloads, not by hand: removed again once watched or no longer wanted. */
        val smart: SmartKind? = null,
        /** Only fetched over Wi-Fi (an unmetered network). */
        val wifiOnly: Boolean = false,
    )

    private val dir = File(context.getExternalFilesDir(null) ?: context.filesDir, "downloads").apply { mkdirs() }
    private val indexFile = File(context.filesDir, "downloads.json")
    private val playlistsFile = File(context.filesDir, "downloads-playlists.json")
    private val playlistInfoFile = File(context.filesDir, "downloads-playlist-info.json")
    private val prefs = context.getSharedPreferences("marquee.downloads", Context.MODE_PRIVATE)
    private val json = Serializer.kotlinxSerializationJson
    private val dm = context.getSystemService(DownloadManager::class.java)
    /** For starting downloads from screens that may go away; Main, for Toasts. */
    val scope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + Dispatchers.Main)

    /** Smart downloads (phones and tablets): episodes ahead and favourite music, kept up to date in the background. */
    val smart: SmartDownloads by lazy { SmartDownloads(context, marquee, this) }

    private val _entries = MutableStateFlow(load())
    val entries: StateFlow<Map<Long, Entry>> = _entries
    private var watcher: Job? = null

    private val playlistsSerializer = kotlinx.serialization.builtins.MapSerializer(Long.serializer(), kotlinx.serialization.builtins.SetSerializer(Long.serializer()))
    /** Playlists kept on the device (MUSIC-19): playlist id -> its items when last synced. */
    private val _playlists = MutableStateFlow<Map<Long, Set<Long>>>(
        runCatching { json.decodeFromString(playlistsSerializer, readFile(playlistsFile)) }.getOrDefault(emptyMap()),
    )
    val playlists: StateFlow<Map<Long, Set<Long>>> = _playlists

    /** A kept playlist as the Offline Library shows it, without the server: its name and order. */
    @Serializable
    data class PlaylistInfo(val title: String, val items: List<Long>, val ownerName: String? = null)

    private val playlistInfoSerializer = kotlinx.serialization.builtins.MapSerializer(Long.serializer(), PlaylistInfo.serializer())
    private val _playlistInfo = MutableStateFlow<Map<Long, PlaylistInfo>>(
        runCatching { json.decodeFromString(playlistInfoSerializer, readFile(playlistInfoFile)) }.getOrDefault(emptyMap()),
    )
    val playlistInfo: StateFlow<Map<Long, PlaylistInfo>> = _playlistInfo

    // Quality

    private val _songQuality = MutableStateFlow(savedQuality(DownloadQuality.Kind.Songs))
    private val _videoQuality = MutableStateFlow(savedQuality(DownloadQuality.Kind.Videos))
    /** The song quality chosen in Settings; null until one is, when the recommended one applies. */
    val songQuality: StateFlow<Quality?> = _songQuality
    val videoQuality: StateFlow<Quality?> = _videoQuality

    private fun savedQuality(kind: DownloadQuality.Kind): Quality? {
        val saved = prefs.getString(kind.name, null)?.let { name -> Quality.entries.firstOrNull { it.name == name } }
        if (saved != null || kind != DownloadQuality.Kind.Videos) return saved
        // 1.6.0 kept a video quality in Smart Downloads' settings: it carries over, once.
        val old = context.getSharedPreferences("smart-downloads", Context.MODE_PRIVATE).getString("quality", null)
            ?.let { name -> Quality.entries.firstOrNull { it.name == name } } ?: return null
        prefs.edit().putString(kind.name, old.name).apply()
        return old
    }

    /** Chooses the quality for new downloads; those already made stay as they are. */
    fun setQuality(kind: DownloadQuality.Kind, q: Quality) {
        (if (kind == DownloadQuality.Kind.Songs) _songQuality else _videoQuality).value = q
        prefs.edit().putString(kind.name, q.name).apply()
    }

    /** What's recommended here and now: it follows the kind of device and the free storage. */
    fun recommendedQuality(kind: DownloadQuality.Kind): Quality = DownloadQuality.recommended(kind,
        tablet = context.resources.configuration.smallestScreenWidthDp >= 600, freeBytes = dir.usableSpace)

    /** The quality new downloads of this kind use. */
    fun quality(kind: DownloadQuality.Kind): Quality =
        (if (kind == DownloadQuality.Kind.Songs) _songQuality else _videoQuality).value ?: recommendedQuality(kind)

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

    /** Many entries in one change (an album's songs moving along together), so screens update once. */
    private fun setAll(changes: Map<Long, (Entry) -> Entry>, progressOnly: Boolean) {
        if (changes.isEmpty()) return
        _entries.update { m -> m + changes.mapNotNull { (id, f) -> m[id]?.let { id to f(it) } } }
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
        val info = kotlinx.coroutines.withContext(Dispatchers.IO) { runCatching { marquee.playlists.getPlaylist(id) }.getOrNull() }
        val now = items.map { it.id }.toSet()
        val before = _playlists.value[id].orEmpty()
        _playlists.update { it + (id to now) }
        _playlistInfo.update { m -> m + (id to PlaylistInfo(info?.title ?: m[id]?.title.orEmpty(), items.map { it.id }, info?.ownerName ?: m[id]?.ownerName)) }
        savePlaylists()
        for (it in items) if (_entries.value[it.id] == null) {
            runCatching { download(it) }
            set(it.id) { e -> e.copy(viaPlaylist = true) }
        } else if (_entries.value[it.id]?.smart != null) {
            // A smart download the playlist now keeps: smart downloads no longer remove it.
            set(it.id) { e -> e.copy(viaPlaylist = true, smart = null) }
        }
        dropOrphans(before - now)
    }

    fun unsyncPlaylist(id: Long) {
        val ids = _playlists.value[id] ?: return
        _playlists.update { it - id }
        _playlistInfo.update { it - id }
        savePlaylists()
        dropOrphans(ids)
    }

    private fun dropOrphans(ids: Set<Long>) {
        val kept = _playlists.value.values.flatten().toSet()
        ids.filter { it !in kept && _entries.value[it]?.viaPlaylist == true }.forEach(::remove)
    }

    private fun savePlaylists() {
        runCatching { writeFile(playlistsFile, json.encodeToString(playlistsSerializer, _playlists.value)) }
        runCatching { writeFile(playlistInfoFile, json.encodeToString(playlistInfoSerializer, _playlistInfo.value)) }
    }

    // Starting and removing

    /**
     * Downloads a movie, episode, video or track at the quality set for its kind. [smart] marks a
     * smart download; [wifiOnly] fetches it only over an unmetered network.
     */
    suspend fun download(item: ItemSummary, smart: SmartKind? = null, wifiOnly: Boolean = false) {
        val q = quality(DownloadQuality.kind(item.type))
        val info = kotlinx.coroutines.withContext(Dispatchers.IO) { marquee.downloads.createDownload(CreateDownloadRequest(item.id, q.api)) }
        val entry = Entry(item, q, State.Preparing, info.progress, size = info.propertySize ?: 0,
            jobId = info.id.takeUnless { it.startsWith("original-") }, url = info.url, fileName = info.fileName, smart = smart, wifiOnly = wifiOnly,
            ownerId = marquee.me.value?.id)
        _entries.update { it + (item.id to entry) }
        save()
        if (info.status == Download.Status.READY && info.url != null) fetch(item.id, info.url!!, info.fileName)
        watch()
        marquee.scope.launch(Dispatchers.IO) { savePoster(item) }
        // Tracks keep their volume levelling data, for playing offline (MUSIC-10).
        if (item.type == ItemType.TRACK) marquee.scope.launch(Dispatchers.IO) { app.marquee.music.MusicService.fetchGains(marquee, item.id) }
    }

    /** Everything playable under a season, show, album or artist. Returns how many it started. */
    suspend fun downloadAll(container: ItemSummary): Int {
        val leaves = kotlinx.coroutines.withContext(Dispatchers.IO) { marquee.items.itemLeaves(container.id) }
        var started = 0
        for (it in leaves) {
            val e = _entries.value[it.id]
            if (e?.state == State.Failed) { if (runCatching { retry(it.id) }.isSuccess) started++ }
            else if (e == null) { if (runCatching { download(it) }.isSuccess) started++ }
            else if (e.smart != null) keep(it.id)
        }
        return started
    }

    /** Tries a failed download again, at the quality set now. Throws, leaving it as it was, when the server refuses or can't be reached. */
    suspend fun retry(id: Long) {
        val e = _entries.value[id] ?: return
        download(e.item, e.smart, e.wifiOnly)
        set(id) { it.copy(viaPlaylist = e.viaPlaylist) }
        // The failed job on the server has been replaced (a job that converted fine is reused).
        e.jobId?.takeIf { it != _entries.value[id]?.jobId }?.let { job -> marquee.scope.launch(Dispatchers.IO) { runCatching { marquee.downloads.deleteDownload(job) } } }
    }

    /** Keeps a smart download as one made by hand: smart downloads won't remove it. */
    fun keep(id: Long) = set(id) { it.copy(smart = null) }

    /** Removes what smart downloads fetched (one kind, or all of it). Downloads made by hand stay. */
    fun removeSmart(kind: SmartKind? = null) {
        _entries.value.values.filter { it.smart != null && (kind == null || it.smart == kind) }.forEach { remove(it.item.id) }
    }

    /** The space smart downloads take (one kind, or all), finished or not. */
    fun smartBytes(kind: SmartKind? = null): Long =
        _entries.value.values.filter { it.smart != null && (kind == null || it.smart == kind) }.sumOf { it.size }

    /** Picks up conversions and transfers left running (after the app was closed). */
    fun resume() = watch()

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
        val name = "$id.${DownloadQuality.extension(fileName, e.item.type, e.quality)}"
        File(dir, name).delete()
        val req = DownloadManager.Request(Uri.parse(url))
            .addRequestHeader("Authorization", "Bearer ${marquee.token}")
            .setTitle(e.item.title)
            .setDescription("Marquee download")
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE)
            .setDestinationUri(Uri.fromFile(File(dir, name)))
            .setAllowedOverMetered(!e.wifiOnly)
        val transfer = dm.enqueue(req)
        set(id) { it.copy(state = State.Downloading, progress = 0.0, file = name, transfer = transfer, url = path) }
    }

    /**
     * Follows conversions on the server and transfers on the device while any are running. An
     * album or artist can queue hundreds, so it's one request for all the conversions, every few
     * seconds, and one query for all the transfers.
     */
    private fun watch() {
        if (watcher?.isActive == true) return
        watcher = marquee.scope.launch {
            var tick = 0
            while (true) {
                val busy = _entries.value.values.filter { it.state == State.Preparing || it.state == State.Downloading }
                if (busy.isEmpty()) break
                checkTransfers(busy.filter { it.state == State.Downloading })
                if (tick++ % CONVERSION_POLL_TICKS == 0) checkConversions(busy.filter { it.state == State.Preparing })
                delay(1000)
            }
        }
    }

    private suspend fun checkConversions(preparing: List<Entry>) {
        // Originals have no job: they only wait to be fetched.
        preparing.filter { it.jobId == null }.forEach { e -> e.url?.let { fetch(e.item.id, it, e.fileName) } }
        val converting = preparing.filter { it.jobId != null }
        if (converting.isEmpty() || marquee.baseUrl == null) return
        val jobs = kotlinx.coroutines.withContext(Dispatchers.IO) { runCatching { marquee.downloads.listDownloads() }.getOrNull() } ?: return
        val byId = jobs.associateBy { it.id }
        val changes = HashMap<Long, (Entry) -> Entry>()
        var askedAlone = 0
        var failed = false
        for (e in converting) {
            val info = byId[e.jobId] ?: when {
                // Not in the list, which would hold it: the server has lost it (it can be tried again).
                jobs.size < MAX_LISTED_JOBS -> null
                // The list is full, so it may be further down: a few at a time.
                askedAlone++ < 10 -> kotlinx.coroutines.withContext(Dispatchers.IO) { runCatching { marquee.downloads.getDownload(e.jobId!!) }.getOrNull() } ?: continue
                else -> continue
            }
            if (info == null) {
                if (e.ownerId != marquee.me.value?.id) continue // another profile's, which this list wouldn't hold
                changes[e.item.id] = { it.copy(state = State.Failed, error = "The server no longer has this conversion.") }
                failed = true
                continue
            }
            when (info.status) {
                Download.Status.READY -> {
                    val url = info.url
                    if (url != null) {
                        set(e.item.id) { it.copy(size = info.propertySize ?: it.size) }
                        fetch(e.item.id, url, info.fileName)
                    } else {
                        changes[e.item.id] = { it.copy(state = State.Failed, error = "The converted file isn't available.") }
                        failed = true
                    }
                }
                Download.Status.FAILED -> {
                    changes[e.item.id] = { it.copy(state = State.Failed, error = info.error ?: "Conversion failed") }
                    failed = true
                }
                else -> if (info.progress != e.progress) changes[e.item.id] = { it.copy(progress = info.progress) }
            }
        }
        setAll(changes, progressOnly = !failed)
    }

    private fun checkTransfers(downloading: List<Entry>) {
        val byTransfer = downloading.filter { it.transfer != null }.associateBy { it.transfer!! }
        val seen = HashSet<Long>()
        val changes = HashMap<Long, (Entry) -> Entry>()
        var finished = false
        // In batches: the query is one SQL statement with a parameter per transfer.
        byTransfer.keys.chunked(200).forEach { ids ->
            dm.query(DownloadManager.Query().setFilterById(*ids.toLongArray())).use { c ->
                while (c.moveToNext()) {
                    val e = byTransfer[c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_ID))] ?: continue
                    seen += e.transfer!!
                    val status = c.getInt(c.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS))
                    val done = c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR))
                    val total = c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_TOTAL_SIZE_BYTES))
                    when (status) {
                        DownloadManager.STATUS_SUCCESSFUL -> {
                            val size = e.file?.let { File(dir, it).length() } ?: total
                            changes[e.item.id] = { it.copy(state = State.Done, progress = 1.0, size = size, transfer = null) }
                            finished = true
                            // The converted copy on the server is no longer needed.
                            e.jobId?.let { job -> marquee.scope.launch(Dispatchers.IO) { runCatching { marquee.downloads.deleteDownload(job) } } }
                        }
                        DownloadManager.STATUS_FAILED -> {
                            val reason = c.getInt(c.getColumnIndexOrThrow(DownloadManager.COLUMN_REASON))
                            changes[e.item.id] = { it.copy(state = State.Failed, error = "Download failed ($reason)", transfer = null) }
                            finished = true
                        }
                        else -> if (total > 0) changes[e.item.id] = { it.copy(progress = done.toDouble() / total) }
                    }
                }
            }
        }
        byTransfer.filterKeys { it !in seen }.values.forEach { e ->
            changes[e.item.id] = { it.copy(state = State.Failed, error = "The download was cancelled.") }
            finished = true
        }
        setAll(changes, progressOnly = !finished)
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

    private companion object {
        /** Seconds between asks about conversions. */
        const val CONVERSION_POLL_TICKS = 3
        /** The most jobs the server's list holds (GET /downloads). */
        const val MAX_LISTED_JOBS = 2000
    }
}
