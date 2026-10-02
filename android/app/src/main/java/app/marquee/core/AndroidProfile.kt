package app.marquee.core

import android.media.MediaCodecList
import android.media.MediaFormat
import app.marquee.api.models.DeviceProfile

/**
 * What this device plays, for the server's playback decision. ExoPlayer plays MKV, MP4 and
 * WebM files directly and HLS; video codecs depend on the device's decoders.
 */
object AndroidProfile {
    private fun hasDecoder(mime: String): Boolean = runCatching {
        MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.any { !it.isEncoder && it.supportedTypes.any { t -> t.equals(mime, true) } }
    }.getOrDefault(false)

    val profile: DeviceProfile by lazy {
        val video = buildList {
            add("h264")
            if (hasDecoder(MediaFormat.MIMETYPE_VIDEO_HEVC)) add("hevc")
            if (hasDecoder(MediaFormat.MIMETYPE_VIDEO_VP9)) add("vp9")
            if (hasDecoder(MediaFormat.MIMETYPE_VIDEO_AV1)) add("av1")
        }
        val audio = listOf("aac", "mp3", "opus", "vorbis", "flac", "alac", "ac3", "eac3")
        DeviceProfile(
            containers = listOf("mp4", "m4v", "mkv", "webm", "mov", "mp3", "m4a", "flac", "ogg", "opus"),
            videoCodecs = video,
            audioCodecs = audio,
            hls = true,
            hlsVideoCodecs = video.filter { it == "h264" || it == "hevc" },
            hlsAudioCodecs = listOf("aac", "ac3", "eac3", "mp3"),
            maxAudioChannels = 6,
            textSubtitles = true,
            hlsSubtitles = true,
        )
    }
}
