import Foundation
@testable import MarqueeKit
import Testing

/// Smart downloads' choosing: budgets, the order shows and music are kept in, and what goes.
struct SmartDownloadsTests {
    private typealias C = SmartSelection.Candidate
    private let gb: Int64 = 1_000_000_000

    @Test func fillStopsAtTheBudget() {
        let list = [C(id: 1, bytes: 400), C(id: 2, bytes: 400), C(id: 3, bytes: 100), C(id: 4, bytes: 50)]
        // 3 would fit after 2 doesn't, but what comes after the first that doesn't fit waits.
        #expect(SmartSelection.fill(list, budget: 900) == [1, 2, 3])
        #expect(SmartSelection.fill(list, budget: 700) == [1])
        #expect(SmartSelection.fill(list, budget: 0) == [])
    }

    @Test func fillCountsRepeatsOnceAndFreeItems() {
        let list = [C(id: 1, bytes: 500), C(id: 1, bytes: 500), C(id: 2, bytes: 0), C(id: 3, bytes: 500)]
        #expect(SmartSelection.fill(list, budget: 1000) == [1, 2, 3])
    }

    @Test func showsKeepTheNextEpisodesOfTheMostRecentlyWatchedFirst() {
        let old = SmartSelection.Show(id: 10, lastWatched: Date(timeIntervalSince1970: 100),
                                      unwatched: [C(id: 101, bytes: gb), C(id: 102, bytes: gb)])
        let recent = SmartSelection.Show(id: 20, lastWatched: Date(timeIntervalSince1970: 200),
                                         unwatched: [C(id: 201, bytes: gb), C(id: 202, bytes: gb), C(id: 203, bytes: gb), C(id: 204, bytes: gb)])
        // Three each, newest show first.
        #expect(SmartSelection.shows([old, recent], perShow: 3, budget: 100 * gb) == [201, 202, 203, 101, 102])
        // At the budget the show watched longest ago goes without.
        #expect(SmartSelection.shows([old, recent], perShow: 3, budget: 4 * gb) == [201, 202, 203, 101])
        #expect(SmartSelection.shows([old, recent], perShow: 3, budget: 3 * gb) == [201, 202, 203])
        #expect(SmartSelection.shows([old, recent], perShow: 1, budget: 100 * gb) == [201, 101])
    }

    @Test func showsWatchedAtTheSameTimeKeepTheirOrder() {
        let t = Date(timeIntervalSince1970: 0)
        let a = SmartSelection.Show(id: 1, lastWatched: t, unwatched: [C(id: 11, bytes: 1)])
        let b = SmartSelection.Show(id: 2, lastWatched: t, unwatched: [C(id: 21, bytes: 1)])
        #expect(SmartSelection.shows([a, b], perShow: 2, budget: 10) == [11, 21])
    }

    @Test func musicPutsFavouritesFirst() {
        let fav = [C(id: 1, bytes: 30), C(id: 2, bytes: 30)]
        let most = [C(id: 2, bytes: 30), C(id: 3, bytes: 30)]
        let recent = [C(id: 4, bytes: 30)]
        #expect(SmartSelection.music(favorites: fav, mostPlayed: most, recent: recent, budget: 1000) == [1, 2, 3, 4])
        #expect(SmartSelection.music(favorites: fav, mostPlayed: most, recent: recent, budget: 70) == [1, 2])
    }

    @Test func removalsAreWhatsNoLongerWanted() {
        // Watched episodes drop out of "the next unwatched", so they go.
        #expect(SmartSelection.removals(held: [1, 2, 3], wanted: [2, 3, 4]) == [1])
        #expect(SmartSelection.removals(held: [1, 2], wanted: []) == [1, 2])
        #expect(SmartSelection.additions(wanted: [2, 3, 4], present: [1, 2, 3]) == [4])
    }

