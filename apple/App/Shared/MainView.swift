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
    @State private var adventure = AdventurePicker.shared
    @State private var linkedItem: LinkedItem?
    @State private var canDiscover = false
    @State private var hasLiveTV = false

    var body: some View {
        TabView {
            Tab("Home", systemImage: "house") { stack { HomeView() } }
            if hasLiveTV {
                Tab("Live TV", systemImage: "tv") { stack { LiveTVView() } }
            }
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
            // iPhone with Live TV: Playlists moves under Libraries to keep five tabs.
            if !(compact && hasLiveTV) {
                Tab("Playlists", systemImage: "music.note.list") { stack { PlaylistsView() } }
            }
            if !compact && canDiscover {
                Tab("Discover", systemImage: "safari") { stack { DiscoverView() } }
            }
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
        .task {
            let s = try? await app.requestsStatus()
            canDiscover = s?.enabled == true && s?.canRequest == true
            hasLiveTV = (try? await app.liveStatus())?.enabled == true
        }
        .sheet(item: $playlistPicker.item) { item in AddToPlaylistSheet(item: item) }
        .sheet(item: $adventure.from) { item in AdventureSheet(from: item) }
        // Links from the Top Shelf: marquee://play/<id> plays, marquee://item/<id> shows the page.
        .onOpenURL { url in
            guard url.scheme == "marquee", let id = Int64(url.lastPathComponent) else { return }
            if url.host() == "play" { video.play(id) } else if url.host() == "item" { linkedItem = LinkedItem(id: id) }
        }
        .fullScreenCover(item: $linkedItem) { link in
            NavigationStack {
                ItemDetailView(id: link.id)
                    .marqueeDestinations()
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { linkedItem = nil } } }
            }
            .environment(video)
        }
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
    @Environment(AppSession.self) private var app
    @State private var canDiscover = false
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
                // Value links only: mixing them with view links in one stack re-pushes screens.
                NavigationLink(value: Route.playlists) {
                    Label("Playlists", systemImage: "music.note.list").foregroundStyle(.primary)
                }
                NavigationLink(value: Route.downloads) {
                    Label("Downloads", systemImage: "arrow.down.circle").foregroundStyle(.primary)
                }
                // iPhone: Discover lives here (a sixth tab would spill into "More").
                if canDiscover {
                    NavigationLink(value: Route.discover) {
                        Label("Discover", systemImage: "safari").foregroundStyle(.primary)
                    }
                }
            }
            #endif
        }
        .navigationTitle("Libraries")
        .task {
            let s = try? await app.requestsStatus()
            canDiscover = s?.enabled == true && s?.canRequest == true
        }
    }
}

/// An item opened from a link (the Apple TV Top Shelf).
struct LinkedItem: Identifiable {
    let id: Int64
}
