import MarqueeKit
import SwiftUI

extension Color {
    static let marqueeGold = Color(red: 0.96, green: 0.74, blue: 0.27)
}

enum PosterShape {
    case poster, square, wide
    var aspect: CGFloat { self == .poster ? 2.0 / 3.0 : self == .square ? 1 : 16.0 / 9.0 }
    static func `for`(_ item: Item) -> PosterShape {
        switch item._type {
        case .album, .artist, .track: .square
        case .episode, .video: .wide
        default: .poster
        }
    }
}

/// Artwork with a titled placeholder, plus watch badges.
struct ArtworkView: View {
    @Environment(AppSession.self) private var app
    let item: Item
    var shape: PosterShape = .poster
    var width: CGFloat = 160

    private var artID: Int64? {
        shape == .wide ? (item.images?.thumb ?? item.images?.backdrop ?? item.images?.poster) : item.images?.poster
    }

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            placeholder
            CachedImage(url: app.imageURL(artID, width: Int(width))) { phase in
                if let image = phase.image { image.resizable().scaledToFill().accessibilityIdentifier("artwork.loaded") }
            }
        }
        .aspectRatio(shape.aspect, contentMode: .fit)
        .clipShape(RoundedRectangle(cornerRadius: shape == .square && item._type == .artist ? width : 8, style: .continuous))
        .overlay(alignment: .topTrailing) { badge }
        .overlay(alignment: .bottom) {
            if let p = item.progress {
                GeometryReader { g in
                    ZStack(alignment: .leading) {
                        Rectangle().fill(.black.opacity(0.6))
                        Rectangle().fill(Color.marqueeGold).frame(width: g.size.width * p)
                    }
                }
                .frame(height: 4)
            }
        }
        .opacity(item.available ? 1 : 0.4)
    }

    private var placeholder: some View {
        // A stable hash (String.hashValue changes every launch), so a title keeps its colour.
        let hash = item.title.unicodeScalars.reduce(UInt32(5381)) { ($0 &<< 5) &+ $0 &+ $1.value }
        let hue = Double(hash % 360) / 360
        return LinearGradient(colors: [Color(hue: hue, saturation: 0.35, brightness: 0.3), Color(hue: hue, saturation: 0.3, brightness: 0.12)],
                              startPoint: .topLeading, endPoint: .bottomTrailing)
            .overlay(alignment: .bottomLeading) {
                Text(item.title).font(.caption.weight(.semibold)).foregroundStyle(.white.opacity(0.85)).lineLimit(3).padding(8)
            }
    }

    @ViewBuilder private var badge: some View {
        if item.unwatchedCount > 0 {
            Text("\(item.unwatchedCount)").font(.caption2.bold()).padding(.horizontal, 5).padding(.vertical, 2)
                .background(Color.marqueeGold, in: RoundedRectangle(cornerRadius: 4)).foregroundStyle(.black).padding(6)
        } else if item.isPlayableVideo, item.watched, item.progress == nil {
            Image(systemName: "checkmark.circle.fill").symbolRenderingMode(.palette).foregroundStyle(.black, Color.marqueeGold).padding(6)
        }
    }
}

/// Decoded artwork kept in memory, so scrolling back to a poster shows it at once instead of
/// decoding it again (and flashing the placeholder). Keyed by the image's path and width, so
/// the same artwork is found again after switching address (LAN or Tailscale) or from the
/// token to the image key.
@MainActor
enum ImageMemory {
    static let cache: NSCache<NSString, UIImage> = {
        let c = NSCache<NSString, UIImage>()
        c.totalCostLimit = 96 << 20
        return c
    }()

    static func cost(_ image: UIImage) -> Int {
        Int(image.size.width * image.scale * image.size.height * image.scale * 4)
    }

