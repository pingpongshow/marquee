import SwiftUI

/// One title on the Up Next screen: a library episode or video, or an episode on demand.
struct UpNextEntry {
    /// The show's title (a film's or video's own title when it isn't an episode).
    var heading: String
    /// The episode's title; nil when `heading` is already the title.
    var title: String?
    /// "SEASON 5 - EPISODE 4", or a year.
    var detail: String
    var overview: String?
    /// A 16:9 still.
    var image: URL?

    /// "SEASON 5 - EPISODE 4" from a season's number or name ("Specials") and an episode's.
    static func episodeLine(season: Int?, seasonTitle: String?, episode: Int?) -> String {
        var s = season
        if s == nil, let t = seasonTitle, t.hasPrefix("Season "), let n = Int(t.dropFirst("Season ".count)) { s = n }
        let first = s.map { "Season \($0)" } ?? seasonTitle
        return [first, episode.map { "Episode \($0)" }].compactMap { $0 }.joined(separator: " - ").uppercased()
    }
}

/// What the Up Next screen shows: the title that just ended, the next one, and the art behind.
struct UpNextContent {
    /// The countdown, in seconds, from the end of one to the start of the next.
    static let countdown = 10

    var previous: UpNextEntry?
    var next: UpNextEntry
    var background: URL?
}

/// Up Next, after an episode (or a playlist entry) has played to its end: full screen over the
/// show's art. The one that ended on the left (select it to play it again), the next one in the
/// middle with a countdown that plays it at zero (select it to play now), and the next one's
/// details on the right. Close, Menu on the Siri Remote or Esc leaves the player. On a narrow
/// screen (an iPhone held upright) the next one comes first, its details under it, then the one
/// that ended.
struct UpNextScreen<Accessory: View>: View {
    let content: UpNextContent
    /// Seconds left.
    let seconds: Int
    /// Waiting while the person answers the rating request.
    var paused = false
    let playNext: () -> Void
    let replay: () -> Void
    let cancel: () -> Void
    /// Shown below the cards (the rating request).
    @ViewBuilder var accessory: () -> Accessory

    #if os(tvOS)
    @Namespace private var focusScope
    #endif

