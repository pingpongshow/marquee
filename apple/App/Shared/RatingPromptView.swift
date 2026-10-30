import MarqueeKit
import SwiftUI

/// A movie or episode to ask about (USER-19).
struct RatingPromptItem: Identifiable, Equatable {
    let item: Item
    var id: Int64 { item.id }

    /// "Movie" or "Show · S01E02 · Episode".
    var title: String {
        guard item._type == .episode else { return item.title }
        var parts = [item.grandparentTitle].compactMap { $0 }
        // Episodes know their season by its title ("Season 1"); specials have no number.
        let digits = item.parentTitle?.filter(\.isNumber) ?? ""
        if let e = item.index {
            parts.append(Int(digits).map { String(format: "S%02dE%02d", $0, e) } ?? "E\(e)")
        }
        parts.append(item.title)
        return parts.joined(separator: " · ")
    }

    static func == (a: Self, b: Self) -> Bool { a.id == b.id }
}

/// Rating requests when a movie or episode ends (USER-19): whether to ask, and the one to
/// show over the page once the player has closed.
@MainActor @Observable
final class RatingPrompt {
    static let shared = RatingPrompt()
    /// Shown over the item page (or Home) after the player closed at the end.
    var pending: RatingPromptItem?

    /// Whether to ask about this item: the server and the person have it on, it's a movie or
    /// an episode (not a trailer, extra or pre-roll), and the person hasn't rated it.
    static func wanted(_ detail: ItemDetail?, app: AppSession) -> RatingPromptItem? {
        guard let d = detail, app.asksForRatings else { return nil }
        guard d.type == .movie || d.type == .episode, d.base.extraType == nil else { return nil }
        guard RatingStore.shared.rating(d.id, d.base.userRating) == nil else { return nil }
        return RatingPromptItem(item: d.base)
    }

    /// How long the card stays over the page when no one touches it (iPhone and iPad; on the
    /// TV the request is a screen of its own and waits to be answered).
    static let pageDuration: Duration = .seconds(12)

    /// How long after the player closes the request appears. The player is a full-screen
    /// cover, and one view can't present two at once: the TV's request screen would be
    /// refused if it went up before the player's cover had gone.
    static let afterPlayer: Duration = .milliseconds(600)
}

/// Where a rating request is drawn.
enum RatingPromptLayout {
    /// A small card in a corner of the page (iPhone and iPad), or beside the Up Next cards.
    case card
    /// A screen of its own over the item's artwork (Apple TV, after the player has closed).
    case screen
}

/// Asks for stars: artwork, the title, five stars (half stars by tapping the left half on
/// iPhone/iPad), Add a comment, Not now and Don't ask again. Choosing stars saves at once,
/// says thanks and closes. On the TV left/right picks the stars, OK saves and Back closes.
struct RatingPromptCard: View {
    @Environment(AppSession.self) private var app
    let prompt: RatingPromptItem
    /// How it's drawn; `.screen` is the Apple TV's own screen for it.
    var layout: RatingPromptLayout = .card
    /// Closes by itself after this long untouched (nil = stays until answered).
    var autoDismiss: Duration?
    /// The person touched the card (the Up Next countdown pauses).
    var onInteract: () -> Void = {}
    /// The card is finished with (rated, Not now, Don't ask again, Back, or timed out).
    let onDone: () -> Void

    @State private var rating: Double?
    @State private var thanks = false
    @State private var touched = false
    @State private var commenting = false
    #if os(tvOS)
    private enum Focus: Hashable { case star(Int), notNow, never }
    @FocusState private var focus: Focus?
    /// The star under focus, previewed until OK saves it.
    @State private var hovered: Int?
    @State private var initialFocusDone = false
    #endif

    var body: some View {
        Group {
            #if os(tvOS)
            if layout == .screen { screen } else { card }
            #else
            card
            #endif
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("ratingPrompt")
        .task(id: touched) {
            guard let autoDismiss, !touched else { return }
            try? await Task.sleep(for: autoDismiss)
            if !Task.isCancelled, !touched { onDone() }
        }
        #if os(tvOS)
        .focusSection()
        .defaultFocus($focus, .star(3))
        .onExitCommand { onDone() }
        .onAppear {
            // Start on the middle star so left/right chooses at once.
            Task {
                try? await Task.sleep(for: .milliseconds(300))
                if focus == nil { focus = .star(3) }
                try? await Task.sleep(for: .milliseconds(300))
                initialFocusDone = true
            }
        }
        .onChange(of: focus) { _, new in
            if case .star(let s) = new { hovered = s } else { hovered = nil }
            if initialFocusDone { interact() }
        }
        #endif
        #if os(iOS)
        .sheet(isPresented: $commenting, onDismiss: onDone) {
            ReviewsSheet(itemID: prompt.id, title: prompt.item.title, yourRating: rating)
        }
        #endif
    }

    // MARK: - The card, in a corner of the page or beside the Up Next cards

    private var card: some View {
        HStack(alignment: .top, spacing: isTV ? 28 : 14) {
            ArtworkView(item: prompt.item, shape: artShape, width: artWidth)
                .frame(width: artWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: isTV ? 16 : 8) {
                if thanks {
                    thanksLabel
                } else {
                    Text(heading)
                        .font(isTV ? .callout : .caption).foregroundStyle(.secondary)
                    Text(prompt.title).font(isTV ? .title3.weight(.semibold) : .subheadline.weight(.semibold)).lineLimit(2)
                        .accessibilityIdentifier("ratingPromptTitle")
                    stars
                    buttons
                }
            }
            Spacer(minLength: 0)
        }
        .padding(isTV ? 28 : 14)
        .frame(maxWidth: isTV ? 760 : 420, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: isTV ? 24 : 16, style: .continuous))
        .shadow(color: .black.opacity(0.3), radius: 12, y: 4)
    }

