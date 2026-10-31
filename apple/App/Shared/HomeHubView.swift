import MarqueeKit
import SwiftUI

/// A Home row's See all (USER-12): the whole row from the server, as a poster grid. Library
/// titles and films and series on demand together when the row mixes them (those on demand
/// marked VOD, opening their own pages), else the row's library titles.
struct HomeHubView: View {
    @Environment(AppSession.self) private var app
    let id: String
    /// The row's title on Home, shown until the server's arrives.
    let title: String
    @State private var hub: Hub?
    @State private var error: String?
    @State private var loading = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                #if os(tvOS)
                // (The TV shows no navigation title.)
                Text(shownTitle).font(.title2.bold()).padding(.top, 40)
                #endif
                if let error {
                    VStack(alignment: .leading, spacing: 8) {
                        ErrorBanner(message: error)
                        Button("Try Again") { Task { await load() } }.buttonStyle(.bordered)
                    }
                }
                if let hub {
                    let entries = Self.entries(hub)
                    if entries.isEmpty {
                        ContentUnavailableView("Nothing here", systemImage: "film.stack",
                                               description: Text("This row has nothing in it right now."))
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: tileWidth, maximum: tileWidth * 1.4), spacing: gap, alignment: .top)],
                                  alignment: .leading, spacing: gap) {
                            ForEach(entries, id: \.key) {
                                HomeEntryTile(entry: $0, width: tileWidth, watchlist: id == "watchlist", suggestion: isSuggestionRow(id))
                            }
                        }
                        .accessibilityIdentifier("homeHub.grid")
                    }
                } else if loading || error == nil {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 40)
                }
            }
            .padding(.horizontal, sidePadding)
            .padding(.vertical)
        }
        .navigationTitle(shownTitle)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.large)
        .refreshable { await load() }
        #endif
        .task { if hub == nil { await load() } }
        // Not interested on a title on demand, or something changed elsewhere: fetched again.
        .onChange(of: HomeChanges.shared.version) { Task { await load() } }
    }

    private var shownTitle: String { hub?.title ?? title }

    /// The row's titles: its mixed entries when it has them (less those on demand marked Not
    /// interested), else its library items.
    static func entries(_ hub: Hub) -> [HomeEntry] {
        if let mixed = hub.mixedEntries {
            return mixed.filter { e in e.vod.map { !VodSuggestionHides.shared.isHidden($0) } ?? true }
        }
        return hub.items.map { HomeEntry(kind: .item, key: "item:\($0.id)", item: $0) }
    }

    private func load() async {
        loading = true
        defer { loading = false }
        do {
            hub = try await app.hub(id, mixed: true, limit: 100)
            error = nil
        } catch {
            self.error = (error as? MarqueeError)?.message ?? error.localizedDescription
        }
    }

    #if os(tvOS)
    private let tileWidth: CGFloat = 230
    private let gap: CGFloat = 50
    #else
    private let tileWidth: CGFloat = 110
    private let gap: CGFloat = 14
    #endif
}

/// One title in a mixed Home row or its See all: a library title opens its page; one on demand
/// (marked VOD) opens its film's or show's page.
struct HomeEntryTile: View {
    let entry: HomeEntry
    var width: CGFloat = PosterCard.defaultWidth
    /// In Your Watchlist: a title on demand offers Remove from Watchlist, not Not interested.
    var watchlist = false
    /// On a suggestion row: a library title offers Not interested too.
    var suggestion = false

    var body: some View {
        if let item = entry.item {
            PosterCard(item: item, width: width, shape: PosterShape.for(item) == .wide ? .wide : nil, suggestion: suggestion)
        } else if let s = entry.vod {
            VodSuggestionTile(suggestion: s, width: width, mixed: true, watchlist: watchlist)
        }
    }
}
