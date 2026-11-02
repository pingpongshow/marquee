import MarqueeKit
import SwiftUI

/// Favourites (the heart, USER-12) set or cleared in this session, so the item page, its rows,
/// menus and Now Playing agree at once, before their lists are fetched again.
@MainActor @Observable
final class FavoriteStore {
    static let shared = FavoriteStore()
    private var overrides: [Int64: Bool] = [:]

    /// What can be a favourite.
    static let types: Set<Schemas.ItemType> = [.movie, .show, .episode, .track, .album, .artist]

    static func canFavorite(_ item: Item) -> Bool { types.contains(item._type) }

    /// This session's change, one waiting to sync (USER-18), or what the server sent.
    func isFavorite(_ item: Item) -> Bool {
        overrides[item.id] ?? OfflineSync.shared.pendingFavorite(item.id) ?? item.favorite ?? false
    }

    /// Sets or clears at once, and puts the old state back if the server refuses. Offline, the
    /// change stays and syncs later. Home's rows (Your Favourites) reload once it's saved.
    /// Making it a favourite takes back a thumbs down (MUSIC-25), as it does on the server.
    func set(_ item: Item, _ on: Bool, app: AppSession) {
        let id = item.id, old = overrides[id], oldDislike = DislikeStore.shared.override(id)
        overrides[id] = on
        if on { DislikeStore.shared.note(id, false) }
        Task {
            do {
                try await app.setItemFavorite(id, on)
                HomeChanges.shared.version += 1
            } catch is CancellationError {
            } catch {
                overrides[id] = old
                DislikeStore.shared.note(id, oldDislike)
                ActionError.shared.message = "Couldn't save the favourite: \(error.localizedDescription)"
            }
        }
    }

    func toggle(_ item: Item, app: AppSession) { set(item, !isFavorite(item), app: app) }

    /// This session's change, if any, to put back later.
    func override(_ id: Int64) -> Bool? { overrides[id] }

    /// Remembers a change made elsewhere (a thumbs down clears the heart on the server).
    func note(_ id: Int64, _ on: Bool?) { overrides[id] = on }

    /// Another person signed in: their own favourites show, not these.
    func reset() { overrides = [:] }
}

/// Thumbs downs on songs (MUSIC-25) given or taken back in this session, so rows, menus and Now
/// Playing agree at once. It works with the heart and the stars as the server does: a thumbs down
/// clears the heart and a rating of 3.5 stars or more, and either of those clears the thumbs down.
@MainActor @Observable
final class DislikeStore {
    static let shared = DislikeStore()
    private var overrides: [Int64: Bool] = [:]

    /// The rating (0–10) from which a song can't also have a thumbs down: 3.5 stars.
    static let highRating = 7.0

    /// Whether it's on offer: for songs, and only while the server can be reached (it isn't kept
    /// to sync later).
    static func canDislike(_ item: Item, app: AppSession) -> Bool { item._type == .track && !app.isOffline }

    /// This session's change or what the server sent. A heart or high rating waiting to sync
    /// (USER-18) has already taken it back.
    func isDisliked(_ id: Int64, _ fallback: Bool?) -> Bool {
        if let o = overrides[id] { return o }
        if OfflineSync.shared.pendingFavorite(id) == true { return false }
        if let pending = OfflineSync.shared.pendingRating(id), (pending ?? 0) >= Self.highRating { return false }
        return fallback ?? false
    }

    func isDisliked(_ item: Item) -> Bool { isDisliked(item.id, item.disliked) }

    /// This session's change, if any, to put back later.
    func override(_ id: Int64) -> Bool? { overrides[id] }

    /// Remembers a change made elsewhere (a heart or a high rating clears it on the server).
    func note(_ id: Int64, _ on: Bool?) { overrides[id] = on }

    /// Gives or takes back at once, clearing the heart and a high rating with it, and puts it all
    /// back if the server refuses. Once it's saved, a station playing the song moves on.
    func set(_ item: Item, _ on: Bool, app: AppSession, music: MusicPlayer) {
        let id = item.id, old = overrides[id]
        let favorites = FavoriteStore.shared, ratings = RatingStore.shared
        let oldFavorite = favorites.override(id)
        let oldRating = ratings.rating(id, item.userRating)
        let clearsRating = on && (oldRating ?? 0) >= Self.highRating
        overrides[id] = on
        if on { favorites.note(id, false) }
        if clearsRating { ratings.set(id, nil) }
        Task {
            do {
                try await app.setDisliked(id, on)
                HomeChanges.shared.version += 1
                if clearsRating { ratings.noteSaved(id) }
                if on, overrides[id] == true { music.thumbedDown(id) }
            } catch is CancellationError {
            } catch {
                overrides[id] = old
                if on { favorites.note(id, oldFavorite) }
                if clearsRating { ratings.set(id, oldRating) }
                ActionError.shared.message = "Couldn't save the thumbs down: \(error.localizedDescription)"
            }
        }
    }

    /// Another person signed in: their own show, not these.
    func reset() { overrides = [:] }
}