    /// The cache key: the path and its parameters (the width), without the host or the
    /// credentials (token or image key).
    static func key(_ url: URL) -> NSString {
        guard let c = URLComponents(url: url, resolvingAgainstBaseURL: false), c.path.hasPrefix("/api/") else {
            return url.absoluteString as NSString // not the server's (TMDB posters): the whole URL
        }
        let params = (c.queryItems ?? []).filter { $0.name != "token" && $0.name != "key" }
            .map { "\($0.name)=\($0.value ?? "")" }.sorted().joined(separator: "&")
        return (params.isEmpty ? c.path : "\(c.path)?\(params)") as NSString
    }

    static func image(_ url: URL) -> UIImage? { cache.object(forKey: key(url)) }
}

/// AsyncImage with the in-memory cache above. Loading stops when the view goes away or the
/// URL changes.
struct CachedImage<Content: View>: View {
    let url: URL?
    @ViewBuilder let content: (AsyncImagePhase) -> Content
    @State private var loaded: UIImage?
    @State private var loadedKey: NSString?
    @State private var failedKey: NSString?

    init(url: URL?, @ViewBuilder content: @escaping (AsyncImagePhase) -> Content) {
        self.url = url
        self.content = content
    }

    var body: some View {
        // The task hangs off a view that is always there: while nothing has loaded, the content
        // is empty, and SwiftUI never starts tasks on empty views (the image never loaded).
        Color.clear
            .overlay { content(phase) }
            .task(id: url.map(ImageMemory.key)) { await load() }
    }

    private var phase: AsyncImagePhase {
        guard let url else { return .empty }
        let key = ImageMemory.key(url)
        if let image = ImageMemory.cache.object(forKey: key) { return .success(Image(uiImage: image)) }
        if loadedKey == key, let loaded { return .success(Image(uiImage: loaded)) }
        if failedKey == key { return .failure(URLError(.cannotDecodeContentData)) }
        return .empty
    }

    private func load() async {
        guard let url else { return }
        let key = ImageMemory.key(url)
        guard ImageMemory.cache.object(forKey: key) == nil else { return }
        let request = URLRequest(url: url)
        // A URL carrying the bearer token (before the image key is known) never stays in URLCache.
        let carriesToken = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.contains { $0.name == "token" } == true
        defer { if carriesToken { URLCache.shared.removeCachedResponse(for: request) } }
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            try Task.checkCancellation()
            guard (response as? HTTPURLResponse)?.statusCode ?? 200 == 200 else { throw URLError(.badServerResponse) }
            // Decoded off the main thread, ready to draw.
            guard let image = await Task.detached(priority: .userInitiated, operation: { UIImage(data: data)?.preparingForDisplay() }).value else {
                throw URLError(.cannotDecodeContentData)
            }
            try Task.checkCancellation()
            ImageMemory.cache.setObject(image, forKey: key, cost: ImageMemory.cost(image))
            loaded = image
            loadedKey = key
        } catch {
            if !Task.isCancelled { failedKey = key }
        }
    }
}

extension CachedImage where Content == AnyView {
    /// An image filling its frame, or nothing until it loads.
    init(fill url: URL?) {
        self.init(url: url) { phase in AnyView(phase.image?.resizable().scaledToFill()) }
    }
}

/// Ratings changed in this session, so every row and page showing an item agrees before
/// its list is fetched again.
@MainActor @Observable
final class RatingStore {
    static let shared = RatingStore()
    /// 0 = cleared.
    private var overrides: [Int64: Double] = [:]

    /// The rating to show: this session's change, or the one the server sent.
    func rating(_ id: Int64, _ fallback: Double?) -> Double? {
        if let o = overrides[id] { return o > 0 ? o : nil }
        return fallback
    }

    func set(_ id: Int64, _ rating: Double?) { overrides[id] = rating ?? 0 }

    /// Rates (0–10, half stars; nil clears) at once, and puts the old value back if the server
    /// refuses.
    func rate(_ id: Int64, _ rating: Double?, was old: Double?, app: AppSession) {
        set(id, rating)
        Task {
            do { try await app.rate(id, rating) } catch {
                set(id, old)
                ActionError.shared.message = "Couldn't save the rating: \(error.localizedDescription)"
            }
        }
    }
}

