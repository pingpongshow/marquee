import Foundation

/// The audio tracks and subtitles a video has, as its page lists them: every version's,
/// each once. Labels match the web and Android pages.
public struct TrackSummary: Equatable, Sendable {
    public var audio: [String]
    public var subtitles: [String]

    public init(streams: [Schemas.MediaStream], locale: Locale = .current) {
        var audio: [String] = [], subs: [String] = []
        for s in streams {
            let label: String
            switch s.kind {
            case .audio: label = Self.audioLabel(s, locale: locale)
            case .subtitle:
                let flags = [s.forced ? "Forced" : nil, s.hearingImpaired ? "SDH" : nil].compactMap { $0 }.joined(separator: ", ")
                label = Self.language(s.language, locale) + (flags.isEmpty ? "" : " (\(flags))")
            default: continue
            }
            if s.kind == .audio, !audio.contains(label) { audio.append(label) }
            if s.kind == .subtitle, !subs.contains(label) { subs.append(label) }
        }
        self.audio = audio
        self.subtitles = subs
    }

    /// For a video's page: nil for anything without a video stream (music).
    public init?(versions: [Schemas.MediaVersion], locale: Locale = .current) {
        let streams = versions.flatMap { $0.files.first?.streams ?? [] }
        guard streams.contains(where: { $0.kind == .video }) else { return nil }
        self.init(streams: streams, locale: locale)
    }

    /// "English · Dolby TrueHD 7.1 · Commentary".
    static func audioLabel(_ s: Schemas.MediaStream, locale: Locale) -> String {
        var format = codecName(s.codec, s.profile)
        if let ch = s.channels, ch > 0 {
            format += " " + (ch == 1 ? "Mono" : ch == 2 ? "Stereo" : ch >= 6 ? "\(ch - 1).1" : "\(ch).0")
        }
        var parts = [language(s.language, locale), format]
        // Track titles worth showing say what the track is, not the release group or its format again.
        if let t = s.title?.trimmingCharacters(in: .whitespaces), t.range(of: usefulTitle, options: [.regularExpression, .caseInsensitive]) != nil {
            parts.append(t)
        }
        return parts.joined(separator: " · ")
    }

    private static let usefulTitle = "commentary|descripti|narrat|isolated|score|brazil|latin|castil|european|canad|quebec|mandarin|cantonese"

    private static let codecs = ["truehd": "Dolby TrueHD", "eac3": "Dolby Digital Plus", "ac3": "Dolby Digital", "aac": "AAC", "flac": "FLAC",
                                 "opus": "Opus", "mp3": "MP3", "mp2": "MP2", "vorbis": "Vorbis", "alac": "ALAC"]

    static func codecName(_ codec: String, _ profile: String?) -> String {
        let p = profile ?? ""
        if codec == "dts" { return p.contains("MA") ? "DTS-HD MA" : p.contains("HD") ? "DTS-HD" : p.contains("X") ? "DTS:X" : "DTS" }
        if codec.hasPrefix("pcm_") { return "PCM" }
        let name = codecs[codec] ?? codec.uppercased()
        return p.contains("Atmos") ? name + " Atmos" : name
    }

    static func language(_ code: String?, _ locale: Locale) -> String {
        guard let code, !code.isEmpty, code != "und" else { return "Unknown" }
        return locale.localizedString(forLanguageCode: code) ?? code.uppercased()
    }
}
