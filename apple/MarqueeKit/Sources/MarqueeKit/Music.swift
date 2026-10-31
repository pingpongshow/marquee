import Foundation
import MarqueeAPI

public typealias Station = Components.Schemas.Station
public typealias RadioRequest = Components.Schemas.RadioRequest
public typealias Lyrics = Components.Schemas.Lyrics
public typealias MusicStatus = Components.Schemas.MusicStatus

/// Music intelligence (M6.5): radios, Muse, mixes, similar, lyrics and ratings.
@MainActor
public extension AppSession {
    private var musicAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    func musicStatus() async throws -> MusicStatus { try await musicAPI.musicStatus().ok.body.json }

    /// A station (MUSIC-3); pass `exclude` to continue one.
    func radio(_ req: RadioRequest) async throws -> Station {
        switch try await musicAPI.musicRadio(body: .json(req)) {
        case let .ok(ok): return try ok.body.json
        // not_analyzed: Soundprint hasn't analysed the tracks the station needs (yet).
        case let .conflict(e): throw MarqueeError((try? e.body.json.message) ?? "These tracks haven't been analysed yet")
        case let .serviceUnavailable(e): throw MarqueeError((try? e.body.json.message) ?? "The Soundprint analysis service isn't running")
        case let .badRequest(e): throw MarqueeError((try? e.body.json.message) ?? "Couldn't start the station")
        default: throw MarqueeError("Couldn't start the station")
        }
    }

    /// A playlist from a description (Muse, MUSIC-5).
    func muse(_ prompt: String, library: Int64? = nil, limit: Int = 40) async throws -> Station {
        try await musicAPI.musicMuse(body: .json(.init(prompt: prompt, limit: limit, libraryId: library))).ok.body.json
    }

    /// A path between two tracks (Sound Journey, MUSIC-4).
    func journey(from: Int64, to: Int64, length: Int = 15) async throws -> Station {
        try await musicAPI.musicJourney(body: .json(.init(fromId: from, toId: to, length: length))).ok.body.json
    }

    /// Daily mixes (MUSIC-7).
    func mixes(library: Int64? = nil) async throws -> [Station] {
        try await musicAPI.musicMixes(query: .init(libraryId: library)).ok.body.json
    }

    /// Tracks, albums or artists that sound like the item (MUSIC-2).
    func soundsLike(_ id: Int64, limit: Int = 20) async throws -> [Item] {
        try await musicAPI.soundsLike(path: .init(itemId: id), query: .init(limit: limit)).ok.body.json
    }

    /// The track's lyrics, or nil when it has none.
    func lyrics(_ id: Int64) async throws -> Lyrics? {
        switch try await musicAPI.getLyrics(path: .init(itemId: id)) {
        case let .ok(ok): return try ok.body.json
        case .notFound: return nil
        default: throw MarqueeError("Couldn't load lyrics")
        }
    }

    /// Searches OpenSubtitles for an item (PLAY-7). languages: ISO 639-1, comma-separated.
    func searchSubtitles(_ id: Int64, languages: String) async throws -> [Schemas.SubtitleResult] {
        switch try await musicAPI.searchSubtitles(path: .init(itemId: id), query: .init(languages: languages)) {
        case let .ok(ok): return try ok.body.json
        case let .badRequest(e): throw MarqueeError((try? e.body.json.message) ?? "Subtitle search isn't set up")
        case let .badGateway(e): throw MarqueeError((try? e.body.json.message) ?? "OpenSubtitles didn't answer")
        default: throw MarqueeError("Couldn't search for subtitles")
        }
    }

    /// Downloads a subtitle and returns its new stream id.
    func downloadSubtitle(_ id: Int64, result r: Schemas.SubtitleResult) async throws -> Int64 {
        switch try await musicAPI.downloadSubtitle(path: .init(itemId: id), body: .json(.init(fileId: r.fileId, language: r.language, release: r.release, hearingImpaired: r.hearingImpaired))) {
        case let .created(c): return try c.body.json.streamId
        case let .badGateway(e): throw MarqueeError((try? e.body.json.message) ?? "OpenSubtitles didn't answer")
        default: throw MarqueeError("Couldn't download the subtitle")
        }
    }

