import MarqueeKit
import SwiftUI

/// A music library's landing page, organised like Plexamp (MUSIC-15): quick actions, shelves
/// of what's yours, then the Library list into the full pages. Never every artist: the full
/// artist grid lives only behind Library › Artists.
struct MusicHomeView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let library: Library
    @State private var status: MusicStatus?
    @State private var mixes: [Station] = []
    @State private var recentlyPlayed: [Item] = []
    @State private var recentlyAdded: [Item] = []
    @State private var playlists: [Playlist] = []
    @State private var topArtists: [Item] = []
    @State private var decades: [String] = []
    @State private var counts: [MusicBrowse: Int] = [:]
    /// What this person rated 4 stars or more: albums and artists first, then tracks.
    @State private var favorites: [Item] = []
    @State private var busy: String?
    @State private var error: String?
    @State private var loaded = false

    private var libraryID: Int64 { library.id }
    private var soundprint: Bool { status?.enabled == true }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 26) {
                    quickActions { withAnimation { proxy.scrollTo("musicLibraryList", anchor: .top) } }
                    if let error { ErrorBanner(message: error).padding(.horizontal, sidePadding) }
                    if !recentlyPlayed.isEmpty {
                        ShelfRow(title: "Recently Played", destination: .musicBrowse(library: libraryID, .recentlyPlayed)) {
                            ForEach(recentlyPlayed, id: \.id) { PosterCard(item: $0) }
                        }
                    }
                    if !mixes.isEmpty {
                        ShelfRow(title: "Mixes for You", destination: soundprint ? .musicMuse(library: libraryID) : nil) {
                            ForEach(mixes, id: \.title) { m in MixCard(station: m) { music.playStation(m) } }
                        }
                    }
                    RecapCard() // MUSIC-22
                    if !recentlyAdded.isEmpty {
                        ShelfRow(title: "Recently Added", destination: .musicBrowse(library: libraryID, .recentAlbums)) {
                            ForEach(recentlyAdded, id: \.id) { PosterCard(item: $0) }
                        }
                    }
                    if !playlists.isEmpty {
                        ShelfRow(title: "Your Playlists", destination: .playlists) {
                            ForEach(playlists, id: \.id) { PlaylistCard(playlist: $0, size: PosterCard.defaultWidth) }
                        }
                    }
                    if !favorites.isEmpty {
                        ShelfRow(title: "Favorites", destination: .musicBrowse(library: libraryID, .favorites)) {
                            ForEach(favorites, id: \.id) { PosterCard(item: $0) }
                        }
                        .accessibilityElement(children: .contain)
                        .accessibilityIdentifier("favoritesShelf")
                    }
                    if !topArtists.isEmpty {
                        ShelfRow(title: "Top Artists", destination: .musicBrowse(library: libraryID, .artists)) {
                            ForEach(topArtists, id: \.id) { a in
                                PosterCard(item: a).accessibilityIdentifier("topArtist.\(a.id)")
                            }
                        }
                    }
                    libraryList.id("musicLibraryList")
                }
                .padding(.vertical)
            }
        }
        .navigationTitle(library.name)
        .task {
            // Once per visit to the library, not on every appear (coming back from an album).
            if !loaded { await load() }
        }
        .refreshable { await load() }
    }

    // MARK: - Library list

    /// Plexamp's Library tab: one row per full page, with its count.
    private var libraryList: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Library").font(.title3.bold()).padding(.horizontal, sidePadding)
            VStack(spacing: 0) {
                row("Artists", "music.mic", .musicBrowse(library: libraryID, .artists), count: counts[.artists], id: "artists")
                row("Albums", "square.stack", .musicBrowse(library: libraryID, .albums), count: counts[.albums], id: "albums")
                row("Songs", "music.note", .musicBrowse(library: libraryID, .songs), count: counts[.songs], id: "songs")
                row("Playlists", "music.note.list", .playlists, count: playlists.count, id: "playlists")
                row("Favorites", "heart", .musicBrowse(library: libraryID, .favorites), count: counts[.favorites], id: "favorites")
                row("Genres", "guitars", .musicBrowse(library: libraryID, .genres), id: "genres")
                if soundprint {
                    row("Moods & Styles", "theatermasks", .musicBrowse(library: libraryID, .moodsAndStyles), id: "moods")
                }
                if !decades.isEmpty {
                    row("Decades", "calendar", .musicBrowse(library: libraryID, .decades), id: "decades")
                }
                if soundprint {
                    row("Muse & Stations", "wand.and.stars", .musicMuse(library: libraryID), id: "muse", last: true)
                }
            }
            #if os(iOS)
            .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
            #endif
            .padding(.horizontal, sidePadding)
            #if os(tvOS)
            .focusSection()
            #endif
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("musicLibraryList")
    }

    private func row(_ title: String, _ icon: String, _ route: Route, count: Int? = nil, id: String, last: Bool = false) -> some View {
        NavigationLink(value: route) {
            HStack(spacing: 14) {
                Image(systemName: icon).foregroundStyle(Color.marqueeGold).frame(width: iconWidth)
                Text(title).foregroundStyle(.primary).lineLimit(1)
                Spacer(minLength: 8)
                if let count {
                    Text(count.formatted()).foregroundStyle(.secondary).monospacedDigit()
                        .accessibilityIdentifier("musicLibrary.\(id).count")
                }
                Image(systemName: "chevron.right").font(.footnote.bold()).foregroundStyle(.tertiary)
            }
            .padding(.horizontal, rowPadding)
            .padding(.vertical, rowVertical)
            .contentShape(Rectangle())
            #if os(iOS)
            .overlay(alignment: .bottom) {
                if !last { Divider().padding(.leading, rowPadding + iconWidth + 14) }
            }
            #endif
        }
        #if os(tvOS)
        .buttonStyle(.bordered)
        .padding(.vertical, 6)
        #else
        .buttonStyle(.plain)
        #endif
        .accessibilityLabel(count.map { "\(title), \($0)" } ?? title)
        .accessibilityIdentifier("musicLibrary.\(id)")
    }

    // MARK: - Quick actions

    /// Round icon buttons with small labels.
    private func quickActions(showLibrary: @escaping () -> Void) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(alignment: .top, spacing: actionSpacing) {
                if soundprint {
                    action("Library Radio", "dot.radiowaves.left.and.right") {
                        let r = RadioRequest(seed: .library, libraryId: libraryID, limit: 50)
                        music.playStation(try await app.radio(r), radio: r)
                    }
                }
                action("Shuffle All", "shuffle") {
                    let tracks = try await app.items(library: libraryID, sort: .random, limit: 200, type: .track).items
                    guard !tracks.isEmpty else { throw MarqueeError("There's no music here yet.") }
                    music.play(tracks, source: "Shuffle All")
                }
                if soundprint {
                    NavigationLink(value: Route.musicMuse(library: libraryID)) { roundLabel("Muse", "wand.and.stars") }
                        .buttonStyle(roundStyle)
                        .accessibilityLabel("Muse")
                        .accessibilityIdentifier("musicMuse")
                }
                #if os(iOS)
                CarModeButton(libraryID: libraryID) { roundLabel("Car Mode", "car.fill") }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Car Mode")
                // The Library list is far down on a phone: a shortcut to it.
                Button(action: showLibrary) { roundLabel("Library", "books.vertical") }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Library")
                    .accessibilityIdentifier("musicShowLibrary")
                #endif
            }
            .padding(.horizontal, sidePadding)
            #if os(tvOS)
            .padding(.vertical, 20)
            #endif
        }
        #if os(tvOS)
        .scrollClipDisabled()
        .focusSection()
        #endif
        // Not disabled while busy: on the TV that throws focus off the row. Taps are ignored
        // while an action runs instead.
    }

    private func action(_ title: String, _ icon: String, _ work: @escaping () async throws -> Void) -> some View {
        Button {
            guard busy == nil else { return }
            busy = title
            error = nil
            Task {
                defer { busy = nil }
                do { try await work() } catch is CancellationError {} catch { self.error = error.localizedDescription }
            }
        } label: {
            roundLabel(title, icon, busy: busy == title)
        }
        .buttonStyle(roundStyle)
        .accessibilityLabel(title)
    }

    private func roundLabel(_ title: String, _ icon: String, busy: Bool = false) -> some View {
        VStack(spacing: 6) {
            ZStack {
                Circle().fill(Color.marqueeGold.opacity(0.18))
                if busy { ProgressView() } else { Image(systemName: icon).font(iconFont).foregroundStyle(Color.marqueeGold) }
            }
            .frame(width: circle, height: circle)
            Text(title).font(.caption2.weight(.medium)).foregroundStyle(.secondary).lineLimit(1).fixedSize()
        }
        .frame(minWidth: circle + 8)
        .contentShape(Rectangle())
    }

    #if os(tvOS)
    private var roundStyle: some PrimitiveButtonStyle { .borderless }
    private let circle: CGFloat = 90
    private let iconFont = Font.title2
    private let actionSpacing: CGFloat = 40
    private let iconWidth: CGFloat = 44
    private let rowPadding: CGFloat = 12
    private let rowVertical: CGFloat = 6
    #else
    private var roundStyle: some PrimitiveButtonStyle { .plain }
    private let circle: CGFloat = 50
    private let iconFont = Font.title3
    private let actionSpacing: CGFloat = 18
    private let iconWidth: CGFloat = 26
    private let rowPadding: CGFloat = 14
    private let rowVertical: CGFloat = 13
    #endif

    // MARK: - Loading

    private func load() async {
        defer { loaded = true }
        async let statusCall = try? app.musicStatus()
        async let playedCall = try? app.hubs().first { $0.id == "played-\(libraryID)" }?.items
        async let addedCall = try? app.items(library: libraryID, sort: ._hyphen_added, limit: 20, type: .album).items
        async let playlistsCall = try? app.playlists(kind: .audio)
        async let topCall = topArtistsForMe()
        async let decadesCall = try? app.filters(library: libraryID, type: .album)
        async let countsCall = libraryCounts()
        async let favoritesCall = favoritesShelf()
        status = await statusCall
        if soundprint { mixes = (try? await app.mixes(library: libraryID)) ?? [] }
        recentlyPlayed = await playedCall ?? []
        recentlyAdded = await addedCall ?? []
        playlists = await playlistsCall ?? []
        topArtists = await topCall
        decades = await decadesCall?.decades.map(\.value) ?? []
        counts = await countsCall
        favorites = await favoritesCall
    }

    /// The Library list's counts: one tiny page each, reading its total, all at once.
    private func libraryCounts() async -> [MusicBrowse: Int] {
        let app = app
        let id = libraryID
        let wanted: [(MusicBrowse, Schemas.ItemType, Int?)] = [(.artists, .artist, nil), (.albums, .album, nil), (.songs, .track, nil), (.favorites, .track, 8)]
        return await withTaskGroup(of: (MusicBrowse, Int?).self) { group in
            for (kind, type, min) in wanted {
                group.addTask { @MainActor in (kind, try? await app.items(library: id, limit: 1, type: type, minMyRating: min).total) }
            }
            var out: [MusicBrowse: Int] = [:]
            for await (kind, total) in group { if let total { out[kind] = total } }
            return out
        }
    }

    /// Favourites (rated 4 stars or more): albums, artists, then tracks, best first.
    private func favoritesShelf() async -> [Item] {
        async let albums = try? app.items(library: libraryID, sort: ._hyphen_myRating, limit: 10, type: .album, minMyRating: 8).items
        async let artists = try? app.items(library: libraryID, sort: ._hyphen_myRating, limit: 10, type: .artist, minMyRating: 8).items
        async let tracks = try? app.items(library: libraryID, sort: ._hyphen_myRating, limit: 20, type: .track, minMyRating: 8).items
        return Array(((await albums ?? []) + (await artists ?? []) + (await tracks ?? [])).prefix(30))
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

}