    var body: some View {
        GeometryReader { g in
            // An iPhone held upright: one above the other.
            let narrow = g.size.width < 600
            ZStack {
                background
                VStack(alignment: .leading, spacing: 0) {
                    topBar
                    if narrow {
                        ScrollView { stacked(width: g.size.width).padding(.vertical, 12) }
                            .scrollBounceBehavior(.basedOnSize)
                    } else {
                        Spacer(minLength: 0)
                        wide(width: g.size.width)
                        Spacer(minLength: 0)
                    }
                    HStack {
                        Spacer(minLength: 0)
                        accessory()
                    }
                }
                .padding(.horizontal, isTV ? 80 : 24)
                .padding(.vertical, isTV ? 50 : 12)
            }
            .foregroundStyle(.white)
        }
        .background(Color.black.ignoresSafeArea())
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isModal) // the player behind it is out of reach
        .accessibilityIdentifier("upNext.screen")
        #if os(tvOS)
        .focusScope(focusScope)
        .onExitCommand(perform: cancel)
        #endif
    }

    // MARK: - Layouts

    /// Side by side: the one that ended, the next (larger), and its details.
    private func wide(width: CGFloat) -> some View {
        let inner = width - (isTV ? 160 : 48)
        let spacing: CGFloat = isTV ? 60 : 28
        return HStack(alignment: .center, spacing: spacing) {
            if let p = content.previous {
                previousCard(p, width: inner * 0.2)
            }
            nextCard(width: inner * 0.36)
            details(content.next, large: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// One above the other, for an iPhone held upright.
    private func stacked(width: CGFloat) -> some View {
        let cardWidth = min(width - 48, 420)
        return VStack(alignment: .leading, spacing: 22) {
            nextCard(width: cardWidth)
            details(content.next, large: false)
            if let p = content.previous {
                previousCard(p, width: min(cardWidth * 0.55, 220))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: - Pieces

    private var background: some View {
        ZStack {
            Color.black
            CachedImage(fill: content.background ?? content.next.image)
                .blur(radius: isTV ? 6 : 4)
                .opacity(0.55)
            // Darker towards the edges, so the text stays legible over any art.
            LinearGradient(colors: [.black.opacity(0.9), .black.opacity(0.45), .black.opacity(0.85)],
                           startPoint: .leading, endPoint: .trailing)
            LinearGradient(colors: [.black.opacity(0.6), .clear, .black.opacity(0.8)],
                           startPoint: .top, endPoint: .bottom)
        }
        .clipped()
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }

    private var topBar: some View {
        HStack {
            #if os(tvOS)
            // Menu on the remote does the same; this one can be reached with focus.
            Button("Cancel", systemImage: "xmark", action: cancel)
                .accessibilityIdentifier("upNext.cancel")
            #else
            Button(action: cancel) {
                Image(systemName: "xmark").font(.headline)
                    .frame(width: 44, height: 44)
                    .background(.ultraThinMaterial, in: Circle())
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .keyboardShortcut(.cancelAction)
            .accessibilityLabel("Close player")
            .accessibilityIdentifier("upNext.cancel")
            #endif
            Spacer()
        }
    }

    /// The one that ended, smaller, with a replay mark; selecting it plays it again.
    private func previousCard(_ p: UpNextEntry, width: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: isTV ? 16 : 8) {
            caption("Previous")
            Button(action: replay) {
                #if os(tvOS)
                still(p.image, width: width) { replayMark }
                #else
                VStack(alignment: .leading, spacing: 8) {
                    still(p.image, width: width) { replayMark }
                    titles(p)
                }
                #endif
            }
            #if os(tvOS)
            .buttonStyle(.card)
            #else
            .buttonStyle(.plain)
            #endif
            .accessibilityLabel("Play again: \(spoken(p))")
            .accessibilityIdentifier("upNext.previous")
            #if os(tvOS)
            titles(p)
            #endif
        }
        .frame(width: width, alignment: .leading)
        .opacity(0.9)
    }

    /// The next one, larger, with the countdown on it; selecting it plays it now.
    private func nextCard(width: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: isTV ? 16 : 8) {
            caption("Playing next")
            Button(action: playNext) {
                still(content.next.image, width: width) {
                    CountdownRing(seconds: seconds, total: UpNextContent.countdown, paused: paused, size: min(width * 0.32, isTV ? 150 : 96))
                }
            }
            #if os(tvOS)
            .buttonStyle(.card)
            .prefersDefaultFocus(true, in: focusScope)
            #else
            .buttonStyle(.plain)
            #endif
            .accessibilityLabel("Play next: \(spoken(content.next))")
            .accessibilityValue(paused ? "Waiting" : "Plays in \(seconds) seconds")
            .accessibilityIdentifier("upNext.next")
        }
        .frame(width: width, alignment: .leading)
    }

    /// The next one's show, title, season and episode, and overview.
    private func details(_ e: UpNextEntry, large: Bool) -> some View {
        VStack(alignment: .leading, spacing: isTV ? 14 : 8) {
            Text(e.heading)
                .font(large ? (isTV ? .system(size: 52, weight: .bold) : .largeTitle.bold()) : .title.bold())
                .lineLimit(2).minimumScaleFactor(0.7)
                .accessibilityIdentifier("upNext.heading")
            if let t = e.title {
                Text(t).font(.title3.weight(.semibold)).lineLimit(2)
                    .accessibilityIdentifier("upNext.title")
            }
            if !e.detail.isEmpty {
                Text(e.detail).font(isTV ? .callout.weight(.semibold) : .caption.weight(.semibold))
                    .foregroundStyle(.white.opacity(0.7)).kerning(0.6)
            }
            if let o = e.overview, !o.isEmpty {
                Text(o).font(isTV ? .callout : .subheadline).foregroundStyle(.white.opacity(0.8))
                    .lineLimit(isTV ? 5 : 4)
                    .padding(.top, 4)
            }
        }
        .shadow(color: .black.opacity(0.5), radius: 6)
    }

    private func caption(_ text: String) -> some View {
        Text(text.uppercased()).font(isTV ? .callout.weight(.bold) : .caption.weight(.bold))
            .kerning(1).foregroundStyle(.white.opacity(0.7))
            .accessibilityHidden(true)
    }

    private func titles(_ e: UpNextEntry) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(e.heading).font(isTV ? .callout.weight(.semibold) : .subheadline.weight(.semibold)).lineLimit(1)
            if let t = e.title { Text(t).font(.caption).foregroundStyle(.white.opacity(0.8)).lineLimit(1) }
            if !e.detail.isEmpty {
                Text(e.detail).font(.caption2.weight(.semibold))
                    .foregroundStyle(.white.opacity(0.6)).lineLimit(1)
            }
        }
    }

    /// A 16:9 still (a dark placeholder until it loads) with something centred on it.
    private func still<Overlay: View>(_ url: URL?, width: CGFloat, @ViewBuilder overlay: () -> Overlay) -> some View {
        ZStack {
            LinearGradient(colors: [Color(white: 0.2), Color(white: 0.08)], startPoint: .topLeading, endPoint: .bottomTrailing)
            CachedImage(fill: url)
            Color.black.opacity(0.3)
            overlay()
        }
        .frame(width: width, height: width * 9 / 16)
        .clipShape(RoundedRectangle(cornerRadius: isTV ? 14 : 10, style: .continuous))
        #if os(iOS)
        .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(.white.opacity(0.12)))
        .shadow(color: .black.opacity(0.45), radius: 12, y: 6)
        #endif
    }

    private var replayMark: some View {
        Image(systemName: "arrow.counterclockwise")
            .font(.system(size: isTV ? 40 : 22, weight: .semibold))
            .padding(isTV ? 18 : 10)
            .background(.ultraThinMaterial, in: Circle())
            .foregroundStyle(.white)
    }

    private func spoken(_ e: UpNextEntry) -> String {
        [e.heading, e.title, e.detail.isEmpty ? nil : e.detail.capitalized].compactMap { $0 }.joined(separator: ", ")
    }
}

extension UpNextScreen where Accessory == EmptyView {
    init(content: UpNextContent, seconds: Int, paused: Bool = false,
         playNext: @escaping () -> Void, replay: @escaping () -> Void, cancel: @escaping () -> Void) {
        self.init(content: content, seconds: seconds, paused: paused, playNext: playNext, replay: replay, cancel: cancel) { EmptyView() }
    }
}

/// The seconds left, large, inside a ring that empties as they run out.
private struct CountdownRing: View {
    let seconds: Int
    let total: Int
    let paused: Bool
    let size: CGFloat

    var body: some View {
        let line = max(4, size * 0.07)
        ZStack {
            Circle().fill(.black.opacity(0.45))
            Circle().stroke(.white.opacity(0.25), lineWidth: line)
            Circle()
                .trim(from: 0, to: total > 0 ? CGFloat(seconds) / CGFloat(total) : 0)
                .stroke(Color.marqueeGold, style: StrokeStyle(lineWidth: line, lineCap: .round))
                .rotationEffect(.degrees(-90))
                .animation(.linear(duration: 1), value: seconds)
            if paused {
                Image(systemName: "pause.fill").font(.system(size: size * 0.34, weight: .bold))
            } else {
                Text("\(seconds)")
                    .font(.system(size: size * 0.42, weight: .bold, design: .rounded).monospacedDigit())
                    .contentTransition(.numericText(countsDown: true))
                    .animation(.default, value: seconds)
                    .accessibilityIdentifier("upNext.countdown")
            }
        }
        .frame(width: size, height: size)
        .shadow(color: .black.opacity(0.4), radius: 8)
    }
}