/// A row's own rating that can be changed in place: small stars when rated, a faint outline
/// star when not; tapping opens a menu of ratings (half stars) and No rating.
struct RowRating: View {
    @Environment(AppSession.self) private var app
    let item: Item

    var body: some View {
        let current = RatingStore.shared.rating(item.id, item.userRating)
        Menu {
            Picker("Rating", selection: Binding(get: { current ?? 0 }, set: { choose($0, current) })) {
                ForEach((1...10).reversed(), id: \.self) { v in Text(Self.label(Double(v))).tag(Double(v)) }
                Text("No rating").tag(0.0)
            }
        } label: {
            Group {
                if let r = current, r > 0 {
                    RatingGlyphs(rating: r)
                } else {
                    Image(systemName: "star").font(.system(size: isTV ? 18 : 11, weight: .regular)).foregroundStyle(.tertiary)
                }
            }
            // One width rated or not, so durations beside it line up down a list.
            .frame(width: isTV ? 110 : 50, alignment: .trailing)
            .frame(minHeight: isTV ? 50 : 30)
            .contentShape(Rectangle())
        }
        .menuIndicator(.hidden)
        #if os(iOS)
        .buttonStyle(.plain)
        #endif
        .fixedSize()
        .accessibilityLabel("Rating")
        .accessibilityValue(current.map { Self.label($0) } ?? "Not rated")
        .accessibilityIdentifier("rowRating.\(item.id)")
    }

    private func choose(_ v: Double, _ old: Double?) {
        let next: Double? = v > 0 ? v : nil
        guard next != old else { return }
        RatingStore.shared.rate(item.id, next, was: old, app: app)
    }

    /// "4½ stars", "1 star", "½ star".
    static func label(_ r: Double) -> String {
        let full = Int(r) / 2, half = Int(r) % 2 == 1
        let n = full == 0 ? "½" : half ? "\(full)½" : "\(full)"
        return "\(n) star\(full == 1 && !half ? "" : "s")"
    }
}

/// Five small stars for a rating (0–10, half stars).
struct RatingGlyphs: View {
    let rating: Double

    var body: some View {
        HStack(spacing: 1) {
            ForEach(1...5, id: \.self) { star in
                Image(systemName: rating >= Double(star * 2) ? "star.fill" : rating >= Double(star * 2 - 1) ? "star.leadinghalf.filled" : "star")
            }
        }
        .font(.system(size: isTV ? 16 : 8, weight: .semibold))
        .foregroundStyle(Color.marqueeGold)
        .fixedSize()
    }
}

/// A person's own rating (0–10, half stars) as five small stars, read-only: shown on rows
/// when the track or title is rated.
struct RatingBadge: View {
    let itemID: Int64
    let rating: Double?

    var body: some View {
        if let r = RatingStore.shared.rating(itemID, rating), r > 0 {
            RatingGlyphs(rating: r)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Rated \(RowRating.label(r))")
                .accessibilityIdentifier("ratingBadge")
        }
    }
}

/// A poster with its title and subtitle underneath; opens the item.
struct PosterCard: View {
    let item: Item
    var width: CGFloat = PosterCard.defaultWidth
    var shape: PosterShape?

    #if os(tvOS)
    static let defaultWidth: CGFloat = 250
    #else
    static let defaultWidth: CGFloat = 130
    #endif

