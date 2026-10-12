import MarqueeKit
import SwiftUI

/// The search text.
@MainActor @Observable
final class SearchText {
    static let shared = SearchText()
    var query = ""
}

struct SearchView: View {
    @Environment(AppSession.self) private var app
    @Bindable private var search = SearchText.shared
    private var query: String {
        get { search.query }
        nonmutating set { search.query = newValue }
    }
    @State private var results: SearchResults?
    @State private var error: String?
    /// Search the library, or describe what you want to watch (Muse, USER-15).
    @State private var mode: Mode = .library
    @State private var muse = VideoMuse()

    enum Mode: String, CaseIterable {
        case library = "Library", muse = "Muse"
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 24) {
                #if os(tvOS)
                // Our own field on the TV: the system search bar sits under the sidebar's
                // collapsed button at the top left and can't be moved aside.
                HStack(spacing: 16) {
                    Image(systemName: "magnifyingglass").font(.title2).foregroundStyle(.secondary)
                    TextField(mode == .muse ? "Describe what you want to watch" : "Movies, shows, music", text: $search.query)
                        .onSubmit {
                            guard mode == .muse else { return }
                            muse.prompt = query
                            muse.run(app, library: nil)
                        }
                        .accessibilityIdentifier("search.field")
                }
                .padding(.leading, 150)
                .padding(.trailing, sidePadding)
                #endif
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
        #if !os(tvOS)
        // On the TV no title: it sat under the keyboard, and the sidebar names the page.
        .navigationTitle("Search")
        #endif
        #if !os(tvOS)
        .searchable(text: $search.query, prompt: mode == .muse ? "Describe what you want to watch" : "Movies, shows, music")
        #endif
        .onSubmit(of: .search) {
            guard mode == .muse else { return }
            muse.prompt = query
            muse.run(app, library: nil)
        }
        .onChange(of: muse.prompt) { if mode == .muse, query != muse.prompt { query = muse.prompt } }
        .task(id: "\(mode)|\(query)") {
            guard mode == .library else { return }
            let q = query.trimmingCharacters(in: .whitespaces)
            guard !q.isEmpty else { results = nil; error = nil; return }
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            do {
                let r = try await app.search(q)
                guard !Task.isCancelled else { return }
                results = r
                error = nil
            } catch {
                // A search overtaken by the next keystroke keeps the results on screen.
                if !Task.isCancelled, !(error is CancellationError) { self.error = error.localizedDescription }
            }
        }
    }

    @ViewBuilder private var libraryResults: some View {
        if let error {
            ErrorBanner(message: error).padding(.horizontal, sidePadding).accessibilityIdentifier("searchError")
        }
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