    /// Playback statistics: your own (ADM-4, "year in music"); days 0 = all time.
    /// Administrators see everyone's unless `userID` names one person (pass your own id for yours).
    func stats(days: Int, limit: Int = 10, userID: Int64? = nil) async throws -> Schemas.Stats {
        try await musicAPI.getStats(query: .init(days: days, userId: userID, limit: limit)).ok.body.json
    }

    /// Rates an item 0–10 (half stars; 10 = loved), or clears the rating with nil (MUSIC-11).
    /// Kept on the device and sent later when the server can't be reached (USER-18): returns
    /// false then.
    @discardableResult
    func rate(_ id: Int64, _ rating: Double?) async throws -> Bool {
        try await OfflineSync.shared.submit(.rating(id, rating))
    }
}

/// What the listener does with a radio's tracks (MUSIC-17), sent with every top-up so the
/// station steers away from quick skips and towards what's played through.
public struct RadioSteering: Sendable, Equatable {
    /// The latest of each kind that are sent.
    public static let keep = 100
    public private(set) var skipped: [Int64] = []
    public private(set) var finished: [Int64] = []
    /// Quick skips in a row; two top the station up early.
    public private(set) var skipsInARow = 0

    public init() {}

    /// Moved on in the first 30 seconds and before half way (the server's definition).
    public static func isQuickSkip(at time: Double, duration: Double) -> Bool {
        time < 30 && (duration <= 0 || time < duration / 2)
    }

    /// Reached 80% of the track.
    public static func isPlayedThrough(at time: Double, duration: Double) -> Bool {
        duration > 0 && time >= duration * 0.8
    }

    /// The listener moved on from a track at `time`. Returns true after two quick skips in a
    /// row: time to top the station up early with what it has learnt.
    public mutating func movedOn(from id: Int64, at time: Double, duration: Double) -> Bool {
        if Self.isPlayedThrough(at: time, duration: duration) {
            finish(id)
            return false
        }
        // Skipped part-way: not a quick skip, so the run of them is broken.
        guard Self.isQuickSkip(at: time, duration: duration) else { skipsInARow = 0; return false }
        Self.note(id, in: &skipped, dropFrom: &finished)
        skipsInARow += 1
        return skipsInARow >= 2
    }

    /// A track played through (80%, or to its end).
    public mutating func finish(_ id: Int64) {
        Self.note(id, in: &finished, dropFrom: &skipped)
        skipsInARow = 0
    }

    /// The station was topped up early: the skips so far have been answered.
    public mutating func toppedUp() { skipsInARow = 0 }

    /// Adds what's been learnt to a top-up request.
    public func apply(to r: inout RadioRequest) {
        r.skipped = skipped.isEmpty ? nil : skipped
        r.finished = finished.isEmpty ? nil : finished
    }

    /// Latest last, each track once and in one list only (the listener's last word counts).
    private static func note(_ id: Int64, in list: inout [Int64], dropFrom other: inout [Int64]) {
        other.removeAll { $0 == id }
        list.removeAll { $0 == id }
        list.append(id)
        if list.count > keep { list.removeFirst(list.count - keep) }
    }
}

/// The moods offered as stations (same as the web client).
public let musicMoods = ["Chill", "Energetic", "Focus", "Melancholy", "Party", "Romantic", "Dreamy", "Aggressive"]

/// Example Muse prompts.
public let museSuggestions = ["Rainy Sunday jazz", "Late-night drive synthwave", "Upbeat 80s pop for cleaning the house", "Acoustic songs for a quiet evening"]

/// One line of lyrics with its time in seconds (nil when unsynced).
public struct LyricLineView: Identifiable, Sendable {
    public let id: Int
    public let time: Double?
    public let text: String
}

public extension Lyrics {
    var parsedLines: [LyricLineView] {
        lines.enumerated().map { LyricLineView(id: $0.offset, time: $0.element.timeMs.map { Double($0) / 1000 }, text: $0.element.text) }
    }

    /// The index of the line being sung at `time`, for synced lyrics.
    func activeLine(at time: Double) -> Int? {
        guard synced else { return nil }
        var active: Int?
        for (i, l) in lines.enumerated() {
            guard let ms = l.timeMs else { continue }
            if Double(ms) / 1000 <= time + 0.15 { active = i } else { break }
        }
        return active
    }
}
