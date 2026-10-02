package app.marquee.music

import android.net.Uri
import androidx.annotation.OptIn
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.datasource.ResolvingDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import app.marquee.MarqueeApplication
import app.marquee.api.models.PlaybackProgress
import app.marquee.api.models.PlaybackRequest
import app.marquee.core.AndroidProfile
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.util.concurrent.ConcurrentHashMap

/**
 * Background music playback (MUSIC-13 on Android). Queue items are "marquee://track/<id>";
 * each resolves to its own server playback session when ExoPlayer loads it, so the next
 * track is prepared ahead (gapless) and sessions only exist for what is actually played.
 */
@OptIn(UnstableApi::class)
class MusicService : MediaSessionService() {
    private var mediaSession: MediaSession? = null
    private val marquee get() = (application as MarqueeApplication).marquee
    /** Track id → (server session id, stream URL). */
    private val resolved = ConcurrentHashMap<Long, Pair<String, String>>()
    private var ticker: Job? = null

    override fun onCreate() {
        super.onCreate()
        val resolver = ResolvingDataSource.Resolver { spec ->
            val uri = spec.uri
            if (uri.scheme != "marquee") return@Resolver spec
            val id = uri.lastPathSegment!!.toLong()
            val (_, url) = resolved.getOrPut(id) {
                // Preloaded sessions take over the device's playback when they first report "playing".
                val s = marquee.playback.startPlayback(PlaybackRequest(id, AndroidProfile.profile, startMs = 0, preload = true))
                s.id to marquee.absolute(s.url)!!
            }
            spec.withUri(Uri.parse(url))
        }
        val http = DefaultHttpDataSource.Factory().setAllowCrossProtocolRedirects(true)
        val player = ExoPlayer.Builder(this)
            .setMediaSourceFactory(DefaultMediaSourceFactory(ResolvingDataSource.Factory(http, resolver)))
            .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
            .setHandleAudioBecomingNoisy(true)
            .build()
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
                report(player, PlaybackProgress.State.PLAYING)
            }
            override fun onIsPlayingChanged(isPlaying: Boolean) = report(player, if (isPlaying) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)
        })
        ticker = marquee.scope.launch {
            while (true) {
                delay(15_000)
                launch(kotlinx.coroutines.Dispatchers.Main) { if (player.isPlaying) report(player, PlaybackProgress.State.PLAYING) }
            }
        }
        mediaSession = MediaSession.Builder(this, player).setCallback(object : MediaSession.Callback {
            // Controllers send items without URIs; rebuild them from the media id.
            override fun onAddMediaItems(session: MediaSession, controller: MediaSession.ControllerInfo, items: MutableList<MediaItem>): ListenableFuture<MutableList<MediaItem>> =
                Futures.immediateFuture(items.map { it.buildUpon().setUri("marquee://track/${it.mediaId}").build() }.toMutableList())
        }).build()
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

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession? = mediaSession

    override fun onTaskRemoved(rootIntent: android.content.Intent?) {
        val p = mediaSession?.player
        if (p == null || !p.playWhenReady || p.mediaItemCount == 0) stopSelf()
    }

    override fun onDestroy() {
        ticker?.cancel()
        resolved.keys.toList().forEach { finish(it, mediaSession?.player?.currentPosition ?: 0) }
        mediaSession?.run {
            player.release()
            release()
        }
        mediaSession = null
        super.onDestroy()
    }
}
