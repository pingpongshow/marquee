import MarqueeKit
import SwiftUI

/// The person's playlist folders (MUSIC-28), shared by the pages that file playlists.
@MainActor @Observable
final class PlaylistFolders {
    static let shared = PlaylistFolders()
    var list: [PlaylistFolder] = []

    func load(_ app: AppSession) async {
        if let l = try? await app.playlistFolders() { list = l }
    }

    func folder(_ id: Int64?) -> PlaylistFolder? {
        list.first { $0.id == id }
    }

    /// Puts a playlist in a folder, or takes it out with nil; the playlist pages load again.
    func move(_ playlist: Playlist, to folder: Int64?, app: AppSession) {
        guard folder != playlist.folderId else { return }
        ActionError.run {
            if let folder {
                try await app.addPlaylistToFolder(folder, playlist: playlist.id)
            } else if let current = playlist.folderId {
                try await app.removePlaylistFromFolder(current, playlist: playlist.id)
            }
            await self.load(app)
            PlaylistChanges.shared.changed()
        }
    }

    /// Makes a folder, and puts the playlist in it when there is one.
    func create(_ name: String, putting playlist: Playlist?, app: AppSession) {
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return }
        ActionError.run {
            let f = try await app.createPlaylistFolder(name: name)
            if let playlist { try await app.addPlaylistToFolder(f.id, playlist: playlist.id) }
            await self.load(app)
            PlaylistChanges.shared.changed()
        }
    }
}

/// Menu items that put a playlist in one of the person's folders, a new one, or take it out.
struct PlaylistFolderMenuItems: View {
    @Environment(AppSession.self) private var app
    let playlist: Playlist
    /// Asks for a new folder's name; the playlist goes in it.
    let newFolder: () -> Void

    var body: some View {
        let folders = PlaylistFolders.shared
        ForEach(folders.list, id: \.id) { f in
            Button { folders.move(playlist, to: f.id, app: app) } label: {
                if f.id == playlist.folderId { Label(f.name, systemImage: "checkmark") } else { Text(f.name) }
            }
        }
        Button("New Folder…", systemImage: "folder.badge.plus", action: newFolder)
        if playlist.folderId != nil {
            Divider()
            Button("Remove from Folder", systemImage: "folder.badge.minus") { folders.move(playlist, to: nil, app: app) }
        }
    }
}

/// A "Folder" menu for a playlist's page.
struct PlaylistFolderButton: View {
    let playlist: Playlist
    @State private var naming = false

    var body: some View {
        Menu {
            PlaylistFolderMenuItems(playlist: playlist) { naming = true }
        } label: {
            Label(PlaylistFolders.shared.folder(playlist.folderId)?.name ?? "Folder", systemImage: playlist.folderId == nil ? "folder" : "folder.fill")
        }
        .buttonStyle(.bordered)
        .accessibilityIdentifier("playlist.folder")
        .playlistFolderNamePrompt(isPresented: $naming, putting: playlist)
    }
}

extension View {
    /// Asks for a name and makes a folder, putting `playlist` in it when given.
    func playlistFolderNamePrompt(isPresented: Binding<Bool>, putting playlist: Playlist? = nil) -> some View {
        modifier(FolderNamePrompt(isPresented: isPresented, folder: nil, playlist: playlist))
    }

    /// Asks for a folder's new name.
    func playlistFolderRenamePrompt(isPresented: Binding<Bool>, folder: PlaylistFolder?) -> some View {
        modifier(FolderNamePrompt(isPresented: isPresented, folder: folder, playlist: nil))
    }
}

private struct FolderNamePrompt: ViewModifier {
    @Environment(AppSession.self) private var app
    @Binding var isPresented: Bool
    /// Renaming this folder; nil makes a new one.
    let folder: PlaylistFolder?
    let playlist: Playlist?
    @State private var name = ""

    func body(content: Content) -> some View {
        content
            .alert(folder == nil ? "New Folder" : "Rename Folder", isPresented: $isPresented) {
                TextField("Name", text: $name)
                    .accessibilityIdentifier("folder.name")
                Button(folder == nil ? "Create" : "Rename") { save() }
                Button("Cancel", role: .cancel) {}
            } message: {
                if let playlist, folder == nil { Text("“\(playlist.title)” goes in it.") }
            }
            .onChange(of: isPresented) { if isPresented { name = folder?.name ?? "" } }
    }

    private func save() {
        guard let folder else {
            PlaylistFolders.shared.create(name, putting: playlist, app: app)
            return
        }
        let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, name != folder.name else { return }
        ActionError.run {
            _ = try await app.renamePlaylistFolder(folder.id, name: name)
            await PlaylistFolders.shared.load(app)
        }
    }
}

/// A folder of playlists in the grid; opens it.
struct PlaylistFolderCard: View {
    let folder: PlaylistFolder
    /// The playlists in it shown here.
    let count: Int
    var size: CGFloat

    var body: some View {
        #if os(tvOS)
        VStack(alignment: .leading, spacing: 14) {
            NavigationLink(value: Route.playlistFolder(folder.id)) { cover }
                .buttonStyle(.card)
                .accessibilityLabel(folder.name)
            titles
        }
        .frame(width: size)
        #else
        NavigationLink(value: Route.playlistFolder(folder.id)) {
            VStack(alignment: .leading, spacing: 6) {
                cover
                titles
            }
            .frame(width: size)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("playlistFolder.\(folder.id)")
        #endif
    }

    private var cover: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 8).fill(Color.gray.opacity(0.25))
            Image(systemName: "folder.fill").font(.system(size: size / 3)).foregroundStyle(.secondary)
        }
        .frame(width: size, height: size)
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(folder.name).font(.subheadline.weight(.medium)).lineLimit(1)
            Text(count == 1 ? "1 playlist" : "\(count) playlists").font(.caption).foregroundStyle(.secondary)
        }
    }
}
