package app.marquee.core

import android.content.Context
import android.net.Uri
import androidx.mediarouter.media.MediaRouteSelector
import androidx.mediarouter.media.MediaRouter
import app.marquee.MainActivity
import app.marquee.api.models.DeviceProfile
import app.marquee.api.models.PlaybackProgress
import app.marquee.api.models.PlaybackRequest
import app.marquee.api.models.PlaybackSession
import com.google.android.gms.cast.CastMediaControlIntent
import com.google.android.gms.cast.HlsSegmentFormat
import com.google.android.gms.cast.HlsVideoSegmentFormat
import com.google.android.gms.cast.MediaInfo
import com.google.android.gms.cast.MediaLoadRequestData
import com.google.android.gms.cast.MediaMetadata
import com.google.android.gms.cast.MediaStatus
import com.google.android.gms.cast.MediaTrack
import com.google.android.gms.cast.framework.CastContext
import com.google.android.gms.cast.framework.CastOptions
import com.google.android.gms.cast.framework.CastSession
import com.google.android.gms.cast.framework.OptionsProvider
import com.google.android.gms.cast.framework.SessionManagerListener
import com.google.android.gms.cast.framework.SessionProvider
import com.google.android.gms.cast.framework.media.CastMediaOptions
import com.google.android.gms.cast.framework.media.NotificationOptions
import com.google.android.gms.cast.framework.media.RemoteMediaClient
import com.google.android.gms.common.images.WebImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Chromecast (D82): TVs, Google TV, Nest speakers and speaker groups play Marquee through the
 * Default Media Receiver. The phone picks a device, asks the server for a stream the receiver
 * plays (Cast profile below) and hands it over; the stream URL carries its own credential
 * (the session id), so the receiver fetches it directly from the server on the home network.
 * Progress is reported from the receiver's position, so resume, play counts and scrobbles work.
 */
class CastOptionsProvider : OptionsProvider {
    override fun getCastOptions(context: Context): CastOptions {
        // The system media notification and lock screen control what's casting.
        val notification = NotificationOptions.Builder().setTargetActivityClassName(MainActivity::class.java.name).build()
        val media = CastMediaOptions.Builder().setNotificationOptions(notification).setMediaSessionEnabled(true).build()
        return CastOptions.Builder()
            .setReceiverApplicationId(CastMediaControlIntent.DEFAULT_MEDIA_RECEIVER_APPLICATION_ID)
            .setCastMediaOptions(media)
            .setResumeSavedSession(true)
            .build()
    }

    override fun getAdditionalSessionProviders(context: Context): List<SessionProvider>? = null
}

/** What the Default Media Receiver plays: H.264 with AAC/MP3 (HLS in fMP4, or MP4 files) and common audio files. */
object CastProfile {
    val profile = DeviceProfile(
        containers = listOf("mp4", "m4v", "mp3", "m4a", "flac", "ogg", "opus", "wav"),
        videoCodecs = listOf("h264"),
        audioCodecs = listOf("aac", "mp3", "flac", "opus", "vorbis"),
        hls = true,
        hlsVideoCodecs = listOf("h264"),
        hlsAudioCodecs = listOf("aac", "mp3"),
        maxAudioChannels = 6,
        textSubtitles = true,
    )
}

class MarqueeCast(private val context: Context, private val marquee: Marquee) {
    /** Null where Google Play services (and so Cast) isn't available. Main thread only. */
    val castContext: CastContext? by lazy { runCatching { CastContext.getSharedInstance(context) }.getOrNull() }
    private val router by lazy { MediaRouter.getInstance(context) }
    private val selector = MediaRouteSelector.Builder()
        .addControlCategory(CastMediaControlIntent.categoryForCast(CastMediaControlIntent.DEFAULT_MEDIA_RECEIVER_APPLICATION_ID))
        .build()

    val available: Boolean get() = castContext != null

    private val _devices = MutableStateFlow<List<MediaRouter.RouteInfo>>(emptyList())
    /** Cast devices found on the network while discovery runs. */
    val devices: StateFlow<List<MediaRouter.RouteInfo>> = _devices
    private val _device = MutableStateFlow<String?>(null)
    /** The connected Cast device's name, or null. */
    val device: StateFlow<String?> = _device
    private val _playing = MutableStateFlow(false)
    val playing: StateFlow<Boolean> = _playing
    private val _position = MutableStateFlow(0L to 0L)
    /** The receiver's position and duration (ms). */
    val position: StateFlow<Pair<Long, Long>> = _position

