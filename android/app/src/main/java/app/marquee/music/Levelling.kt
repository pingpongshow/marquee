package app.marquee.music

import android.content.Context
import android.os.Handler
import androidx.annotation.OptIn
import androidx.media3.common.C
import androidx.media3.common.Format
import androidx.media3.common.Timeline
import androidx.media3.common.audio.AudioProcessor
import androidx.media3.common.audio.BaseAudioProcessor
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.Renderer
import androidx.media3.exoplayer.audio.AudioRendererEventListener
import androidx.media3.exoplayer.audio.AudioSink
import androidx.media3.exoplayer.audio.DefaultAudioSink
import androidx.media3.exoplayer.audio.MediaCodecAudioRenderer
import androidx.media3.exoplayer.mediacodec.MediaCodecSelector
import androidx.media3.exoplayer.source.MediaSource
import java.nio.ByteBuffer
import java.util.concurrent.ConcurrentHashMap
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.roundToInt

/**
 * Volume levelling (MUSIC-10): the maths. Gains are ReplayGain values (relative to -18 LUFS)
 * from the track's playback session.
 */
object Levelling {
    /** A track's levelling data: track gain, album gain (dB) and peak sample level (1.0 = full scale). */
    data class Gains(val trackDb: Double?, val albumDb: Double?, val peak: Double?)

    const val MAX_CUT_DB = -15.0
    /** Quiet tracks come up by at most this much, and never so far that their peak would clip. */
    const val MAX_BOOST_DB = 6.0

    /** The gain in dB: album gain when an album plays through (falling back to track gain), else track gain. */
    fun db(g: Gains?, albumMode: Boolean): Double =
        ((if (albumMode) g?.albumDb ?: g?.trackDb else g?.trackDb) ?: 0.0).coerceIn(MAX_CUT_DB, MAX_BOOST_DB)

    /**
     * The linear gain to apply. A boost is capped so peak × gain ≤ 1; without a known peak
     * there's no boost at all (a cut is always safe).
     */
    fun linear(g: Gains?, albumMode: Boolean): Float {
        val gain = 10.0.pow(db(g, albumMode) / 20)
        if (gain <= 1.0) return gain.toFloat()
        val peak = g?.peak?.takeIf { it > 0 } ?: return 1f
        return min(gain, (1.0 / peak).coerceAtLeast(1.0)).toFloat()
    }

    /** Whether each queue entry is part of an album playing in order (a neighbour from the same album, not shuffled). */
    fun albumOrder(albums: List<Long?>, shuffled: Boolean): List<Boolean> = albums.indices.map { i ->
        val a = albums[i]
        !shuffled && a != null && (albums.getOrNull(i - 1) == a || albums.getOrNull(i + 1) == a)
    }
}

/**
 * Levelling data for tracks, from each playback session as it starts; downloaded tracks' is
 * kept on the device so it applies offline. Read on the playback thread, written from anywhere.
 */
object TrackGains {
    private val map = ConcurrentHashMap<Long, Levelling.Gains>()
    /** Per track id: true when it should use album gain (an album playing in order). */
    val albumMode = ConcurrentHashMap<Long, Boolean>()
    @Volatile private var prefs: android.content.SharedPreferences? = null

    fun init(context: Context) {
        if (prefs != null) return
        val p = context.getSharedPreferences("marquee.gains", Context.MODE_PRIVATE)
        p.all.forEach { (k, v) ->
            val id = k.toLongOrNull() ?: return@forEach
            val parts = (v as? String)?.split('|') ?: return@forEach
            if (parts.size == 3) map[id] = Levelling.Gains(parts[0].toDoubleOrNull(), parts[1].toDoubleOrNull(), parts[2].toDoubleOrNull())
        }
        prefs = p
    }

    operator fun get(id: Long): Levelling.Gains? = map[id]
    fun has(id: Long) = map.containsKey(id)

    /** Keeps a track's levelling data; persist for downloaded tracks, which need it offline. */
    fun put(id: Long, g: Levelling.Gains, persist: Boolean = true) {
        val same = map[id] == g
        map[id] = g
        if (!persist || (same && prefs?.contains(id.toString()) == true)) return
        prefs?.edit()?.putString(id.toString(), listOf(g.trackDb, g.albumDb, g.peak).joinToString("|") { it?.toString() ?: "" })?.apply()
    }

    /** The linear gain for a track now. */
    fun linear(id: Long): Float = Levelling.linear(map[id], albumMode[id] == true)
}

/**
 * Applies the current track's levelling gain to the decoded audio, before it reaches the
 * device: so it's in effect from the very first sample, cuts and boosts alike, and changes
 * exactly at the track boundary on gapless and crossfade transitions.
 */
