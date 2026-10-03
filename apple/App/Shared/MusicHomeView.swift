import MarqueeKit
import SwiftUI

/// A music library's landing page: a browse row into the full lists, quick actions, then
/// shelves of what's yours (MUSIC-15). The full artist grid lives on the Artists screen.
struct MusicHomeView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let library: Library
    @State private var status: MusicStatus?
    @State private var mixes: [Station] = []
    @State private var recentlyPlayed: [Item] = []
    @State private var recentlyAdded: [Item] = []
    @State private var styles: [String] = []
    @State private var playlists: [Playlist] = []
    @State private var topArtists: [Item] = []
    @State private var busy: String?
    @State private var error: String?

    private var libraryID: Int64 { library.id }
    private var soundprint: Bool { status?.enabled == true }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 26) {
                browseRow
                quickActions
                if let error { ErrorBanner(message: error).padding(.horizontal, sidePadding) }
                RecapCard() // MUSIC-22
                if !mixes.isEmpty {
                    ShelfRow(title: "Mixes for you") {
                        ForEach(mixes, id: \.title) { m in MixCard(station: m) { music.playStation(m) } }
                    }
                }
                if !recentlyPlayed.isEmpty {
                    ShelfRow(title: "Recently Played") {
                        ForEach(recentlyPlayed, id: \.id) { PosterCard(item: $0) }
                    }
                }
                if !recentlyAdded.isEmpty {
                    ShelfRow(title: "Recently Added", destination: .musicBrowse(library: libraryID, .recentAlbums)) {
                        ForEach(recentlyAdded, id: \.id) { PosterCard(item: $0) }
                    }
                }
                if soundprint { MoodsAndStyles(libraryID: libraryID, styles: styles) }
                if !playlists.isEmpty {
                    ShelfRow(title: "Your Playlists", destination: .playlists) {
                        ForEach(playlists, id: \.id) { PlaylistCard(playlist: $0, size: PosterCard.defaultWidth) }
                    }
                }
                if !topArtists.isEmpty {
                    ShelfRow(title: "Top Artists", destination: .musicBrowse(library: libraryID, .artists)) {
                        ForEach(topArtists, id: \.id) { PosterCard(item: $0) }
                    }
                    .accessibilityIdentifier("topArtists")
                }
            }
            .padding(.vertical)
        }
        .navigationTitle(library.name)
        .task { await load() }
        .refreshable { await load() }
    }

    // MARK: - Browse and quick actions

    private var browseRow: some View {
        #if os(tvOS)
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: chipSpacing) { browseLinks }
                .padding(.horizontal, sidePadding)
                .padding(.vertical, 20)
        }
        .scrollClipDisabled()
        .focusSection()
        #else
        // Five equal tiles, icon over word, so all of them fit across a phone.
        HStack(spacing: 8) { browseLinks }
            .padding(.horizontal, sidePadding)
        #endif
    }

    @ViewBuilder private var browseLinks: some View {
        browse("Artists", "music.mic", .musicBrowse(library: libraryID, .artists), id: "artists")
        browse("Albums", "square.stack", .musicBrowse(library: libraryID, .albums), id: "albums")
        browse("Songs", "music.note", .musicBrowse(library: libraryID, .songs), id: "songs")
        browse("Playlists", "music.note.list", .playlists, id: "playlists")
        browse("Genres", "guitars", .musicBrowse(library: libraryID, .genres), id: "genres")
    }

    private func browse(_ title: String, _ icon: String, _ route: Route, id: String) -> some View {
        NavigationLink(value: route) {
            #if os(tvOS)
            Label(title, systemImage: icon).lineLimit(1).fixedSize()
            #else
            VStack(spacing: 4) {
                Image(systemName: icon).font(.title3).frame(height: 26)
                Text(title).font(.caption.weight(.medium)).lineLimit(1).minimumScaleFactor(0.8)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 4)
            #endif
        }
        .buttonStyle(.bordered)
        .accessibilityLabel(title)
        .accessibilityIdentifier("musicBrowse.\(id)")
    }

    private var quickActions: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: chipSpacing) {
                if soundprint {
                    pill("Library Radio", "dot.radiowaves.left.and.right") {
                        let r = RadioRequest(seed: .library, libraryId: libraryID, limit: 50)
                        music.playStation(try await app.radio(r), radio: r)
                    }
                }
                pill("Shuffle All", "shuffle") {
                    let tracks = try await app.items(library: libraryID, sort: .random, limit: 200, type: .track).items
                    guard !tracks.isEmpty else { throw MarqueeError("There's no music here yet.") }
                    music.play(tracks, source: "Shuffle All")
                }
                if soundprint {
                    NavigationLink(value: Route.musicMuse(library: libraryID)) {
                        Label("Muse", systemImage: "wand.and.stars").lineLimit(1).fixedSize()
                    }
                    .buttonStyle(.bordered)
                    .accessibilityIdentifier("musicMuse")
                }
                #if os(iOS)
                CarModeButton(libraryID: libraryID).buttonStyle(.bordered).lineLimit(1).fixedSize()
                #endif
            }
            .font(.subheadline)
            .padding(.horizontal, sidePadding)
            #if os(tvOS)
            .padding(.vertical, 20)
            #endif
        }
        #if os(tvOS)
        .scrollClipDisabled()
        .focusSection()
        #endif
        .disabled(busy != nil)
    }

    private func pill(_ title: String, _ icon: String, _ work: @escaping () async throws -> Void) -> some View {
        Button {
            guard busy == nil else { return }
            busy = title
            error = nil
            Task {
                defer { busy = nil }
                do { try await work() } catch is CancellationError {} catch { self.error = error.localizedDescription }
            }
        } label: {
            if busy == title {
                HStack(spacing: 6) { ProgressView(); Text(title) }.lineLimit(1).fixedSize()
            } else {
                Label(title, systemImage: icon).lineLimit(1).fixedSize()
            }
        }
        .buttonStyle(.bordered)
        .accessibilityLabel(title)
    }

    // MARK: - Loading

    private func load() async {
        async let statusCall = try? app.musicStatus()
        async let playedCall = try? app.hubs().first { $0.id == "played-\(libraryID)" }?.items
        async let addedCall = try? app.items(library: libraryID, sort: ._hyphen_added, limit: 20, type: .album).items
        async let playlistsCall = try? app.playlists(kind: .audio)
        async let topCall = topArtistsForMe()
        status = await statusCall
        recentlyPlayed = await playedCall ?? []
        recentlyAdded = await addedCall ?? []
        playlists = await playlistsCall ?? []
        topArtists = await topCall
        if soundprint {
            mixes = (try? await app.mixes(library: libraryID)) ?? []
            let f = try? await app.filters(library: libraryID, type: .album)
            styles = (f?.genres.sorted { $0.count > $1.count }.map(\.value) ?? []).prefix(18).map { $0 }
        }
    }

    /// The artists this person plays most (all time), in this library.
    private func topArtistsForMe() async -> [Item] {
        guard let stats = try? await app.stats(days: 0, limit: 30, userID: app.me?.id) else { return [] }
        let ids = stats.artists.compactMap(\.id)
        guard !ids.isEmpty else { return [] }
        // The library's artists, most recently played first, carry the artwork.
        let recent = (try? await app.items(library: libraryID, sort: ._hyphen_viewed, limit: 200, type: .artist).items) ?? []
        let byID = Dictionary(recent.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        return Array(ids.compactMap { byID[$0] }.prefix(20))
    }

    #if os(tvOS)
    private let chipSpacing: CGFloat = 24
    #else
    private let chipSpacing: CGFloat = 8
    #endif
}

