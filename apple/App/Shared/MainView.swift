import MarqueeKit
import SwiftUI

/// Tabs: Home, each library, Playlists, Search and Settings. On iPad and Apple TV they're
/// the main sidebar; on iPhone a tab bar.
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
    /// Films and shows on demand are on and usable (LIVE-6): VOD Movies and VOD Shows entries
    /// right after Live TV, each while it has something.
    @State private var hasVodMovies = false
    @State private var hasVodShows = false
    /// Why the libraries couldn't be loaded (shown with a retry instead of an empty list).
    @State private var librariesError: String?
    /// Bumped when another person signs in (a profile switch stays signed in): the tabs and
    /// their pages start again, so nothing of the last person's stays on screen.
    var generation = 0

    var body: some View {
        TabView {
            Tab("Home", systemImage: "house") { stack { HomeView() } }
            if compact {
                // iPhone: one Libraries tab so the tab bar doesn't overflow into "More". Live TV
                // is listed there with the libraries.
                Tab("Libraries", systemImage: "square.stack") {
                    stack { LibrariesList(libraries: libraries, liveTV: hasLiveTV, vodMovies: hasVodMovies, vodShows: hasVodShows, error: librariesError, icon: icon, reload: { await loadAll() }) }
                }
            } else {
                // iPad and Apple TV sidebar: Live TV is one of the libraries, after
                // the video ones.
                TabSection("Libraries") {
                    ForEach(LibraryEntry.list(libraries, liveTV: hasLiveTV, vodMovies: hasVodMovies, vodShows: hasVodShows)) { entry in
                        switch entry {
                        case .library(let lib):
                            Tab(lib.name, systemImage: icon(lib._type)) { stack { LibraryView(libraryID: lib.id) } }
                        case .liveTV:
                            Tab("Live TV", systemImage: "antenna.radiowaves.left.and.right") { stack { LiveTVView() } }
                        case .vodMovies:
                            Tab("VOD Movies", systemImage: "film.stack") { stack { VodPage() } }
                        case .vodShows:
                            Tab("VOD Shows", systemImage: "play.tv") { stack { VodPage(shows: true) } }
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
            #if os(tvOS)
            // The TV's sidebar lists plain tabs before sections: the rest go in a section with
            // no header so they come after Libraries (Home, Libraries, then these).
            TabSection { otherTabs } header: { EmptyView() }
            #else
            otherTabs
            #endif
        }
        .id(generation)
        .onChange(of: generation) {
            video.request = nil
            linkedItem = nil
        }
        // iPad and Apple TV: a sidebar (Home, the Libraries section, then the rest). On the TV a
        // top bar fits only about six tabs; the sidebar lists them all, and Back from the top of
        // a page (or Left from its leftmost item) opens it. iPhone keeps its tab bar.
        .tabViewStyle(.sidebarAdaptable)
        .environment(video)
        // A rating request after a movie or episode ended (USER-19).
        .ratingPromptOverlay(active: linkedItem == nil)
        .remoteToast()
        // Remote control (USER-14): a player while open, and while music plays in the background.
        .onChange(of: scenePhase, initial: true) { old, new in
            updateRemote()
            // Back in the app: anything that failed at launch (server unreachable) loads now,
            // and Live TV is checked again (a source may have lost its channels, LIVE-1).
            if old != new, new == .active { Task { await loadAll() } }
            // And changes made offline go to the server (USER-18).
            if old != new, new == .active { Task { await OfflineSync.shared.flush() } }
            // And the rating request switches may have changed (USER-19).
            if old != new, new == .active, app.client != nil {
                Task {
                    await app.refreshInfo()
                    await app.refreshMe()
                }
            }
        }
        .onChange(of: music.playing) { updateRemote() }
        // A video played: its watched state is the server's again.
        .onChange(of: video.request) { old, new in
            if new == nil, let old { WatchStore.shared.played(old.itemID) }
        }
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
            .ratingPromptOverlay()
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

    /// Playlists, Discover, Downloads (iPad), Now Playing (TV), Settings and Search.
    @TabContentBuilder<Never>
    private var otherTabs: some TabContent<Never> {
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
        Tab(role: .search) {
            stack(rootTitle: true) { SearchView() }

        }
    }

    private var compact: Bool {
        #if os(iOS)
        UIDevice.current.userInterfaceIdiom == .phone
        #else
        false
        #endif
    }

    /// A tab's navigation stack. On the TV the root page has no navigation title: the
    /// sidebar names it (open, and collapsed to its small button at the top left), and tvOS
    /// otherwise keeps a faded title over the content as it scrolls. Search keeps its bar.
    private func stack<Content: View>(rootTitle: Bool = false, @ViewBuilder _ content: () -> Content) -> some View {
        NavigationStack {
            content()
                .miniPlayerSpace()
                #if os(tvOS)
                .toolbar(rootTitle ? .automatic : .hidden, for: .navigationBar)
                #endif
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
        async let vod = try? app.vodStatus()
        do {
            libraries = try await app.libraries()
            librariesError = nil
        } catch is CancellationError {
        } catch {
            librariesError = error.localizedDescription
        }
        // A failed check keeps what was known (a tab doesn't vanish on a blip).
        if let s = await requests { canDiscover = s.enabled && s.canRequest }
        // Live TV is listed when its channels are usable; VOD Movies and VOD Shows (LIVE-6)
        // separately, after it, each while it has something.
        let l = await live, v = await vod
        if let v {
            hasVodMovies = v.enabled && v.movies > 0
            hasVodShows = v.enabled && v.series > 0
        }
        if let l { hasLiveTV = l.enabled }
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
/// listed after the video libraries when it's set up, with VOD Movies and VOD Shows (films and
/// shows on demand, LIVE-6) right after it when they have something.
enum LibraryEntry: Identifiable {
    case library(Library)
    case liveTV
    case vodMovies
    case vodShows

    var id: String {
        switch self {
        case .library(let l): "library-\(l.id)"
        case .liveTV: "liveTV"
        case .vodMovies: "vod"
        case .vodShows: "vodShows"
        }
    }

    static func list(_ libraries: [Library], liveTV: Bool, vodMovies: Bool = false, vodShows: Bool = false) -> [LibraryEntry] {
        var out = libraries.map(LibraryEntry.library)
        let video: Set<Schemas.LibraryType> = [.movies, .shows, .anime, .videos]
        var at = (libraries.lastIndex { video.contains($0._type) }).map { $0 + 1 } ?? libraries.count
        // Live TV after the video libraries when it has channels; VOD Movies and VOD Shows
        // (their own pages) after it.
        if liveTV {
            out.insert(.liveTV, at: at)
            at += 1
        }
        if vodMovies {
            out.insert(.vodMovies, at: at)
            at += 1
        }
        if vodShows { out.insert(.vodShows, at: at) }
        return out
    }
}

struct LibrariesList: View {
    @Environment(AppSession.self) private var app
    @State private var canDiscover = false
    let libraries: [Library]
    /// Live TV is set up: listed with the libraries.
    var liveTV = false
    /// Films / shows on demand are on: VOD Movies and VOD Shows are listed right after Live TV.
    var vodMovies = false
    var vodShows = false
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
            ForEach(LibraryEntry.list(libraries, liveTV: liveTV, vodMovies: vodMovies, vodShows: vodShows)) { entry in
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
                case .vodMovies:
                    NavigationLink(value: Route.liveTVMovies) {
                        Label {
                            VStack(alignment: .leading) {
                                Text("VOD Movies")
                                Text("Movies on demand").font(.caption).foregroundStyle(.secondary)
                            }
                        } icon: {
                            Image(systemName: "film.stack").foregroundStyle(Color.marqueeGold)
                        }
                    }
                    .accessibilityIdentifier("libraries.vod")
                case .vodShows:
                    NavigationLink(value: Route.vodShows) {
                        Label {
                            VStack(alignment: .leading) {
                                Text("VOD Shows")
                                Text("Shows on demand").font(.caption).foregroundStyle(.secondary)
                            }
                        } icon: {
                            Image(systemName: "play.tv").foregroundStyle(Color.marqueeGold)
                        }
                    }
                    .accessibilityIdentifier("libraries.vodShows")
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