    private var heading: String { prompt.item._type == .episode ? "Rate this episode" : "Rate this movie" }
    private var artShape: PosterShape { prompt.item._type == .episode ? .wide : .poster }

    private var artWidth: CGFloat {
        let wide = prompt.item._type == .episode
        return isTV ? (wide ? 220 : 130) : (wide ? 110 : 64)
    }

    @ViewBuilder private var thanksLabel: some View {
        Label("Thanks!", systemImage: "checkmark.circle.fill")
            .font(isTV ? .title3.weight(.semibold) : .headline)
            .foregroundStyle(Color.marqueeGold)
            .accessibilityIdentifier("ratingPromptThanks")
    }

    // MARK: - Stars

    @ViewBuilder private var stars: some View {
        HStack(spacing: isTV ? 16 : 8) {
            ForEach(1...5, id: \.self) { star in
                #if os(tvOS)
                Button { choose(Double(star * 2)) } label: {
                    RatingStarTV(symbol: symbol(star), big: layout == .screen, focused: focus == .star(star))
                }
                .buttonStyle(.plain)
                .hoverEffectDisabled() // the system's white panel sits oddly on a star
                .focused($focus, equals: .star(star))
                .accessibilityLabel("Rate \(star) star\(star == 1 ? "" : "s")")
                .accessibilityIdentifier("ratingPromptStar\(star)")
                #else
                // The left half of a star gives a half star.
                Image(systemName: symbol(star)).font(.system(size: 28))
                    .foregroundStyle(symbol(star) == "star" ? Color.secondary : Color.marqueeGold)
                    .contentShape(Rectangle())
                    .overlay {
                        GeometryReader { g in
                            Color.clear.contentShape(Rectangle())
                                .onTapGesture(coordinateSpace: .local) { p in
                                    choose(p.x < g.size.width / 2 ? Double(star * 2 - 1) : Double(star * 2))
                                }
                        }
                    }
                    .accessibilityElement()
                    .accessibilityAddTraits(.isButton)
                    .accessibilityLabel("Rate \(star) star\(star == 1 ? "" : "s")")
                    .accessibilityIdentifier("ratingPromptStar\(star)")
                    .accessibilityAction { choose(Double(star * 2)) }
                #endif
            }
        }
    }

    private func symbol(_ star: Int) -> String {
        #if os(tvOS)
        let r = rating ?? hovered.map { Double($0 * 2) } ?? 0
        #else
        let r = rating ?? 0
        #endif
        if r >= Double(star * 2) { return "star.fill" }
        if r >= Double(star * 2 - 1) { return "star.leadinghalf.filled" }
        return "star"
    }

    @ViewBuilder private var buttons: some View {
        HStack(spacing: isTV ? 20 : 10) {
            #if os(iOS)
            Button("Add a comment") { interact(); commenting = true }
                .accessibilityIdentifier("ratingPromptComment")
            #endif
            Button("Not now") { onDone() }
                #if os(tvOS)
                .focused($focus, equals: .notNow)
                #endif
                .accessibilityIdentifier("ratingPromptNotNow")
            Button("Don't ask again") { neverAgain() }
                #if os(tvOS)
                .focused($focus, equals: .never)
                #endif
                .accessibilityIdentifier("ratingPromptNever")
        }
        #if os(iOS)
        .buttonStyle(.bordered)
        .controlSize(.small)
        .font(.caption)
        #endif
    }

    // MARK: - Actions

    private func interact() {
        guard !touched else { return }
        touched = true
        onInteract()
    }

    /// Saves the stars at once, says thanks and closes.
    private func choose(_ value: Double) {
        guard !thanks else { return }
        interact()
        rating = value
        RatingStore.shared.rate(prompt.id, value, was: nil, app: app)
        withAnimation { thanks = true }
        Task {
            try? await Task.sleep(for: .seconds(1.5))
            onDone()
        }
    }

    private func neverAgain() {
        interact()
        Task {
            do { try await app.setRatingPrompt(false) } catch {
                ActionError.shared.message = "Couldn't save: \(error.localizedDescription)"
            }
        }
        onDone()
    }
}

