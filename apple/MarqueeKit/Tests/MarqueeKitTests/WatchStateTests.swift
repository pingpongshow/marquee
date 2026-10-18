import Foundation
import MarqueeAPI
import XCTest
@testable import MarqueeKit

/// Which watched marks an item offers: Mark Watched unless fully watched, Mark Unwatched when
/// played or part-way through (Continue Watching's titles offer both).
final class WatchStateTests: XCTestCase {
    private func video(_ type: Components.Schemas.ItemType = .movie, offset: Int64? = nil, views: Int? = nil) -> Item {
        Item(id: 1, libraryId: 1, _type: type, title: "T", childCount: 0, leafCount: 0, available: true,
             matchState: .local, addedAt: Date(), viewOffsetMs: offset, viewCount: views)
    }

    private func show(_ type: Components.Schemas.ItemType = .show, leaves: Int, watched: Int?) -> Item {
        Item(id: 2, libraryId: 1, _type: type, title: "S", childCount: 1, leafCount: leaves, available: true,
             matchState: .local, addedAt: Date(), watchedLeafCount: watched)
    }

    func testVideos() {
        let fresh = video()
        XCTAssertFalse(fresh.isFullyWatched)
        XCTAssertFalse(fresh.hasWatchState, "nothing to unmark")

        // Part-way through (Continue Watching): both marks.
        for type in [Components.Schemas.ItemType.movie, .episode, .video] {
            let started = video(type, offset: 60_000, views: 0)
            XCTAssertFalse(started.isFullyWatched)
            XCTAssertTrue(started.hasWatchState)
        }

        let played = video(views: 2)
        XCTAssertTrue(played.isFullyWatched)
        XCTAssertTrue(played.hasWatchState)

        // Watched before and part-way through again: both marks.
        let rewatch = video(offset: 5_000, views: 1)
        XCTAssertFalse(rewatch.isFullyWatched)
        XCTAssertTrue(rewatch.hasWatchState)
    }

    func testShowsAndSeasons() {
        for type in [Components.Schemas.ItemType.show, .season] {
            XCTAssertFalse(show(type, leaves: 10, watched: nil).hasWatchState)
            XCTAssertFalse(show(type, leaves: 10, watched: nil).isFullyWatched)
            XCTAssertTrue(show(type, leaves: 10, watched: 3).hasWatchState)
            XCTAssertFalse(show(type, leaves: 10, watched: 3).isFullyWatched)
            XCTAssertTrue(show(type, leaves: 10, watched: 10).isFullyWatched)
            XCTAssertTrue(show(type, leaves: 10, watched: 10).hasWatchState)
        }
    }
}
