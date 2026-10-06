package app.marquee.music

import android.content.SharedPreferences
import android.media.AudioManager
import android.net.Uri
import androidx.annotation.OptIn
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.datasource.HttpDataSource
import androidx.media3.datasource.TransferListener
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaLibraryService
import androidx.media3.session.MediaLibraryService.MediaLibrarySession
import app.marquee.MarqueeApplication
import app.marquee.api.models.PlaybackProgress
import app.marquee.api.models.PlaybackRequest
import app.marquee.core.AndroidProfile
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap

/**
 * Background music playback (MUSIC-13 on Android). Queue items are "marquee://track/<id>";
 * each resolves to its own server playback session when ExoPlayer loads it, so the next
 * track is prepared ahead (gapless) and sessions only exist for what is actually played.
 */
@OptIn(UnstableApi::class)
class MusicService : MediaLibraryService() {
    private var mediaSession: MediaLibrarySession? = null
    private val marquee get() = (application as MarqueeApplication).marquee
    /** Track id → (server session id, stream URL). */
    private val resolved = ConcurrentHashMap<Long, Pair<String, String>>()
    private val prefs by lazy { getSharedPreferences("marquee.music", MODE_PRIVATE) }
    private val settingsChanged = SharedPreferences.OnSharedPreferenceChangeListener { p, key ->
        if (key?.startsWith("eq.") == true) EqSettings.load(p).let { s -> equalizers.forEach { it.apply(s) } }
    }
    /** The equaliser on the main player's audio session, and on the crossfade player's. */
    private val equalizers = mutableListOf<SessionEqualizer>()
    private var ticker: Job? = null
    private val app get() = application as MarqueeApplication
    /** Tracks playing from downloaded files. */
    private val local = ConcurrentHashMap.newKeySet<Long>()

    /**
     * The stream for a queue entry: the downloaded file, or its playback session's URL (one
     * session per track, kept while it's queued). fresh drops a cached session the server has
     * ended and starts a new one.
     */
    private fun trackUri(id: Long, fresh: Boolean): Uri {
        // Downloaded tracks play from the device (and count when the server is back).
        app.downloads.localFile(id)?.let { f ->
            local.add(id)
            if (!TrackGains.has(id)) marquee.scope.launch(kotlinx.coroutines.Dispatchers.IO) { fetchGains(marquee, id) }
            return Uri.fromFile(f)
        }
        if (fresh) resolved.remove(id)
        val (_, url) = resolved.getOrPut(id) {
            // Preloaded sessions take over the device's playback when they first report "playing".
            val s = try {
                marquee.playback.startPlayback(PlaybackRequest(id, AndroidProfile.profile, startMs = 0, preload = true))
            } catch (e: Exception) { throw IOException("Couldn't start track $id: ${e.message}", e) }
            // Known before any audio is decoded, so levelling applies from the first sample.
            TrackGains.put(id, Levelling.Gains(s.trackGainDb, s.albumGainDb, s.peak), persist = app.downloads.entries.value.containsKey(id))
            // What's streamed when it isn't the original (Show audio quality).
            streamed.value = streamed.value + (id to AudioQuality.streamed(s))
            s.id to marquee.absolute(s.url)!!
        }
        return Uri.parse(url)
    }

    /**
     * Opens "marquee://track/<id>" entries. When the session's stream is gone (the server
     * ended it, e.g. a track loaded ahead that sat idle), it starts a fresh session and
     * carries on from the same byte, so a half-loaded or preloaded track doesn't stop.
     */
    private inner class TrackDataSource(private val upstream: DataSource) : DataSource {
        override fun addTransferListener(transferListener: TransferListener) = upstream.addTransferListener(transferListener)
        override fun open(dataSpec: DataSpec): Long {
            if (dataSpec.uri.scheme != "marquee") return upstream.open(dataSpec)
            val id = dataSpec.uri.lastPathSegment!!.toLong()
            return try {
                upstream.open(dataSpec.withUri(trackUri(id, fresh = false)))
            } catch (e: HttpDataSource.InvalidResponseCodeException) {
                if (e.responseCode != 404 && e.responseCode != 410) throw e
                android.util.Log.i("Marquee", "track $id: stream gone (${e.responseCode}), starting a new session")
                runCatching { upstream.close() }
                upstream.open(dataSpec.withUri(trackUri(id, fresh = true)))
            }
        }
        override fun read(buffer: ByteArray, offset: Int, length: Int) = upstream.read(buffer, offset, length)
        override fun getUri(): Uri? = upstream.uri
        override fun getResponseHeaders(): Map<String, List<String>> = upstream.responseHeaders
        override fun close() = upstream.close()
    }

