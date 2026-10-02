import MarqueeKit
import SwiftUI

struct SearchView: View {
    @Environment(AppSession.self) private var app
    @State private var query = ""
    @State private var results: SearchResults?

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 24) {
                ForEach(results?.groups ?? [], id: \._type) { g in
                    ShelfRow(title: title(g._type)) {
                        ForEach(g.items, id: \.id) { PosterCard(item: $0) }
                    }
                }
                if let r = results, r.groups.isEmpty, !query.isEmpty {
                    ContentUnavailableView.search(text: query)
                }
            }
            .padding(.vertical)
        }
        .navigationTitle("Search")
        .searchable(text: $query, prompt: "Movies, shows, music")
        .task(id: query) {
            let q = query.trimmingCharacters(in: .whitespaces)
            guard !q.isEmpty else { results = nil; return }
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            results = try? await app.search(q)
        }
    }

    private func title(_ t: Schemas.ItemType) -> String {
        switch t {
        case .movie: "Movies"
        case .show: "Shows"
        case .season: "Seasons"
        case .episode: "Episodes"
        case .artist: "Artists"
        case .album: "Albums"
        case .track: "Tracks"
        case .video: "Videos"
        }
    }
}
