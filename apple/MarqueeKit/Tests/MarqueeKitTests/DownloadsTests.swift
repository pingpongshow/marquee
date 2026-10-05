import Foundation
@testable import MarqueeKit
import Testing

/// The old offline-play records (folded into the offline sync queue) and network errors.
struct OfflineProgressTests {
    private func record(_ id: Int64, server: String?, user: Int64?) -> Downloads.PendingProgress {
        .init(itemID: id, positionMs: 0, watched: true, at: Date(timeIntervalSince1970: 0), server: server, user: user)
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
