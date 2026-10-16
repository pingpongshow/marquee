import Foundation
import MarqueeAPI

public typealias ContinueEntry = Components.Schemas.ContinueEntry

/// Continue Watching with library titles and films and episodes on demand together (Home's row
/// and the page with all of them; LIVE-6, USER-12).
@MainActor
public extension AppSession {
    private var continueAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    /// A page of the whole list, newest first, with how many there are in all.
    func continueWatching(offset: Int = 0, limit: Int = 50) async throws -> (entries: [ContinueEntry], total: Int) {
        let page = try await continueAPI.continueWatching(query: .init(offset: offset, limit: limit)).ok.body.json
        return (page.entries, page.total)
    }

    /// Takes a film on demand, or an episode's whole show, off Continue Watching until it's played again.
    func removeVodFromContinueWatching(_ entry: ContinueEntry) async throws {
        let path: Operations.RemoveVodFromContinueWatching.Input.Path
        if let m = entry.vodMovie {
            path = .init(kind: .movie, vodId: m.id)
        } else if let e = entry.vodEpisode {
            path = .init(kind: .episode, vodId: e.id)
        } else {
            return
        }
        switch try await continueAPI.removeVodFromContinueWatching(path: path) {
        case .noContent: return
        case .notFound: throw MarqueeError("This isn't in Continue Watching any more.")
        default: throw MarqueeError("Couldn't remove it from Continue Watching.")
        }
    }
}

public extension ContinueEntry {
    /// The first line: the film's or video's title, or an episode's show.
    var title: String {
        if let item { return item._type == .episode ? (item.grandparentTitle ?? item.title) : item.title }
        if let vodMovie { return vodMovie.title }
        return vodEpisode?.seriesTitle ?? ""
    }

    /// The second line for one on demand: "On demand · 1985", "On demand · S1 · E3 · Pilot".
    var vodSubtitle: String {
        if let m = vodMovie { return (["On demand"] + [m.year.map(String.init)].compactMap { $0 }).joined(separator: " · ") }
        if let e = vodEpisode { return (["On demand", e.label] + (e.title.isEmpty ? [] : [e.title])).joined(separator: " · ") }
        return ""
    }

    /// Wide artwork for one on demand (server-relative): the film's backdrop, else the episode's
    /// still, else its show's poster.
    var vodArtPath: String? {
        if let m = vodMovie { return "/api/v1/livetv/vod/movies/\(m.id)/image?kind=backdrop" }
        return vodEpisode?.stillUrl ?? seriesPosterUrl
    }

    /// How far through (0…1) for the progress bar; nil when not started or the length is unknown.
    var progress: Double? {
        let total = durationMs > 0 ? durationMs : (vodMovie?.runtimeMs ?? vodEpisode?.runtimeMs ?? item?.durationMs ?? 0)
        guard positionMs > 0, total > 0 else { return nil }
        return min(1, max(0.02, Double(positionMs) / Double(total)))
    }

    /// The show an episode belongs to (library or on demand), as "lib:<id>" / "vod:<id>".
    private var showKey: String? {
        if let item, item._type == .episode, let show = item.grandparentId { return "lib:\(show)" }
        if let e = vodEpisode { return "vod:\(e.seriesId)" }
        return nil
    }

    /// Whether taking this entry off Continue Watching takes `other` with it: itself, and for an
    /// episode, the rest of its show (the server hides the whole show).
    func removes(_ other: ContinueEntry) -> Bool {
        if other.key == key { return true }
        guard let show = showKey else { return false }
        return other.showKey == show
    }
}
