import MarqueeKit
import SwiftUI

/// Which sidebar row is showing and each row's pages, kept apart from the views so they survive
/// closing the window. Menus reach them too.
@MainActor @Observable
final class MacNavigation {
    static let shared = MacNavigation()
    static let home = "home", playlists = "playlists", downloads = "downloads", discover = "discover"
    static let search = "search", settings = "settings", profile = "profile"

    var selection: String? = home
    var paths: [String: NavigationPath] = [:]
    var showNowPlaying = false
    var searchFocused = false

    func select(_ row: String) { selection = row }

    func focusSearch() {
        selection = Self.search
        searchFocused = true
    }

    func goBack() {
        guard let s = selection, var p = paths[s], !p.isEmpty else { return }
        p.removeLast()
        paths[s] = p
    }

    /// Opens a page on the current stack (from a menu, which can't hold a link).
    func push(_ route: Route) {
        guard let s = selection else { return }
        paths[s, default: NavigationPath()].append(route)
    }

    func reset() {
        selection = Self.home
        paths = [:]
    }
}

/// The Mac's sidebar like Apple Music's, with the now-playing bar along the bottom and a video over
/// everything while one plays.
struct MainView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(VideoPresenter.self) private var video
    @State private var nav = MacNavigation.shared
    @State private var libraries: [Library] = []
    @State private var playlistPicker = PlaylistPicker.shared
    @State private var journey = JourneyPicker.shared
    @State private var actionError = ActionError.shared
    @State private var linkedItem: LinkedItem?
    @State private var canDiscover = false
    @State private var hasLiveTV = false
    @State private var hasVodMovies = false
    @State private var hasVodShows = false
    @State private var librariesError: String?
    @State private var profileIcon = ProfileIcon()
    @Environment(SmartDownloads.self) private var smart
    /// Bumped when another person signs in: the pages start again for them.
    var generation = 0

    var body: some View {
        ZStack {
            VStack(spacing: 0) {
                NavigationSplitView {
                    sidebar
                } detail: {
                    detail
                }
                Divider()
                MacNowPlayingBar()
            }
            .opacity(video.request == nil ? 1 : 0)
            .accessibilityHidden(video.request != nil)
            if let req = video.request {
                PlayerView(request: req)
                    .id(req.id)
                    .ignoresSafeArea()
                    .transition(.opacity)
            }
        }
        .id(generation)
        .toolbar(video.request == nil ? .automatic : .hidden, for: .windowToolbar)
        .ratingPromptOverlay(active: video.request == nil)
        .onChange(of: generation) {
            video.request = nil
            linkedItem = nil
            nav.reset()
        }
        .task(id: "\(app.me?.id ?? 0)|\(app.me?.avatarUrl ?? "")|\(app.me?.displayName ?? "")") {
            await profileIcon.load(app: app, size: 20)
        }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            Task { await loadAll() }
            Task { await OfflineSync.shared.flush() }
            if app.client != nil {
                Task {
                    await app.refreshInfo()
                    await app.refreshMe()
                }
            }
            if app.isOffline { Task { await app.reconnect() } }
        }
        .onChange(of: app.isOffline, initial: true) {
            if !app.isOffline {
                HomeChanges.shared.version += 1
            } else if nav.selection != MacNavigation.settings {
                nav.selection = MacNavigation.downloads // the row showing, in the shorter sidebar
            }
        }
        .onChange(of: video.request) { old, new in
            if new == nil, let old { WatchStore.shared.played(old.itemID) }
            if new == nil, old != nil { Task { try? await Task.sleep(for: .seconds(3)); await smart.run(app) } }
        }
        .task(id: "\(app.baseURL?.absoluteString ?? "")|\(app.state)|\(app.me?.id ?? 0)|\(app.isOffline)") {
            await loadAll()
            await refreshRooms()
        }
        .sheet(item: $playlistPicker.item) { item in AddToPlaylistSheet(item: item).frame(minWidth: 420, minHeight: 440) }
        .sheet(item: $journey.from) { item in JourneySheet(from: item).frame(minWidth: 480, minHeight: 480) }
        .alert("Something went wrong", isPresented: Binding(get: { actionError.message != nil }, set: { if !$0 { actionError.message = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(actionError.message ?? "")
        }
        .onOpenURL { url in
            guard url.scheme == "marquee", let id = Int64(url.lastPathComponent) else { return }
            if url.host() == "play" { video.play(id) } else if url.host() == "item" { nav.push(.item(id)) }
        }
        .sheet(isPresented: $nav.showNowPlaying) {
            NowPlayingView()
                .frame(width: 940, height: 620)
                .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Done") { nav.showNowPlaying = false } } }
                .macEnvironment()
        }
        .environment(\.openNowPlaying, OpenNowPlayingAction { nav.showNowPlaying = true })
    }

    // MARK: Sidebar

    private var sidebar: some View {
        List(selection: $nav.selection) {
            if app.isOffline {
                Section {
                    row("Offline Library", "arrow.down.circle", MacNavigation.downloads)
                    row("Settings", "gearshape", MacNavigation.settings)
                }
            } else {
                Section {
                    Label {
                        Text(app.me?.displayName ?? "Profile")
                    } icon: {
                        if let icon = profileIcon.image { Image(nsImage: icon) } else { Image(systemName: "person.crop.circle") }
                    }
                    .tag(MacNavigation.profile)
                    .accessibilityIdentifier("sidebar.profile")
                    row("Home", "house", MacNavigation.home)
                }
                Section("Libraries") {
                    ForEach(LibraryEntry.list(libraries, liveTV: hasLiveTV, vodMovies: hasVodMovies, vodShows: hasVodShows)) { entry in
                        switch entry {
                        case .library(let lib): row(lib.name, icon(lib._type), "library-\(lib.id)")
                        case .liveTV: row("Live TV", "antenna.radiowaves.left.and.right", "livetv")
                        case .vodMovies: row("VOD Movies", "film.stack", "vodMovies")
                        case .vodShows: row("VOD Shows", "play.tv", "vodShows")
                        }
                    }
                    if libraries.isEmpty, librariesError != nil {
                        row("Libraries", "exclamationmark.triangle", "librariesError")
                    }
                }
                Section {
                    row("Playlists", "music.note.list", MacNavigation.playlists)
                    row("Downloads", "arrow.down.circle", MacNavigation.downloads)
                    if canDiscover { row("Discover", "safari", MacNavigation.discover) }
                    row("Search", "magnifyingglass", MacNavigation.search)
                    row("Settings", "gearshape", MacNavigation.settings)
                }
            }
        }
        .listStyle(.sidebar)
        .safeAreaInset(edge: .bottom) {
            if let update = MacUpdates.shared.available {
                Button { MacUpdates.shared.openRelease() } label: {
                    Label("Marquee \(update.version) is available", systemImage: "arrow.down.circle.fill")
                        .font(.caption)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(8)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("sidebar.update")
            }
        }
        .navigationSplitViewColumnWidth(min: 190, ideal: 220, max: 300)
        .accessibilityIdentifier("sidebar")
    }

    private func row(_ title: String, _ icon: String, _ tag: String) -> some View {
        Label(title, systemImage: icon)
            .tag(tag)
            .accessibilityIdentifier("sidebar.\(tag)")
    }

    // MARK: Pages

    /// What's showing: offline only what works without the server.
    private var current: String {
        let s = nav.selection ?? MacNavigation.home
        if app.isOffline { return s == MacNavigation.settings ? s : MacNavigation.downloads }
        return s
    }

    private var detail: some View {
        let row = current
        return NavigationStack(path: Binding(get: { nav.paths[row] ?? NavigationPath() }, set: { nav.paths[row] = $0 })) {
            page(row)
                .marqueeDestinations()
                .navigationDestination(for: OfflineRoute.self) { OfflineRouteView(route: $0) }
        }
        .id(row)
        .offlineBanner()
    }

    @ViewBuilder private func page(_ row: String) -> some View {
        switch row {
        case MacNavigation.profile:
            ProfilesPage(onAccountSettings: { nav.select(MacNavigation.settings) }, showsSignOut: true)
                .navigationTitle(app.me?.displayName ?? "Profile")
        case MacNavigation.home: HomeView()
        case "livetv": LiveTVView()
        case "vodMovies": VodPage()
        case "vodShows": VodPage(shows: true)
        case MacNavigation.playlists: PlaylistsView()
        case MacNavigation.downloads:
            if app.isOffline { OfflineLibraryView().navigationTitle("Offline Library") } else { DownloadsView() }
        case MacNavigation.discover: DiscoverView()
        case MacNavigation.search: SearchView()
        case MacNavigation.settings: SettingsView()
        case "librariesError":
            LibrariesList(libraries: [], liveTV: false, error: librariesError, icon: icon, reload: { await loadAll() })
        default:
            if row.hasPrefix("library-"), let id = Int64(row.dropFirst("library-".count)) {
                LibraryView(libraryID: id).id(id)
            } else {
                HomeView()
            }
        }
    }

    private func refreshRooms() async {
        guard app.client != nil, !app.isOffline else {
            MacPlayback.shared.sonos.clearRooms()
            return
        }
        await MacPlayback.shared.sonos.refreshRooms()
    }

    private func loadAll() async {
        guard app.client != nil, !app.isOffline else { return }
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
        if let s = await requests { canDiscover = s.enabled && s.canRequest }
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
