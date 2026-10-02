import MarqueeKit
import SwiftUI

/// Destinations pushed onto navigation stacks.
enum Route: Hashable {
    case item(Int64)
    case library(Int64)
    case person(Int64)
    case playlist(Int64)
    case playlists
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
            }
        }
    }
}

/// What's playing full screen (video).
struct VideoRequest: Identifiable, Hashable {
    let itemID: Int64
    var startMs: Int64?
    var playlistID: Int64?
    var id: String { "\(itemID)-\(startMs ?? -1)-\(playlistID ?? 0)" }
}

/// Lets any view start a video; MainView presents the player.
@MainActor @Observable
final class VideoPresenter {
    var request: VideoRequest?
    func play(_ itemID: Int64, startMs: Int64? = nil, playlistID: Int64? = nil) {
        request = VideoRequest(itemID: itemID, startMs: startMs, playlistID: playlistID)
    }
}
