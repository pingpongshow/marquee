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

    /// How long the card stays over the page when no one touches it.
    static let pageDuration: Duration = .seconds(12)
}

/// A small card asking for stars: artwork, the title, five stars (half stars by tapping the
/// left half on iPhone/iPad), Add a comment, Not now and Don't ask again. Choosing stars saves
/// at once, says thanks and closes. On the TV, left/right picks the stars, OK saves and Back
/// closes.
struct RatingPromptCard: View {
    @Environment(AppSession.self) private var app
    let prompt: RatingPromptItem
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
        HStack(alignment: .top, spacing: isTV ? 28 : 14) {
            ArtworkView(item: prompt.item, shape: prompt.item._type == .episode ? .wide : .poster, width: artWidth)
                .frame(width: artWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: isTV ? 16 : 8) {
                if thanks {
                    Label("Thanks!", systemImage: "checkmark.circle.fill")
                        .font(isTV ? .title3.weight(.semibold) : .headline)
                        .foregroundStyle(Color.marqueeGold)
                        .accessibilityIdentifier("ratingPromptThanks")
                } else {
                    Text(prompt.item._type == .episode ? "Rate this episode" : "Rate this movie")
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
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("ratingPrompt")
        .task(id: touched) {
            guard let autoDismiss, !touched else { return }
            try? await Task.sleep(for: autoDismiss)
            if !Task.isCancelled, !touched { onDone() }
        }
        #if os(tvOS)
        .focusSection()
        .onExitCommand { onDone() }
        .onAppear {
            // Start on the middle star so left/right chooses at once.
            Task {
                try? await Task.sleep(for: .milliseconds(300))
                focus = .star(3)
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

    private var artWidth: CGFloat {
        let wide = prompt.item._type == .episode
        return isTV ? (wide ? 220 : 130) : (wide ? 110 : 64)
    }

    // MARK: - Stars

    @ViewBuilder private var stars: some View {
        HStack(spacing: isTV ? 14 : 8) {
            ForEach(1...5, id: \.self) { star in
                #if os(tvOS)
                Button { choose(Double(star * 2)) } label: {
                    Image(systemName: symbol(star)).font(.system(size: 40))
                        .foregroundStyle(symbol(star) == "star" ? Color.secondary : Color.marqueeGold)
                        .padding(6)
                }
                .buttonStyle(.plain)
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
        content.overlay(alignment: isTV ? .bottomTrailing : .bottom) {
            if active, let p = prompt.pending {
                RatingPromptCard(prompt: p, autoDismiss: RatingPrompt.pageDuration) {
                    if prompt.pending == p { withAnimation { prompt.pending = nil } }
                }
                .id(p.id)
                .padding(.horizontal, isTV ? 60 : 16)
                .padding(.bottom, isTV ? 60 : 110) // above the tab bar and mini player
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.25), value: prompt.pending)
    }
}
