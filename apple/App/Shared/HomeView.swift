import MarqueeKit
import SwiftUI
#if os(tvOS)
import TVServices
#endif

struct HomeView: View {
    @Environment(AppSession.self) private var app
    @Environment(VideoPresenter.self) private var video
    @State private var hubs: [Hub] = []
    @State private var loaded = false
    /// The HomeChanges version this page last loaded.
    @State private var loadedVersion = -1
    @State private var error: String?

    @State private var groups: [WatchGroup] = []
    /// Video playlists, a row on the TV's Home: the Playlists tab sits at the far end of the
    /// top bar, out of sight behind the libraries.
    @State private var videoPlaylists: [Playlist] = []
    @State private var editing = false
    /// A film or episode on demand playing from Continue Watching.
    @State private var vodPlaying: VodPlayTarget?

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: rowSpacing) {
                if let error {
                    VStack(alignment: .leading, spacing: 8) {
                        ErrorBanner(message: error)
                        Button("Try Again") { Task { await load() } }.buttonStyle(.bordered)
                    }
                    .padding(.horizontal, sidePadding)
                }
                ForEach(groups, id: \.id) { g in
                    Button { video.play(g.itemId, group: g.id) } label: {
                        HStack(spacing: 12) {
                            Image(systemName: "person.2.fill").foregroundStyle(Color.marqueeGold)
                            VStack(alignment: .leading) {
                                Text(g.title).font(.headline).lineLimit(1)
                                Text("\(g.members.map(\.name).formatted()) \(g.members.count == 1 ? "is" : "are") watching together").font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Text("Join").font(.headline).foregroundStyle(Color.marqueeGold)
                        }
                        .padding()
                        .background(Color.marqueeGold.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
                    }
                    .buttonStyle(.plain)
                    .padding(.horizontal, sidePadding)
                    .accessibilityLabel("Join \(g.title)")
                }
                if loaded && hubs.isEmpty && error == nil {
                    ContentUnavailableView("Nothing here yet", systemImage: "film.stack", description: Text("Libraries are still being scanned, or none have been added."))
                }
                ForEach(hubs, id: \.id) { hub in
                    let wide = hub.id == "continue-watching"
                    if hub.id == Self.vodRow {
                        // Watch on VOD (LIVE-6, USER-16): films and series on demand picked
                        // for this viewer.
                        VodSuggestionRow(title: hub.title, list: hub.vodEntries ?? [], identifier: "home.vodSuggestions")
                    } else if wide, let entries = hub.entries {
                        // Library titles and films and episodes on demand together, in the
                        // server's order; View more when there are more than the row holds.
                        let more = (hub.total ?? 0) > entries.count
                        ShelfRow(title: hub.title, destination: more ? .continueWatching : nil) {
                            ForEach(entries, id: \.key) { e in
                                ContinueEntryTile(entry: e, onRemove: { removeFromContinue(e) }, playVod: playVod)
                            }
                            if more, let total = hub.total { ContinueViewMoreTile(total: total) }
                        }
                    } else {
                        ShelfRow(title: hub.title, destination: destination(hub)) {
                            ForEach(hub.items, id: \.id) { item in
                                let remove: (() -> Void)? = wide ? { removeFromContinue(item) } : nil
                                if wide && item.isPlayableVideo {
                                    ContinueCard(item: item, onRemove: remove)
                                } else {
                                    PosterCard(item: item, shape: PosterShape.for(item) == .wide ? .wide : nil, onRemoveFromContinue: remove)
                                }
                            }
                        }
                    }
                }
                #if os(tvOS)
                if !videoPlaylists.isEmpty {
                    ShelfRow(title: "Video Playlists") {
                        ForEach(videoPlaylists, id: \.id) { PlaylistCard(playlist: $0, size: 250) }
                    }
                }
                if loaded {
                    HStack(spacing: 30) {
                        Button { editing = true } label: { Label("Edit Home", systemImage: "slider.horizontal.3") }
                        NavigationLink(value: Route.museVideo(library: nil)) { Label("Muse", systemImage: "sparkles") }
                    }
                    .padding(.horizontal, sidePadding)
                }
                #endif
            }
            .padding(.vertical)
        }
        .navigationTitle("Home")
        #if os(iOS)
        .toolbar {
            // Muse for movies and shows (USER-15).
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink(value: Route.museVideo(library: nil)) { Label("Muse", systemImage: "sparkles") }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button { editing = true } label: { Label("Edit Home", systemImage: "slider.horizontal.3") }
            }
        }
        #endif
        #if os(iOS)
        .sheet(isPresented: $editing, onDismiss: { Task { await load() } }) {
            NavigationStack {
                HomeEditView()
                    .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { editing = false } } }
            }
        }
        #else
        // Full screen on the TV: a sheet is too narrow for the rows and their buttons.
        .fullScreenCover(isPresented: $editing, onDismiss: { Task { await load() } }) {
            NavigationStack { HomeEditView() }
                .background(Color(white: 0.06).ignoresSafeArea())
        }
        #endif
        .refreshable { await load() }
        // Changes made offline reached the server (USER-18).
        .onChange(of: OfflineSync.shared.generation) { Task { await load() } }
        .task {
            // Watch-together groups to join, refreshed while Home is open.
            while !Task.isCancelled {
                let me = app.me?.id
                groups = ((try? await app.watchGroups()) ?? []).filter { !$0.members.contains { $0.userId == me } }
                try? await Task.sleep(for: .seconds(15))
            }
        }
        .task(id: HomeChanges.shared.version) {
            // Once, and again after a pin elsewhere; otherwise Home refreshes when a video closes
            // (Continue Watching) or on pull.
            if !loaded || loadedVersion != HomeChanges.shared.version { await load() }
        }
        .onChange(of: video.request) { if video.request == nil { Task { await load() } } }
        .modifier(VodContinuePlayback(target: $vodPlaying, onDismiss: { Task { await load() } }))
    }

    /// A film or episode on demand from Continue Watching, where it stopped.
    private func playVod(_ entry: ContinueEntry) {
        ActionError.run { vodPlaying = try await app.vodPlayTarget(entry) }
    }

    /// Takes an entry off Continue Watching: a library title as before, or one on demand (an
    /// episode takes the rest of its show with it).
    private func removeFromContinue(_ entry: ContinueEntry) {
        if let item = entry.item { removeFromContinue(item); return }
        let before = hubs
        if let i = hubs.firstIndex(where: { $0.id == "continue-watching" }) {
            let gone = hubs[i].entries?.filter { entry.removes($0) }.count ?? 0
            hubs[i].entries?.removeAll { entry.removes($0) }
            hubs[i].total = hubs[i].total.map { max(0, $0 - gone) }
            if Self.isEmptyContinue(hubs[i]) { hubs.remove(at: i) }
        }
        ActionError.run {
            do {
                try await app.removeVodFromContinueWatching(entry)
            } catch {
                hubs = before
                throw error
            }
            await load()
        }
    }

    /// Home's Watch on VOD row.
    private static let vodRow = "vod-suggestions"

    /// Continue Watching with nothing in it (the server may send the row with no library items
    /// but films on demand, or nothing left after a removal).
    private static func isEmptyContinue(_ hub: Hub) -> Bool {
        hub.id == "continue-watching" && hub.items.isEmpty && (hub.entries ?? []).isEmpty
    }

    #if os(tvOS)
    private let rowSpacing: CGFloat = 20
    #else
    private let rowSpacing: CGFloat = 28
    #endif

    /// Where a row's title leads: its library, or a pinned collection or playlist (USER-12).
    /// Recommendations (USER-16) have no "see all".
    private func destination(_ hub: Hub) -> Route? {
        if hub.id == "recommended" || hub.id.hasPrefix("because-") { return nil }
        if hub.id.hasPrefix("collection-"), let id = Int64(hub.id.dropFirst("collection-".count)) { return .item(id) }
        if hub.id.hasPrefix("playlist-"), let id = Int64(hub.id.dropFirst("playlist-".count)) { return .playlist(id) }
        return hub.libraryId.map { .library($0) }
    }

    /// Takes a title off Continue Watching (USER-6): gone from the row at once (an episode
    /// takes its show's other episodes with it), then the row comes from the server again.
    private func removeFromContinue(_ item: Item) {
        let before = hubs
        if let i = hubs.firstIndex(where: { $0.id == "continue-watching" }) {
            let sameShow: (Item) -> Bool = { $0.id == item.id || (item._type == .episode && item.grandparentId != nil && $0.grandparentId == item.grandparentId) }
            hubs[i].items.removeAll(where: sameShow)
            let gone = hubs[i].entries?.filter { $0.item.map(sameShow) ?? false }.count ?? 0
            hubs[i].entries?.removeAll { $0.item.map(sameShow) ?? false }
            hubs[i].total = hubs[i].total.map { max(0, $0 - gone) }
            if Self.isEmptyContinue(hubs[i]) { hubs.remove(at: i) }
        }
        ActionError.run {
            do {
                try await app.removeFromContinueWatching(item.id)
            } catch {
                hubs = before
                throw error
            }
            await load()
        }
    }

    private func load() async {
        loadedVersion = HomeChanges.shared.version
        do {
            hubs = try await app.hubs(vod: true).filter { !Self.isEmptyContinue($0) && !($0.id == Self.vodRow && ($0.vodEntries ?? []).isEmpty) }
            error = nil
            #if os(tvOS)
            if let lists = try? await app.playlists(kind: .video) { videoPlaylists = lists }
            saveTopShelf()
            #endif
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }

    #if os(tvOS)
    /// Continue Watching and the newest titles for the Apple TV Top Shelf.
    private func saveTopShelf() {
        let wanted = hubs.filter { $0.id == "continue-watching" || ($0.id.hasPrefix("recent-") && $0.items.first?._type != .album) }.prefix(3)
        let sections = wanted.map { hub in
            TopShelfSnapshot.Section(title: hub.title, items: hub.items.prefix(12).map { it in
                let wide = hub.id == "continue-watching"
                let art = wide ? (it.images?.thumb ?? it.images?.backdrop) : it.images?.poster
                // Only image-key URLs: until the key is known an image URL carries the sign-in
                // token, which mustn't be written to the shared container's file.
                let image = app.imageURLsUseKey ? app.imageURL(art, width: wide ? 640 : 300) : nil
                return TopShelfSnapshot.Item(id: it.id, title: it.title,
                                             subtitle: it._type == .episode ? it.grandparentTitle : it.year.map(String.init),
                                             imageURL: image, wide: wide,
                                             playable: [.movie, .episode, .video].contains(it._type))
            })
        }
        TopShelfSnapshot(sections: Array(sections)).save()
        TVTopShelfContentProvider.topShelfContentDidChange()
    }
    #endif
}

