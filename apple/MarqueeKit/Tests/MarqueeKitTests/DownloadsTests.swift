import Foundation
@testable import MarqueeKit
import Testing

/// Offline plays: which records are sent, and which the server's answer removes.
struct OfflineProgressTests {
    private func record(_ id: Int64, server: String?, user: Int64?) -> Downloads.PendingProgress {
        .init(itemID: id, positionMs: 0, watched: true, at: Date(timeIntervalSince1970: 0), server: server, user: user)
    }

    @Test func onlyThisServerAndPersonsPlaysAreSent() {
        let pending = [
            record(1, server: "a", user: 1),
            record(2, server: "a", user: 2),   // someone else
            record(3, server: "b", user: 1),   // another server
            record(4, server: nil, user: nil), // from before records were tagged
            record(5, server: "a", user: nil),
        ]
        let ids = Downloads.flushable(pending, server: "a", user: 1).map(\.itemID)
        #expect(ids == [1, 4, 5])
    }

    @Test func rejectedPlaysAreDroppedAndOthersKept() {
        #expect(Downloads.outcome(status: 204) == .sent)
        #expect(Downloads.outcome(status: 404) == .drop)
        #expect(Downloads.outcome(status: 400) == .drop)
        #expect(Downloads.outcome(status: 403) == .drop)
        #expect(Downloads.outcome(status: 401) == .keep) // signed out: send after signing in
        #expect(Downloads.outcome(status: 429) == .keep)
        #expect(Downloads.outcome(status: 500) == .keep)
        #expect(Downloads.outcome(status: nil) == .keep) // no answer
    }

    @Test func untaggedRecordsStillDecode() throws {
        let old = #"[{"itemID":7,"positionMs":10,"watched":false,"at":0}]"#
        let list = try JSONDecoder().decode([Downloads.PendingProgress].self, from: Data(old.utf8))
        #expect(list.first?.itemID == 7)
        #expect(list.first?.server == nil)
    }

    @Test func networkErrorsAreRetryable() {
        #expect(Downloads.isNetworkError(URLError(.cannotConnectToHost)))
        #expect(Downloads.isNetworkError(URLError(.timedOut)))
        #expect(!Downloads.isNetworkError(URLError(.badServerResponse)))
        #expect(!Downloads.isNetworkError(CocoaError(.fileWriteOutOfSpace)))
    }
}
