import MarqueeKit
import SwiftUI

/// A library: a music library's landing page, or a grid (a music library's artists or albums
/// when `type` says which).
struct LibraryView: View {
    @Environment(AppSession.self) private var app
    let libraryID: Int64
    var type: Schemas.ItemType? = nil
    var initialSort: ItemSort = .title
    @State private var library: Library?
    @State private var error: String?

    var body: some View {
        if let library {
            if library._type == .music && type == nil {
                MusicHomeView(library: library)
            } else {
                LibraryGrid(library: library, type: type, sort: initialSort)
            }
        } else if let error {
            ErrorBanner(message: error).padding()
        } else {
            ProgressView().padding(.top, 80).task {
                do {
                    library = try await app.libraries().first { $0.id == libraryID }
                    if library == nil { error = "This library is gone." }
                } catch {
                    self.error = error.localizedDescription
                }
            }
        }
    }
}

/// A library's grid, loaded a page at a time as you scroll.
struct LibraryGrid: View {
    @Environment(AppSession.self) private var app
    let library: Library
    var type: Schemas.ItemType?
    @State var sort: ItemSort
    @State private var items: [Item] = []
    @State private var total = 0
    @State private var loading = false
    @State private var unwatchedOnly = false
    /// Only what this person rated 4 stars or more (favourites).
    @State private var ratedOnly = false
    @State private var showCollections = false
    @State private var error: String?
    /// Each item's place in the grid (for paging) and the duplicate check.
    @State private var positions: [Int64: Int] = [:]
    /// Bumped by every reload, so a page that arrives for an older sort is dropped.
    @State private var generation = 0

    private var libraryID: Int64 { library.id }
    private var isMusic: Bool { library._type == .music }
    private var title: String {
        switch type {
        case .artist: "Artists"
        case .album: "Albums"
        default: library.name
        }
    }

    private static let page = 120

    var body: some View {
        ScrollView {
            if let error { ErrorBanner(message: error).padding() }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: minWidth, maximum: minWidth * 1.4), spacing: gap, alignment: .top)], spacing: gap) {
                ForEach(items, id: \.id) { item in
                    PosterCard(item: item, width: minWidth)
                        .onAppear { if (positions[item.id] ?? 0) >= items.count - 30 { Task { await loadMore() } } }
                }
            }
            .padding(.horizontal, sidePadding)
            .padding(.vertical)
            .accessibilityIdentifier("libraryGrid")
            if loading { ProgressView().padding() }
            if !loading, error == nil, items.isEmpty, generation > 0 {
                ContentUnavailableView(ratedOnly ? "Nothing rated 4★ or more" : unwatchedOnly ? "Nothing left to watch" : "Nothing here yet",
                                       systemImage: ratedOnly ? "star" : "square.grid.2x2",
                                       description: Text(ratedOnly || unwatchedOnly ? "Try turning the filter off." : "This library is empty, or still being scanned."))
            }
        }
        .navigationTitle(title)
        .toolbar {
            if [.movies, .shows, .anime, .videos].contains(library._type) {
                // Muse for this library (USER-15).
                ToolbarItem {
                    NavigationLink(value: Route.museVideo(library: libraryID)) { Label("Muse", systemImage: "sparkles") }
                }
            }
            ToolbarItem {
                Menu {
                    if library._type == .movies {
                        Picker("Show", selection: $showCollections) {
                            Text("Movies").tag(false)
                            Text("Collections").tag(true)
                        }
                    }
                    Picker("Sort", selection: $sort) {
                        Text("Title").tag(ItemSort.title)
                        Text("Recently added").tag(ItemSort._hyphen_added)
                        Text("Release date").tag(ItemSort._hyphen_released)
                        Text("Rating").tag(ItemSort._hyphen_rating)
                        if !showCollections { Text("My rating").tag(ItemSort._hyphen_myRating) }
                        Text(isMusic ? "Recently played" : "Last watched").tag(ItemSort._hyphen_viewed)
                        Text("Random").tag(ItemSort.random)
                    }
                    Toggle(isMusic ? "Unplayed only" : "Unwatched only", isOn: $unwatchedOnly)
                    if !showCollections { Toggle("Rated 4★+", isOn: $ratedOnly) }
                } label: {
                    Label("Sort and filter", systemImage: "line.3.horizontal.decrease.circle")
                }
                .accessibilityIdentifier("sortMenu")
            }
        }
        .task(id: "\(sort)-\(unwatchedOnly)-\(ratedOnly)-\(showCollections)") { await reload() }
    }

    #if os(tvOS)
    private let minWidth: CGFloat = 230
    private let gap: CGFloat = 50
    #else
    private let minWidth: CGFloat = 110
    private let gap: CGFloat = 14
    #endif

    private func reload() async {
        generation += 1
        loading = false // a load for the old sort may still be running; it's dropped
        items = []
        positions = [:]
        total = 0
        await loadMore()
    }

    private func loadMore() async {
        guard !loading, items.isEmpty || items.count < total else { return }
        let gen = generation
        loading = true
        defer { if gen == generation { loading = false } }
        do {
            let page = try await app.items(library: libraryID, sort: sort, offset: items.count, limit: Self.page,
                                           watch: unwatchedOnly && !showCollections ? .unwatched : nil, type: showCollections ? .collection : type,
                                           minMyRating: ratedOnly && !showCollections ? 8 : nil)
            guard gen == generation else { return }
            var fresh: [Item] = []
            for item in page.items where positions[item.id] == nil {
                positions[item.id] = items.count + fresh.count
                fresh.append(item)
            }
            items += fresh
            total = page.total
            error = nil
        } catch {
            // Cancelled (the sort changed) or overtaken by a newer load: not an error.
            if gen == generation, !Task.isCancelled { self.error = error.localizedDescription }
        }
    }
}
