import XCTest
@testable import MarqueeKit

/// The VOD pages' filters: the query they make, whether any are on, and how they're kept.
final class VodFiltersTests: XCTestCase {
    func testDecadeIsTenYears() {
        let f = VodFilters(decade: 1980)
        XCTAssertEqual(f.yearFrom, 1980)
        XCTAssertEqual(f.yearTo, 1989)
        XCTAssertNil(VodFilters().yearFrom)
        XCTAssertNil(VodFilters().yearTo)
    }

    func testActive() {
        XCTAssertFalse(VodFilters().isActive)
        XCTAssertTrue(VodFilters(genre: "Drama").isActive)
        XCTAssertTrue(VodFilters(minRating: 7).isActive)
        XCTAssertTrue(VodFilters(watched: .inProgress).isActive)
        XCTAssertTrue(VodFilters(sourceId: "a").isActive)
    }

    func testStoredRoundTrip() {
        let f = VodFilters(genre: "Sci-Fi & Fantasy", decade: 2010, minRating: 8, watched: .unwatched, sourceId: "src1")
        XCTAssertEqual(VodFilters(stored: f.stored), f)
        // Nothing stored, or something unreadable: no filters.
        XCTAssertEqual(VodFilters(stored: ""), VodFilters())
        XCTAssertEqual(VodFilters(stored: "{not json"), VodFilters())
    }

    func testWatchedMatchesTheAPI() {
        XCTAssertEqual(VodFilters.Watched.allCases.map(\.rawValue), ["unwatched", "inProgress", "watched"])
        XCTAssertEqual(VodFilters.label(.unwatched, shows: true), "Unwatched episodes")
        XCTAssertEqual(VodFilters.label(.unwatched, shows: false), "Unwatched")
    }
}
