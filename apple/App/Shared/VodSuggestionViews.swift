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

    var body: some View {
        let shown = VodSuggestionHides.shared.visible(list)
        if !shown.isEmpty {
            ShelfRow(title: title) {
                ForEach(shown, id: \.key) { VodSuggestionTile(suggestion: $0) }
            }
            .accessibilityIdentifier(identifier)
        }
    }
}

/// One suggestion: the poster, the title and the reason; opens the film's or show's page.
struct VodSuggestionTile: View {
    @Environment(AppSession.self) private var app
    let suggestion: VodSuggestion
    var width: CGFloat = VodSuggestionTile.defaultWidth

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
                    .accessibilityLabel(suggestion.title)
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
            .accessibilityLabel(suggestion.title)
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
               progress: suggestion.movie?.progress ?? suggestion.series?.progress, width: width)
    }

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
            Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(isTV ? 1 : 2)
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
