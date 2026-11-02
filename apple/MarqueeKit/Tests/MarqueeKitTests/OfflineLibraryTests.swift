import Foundation
import Testing
@testable import MarqueeKit

/// Grouping what's downloaded for the offline library.
struct OfflineLibraryTests {
    private func item(_ id: Int64, _ type: Schemas.ItemType, _ title: String) -> Item {
        Item(id: id, libraryId: 1, _type: type, title: title, childCount: 0, leafCount: 0, available: true, matchState: .local, addedAt: Date())
    }

    private func episode(_ id: Int64, show: Int64, showTitle: String, season: String, index: Int) -> Item {
        var e = item(id, .episode, "E\(index)")
        e.grandparentId = show
        e.grandparentTitle = showTitle
        e.parentId = show * 100
        e.parentTitle = season
        e.index = index
        return e
    }

    private func song(_ id: Int64, album: Int64?, albumTitle: String, disc: Int = 1, index: Int, title: String? = nil) -> Item {
        var t = item(id, .track, title ?? "S\(id)")
        t.parentId = album
        t.parentTitle = albumTitle
        t.artistCredit = "Artist"
        t.disc = disc
        t.index = index
        return t
    }

    @Test func nothingDownloadedIsEmpty() {
        #expect(OfflineLibrary(items: []).isEmpty)
    }

    @Test func moviesAndVideosGoTogether() {
        let lib = OfflineLibrary(items: [item(1, .movie, "Zulu"), item(2, .video, "Alpha")])
        #expect(lib.movies.map(\.title) == ["Alpha", "Zulu"])
        #expect(!lib.isEmpty)
    }

    @Test func showsHoldTheirEpisodesInOrder() {
        let lib = OfflineLibrary(items: [
            episode(1, show: 2, showTitle: "B", season: "Season 10", index: 1),
            episode(2, show: 2, showTitle: "B", season: "Season 2", index: 2),
            episode(3, show: 2, showTitle: "B", season: "Season 2", index: 1),
            episode(4, show: 1, showTitle: "A", season: "Season 1", index: 1),
        ])
        #expect(lib.shows.map(\.title) == ["A", "B"])
        #expect(lib.shows[1].items.map(\.id) == [3, 2, 1])
        #expect(lib.movies.isEmpty)
    }

    @Test func albumsHoldTheirTracksInOrderAndEverySongIsListed() {
        let lib = OfflineLibrary(items: [
            song(1, album: 7, albumTitle: "Second", disc: 2, index: 1),
            song(2, album: 7, albumTitle: "Second", disc: 1, index: 2),
            song(3, album: 8, albumTitle: "First", index: 1),
            song(4, album: nil, albumTitle: "", index: 1, title: "Loose"),
        ])
        #expect(lib.albums.map(\.title) == ["First", "Second"])
        #expect(lib.albums[1].items.map(\.id) == [2, 1])
        #expect(lib.albums[1].subtitle == "Artist")
        #expect(lib.songs.count == 4)
    }

    @Test func aPlaylistShowsOnlyWhatsOnTheDeviceInItsOrder() {
        let here = [song(1, album: 7, albumTitle: "A", index: 1), song(2, album: 7, albumTitle: "A", index: 2)]
        let lib = OfflineLibrary(items: here, playlists: [
            .init(id: 1, title: "Mix", isAudio: true, items: [2, 9, 1]),
            .init(id: 2, title: "Not here", isAudio: true, items: [9]),
        ])
        #expect(lib.playlists.map(\.title) == ["Mix"])
        #expect(lib.playlists[0].items.map(\.id) == [2, 1])
    }
}
