import XCTest
@testable import MarqueeKit

/// The audio and subtitles a title's page lists.
final class TrackSummaryTests: XCTestCase {
    private let en = Locale(identifier: "en_US")
    private var nextID: Int64 = 0

    private func stream(_ kind: Schemas.MediaStream.KindPayload, _ language: String?, codec: String = "", profile: String? = nil,
                        channels: Int? = nil, title: String? = nil, forced: Bool = false, sdh: Bool = false) -> Schemas.MediaStream {
        nextID += 1
        return .init(id: nextID, kind: kind, codec: codec, profile: profile, language: language, title: title,
                     _default: false, forced: forced, hearingImpaired: sdh, external: false, channels: channels)
    }

    func testLabels() {
        let t = TrackSummary(streams: [
            stream(.video, nil, codec: "hevc"),
            stream(.audio, "eng", codec: "truehd", profile: "Dolby TrueHD + Dolby Atmos", channels: 8),
            stream(.audio, "eng", codec: "ac3", channels: 2, title: "Director's Commentary"),
            stream(.audio, "fre", codec: "dts", profile: "DTS-HD MA", channels: 6, title: "French 5.1"),
            stream(.audio, "eng", codec: "eac3", channels: 6, title: "MiNX - Small HD episodes"),
            stream(.subtitle, "eng", codec: "subrip"), stream(.subtitle, "eng", codec: "pgs"),
            stream(.subtitle, "eng", codec: "subrip", sdh: true), stream(.subtitle, "fre", codec: "subrip", forced: true),
            stream(.subtitle, "und", codec: "subrip"),
        ], locale: en)
        XCTAssertEqual(t.audio, ["English · Dolby TrueHD Atmos 7.1", "English · Dolby Digital Stereo · Director's Commentary",
                                 "French · DTS-HD MA 5.1", "English · Dolby Digital Plus 5.1"])
        XCTAssertEqual(t.subtitles, ["English", "English (SDH)", "French (Forced)", "Unknown"])
    }

    func testOnlyVideos() {
        let file = { (streams: [Schemas.MediaStream]) in
            Schemas.MediaVersion(id: 1, label: "", files: [.init(id: 1, size: 1, partIndex: 0, available: true, streams: streams)])
        }
        XCTAssertNil(TrackSummary(versions: [file([stream(.audio, "eng", codec: "flac", channels: 2)])], locale: en))
        let t = TrackSummary(versions: [file([stream(.video, nil, codec: "h264"), stream(.audio, "eng", codec: "aac", channels: 2)]),
                                        file([stream(.video, nil, codec: "hevc"), stream(.audio, "eng", codec: "aac", channels: 2)])], locale: en)
        XCTAssertEqual(t?.audio, ["English · AAC Stereo"])
        XCTAssertEqual(t?.subtitles, [])
    }
}