/// Continue Watching: wide art that plays straight away.
struct ContinueCard: View {
    @Environment(VideoPresenter.self) private var video
    let item: Item
    /// Takes it off Continue Watching (its context menu).
    var onRemove: (() -> Void)?
    #if os(tvOS)
    static let width: CGFloat = 420
    #else
    static let width: CGFloat = 260
    #endif
    private var width: CGFloat { Self.width }

    var body: some View {
        Button { video.play(item.id) } label: {
            VStack(alignment: .leading, spacing: 6) {
                ArtworkView(item: item, shape: .wide, width: width)
                    .overlay { Image(systemName: "play.circle.fill").font(.system(size: 40)).foregroundStyle(.white.opacity(0.85)).shadow(radius: 6) }
                Text(item._type == .episode ? (item.grandparentTitle ?? item.title) : item.title).font(.subheadline.weight(.medium)).lineLimit(1)
                Text(item._type == .episode ? "\(item.subtitle) · \(item.title)" : item.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            .frame(width: width)
        }
        #if os(tvOS)
        .buttonStyle(.card)
        #else
        .buttonStyle(.plain)
        #endif
        .accessibilityIdentifier("continue.\(item.id)")
        .contextMenu {
            if let onRemove { RemoveFromContinueButton(action: onRemove) }
            ItemMenuItems(item: item)
        }
    }
}

/// The VOD pages' Continue Watching rows' context menu (LIVE-6): Mark Watched and Mark
/// Unwatched when `mark` is given, and "Remove from Continue Watching" when there's an action;
/// no menu outside the row.
struct RemoveFromContinueMenu: ViewModifier {
    let action: (() -> Void)?
    var mark: (@MainActor (Bool) async throws -> Void)?
    func body(content: Content) -> some View {
        if action != nil || mark != nil {
            content.contextMenu {
                if let mark { VodMarkWatchedButtons(save: mark) }
                if let action { RemoveFromContinueButton(action: action) }
            }
        } else {
            content
        }
    }
}

/// "Remove from Continue Watching" in a Continue Watching tile's context menu (USER-6).
struct RemoveFromContinueButton: View {
    let action: () -> Void
    var body: some View {
        Button(role: .destructive, action: action) { Label("Remove from Continue Watching", systemImage: "eye.slash") }
            .accessibilityIdentifier("removeFromContinue")
    }
}
