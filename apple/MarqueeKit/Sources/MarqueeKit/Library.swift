import Foundation
import MarqueeAPI

public typealias Library = Components.Schemas.Library
public typealias Hub = Components.Schemas.Hub
public typealias Playlist = Components.Schemas.Playlist
public typealias PlaylistEntry = Components.Schemas.PlaylistEntry
public typealias Person = Components.Schemas.PersonDetail
public typealias SearchResults = Components.Schemas.SearchResults
public typealias PlaybackSession = Components.Schemas.PlaybackSession
public typealias Marker = Components.Schemas.Marker
public typealias LibraryFilters = Components.Schemas.LibraryFilters
public typealias ItemSort = Components.Parameters.Sort

/// Data access used by the app screens.
@MainActor
public extension AppSession {
    private var api: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    func libraries() async throws -> [Library] { try await api.listLibraries().ok.body.json }

    func hubs() async throws -> [Hub] { try await api.homeHubs().ok.body.json }

    func items(library: Int64, sort: ItemSort = .title, offset: Int = 0, limit: Int = 100,
               watch: Operations.ListLibraryItems.Input.Query.WatchPayload? = nil, genre: String? = nil,
               type: Schemas.ItemType? = nil, decade: Int? = nil, minMyRating: Int? = nil) async throws -> Schemas.ItemPage {
        // minMyRating: only what this person rated at least that (0–10; 8 = 4 stars, favourites).
        try await api.listLibraryItems(path: .init(libraryId: library),
                                       query: .init(_type: type, sort: sort, offset: offset, limit: limit, watch: watch, genre: genre,
                                                    decade: decade, minMyRating: minMyRating)).ok.body.json
    }

    func filters(library: Int64, type: Schemas.ItemType? = nil) async throws -> LibraryFilters {
        try await api.libraryFilters(path: .init(libraryId: library), query: .init(_type: type)).ok.body.json
    }

    func item(_ id: Int64) async throws -> ItemDetail { try await api.getItem(path: .init(itemId: id)).ok.body.json }

    func children(_ id: Int64) async throws -> [Item] {
        try await api.listItemChildren(path: .init(itemId: id), query: .init(limit: 500)).ok.body.json.items
    }

    /// An artist's most-listened tracks in the library (MUSIC-15).
    func popularTracks(_ artistID: Int64) async throws -> [Item] {
        try await api.listPopularTracks(path: .init(itemId: artistID)).ok.body.json.items
    }

    /// Playable items under a container, in play order.
    func leaves(_ id: Int64, shuffle: Bool = false, unwatched: Bool = false) async throws -> [Item] {
        try await api.itemLeaves(path: .init(itemId: id), query: .init(shuffle: shuffle, unwatched: unwatched)).ok.body.json
    }

    func related(_ id: Int64) async throws -> [Item] { try await api.relatedItems(path: .init(itemId: id)).ok.body.json }

    func person(_ id: Int64) async throws -> Person { try await api.getPerson(path: .init(personId: id)).ok.body.json }

    func search(_ q: String, limit: Int = 20) async throws -> SearchResults {
        try await api.search(query: .init(q: q, limit: limit)).ok.body.json
    }

    func next(after episode: Int64) async -> Item? {
        guard case let .ok(ok) = try? await api.nextItem(path: .init(itemId: episode)) else { return nil }
        return try? ok.body.json
    }

    func setWatched(_ id: Int64, _ watched: Bool) async throws {
        if watched { _ = try await api.markWatched(path: .init(itemId: id)) } else { _ = try await api.markUnwatched(path: .init(itemId: id)) }
    }

    /// Adds to or removes from the watchlist (USER-8).
    func setWatchlist(_ id: Int64, _ on: Bool) async throws {
        if on { _ = try await api.addToWatchlist(path: .init(itemId: id)).noContent } else { _ = try await api.removeFromWatchlist(path: .init(itemId: id)).noContent }
    }

    func watchlist() async throws -> [Item] { try await api.getWatchlist().ok.body.json }

    func playlists(kind: Schemas.PlaylistKind? = nil) async throws -> [Playlist] {
        try await api.listPlaylists(query: .init(kind: kind)).ok.body.json
    }