/// The thumbs down in a menu: "Thumbs Down", or "Take Back Thumbs Down" when it's set. Nothing
/// when it isn't on offer.
struct ThumbsDownMenuItem: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let item: Item

    var body: some View {
        if DislikeStore.canDislike(item, app: app) {
            let on = DislikeStore.shared.isDisliked(item)
            Button { DislikeStore.shared.set(item, !on, app: app, music: music) } label: {
                Label(on ? "Take Back Thumbs Down" : "Thumbs Down", systemImage: on ? "hand.thumbsdown.fill" : "hand.thumbsdown")
            }
            .accessibilityIdentifier("thumbsDownMenu.\(item.id)")
        }
    }
}

/// The thumbs down as a button of its own (Now Playing, beside the heart): filled gold when given.
struct RowDislike: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let item: Item
    var size: CGFloat = isTV ? 22 : 13
    /// How much takes the tap (or the focus).
    var target: CGFloat = isTV ? 50 : 30
    var identifier: String?

    var body: some View {
        if DislikeStore.canDislike(item, app: app) {
            let on = DislikeStore.shared.isDisliked(item)
            Button { DislikeStore.shared.set(item, !on, app: app, music: music) } label: {
                Image(systemName: on ? "hand.thumbsdown.fill" : "hand.thumbsdown")
                    .font(.system(size: size, weight: .regular))
                    .foregroundStyle(on ? AnyShapeStyle(Color.marqueeGold) : AnyShapeStyle(.tertiary))
                    .frame(minWidth: target, minHeight: target)
                    .contentShape(Rectangle())
            }
            #if os(iOS)
            .buttonStyle(.plain)
            #endif
            .fixedSize()
            .accessibilityLabel(on ? "Take back thumbs down on \(item.title)" : "Thumbs down \(item.title)")
            .accessibilityValue(on ? "On" : "Off")
            .accessibilityIdentifier(identifier ?? "rowDislike.\(item.id)")
        }
    }
}

/// Watchlist changes made in this session, so menus agree with them before their lists are
/// fetched again (a tile's menu kept offering Add to Watchlist after adding).
@MainActor @Observable
final class WatchlistStore {
    static let shared = WatchlistStore()
    private var overrides: [Int64: Bool] = [:]

    /// This session's change, one waiting to sync (USER-18), or what the server sent.
    func isListed(_ item: Item) -> Bool {
        overrides[item.id] ?? OfflineSync.shared.pendingWatchlist(item.id) ?? item.watchlisted ?? false
    }

    /// Remembers a change made elsewhere (the item page's own button).
    func note(_ id: Int64, _ on: Bool?) { overrides[id] = on }

    /// Adds or removes at once, and puts the old state back if the server refuses.
    func set(_ item: Item, _ on: Bool, app: AppSession) {
        let id = item.id, old = overrides[id]
        overrides[id] = on
        ActionError.run { [self] in
            do {
                try await app.setWatchlist(id, on)
            } catch {
                overrides[id] = old
                throw error
            }
            HomeChanges.shared.version += 1
        }
    }

    /// Another person signed in.
    func reset() { overrides = [:] }
}

/// The item page's heart: a bordered button beside Watchlist (focusable on the TV).
struct FavoriteButton: View {
    @Environment(AppSession.self) private var app
    let item: Item

    var body: some View {
        let on = FavoriteStore.shared.isFavorite(item)
        Button { FavoriteStore.shared.toggle(item, app: app) } label: {
            Label("Favourite", systemImage: on ? "heart.fill" : "heart")
        }
        .buttonStyle(.bordered)
        .accessibilityLabel(on ? "Remove from favourites" : "Add to favourites")
        .accessibilityValue(on ? "On" : "Off")
        .accessibilityIdentifier("favoriteToggle")
    }
}

/// A small heart for track and episode rows (and Now Playing, larger): filled gold when it's a
/// favourite, a faint outline otherwise. Tapped on its own, apart from the row.
struct RowFavorite: View {
    @Environment(AppSession.self) private var app
    let item: Item
    var size: CGFloat = isTV ? 22 : 13
    /// How much takes the tap (or the focus).
    var target: CGFloat = isTV ? 50 : 30
    var identifier: String?

    var body: some View {
        let on = FavoriteStore.shared.isFavorite(item)
        Button { FavoriteStore.shared.toggle(item, app: app) } label: {
            Image(systemName: on ? "heart.fill" : "heart")
                .font(.system(size: size, weight: .regular))
                .foregroundStyle(on ? AnyShapeStyle(Color.marqueeGold) : AnyShapeStyle(.tertiary))
                .frame(minWidth: target, minHeight: target)
                .contentShape(Rectangle())
        }
        #if os(iOS)
        .buttonStyle(.plain)
        #endif
        .fixedSize()
        .accessibilityLabel(on ? "Remove \(item.title) from favourites" : "Add \(item.title) to favourites")
        .accessibilityValue(on ? "On" : "Off")
        .accessibilityIdentifier(identifier ?? "rowFavorite.\(item.id)")
    }
}
