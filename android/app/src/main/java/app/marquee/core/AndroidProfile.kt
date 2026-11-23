package app.marquee.core

import android.content.Context
import android.hardware.display.DisplayManager
import android.media.MediaCodecInfo
import android.media.MediaCodecInfo.CodecProfileLevel
import android.media.MediaCodecList
import android.media.MediaFormat
import android.os.Build
import android.view.Display
import app.marquee.api.models.DeviceProfile

/**
 * What this device plays, for the server's playback decision. ExoPlayer plays MKV, MP4 and
 * WebM files directly and HLS; video codecs, 10-bit and HDR depend on the device's decoders,
 * and HDR on its screen too.
 */
object AndroidProfile {
    /** What the video decoders can do beyond 8-bit SDR, and the tallest picture they take (0 = unknown). */
    internal data class VideoDecoders(val tenBit: Boolean = false, val hdr10: Boolean = false, val hdr10Plus: Boolean = false,
        val dolbyVision: Boolean = false, val maxHeight: Int = 0)

    private var appContext: Context? = null

    /** Called once as the app starts: the screen's HDR formats are read from the system. */
    fun init(context: Context) { appContext = context.applicationContext }

    private fun decoders(): List<MediaCodecInfo> = runCatching {
        MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.filter { !it.isEncoder }
    }.getOrDefault(emptyList())

    private fun hasDecoder(all: List<MediaCodecInfo>, mime: String): Boolean = all.any { it.supportedTypes.any { t -> t.equals(mime, true) } }

    /** Whether a decoder for [mime] has one of [profiles]. */
    private fun hasProfile(all: List<MediaCodecInfo>, mime: String, vararg profiles: Int): Boolean = all.any { info ->
        info.supportedTypes.any { it.equals(mime, true) } &&
            runCatching { info.getCapabilitiesForType(mime).profileLevels.any { it.profile in profiles } }.getOrDefault(false)
    }

    /**
     * The tallest picture the hardware decoders take (the software ones claim sizes they can't
     * keep up with); every decoder's where the system doesn't say which are which.
     */
    private fun maxHeight(all: List<MediaCodecInfo>, mimes: List<String>): Int {
        val hardware = if (Build.VERSION.SDK_INT >= 29) all.filter { it.isHardwareAccelerated } else all
        return hardware.maxOfOrNull { info ->
            mimes.filter { m -> info.supportedTypes.any { it.equals(m, true) } }.maxOfOrNull { m ->
                runCatching { info.getCapabilitiesForType(m).videoCapabilities?.supportedHeights?.upper }.getOrNull() ?: 0
            } ?: 0
        } ?: 0
    }

    private val decoders: List<MediaCodecInfo> by lazy { decoders() }

    private val video: VideoDecoders by lazy {
        val hevc = MediaFormat.MIMETYPE_VIDEO_HEVC
        VideoDecoders(
            // 10-bit means HEVC Main 10, as the web and Apple players take it.
            tenBit = hasProfile(decoders, hevc, CodecProfileLevel.HEVCProfileMain10, CodecProfileLevel.HEVCProfileMain10HDR10, CodecProfileLevel.HEVCProfileMain10HDR10Plus),
            hdr10 = hasProfile(decoders, hevc, CodecProfileLevel.HEVCProfileMain10HDR10, CodecProfileLevel.HEVCProfileMain10HDR10Plus),
            hdr10Plus = hasProfile(decoders, hevc, CodecProfileLevel.HEVCProfileMain10HDR10Plus),
            dolbyVision = hasDecoder(decoders, MediaFormat.MIMETYPE_VIDEO_DOLBY_VISION),
            maxHeight = maxHeight(decoders, listOf(MediaFormat.MIMETYPE_VIDEO_AVC, hevc, MediaFormat.MIMETYPE_VIDEO_VP9, MediaFormat.MIMETYPE_VIDEO_AV1)),
        )
    }

    /** The HDR formats the screen (a TV: the one plugged in now) shows; none when it can't be asked. */
    @Suppress("DEPRECATION")
    private fun displayHdrTypes(): Set<Int> = runCatching {
        appContext?.getSystemService(DisplayManager::class.java)?.getDisplay(Display.DEFAULT_DISPLAY)?.hdrCapabilities?.supportedHdrTypes?.toSet()
    }.getOrNull().orEmpty()

    /**
     * The HDR formats to play as they are: only those both a decoder and the screen take. Anything
     * else the server tone maps to SDR, which looks right; HDR sent to a screen without it doesn't.
     */
    internal fun hdrFormats(d: VideoDecoders, displayTypes: Set<Int>): List<DeviceProfile.Hdr> = buildList {
        if (d.hdr10 && Display.HdrCapabilities.HDR_TYPE_HDR10 in displayTypes) add(DeviceProfile.Hdr.HDR10)
        if (d.hdr10Plus && Display.HdrCapabilities.HDR_TYPE_HDR10_PLUS in displayTypes) add(DeviceProfile.Hdr.HDR10PLUS)
        // HLG is plain Main 10 with its own transfer curve: any 10-bit decoder does.
        if (d.tenBit && Display.HdrCapabilities.HDR_TYPE_HLG in displayTypes) add(DeviceProfile.Hdr.HLG)
        if (d.dolbyVision && Display.HdrCapabilities.HDR_TYPE_DOLBY_VISION in displayTypes) add(DeviceProfile.Hdr.DOLBY_VISION)
    }

    private val base: DeviceProfile by lazy {
        val codecs = buildList {
            add("h264")
            if (hasDecoder(decoders, MediaFormat.MIMETYPE_VIDEO_HEVC)) add("hevc")
            if (hasDecoder(decoders, MediaFormat.MIMETYPE_VIDEO_VP9)) add("vp9")
            if (hasDecoder(decoders, MediaFormat.MIMETYPE_VIDEO_AV1)) add("av1")
        }
        val audio = listOf("aac", "mp3", "opus", "vorbis", "flac", "alac", "ac3", "eac3")
        DeviceProfile(
            containers = listOf("mp4", "m4v", "mkv", "webm", "mov", "mp3", "m4a", "flac", "ogg", "opus"),
            videoCodecs = codecs,
            audioCodecs = audio,
            hls = true,
            hlsVideoCodecs = codecs.filter { it == "h264" || it == "hevc" },
            hlsAudioCodecs = listOf("aac", "ac3", "eac3", "mp3"),
            maxAudioChannels = 6,
            // ExoPlayer shows WebVTT sidecars, so files with subtitles can still play directly.
            textSubtitles = true,
        )
    }

    /**
     * 8-bit SDR only, as every device plays: what a video is asked for again with when it wouldn't
     * play as it is (a 10-bit kind the decoder doesn't take after all, like H.264 High 10).
     */
    val cautious: DeviceProfile get() = base

    /**
     * What the device plays now. Without 10-bit and HDR here the server would convert every such
     * video (most 4K films), tone mapped, even for a screen that shows HDR.
     */
    val profile: DeviceProfile
        get() = base.copy(
            tenBit = video.tenBit,
            hdr = hdrFormats(video, displayHdrTypes()),
            maxHeight = video.maxHeight.takeIf { it > 0 },
        )
}