    func playlist(_ id: Int64) async throws -> Playlist {
        switch try await api.getPlaylist(path: .init(playlistId: id)) {
        case let .ok(ok): return try ok.body.json
        case .notFound: throw MarqueeError("This playlist is gone.")
        default: throw MarqueeError("Couldn't load the playlist.")
        }
    }

    func playlistItems(_ id: Int64) async throws -> [PlaylistEntry] {
        try await api.listPlaylistItems(path: .init(playlistId: id), query: .init(limit: 2000)).ok.body.json.items
    }

    func addToPlaylist(_ playlist: Int64, items: [Int64]) async throws {
        switch try await api.addPlaylistItems(path: .init(playlistId: playlist), body: .json(.init(itemIds: items))) {
        case .ok: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Those items can't go in this playlist.")
        default: throw MarqueeError("Couldn't add to the playlist.")
        }
    }

    func createPlaylist(title: String, kind: Schemas.PlaylistKind, items: [Int64]) async throws -> Playlist {
        try await api.createPlaylist(body: .json(.init(title: title, kind: kind, itemIds: items))).created.body.json
    }

    func approveQuickConnect(code: String) async throws -> String {
        switch try await api.authorizeQuickConnect(body: .json(.init(code: code))) {
        case .ok(let ok): return try ok.body.json.deviceName
        case .notFound(let r): throw MarqueeError((try? r.body.json.message) ?? "That code isn't valid.")
        case .tooManyRequests: throw MarqueeError("Too many attempts. Wait a minute.")
        default: throw MarqueeError("Couldn't link the device.")
        }
    }

    func switchableProfiles() async throws -> [Schemas.Profile] { try await api.listProfiles().ok.body.json }
}

// MARK: - Display helpers

public extension Components.Schemas.ItemSummary {
    var isPlayableVideo: Bool { _type == .movie || _type == .episode || _type == .video }
    var isMusic: Bool { _type == .track || _type == .album || _type == .artist }
    var progress: Double? {
        guard let off = viewOffsetMs, off > 0, let d = durationMs, d > 0 else { return nil }
        return min(1, Double(off) / Double(d))
    }
    var watched: Bool {
        switch _type {
        case .show, .season: leafCount > 0 && (watchedLeafCount ?? 0) >= leafCount
        default: (viewCount ?? 0) > 0
        }
    }
    var unwatchedCount: Int {
        (_type == .show || _type == .season) ? max(0, leafCount - (watchedLeafCount ?? 0)) : 0
    }
    /// The line under a poster.
    var subtitle: String {
        switch _type {
        case .episode:
            let s = parentTitle.map { "\($0) · " } ?? ""
            return "\(s)E\(index ?? 0)"
        case .season: return leafCount == 1 ? "1 episode" : "\(leafCount) episodes"
        case .show: return childCount == 1 ? "1 season" : "\(childCount) seasons"
        case .album: return [artistCredit ?? parentTitle, year.map(String.init)].compactMap { $0 }.joined(separator: " · ")
        case .artist: return childCount == 1 ? "1 album" : "\(childCount) albums"
        case .track: return artistCredit ?? grandparentTitle ?? ""
        default: return year.map(String.init) ?? ""
        }
    }
}

public func formatDuration(ms: Int64?) -> String {
    guard let ms, ms > 0 else { return "" }
    let m = Int(ms / 60000)
    return m >= 60 ? "\(m / 60) hr \(m % 60) min" : "\(m) min"
}

public func formatTime(seconds: Double) -> String {
    guard seconds.isFinite, seconds >= 0 else { return "0:00" }
    let s = Int(seconds)
    return s >= 3600 ? String(format: "%d:%02d:%02d", s / 3600, (s % 3600) / 60, s % 60) : String(format: "%d:%02d", s / 60, s % 60)
}

/// ItemDetail is generated as allOf(summary, details); these name the two halves.
public extension Components.Schemas.ItemDetail {
    var base: Item { value1 }
    var info: Value2Payload { value2 }
    var id: Int64 { value1.id }
    var title: String { value1.title }
    var type: Schemas.ItemType { value1._type }
}

extension Components.Schemas.ItemSummary: Identifiable {}
extension Components.Schemas.Profile: Identifiable {}
extension Components.Schemas.Playlist: Identifiable {}
extension Components.Schemas.RemotePlayer: Identifiable {
    public var id: Int64 { deviceId }
}