    var body: some View {
        #if os(tvOS)
        // TV: the artwork is a focusable card (lift and shine); titles sit underneath.
        VStack(alignment: .leading, spacing: 14) {
            NavigationLink(value: Route.item(item.id)) {
                ArtworkView(item: item, shape: shape ?? .for(item), width: width)
            }
            .buttonStyle(.card)
            .contextMenu { ItemMenuItems(item: item) }
            .accessibilityLabel(item.title)
            titles
            trackRating
        }
        .frame(width: width)
        #else
        VStack(alignment: .leading, spacing: 0) {
            NavigationLink(value: Route.item(item.id)) {
                VStack(alignment: .leading, spacing: 6) {
                    ArtworkView(item: item, shape: shape ?? .for(item), width: width)
                    titles
                }
                .frame(width: width)
            }
            .buttonStyle(.plain)
            .contextMenu { ItemMenuItems(item: item) }
            trackRating
        }
        #endif
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(item._type == .episode ? (item.grandparentTitle ?? item.title) : item.title).font(.subheadline.weight(.medium)).lineLimit(1)
            Text(item._type == .episode ? item.subtitle + " · " + item.title : item.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }
    }

    /// A track's rating under its titles: changeable on iPhone and iPad, shown on the TV.
    @ViewBuilder private var trackRating: some View {
        if item._type == .track {
            #if os(tvOS)
            RatingBadge(itemID: item.id, rating: item.userRating)
            #else
            RowRating(item: item).padding(.top, -6)
            #endif
        }
    }
}

/// Round profile picture or initial.
struct AvatarView: View {
    @Environment(AppSession.self) private var app
    let name: String
    let url: String?
    var size: CGFloat = 64

    var body: some View {
        ZStack {
            Circle().fill(Color.gray.opacity(0.35))
            Text(name.prefix(1).uppercased()).font(.system(size: size * 0.42, weight: .bold))
            if let u = app.absolute(url) {
                CachedImage(url: u) { phase in
                    if let image = phase.image { image.resizable().scaledToFill() }
                }
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
    }
}

/// Four-digit PIN entry.
struct PinPad: View {
    var disabled = false
    let onComplete: (String) -> Void
    @State private var digits = ""

    var body: some View {
        VStack(spacing: 20) {
            HStack(spacing: 14) {
                ForEach(0..<4, id: \.self) { i in
                    Circle().fill(i < digits.count ? Color.primary : Color.secondary.opacity(0.3)).frame(width: 14, height: 14)
                }
            }
            LazyVGrid(columns: Array(repeating: GridItem(.fixed(keySize), spacing: 16), count: 3), spacing: 16) {
                ForEach(["1", "2", "3", "4", "5", "6", "7", "8", "9", "", "0", "⌫"], id: \.self) { k in
                    if k.isEmpty {
                        Color.clear.frame(width: keySize, height: keySize)
                    } else {
                        Button { press(k) } label: {
                            Text(k).font(.title2.weight(.medium)).frame(width: keySize, height: keySize)
                                .background(Circle().fill(Color.secondary.opacity(0.18)))
                        }
                        .buttonStyle(.plain)
                        .disabled(disabled)
                    }
                }
            }
        }
    }

    #if os(tvOS)
    private let keySize: CGFloat = 90
    #else
    private let keySize: CGFloat = 70
    #endif

    private func press(_ k: String) {
        if k == "⌫" { if !digits.isEmpty { digits.removeLast() }; return }
        guard digits.count < 4 else { return }
        digits += k
        if digits.count == 4 {
            let pin = digits
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) { digits = ""; onComplete(pin) }
        }
    }
}

struct ErrorBanner: View {
    let message: String
    var body: some View {
        Label(message, systemImage: "exclamationmark.triangle.fill")
            .font(.callout).foregroundStyle(.red).padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.red.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
    }
}

/// Horizontal row of cards (Home hubs, cast, related).
struct ShelfRow<Content: View>: View {
    let title: String
    var destination: Route?
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let destination, !isTV {
                NavigationLink(value: destination) {
                    HStack(spacing: 4) { Text(title).font(.title3.bold()); Image(systemName: "chevron.right").font(.footnote.bold()).foregroundStyle(.secondary) }
                }
                .buttonStyle(.plain)
                .padding(.horizontal, sidePadding)
            } else {
                Text(title).font(.title3.bold()).padding(.horizontal, sidePadding)
            }
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(alignment: .top, spacing: spacing) { content() }
                    .padding(.horizontal, sidePadding)
                    #if os(tvOS)
                    .padding(.vertical, 30)
                    #endif
            }
            #if os(tvOS)
            .scrollClipDisabled()
            #endif
        }
    }

    #if os(tvOS)
    private let spacing: CGFloat = 40
    #else
    private let spacing: CGFloat = 12
    #endif
}