/// Every song in a music library, sortable; a tap plays the list from there.
struct MusicSongsView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    @State private var sort: ItemSort = .title
    @State private var ratedOnly = false
    @State private var genre: String?
    @State private var decade: Int?
    /// The songs' own genres and decades (asked for with type=track).
    @State private var facets: LibraryFilters?
    @State private var tracks: [Item] = []
    @State private var total = 0
    @State private var loading = false
    @State private var error: String?
    @State private var generation = 0

    private static let page = 200

    private var sortMenu: some View {
        Menu {
            Picker("Sort", selection: $sort) {
                Text("Title").tag(ItemSort.title)
                Text("Recently added").tag(ItemSort._hyphen_added)
                Text("Rating").tag(ItemSort._hyphen_rating)
                Text("My rating").tag(ItemSort._hyphen_myRating)
                Text("Recently played").tag(ItemSort._hyphen_viewed)
                Text("Random").tag(ItemSort.random)
            }
            Toggle("Rated 4★+", isOn: $ratedOnly)
            FacetPickers(facets: facets, genre: $genre, decade: $decade)
        } label: {
            Label("Sort", systemImage: "arrow.up.arrow.down")
        }
        .accessibilityIdentifier("sortMenu")
    }

    var body: some View {
        List {
            if let error { ErrorBanner(message: error) }
            ForEach(Array(tracks.enumerated()), id: \.element.id) { i, t in
                HStack(spacing: 4) {
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
                    .accessibilityIdentifier("songRow.\(t.id)")
                    RowRating(item: t)
                }
                .onAppear { if i >= tracks.count - 40 { Task { await loadMore() } } }
            }
            if loading { ProgressView() }
            if !loading, error == nil, tracks.isEmpty {
                ContentUnavailableView(ratedOnly ? "No songs rated 4★ or more" : genre != nil || decade != nil ? "No songs match" : "No songs", systemImage: "music.note")
            }
        }
        .listStyle(.plain)
        .navigationTitle("Songs")
        #if os(tvOS)
        // The TV hides the navigation bar as the list scrolls: the sort menu stays pinned at
        // the top right (Right from the list reaches it).
        .safeAreaInset(edge: .trailing, alignment: .top, spacing: 0) {
            sortMenu.labelStyle(.iconOnly).padding(.top, 30).padding(.trailing, 50)
                .frame(maxHeight: .infinity, alignment: .top) // Right from any row lands here
                .focusSection()
        }
        #else
        .toolbar { ToolbarItem { sortMenu } }
        #endif
        .task { if facets == nil { facets = try? await app.filters(library: libraryID, type: .track) } }
        .task(id: "\(sort)-\(ratedOnly)-\(genre ?? "")-\(decade ?? 0)") {
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
            let page = try await app.items(library: libraryID, sort: sort, offset: tracks.count, limit: Self.page, genre: genre, type: .track,
                                           decade: decade, minMyRating: ratedOnly ? 8 : nil)
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

/// Moods and styles as tiles, on their own page from the Library list (MUSIC-18).
struct MoodsAndStylesPage: View {
    @Environment(AppSession.self) private var app
    let libraryID: Int64
    @State private var styles: [String] = []

    var body: some View {
        ScrollView {
            MoodsAndStyles(libraryID: libraryID, styles: styles).padding(.vertical)
        }
        .navigationTitle("Moods & Styles")
        .task {
            let f = try? await app.filters(library: libraryID, type: .album)
            styles = (f?.genres.sorted { $0.count > $1.count }.map(\.value) ?? []).prefix(30).map { $0 }
        }
    }
}

/// The decades a music library spans; each opens its albums and radio.
struct MusicDecadesView: View {
    @Environment(AppSession.self) private var app
    let libraryID: Int64
    @State private var decades: [Schemas.Facet] = []

    var body: some View {
        List {
            ForEach(decades, id: \.value) { d in
                if let year = Int(d.value) {
                    NavigationLink(value: Route.musicBrowse(library: libraryID, .decade(year))) {
                        HStack {
                            Text("\(d.value)s")
                            Spacer()
                            Text("\(d.count)").font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        .navigationTitle("Decades")
        .task {
            decades = ((try? await app.filters(library: libraryID, type: .album))?.decades ?? []).sorted { $0.value > $1.value }
        }
    }
}

/// One decade: its radio (with Soundprint) and its albums.
struct MusicDecadeView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    let decade: Int
    @State private var albums: [Item] = []
    @State private var radio = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if radio {
                    Button {
                        Task {
                            do {
                                let r = RadioRequest(seed: .decade, value: String(decade), libraryId: libraryID, limit: 50)
                                music.playStation(try await app.radio(r), radio: r)
                            } catch { self.error = error.localizedDescription }
                        }
                    } label: { Label("Play \(String(decade))s Radio", systemImage: "radio") }
                        .buttonStyle(.borderedProminent)
                        .padding(.horizontal, sidePadding)
                }
                if let error { ErrorBanner(message: error).padding(.horizontal, sidePadding) }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: minWidth), spacing: 14, alignment: .top)], spacing: 14) {
                    ForEach(albums, id: \.id) { PosterCard(item: $0, width: minWidth) }
                }
                .padding(.horizontal, sidePadding)
            }
            .padding(.vertical)
        }
        .navigationTitle("\(String(decade))s")
        .task {
            radio = (try? await app.musicStatus())?.enabled == true
            albums = (try? await app.items(library: libraryID, sort: .year, limit: 300, type: .album, decade: decade))?.items ?? []
        }
    }

    #if os(tvOS)
    private let minWidth: CGFloat = 230
    #else
    private let minWidth: CGFloat = 110
    #endif
}

