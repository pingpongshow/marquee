import Foundation
import XCTest
@testable import MarqueeKit

/// Continue Watching entries on demand: their lines, artwork, progress and what removing one takes with it.
final class ContinueEntryTests: XCTestCase {
    private func movie(_ id: Int64, runtime: Int64? = 6_000_000, pos: Int64 = 1_500_000, dur: Int64 = 0) -> ContinueEntry {
        ContinueEntry(kind: .vodMovie, key: "vodMovie:\(id)",
                      vodMovie: VodMovie(id: id, title: "Film \(id)", year: 1985, runtimeMs: runtime, watched: false),
                      positionMs: pos, durationMs: dur, lastWatchedAt: Date())
    }

    private func episode(_ id: Int64, series: Int64, title: String = "Pilot") -> ContinueEntry {
        ContinueEntry(kind: .vodEpisode, key: "vodEpisode:\(id)",
                      vodEpisode: VodEpisode(id: id, seriesId: series, seriesTitle: "Show \(series)", season: 1, episode: 3, title: title, watched: false),
                      seriesPosterUrl: "/poster/\(series)", positionMs: 60_000, durationMs: 120_000, lastWatchedAt: Date())
    }

    func testLines() {
        XCTAssertEqual(movie(5).title, "Film 5")
        XCTAssertEqual(movie(5).vodSubtitle, "VOD · 1985")
        XCTAssertEqual(episode(7, series: 2).title, "Show 2")
        XCTAssertEqual(episode(7, series: 2).vodSubtitle, "VOD · S1 · E3 · Pilot")
        XCTAssertEqual(episode(7, series: 2, title: "").vodSubtitle, "VOD · S1 · E3")
    }

    func testArtwork() {
        XCTAssertEqual(movie(5).vodArtPath, "/api/v1/livetv/vod/movies/5/image?kind=backdrop")
        // No still: the show's poster.
        XCTAssertEqual(episode(7, series: 2).vodArtPath, "/poster/2")
    }

    func testProgress() {
        // The entry's length first, else the film's runtime.
        XCTAssertEqual(movie(5, pos: 1_500_000, dur: 3_000_000).progress!, 0.5, accuracy: 0.001)
        XCTAssertEqual(movie(5, runtime: 6_000_000, pos: 1_500_000).progress!, 0.25, accuracy: 0.001)
        XCTAssertNil(movie(5, runtime: nil, pos: 1_500_000).progress, "length unknown")
        XCTAssertNil(movie(5, pos: 0).progress, "not started")
        XCTAssertEqual(episode(7, series: 2).progress!, 0.5, accuracy: 0.001)
    }

    func testRemovingAnEpisodeTakesItsShow() {
        let e = episode(7, series: 2)
        XCTAssertTrue(e.removes(e))
        XCTAssertTrue(e.removes(episode(8, series: 2)))
        XCTAssertFalse(e.removes(episode(9, series: 3)))
        XCTAssertFalse(e.removes(movie(7)))
        let m = movie(5)
        XCTAssertTrue(m.removes(m))
        XCTAssertFalse(m.removes(movie(6)))
        XCTAssertFalse(m.removes(e))
    }
}
