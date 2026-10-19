package app.marquee.core

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import app.marquee.api.apis.ItemsApi
import app.marquee.api.models.ContinueEntry
import app.marquee.api.models.ItemSummary
import app.marquee.api.models.ItemType
import app.marquee.api.models.LibraryType
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.util.concurrent.TimeUnit

/**
 * Smart downloads (phones and tablets): keeps the next episodes of the shows being watched and
 * the music listened to most on the device, each within its own storage budget, using the
 * offline downloads ([Downloads]). Runs every few hours with WorkManager (Wi-Fi only when asked)
 * and shortly after a video closes. What it fetched is marked as a smart download and removed
 * again once watched or no longer wanted; downloads made by hand are never touched.
 */
class SmartDownloads(context: Context, private val marquee: Marquee, private val downloads: Downloads) {
    data class Settings(
        val showsOn: Boolean = false,
        val showsBudgetGb: Int = 10,
        val episodes: Int = 3,
        val musicOn: Boolean = false,
        val musicBudgetGb: Int = 5,
        val wifiOnly: Boolean = true,
        val quality: Downloads.Quality = Downloads.Quality.Medium,
    ) {
        val on: Boolean get() = showsOn || musicOn
    }

    private val appContext = context.applicationContext
    private val prefs = context.getSharedPreferences("smart-downloads", Context.MODE_PRIVATE)
    private val _settings = MutableStateFlow(load())
    val settings: StateFlow<Settings> = _settings
    private val _running = MutableStateFlow(false)
    val running: StateFlow<Boolean> = _running
    private val _lastRun = MutableStateFlow(prefs.getLong("lastRun", 0).takeIf { it > 0 })
    /** When it last brought the device up to date. */
    val lastRun: StateFlow<Long?> = _lastRun
    private val _lastError = MutableStateFlow(prefs.getString("lastError", null))
    val lastError: StateFlow<String?> = _lastError
    private val mutex = Mutex()

    private fun load() = Settings(
        showsOn = prefs.getBoolean("showsOn", false),
        showsBudgetGb = prefs.getInt("showsBudgetGb", 10).takeIf { it in SmartSelection.budgets } ?: 10,
        episodes = prefs.getInt("episodes", 3).takeIf { it in SmartSelection.episodeCounts } ?: 3,
        musicOn = prefs.getBoolean("musicOn", false),
        musicBudgetGb = prefs.getInt("musicBudgetGb", 5).takeIf { it in SmartSelection.budgets } ?: 5,
        wifiOnly = prefs.getBoolean("wifiOnly", true),
        quality = runCatching { Downloads.Quality.valueOf(prefs.getString("quality", null) ?: "") }.getOrDefault(Downloads.Quality.Medium),
    )

    /**
     * Changes the settings. Turning shows or music off removes what it downloaded; any change
     * reschedules the background runs and brings the device up to date soon.
     */
    fun update(f: (Settings) -> Settings) {
        val old = _settings.value
        val new = f(old)
        _settings.value = new
        prefs.edit().putBoolean("showsOn", new.showsOn).putInt("showsBudgetGb", new.showsBudgetGb).putInt("episodes", new.episodes)
            .putBoolean("musicOn", new.musicOn).putInt("musicBudgetGb", new.musicBudgetGb).putBoolean("wifiOnly", new.wifiOnly)
            .putString("quality", new.quality.name).apply()
        if (old.showsOn && !new.showsOn) downloads.removeSmart(Downloads.SmartKind.Shows)
        if (old.musicOn && !new.musicOn) downloads.removeSmart(Downloads.SmartKind.Music)
        schedule()
        runSoon(delaySeconds = 2)
    }

    /** Turns smart downloads off and removes everything it downloaded. */
    fun removeAll() {
        downloads.removeSmart()
        update { it.copy(showsOn = false, musicOn = false) }
    }

    private fun constraints() = Constraints.Builder()
        .setRequiredNetworkType(if (_settings.value.wifiOnly) NetworkType.UNMETERED else NetworkType.CONNECTED)
        .setRequiresStorageNotLow(true)
        .build()

    private val work get() = runCatching { WorkManager.getInstance(appContext) }.getOrNull()