    @Test func budgets() {
        #expect(SmartBudget.gigabytes(5).bytes(free: 0, held: 0) == 5 * gb)
        // A share of the free space counts what's already held as free.
        #expect(SmartBudget.percentOfFree(10).bytes(free: 90 * gb, held: 10 * gb) == 10 * gb)
        #expect(SmartBudget.percentOfFree(25).bytes(free: 0, held: 0) == 0)
        #expect(SmartBudget.percentOfFree(50).label == "Up to 50% of free space")
        #expect(SmartBudget.gigabytes(20).label == "20 GB")
    }

    @Test func estimates() {
        // An hour at 720p (2.4 Mbps) is about 1.08 GB.
        #expect(SmartSelection.estimatedBytes(durationMs: 3_600_000, quality: .medium) == 1_080_000_000)
        #expect(SmartSelection.estimatedBytes(durationMs: 3_600_000, quality: .low) < SmartSelection.estimatedBytes(durationMs: 3_600_000, quality: .high))
        // A four-minute track at 320 kbps.
        #expect(SmartSelection.estimatedBytes(durationMs: 240_000, quality: .original, isTrack: true, audioKbps: 320) == 9_600_000)
        #expect(SmartSelection.estimatedBytes(durationMs: nil, quality: .original, isTrack: true) > 0)
    }

    @Test func settingsRoundTrip() throws {
        var s = SmartDownloadSettings()
        #expect(s.wifiOnly && s.episodesPerShow == 3 && !s.showsEnabled && !s.musicEnabled)
        s.showsBudget = .percentOfFree(25)
        s.quality = .high
        let back = try JSONDecoder().decode(SmartDownloadSettings.self, from: JSONEncoder().encode(s))
        #expect(back == s)
    }
}

/// Time limits: when a kept status refuses offline plays, and the profile menu's line.
struct TimeLimitTests {
    @Test func refusal() {
        let now = Date(timeIntervalSince1970: 1000)
        #expect(TimeLimits.refusal(nil, now: now) == nil)
        #expect(TimeLimits.refusal(.init(limited: false, allowedNow: false), now: now) == nil)
        #expect(TimeLimits.refusal(.init(limited: true, allowedNow: true), now: now) == nil)
        #expect(TimeLimits.refusal(.init(limited: true, allowedNow: false, reason: "Time's up"), now: now) == "Time's up")
        #expect(TimeLimits.refusal(.init(limited: true, allowedNow: false), now: now) == TimeLimitReached.defaultReason)
        // Refused until a time that has passed: allowed again.
        #expect(TimeLimits.refusal(.init(limited: true, allowedNow: false, resetsAt: Date(timeIntervalSince1970: 999)), now: now) == nil)
        #expect(TimeLimits.refusal(.init(limited: true, allowedNow: false, resetsAt: Date(timeIntervalSince1970: 2000)), now: now) != nil)
    }

    @Test func leftLabel() {
        #expect(TimeLimits.leftLabel(.init(limited: true, allowedNow: true, minutesLeft: 45)) == "45 min left today")
        #expect(TimeLimits.leftLabel(.init(limited: true, allowedNow: true, minutesLeft: 65)) == "1 hr 5 min left today")
        #expect(TimeLimits.leftLabel(.init(limited: true, allowedNow: true, minutesLeft: 120)) == "2 hr left today")
        #expect(TimeLimits.leftLabel(.init(limited: false, allowedNow: true, minutesLeft: 45)) == nil)
        #expect(TimeLimits.leftLabel(.init(limited: true, allowedNow: true, allowedFrom: "07:00", allowedUntil: "20:00")) == "Viewing hours 07:00–20:00")
    }

    @Test func errorBody() {
        let body = Data(#"{"code":"time_limit","message":"Viewing hours are 07:00–20:00"}"#.utf8)
        #expect(TimeLimitReached.from(json: body)?.reason == "Viewing hours are 07:00–20:00")
        #expect(TimeLimitReached.from(json: Data(#"{"code":"forbidden","message":"no"}"#.utf8)) == nil)
        #expect(TimeLimitReached(reason: "  ").reason == TimeLimitReached.defaultReason)
    }
}
