import MarqueeKit
import SwiftUI

// Continue Watching with library titles and films and episodes on demand together (LIVE-6,
// USER-12): the tiles Home's row and the Continue Watching page share, the row's View more tile,
// and the page with every entry.

/// One Continue Watching entry: a library title (as before) or a film or episode on demand.
struct ContinueEntryTile: View {
    let entry: ContinueEntry
    /// Takes it off Continue Watching (its context menu).
    var onRemove: (() -> Void)?
    /// Plays a film or episode on demand where it stopped.
    let playVod: (ContinueEntry) -> Void

    var body: some View {
        if let item = entry.item {
            if item.isPlayableVideo {
                ContinueCard(item: item, onRemove: onRemove)
            } else {
                PosterCard(item: item, shape: PosterShape.for(item) == .wide ? .wide : nil, onRemoveFromContinue: onRemove)
            }
        } else {
            VodContinueCard(entry: entry, onRemove: onRemove, play: { playVod(entry) })
        }
    }
}

/// A film or episode on demand in Continue Watching: wide art like the library's tiles, and a
/// tap resumes it in the VOD player (an episode with Up Next).
struct VodContinueCard: View {
    @Environment(AppSession.self) private var app
    let entry: ContinueEntry
    var onRemove: (() -> Void)?
    let play: () -> Void

