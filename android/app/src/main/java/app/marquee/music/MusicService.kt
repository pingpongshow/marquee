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

    override fun onCreate() {
        super.onCreate()
        val resolver = ResolvingDataSource.Resolver { spec ->
            val uri = spec.uri
            if (uri.scheme != "marquee") return@Resolver spec
            val id = uri.lastPathSegment!!.toLong()
            val (_, url) = resolved.getOrPut(id) {
                // Preloaded sessions take over the device's playback when they first report "playing".
                val s = marquee.playback.startPlayback(PlaybackRequest(id, AndroidProfile.profile, startMs = 0, preload = true))
                gains[id] = s.trackGainDb to s.albumGainDb
                mainHandler.post { mediaSession?.player?.let { p -> if (p.currentMediaItem?.mediaId == id.toString()) level(p) } }
                s.id to marquee.absolute(s.url)!!
            }
            spec.withUri(Uri.parse(url))
        }
        val http = DefaultHttpDataSource.Factory().setAllowCrossProtocolRedirects(true)
        // Our own audio session, so the loudness effect can attach before playback starts.
        val audioSession = (getSystemService(AUDIO_SERVICE) as AudioManager).generateAudioSessionId()
        val player = ExoPlayer.Builder(this)
            .setMediaSourceFactory(DefaultMediaSourceFactory(ResolvingDataSource.Factory(http, resolver)))
            .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
            .setHandleAudioBecomingNoisy(true)
            .build()
        player.audioSessionId = audioSession
        enhancer = runCatching { LoudnessEnhancer(audioSession).apply { enabled = true } }.getOrNull()
        prefs.registerOnSharedPreferenceChangeListener(levellingChanged)
        player.addListener(object : Player.Listener {
            private var current: Long? = null
            private var lastPosition = 0L
            private var lastDuration = 0L
            override fun onEvents(p: Player, events: Player.Events) {
                if (p.duration > 0) lastDuration = p.duration
                lastPosition = p.currentPosition
            }
            override fun onMediaItemTransition(item: MediaItem?, reason: Int) {
                // The previous track finished or was skipped: record where it ended and close its session.
                current?.let { prev -> finish(prev, if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO) lastDuration else lastPosition) }
                current = item?.mediaId?.toLongOrNull()
                lastDuration = 0
                level(player)
                report(player, PlaybackProgress.State.PLAYING)
            }
            override fun onIsPlayingChanged(isPlaying: Boolean) = report(player, if (isPlaying) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)

            // A new queue: close sessions for tracks that were loaded ahead but left it.
            override fun onTimelineChanged(timeline: Timeline, reason: Int) {
                if (reason != Player.TIMELINE_CHANGE_REASON_PLAYLIST_CHANGED) return
                val queued = (0 until player.mediaItemCount).mapNotNull { player.getMediaItemAt(it).mediaId.toLongOrNull() }.toSet()
                resolved.keys.filter { it !in queued }.forEach { finish(it, 0) }
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
        val app = application as MarqueeApplication
        mediaSession = MediaLibrarySession.Builder(this, player, MusicLibrary(marquee) { app.music }).build()
        // Stations and the Guest DJ live in the app's controller; make sure it's listening.
        app.music.attach()
    }

    /**
     * Volume levelling (MUSIC-10): cuts with the player's volume and boosts with a loudness
     * enhancer (which limits peaks), so quiet tracks come up as well as loud ones down.
     */
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
        p.volume = if (g < 0) 10.0.pow(g / 20).toFloat() else 1f
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
        prefs.unregisterOnSharedPreferenceChangeListener(levellingChanged)
        enhancer?.release()
        resolved.keys.toList().forEach { finish(it, mediaSession?.player?.currentPosition ?: 0) }
        mediaSession?.run {
            player.release()
            release()
        }
        mediaSession = null
        super.onDestroy()
    }
}
