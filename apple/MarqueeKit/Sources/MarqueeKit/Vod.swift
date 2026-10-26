import Foundation
import MarqueeAPI

public typealias VodMovie = Components.Schemas.VodMovie
public typealias VodMovieDetail = Components.Schemas.VodMovieDetail
public typealias VodStatus = Components.Schemas.VodStatus
public typealias VodCategory = Components.Schemas.VodCategory
public typealias VodSort = Operations.ListVodMovies.Input.Query.SortPayload
public typealias VodSeries = Components.Schemas.VodSeries
public typealias VodSeriesDetail = Components.Schemas.VodSeriesDetail
public typealias VodSeason = Components.Schemas.VodSeason
public typealias VodEpisode = Components.Schemas.VodEpisode
public typealias VodSuggestion = Components.Schemas.VodSuggestion
public typealias VodRecommendationRow = Components.Schemas.VodRecommendationRow
public typealias VodRef = Components.Schemas.VodRef

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
            return VodStatus(enabled: false, movies: 0, series: 0, matched: 0, unmatched: 0, matching: false, sources: [])
        }
        return try await vodAPI.vodStatus().ok.body.json
    }

    func vodCategories() async throws -> [VodCategory] {
        try await vodAPI.listVodCategories().ok.body.json
    }

    /// The films' genres with counts (for the genre filter).
    func vodGenres() async throws -> [VodCategory] {
        try await vodAPI.listVodGenres().ok.body.json
    }

    /// A page of films, with how many there are in all.
    func vodMovies(category: String? = nil, query: String? = nil, sort: VodSort = .title, filters: VodFilters = .init(),
                   offset: Int = 0, limit: Int = 120) async throws -> (movies: [VodMovie], total: Int) {
        let page = try await vodAPI.listVodMovies(query: .init(
            category: category, q: query, sort: sort, limit: limit, genre: filters.genre, yearFrom: filters.yearFrom,
            yearTo: filters.yearTo, minRating: filters.minRating.map(Double.init),
            watched: filters.watched.flatMap { .init(rawValue: $0.rawValue) }, sourceId: filters.sourceId,
            inLibrary: filters.inLibrary, offset: offset)).ok.body.json
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

    /// Puts a film on demand on the watchlist or takes it off (USER-8).
    func setVodWatchlisted(_ id: Int64, _ on: Bool) async throws {
        if on {
            _ = try await vodAPI.addVodMovieToWatchlist(path: .init(movieId: id)).noContent
        } else {
            _ = try await vodAPI.removeVodMovieFromWatchlist(path: .init(movieId: id)).noContent
        }
    }

    /// The app's quality setting for VOD playback, as for the library (nil = original / automatic).
    private var vodMaxBitrate: Int? {
        let q = isRemote ? QualityPreference.remote : QualityPreference.local
        return q == 0 ? nil : q
    }

    /// Starts a film: the session id, the HLS URL and the caller's resume point (ms). Sends the
    /// device's profile, so the server copies what this device plays (HEVC, AC3…).
    func playVod(_ id: Int64) async throws -> (id: String, url: URL, startMs: Int64) {
        let body = Operations.PlayVodMovie.Input.Body.JsonPayload(profile: .init(value1: AppleDeviceProfile.current()), maxBitrateKbps: vodMaxBitrate)
        switch try await vodAPI.playVodMovie(path: .init(movieId: id), body: .json(body)) {
        case let .ok(ok):
            let s = try ok.body.json
            guard let url = absolute(s.url) else { throw MarqueeError("Not connected") }
            return (s.id, url, s.startMs ?? 0)
        case let .forbidden(e):
            if let reached = TimeLimitReached.from(try? e.body.json) { throw reached }
            throw MarqueeError((try? e.body.json.message) ?? "This movie isn't available on this profile")
        case .notFound: throw MarqueeError("That movie isn't available any more")
        case let .badGateway(e): throw MarqueeError(Self.vodFailed((try? e.body.json.message), what: "movie"))
        case .serviceUnavailable: throw MarqueeError(Self.vodBusy)
        default: throw MarqueeError("Couldn't start the movie")
        }
    }

    /// Starts an episode: the session id, the HLS URL and where to start (ms). `startMs` nil =
    /// the caller's resume point, 0 = from the beginning.
    func playVodEpisode(_ id: Int64, startMs: Int64? = nil) async throws -> (id: String, url: URL, startMs: Int64) {
        let body = Operations.PlayVodEpisode.Input.Body.JsonPayload(profile: .init(value1: AppleDeviceProfile.current()),
                                                                   maxBitrateKbps: vodMaxBitrate, startMs: startMs)
        switch try await vodAPI.playVodEpisode(path: .init(episodeId: id), body: .json(body)) {
        case let .ok(ok):
            let s = try ok.body.json
            guard let url = absolute(s.url) else { throw MarqueeError("Not connected") }
            return (s.id, url, s.startMs ?? 0)
        case let .forbidden(e):
            if let reached = TimeLimitReached.from(try? e.body.json) { throw reached }
            throw MarqueeError((try? e.body.json.message) ?? "This show isn't available on this profile")
        case .notFound: throw MarqueeError("That episode isn't available any more")
        case let .badGateway(e): throw MarqueeError(Self.vodFailed((try? e.body.json.message), what: "episode"))
        case .serviceUnavailable: throw MarqueeError(Self.vodBusy)
        default: throw MarqueeError("Couldn't start the episode")
        }
    }

    private static let vodBusy = "The server is busy with other streams right now. Try again in a moment."

    private static func vodFailed(_ message: String?, what: String) -> String {
        let base = "The source couldn't play this \(what) right now. Try again later."
        guard let message, !message.isEmpty else { return base }
        return "\(base) (\(message))"
    }

    /// Where the viewer is, so the film resumes there (and counts as watched near the end).
    /// Returns the time limit when it has run out (the session has ended on the server).
    @discardableResult
    func reportVod(_ session: String, positionMs: Int64, durationMs: Int64?) async -> TimeLimitReached? {
        guard case let .undocumented(code, payload)? = try? await vodAPI.reportVodPlayback(
            path: .init(sessionId: session), body: .json(.init(positionMs: positionMs, durationMs: durationMs))) else { return nil }
        return await TimeLimitReached.from(statusCode: code, payload: payload)
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

    // MARK: Shows (series from Xtream Codes sources)

    func vodSeriesCategories() async throws -> [VodCategory] {
        try await vodAPI.listVodSeriesCategories().ok.body.json
    }

    /// The shows' genres with counts (for the genre filter).
    func vodSeriesGenres() async throws -> [VodCategory] {
        try await vodAPI.listVodSeriesGenres().ok.body.json
    }

    /// A page of shows, with how many there are in all.
    func vodSeriesList(category: String? = nil, query: String? = nil, sort: VodSort = .title, filters: VodFilters = .init(),
                       offset: Int = 0, limit: Int = 120) async throws -> (series: [VodSeries], total: Int) {
        let page = try await vodAPI.listVodSeries(query: .init(
            category: category, q: query, sort: .init(rawValue: sort.rawValue), limit: limit, genre: filters.genre,
            yearFrom: filters.yearFrom, yearTo: filters.yearTo, minRating: filters.minRating.map(Double.init),
            watched: filters.watched.flatMap { .init(rawValue: $0.rawValue) }, sourceId: filters.sourceId,
            inLibrary: filters.inLibrary, offset: offset)).ok.body.json
        return (page.series, page.total)
    }

    /// Shows being watched, each with the episode to watch next.
    func vodSeriesContinueWatching() async throws -> [VodSeries] {
        try await vodAPI.vodSeriesContinueWatching().ok.body.json
    }

    /// A show with its seasons and episodes (the first open fetches the episodes, which can take a few seconds).
    func vodSeries(_ id: Int64, refresh: Bool = false) async throws -> VodSeriesDetail {
        switch try await vodAPI.getVodSeries(path: .init(seriesId: id), query: .init(refresh: refresh ? true : nil)) {
        case let .ok(ok): return try ok.body.json
        case .notFound: throw MarqueeError("That show isn't available any more")
        case .forbidden: throw MarqueeError("This show isn't available on this profile")
        default: throw MarqueeError("Couldn't load the show")
        }
    }

    /// Marks every episode of a show (or of one season) watched or unwatched.
    func setVodSeriesWatched(_ id: Int64, season: Int? = nil, _ watched: Bool) async throws {
        if watched {
            _ = try await vodAPI.markVodSeriesWatched(path: .init(seriesId: id), query: .init(season: season)).noContent
        } else {
            _ = try await vodAPI.markVodSeriesUnwatched(path: .init(seriesId: id), query: .init(season: season)).noContent
        }
    }

    /// Puts a show on demand on the watchlist or takes it off (USER-8).
    func setVodSeriesWatchlisted(_ id: Int64, _ on: Bool) async throws {
        if on {
            _ = try await vodAPI.addVodSeriesToWatchlist(path: .init(seriesId: id)).noContent
        } else {
            _ = try await vodAPI.removeVodSeriesFromWatchlist(path: .init(seriesId: id)).noContent
        }
    }

    func setVodEpisodeWatched(_ id: Int64, _ watched: Bool) async throws {
        if watched {
            _ = try await vodAPI.markVodEpisodeWatched(path: .init(episodeId: id)).noContent
        } else {
            _ = try await vodAPI.markVodEpisodeUnwatched(path: .init(episodeId: id)).noContent
        }
    }

    // MARK: Suggestions (LIVE-6, USER-16)

    /// Films (or series) on demand picked for the viewer from their taste: the VOD pages'
    /// Recommended for you.
    func vodSuggestions(series: Bool, limit: Int = 20) async throws -> [VodSuggestion] {
        try await vodAPI.vodSuggestions(query: .init(kind: series ? .series : .movie, limit: limit)).ok.body.json
    }

    /// The recommendations page (Recommended for you's See All): rows of films (or series) on
    /// demand, each with its own angle, all from the viewer's own taste.
    func vodRecommendations(series: Bool, perRow: Int = 40) async throws -> [VodRecommendationRow] {
        try await vodAPI.vodRecommendations(query: .init(kind: series ? .series : .movie, perRow: perRow)).ok.body.json
    }

    /// Also on VOD: films or series on demand like a library movie or show (empty when the
    /// viewer can't watch on demand).
    func itemVodSimilar(_ itemId: Int64, limit: Int = 20) async throws -> [VodSuggestion] {
        try await vodAPI.itemVodSimilar(path: .init(itemId: itemId), query: .init(limit: limit)).ok.body.json
    }

    /// Not interested: the title isn't suggested to this viewer again.
    func hideVodSuggestion(_ s: VodSuggestion) async throws {
        guard let id = s.vodId else { return }
        switch try await vodAPI.hideVodSuggestion(path: .init(kind: s.kind == .series ? .series : .movie, vodId: id)) {
        case .noContent, .notFound: return
        default: throw MarqueeError("Couldn't hide that suggestion")
        }
    }

    /// The episode after this one in viewing order, if any (for Up Next).
    func vodNextEpisode(_ id: Int64) async throws -> VodEpisode? {
        try await vodAPI.vodNextEpisode(path: .init(episodeId: id)).ok.body.json.episode
    }
}

/// The VOD pages' filters (genre, decade, minimum rating, watched state, source, in the library), kept per page.
public struct VodFilters: Codable, Hashable, Sendable {
    public enum Watched: String, Codable, CaseIterable, Sendable {
        case unwatched, inProgress, watched
    }

    public var genre: String?
    /// The decade's first year (1980 = 1980–1989).
    public var decade: Int?
    /// Rated at least this, out of 10.
    public var minRating: Int?
    public var watched: Watched?
    /// VodStatus.sources[].id
    public var sourceId: String?
    /// true = only titles a library the viewer can see already has; false = only those it doesn't.
    public var inLibrary: Bool?

    public init(genre: String? = nil, decade: Int? = nil, minRating: Int? = nil, watched: Watched? = nil, sourceId: String? = nil,
                inLibrary: Bool? = nil) {
        self.genre = genre
        self.decade = decade
        self.minRating = minRating
        self.watched = watched
        self.sourceId = sourceId
        self.inLibrary = inLibrary
    }

    public var yearFrom: Int? { decade }
    public var yearTo: Int? { decade.map { $0 + 9 } }
    public var isActive: Bool { self != VodFilters() }

    /// The decades offered, newest first (as on the web).
    public static let decades = [2020, 2010, 2000, 1990, 1980, 1970, 1960, 1950, 1940, 1930]
    public static let ratings = [9, 8, 7, 6, 5]

    /// Stored as JSON in the app's settings; anything unreadable is no filters.
    public init(stored: String) {
        self = (try? JSONDecoder().decode(VodFilters.self, from: Data(stored.utf8))) ?? VodFilters()
    }

    public var stored: String {
        (try? JSONEncoder().encode(self)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }

    public static func inLibraryLabel(_ v: Bool) -> String { v ? "In my library" : "Not in my library" }

    public static func label(_ w: Watched, shows: Bool) -> String {
        switch w {
        case .unwatched: shows ? "Unwatched episodes" : "Unwatched"
        case .inProgress: "In progress"
        case .watched: "Watched"
        }
    }
}

public extension VodSeriesDetail {
    var base: VodSeries { value1 }
    var info: Value2Payload { value2 }
    var id: Int64 { value1.id }
    var title: String { value1.title }
}

public extension VodSeries {
    /// The episode the viewer would watch next.
    var next: VodEpisode? { nextEpisode?.value1 }
}

public extension VodEpisode {
    /// "S1 · E2", as on the web.
    var label: String { "S\(season) · E\(episode)" }
}

public extension VodMovieDetail {
    var base: VodMovie { value1 }
    var info: Value2Payload { value2 }
    var id: Int64 { value1.id }
    var title: String { value1.title }
}

public extension VodSuggestion {
    /// The film's or series' id.
    var vodId: Int64? { movie?.id ?? series?.id }
    var title: String { movie?.title ?? series?.title ?? "" }
    var posterUrl: String? { movie?.posterUrl ?? series?.posterUrl }
}
