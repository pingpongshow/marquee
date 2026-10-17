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
    func set(_ item: Item, _ on: Bool, app: AppSession) {
        let id = item.id, old = overrides[id]
        overrides[id] = on
        Task {
            do {
                try await app.setItemFavorite(id, on)
                HomeChanges.shared.version += 1
            } catch is CancellationError {
            } catch {
                overrides[id] = old
                ActionError.shared.message = "Couldn't save the favourite: \(error.localizedDescription)"
            }
        }
    }

    func toggle(_ item: Item, app: AppSession) { set(item, !isFavorite(item), app: app) }
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
    var identifier: String?

    var body: some View {
        let on = FavoriteStore.shared.isFavorite(item)
        Button { FavoriteStore.shared.toggle(item, app: app) } label: {
            Image(systemName: on ? "heart.fill" : "heart")
                .font(.system(size: size, weight: .regular))
                .foregroundStyle(on ? AnyShapeStyle(Color.marqueeGold) : AnyShapeStyle(.tertiary))
                .frame(minWidth: isTV ? 50 : 30, minHeight: isTV ? 50 : 30)
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
