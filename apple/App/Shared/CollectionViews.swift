import MarqueeKit
import SwiftUI

extension Library {
    /// A video library whose people keep collections of their own in it (USER-23): on unless
    /// an admin turned it off.
    var takesPersonalCollections: Bool {
        [.movies, .shows, .anime, .videos].contains(_type) && options.personalCollections != false
    }
}

/// The libraries that take personal collections, for menus (which can't load anything): kept
/// current by MainView as it loads the libraries.
@MainActor @Observable
final class CollectionLibraries {
    static let shared = CollectionLibraries()
    private(set) var ids: Set<Int64> = []

    func update(_ libraries: [Library]) {
        let now = Set(libraries.filter(\.takesPersonalCollections).map(\.id))
        if now != ids { ids = now }
    }

    /// Whether "Add to My Collection" is offered for an item: a film, show, season, episode or
    /// video of a library that takes them.
    func takes(_ item: Item) -> Bool {
        [.movie, .show, .season, .episode, .video].contains(item._type) && ids.contains(item.libraryId)
    }
}

/// The person's own collections in a video library (USER-23), first under the library's
/// Collections: New collection, theirs and the ones shared with them, and invitations waiting
/// for an answer. The library's own collections follow, headed apart only when there are both.
struct MyCollections: View {
    @Environment(AppSession.self) private var app
    let library: Library
    /// The library grid's tile width and gap.
    let width: CGFloat
    let gap: CGFloat
    let libraryHasCollections: Bool
    @State private var collections: [Playlist] = []
    @State private var invites: [Playlist] = []
    @State private var naming = false
    @State private var name = ""

    private var headed: Bool { libraryHasCollections && !collections.isEmpty }

    var body: some View {
        VStack(alignment: .leading, spacing: gap) {
            if !invites.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    heading("Shared with you")
                    ForEach(invites, id: \.id) { p in
                        PlaylistInviteRow(playlist: p, accept: { accept(p) }, decline: { decline(p) })
                    }
                }
            }
            if headed { heading("My Collections").accessibilityIdentifier("collections.mine") }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: width, maximum: width * 1.4), spacing: gap, alignment: .top)], spacing: gap) {
                newTile
                ForEach(collections, id: \.id) { PlaylistCard(playlist: $0, size: width) }
            }
            .accessibilityIdentifier("myCollections")
            #if os(tvOS)
            .focusSection()
            #endif
            if headed { heading("Library collections").accessibilityIdentifier("collections.library") }
        }
        .padding(.horizontal, sidePadding)
        .padding(.top)
        .alert("New Collection", isPresented: $naming) {
            TextField("Name", text: $name)
                .accessibilityIdentifier("collection.name")
            Button("Create") { create() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("A collection of titles from \(library.name).")
        }
        .task { await load() }
        .onChange(of: PlaylistChanges.shared.version) { Task { await load() } }
    }

    private func heading(_ title: String) -> some View {
        Text(title).font(.title2.bold()).frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }

    /// The first tile: makes a collection (named in an alert, which the TV's remote can type in).
    private var newTile: some View {
        VStack(alignment: .leading, spacing: isTV ? 14 : 6) {
            Button { name = ""; naming = true } label: {
                ZStack {
                    RoundedRectangle(cornerRadius: 8).fill(Color.secondary.opacity(0.15))
                    Image(systemName: "plus").font(.largeTitle).foregroundStyle(Color.marqueeGold)
                }
                .frame(width: width, height: width)
                .contentShape(Rectangle())
            }
            #if os(tvOS)
            .buttonStyle(.card)
            #else
            .buttonStyle(.plain)
            #endif
            .accessibilityLabel("New collection")
            .accessibilityIdentifier("collections.new")
            Text("New collection").font(.subheadline.weight(.medium)).lineLimit(1)
        }
        .frame(width: width, alignment: .leading)
    }

    private func create() {
        let title = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !title.isEmpty else { return }
        ActionError.run {
            _ = try await app.createPlaylist(title: title, kind: .video, libraryId: library.id, items: [])
            PlaylistChanges.shared.changed()
        }
    }

    private func accept(_ p: Playlist) {
        invites.removeAll { $0.id == p.id }
        ActionError.run {
            do {
                _ = try await app.acceptPlaylistShare(p.id)
                PlaylistChanges.shared.changed()
            } catch {
                await load()
                throw error
            }
        }
    }

    private func decline(_ p: Playlist) {
        invites.removeAll { $0.id == p.id }
        ActionError.run {
            do {
                try await app.removePlaylistShare(p.id)
            } catch {
                await load()
                throw error
            }
        }
    }

    private func load() async {
        async let mine = try? app.playlists(kind: .video, libraryId: library.id)
        async let asked = try? app.playlistInvites()
        if let list = await mine { collections = list }
        if let list = await asked { invites = list.filter { $0.isCollection && $0.libraryId == library.id } }
    }
}
