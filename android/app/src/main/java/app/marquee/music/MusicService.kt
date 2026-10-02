package app.marquee.music

import android.content.SharedPreferences
import android.media.AudioManager
import android.media.audiofx.LoudnessEnhancer
import android.net.Uri
import android.os.Handler
import android.os.Looper
import androidx.annotation.OptIn
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.Timeline
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.datasource.HttpDataSource
import androidx.media3.datasource.ResolvingDataSource
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
import java.util.concurrent.ConcurrentHashMap
import kotlin.math.pow
import kotlin.math.roundToInt

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
    /** Track id → (track gain, album gain) in dB, from its playback session. */
    private val gains = ConcurrentHashMap<Long, Pair<Double?, Double?>>()
    private var enhancer: LoudnessEnhancer? = null
    private val prefs by lazy { getSharedPreferences("marquee.music", MODE_PRIVATE) }
    private val levellingChanged = SharedPreferences.OnSharedPreferenceChangeListener { _, key -> if (key == "levelling") mediaSession?.player?.let(::level) }
    private var ticker: Job? = null
    private val mainHandler = Handler(Looper.getMainLooper())
    private val app get() = application as MarqueeApplication
    /** Tracks playing from downloaded files. */
    private val local = ConcurrentHashMap.newKeySet<Long>()

    override fun onCreate() {
        super.onCreate()
        val resolver = ResolvingDataSource.Resolver { spec ->
            val uri = spec.uri
            if (uri.scheme != "marquee") return@Resolver spec
            val id = uri.lastPathSegment!!.toLong()
            // Downloaded tracks play from the device (and count when the server is back).
            app.downloads.localFile(id)?.let { f -> local.add(id); return@Resolver spec.withUri(Uri.fromFile(f)) }
            val (_, url) = resolved.getOrPut(id) {
                // Preloaded sessions take over the device's playback when they first report "playing".
                val s = marquee.playback.startPlayback(PlaybackRequest(id, AndroidProfile.profile, startMs = 0, preload = true))
                gains[id] = s.trackGainDb to s.albumGainDb
                mainHandler.post { mediaSession?.player?.let { p -> if (p.currentMediaItem?.mediaId == id.toString()) level(p) } }
                s.id to marquee.absolute(s.url)!!
            }
            spec.withUri(Uri.parse(url))
        }
        val http = DefaultDataSource.Factory(this, DefaultHttpDataSource.Factory().setAllowCrossProtocolRedirects(true))
        // Our own audio session, so the loudness effect can attach before playback starts.
        val audioSession = (getSystemService(AUDIO_SERVICE) as AudioManager).generateAudioSessionId()
        val player = ExoPlayer.Builder(this)
            .setMediaSourceFactory(DefaultMediaSourceFactory(ResolvingDataSource.Factory(http, resolver)))
            .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
            .setHandleAudioBecomingNoisy(true)
            .build()
        player.audioSessionId = audioSession
        mediaSourceFactory = DefaultMediaSourceFactory(ResolvingDataSource.Factory(http, resolver))
        enhancer = runCatching { LoudnessEnhancer(audioSession).apply { enabled = true } }.getOrNull()
        prefs.registerOnSharedPreferenceChangeListener(levellingChanged)
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
                level(player)
                report(player, PlaybackProgress.State.PLAYING)
            }
            override fun onIsPlayingChanged(isPlaying: Boolean) {
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
                val queued = (0 until player.mediaItemCount).mapNotNull { player.getMediaItemAt(it).mediaId.toLongOrNull() }.toSet()
                resolved.keys.filter { it !in queued && it != current && it != fadingFrom }.forEach { finish(it, 0) }
            }

            // The server ended a session we still had cached (it was idle too long): start a fresh one, once.
            private var retried: Long? = null
            override fun onPlayerError(error: PlaybackException) {
                val id = player.currentMediaItem?.mediaId?.toLongOrNull() ?: return
                val code = (error.cause as? HttpDataSource.InvalidResponseCodeException)?.responseCode
                if (retried == id || (code != 404 && code != 410)) return
                retried = id
                resolved.remove(id)
                player.prepare()
                player.play()
            }
        })
        ticker = marquee.scope.launch {
            while (true) {
                delay(15_000)
                launch(kotlinx.coroutines.Dispatchers.Main) { if (player.isPlaying) report(player, PlaybackProgress.State.PLAYING) }
            }
        }
        mediaSession = MediaLibrarySession.Builder(this, player, MusicLibrary(marquee, packageName) { app.music }).build()
        // Stations and the Guest DJ live in the app's controller; make sure it's listening.
        app.music.attach()
    }

    private var current: Long? = null
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

    /**
     * Volume levelling (MUSIC-10): cuts with the player's volume and boosts with a loudness
     * enhancer (which limits peaks), so quiet tracks come up as well as loud ones down.
     */
    // ---- Crossfade (MUSIC-9) ----
    // A second player takes over the outgoing track's tail and fades it out while the main
    // player moves on and fades in. Consecutive tracks of one album stay gapless.

    private lateinit var mediaSourceFactory: DefaultMediaSourceFactory
    private var fader: ExoPlayer? = null
    private var fadeJob: Job? = null
    private var fadingFrom: Long? = null
    private var baseVolume = 1f // from volume levelling
    private var ramp = 1f // the crossfade's share of the main player's volume

    private fun applyVolume(p: Player) { p.volume = baseVolume * ramp }

    private fun faderPlayer(): ExoPlayer = fader ?: ExoPlayer.Builder(this)
        .setMediaSourceFactory(mediaSourceFactory)
        .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), false)
        .build().also { fader = it }

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
        val fromVolume = baseVolume
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

    private fun level(p: Player) {
        val id = p.currentMediaItem?.mediaId?.toLongOrNull()
        val (track, album) = id?.let { gains[it] } ?: (null to null)
        val mode = prefs.getString("levelling", null)?.let { n -> Levelling.entries.firstOrNull { it.name == n } } ?: Levelling.Auto
        val db = when (mode) {
            Levelling.Off -> null
            Levelling.Track -> track
            Levelling.Album -> album ?: track
            Levelling.Auto -> if (inAlbumOrder(p)) album ?: track else track
        } ?: 0.0
        val g = db.coerceIn(-15.0, 6.0)
        baseVolume = if (g < 0) 10.0.pow(g / 20).toFloat() else 1f
        applyVolume(p)
        runCatching { enhancer?.setTargetGain(if (g > 0) (g * 100).roundToInt() else 0) }
    }

    /** True when a neighbouring queue entry is from the same album: an album playing through. */
    private fun inAlbumOrder(p: Player): Boolean {
        if (p.shuffleModeEnabled) return false
        val i = p.currentMediaItemIndex
        val album = p.currentMediaItem?.mediaMetadata?.extras?.getLong("album", -1) ?: -1
        if (album < 0) return false
        return listOf(i - 1, i + 1).any { j -> j in 0 until p.mediaItemCount && p.getMediaItemAt(j).mediaMetadata.extras?.getLong("album", -2) == album }
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

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaLibrarySession? = mediaSession

    override fun onTaskRemoved(rootIntent: android.content.Intent?) {
        val p = mediaSession?.player
        if (p == null || !p.playWhenReady || p.mediaItemCount == 0) stopSelf()
    }

    override fun onDestroy() {
        ticker?.cancel()
        watcher?.cancel()
        prefs.unregisterOnSharedPreferenceChangeListener(levellingChanged)
        enhancer?.release()
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
