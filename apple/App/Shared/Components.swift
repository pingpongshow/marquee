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
/// decoding it again (and flashing the placeholder). The bytes also stay in URLCache.
@MainActor
enum ImageMemory {
    static let cache: NSCache<NSURL, UIImage> = {
        let c = NSCache<NSURL, UIImage>()
        c.totalCostLimit = 96 << 20
        return c
    }()

    static func cost(_ image: UIImage) -> Int {
        Int(image.size.width * image.scale * image.size.height * image.scale * 4)
    }
}

/// AsyncImage with the in-memory cache above. Loading stops when the view goes away or the
/// URL changes.
struct CachedImage<Content: View>: View {
    let url: URL?
    @ViewBuilder let content: (AsyncImagePhase) -> Content
    @State private var loaded: UIImage?
    @State private var loadedURL: URL?
    @State private var failedURL: URL?

    var body: some View {
        // The task hangs off a view that is always there: while nothing has loaded, the content
        // is empty, and SwiftUI never starts tasks on empty views (the image never loaded).
        Color.clear
            .overlay { content(phase) }
            .task(id: url) { await load() }
    }

    private var phase: AsyncImagePhase {
        guard let url else { return .empty }
        if let image = ImageMemory.cache.object(forKey: url as NSURL) { return .success(Image(uiImage: image)) }
        if loadedURL == url, let loaded { return .success(Image(uiImage: loaded)) }
        if failedURL == url { return .failure(URLError(.cannotDecodeContentData)) }
        return .empty
    }

    private func load() async {
        guard let url, ImageMemory.cache.object(forKey: url as NSURL) == nil else { return }
        do {
            let (data, response) = try await URLSession.shared.data(from: url)
            try Task.checkCancellation()
            guard (response as? HTTPURLResponse)?.statusCode ?? 200 == 200 else { throw URLError(.badServerResponse) }
            // Decoded off the main thread, ready to draw.
            guard let image = await Task.detached(priority: .userInitiated, operation: { UIImage(data: data)?.preparingForDisplay() }).value else {
                throw URLError(.cannotDecodeContentData)
            }
            try Task.checkCancellation()
            ImageMemory.cache.setObject(image, forKey: url as NSURL, cost: ImageMemory.cost(image))
            loaded = image
            loadedURL = url
        } catch {
            if !Task.isCancelled { failedURL = url }
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
        }
        .frame(width: width)
        #else
        NavigationLink(value: Route.item(item.id)) {
            VStack(alignment: .leading, spacing: 6) {
                ArtworkView(item: item, shape: shape ?? .for(item), width: width)
                titles
            }
            .frame(width: width)
        }
        .buttonStyle(.plain)
        .contextMenu { ItemMenuItems(item: item) }
        #endif
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(item._type == .episode ? (item.grandparentTitle ?? item.title) : item.title).font(.subheadline.weight(.medium)).lineLimit(1)
            Text(item._type == .episode ? item.subtitle + " · " + item.title : item.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
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

/// A label with its title, or only its icon (the title stays its accessibility label).
struct AdaptiveLabelStyle: LabelStyle {
    var iconOnly: Bool
    func makeBody(configuration: Configuration) -> some View {
        if iconOnly {
            Label(configuration).labelStyle(.iconOnly)
        } else {
            Label(configuration).labelStyle(.titleAndIcon)
        }
    }
}

/// A header's action buttons on one line (item, playlist and collection pages). Every label
/// stays on one line: when the labelled row doesn't fit, the secondary actions become
/// icon-only, then they move to a second line, then everything is icon-only, and only then
/// does the row scroll.
struct HeaderActions<Primary: View, Secondary: View>: View {
    @ViewBuilder var primary: () -> Primary
    @ViewBuilder var secondary: () -> Secondary

    var body: some View {
        ViewThatFits(in: .horizontal) {
            row(primaryIcons: false, secondaryIcons: false)
            row(primaryIcons: false, secondaryIcons: true)
            // Play and Shuffle keep their words on one line; the rest go icon-only beneath.
            VStack(alignment: .leading, spacing: spacing) {
                group(primary().labelStyle(AdaptiveLabelStyle(iconOnly: false)))
                group(secondary().labelStyle(AdaptiveLabelStyle(iconOnly: true)))
            }
            row(primaryIcons: true, secondaryIcons: true)
            ScrollView(.horizontal, showsIndicators: false) {
                row(primaryIcons: true, secondaryIcons: true).padding(.vertical, 2)
            }
            #if os(iOS)
            .scrollClipDisabled()
            #endif
        }
    }

    private func group(_ content: some View) -> some View {
        HStack(spacing: spacing) { content }.lineLimit(1).fixedSize()
    }

    private func row(primaryIcons: Bool, secondaryIcons: Bool) -> some View {
        HStack(spacing: spacing) {
            primary().labelStyle(AdaptiveLabelStyle(iconOnly: primaryIcons))
            secondary().labelStyle(AdaptiveLabelStyle(iconOnly: secondaryIcons))
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