    override fun onCreate() {
        super.onCreate()
        TrackGains.init(this)
        val http = DefaultDataSource.Factory(this, DefaultHttpDataSource.Factory().setAllowCrossProtocolRedirects(true))
        val tracks = DataSource.Factory { TrackDataSource(http.createDataSource()) }
        // Our own audio session, so the equaliser can attach before playback starts.
        val audioSession = (getSystemService(AUDIO_SERVICE) as AudioManager).generateAudioSessionId()
        mediaSourceFactory = DefaultMediaSourceFactory(tracks)
        val player = ExoPlayer.Builder(this, LevellingRenderersFactory(this))
            .setMediaSourceFactory(mediaSourceFactory)
            .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
            .setHandleAudioBecomingNoisy(true)
            .build()
        player.audioSessionId = audioSession
        // The equaliser rides on the same session, which this service owns for its lifetime
        // (it's never regenerated, so the effect needn't be re-attached).
        equalizers += SessionEqualizer(audioSession).also { it.apply(EqSettings.load(prefs)); equalizerBands = it.bands }
        prefs.registerOnSharedPreferenceChangeListener(settingsChanged)
        player.addListener(object : Player.Listener {
            override fun onEvents(p: Player, events: Player.Events) {
                if (p.duration > 0) lastDuration = p.duration
                lastPosition = p.currentPosition
            }
            override fun onMediaItemTransition(item: MediaItem?, reason: Int) {
                // The previous track finished or was skipped: record where it ended and close its session.
                // While it's fading out on the crossfade player, that waits until the fade ends.
                current?.takeIf { it != fadingFrom }?.let { prev ->
                    val end = if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO) lastDuration else lastPosition
                    if (prev in local) {
                        // A play counts once half the track has played, as on the server.
                        if (lastDuration > 0 && end >= lastDuration / 2) app.downloads.recordProgress(prev, end, watched = true)
                    } else finish(prev, end)
                }
                current = item?.mediaId?.toLongOrNull()
                lastDuration = 0
                if (current != retried) retried = null
                report(player, PlaybackProgress.State.PLAYING)
            }
            override fun onShuffleModeEnabledChanged(shuffleModeEnabled: Boolean) = updateAlbumOrder(player)
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                if (isPlaying) failures = 0
                report(player, if (isPlaying) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)
                if (isPlaying) watch(player) else { watcher?.cancel(); watcher = null }
                // Paused mid-crossfade: the tail stops too.
                if (!isPlaying && player.playbackState == Player.STATE_READY && fadeJob?.isActive == true) {
                    fadeJob?.cancel()
                    endFade(player)
                }
            }

            // A new queue: close sessions for tracks that were loaded ahead but left it. The
            // interrupted track and one fading out are closed at their real positions by the
            // item transition and the end of the fade, which come after this.
            override fun onTimelineChanged(timeline: Timeline, reason: Int) {
                if (reason != Player.TIMELINE_CHANGE_REASON_PLAYLIST_CHANGED) return
                updateAlbumOrder(player)
                val queued = (0 until player.mediaItemCount).mapNotNull { player.getMediaItemAt(it).mediaId.toLongOrNull() }.toSet()
                resolved.keys.filter { it !in queued && it != current && it != fadingFrom }.forEach { finish(it, 0) }
            }

            // A track that fails (its stream gone, or the session failing mid-track): once, a
            // fresh session from the same position; then on to the next track rather than
            // stopping. Not when the server can't be reached at all, or after several in a row.
            override fun onPlayerError(error: PlaybackException) {
                val id = player.currentMediaItem?.mediaId?.toLongOrNull() ?: return
                val code = (error.cause as? HttpDataSource.InvalidResponseCodeException)?.responseCode
                android.util.Log.w("Marquee", "track $id failed (${error.errorCodeName}, http $code)", error)
                // Its session may be dead or broken: the retry starts a new one.
                if (code == 404 || code == 410) resolved.remove(id) else finish(id, player.currentPosition)
                val offline = error.errorCode == PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_FAILED ||
                    error.errorCode == PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_TIMEOUT
                when {
                    retried != id -> {
                        retried = id
                        player.prepare() // keeps the position
                        player.play()
                    }
                    !offline && player.hasNextMediaItem() && ++failures <= 5 -> {
                        player.seekToNextMediaItem()
                        player.prepare()
                        player.play()
                    }
                }
            }
        })
        ticker = marquee.scope.launch {
            while (true) {
                delay(15_000)
                launch(kotlinx.coroutines.Dispatchers.Main) { if (player.isPlaying) report(player, PlaybackProgress.State.PLAYING) }
            }
        }
        mediaSession = MediaLibrarySession.Builder(this, player, MusicLibrary(marquee, packageName) { app.music }).build()
        // Stations and the DJ live in the app's controller; make sure it's listening.
        app.music.attach()
    }

    private var current: Long? = null
    /** The track retried after an error (once per track), and failed tracks skipped in a row. */
    private var retried: Long? = null
    private var failures = 0
    private var lastPosition = 0L
    private var lastDuration = 0L
    private var watcher: Job? = null

    /**
     * While playing: keeps the position fresh (so a skipped track closes where it was left)
     * and, with crossfade on, watches for the track's end a few times a second.
     */
    private fun watch(p: Player) {
        if (watcher?.isActive == true) return
        watcher = marquee.scope.launch(kotlinx.coroutines.Dispatchers.Main) {
            while (p.isPlaying) {
                val crossfade = crossfadeSeconds() > 0
                delay(if (crossfade) 200 else 1000)
                if (mediaSession == null) break
                lastPosition = p.currentPosition
                if (crossfade) maybeCrossfade(p)
            }
        }
    }

    // ---- Crossfade (MUSIC-9) ----
    // A second player takes over the outgoing track's tail and fades it out while the main
    // player moves on and fades in. Consecutive tracks of one album stay gapless.

    private lateinit var mediaSourceFactory: DefaultMediaSourceFactory
    private var fader: ExoPlayer? = null
    private var fadeJob: Job? = null
    private var fadingFrom: Long? = null
    private var ramp = 1f // the crossfade's share of the main player's volume (levelling is in the audio path)

    private fun applyVolume(p: Player) { p.volume = ramp }

    private fun faderPlayer(): ExoPlayer = fader ?: ExoPlayer.Builder(this, LevellingRenderersFactory(this))
        .setMediaSourceFactory(mediaSourceFactory)
        .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), false)
        .build().also { f ->
            // Its own session with the same equaliser, so a fading tail sounds like the rest.
            val session = (getSystemService(AUDIO_SERVICE) as AudioManager).generateAudioSessionId()
            f.audioSessionId = session
            equalizers += SessionEqualizer(session).also { it.apply(EqSettings.load(prefs)) }
            fader = f
        }

    private fun crossfadeSeconds() = prefs.getInt("crossfade", 0)

    private fun sameAlbumNext(p: Player): Boolean {
        val next = p.nextMediaItemIndex.takeIf { it >= 0 } ?: return false
        val a = p.currentMediaItem?.mediaMetadata?.extras?.getLong("album", -1) ?: -1
        val b = p.getMediaItemAt(next).mediaMetadata.extras?.getLong("album", -2) ?: -2
        return a >= 0 && a == b && !p.shuffleModeEnabled
    }

    /** Called a few times a second: starts a crossfade when the track is about to end. */
    private fun maybeCrossfade(p: Player) {
        val secs = crossfadeSeconds()
        if (secs <= 0 || fadeJob?.isActive == true || !p.isPlaying || !p.hasNextMediaItem() || p.repeatMode == Player.REPEAT_MODE_ONE) return
        val dur = p.duration
        if (dur <= secs * 3000L || dur - p.currentPosition > secs * 1000L || sameAlbumNext(p)) return
        val item = p.currentMediaItem ?: return
        val id = item.mediaId.toLongOrNull() ?: return
        val f = faderPlayer()
        val fromVolume = 1f
        fadingFrom = id
        f.setMediaItem(item.buildUpon().setUri("marquee://track/$id").build())
        f.prepare()
        f.seekTo(p.currentPosition + 200)
        f.volume = fromVolume
        f.play()
        fadeJob = marquee.scope.launch(kotlinx.coroutines.Dispatchers.Main) {
            // Hand over only once the tail is audibly playing; otherwise don't crossfade.
            val started = kotlinx.coroutines.withTimeoutOrNull(1500) { while (!f.isPlaying) delay(25); true } == true
            if (!started) {
                f.stop(); f.clearMediaItems(); fadingFrom = null
                return@launch
            }
            val end = f.duration
            android.util.Log.i("Marquee", "crossfade ${secs}s from track $id at ${p.currentPosition}/${p.duration} ms")
            ramp = 0f
            applyVolume(p)
            try {
                p.seekToNextMediaItem()
                val steps = secs * 25
                for (i in 1..steps) {
                    val t = i / steps.toFloat()
                    // Equal-power curves keep the loudness steady through the blend.
                    ramp = kotlin.math.sin(t * Math.PI / 2).toFloat()
                    applyVolume(p)
                    f.volume = fromVolume * kotlin.math.cos(t * Math.PI / 2).toFloat()
                    delay(40)
                }
                endFade(p, end)
            } finally {
                // However the fade ends (cancelled, or failing), the main player is never left silent.
                if (ramp < 1f) { ramp = 1f; applyVolume(p) }
            }
        }
    }

    private fun endFade(p: Player, endMs: Long = fader?.currentPosition ?: 0) {
        fader?.let { it.stop(); it.clearMediaItems() }
        ramp = 1f
        applyVolume(p)
        val from = fadingFrom
        fadingFrom = null
        if (from != null) {
            if (from in local) app.downloads.recordProgress(from, endMs, watched = true) else finish(from, endMs)
        }
    }

    /**
     * Volume levelling (MUSIC-10), always on: album gain while an album plays in order, else
     * track gain. The gain itself is applied in the audio path ([LevelProcessor]); this keeps
     * which tracks use album gain in step with the queue.
     */
    private fun updateAlbumOrder(p: Player) {
        val ids = (0 until p.mediaItemCount).map { p.getMediaItemAt(it) }
        val order = Levelling.albumOrder(ids.map { it.mediaMetadata.extras?.getLong("album", -1)?.takeIf { a -> a >= 0 } }, p.shuffleModeEnabled)
        ids.forEachIndexed { i, item -> item.mediaId.toLongOrNull()?.let { TrackGains.albumMode[it] = order[i] } }
    }

    private fun report(player: Player, state: PlaybackProgress.State) {
        val id = player.currentMediaItem?.mediaId?.toLongOrNull() ?: return
        val session = resolved[id]?.first ?: return
        val pos = player.currentPosition
        marquee.scope.launch { runCatching { marquee.playback.reportPlayback(session, PlaybackProgress(pos, state)) } }
    }

    private fun finish(id: Long, positionMs: Long) {
        val (session, _) = resolved.remove(id) ?: return
        marquee.scope.launch {
            runCatching {
                marquee.playback.reportPlayback(session, PlaybackProgress(positionMs, PlaybackProgress.State.PAUSED))
                marquee.playback.stopPlayback(session)
            }
        }
    }

    companion object {
        /** Levelling data for a downloaded track, when it has none stored: a session started and closed at once. */
        suspend fun fetchGains(marquee: app.marquee.core.Marquee, id: Long) {
            if (marquee.baseUrl == null || TrackGains.has(id)) return
            runCatching {
                val s = marquee.playback.startPlayback(PlaybackRequest(id, AndroidProfile.profile, startMs = 0, preload = true))
                TrackGains.put(id, Levelling.Gains(s.trackGainDb, s.albumGainDb, s.peak))
                runCatching { marquee.playback.stopPlayback(s.id) }
            }
        }

        /** Bands of the platform equaliser in use: -1 before the service starts, 0 when the device has none. */
        @Volatile var equalizerBands = -1
        /** Per track, what its session streams when it isn't the original file (null: direct play). */
        val streamed = kotlinx.coroutines.flow.MutableStateFlow<Map<Long, String?>>(emptyMap())
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaLibrarySession? = mediaSession

    override fun onTaskRemoved(rootIntent: android.content.Intent?) {
        val p = mediaSession?.player
        if (p == null || !p.playWhenReady || p.mediaItemCount == 0) stopSelf()
    }

    override fun onDestroy() {
        ticker?.cancel()
        watcher?.cancel()
        prefs.unregisterOnSharedPreferenceChangeListener(settingsChanged)
        equalizers.forEach { it.release() }
        equalizers.clear()
        fadeJob?.cancel()
        fader?.release()
        resolved.keys.toList().forEach { finish(it, mediaSession?.player?.currentPosition ?: 0) }
        mediaSession?.run {
            player.release()
            release()
        }
        mediaSession = null
        super.onDestroy()
    }
}
