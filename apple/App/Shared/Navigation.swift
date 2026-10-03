import MarqueeKit
import SwiftUI

/// Destinations pushed onto navigation stacks.
enum Route: Hashable {
    case item(Int64)
    case library(Int64)
    case person(Int64)
    case playlist(Int64)
    case playlists
    case downloads
    case discover
    case moodStyle(library: Int64, mood: Bool, name: String)
    /// Muse for movies and shows (USER-15), optionally in one library.
    case museVideo(library: Int64?)
    /// A music library's full lists, opened from its landing page's browse row (MUSIC-15).
    case musicBrowse(library: Int64, MusicBrowse)
    /// Muse and the stations of a music library.
    case musicMuse(library: Int64)
}

/// The full lists behind a music library's landing page.
enum MusicBrowse: Hashable {
    case artists, albums, recentAlbums, songs, genres
}

extension View {
    /// Registers every Route destination on a NavigationStack.
    func marqueeDestinations() -> some View {
        navigationDestination(for: Route.self) { route in
            switch route {
            case .item(let id): ItemDetailView(id: id)
            case .library(let id): LibraryView(libraryID: id)
            case .person(let id): PersonView(id: id)
            case .playlist(let id): PlaylistView(id: id)
            case .playlists: PlaylistsView()
            #if os(iOS)
            case .downloads: DownloadsView()
            #else
            case .downloads: EmptyView()
            #endif
            case .discover: DiscoverView()
            case let .moodStyle(library, mood, name): MoodStyleView(libraryID: library, kind: mood ? .mood : .style, name: name)
            case let .museVideo(library): VideoMuseView(libraryID: library)
            case let .musicBrowse(library, kind):
                switch kind {
                case .artists: LibraryView(libraryID: library, type: .artist)
                case .albums: LibraryView(libraryID: library, type: .album)
                case .recentAlbums: LibraryView(libraryID: library, type: .album, initialSort: ._hyphen_added)
                case .songs: MusicSongsView(libraryID: library)
                case .genres: MusicGenresView(libraryID: library)
                }
            case let .musicMuse(library): MuseStationsView(libraryID: library)
            }
        }
    }
}

/// What's playing full screen (video).
struct VideoRequest: Identifiable, Hashable {
    let itemID: Int64
    var startMs: Int64?
    var playlistID: Int64?
    /// A watch-together group to join.
    var groupID: String?
    var id: String { "\(itemID)-\(startMs ?? -1)-\(playlistID ?? 0)-\(groupID ?? "")" }
}

/// Lets any view start a video; MainView presents the player.
@MainActor @Observable
final class VideoPresenter {
    var request: VideoRequest?
    func play(_ itemID: Int64, startMs: Int64? = nil, playlistID: Int64? = nil, group: String? = nil) {
        request = VideoRequest(itemID: itemID, startMs: startMs, playlistID: playlistID, groupID: group)
    }
}
