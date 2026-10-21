import MarqueeKit
import SwiftUI

// Suggestions from the films and series on demand (LIVE-6, USER-16): Home's Watch on VOD row,
// Also on VOD on a movie's or show's page, the VOD pages' Recommended for you and Muse's On VOD.
// Each tile opens the film's or show's page and says why it's suggested; its context menu has
// Not interested and Go to film / Go to show.

/// Titles this viewer said Not interested to in this session, so every row drops them at once.
@MainActor @Observable
final class VodSuggestionHides {
    static let shared = VodSuggestionHides()
    private(set) var keys: Set<String> = []

    static func key(_ s: VodSuggestion) -> String { "\(s.kind.rawValue):\(s.vodId ?? 0)" }

    func isHidden(_ s: VodSuggestion) -> Bool { keys.contains(Self.key(s)) }

    /// Gone from every row at once, back if the server says no; Home reloads afterwards.
    func hide(_ s: VodSuggestion, app: AppSession) {
        let key = Self.key(s)
        keys.insert(key)
        ActionError.run {
            do {
                try await app.hideVodSuggestion(s)
            } catch {
                self.keys.remove(key)
                throw error
            }
            HomeChanges.shared.version += 1
        }
    }

    func visible(_ list: [VodSuggestion]) -> [VodSuggestion] { list.filter { !isHidden($0) } }

    /// Another person signed in: Not interested was someone else's.
    func reset() { keys = [] }
}

/// A row of suggestions (nothing when empty, or once every title in it is hidden).
struct VodSuggestionRow: View {
    let title: String
    let list: [VodSuggestion]
    var identifier = "vod.suggestions"
    /// The recommendations page: See All in the header (a tile at the row's end on the TV).
    var seeAll: Route?

    var body: some View {
        let shown = VodSuggestionHides.shared.visible(list)
        if !shown.isEmpty {
            ShelfRow(title: title, seeAll: seeAll) {
                ForEach(shown, id: \.key) { VodSuggestionTile(suggestion: $0) }
                #if os(tvOS)
                if let seeAll { VodSeeAllTile(route: seeAll) }
                #endif
            }
            .accessibilityIdentifier(identifier)
        }
    }
}

#if os(tvOS)
/// The TV's See All: a poster-sized tile at the end of Recommended for you.
struct VodSeeAllTile: View {
    let route: Route

    var body: some View {
        NavigationLink(value: route) {
            VStack(spacing: 10) {
                Image(systemName: "square.grid.2x2").font(.title2)
                Text("See All").font(.headline)
            }
            .frame(width: VodSuggestionTile.defaultWidth, height: VodSuggestionTile.defaultWidth * 1.5)
            .background(Color.secondary.opacity(0.15), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
        .buttonStyle(.card)
        .accessibilityLabel("See All")
        .accessibilityIdentifier("shelf.seeAll")
    }
}
#endif

/// Recommended for you's See All (USER-16): rows of films or series on demand, each with its
/// own angle, from the viewer's own taste; Movies and Shows switch between the two.
struct VodRecommendedView: View {
    @Environment(AppSession.self) private var app
    @State private var series: Bool
    @State private var rows: [VodRecommendationRow]?
    @State private var error: String?

    init(series: Bool) {
        _series = State(initialValue: series)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: isTV ? 30 : 24) {
                VStack(alignment: .leading, spacing: 10) {
                    Picker("Movies or shows", selection: $series) {
                        Text("Movies").tag(false)
                        Text("Shows").tag(true)
                    }
                    .pickerStyle(.segmented)
                    .frame(maxWidth: isTV ? 500 : 320)
                    .accessibilityIdentifier("vodRecommended.kind")
                    Text("Picked from your favourites, ratings and what you watch. Nothing here comes from what other people watch.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                .padding(.horizontal, sidePadding)
                if let error {
                    ErrorBanner(message: error).padding(.horizontal, sidePadding)
                } else if let rows {
                    if rows.allSatisfy({ VodSuggestionHides.shared.visible($0.entries).isEmpty }) {
                        Text("Nothing to recommend yet. Watch, rate or heart a few titles and come back.")
                            .foregroundStyle(.secondary).padding(.horizontal, sidePadding)
                            .accessibilityIdentifier("vodRecommended.empty")
                    }
                    ForEach(rows, id: \.id) { r in
                        VodSuggestionRow(title: r.title, list: r.entries, identifier: "vodRecommended.row")
                    }
                } else {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 40)
                }
            }
            .padding(.vertical)
        }
        .navigationTitle(series ? "Recommended Shows" : "Recommended Movies")
        #if os(iOS)
        .refreshable { await load() }
        #endif
        .task(id: series) {
            rows = nil
            await load()
        }
    }

    private func load() async {
        do {
            rows = try await app.vodRecommendations(series: series)
            error = nil
        } catch {
            self.error = (error as? MarqueeError)?.message ?? error.localizedDescription
        }
    }
}

/// One suggestion: the poster, the title and the reason; opens the film's or show's page.
struct VodSuggestionTile: View {
    @Environment(AppSession.self) private var app
    let suggestion: VodSuggestion
    var width: CGFloat = VodSuggestionTile.defaultWidth
    /// In a Home row beside library titles: a VOD badge on the poster, and one line under it.
    var mixed = false

