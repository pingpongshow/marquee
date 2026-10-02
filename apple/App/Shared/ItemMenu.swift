import MarqueeKit
import SwiftUI

/// Asks MainView to show the add-to-playlist sheet (menus can't present sheets themselves).
@MainActor @Observable
final class PlaylistPicker {
    static let shared = PlaylistPicker()
    var item: Item?
}

/// Queue and playlist actions for any item (context menus and "…" buttons).
struct ItemMenuItems: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let item: Item

    var body: some View {
        if item.isMusic {
            if [.track, .album, .artist].contains(item._type) {
                Button { startRadio() } label: { Label("Start Radio", systemImage: "dot.radiowaves.left.and.right") }
            }
            if item._type == .track {
                Button { AdventurePicker.shared.from = item } label: { Label("Sonic Adventure…", systemImage: "point.topleft.down.to.point.bottomright.curvepath") }
            }
            Button { Task { music.playNext(await leaves()) } } label: { Label("Play Next", systemImage: "text.line.first.and.arrowtriangle.forward") }
            Button { Task { music.addToQueue(await leaves()) } } label: { Label("Add to Queue", systemImage: "text.line.last.and.arrowtriangle.forward") }
            if item._type != .track {
                Button { Task { music.play(await leaves(), shuffle: true, source: item.title) } } label: { Label("Shuffle", systemImage: "shuffle") }
            }
        }
        Button { PlaylistPicker.shared.item = item } label: { Label("Add to Playlist…", systemImage: "text.badge.plus") }
        if item._type == .track, let album = item.parentId {
            NavigationLink(value: Route.item(album)) { Label("Go to Album", systemImage: "square.stack") }
        }
        if item._type == .track || item._type == .album, let artist = item._type == .track ? item.grandparentId : item.parentId {
            NavigationLink(value: Route.item(artist)) { Label("Go to Artist", systemImage: "music.mic") }
        }
        if [.movie, .show, .episode, .video].contains(item._type) {
            let listed = item.watchlisted ?? false
            Button { Task { try? await app.setWatchlist(item.id, !listed) } } label: {
                Label(listed ? "Remove from Watchlist" : "Add to Watchlist", systemImage: listed ? "bookmark.slash" : "bookmark")
            }
        }
        if item.isPlayableVideo || item._type == .show || item._type == .season {
            Button { Task { try? await app.setWatched(item.id, !item.watched) } } label: {
                Label(item.watched ? "Mark Unwatched" : "Mark Watched", systemImage: item.watched ? "circle" : "checkmark.circle")
            }
        }
    }

    private func startRadio() {
        let req = RadioRequest(seed: .item, itemId: item.id, limit: 50)
        Task { if let st = try? await app.radio(req) { music.playStation(st, radio: req) } }
    }

    private func leaves() async -> [Item] {
        item._type == .track ? [item] : ((try? await app.leaves(item.id)) ?? [])
    }
}

/// Pick or create a playlist for an item.
struct AddToPlaylistSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let item: Item
    @State private var playlists: [Playlist] = []
    @State private var newTitle = ""
    @State private var error: String?
    @State private var done: String?

    private var kind: Schemas.PlaylistKind { item.isMusic ? .audio : .video }

    var body: some View {
        NavigationStack {
            List {
                if let done { Label("Added to \(done)", systemImage: "checkmark.circle.fill").foregroundStyle(.green) }
                if let error { ErrorBanner(message: error) }
                Section("New playlist") {
                    HStack {
                        TextField("Name", text: $newTitle)
                        Button("Create") {
                            Task {
                                do {
                                    let p = try await app.createPlaylist(title: newTitle, kind: kind, items: [item.id])
                                    finish(p.title)
                                } catch { self.error = error.localizedDescription }
                            }
                        }
                        .disabled(newTitle.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                }
                Section(kind == .audio ? "Music playlists" : "Video playlists") {
                    ForEach(playlists, id: \.id) { p in
                        Button {
                            Task {
                                do {
                                    try await app.addToPlaylist(p.id, items: [item.id])
                                    finish(p.title)
                                } catch { self.error = error.localizedDescription }
                            }
                        } label: {
                            LabeledContent(p.title, value: "\(p.itemCount)")
                        }
                    }
                }
            }
            .navigationTitle("Add to Playlist")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
            .task { playlists = (try? await app.playlists(kind: kind)) ?? [] }
        }
    }

    private func finish(_ title: String) {
        done = title
        Task { try? await Task.sleep(for: .milliseconds(700)); dismiss() }
    }
}
