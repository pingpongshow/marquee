import MarqueeKit
import SwiftUI

/// Tabs: Home, each library, Playlists, Search and Settings. On iPad the tab bar becomes a
/// Plex-style sidebar.
struct MainView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @State private var libraries: [Library] = []
    @State private var video = VideoPresenter()
    @State private var showNowPlaying = false
    @State private var playlistPicker = PlaylistPicker.shared

    var body: some View {
        TabView {
            Tab("Home", systemImage: "house") { stack { HomeView() } }
            if compact {
                // iPhone: one Libraries tab so the tab bar doesn't overflow into "More".
                Tab("Libraries", systemImage: "square.stack") { stack { LibrariesList(libraries: libraries, icon: icon) } }
            } else {
                TabSection("Libraries") {
                    ForEach(libraries, id: \.id) { lib in
                        Tab(lib.name, systemImage: icon(lib._type)) { stack { LibraryView(libraryID: lib.id) } }
                    }
                }
            }
            Tab("Playlists", systemImage: "music.note.list") { stack { PlaylistsView() } }
            #if os(iOS)
            if !compact {
                // iPhone lists Downloads under Libraries (a sixth tab would spill into "More").
                Tab("Downloads", systemImage: "arrow.down.circle") { stack { DownloadsView() } }
            }
            #endif
            #if os(tvOS)
            if music.current != nil {
                Tab("Now Playing", systemImage: "waveform") { NowPlayingView() }
            }
            #endif
            Tab("Settings", systemImage: "gearshape") { stack { SettingsView() } }
            Tab(role: .search) { stack { SearchView() } }
        }
        #if os(iOS)
        .tabViewStyle(.sidebarAdaptable)
        #endif
        .environment(video)
        .task { await loadLibraries() }
        .sheet(item: $playlistPicker.item) { item in AddToPlaylistSheet(item: item) }
        .fullScreenCover(item: $video.request) { req in
            PlayerView(request: req)
                .environment(video)
        }
        #if os(iOS)
        .sheet(isPresented: $showNowPlaying) { NowPlayingView() }
        .environment(\.openNowPlaying, OpenNowPlayingAction { showNowPlaying = true })
        #endif
    }

    private var compact: Bool {
        #if os(iOS)
        UIDevice.current.userInterfaceIdiom == .phone
        #else
        false
        #endif
    }

    private func stack<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        NavigationStack {
            content()
                .marqueeDestinations()
        }
        #if os(iOS)
        .safeAreaInset(edge: .bottom) { MiniPlayerBar() }
        #endif
    }

    private func loadLibraries() async {
        if let libs = try? await app.libraries() { libraries = libs }
    }

    private func icon(_ t: Schemas.LibraryType) -> String {
        switch t {
        case .movies: "film"
        case .shows: "tv"
        case .anime: "sparkles.tv"
        case .music: "music.note"
        case .videos: "video"
        case .photos: "photo"
        }
    }
}

struct OpenNowPlayingAction {
    let action: () -> Void
    func callAsFunction() { action() }
}

private struct OpenNowPlayingKey: EnvironmentKey {
    static let defaultValue = OpenNowPlayingAction {}
}

extension EnvironmentValues {
    var openNowPlaying: OpenNowPlayingAction {
        get { self[OpenNowPlayingKey.self] }
        set { self[OpenNowPlayingKey.self] = newValue }
    }
}

struct LibrariesList: View {
    let libraries: [Library]
    let icon: (Schemas.LibraryType) -> String
    var body: some View {
        List {
            ForEach(libraries, id: \.id) { lib in
                NavigationLink(value: Route.library(lib.id)) {
                    Label {
                        VStack(alignment: .leading) {
                            Text(lib.name)
                            Text("\(lib.itemCount) items").font(.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: icon(lib._type)).foregroundStyle(Color.marqueeGold)
                    }
                }
            }
            #if os(iOS)
            Section {
                NavigationLink { DownloadsView() } label: {
                    Label("Downloads", systemImage: "arrow.down.circle").foregroundStyle(.primary)
                }
            }
            #endif
        }
        .navigationTitle("Libraries")
    }
}
