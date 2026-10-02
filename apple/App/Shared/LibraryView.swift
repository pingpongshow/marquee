import MarqueeKit
import SwiftUI

/// A library's grid, loaded a page at a time as you scroll.
struct LibraryView: View {
    @Environment(AppSession.self) private var app
    let libraryID: Int64
    @State private var library: Library?
    @State private var items: [Item] = []
    @State private var total = 0
    @State private var loading = false
    @State private var sort: ItemSort = .title
    @State private var unwatchedOnly = false
    @State private var showCollections = false
    @State private var error: String?
    /// Each item's place in the grid (for paging) and the duplicate check.
    @State private var positions: [Int64: Int] = [:]
    /// Bumped by every reload, so a page that arrives for an older sort is dropped.
    @State private var generation = 0

    private static let page = 120

    var body: some View {
        ScrollView {
            if let error { ErrorBanner(message: error).padding() }
            if library?._type == .music {
                MusicDiscoverView(libraryID: libraryID).padding(.top)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: minWidth, maximum: minWidth * 1.4), spacing: gap, alignment: .top)], spacing: gap) {
                ForEach(items, id: \.id) { item in
                    PosterCard(item: item, width: minWidth)
                        .onAppear { if (positions[item.id] ?? 0) >= items.count - 30 { Task { await loadMore() } } }
                }
            }
            .padding(.horizontal, sidePadding)
            .padding(.vertical)
            if loading { ProgressView().padding() }
        }
        .navigationTitle(library?.name ?? "Library")
        .toolbar {
            ToolbarItem {
                Menu {
                    if library?._type == .movies {
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
                        Text("Last watched").tag(ItemSort._hyphen_viewed)
                        Text("Random").tag(ItemSort.random)
                    }
                    Toggle(library?._type == .music ? "Unplayed only" : "Unwatched only", isOn: $unwatchedOnly)
                } label: {
                    Label("Sort and filter", systemImage: "line.3.horizontal.decrease.circle")
                }
            }
        }
        .task(id: "\(sort)-\(unwatchedOnly)-\(showCollections)") { await reload() }
    }

    #if os(tvOS)
    private let minWidth: CGFloat = 230
    private let gap: CGFloat = 50
    #else
    private let minWidth: CGFloat = 110
    private let gap: CGFloat = 14
    #endif

    private func reload() async {
        if library == nil { library = try? await app.libraries().first { $0.id == libraryID } }
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
                                           watch: unwatchedOnly && !showCollections ? .unwatched : nil, type: showCollections ? .collection : nil)
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
