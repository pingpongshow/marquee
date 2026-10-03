import Foundation
import MarqueeAPI

public typealias ListeningRecap = Components.Schemas.ListeningRecap
public typealias RecapEntry = Components.Schemas.RecapEntry
public typealias MuseVideoResult = Components.Schemas.MuseVideoResult

/// Year in music (MUSIC-22) and Muse for movies and shows (USER-15).
@MainActor
public extension AppSession {
    private var recapAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    /// Years with music in your history, newest first.
    func recapYears() async throws -> [Int] {
        try await recapAPI.recapYears().ok.body.json
    }

    func recap(year: Int) async throws -> ListeningRecap {
        try await recapAPI.listeningRecap(query: .init(year: year)).ok.body.json
    }

    /// Makes (or refreshes) "Your Top Songs <year>".
    func saveRecapPlaylist(year: Int) async throws -> Playlist {
        switch try await recapAPI.recapPlaylist(query: .init(year: year)) {
        case .created(let c): return try c.body.json
        case .notFound: throw MarqueeError("There's no music to save for \(year).")
        default: throw MarqueeError("Couldn't save the playlist.")
        }
    }

    /// Movies and shows matching a description, best first.
    func museVideo(_ prompt: String, library: Int64? = nil, limit: Int = 40) async throws -> MuseVideoResult {
        switch try await recapAPI.museVideo(body: .json(.init(prompt: prompt, libraryId: library, limit: limit))) {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Muse didn't understand that.")
        case .serviceUnavailable(let r): throw MarqueeError((try? r.body.json.message) ?? "Muse isn't available right now.")
        default: throw MarqueeError("Muse didn't answer.")
        }
    }
}

/// Example Muse prompts for movies and shows.
public let videoMuseSuggestions = ["90s sci-fi with time travel", "feel-good comedy under 90 minutes",
                                   "acclaimed war films I haven't seen", "family animation"]
