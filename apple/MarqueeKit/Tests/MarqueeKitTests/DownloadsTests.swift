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

/// The recommended download quality, and what's said about each option.
struct DownloadQualityTests {
    private let plenty: Int64 = 64_000_000_000
    private let little: Int64 = 15_999_999_999

    @Test func songsAreHighOnEveryDevice() {
        #expect(Downloads.Quality.recommended(.song, isPad: false, freeBytes: plenty) == .high)
        #expect(Downloads.Quality.recommended(.song, isPad: true, freeBytes: plenty) == .high)
    }

    @Test func videosAreMediumOnIPhoneAndHighOnIPad() {
        #expect(Downloads.Quality.recommended(.video, isPad: false, freeBytes: plenty) == .medium)
        #expect(Downloads.Quality.recommended(.video, isPad: true, freeBytes: plenty) == .high)
    }

    @Test func littleFreeSpaceIsOneStepLower() {
        #expect(Downloads.Quality.recommended(.song, isPad: false, freeBytes: little) == .medium)
        #expect(Downloads.Quality.recommended(.song, isPad: true, freeBytes: little) == .medium)
        #expect(Downloads.Quality.recommended(.video, isPad: false, freeBytes: little) == .low)
        #expect(Downloads.Quality.recommended(.video, isPad: true, freeBytes: little) == .medium)
    }

    @Test func sixteenGigabytesFreeIsEnough() {
        #expect(Downloads.Quality.recommended(.song, isPad: false, freeBytes: 16_000_000_000) == .high)
        #expect(Downloads.Quality.recommended(.video, isPad: false, freeBytes: 16_000_000_000) == .medium)
    }

    @Test func unknownFreeSpaceDoesNotLowerIt() {
        #expect(Downloads.Quality.recommended(.video, isPad: true, freeBytes: nil) == .high)
    }

    @Test func labelsSayWhatEachKindGets() {
        #expect(Downloads.Quality.high.label(.video) == "High (1080p)")
        #expect(Downloads.Quality.low.label(.video) == "Low (480p)")
        #expect(Downloads.Quality.high.label(.song) == "High (256 kbps)")
        #expect(Downloads.Quality.medium.label(.song) == "Medium (160 kbps)")
        #expect(Downloads.Quality.original.label(.song) == "Original")
    }

    @Test func sizeGuides() {
        #expect(Downloads.Quality.medium.sizeGuide(.song) == "About 5 MB a song")
        #expect(Downloads.Quality.medium.sizeGuide(.video) == "About 1.1 GB an hour")
        #expect(Downloads.Quality.original.sizeGuide(.video).hasPrefix("Varies"))
    }

    /// Songs come as M4A now; the player goes by the file's extension.
    @Test func theFileKeepsTheExtensionItCameWith() {
        func ext(_ server: String?, _ suggested: String?, _ q: Downloads.Quality, track: Bool) -> String {
            Downloads.fileExtension(serverName: server, suggested: suggested, quality: q, isTrack: track)
        }
        #expect(ext("abc.m4a", nil, .medium, track: true) == "m4a")
        #expect(ext("abc.m4a", "other.mp4", .medium, track: true) == "m4a")
        #expect(ext(nil, "song.flac", .original, track: true) == "flac")
        #expect(ext("abc.mp4", nil, .medium, track: false) == "mp4")
        // Nothing to go by: what the conversion makes.
        #expect(ext(nil, nil, .medium, track: true) == "m4a")
        #expect(ext(nil, nil, .medium, track: false) == "mp4")
        #expect(ext(nil, "noextension", .original, track: false) == "mkv")
    }
}
