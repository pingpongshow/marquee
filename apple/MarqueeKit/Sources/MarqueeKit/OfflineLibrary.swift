import Foundation

/// What's on the device, as the offline library shows it (iPhone/iPad): movies, shows with their
/// episodes, albums and songs, and the playlists kept on the device with what they have here.
/// Built from finished downloads only, so nothing that can't play shows.
public struct OfflineLibrary: Sendable {
    public struct Group: Identifiable, Sendable {
        public let id: Int64
        public let title: String
        /// The artist, for an album.
        public let subtitle: String?
        /// In play order.
        public let items: [Item]
    }

    public struct List: Identifiable, Sendable {
        public let id: Int64
        public let title: String
        public let isAudio: Bool
        /// The ones on the device, in the playlist's order.
        public let items: [Item]
    }

    public let movies: [Item]
    public let shows: [Group]
    public let albums: [Group]
    public let songs: [Item]
    public let playlists: [List]

    public var isEmpty: Bool { movies.isEmpty && shows.isEmpty && songs.isEmpty }

    /// `items`: the finished downloads.
    public init(items: [Item], playlists kept: [Downloads.SyncedPlaylist] = []) {
        func byTitle(_ a: Item, _ b: Item) -> Bool { a.title.localizedStandardCompare(b.title) == .orderedAscending }
        movies = items.filter { $0._type == .movie || $0._type == .video }.sorted(by: byTitle)

        let episodes = Dictionary(grouping: items.filter { $0._type == .episode }) { $0.grandparentId ?? $0.parentId ?? $0.id }
        shows = episodes.map { id, list in
            let ordered = list.sorted { a, b in
                let order = (a.parentTitle ?? "").localizedStandardCompare(b.parentTitle ?? "")
                return order == .orderedSame ? (a.index ?? 0) < (b.index ?? 0) : order == .orderedAscending
            }
            return Group(id: id, title: ordered[0].grandparentTitle ?? ordered[0].title, subtitle: nil, items: ordered)
        }
        .sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }

        let tracks = items.filter { $0._type == .track }
        func trackOrder(_ a: Item, _ b: Item) -> Bool { (a.disc ?? 0, a.index ?? 0, a.title) < (b.disc ?? 0, b.index ?? 0, b.title) }
        albums = Dictionary(grouping: tracks.filter { $0.parentId != nil }) { $0.parentId ?? 0 }
            .map { id, list in
                let ordered = list.sorted(by: trackOrder)
                return Group(id: id, title: ordered[0].parentTitle ?? ordered[0].title,
                             subtitle: ordered[0].artistCredit ?? ordered[0].grandparentTitle, items: ordered)
            }
            .sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
        songs = tracks.sorted(by: byTitle)

        let byID = Dictionary(items.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        playlists = kept.compactMap { p in
            let here = p.items.compactMap { byID[$0] }
            return here.isEmpty ? nil : List(id: p.id, title: p.title, isAudio: p.isAudio, items: here)
        }
        .sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
    }
}