    /** Set while a video screen is open: a device connected then is for the video, not music. */
    @Volatile var videoActive = false

    /** Told when the receiver finishes what it was playing (music moves to the next track). */
    var onFinished: (() -> Unit)? = null
    /** Told when casting stops, with the last position (playback continues on the phone). */
    var onEnded: ((positionMs: Long) -> Unit)? = null

    private var session: PlaybackSession? = null
    private var ticker: Job? = null

    private val routerCallback = object : MediaRouter.Callback() {
        override fun onRouteAdded(r: MediaRouter, route: MediaRouter.RouteInfo) = refresh()
        override fun onRouteRemoved(r: MediaRouter, route: MediaRouter.RouteInfo) = refresh()
        override fun onRouteChanged(r: MediaRouter, route: MediaRouter.RouteInfo) = refresh()
    }

    private fun refresh() {
        _devices.value = router.routes.filter { !it.isDefault && it.isEnabled && it.matchesSelector(selector) }.sortedBy { it.name }
    }

    private val sessionListener = object : SessionManagerListener<CastSession> {
        override fun onSessionStarted(s: CastSession, id: String) = connected(s)
        override fun onSessionResumed(s: CastSession, wasSuspended: Boolean) = connected(s)
        override fun onSessionEnded(s: CastSession, error: Int) = disconnected()
        override fun onSessionSuspended(s: CastSession, reason: Int) {}
        override fun onSessionStarting(s: CastSession) {}
        override fun onSessionStartFailed(s: CastSession, error: Int) { _device.value = null }
        override fun onSessionEnding(s: CastSession) { lastPosition = remote?.approximateStreamPosition ?: lastPosition }
        override fun onSessionResuming(s: CastSession, id: String) {}
        override fun onSessionResumeFailed(s: CastSession, error: Int) {}
    }

    private var lastPosition = 0L
    private var started = false

    /** Starts listening for Cast sessions (call once, on the main thread). */
    fun init() {
        if (started) return
        started = true
        castContext?.sessionManager?.addSessionManagerListener(sessionListener, CastSession::class.java)
        castContext?.sessionManager?.currentCastSession?.let { if (it.isConnected) connected(it) }
    }

    /** Looks for devices while a picker is open. */
    fun discover(on: Boolean) {
        if (!available) return
        if (on) {
            router.addCallback(selector, routerCallback, MediaRouter.CALLBACK_FLAG_REQUEST_DISCOVERY)
            refresh()
        } else router.removeCallback(routerCallback)
    }

    fun connect(route: MediaRouter.RouteInfo) = router.selectRoute(route)

    fun disconnect() {
        lastPosition = remote?.approximateStreamPosition ?: lastPosition
        castContext?.sessionManager?.endCurrentSession(true)
    }

    val remote: RemoteMediaClient? get() = castContext?.sessionManager?.currentCastSession?.remoteMediaClient

    private val remoteCallback = object : RemoteMediaClient.Callback() {
        override fun onStatusUpdated() {
            val r = remote ?: return
            val wasPlaying = _playing.value
            _playing.value = r.isPlaying || r.isBuffering
            _position.value = r.approximateStreamPosition to r.streamDuration.coerceAtLeast(0)
            if (wasPlaying != _playing.value) report(if (_playing.value) PlaybackProgress.State.PLAYING else PlaybackProgress.State.PAUSED)
            if (r.playerState == MediaStatus.PLAYER_STATE_IDLE && r.idleReason == MediaStatus.IDLE_REASON_FINISHED && session != null) {
                report(PlaybackProgress.State.PAUSED, finished = true)
                closeSession()
                onFinished?.invoke()
            }
        }
    }

    private fun connected(s: CastSession) {
        _device.value = s.castDevice?.friendlyName ?: "Chromecast"
        s.remoteMediaClient?.registerCallback(remoteCallback)
    }

    private fun disconnected() {
        _device.value = null
        _playing.value = false
        ticker?.cancel()
        report(PlaybackProgress.State.PAUSED)
        closeSession()
        onEnded?.invoke(lastPosition)
    }

