import MarqueeKit
import SwiftUI

/// Tabs: Home, each library, Playlists, Search and Settings. On iPad the tab bar becomes a
/// The main sidebar.
struct MainView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(\.scenePhase) private var scenePhase
    @State private var libraries: [Library] = []
    @State private var video = VideoPresenter()
    @State private var showNowPlaying = false
    @State private var playlistPicker = PlaylistPicker.shared
    @State private var journey = JourneyPicker.shared
    @State private var actionError = ActionError.shared
    @State private var linkedItem: LinkedItem?
    @State private var canDiscover = false
    @State private var hasLiveTV = false
    /// Why the libraries couldn't be loaded (shown with a retry instead of an empty list).
    @State private var librariesError: String?

    var body: some View {
        TabView {
            Tab("Home", systemImage: "house") { stack { HomeView() } }
            if compact {
                // iPhone: one Libraries tab so the tab bar doesn't overflow into "More". Live TV
                // is listed there with the libraries.
                Tab("Libraries", systemImage: "square.stack") {
                    stack { LibrariesList(libraries: libraries, liveTV: hasLiveTV, error: librariesError, icon: icon, reload: { await loadAll() }) }
                }
            } else {
                // iPad sidebar and Apple TV top bar: Live TV is one of the libraries, after
                // the video ones.
                TabSection("Libraries") {
                    ForEach(LibraryEntry.list(libraries, liveTV: hasLiveTV)) { entry in
                        switch entry {
                        case .library(let lib):
                            Tab(lib.name, systemImage: icon(lib._type)) { stack { LibraryView(libraryID: lib.id) } }
                        case .liveTV:
                            Tab("Live TV", systemImage: "antenna.radiowaves.left.and.right") { stack { LiveTVView() } }
                        }
                    }
                    if libraries.isEmpty, librariesError != nil {
                        // The server wasn't reachable: a way back instead of an empty section.
                        Tab("Libraries", systemImage: "exclamationmark.triangle") {
                            stack { LibrariesList(libraries: [], liveTV: false, error: librariesError, icon: icon, reload: { await loadAll() }) }
                        }
                    }
                }
            }
            Tab("Playlists", systemImage: "music.note.list") { stack { PlaylistsView() } }
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
        .remoteToast()
        // Remote control (USER-14): a player while open, and while music plays in the background.
        .onChange(of: scenePhase, initial: true) { old, new in
            updateRemote()
            // Back in the app: anything that failed at launch (server unreachable) loads now.
            if old != new, new == .active, libraries.isEmpty || librariesError != nil { Task { await loadAll() } }
            // And changes made offline go to the server (USER-18).
            if old != new, new == .active { Task { await OfflineSync.shared.flush() } }
        }
        .onChange(of: music.playing) { updateRemote() }
        .onDisappear { RemoteReceiver.shared.stop() }
        // Again whenever the connection changes (another address, reconnected, signed in).
        .task(id: "\(app.baseURL?.absoluteString ?? "")|\(app.state)|\(app.me?.id ?? 0)") { await loadAll() }
        .sheet(item: $playlistPicker.item) { item in AddToPlaylistSheet(item: item) }
        .sheet(item: $journey.from) { item in JourneySheet(from: item) }
        .alert("Something went wrong", isPresented: Binding(get: { actionError.message != nil }, set: { if !$0 { actionError.message = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(actionError.message ?? "")
        }
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
            // tvOS covers are see-through by default: Home showed behind the page.
            .background(Color.black.ignoresSafeArea())
            // One view can't present two covers at once: while a linked item is up, the
            // player is presented from inside its cover.
            .fullScreenCover(item: $video.request) { req in
                PlayerView(request: req)
                    .environment(video)
            }
            .environment(video)
        }
        .fullScreenCover(item: Binding(get: { linkedItem == nil ? video.request : nil },
                                       set: { if linkedItem == nil { video.request = $0 } })) { req in
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
                .miniPlayerSpace()
                .marqueeDestinations()
        }
        #if os(iOS)
        // The bar floats above the tab bar, and every screen in the stack (the root here, pushed
        // ones in marqueeDestinations) keeps room for it at the bottom: an inset on the stack
        // itself didn't reach the scroll views inside, so lists ended under the bar.
        .overlay(alignment: .bottom) { MiniPlayerBar() }
        #endif
    }

    private func updateRemote() {
        let remote = RemoteReceiver.shared
        if scenePhase == .background && !music.playing {
            remote.stop()
        } else {
            remote.start(app: app, music: music, presenter: video)
        }
    }

    private func loadAll() async {
        guard app.client != nil else { return }
        async let requests = try? app.requestsStatus()
        async let live = try? app.liveStatus()
        do {
            libraries = try await app.libraries()
            librariesError = nil
        } catch is CancellationError {
        } catch {
            librariesError = error.localizedDescription
        }
        // A failed check keeps what was known (a tab doesn't vanish on a blip).
        if let s = await requests { canDiscover = s.enabled && s.canRequest }
        if let l = await live { hasLiveTV = l.enabled }
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

/// A row of the Libraries list (or a sidebar / top bar tab): a library, or Live TV, which is
/// listed after the video libraries when it's set up.
enum LibraryEntry: Identifiable {
    case library(Library)
    case liveTV

    var id: String {
        switch self {
        case .library(let l): "library-\(l.id)"
        case .liveTV: "liveTV"
        }
    }

    static func list(_ libraries: [Library], liveTV: Bool) -> [LibraryEntry] {
        var out = libraries.map(LibraryEntry.library)
        guard liveTV else { return out }
        let video: Set<Schemas.LibraryType> = [.movies, .shows, .anime, .videos]
        let at = (libraries.lastIndex { video.contains($0._type) }).map { $0 + 1 } ?? libraries.count
        out.insert(.liveTV, at: at)
        return out
    }
}

struct LibrariesList: View {
    @Environment(AppSession.self) private var app
    @State private var canDiscover = false
    let libraries: [Library]
    /// Live TV is set up: listed with the libraries.
    var liveTV = false
    var error: String?
    let icon: (Schemas.LibraryType) -> String
    var reload: () async -> Void = {}
    var body: some View {
        List {
            if let error, libraries.isEmpty {
                Section {
                    Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red)
                    Button("Try Again") { Task { await reload() } }
                        .accessibilityIdentifier("librariesRetry")
                }
            }
            ForEach(LibraryEntry.list(libraries, liveTV: liveTV)) { entry in
                switch entry {
                case .library(let lib):
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
                case .liveTV:
                    NavigationLink(value: Route.liveTV) {
                        Label {
                            VStack(alignment: .leading) {
                                Text("Live TV")
                                Text("Guide, channels and recordings").font(.caption).foregroundStyle(.secondary)
                            }
                        } icon: {
                            Image(systemName: "antenna.radiowaves.left.and.right").foregroundStyle(Color.marqueeGold)
                        }
                    }
                    .accessibilityIdentifier("libraries.liveTV")
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
        .refreshable {
            await reload()
            await loadDiscover()
        }
        .task { await loadDiscover() }
    }

    private func loadDiscover() async {
        if let s = try? await app.requestsStatus() { canDiscover = s.enabled && s.canRequest }
    }
}

/// An item opened from a link (the Apple TV Top Shelf).
struct LinkedItem: Identifiable {
    let id: Int64
}
