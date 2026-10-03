import MarqueeKit
import SwiftUI

/// Tracks to save as an audio playlist, with the suggested title.
struct SavePlaylistRequest: Identifiable {
    let id = UUID()
    var title: String
    var itemIDs: [Int64]
}

extension MusicPlayer {
    /// What's playing as a playlist request: the queue's tracks in order (played and
    /// upcoming), each once, at most 500, titled after the station or album ("Queue – date"
    /// for a plain queue).
    var queueAsPlaylist: SavePlaylistRequest? {
        var seen = Set<Int64>()
        let ids = queue.entries.map(\.item.id).filter { seen.insert($0).inserted }.prefix(500)
        guard !ids.isEmpty else { return nil }
        let title = source?.title ?? "Queue – \(Date().formatted(date: .abbreviated, time: .omitted))"
        return SavePlaylistRequest(title: title, itemIDs: Array(ids))
    }
}

extension View {
    /// Asks for a title (editable), saves the playlist, then confirms with Open.
    func savePlaylistFlow(_ request: Binding<SavePlaylistRequest?>) -> some View {
        modifier(SavePlaylistFlow(request: request))
    }
}

private struct OpenedPlaylist: Identifiable { let id: Int64 }

private struct SavePlaylistFlow: ViewModifier {
    @Environment(AppSession.self) private var app
    @Binding var request: SavePlaylistRequest?
    @State private var title = ""
    @State private var saved: Playlist?
    @State private var open: OpenedPlaylist?
    @State private var error: String?

    func body(content: Content) -> some View {
        content
            .onChange(of: request?.id) { title = request?.title ?? "" }
            .alert("Save as Playlist", isPresented: Binding(get: { request != nil }, set: { if !$0 { request = nil } })) {
                TextField("Title", text: $title)
                    .accessibilityIdentifier("playlistTitle")
                Button("Save") { save() }
                Button("Cancel", role: .cancel) { request = nil }
            } message: {
                let n = request?.itemIDs.count ?? 0
                Text("\(n) track\(n == 1 ? "" : "s") in this order.")
            }
            .alert(saved.map { "Saved “\($0.title)”" } ?? "", isPresented: Binding(get: { saved != nil }, set: { if !$0 { saved = nil } })) {
                Button("Open") { if let s = saved { open = OpenedPlaylist(id: s.id) } }
                Button("Done", role: .cancel) {}
            }
            .alert("Couldn't save the playlist", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(error ?? "")
            }
            .sheet(item: $open) { p in
                NavigationStack {
                    PlaylistView(id: p.id)
                        .marqueeDestinations()
                        .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { open = nil } } }
                }
                // Audio playlists only; a presenter so the playlist page has one.
                .environment(VideoPresenter())
            }
    }

    private func save() {
        guard let req = request else { return }
        request = nil
        let name = title.trimmingCharacters(in: .whitespaces).isEmpty ? req.title : title.trimmingCharacters(in: .whitespaces)
        Task {
            do {
                saved = try await app.createPlaylist(title: name, kind: .audio, items: req.itemIDs)
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