#if os(tvOS)

// MARK: - The Apple TV screen

extension RatingPromptCard {
    /// The request as a screen of its own: the artwork over its own blurred backdrop, the
    /// title, big stars and the two ways out. Shown as a cover once the player has closed, so
    /// the remote reaches it (as an overlay on the page the page kept the focus) and nothing
    /// takes it away again until it's answered.
    var screen: some View {
        ZStack {
            backdrop
            VStack(spacing: 0) {
                ArtworkView(item: prompt.item, shape: artShape, width: prompt.item._type == .episode ? 460 : 260)
                    .frame(width: prompt.item._type == .episode ? 460 : 260)
                    .shadow(color: .black.opacity(0.6), radius: 30, y: 16)
                    .accessibilityHidden(true)
                    .padding(.bottom, 40)
                Text(heading.uppercased())
                    .font(.callout.weight(.bold)).kerning(1.6)
                    .foregroundStyle(.white.opacity(0.65))
                Text(prompt.title)
                    .font(.system(size: 44, weight: .bold))
                    .multilineTextAlignment(.center).lineLimit(2).minimumScaleFactor(0.7)
                    .padding(.top, 10)
                    .accessibilityIdentifier("ratingPromptTitle")
                stars.padding(.top, 24)
                // One height for both, so saving the stars doesn't move everything above them.
                ZStack {
                    if thanks {
                        thanksLabel.font(.system(size: 32, weight: .semibold))
                            .transition(.opacity)
                    } else {
                        VStack(spacing: 26) {
                            Text(starHint).font(.callout).foregroundStyle(.white.opacity(0.65))
                            buttons
                        }
                    }
                }
                .frame(height: 150, alignment: .top)
                .padding(.top, 14)
            }
            .frame(maxWidth: 1000)
            .padding(60)
        }
        .foregroundStyle(.white)
        .ignoresSafeArea()
    }

    /// The title's own artwork behind it, blurred and darkened, as the Up Next screen does.
    private var backdrop: some View {
        ZStack {
            Color.black
            CachedImage(fill: app.imageURL(prompt.item.images?.backdrop ?? prompt.item.images?.thumb ?? prompt.item.images?.poster, width: 1920))
                .blur(radius: 40)
                .opacity(0.5)
            LinearGradient(colors: [.black.opacity(0.75), .black.opacity(0.45), .black.opacity(0.85)],
                           startPoint: .top, endPoint: .bottom)
        }
        .clipped()
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }

    /// What the stars under the focus would save, or how to rate before anything is focused.
    private var starHint: String {
        guard let s = hovered else { return "Press Select to rate, or Back to skip" }
        return RowRating.label(Double(s * 2))
    }
}

/// One star on the TV: gold when it counts towards the rating, and lifted with a soft glow
/// while the remote is on it (tvOS's own focus ring sits oddly on a star).
private struct RatingStarTV: View {
    let symbol: String
    let big: Bool
    let focused: Bool

    var body: some View {
        let size: CGFloat = big ? 56 : 40
        Image(systemName: symbol)
            .font(.system(size: size, weight: .semibold))
            .foregroundStyle(symbol == "star" ? Color.white.opacity(0.35) : Color.marqueeGold)
            .shadow(color: Color.marqueeGold.opacity(focused ? 0.6 : 0), radius: 18)
            .frame(width: size * 1.5, height: size * 1.5)
            .background {
                Circle().fill(.white.opacity(focused ? 0.16 : 0))
            }
            .scaleEffect(focused ? 1.18 : 1)
            .animation(.spring(response: 0.3, dampingFraction: 0.72), value: focused)
    }
}

#endif

extension View {
    /// The rating request left by a player that closed at the end (USER-19), over this page
    /// for a few seconds or until answered.
    func ratingPromptOverlay(active: Bool = true) -> some View {
        modifier(RatingPromptOverlay(active: active))
    }
}

private struct RatingPromptOverlay: ViewModifier {
    let active: Bool
    @State private var prompt = RatingPrompt.shared

    func body(content: Content) -> some View {
        #if os(tvOS)
        // A cover, not an overlay: an overlay over the page never got the focus, so the
        // remote couldn't reach the stars and Back dismissed the whole thing (USER-19).
        content.fullScreenCover(item: Binding(get: { active ? prompt.pending : nil },
                                              set: { if $0 == nil { prompt.pending = nil } })) { p in
            RatingPromptCard(prompt: p, layout: .screen) {
                if prompt.pending == p { prompt.pending = nil }
            }
        }
        #else
        content.overlay(alignment: .bottom) {
            if active, let p = prompt.pending {
                RatingPromptCard(prompt: p, autoDismiss: RatingPrompt.pageDuration) {
                    if prompt.pending == p { withAnimation { prompt.pending = nil } }
                }
                .id(p.id)
                .padding(.horizontal, 16)
                .padding(.bottom, 110) // above the tab bar and mini player
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.25), value: prompt.pending)
        #endif
    }
}