@OptIn(UnstableApi::class)
class LevelProcessor : BaseAudioProcessor() {
    /** The track whose audio is being processed (set on the playback thread at each stream change). */
    @Volatile private var trackId: Long? = null
    private var applied = 1f
    private var snap = true

    fun setTrack(id: Long?) {
        trackId = id
        snap = true // a new track starts at its own gain, not faded from the last one's
    }

    override fun onConfigure(inputAudioFormat: AudioProcessor.AudioFormat): AudioProcessor.AudioFormat =
        if (inputAudioFormat.encoding == C.ENCODING_PCM_16BIT || inputAudioFormat.encoding == C.ENCODING_PCM_FLOAT) inputAudioFormat
        else AudioProcessor.AudioFormat.NOT_SET

    override fun queueInput(inputBuffer: ByteBuffer) {
        val size = inputBuffer.remaining()
        if (size == 0) return
        val target = trackId?.let(TrackGains::linear) ?: 1f
        val from = if (snap) target else applied
        snap = false
        applied = target
        val out = replaceOutputBuffer(size)
        if (from == 1f && target == 1f) {
            out.put(inputBuffer)
        } else if (inputAudioFormat.encoding == C.ENCODING_PCM_16BIT) {
            val n = size / 2
            for (i in 0 until n) {
                // A change mid-track (gains that arrived late) ramps across the buffer instead of clicking.
                val g = if (from == target) target else from + (target - from) * i / n
                val v = (inputBuffer.getShort() * g).roundToInt()
                out.putShort(v.coerceIn(Short.MIN_VALUE.toInt(), Short.MAX_VALUE.toInt()).toShort())
            }
        } else {
            val n = size / 4
            for (i in 0 until n) {
                val g = if (from == target) target else from + (target - from) * i / n
                out.putFloat((inputBuffer.getFloat() * g).coerceIn(-1f, 1f))
            }
        }
        inputBuffer.position(inputBuffer.limit())
        out.flip()
    }

    override fun onFlush() { snap = true }
    override fun onReset() { trackId = null; applied = 1f; snap = true }
}

/** The audio renderer that tells its [LevelProcessor] which track each stream of audio belongs to. */
@OptIn(UnstableApi::class)
private class LevellingAudioRenderer(
    context: Context, selector: MediaCodecSelector, handler: Handler?, listener: AudioRendererEventListener?, sink: AudioSink,
    private val processor: LevelProcessor,
) : MediaCodecAudioRenderer(context, selector, false, handler, listener, sink) {
    /** Stream offset → track id, for streams read ahead but not yet output. */
    private val tracks = LinkedHashMap<Long, Long?>()

    override fun onStreamChanged(formats: Array<out Format>, startPositionUs: Long, offsetUs: Long, mediaPeriodId: MediaSource.MediaPeriodId) {
        tracks[offsetUs] = trackOf(mediaPeriodId)
        while (tracks.size > 8) tracks.remove(tracks.keys.first())
        super.onStreamChanged(formats, startPositionUs, offsetUs, mediaPeriodId)
    }

    // Called when output switches to a stream: right after the previous track's last sample.
    override fun onOutputStreamOffsetUsChanged(outputStreamOffsetUs: Long) {
        super.onOutputStreamOffsetUsChanged(outputStreamOffsetUs)
        processor.setTrack(tracks[outputStreamOffsetUs])
    }

    private fun trackOf(id: MediaSource.MediaPeriodId): Long? = runCatching {
        val tl = timeline
        val window = tl.getPeriodByUid(id.periodUid, Timeline.Period()).windowIndex
        tl.getWindow(window, Timeline.Window()).mediaItem.mediaId.toLongOrNull()
    }.getOrNull()
}

/** Renderers for music: the usual ones, with levelling in the audio path. */
@OptIn(UnstableApi::class)
class LevellingRenderersFactory(context: Context) : DefaultRenderersFactory(context) {
    override fun buildAudioRenderers(
        context: Context, extensionRendererMode: Int, mediaCodecSelector: MediaCodecSelector, enableDecoderFallback: Boolean,
        audioSink: AudioSink, eventHandler: Handler, eventListener: AudioRendererEventListener, out: ArrayList<Renderer>,
    ) {
        val processor = LevelProcessor()
        val sink = DefaultAudioSink.Builder(context).setAudioProcessors(arrayOf<AudioProcessor>(processor)).build()
        out.add(LevellingAudioRenderer(context, mediaCodecSelector, eventHandler, eventListener, sink, processor))
    }
}
