import AVFoundation
import MarqueeAPI
import VideoToolbox

/// What this Apple device plays natively (AVPlayer); the server repackages or transcodes the rest.
public enum AppleDeviceProfile {
    public static func current() -> Schemas.DeviceProfile {
        let hevc = VTIsHardwareDecodeSupported(kCMVideoCodecType_HEVC)
        var video = ["h264"]
        if hevc { video.append("hevc") }
        if VTIsHardwareDecodeSupported(kCMVideoCodecType_AV1) { video.append("av1") }
        var hdr: [String] = []
        if AVPlayer.eligibleForHDRPlayback {
            hdr = ["hdr10", "hlg"]
            if VTIsHardwareDecodeSupported(kCMVideoCodecType_DolbyVisionHEVC) { hdr.append("dolby_vision") }
        }
        let audio = ["aac", "mp3", "alac", "flac", "ac3", "eac3"]
        #if os(tvOS)
        let channels = 8
        #else
        let channels = 6 // AVPlayer downmixes for headphones and speakers
        #endif
        return .init(
            containers: ["mp4", "mov", "m4v", "mp3", "m4a", "flac", "wav", "aac"],
            videoCodecs: video,
            audioCodecs: audio,
            maxAudioChannels: channels,
            tenBit: hevc,
            hdr: hdr.compactMap { Schemas.DeviceProfile.HdrPayloadPayload(rawValue: $0) },
            hls: true,
            hlsVideoCodecs: video,
            hlsAudioCodecs: audio,
            textSubtitles: false,
            assSubtitles: false,
            hlsSubtitles: true
        )
    }
}
