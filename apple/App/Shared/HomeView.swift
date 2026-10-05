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
    @State private var editing = false

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
                    ShelfRow(title: hub.title, destination: destination(hub)) {
                        ForEach(hub.items, id: \.id) { item in
                            if wide && item.isPlayableVideo {
                                ContinueCard(item: item)
                            } else {
                                PosterCard(item: item, shape: PosterShape.for(item) == .wide ? .wide : nil)
                            }
                        }
                    }
                }
                #if os(tvOS)
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

    private func load() async {
        loadedVersion = HomeChanges.shared.version
        do {
            hubs = try await app.hubs()
            error = nil
            #if os(tvOS)
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
                return TopShelfSnapshot.Item(id: it.id, title: it.title,
                                             subtitle: it._type == .episode ? it.grandparentTitle : it.year.map(String.init),
                                             imageURL: app.imageURL(art, width: wide ? 640 : 300), wide: wide,
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
    #if os(tvOS)
    private let width: CGFloat = 420
    #else
    private let width: CGFloat = 260
    #endif

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
        .contextMenu { ItemMenuItems(item: item) }
    }
}