/// Favourites: the tracks, albums and artists this person rated 4 stars or more, best first.
struct MusicFavoritesView: View {
    enum Kind: String, CaseIterable, Identifiable {
        case tracks = "Tracks", albums = "Albums", artists = "Artists"
        var id: String { rawValue }
        var type: Schemas.ItemType { self == .tracks ? .track : self == .albums ? .album : .artist }
    }

    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    @State private var kind: Kind = .tracks
    @State private var items: [Kind: [Item]] = [:]
    @State private var error: String?
    @State private var busy = false

    private var current: [Item]? { items[kind] }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Picker("Show", selection: $kind) {
                    ForEach(Kind.allCases) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .accessibilityIdentifier("favoritesKind")
                .padding(.horizontal, sidePadding)
                if let error {
                    VStack(alignment: .leading, spacing: 8) {
                        ErrorBanner(message: error)
                        Button("Try Again") { Task { await load(kind, force: true) } }.buttonStyle(.bordered)
                    }
                    .padding(.horizontal, sidePadding)
                }
                if let list = current {
                    if list.isEmpty {
                        ContentUnavailableView("No favorite \(kind.rawValue.lowercased()) yet", systemImage: "heart",
                                               description: Text("Rate something 4 stars or more and it shows up here."))
                    } else if kind == .tracks {
                        tracks(list)
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: minWidth), spacing: 14, alignment: .top)], spacing: 14) {
                            ForEach(list, id: \.id) { a in
                                VStack(alignment: .leading, spacing: 2) {
                                    PosterCard(item: a, width: minWidth)
                                    CommunityBadge(rating: a.communityRating) // everyone's average
                                }
                            }
                        }
                        .padding(.horizontal, sidePadding)
                    }
                } else if error == nil {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 60)
                }
            }
            .padding(.vertical)
        }
        .navigationTitle("Favorites")
        .task(id: kind) { await load(kind) }
        .refreshable { await load(kind, force: true) }
    }

    private func tracks(_ list: [Item]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Button { music.play(list, source: "Favorites") } label: { Label("Play", systemImage: "play.fill") }
                    .buttonStyle(.borderedProminent)
                Button { music.play(list, shuffle: true, source: "Favorites") } label: { Label("Shuffle", systemImage: "shuffle") }
                    .buttonStyle(.bordered)
            }
            .padding(.horizontal, sidePadding)
            .padding(.bottom, 10)
            ForEach(Array(list.enumerated()), id: \.element.id) { i, t in
                HStack(spacing: 4) {
                    Button { music.play(list, start: i, source: "Favorites") } label: {
                        HStack(spacing: 12) {
                            ArtworkView(item: t, shape: .square, width: 60).frame(width: isTV ? 80 : 44)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(t.title).lineLimit(1).foregroundStyle(music.current?.item.id == t.id ? Color.marqueeGold : .primary)
                                Text([t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · "))
                                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer()
                            CommunityBadge(rating: t.communityRating)
                        }
                        .padding(.leading, sidePadding)
                        .padding(.vertical, 6)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .contextMenu { ItemMenuItems(item: t) }
                    .accessibilityIdentifier("favoriteTrack.\(t.id)")
                    RowRating(item: t)
                }
                .padding(.trailing, sidePadding - 6)
            }
        }
    }

    private func load(_ k: Kind, force: Bool = false) async {
        guard force || items[k] == nil else { return }
        do {
            let page = try await app.items(library: libraryID, sort: ._hyphen_myRating, limit: k == .tracks ? 500 : 300,
                                           type: k.type, minMyRating: 8)
            items[k] = page.items
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    #if os(tvOS)
    private let minWidth: CGFloat = 230
    #else
    private let minWidth: CGFloat = 110
    #endif
}

/// Genre and decade choices from a library's filters (for the type being listed).
struct FacetPickers: View {
    let facets: LibraryFilters?
    @Binding var genre: String?
    @Binding var decade: Int?

    var body: some View {
        if let g = facets?.genres, !g.isEmpty {
            Picker("Genre", selection: $genre) {
                Text("Any genre").tag(String?.none)
                ForEach(g.sorted { $0.value.localizedCaseInsensitiveCompare($1.value) == .orderedAscending }, id: \.value) {
                    Text("\($0.value) (\($0.count))").tag(String?.some($0.value))
                }
            }
            .pickerStyle(.menu)
        }
        if let d = facets?.decades, !d.isEmpty {
            Picker("Decade", selection: $decade) {
                Text("Any decade").tag(Int?.none)
                ForEach(d.sorted { $0.value > $1.value }, id: \.value) { f in
                    if let y = Int(f.value) { Text("\(f.value)s (\(f.count))").tag(Int?.some(y)) }
                }
            }
            .pickerStyle(.menu)
        }
    }
}
