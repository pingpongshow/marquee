import Foundation
import MarqueeAPI

public typealias VodMovie = Components.Schemas.VodMovie
public typealias VodMovieDetail = Components.Schemas.VodMovieDetail
public typealias VodStatus = Components.Schemas.VodStatus
public typealias VodCategory = Components.Schemas.VodCategory
public typealias VodSort = Operations.ListVodMovies.Input.Query.SortPayload

/// Movies on demand under Live TV (LIVE-6): films from M3U sources (e.g. Pluto TV), played by
/// passing the source's HLS through the server, with per-viewer resume and watched state.
@MainActor
public extension AppSession {
    private var vodAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    /// Whether movies on demand are on and usable for this profile. UI tests force "off" with
    /// -marquee-test-vod-off (and -marquee-test-live-off, since they follow Live TV's permission).
    func vodStatus() async throws -> VodStatus {
        let args = ProcessInfo.processInfo.arguments
        if args.contains("-marquee-test-vod-off") || args.contains("-marquee-test-live-off") {
            return VodStatus(enabled: false, movies: 0, matched: 0, unmatched: 0, matching: false, sources: [])
        }
        return try await vodAPI.vodStatus().ok.body.json
    }

    func vodCategories() async throws -> [VodCategory] {
        try await vodAPI.listVodCategories().ok.body.json
    }

    /// A page of films, with how many there are in all.
    func vodMovies(category: String? = nil, query: String? = nil, sort: VodSort = .title, offset: Int = 0, limit: Int = 120) async throws -> (movies: [VodMovie], total: Int) {
        let page = try await vodAPI.listVodMovies(query: .init(category: category, q: query, sort: sort, limit: limit, offset: offset)).ok.body.json
        return (page.movies, page.total)
    }

    func vodContinueWatching() async throws -> [VodMovie] {
        try await vodAPI.vodContinueWatching().ok.body.json
    }

    func vodMovie(_ id: Int64) async throws -> VodMovieDetail {
        switch try await vodAPI.getVodMovie(path: .init(movieId: id)) {
        case let .ok(ok): return try ok.body.json
        case .notFound: throw MarqueeError("That movie isn't available any more")
        case .forbidden: throw MarqueeError("This movie isn't available on this profile")
        default: throw MarqueeError("Couldn't load the movie")
        }
    }

    func setVodWatched(_ id: Int64, _ watched: Bool) async throws {
        if watched {
            _ = try await vodAPI.markVodWatched(path: .init(movieId: id)).noContent
        } else {
            _ = try await vodAPI.markVodUnwatched(path: .init(movieId: id)).noContent
        }
    }

    /// Starts a film: the session id, the HLS URL and the caller's resume point (ms).
    func playVod(_ id: Int64) async throws -> (id: String, url: URL, startMs: Int64) {
        switch try await vodAPI.playVodMovie(path: .init(movieId: id)) {
        case let .ok(ok):
            let s = try ok.body.json
            guard let url = absolute(s.url) else { throw MarqueeError("Not connected") }
            return (s.id, url, s.startMs ?? 0)
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "This movie isn't available on this profile")
        case .notFound: throw MarqueeError("That movie isn't available any more")
        default: throw MarqueeError("Couldn't start the movie")
        }
    }

    /// Where the viewer is, so the film resumes there (and counts as watched near the end).
    func reportVod(_ session: String, positionMs: Int64, durationMs: Int64?) async {
        _ = try? await vodAPI.reportVodPlayback(path: .init(sessionId: session), body: .json(.init(positionMs: positionMs, durationMs: durationMs)))
    }

    func stopVod(_ session: String) async {
        _ = try? await vodAPI.stopVodSession(path: .init(sessionId: session))
    }

    /// Reloads every source now (admin only).
    func refreshVod() async throws -> VodStatus {
        switch try await vodAPI.refreshVod() {
        case let .ok(ok): return try ok.body.json
        case .forbidden: throw MarqueeError("Only an admin can refresh movies")
        default: throw MarqueeError("Couldn't refresh the movies")
        }
    }

    /// A film's poster or backdrop (served without a token).
    func vodImageURL(_ path: String?) -> URL? { absolute(path) }
}

public extension VodMovieDetail {
    var base: VodMovie { value1 }
    var info: Value2Payload { value2 }
    var id: Int64 { value1.id }
    var title: String { value1.title }
}