/// Every song in a music library, sortable; a tap plays the list from there.
struct MusicSongsView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    @State private var sort: ItemSort = .title
    @State private var tracks: [Item] = []
    @State private var total = 0
    @State private var loading = false
    @State private var error: String?
    @State private var generation = 0

    private static let page = 200

    var body: some View {
        List {
            if let error { ErrorBanner(message: error) }
            ForEach(Array(tracks.enumerated()), id: \.element.id) { i, t in
                Button { music.play(tracks, start: i, source: "Songs") } label: {
                    HStack(spacing: 12) {
                        ArtworkView(item: t, shape: .square, width: 60).frame(width: 44)
                        VStack(alignment: .leading) {
                            Text(t.title).lineLimit(1)
                            Text([t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · "))
                                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer()
                        Text(formatTime(seconds: Double(t.durationMs ?? 0) / 1000)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .contextMenu { ItemMenuItems(item: t) }
                .onAppear { if i >= tracks.count - 40 { Task { await loadMore() } } }
            }
            if loading { ProgressView() }
        }
        .listStyle(.plain)
        .navigationTitle("Songs")
        .toolbar {
            ToolbarItem {
                Menu {
                    Picker("Sort", selection: $sort) {
                        Text("Title").tag(ItemSort.title)
                        Text("Recently added").tag(ItemSort._hyphen_added)
                        Text("Rating").tag(ItemSort._hyphen_rating)
                        Text("Recently played").tag(ItemSort._hyphen_viewed)
                        Text("Random").tag(ItemSort.random)
                    }
                } label: {
                    Label("Sort", systemImage: "arrow.up.arrow.down")
                }
            }
        }
        .task(id: sort) {
            generation += 1
            tracks = []
            total = 0
            loading = false
            await loadMore()
        }
    }

    private func loadMore() async {
        guard !loading, tracks.isEmpty || tracks.count < total else { return }
        let gen = generation
        loading = true
        defer { if gen == generation { loading = false } }
        do {
            let page = try await app.items(library: libraryID, sort: sort, offset: tracks.count, limit: Self.page, type: .track)
            guard gen == generation else { return }
            let have = Set(tracks.map(\.id))
            tracks += page.items.filter { !have.contains($0.id) }
            total = page.total
            error = nil
        } catch {
            if gen == generation, !Task.isCancelled { self.error = error.localizedDescription }
        }
    }
}

/// A music library's genres, by name or size; each opens its style page (MUSIC-18).
struct MusicGenresView: View {
    @Environment(AppSession.self) private var app
    let libraryID: Int64
    @State private var genres: [Schemas.Facet] = []
    @State private var bySize = false
    @State private var loaded = false

    private var sorted: [Schemas.Facet] {
        bySize ? genres.sorted { $0.count > $1.count }
            : genres.sorted { $0.value.localizedCaseInsensitiveCompare($1.value) == .orderedAscending }
    }

    var body: some View {
        List {
            ForEach(sorted, id: \.value) { g in
                NavigationLink(value: Route.moodStyle(library: libraryID, mood: false, name: g.value)) {
                    HStack {
                        Text(g.value)
                        Spacer()
                        Text("\(g.count)").font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
            }
            if loaded && genres.isEmpty {
                ContentUnavailableView("No genres", systemImage: "guitars", description: Text("Your albums' tags don't name any genres."))
            }
        }
        .navigationTitle("Genres")
        .toolbar {
            ToolbarItem {
                Menu {
                    Picker("Sort", selection: $bySize) {
                        Text("Name").tag(false)
                        Text("Most albums").tag(true)
                    }
                } label: {
                    Label("Sort", systemImage: "arrow.up.arrow.down")
                }
            }
        }
        .task {
            genres = (try? await app.filters(library: libraryID, type: .album))?.genres ?? []
            loaded = true
        }
    }
}