    var body: some View {
        Button(action: play) {
            VStack(alignment: .leading, spacing: 6) {
                VodArt(title: entry.title, path: entry.vodArtPath, progress: entry.progress, width: ContinueCard.width, aspect: 9 / 16)
                    .overlay { Image(systemName: "play.circle.fill").font(.system(size: 40)).foregroundStyle(.white.opacity(0.85)).shadow(radius: 6) }
                Text(entry.title).font(.subheadline.weight(.medium)).lineLimit(1)
                Text(entry.vodSubtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            .frame(width: ContinueCard.width)
        }
        #if os(tvOS)
        .buttonStyle(.card)
        #else
        .buttonStyle(.plain)
        #endif
        .accessibilityLabel("\(entry.title), \(entry.vodSubtitle)")
        .accessibilityIdentifier("continue.\(entry.key)")
        .contextMenu {
            // Part-way through: watched or unwatched (the resume point forgotten), either way
            // it leaves Continue Watching (Home and the Continue Watching page reload).
            VodMarkWatchedButtons { watched in
                try await app.setVodWatched(entry, watched)
                HomeChanges.shared.version += 1
            }
            if let onRemove { RemoveFromContinueButton(action: onRemove) }
            if let m = entry.vodMovie {
                NavigationLink(value: Route.vodMovie(m.id)) { Label("Go to Film", systemImage: "film") }
            } else if let e = entry.vodEpisode {
                NavigationLink(value: Route.vodSeries(e.seriesId)) { Label("Go to Show", systemImage: "tv") }
            }
        }
    }
}

/// Mark Watched and Mark Unwatched for a film or episode on demand part-way through (Continue
/// Watching's menus): `save` marks it on the server; a failure shows as an alert.
struct VodMarkWatchedButtons: View {
    let save: @MainActor (Bool) async throws -> Void

    var body: some View {
        Button { ActionError.run { try await save(true) } } label: { Label("Mark Watched", systemImage: "checkmark.circle") }
        Button { ActionError.run { try await save(false) } } label: { Label("Mark Unwatched", systemImage: "circle") }
    }
}

/// The row's last tile when there's more than it shows: opens the Continue Watching page.
struct ContinueViewMoreTile: View {
    let total: Int

    var body: some View {
        NavigationLink(value: Route.continueWatching) {
            VStack(spacing: 6) {
                Text("View more").font(.headline)
                Text("\(total) in all").font(.caption).foregroundStyle(.secondary)
                Image(systemName: "chevron.right").font(.headline)
            }
            .frame(width: width, height: ContinueCard.width * 9 / 16)
            .background(Color.secondary.opacity(0.15), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
        #if os(tvOS)
        .buttonStyle(.card)
        #else
        .buttonStyle(.plain)
        #endif
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("View more")
        .accessibilityAddTraits(.isButton)
        .accessibilityIdentifier("continue.viewMore")
    }

    #if os(tvOS)
    private let width: CGFloat = 260
    #else
    private let width: CGFloat = 150
    #endif
}

/// Plays Continue Watching's films and episodes on demand full screen: `target` is what's
/// playing; `onDismiss` runs when the player closes (the list refreshes then).
struct VodContinuePlayback: ViewModifier {
    @Binding var target: VodPlayTarget?
    let onDismiss: () -> Void

    func body(content: Content) -> some View {
        content.fullScreenCover(item: $target, onDismiss: onDismiss) { t in
            VodPlayerView(target: t)
        }
    }
}

extension AppSession {
    /// What to play for a Continue Watching entry on demand, from its resume point (a film's
    /// details come first: the player shows them).
    func vodPlayTarget(_ entry: ContinueEntry) async throws -> VodPlayTarget? {
        if let m = entry.vodMovie { return VodPlayTarget(movie: try await vodMovie(m.id), startMs: nil) }
        if let e = entry.vodEpisode { return VodPlayTarget(episode: e, startMs: nil) }
        return nil
    }
}

/// Every Continue Watching entry, newest first, a page at a time as it scrolls (Home's row
/// shows the first 20).
struct ContinueWatchingView: View {
    @Environment(AppSession.self) private var app
    @Environment(VideoPresenter.self) private var video
    @State private var entries: [ContinueEntry] = []
    @State private var total: Int?
    @State private var loading = false
    @State private var error: String?
    @State private var playing: VodPlayTarget?

    private static let pageSize = 50

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                #if os(tvOS)
                // (The TV shows no navigation title.)
                Text("Continue Watching").font(.title2.bold()).padding(.top, 40)
                #endif
                if let error {
                    VStack(alignment: .leading, spacing: 8) {
                        ErrorBanner(message: error)
                        Button("Try Again") { Task { await reload() } }.buttonStyle(.bordered)
                    }
                }
                if entries.isEmpty {
                    if loading { ProgressView().frame(maxWidth: .infinity) }
                    else if error == nil && total != nil {
                        ContentUnavailableView("Nothing to continue", systemImage: "play.rectangle", description: Text("Movies and episodes you've started show here."))
                    }
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: ContinueCard.width, maximum: ContinueCard.width), spacing: spacing, alignment: .top)],
                              alignment: .center, spacing: spacing) {
                        ForEach(entries, id: \.key) { e in
                            ContinueEntryTile(entry: e, onRemove: { remove(e) }, playVod: play)
                                .onAppear { if e.key == entries.last?.key { Task { await loadMore() } } }
                        }
                    }
                    // Centred: on a phone one fixed-width column fits, and it shouldn't hug the left.
                    .frame(maxWidth: .infinity)
                    .accessibilityIdentifier("continue.grid")
                }
            }
            .padding(.horizontal, sidePadding)
            .padding(.vertical)
        }
        .navigationTitle("Continue Watching")
        .refreshable { await reload() }
        .task { if total == nil { await reload() } }
        // Back from a library title's player, or an on demand one's: where they are changed.
        .onChange(of: video.request) { if video.request == nil { Task { await reload() } } }
        // Marked watched or unwatched (or removed) from a tile's menu: it's left the list.
        .onChange(of: HomeChanges.shared.version) { Task { await reload() } }
        .modifier(VodContinuePlayback(target: $playing, onDismiss: { Task { await reload() } }))
    }

    #if os(tvOS)
    private let spacing: CGFloat = 48
    #else
    private let spacing: CGFloat = 16
    #endif

    private func reload() async {
        loading = true
        defer { loading = false }
        do {
            let page = try await app.continueWatching(offset: 0, limit: Self.pageSize)
            entries = page.entries
            total = page.total
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func loadMore() async {
        guard !loading, let total, entries.count < total else { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await app.continueWatching(offset: entries.count, limit: Self.pageSize)
            let have = Set(entries.map(\.key))
            entries += page.entries.filter { !have.contains($0.key) }
            self.total = page.total
        } catch {
            ActionError.shared.message = error.localizedDescription
        }
    }

    private func play(_ entry: ContinueEntry) {
        ActionError.run { playing = try await app.vodPlayTarget(entry) }
    }

    /// Gone at once (an episode takes the rest of its show with it), then confirmed by the server.
    private func remove(_ entry: ContinueEntry) {
        let before = (entries, total)
        let gone = entries.filter { entry.removes($0) }.count
        entries.removeAll { entry.removes($0) }
        total = total.map { max(0, $0 - gone) }
        ActionError.run {
            do {
                if let item = entry.item {
                    try await app.removeFromContinueWatching(item.id)
                } else {
                    try await app.removeVodFromContinueWatching(entry)
                }
            } catch {
                (entries, total) = before
                throw error
            }
            HomeChanges.shared.version += 1
        }
    }
}