    /** The periodic background run, while either kind is on (never on a TV). */
    fun schedule() {
        val wm = work ?: return
        if (marquee.isTv || !_settings.value.on) {
            wm.cancelUniqueWork(PERIODIC)
            wm.cancelUniqueWork(SOON)
            return
        }
        wm.enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.UPDATE,
            PeriodicWorkRequestBuilder<SmartDownloadsWorker>(6, TimeUnit.HOURS).setConstraints(constraints()).build())
    }

    /** A run soon (after playback ends, or a settings change), when the network allows. */
    fun runSoon(delaySeconds: Long = 30) {
        if (marquee.isTv || !_settings.value.on) return
        work?.enqueueUniqueWork(SOON, ExistingWorkPolicy.REPLACE,
            OneTimeWorkRequestBuilder<SmartDownloadsWorker>().setConstraints(constraints()).setInitialDelay(delaySeconds, TimeUnit.SECONDS).build())
    }

    /**
     * Brings the device up to date: downloads what's wanted and removes smart downloads that
     * aren't. Returns false when the server couldn't be reached (to try again later).
     */
    suspend fun run(): Boolean = mutex.withLock {
        val s = _settings.value
        if (marquee.isTv || !s.on || marquee.token == null) return true
        if (marquee.baseUrl == null) runCatching { marquee.reconnect(quiet = true) }
        if (marquee.baseUrl == null || marquee.token == null) return false
        _running.value = true
        try {
            // Watched marks made offline reach the server first, so watched episodes go.
            runCatching { marquee.sync.syncNow() }
            downloads.resume()
            val errors = listOfNotNull(
                if (s.showsOn) runCatching { shows(s) }.exceptionOrNull()?.let { "Shows: ${it.message ?: "couldn't update"}" } else null,
                if (s.musicOn) runCatching { music(s) }.exceptionOrNull()?.let { "Music: ${it.message ?: "couldn't update"}" } else null,
            )
            val error = errors.joinToString("\n").ifEmpty { null }
            _lastError.value = error
            _lastRun.value = System.currentTimeMillis()
            prefs.edit().putLong("lastRun", _lastRun.value!!).putString("lastError", error).apply()
            true
        } finally {
            _running.value = false
        }
    }

    /** Bytes a candidate counts as: nothing when it's already downloaded by hand, its real size once known, else an estimate. */
    private fun bytes(item: ItemSummary, estimate: () -> Long): Long {
        val e = downloads.entries.value[item.id] ?: return estimate()
        if (e.smart == null) return 0
        return e.size.takeIf { it > 0 } ?: estimate()
    }

    private suspend fun shows(s: Settings) {
        // Shows being watched, from Continue Watching (in-progress episodes and next episodes).
        val page = withContext(Dispatchers.IO) { marquee.hubs.continueWatching(0, 50) }
        val lastWatched = LinkedHashMap<Long, Long>()
        for (c in page.propertyEntries) {
            val item = c.item ?: continue
            if (c.kind != ContinueEntry.Kind.ITEM || item.type != ItemType.EPISODE) continue
            val show = item.grandparentId ?: continue
            val at = c.lastWatchedAt.toInstant().toEpochMilli()
            lastWatched[show] = maxOf(lastWatched[show] ?: 0, at)
        }
        val items = HashMap<Long, ItemSummary>()
        val queues = lastWatched.map { (show, at) ->
            withContext(Dispatchers.IO) { runCatching { marquee.items.itemLeaves(show, unwatched = true) } }.fold(
                { leaves ->
                    val next = leaves.filter { it.type == ItemType.EPISODE }.take(s.episodes)
                    next.forEach { items[it.id] = it }
                    SmartSelection.ShowQueue(show, at, next.map { SmartSelection.Candidate(it.id, bytes(it) { SmartSelection.estimateVideoBytes(it.durationMs, s.quality) }) })
                },
                { SmartSelection.ShowQueue(show, at, emptyList(), failed = true) },
            )
        }
        val all = downloads.entries.value.values
        val mine = all.filter { it.smart == Downloads.SmartKind.Shows }
        // A show whose episodes couldn't be fetched keeps what it has until the next run.
        val failedShows = queues.filter { it.failed }.map { it.showId }.toSet()
        val wanted = SmartSelection.episodes(queues, s.episodes, s.showsBudgetGb * SmartSelection.GB) +
            mine.filter { it.item.grandparentId in failedShows }.map { it.item.id }
        bringUpToDate(wanted, Downloads.SmartKind.Shows, items, s)
    }

    private suspend fun music(s: Settings) {
        val budget = s.musicBudgetGb * SmartSelection.GB
        val libs = withContext(Dispatchers.IO) { marquee.libraries.listLibraries().filter { it.type == LibraryType.MUSIC } }
        val items = HashMap<Long, ItemSummary>()
        val candidates = mutableListOf<SmartSelection.Candidate>()
        var total = 0L
        fun add(tracks: List<ItemSummary>) {
            for (t in tracks) {
                if (t.type != ItemType.TRACK || items.containsKey(t.id)) continue
                items[t.id] = t
                val b = bytes(t) { SmartSelection.estimateTrackBytes(t.durationMs, t.audioFormat?.bitrateKbps, t.audioFormat?.lossless == true) }
                candidates += SmartSelection.Candidate(t.id, b)
                total += b
            }
        }
        // Enough to fill the budget, with some room for tracks too big for what's left.
        fun full() = total >= budget + budget / 4
        suspend fun <T> io(block: () -> T): T? = withContext(Dispatchers.IO) { runCatching(block).getOrNull() }
        val sort = ItemsApi.SortListLibraryItems._FAVORITED
        // Hearted songs, then hearted albums' and artists' tracks.
        for (lib in libs) { if (full()) break; add(io { marquee.items.listLibraryItems(lib.id, ItemType.TRACK, sort, limit = 500, favorites = true).items } ?: emptyList()) }
        for (type in listOf(ItemType.ALBUM, ItemType.ARTIST)) for (lib in libs) {
            if (full()) break
            val containers = io { marquee.items.listLibraryItems(lib.id, type, sort, limit = 100, favorites = true).items } ?: continue
            for (c in containers) { if (full()) break; add(io { marquee.items.itemLeaves(c.id) } ?: emptyList()) }
        }
        // Then what's played: the recently played, most played first.
        for (lib in libs) {
            if (full()) break
            val played = io { marquee.items.listLibraryItems(lib.id, ItemType.TRACK, ItemsApi.SortListLibraryItems._VIEWED, limit = 300).items } ?: continue
            add(played.filter { it.lastViewedAt != null || (it.viewCount ?: 0) > 0 }.sortedByDescending { it.viewCount ?: 0 })
        }
        bringUpToDate(SmartSelection.tracks(candidates, budget), Downloads.SmartKind.Music, items, s)
    }

    private suspend fun bringUpToDate(wanted: List<Long>, kind: Downloads.SmartKind, items: Map<Long, ItemSummary>, s: Settings) {
        val all = downloads.entries.value.values
        val plan = SmartSelection.plan(
            wanted,
            smartNow = all.filter { it.smart == kind }.map { it.item.id }.toSet(),
            manual = all.filter { it.smart == null }.map { it.item.id }.toSet(),
            failed = all.filter { it.smart == kind && it.state == Downloads.State.Failed }.map { it.item.id }.toSet(),
        )
        plan.remove.forEach(downloads::remove)
        for (id in plan.download) {
            val item = items[id] ?: continue
            // Settings may have changed meanwhile (turned off): stop adding.
            val now = _settings.value
            if (kind == Downloads.SmartKind.Shows && !now.showsOn || kind == Downloads.SmartKind.Music && !now.musicOn) break
            runCatching { downloads.download(item, s.quality, smart = kind, wifiOnly = s.wifiOnly) }
        }
    }

    companion object {
        private const val PERIODIC = "smart-downloads"
        private const val SOON = "smart-downloads-soon"
    }
}

/** The background run of smart downloads. */
class SmartDownloadsWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as? app.marquee.MarqueeApplication ?: return Result.success()
        val ok = runCatching { app.downloads.smart.run() }.getOrDefault(false)
        return if (ok || runAttemptCount >= 3) Result.success() else Result.retry()
    }
}
