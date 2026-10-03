import MarqueeKit
import SwiftUI

struct SearchView: View {
    @Environment(AppSession.self) private var app
    @State private var query = ""
    @State private var results: SearchResults?
    /// Search the library, or describe what you want to watch (Muse, USER-15).
    @State private var mode: Mode = .library
    @State private var muse = VideoMuse()

    enum Mode: String, CaseIterable {
        case library = "Library", muse = "Muse"
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 24) {
                Picker("Search mode", selection: $mode) {
                    ForEach(Mode.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, sidePadding)
                if mode == .muse {
                    if muse.result == nil && !muse.busy {
                        Text("Describe what you want to watch, or try one of these:").font(.callout).foregroundStyle(.secondary)
                            .padding(.horizontal, sidePadding)
                    }
                    VideoMuseChips(muse: muse)
                    if muse.busy { ProgressView().frame(maxWidth: .infinity) }
                    VideoMuseResults(muse: muse)
                } else {
                    libraryResults
                }
            }
            .padding(.vertical)
        }
        .navigationTitle("Search")
        .searchable(text: $query, prompt: mode == .muse ? "Describe what you want to watch" : "Movies, shows, music")
        .onSubmit(of: .search) {
            guard mode == .muse else { return }
            muse.prompt = query
            muse.run(app, library: nil)
        }
        .onChange(of: muse.prompt) { if mode == .muse, query != muse.prompt { query = muse.prompt } }
        .task(id: "\(mode)|\(query)") {
            guard mode == .library else { return }
            let q = query.trimmingCharacters(in: .whitespaces)
            guard !q.isEmpty else { results = nil; return }
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            results = try? await app.search(q)
        }
    }

    @ViewBuilder private var libraryResults: some View {
        ForEach(results?.groups ?? [], id: \._type) { g in
            ShelfRow(title: title(g._type)) {
                ForEach(g.items, id: \.id) { PosterCard(item: $0) }
            }
        }
        if let r = results, r.groups.isEmpty, !query.isEmpty {
            ContentUnavailableView.search(text: query)
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
        case .collection: "Collections"
        }
    }
}
