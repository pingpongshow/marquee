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
    @State private var error: String?

    private static let page = 120

    var body: some View {
        ScrollView {
            if let error { ErrorBanner(message: error).padding() }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: minWidth, maximum: minWidth * 1.4), spacing: gap, alignment: .top)], spacing: gap) {
                ForEach(Array(items.enumerated()), id: \.element.id) { i, item in
                    PosterCard(item: item, width: minWidth)
                        .onAppear { if i >= items.count - 30 { Task { await loadMore() } } }
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
        .task(id: "\(sort)-\(unwatchedOnly)") { await reload() }
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
        items = []
        total = 0
        await loadMore()
    }

    private func loadMore() async {
        guard !loading, items.isEmpty || items.count < total else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await app.items(library: libraryID, sort: sort, offset: items.count, limit: Self.page, watch: unwatchedOnly ? .unwatched : nil)
            items += page.items.filter { new in !items.contains { $0.id == new.id } }
            total = page.total
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}