#if os(tvOS)
let sidePadding: CGFloat = 80
#else
let sidePadding: CGFloat = 16
#endif

#if os(tvOS)
let isTV = true
#else
let isTV = false
#endif

/// A label with its title, or only its icon (the title stays its accessibility label). An
/// icon alone is as tall as a line of text, so icon-only buttons match labelled ones.
struct AdaptiveLabelStyle: LabelStyle {
    var iconOnly: Bool
    func makeBody(configuration: Configuration) -> some View {
        if iconOnly {
            Label {
                configuration.title
            } icon: {
                LineHeight { configuration.icon }
            }
            .labelStyle(.iconOnly)
        } else {
            Label(configuration).labelStyle(.titleAndIcon)
        }
    }
}

/// Content at least as tall as a line of body text (it scales with the text size).
private struct LineHeight<Content: View>: View {
    @ScaledMetric(relativeTo: .body) private var line: CGFloat = 22
    @ViewBuilder var content: Content
    var body: some View { content.frame(minHeight: line) }
}

/// A header's action buttons on one line (item, playlist and collection pages). Every label
/// stays on one line: when the labelled row doesn't fit, the secondary actions become
/// icon-only, then they move to a second line, then everything is icon-only (one line, then two), and only then
/// does the row scroll. The buttons are built once and only their label style and layout
/// change, so their state (a pin, a download, an open alert) survives the change.
struct HeaderActions<Primary: View, Secondary: View>: View {
    /// Only the all-labelled row, never a fallback: for layouts that try something else
    /// (stacking) when it doesn't fit.
    var labelledOnly = false
    @ViewBuilder var primary: () -> Primary
    @ViewBuilder var secondary: () -> Secondary

    /// 0 everything labelled, 1 secondary icon-only, 2 secondary on a second line,
    /// 3 everything icon-only, 4 icon-only on two lines, 5 icon-only and scrolling.
    @State private var mode = 0
    /// The width each mode needed when it was last shown.
    @State private var needed: [Int: CGFloat] = [:]
    @State private var available: CGFloat = 0

    var body: some View {
        if labelledOnly {
            content(mode: 0)
        } else {
            ScrollView(.horizontal, showsIndicators: false) {
                content(mode: mode)
                    .padding(.vertical, 2)
                    .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { w in
                        needed[mode] = w
                        settle()
                    }
            }
            .scrollDisabled(mode < 5)
            .scrollClipDisabled()
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { w in
                available = w
                settle()
            }
        }
    }

    /// Moves to the roomiest mode that fits: one step on when the current one overflows, or
    /// back to an earlier one whose last measured width fits (after a rotation, or a passing
    /// narrow layout during a push).
    private func settle() {
        guard available > 0 else { return }
        if let n = needed[mode], n > available + 0.5 {
            if mode < 5 { mode += 1 }
            return
        }
        if let best = (0..<mode).first(where: { (needed[$0] ?? .infinity) <= available + 0.5 }) { mode = best }
    }

    private func content(mode: Int) -> some View {
        let layout = mode == 2 || mode == 4 ? AnyLayout(VStackLayout(alignment: .leading, spacing: spacing)) : AnyLayout(HStackLayout(spacing: spacing))
        return layout {
            HStack(spacing: spacing) { primary() }
                .labelStyle(AdaptiveLabelStyle(iconOnly: mode >= 3))
            HStack(spacing: spacing) { secondary() }
                .labelStyle(AdaptiveLabelStyle(iconOnly: mode >= 1))
        }
        .lineLimit(1)
        .fixedSize()
    }

    #if os(tvOS)
    private let spacing: CGFloat = 24
    #else
    private let spacing: CGFloat = 10
    #endif
}
