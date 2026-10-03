import Foundation
import MarqueeAPI

public typealias AudioFormat = Components.Schemas.AudioFormat
public typealias PlaybackDecision = Components.Schemas.PlaybackDecision

/// Audio quality labels (MUSIC-23): "FLAC · 24-bit/96 kHz", "MP3 · 320 kbps".
public extension AudioFormat {
    /// The codec as people know it.
    static func codecLabel(_ codec: String) -> String {
        let c = codec.lowercased()
        switch c {
        case "flac": return "FLAC"
        case "alac": return "ALAC"
        case "mp3": return "MP3"
        case "aac": return "AAC"
        case "opus": return "Opus"
        case "vorbis": return "Ogg Vorbis"
        case "wavpack": return "WavPack"
        case "ape": return "APE"
        default:
            if c.hasPrefix("pcm_") { return "PCM" }
            if c.hasPrefix("dsd_") { return "DSD" }
            return codec.uppercased()
        }
    }

    /// 44100 → "44.1", 96000 → "96".
    static func kilohertz(_ hz: Int) -> String {
        let k = Double(hz) / 1000
        return k == k.rounded() ? String(Int(k)) : String(format: "%.1f", k)
    }

    var codecLabel: String { Self.codecLabel(codec) }

    /// Lossless and more than CD quality.
    var isHiRes: Bool { lossless && ((bitDepth ?? 0) >= 24 || (sampleRate ?? 0) > 48000) }

    /// The full label for Now Playing.
    var label: String {
        if lossless {
            switch (bitDepth, sampleRate) {
            case let (bits?, rate?): return "\(codecLabel) · \(bits)-bit/\(Self.kilohertz(rate)) kHz"
            case let (nil, rate?): return "\(codecLabel) · \(Self.kilohertz(rate)) kHz"
            case let (bits?, nil): return "\(codecLabel) · \(bits)-bit"
            default: return codecLabel
            }
        }
        if let kbps = bitrateKbps, kbps > 0 { return "\(codecLabel) · \(kbps) kbps" }
        return codecLabel
    }

    /// The short label for track lists: "FLAC 24/96", "MP3 320".
    var shortLabel: String {
        if lossless {
            switch (bitDepth, sampleRate) {
            case let (bits?, rate?): return "\(codecLabel) \(bits)/\(Self.kilohertz(rate))"
            case let (nil, rate?): return "\(codecLabel) \(Self.kilohertz(rate))"
            default: return codecLabel
            }
        }
        if let kbps = bitrateKbps, kbps > 0 { return "\(codecLabel) \(kbps)" }
        return codecLabel
    }
}

/// What the server actually streams when it isn't the original file.
public struct StreamedAudio: Sendable, Equatable {
    public var codec: String?
    public var kbps: Int?

    /// Nil for direct play, or when the audio is copied as it is.
    init?(_ d: PlaybackDecision) {
        guard d.method != .directPlay, !d.audioCopy else { return nil }
        codec = d.audioCodec
        kbps = d.audioKbps
    }

    /// "AAC 256 kbps".
    public var label: String {
        [codec.map(AudioFormat.codecLabel) ?? "Transcoded", kbps.map { "\($0) kbps" }].compactMap { $0 }.joined(separator: " ")
    }
}