    #if os(tvOS)
    static let defaultWidth: CGFloat = 220
    #else
    static let defaultWidth: CGFloat = 120
    #endif

    private var route: Route? {
        if let m = suggestion.movie { return .vodMovie(m.id) }
        if let s = suggestion.series { return .vodSeries(s.id) }
        return nil
    }

    var body: some View {
        if let route {
            #if os(tvOS)
            VStack(alignment: .leading, spacing: 14) {
                NavigationLink(value: route) { poster }
                    .buttonStyle(.card)
                    .accessibilityLabel(label)
                    .accessibilityValue(subtitle)
                    .accessibilityIdentifier("vod.suggestion")
                    .contextMenu { menu(route) }
                titles
            }
            .frame(width: width)
            #else
            NavigationLink(value: route) {
                VStack(alignment: .leading, spacing: 6) {
                    poster
                    titles
                }
                .frame(width: width)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(label)
            .accessibilityValue(subtitle)
            .accessibilityAddTraits(.isButton)
            .accessibilityIdentifier("vod.suggestion")
            .contextMenu { menu(route) }
            #endif
        }
    }

    @ViewBuilder private func menu(_ route: Route) -> some View {
        Button(role: .destructive) { VodSuggestionHides.shared.hide(suggestion, app: app) } label: {
            Label("Not interested", systemImage: "eye.slash")
        }
        .accessibilityIdentifier("vod.notInterested")
        NavigationLink(value: route) {
            Label(suggestion.movie != nil ? "Go to film" : "Go to show", systemImage: suggestion.movie != nil ? "film" : "tv")
        }
    }

    private var poster: some View {
        VodArt(title: suggestion.title, path: suggestion.posterUrl,
               watched: suggestion.movie?.watched ?? suggestion.series?.watched ?? false,
               progress: suggestion.movie?.progress ?? suggestion.series?.progress, width: width, vodBadge: mixed)
    }

    /// The title, said to be on demand where the poster's badge shows it.
    private var label: String { mixed ? "\(suggestion.title), VOD" : suggestion.title }

    /// Why it's suggested, else the year and length (or seasons).
    private var subtitle: String {
        if let r = suggestion.reason, !r.isEmpty { return r }
        let facts = suggestion.movie?.metaLine ?? suggestion.series?.metaLine ?? ""
        return facts.isEmpty ? "VOD" : facts
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 4) {
                Image(systemName: suggestion.movie != nil ? "film" : "tv").font(.caption2).foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                Text(suggestion.title).font(.subheadline.weight(.medium)).lineLimit(1)
            }
            Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(isTV || mixed ? 1 : 2)
        }
    }
}

/// Watch now on VOD: the same title on demand (its film's or show's page), on Discover.
struct WatchOnVodLink: View {
    let ref: VodRef
    /// "VOD" on a card; the full label elsewhere.
    var compact = false

    var body: some View {
        NavigationLink(value: ref.kind == .movie ? Route.vodMovie(ref.id) : Route.vodSeries(ref.id)) {
            Label(compact ? "VOD" : "Watch now on VOD", systemImage: "play.fill")
                .font(compact ? .caption2.weight(.semibold) : .subheadline.weight(.semibold))
                #if os(iOS)
                .padding(.horizontal, compact ? 8 : 14).padding(.vertical, compact ? 4 : 8)
                .background(Color.marqueeGold, in: RoundedRectangle(cornerRadius: 6))
                .foregroundStyle(.black)
                #endif
        }
        #if os(iOS)
        .buttonStyle(.plain)
        #endif
        .accessibilityLabel("Watch now on VOD")
        .accessibilityIdentifier("discover.watchOnVod")
    }
}