    /** A stream URL the receiver can reach: the server's home-network address when known. */
    private fun castUrl(path: String): String {
        if (path.startsWith("http")) return path
        val base = marquee.server?.lanUrl ?: marquee.absolute("")!!
        return base.trimEnd('/') + path
    }

    /**
     * Plays an item on the connected device from positionMs: asks the server for a stream in
     * the Cast profile and loads it. Returns an error message, or null when it started.
     */
    suspend fun load(
        itemId: Long, positionMs: Long, title: String, subtitle: String?, artwork: String?, music: Boolean,
        fileId: Long? = null, audioStreamId: Long? = null, subtitleStreamId: Long? = null,
    ): String? {
        val r = withContext(Dispatchers.Main) { remote } ?: return "Not connected to a Cast device"
        val req = PlaybackRequest(itemId, CastProfile.profile, fileId = fileId, audioStreamId = audioStreamId,
            subtitleStreamId = subtitleStreamId, startMs = positionMs)
        val s = withContext(Dispatchers.IO) { runCatching { marquee.playback.startPlayback(req) } }.getOrElse { return it.message ?: "Couldn't start playback" }
        val meta = MediaMetadata(if (music) MediaMetadata.MEDIA_TYPE_MUSIC_TRACK else MediaMetadata.MEDIA_TYPE_MOVIE).apply {
            putString(MediaMetadata.KEY_TITLE, title)
            subtitle?.let { putString(if (music) MediaMetadata.KEY_ARTIST else MediaMetadata.KEY_SUBTITLE, it) }
            artwork?.let { addImage(WebImage(Uri.parse(it))) }
        }
        val info = MediaInfo.Builder(castUrl(s.url))
            .setStreamType(MediaInfo.STREAM_TYPE_BUFFERED)
            .setContentType(s.contentType ?: if (music) "audio/mp4" else "video/mp4")
            .setMetadata(meta)
            .setStreamDuration(s.durationMs)
        if (s.protocol == PlaybackSession.Protocol.HLS) {
            info.setHlsSegmentFormat(HlsSegmentFormat.FMP4)
            info.setHlsVideoSegmentFormat(HlsVideoSegmentFormat.FMP4)
        }
        val active = mutableListOf<Long>()
        s.subtitleUrl?.takeIf { s.subtitleFormat == PlaybackSession.SubtitleFormat.VTT }?.let { u ->
            info.setMediaTracks(listOf(MediaTrack.Builder(1, MediaTrack.TYPE_TEXT).setContentId(castUrl(u)).setContentType("text/vtt")
                .setSubtype(MediaTrack.SUBTYPE_SUBTITLES).setName("Subtitles").build()))
            active += 1
        }
        val load = MediaLoadRequestData.Builder().setMediaInfo(info.build()).setAutoplay(true).setCurrentTime(s.startMs)
        if (active.isNotEmpty()) load.setActiveTrackIds(active.toLongArray())
        return withContext(Dispatchers.Main) {
            closeSession()
            session = s
            r.load(load.build())
            startTicker()
            null
        }
    }

    fun toggle() { remote?.let { if (it.isPlaying) it.pause() else it.play() } }
    fun seek(ms: Long) { remote?.seek(com.google.android.gms.cast.MediaSeekOptions.Builder().setPosition(ms).build()) }
    fun pause() { remote?.pause() }

    /** Reports the receiver's position every 10 s while playing (resume, history). */
    private fun startTicker() {
        ticker?.cancel()
        ticker = marquee.scope.launch(Dispatchers.Main) {
            while (true) {
                delay(10_000)
                val r = remote ?: break
                lastPosition = r.approximateStreamPosition
                if (r.isPlaying) report(PlaybackProgress.State.PLAYING)
            }
        }
    }

    private fun report(state: PlaybackProgress.State, finished: Boolean = false) {
        val s = session ?: return
        val pos = if (finished) s.durationMs else remote?.approximateStreamPosition ?: lastPosition
        marquee.scope.launch { runCatching { marquee.playback.reportPlayback(s.id, PlaybackProgress(pos, state)) } }
    }

    private fun closeSession() {
        val s = session ?: return
        session = null
        marquee.scope.launch { runCatching { marquee.playback.stopPlayback(s.id) } }
    }
}
