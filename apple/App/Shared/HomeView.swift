import MarqueeKit
import SwiftUI

struct HomeView: View {
    @Environment(AppSession.self) private var app
    @Environment(VideoPresenter.self) private var video
    @State private var hubs: [Hub] = []
    @State private var loaded = false
    @State private var error: String?

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: rowSpacing) {
                if let error { ErrorBanner(message: error).padding(.horizontal, sidePadding) }
                if loaded && hubs.isEmpty {
                    ContentUnavailableView("Nothing here yet", systemImage: "film.stack", description: Text("Libraries are still being scanned, or none have been added."))
                }
                ForEach(hubs, id: \.id) { hub in
                    let wide = hub.id == "continue-watching"
                    ShelfRow(title: hub.title, destination: hub.libraryId.map { Route.library($0) }) {
                        ForEach(hub.items, id: \.id) { item in
                            if wide && item.isPlayableVideo {
                                ContinueCard(item: item)
                            } else {
                                PosterCard(item: item, shape: PosterShape.for(item) == .wide ? .wide : nil)
                            }
                        }
                    }
                }
            }
            .padding(.vertical)
        }
        .navigationTitle("Home")
        .refreshable { await load() }
        .task { await load() }
        .onChange(of: video.request) { if video.request == nil { Task { await load() } } }
    }

    #if os(tvOS)
    private let rowSpacing: CGFloat = 20
    #else
    private let rowSpacing: CGFloat = 28
    #endif

    private func load() async {
        do {
            hubs = try await app.hubs()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }
}

/// Continue Watching: wide art that plays straight away (Plex behaviour).
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
