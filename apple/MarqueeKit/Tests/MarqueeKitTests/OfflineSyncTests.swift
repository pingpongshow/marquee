import Foundation
@testable import MarqueeKit
import Testing

/// The offline sync queue (USER-18): coalescing, scoping, order, outcomes, persistence.
struct PendingQueueTests {
    private func tagged(_ c: PendingChange, server: String? = "a", user: Int64? = 1, at: TimeInterval = 0) -> PendingChange {
        var c = c
        c.server = server
        c.user = user
        c.at = Date(timeIntervalSince1970: at)
        return c
    }

    @Test func keepsOnlyTheLatestChangePerItemAndKind() {
        var q = PendingQueue()
        q.add(tagged(.rating(1, 6), at: 1))
        q.add(tagged(.watched(1, true), at: 2))
        q.add(tagged(.rating(1, 8), at: 3))
        q.add(tagged(.watchlist(2, true), at: 4))
        q.add(tagged(.watchlist(2, false), at: 5))
        q.add(tagged(.rating(1, nil), at: 6)) // cleared
        #expect(q.entries.count == 3)
        #expect(q.latest(.rating, item: 1, server: "a", user: 1)?.rating == nil)
        #expect(q.latest(.rating, item: 1, server: "a", user: 1)?.at == Date(timeIntervalSince1970: 6))
        #expect(q.latest(.watchlist, item: 2, server: "a", user: 1)?.on == false)
        // A comment and deleting your own review are the same slot; someone else's isn't.
        q.add(tagged(.comment(3, "hi")))
        q.add(tagged(.deleteReview(3, user: 1)))
        q.add(tagged(.deleteReview(3, user: 9)))
        #expect(q.entries.filter { $0.kind == .comment }.count == 2)
    }

    @Test func unfinishedProgressCoalescesButEveryFinishedPlayCounts() {
        var q = PendingQueue()
        q.add(tagged(.progress(1, positionMs: 1000, watched: false), at: 1))
        q.add(tagged(.progress(1, positionMs: 5000, watched: false), at: 2))
        q.add(tagged(.progress(1, positionMs: 9000, watched: true), at: 3))
        q.add(tagged(.progress(1, positionMs: 9000, watched: true), at: 4))
        #expect(q.entries.map(\.positionMs) == [5000, 9000, 9000])
    }

    @Test func changesAreScopedToServerAndPerson() {
        var q = PendingQueue()
        q.add(tagged(.rating(1, 6)))
        q.add(tagged(.rating(1, 4), user: 2))         // another profile, same item: its own entry
        q.add(tagged(.rating(2, 6), server: "b"))     // another server
        q.add(tagged(.progress(4, positionMs: 0, watched: true), server: nil, user: nil)) // from before tags
        #expect(q.entries.count == 4)
        #expect(q.flushable(server: "a", user: 1).map(\.itemID) == [1, 4])
        #expect(q.flushable(server: "a", user: 2).map(\.rating) == [4, nil])
        #expect(q.latest(.rating, item: 1, server: "a", user: 2)?.rating == 4)
        #expect(q.count(server: "b", user: 1) == 2)
    }

    @Test func sendsInTheOrderChangesWereMade() {
        var q = PendingQueue()
        q.add(tagged(.watched(1, true), at: 1))
        q.add(tagged(.watchlist(2, true), at: 2))
        q.add(tagged(.rating(3, 10), at: 3))
        q.add(tagged(.watched(1, false), at: 4)) // a change again moves to the end
        #expect(q.flushable(server: "a", user: 1).map(\.itemID) == [2, 3, 1])
    }

    @Test func theAnswerDecidesWhetherAChangeStays() {
        #expect(PendingQueue.outcome(status: 204) == .sent)
        #expect(PendingQueue.outcome(status: 404) == .drop)
        #expect(PendingQueue.outcome(status: 400) == .drop)
        #expect(PendingQueue.outcome(status: 403) == .drop)
        #expect(PendingQueue.outcome(status: 401) == .keep) // signed out: send after signing in
        #expect(PendingQueue.outcome(status: 429) == .keep)
        #expect(PendingQueue.outcome(status: 500) == .keep)
        #expect(PendingQueue.outcome(status: nil) == .keep) // no answer
        var q = PendingQueue()
        let a = tagged(.rating(1, 6)), b = tagged(.watched(2, true)), c = tagged(.watchlist(3, true))
        q.add(a); q.add(b); q.add(c)
        q.resolve(a, PendingQueue.outcome(status: 204))
        q.resolve(b, PendingQueue.outcome(status: 404))
        q.resolve(c, PendingQueue.outcome(status: 401))
        #expect(q.entries == [c])
    }

    @Test func survivesARestart() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "marquee-sync-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let url = dir.appending(path: "pending.json")
        var q = PendingQueue()
        q.add(tagged(.rating(1, 7), at: 10))
        q.add(tagged(.comment(2, "great")))
        q.add(tagged(.progress(3, positionMs: 1234, watched: false)))
        try q.save(to: url)
        #expect(PendingQueue.load(from: url) == q)
        #expect(PendingQueue.load(from: dir.appending(path: "missing.json")).entries.isEmpty)
    }

    @MainActor @Test func foldsInTheOldOfflinePlayQueue() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "marquee-sync-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let legacy = dir.appending(path: "legacy.json"), url = dir.appending(path: "pending.json")
        let old = #"[{"itemID":7,"positionMs":10,"watched":false,"at":0,"server":"a","user":1},{"itemID":8,"positionMs":99,"watched":true,"at":5}]"#
        try Data(old.utf8).write(to: legacy)
        let sync = OfflineSync(fileURL: url, legacyProgressURL: legacy)
        #expect(sync.queue.entries.map(\.itemID) == [7, 8])
        #expect(sync.queue.entries.first?.kind == .progress)
        #expect(sync.queue.entries.last?.watched == true)
        #expect(!FileManager.default.fileExists(atPath: legacy.path))
        // And it's on disk for the next launch.
        #expect(OfflineSync(fileURL: url, legacyProgressURL: nil).queue == sync.queue)
    }

    @Test func networkFailuresAreKeptNotShown() {
        #expect(OfflineSync.isNetworkFailure(URLError(.notConnectedToInternet)))
        #expect(OfflineSync.isNetworkFailure(URLError(.timedOut)))
        #expect(!OfflineSync.isNetworkFailure(MarqueeError("nope")))
    }
}
