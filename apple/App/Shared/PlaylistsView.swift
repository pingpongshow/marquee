import MarqueeKit
import SwiftUI

/// Bumped when a playlist leaves or joins the person's own (accepted, or removed from a pushed
/// page): the lists showing them load again.
@MainActor @Observable
final class PlaylistChanges {
    static let shared = PlaylistChanges()
    var version = 0

    func changed() {
        version += 1
        HomeChanges.shared.version += 1
    }
}

struct PlaylistMosaic: View {
    @Environment(AppSession.self) private var app
    let playlist: Playlist
    var size: CGFloat

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 8).fill(Color.gray.opacity(0.25))
            Image(systemName: playlist.kind == .audio ? "music.note.list" : "list.and.film").font(.title).foregroundStyle(.secondary)
            if playlist.imageIds.count >= 4 {
                Grid(horizontalSpacing: 0, verticalSpacing: 0) {
                    GridRow { tile(0); tile(1) }
                    GridRow { tile(2); tile(3) }
                }
            } else if let first = playlist.imageIds.first {
                CachedImage(fill: app.imageURL(first, width: Int(size)))
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func tile(_ i: Int) -> some View {
        CachedImage(fill: app.imageURL(playlist.imageIds[i], width: Int(size / 2)))
            .frame(width: size / 2, height: size / 2).clipped()
    }
}

/// A playlist's cover, title and length; opens it.
struct PlaylistCard: View {
    let playlist: Playlist
    var size: CGFloat

    var body: some View {
        #if os(tvOS)
        VStack(alignment: .leading, spacing: 14) {
            NavigationLink(value: Route.playlist(playlist.id)) { PlaylistMosaic(playlist: playlist, size: size) }
                .buttonStyle(.card)
                .accessibilityLabel(playlist.title)
            titles
        }
        .frame(width: size)
        #else
        NavigationLink(value: Route.playlist(playlist.id)) {
            VStack(alignment: .leading, spacing: 6) {
                PlaylistMosaic(playlist: playlist, size: size)
                titles
            }
            .frame(width: size)
        }
        .buttonStyle(.plain)
        #endif
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(playlist.title).font(.subheadline.weight(.medium)).lineLimit(1)
            Text("\(playlist.itemCount) \(playlist.kind == .audio ? "tracks" : "items")").font(.caption).foregroundStyle(.secondary)
            if let owner = playlist.sharedBy {
                Text(owner).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    .accessibilityIdentifier("playlist.sharedBy")
            }
        }
    }
}

extension Playlist {
    /// "Shared by <owner>", on a playlist someone else shared with this person.
    var sharedBy: String? {
        guard readOnly == true else { return nil }
        return ownerName.map { "Shared by \($0)" } ?? "Shared with you"
    }
}

/// A playlist someone shared with this person, waiting for them to accept (MUSIC-24).
private struct PlaylistInviteRow: View {
    let playlist: Playlist
    let accept: () -> Void
    let decline: () -> Void

    #if os(tvOS)
    private let cover: CGFloat = 140
    #else
    private let cover: CGFloat = 64
    #endif

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 16) {
                titles
                Spacer(minLength: 16)
                buttons
            }
            VStack(alignment: .leading, spacing: 10) {
                titles
                buttons
            }
        }
        .padding(isTV ? 24 : 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("playlistInvite.\(playlist.id)")
        #if os(tvOS)
        .focusSection()
        #endif
    }

    private var titles: some View {
        HStack(spacing: 12) {
            PlaylistMosaic(playlist: playlist, size: cover)
            VStack(alignment: .leading, spacing: 2) {
                Text(playlist.title).font(.subheadline.weight(.semibold)).lineLimit(2)
                Text(playlist.sharedBy ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                Text(playlist.itemCount == 1 ? "1 \(playlist.kind == .audio ? "track" : "item")" : "\(playlist.itemCount) \(playlist.kind == .audio ? "tracks" : "items")")
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }

    private var buttons: some View {
        HStack(spacing: isTV ? 24 : 10) {
            Button("Accept", action: accept)
                .buttonStyle(.borderedProminent)
                .accessibilityIdentifier("playlistInvite.accept")
            Button("Decline", action: decline)
                .buttonStyle(.bordered)
                .accessibilityIdentifier("playlistInvite.decline")
        }
        .fixedSize()
    }
}

struct PlaylistsView: View {
    @Environment(AppSession.self) private var app
    /// Shows the playlists in this folder of the person's (MUSIC-28); nil, the top level.
    var folderID: Int64? = nil
    @State private var playlists: [Playlist] = []
    /// Playlists shared with this person, waiting for an answer (MUSIC-24).
    @State private var invites: [Playlist] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var naming = false
    @State private var renaming = false
    @State private var confirmingDelete = false
    /// The playlist a new folder from its menu is for.
    @State private var filing: Playlist?
    /// Combine Playlists is open (MUSIC-29).
    @State private var combining = false
    @Environment(\.dismiss) private var dismiss

    private var folders: PlaylistFolders { PlaylistFolders.shared }
    private var folder: PlaylistFolder? { folders.folder(folderID) }

    /// The folder a playlist shows in: one that's gone (or not loaded yet) leaves it at the top level.
    private func shownFolder(_ p: Playlist) -> Int64? {
        folders.folder(p.folderId)?.id
    }

    private var shown: [Playlist] { playlists.filter { shownFolder($0) == folderID } }

    var body: some View {
        ScrollView {
            #if os(tvOS)
            folderButtons.padding(.horizontal, sidePadding).padding(.top, sidePadding)
            #endif
            if folderID == nil, !invites.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Shared with you").font(.title2.bold())
                        .accessibilityAddTraits(.isHeader)
                    ForEach(invites, id: \.id) { p in
                        PlaylistInviteRow(playlist: p, accept: { accept(p) }, decline: { decline(p) })
                    }
                }
                .padding(.horizontal, sidePadding)
                .padding(.top, sidePadding)
            }
            // Video playlists first (movies and episodes, a few at most), then the music ones.
            LazyVGrid(columns: [GridItem(.adaptive(minimum: cell), spacing: 16, alignment: .top)], alignment: .leading, spacing: 20) {
                if folderID == nil, !folders.list.isEmpty {
                    Section {
                        ForEach(folders.list, id: \.id) { f in
                            PlaylistFolderCard(folder: f, count: playlists.filter { $0.folderId == f.id }.count, size: cell)
                        }
                    } header: {
                        Text("Folders").font(.title2.bold()).frame(maxWidth: .infinity, alignment: .leading)
                            .accessibilityAddTraits(.isHeader)
                            .accessibilityIdentifier("playlists.folders")
                    }
                }
                section("Video Playlists", kind: .video)
                section("Music Playlists", kind: .audio)
            }
            .padding(sidePadding)
            if let error, playlists.isEmpty {
                ContentUnavailableView {
                    Label("Couldn't load playlists", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try Again") { Task { await load() } }
                }
            } else if !loaded {
                ProgressView().padding(.top, 60)
            } else if folderID != nil, shown.isEmpty {
                ContentUnavailableView(folder == nil ? "This folder is gone" : "Nothing in this folder", systemImage: "folder",
                                       description: Text(folder == nil ? "" : "Use Folder on a playlist to put it here."))
                    #if os(tvOS)
                    .focusable()
                    #endif
            } else if playlists.isEmpty, invites.isEmpty, folders.list.isEmpty {
                ContentUnavailableView("No playlists", systemImage: "music.note.list", description: Text("Use “Add to Playlist” on any track, album, movie or episode."))
                    #if os(tvOS)
                    // Something to hold focus: with nothing focusable the TV's sidebar closed
                    // onto an empty page, and Back then left the app instead of reopening it.
                    .focusable()
                    #endif
            }
        }
        .navigationTitle(folder?.name ?? "Playlists")
        #if !os(tvOS)
        .toolbar {
            ToolbarItem(placement: .primaryAction) { folderButtons }
            if folderID == nil, playlists.count >= 2 {
                ToolbarItem(placement: .primaryAction) {
                    Button { combining = true } label: { Label("Combine Playlists", systemImage: "arrow.triangle.merge") }
                        .accessibilityIdentifier("playlists.combine")
                }
            }
        }
        .sheet(isPresented: $combining) { CombinePlaylistsSheet(playlists: playlists).macSheetFrame() }
        #endif
        .playlistFolderNamePrompt(isPresented: $naming, putting: filing)
        .playlistFolderRenamePrompt(isPresented: $renaming, folder: folder)
        .alert("Delete “\(folder?.name ?? "")”?", isPresented: $confirmingDelete) {
            Button("Delete Folder", role: .destructive) { deleteFolder() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Its playlists aren't deleted: they go back to Playlists.")
        }
        .task {
            if !loaded {
                await load()
            } else {
                await loadInvites() // each time the page is back: someone may have shared one
            }
        }
        .refreshable { await load() }
        .onChange(of: PlaylistChanges.shared.version) { Task { await load() } }
    }

    /// New Folder at the top level; Rename and Delete in a folder.
    @ViewBuilder private var folderButtons: some View {
        if let folder {
            Menu {
                Button("Rename Folder…", systemImage: "pencil") { renaming = true }
                Button("Delete Folder…", systemImage: "trash", role: .destructive) { confirmingDelete = true }
            } label: {
                Label("Folder", systemImage: "folder")
            }
            .accessibilityIdentifier("folder.menu")
            .accessibilityLabel("\(folder.name) folder")
        } else if folderID == nil {
            Button { filing = nil; naming = true } label: { Label("New Folder", systemImage: "folder.badge.plus") }
                .accessibilityIdentifier("playlists.newFolder")
        }
    }

    private func deleteFolder() {
        guard let folderID else { return }
        ActionError.run {
            try await app.deletePlaylistFolder(folderID)
            await folders.load(app)
            PlaylistChanges.shared.changed()
            dismiss()
        }
    }

    private func accept(_ p: Playlist) {
        invites.removeAll { $0.id == p.id }
        ActionError.run {
            do {
                _ = try await app.acceptPlaylistShare(p.id)
                PlaylistChanges.shared.changed()
            } catch {
                await loadInvites()
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
                await loadInvites()
                throw error
            }
        }
    }

    private func loadInvites() async {
        if let list = try? await app.playlistInvites() { invites = list }
    }

    @ViewBuilder private func section(_ title: String, kind: Schemas.PlaylistKind) -> some View {
        let all = shown
        let list = all.filter { $0.kind == kind }
        if !list.isEmpty {
            Section {
                ForEach(list, id: \.id) { p in
                    PlaylistCard(playlist: p, size: cell)
                        .contextMenu {
                            Menu {
                                PlaylistFolderMenuItems(playlist: p) { filing = p; naming = true }
                            } label: {
                                Label(p.folderId == nil ? "Add to Folder" : "Move to Folder", systemImage: "folder")
                            }
                        }
                }
            } header: {
                // Headed only when both kinds are there, or there are folders above.
                if list.count != all.count || (folderID == nil && !folders.list.isEmpty) {
                    Text(title).font(.title2.bold()).frame(maxWidth: .infinity, alignment: .leading)
                        .accessibilityAddTraits(.isHeader)
                        .accessibilityIdentifier("playlists.\(kind.rawValue)")
                }
            }
        }
    }

    private func load() async {
        async let asked: Void = loadInvites()
        async let filed: Void = folders.load(app)
        do {
            playlists = try await app.playlists()
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
        await asked
        await filed
        loaded = true
    }

    #if os(tvOS)
    private let cell: CGFloat = 260
    #else
    private let cell: CGFloat = 150
    #endif
}

struct PlaylistView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(VideoPresenter.self) private var video
    @Environment(\.dismiss) private var dismiss
    @Environment(\.horizontalSizeClass) private var sizeClass
    #if !os(tvOS)
    @Environment(Downloads.self) private var downloads
    #endif
    let id: Int64
    @State private var playlist: Playlist?
    @State private var entries: [PlaylistEntry] = []
    @State private var loaded = false
    @State private var error: String?
    /// Someone else's playlist, shared with this person: asking whether to remove it.
    @State private var confirmingRemove = false
    #if !os(tvOS)
    @State private var sharing = false
    /// The editor is open (MUSIC-29).
    @State private var editing = false
    #endif

    var body: some View {
        List {
            if let p = playlist {
                header(p)
                #if !os(tvOS)
                .listRowSeparator(.hidden)
                #endif
            }
            ForEach(Array(entries.enumerated()), id: \.element.entryId) { i, e in
                HStack(spacing: 4) {
                    Button { play(i) } label: {
                        // At iPhone width a song's time joins its subtitle, leaving the title room beside the row's buttons.
                        let time = formatTime(seconds: Double(e.item.durationMs ?? 0) / 1000)
                        let timeInline = sizeClass == .compact && e.item._type == .track
                        HStack(spacing: 12) {
                            ArtworkView(item: e.item, shape: e.item._type == .track ? .square : .wide, width: 60).frame(width: e.item._type == .track ? 44 : 80)
                            VStack(alignment: .leading) {
                                Text(e.item.title).lineLimit(1)
                                Text(timeInline ? "\(time) · \(e.item.subtitle)" : e.item.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer()
                            if !timeInline {
                                Text(time).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                                    .lineLimit(1).fixedSize().layoutPriority(1)
                            }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .contextMenu { ItemMenuItems(item: e.item) }
                    if e.item._type == .track { TrackRadioButton(track: e.item) }
                    if FavoriteStore.canFavorite(e.item) { RowFavorite(item: e.item) }
                    if e.item._type == .track { RowRating(item: e.item, gap: isTV ? 8 : 4) }
                }
                .macRowHover()
                .macKeyboardRow(id: Int64(i)) { play(i) }
            }
        }
        .listStyle(.plain)
        .macFocusList(entries.indices.map(Int64.init))
        .overlay {
            if let error, playlist == nil {
                ContentUnavailableView {
                    Label("Couldn't load the playlist", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try Again") { Task { await load() } }
                }
            } else if !loaded {
                ProgressView()
            }
        }
        .navigationTitle(playlist?.title ?? "Playlist")
        .task { if !loaded { await load() } }
        .refreshable { await load() }
        .onChange(of: PlaylistChanges.shared.version) { Task { await load() } }
        #if !os(tvOS)
        .sheet(isPresented: $sharing) {
            if let playlist { PlaylistShareSheet(playlist: playlist) { Task { await load() } }.macSheetFrame() }
        }
        .sheet(isPresented: $editing) {
            if let playlist { PlaylistEditSheet(playlist: playlist, entries: entries) { Task { await load() } }.macSheetFrame() }
        }
        #endif
        .alert("Remove “\(playlist?.title ?? "")”?", isPresented: $confirmingRemove) {
            Button("Remove", role: .destructive) { remove() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("It goes from your playlists. You'd need \(playlist?.ownerName ?? "the owner") to share it again to get it back.")
        }
    }

    /// Takes a playlist someone shared off this person's list; theirs stays as it is.
    private func remove() {
        ActionError.run {
            try await app.removePlaylistShare(id)
            #if !os(tvOS)
            downloads.unsyncPlaylist(id)
            #endif
            PlaylistChanges.shared.changed()
            dismiss()
        }
    }

    /// iPhone (any width, either orientation): cover, then the title, then the actions, each
    /// full width. iPad: the cover beside the title, but only while every action fits there
    /// with its words; otherwise stacked too.
    @ViewBuilder private func header(_ p: Playlist) -> some View {
        if compact {
            stacked(p)
        } else {
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .bottom, spacing: 16) {
                    PlaylistMosaic(playlist: p, size: coverSize)
                    VStack(alignment: .leading, spacing: 8) {
                        titles(p)
                        actions(labelledOnly: true)
                    }
                }
                .accessibilityIdentifier("playlistHeader")
                stacked(p)
            }
        }
    }

    private func stacked(_ p: Playlist) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            PlaylistMosaic(playlist: p, size: 160)
            titles(p)
            actions(labelledOnly: false)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityIdentifier("playlistHeader")
    }

    private func titles(_ p: Playlist) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(p.title).font(.title2.bold()).lineLimit(3)
            Text("\(p.itemCount) items · \(formatDuration(ms: p.durationMs))").foregroundStyle(.secondary).lineLimit(1)
            if let shared = p.sharedBy ?? sharedWith(p) {
                Text(shared).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                    .accessibilityIdentifier("playlist.sharedBy")
            }
        }
    }

    /// Who your own playlist is shared with, when it is.
    private func sharedWith(_ p: Playlist) -> String? {
        if p.sharedWithAll == true { return "Shared with everyone" }
        guard let n = p.shareCount, n > 0 else { return nil }
        return "Shared with \(n) \(n == 1 ? "person" : "people")"
    }

    /// Play and Shuffle first; Download and Pin to Home turn icon-only when there's no room.
    private func actions(labelledOnly: Bool) -> some View {
        HeaderActions(labelledOnly: labelledOnly) {
            Button { play(0) } label: { Label("Play", systemImage: "play.fill") }.buttonStyle(.borderedProminent)
            Button { play(0, shuffle: true) } label: { Label("Shuffle", systemImage: "shuffle") }.buttonStyle(.bordered)
        } secondary: {
            #if !os(tvOS)
            PlaylistDownloadButton(id: id)
            if let p = playlist, p.readOnly != true, p.rules == nil, !entries.isEmpty {
                Button { editing = true } label: { Label("Edit", systemImage: "pencil") }
                    .buttonStyle(.bordered)
                    .accessibilityIdentifier("playlist.edit")
            }
            if let p = playlist, p.readOnly != true {
                Button { sharing = true } label: { Label("Share", systemImage: "person.2") }
                    .buttonStyle(.bordered)
                    .accessibilityIdentifier("playlist.share")
            }
            #endif
            PinToHomeButton(rowID: AppSession.pinnedRowID(playlist: id))
            if let p = playlist { PlaylistFolderButton(playlist: p) }
            if playlist?.readOnly == true {
                Button { confirmingRemove = true } label: { Label("Remove", systemImage: "minus.circle") }
                    .buttonStyle(.bordered)
                    .accessibilityIdentifier("playlist.remove")
            }
        }
    }

    private var compact: Bool {
        #if !os(tvOS)
        // Every iPhone, at any width or orientation (a Pro Max in landscape is "regular").
        UIDevice.current.userInterfaceIdiom == .phone || sizeClass == .compact
        #else
        false
        #endif
    }

    #if os(tvOS)
    private let coverSize: CGFloat = 260
    #else
    private let coverSize: CGFloat = 140
    #endif

    private func load() async {
        do {
            // The one playlist, not every playlist to find it.
            async let p = app.playlist(id)
            async let e = app.playlistItems(id)
            async let filed: Void = PlaylistFolders.shared.load(app)
            (playlist, entries) = try await (p, e)
            await filed
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }

    private func play(_ start: Int, shuffle: Bool = false) {
        let items = entries.map(\.item)
        guard !items.isEmpty else { return }
        if playlist?.kind == .audio {
            music.play(items, start: start, shuffle: shuffle, source: playlist?.title)
        } else if shuffle {
            // Every item once, in a random order, one after another.
            let order = items.map(\.id).shuffled()
            video.play(order[0], startMs: 0, playlistID: id, order: order, position: 0)
        } else {
            video.play(items[start].id, startMs: 0, playlistID: id, position: start)
        }
    }
}

#if !os(tvOS)
/// Choose who your playlist is shared with (MUSIC-24): people by name, whether each has accepted,
/// and everyone on the server for admins. Saved as each switch changes.
struct PlaylistShareSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let playlist: Playlist
    /// Who it's shared with changed.
    let onChange: () -> Void
    @State private var targets: [PlaylistShareTarget] = []
    @State private var sharing: PlaylistSharing?
    @State private var saving = false
    @State private var error: String?

    private var everyone: Bool { sharing?.all ?? false }

    var body: some View {
        NavigationStack {
            Form {
                if let error { ErrorBanner(message: error) }
                if let sharing {
                    if app.me?.isAdmin == true {
                        Section {
                            Toggle("Everyone", isOn: Binding(get: { sharing.all }, set: { save(all: $0) }))
                                .accessibilityIdentifier("share.everyone")
                        } footer: {
                            Text("Everyone on this server is offered it, including people added later.")
                        }
                    }
                    Section {
                        ForEach(targets, id: \.id) { person(sharing, $0) }
                        if targets.isEmpty { Text("There's nobody else on this server.").foregroundStyle(.secondary) }
                    } header: {
                        Text("People")
                    } footer: {
                        Text("They see it once they accept, and can play it but not change it.")
                    }
                } else if error == nil {
                    ProgressView()
                }
            }
            .disabled(saving)
            .navigationTitle("Share “\(playlist.title)”")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task { await load() }
        }
    }

    private func person(_ sharing: PlaylistSharing, _ t: PlaylistShareTarget) -> some View {
        let share = sharing.shares.first { $0.userId == t.id }
        let on = sharing.all || share != nil
        return Toggle(isOn: Binding(get: { on }, set: { save(person: t.id, $0) })) {
            VStack(alignment: .leading, spacing: 2) {
                Text(t.name)
                if on {
                    let accepted = share?.status == .accepted
                    Text(accepted ? "Accepted" : "Pending").font(.caption).foregroundStyle(accepted ? Color.green : .secondary)
                        .accessibilityIdentifier("share.status.\(t.id)")
                }
            }
        }
        // Everyone has it anyway while it's shared with everyone.
        .disabled(sharing.all)
        .accessibilityIdentifier("share.person.\(t.id)")
    }

    private func load() async {
        do {
            async let people = app.playlistShareTargets()
            async let current = app.playlistSharing(playlist.id)
            (targets, sharing) = try await (people, current)
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func save(person id: Int64, _ on: Bool) {
        var ids = (sharing?.shares ?? []).map(\.userId).filter { $0 != id }
        if on { ids.append(id) }
        save(ids: ids, all: everyone)
    }

    private func save(all: Bool) {
        save(ids: (sharing?.shares ?? []).map(\.userId), all: all)
    }

    /// What comes back says who it's now shared with, and shows; on failure, what was there stays.
    private func save(ids: [Int64], all: Bool) {
        saving = true
        Task {
            defer { saving = false }
            do {
                sharing = try await app.setPlaylistSharing(playlist.id, userIDs: ids, all: app.me?.isAdmin == true ? all : nil)
                error = nil
                onChange()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Keep a playlist on the device, in sync with its changes (MUSIC-19).
struct PlaylistDownloadButton: View {
    @Environment(Downloads.self) private var downloads
    let id: Int64

    var body: some View {
        if downloads.isSynced(id) {
            let c = downloads.syncedCount(id)
            Menu {
                Text(c.done == c.total ? "All \(c.total) on this device; changes download automatically." : "\(c.done) of \(c.total) downloaded")
                Button("Remove Download", systemImage: "trash", role: .destructive) { downloads.unsyncPlaylist(id) }
            } label: {
                Label(c.done == c.total ? "Downloaded" : "\(c.done)/\(c.total)", systemImage: c.done == c.total ? "arrow.down.circle.fill" : "arrow.down.circle.dotted")
            }
            .buttonStyle(.bordered)
        } else {
            Button { Task { await downloads.syncPlaylist(id) } } label: { Label("Download", systemImage: "arrow.down.circle") }
                .buttonStyle(.bordered)
        }
    }
}
#endif

#if !os(tvOS)
// MARK: - Editing and combining playlists (MUSIC-29)

/// The playlist editor: select several to delete, drag to reorder, sort, take out duplicates,
/// then Save sends the new order in one go. Nothing changes until then; Cancel leaves it as it
/// was. For the person's own playlists (not smart ones, which fill themselves).
struct PlaylistEditSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let playlist: Playlist
    let saved: () -> Void
    @State private var entries: [PlaylistEntry]
    @State private var selection = Set<Int64>()
    @State private var saving = false
    @State private var error: String?
    private let original: [Int64]

    init(playlist: Playlist, entries: [PlaylistEntry], saved: @escaping () -> Void) {
        self.playlist = playlist
        self.saved = saved
        _entries = State(initialValue: entries)
        original = entries.map(\.entryId)
    }

    private var changed: Bool { entries.map(\.entryId) != original }

    /// Entries that repeat one earlier in the list.
    private var duplicateIDs: Set<Int64> {
        var seen = Set<Int64>(), out = Set<Int64>()
        for e in entries where !seen.insert(e.item.id).inserted { out.insert(e.entryId) }
        return out
    }

    var body: some View {
        NavigationStack {
            List(selection: $selection) {
                ForEach(entries, id: \.entryId) { e in
                    row(e).tag(e.entryId)
                }
                .onMove { entries.move(fromOffsets: $0, toOffset: $1) }
                .onDelete { offsets in
                    // Out of the selection too: "Delete 2" went on counting a row that was gone.
                    selection.subtract(offsets.map { entries[$0].entryId })
                    entries.remove(atOffsets: offsets)
                }
            }
            #if os(iOS)
            .environment(\.editMode, .constant(.active))
            #endif
            .navigationTitle("Edit Playlist")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(!changed || saving)
                        .accessibilityIdentifier("playlistEdit.save")
                }
                #if os(iOS)
                ToolbarItemGroup(placement: .bottomBar) { tools }
                #else
                ToolbarItemGroup(placement: .primaryAction) { tools }
                #endif
            }
            .overlay { if saving { ProgressView().controlSize(.large) } }
            .alert("Couldn't save the playlist", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(error ?? "")
            }
        }
        .interactiveDismissDisabled(changed)
    }

    @ViewBuilder private var tools: some View {
        Button(role: .destructive) {
            entries.removeAll { selection.contains($0.entryId) }
            selection = []
        } label: {
            Label(selection.isEmpty ? "Delete" : "Delete \(selection.count)", systemImage: "trash")
                .labelStyle(.titleAndIcon)
        }
        .disabled(selection.isEmpty)
        .accessibilityIdentifier("playlistEdit.delete")
        Spacer()
        Button(selection.count == entries.count && !entries.isEmpty ? "Select None" : "Select All") {
            selection = selection.count == entries.count ? [] : Set(entries.map(\.entryId))
        }
        Spacer()
        Menu {
            Section("Sort") {
                Button("Title", systemImage: "textformat") { sort { $0.item.title } }
                Button("Artist", systemImage: "music.mic") { sort { $0.item.artistCredit ?? $0.item.grandparentTitle ?? "" } }
                Button("Album", systemImage: "square.stack") { sort { $0.item.parentTitle ?? "" } }
                Button("Date Added, Oldest First", systemImage: "calendar") { entries.sort { $0.entryId < $1.entryId } }
                Button("Date Added, Newest First", systemImage: "calendar") { entries.sort { $0.entryId > $1.entryId } }
                Button("Reverse", systemImage: "arrow.up.arrow.down") { entries.reverse() }
                Button("Shuffle", systemImage: "shuffle") { entries.shuffle() }
            }
            let dupes = duplicateIDs
            Button("Remove \(dupes.count) Duplicate\(dupes.count == 1 ? "" : "s")", systemImage: "square.on.square.dashed") {
                entries.removeAll { dupes.contains($0.entryId) }
                selection.subtract(dupes)
            }
            .disabled(dupes.isEmpty)
        } label: {
            Label("Arrange", systemImage: "arrow.up.arrow.down.circle")
        }
        .accessibilityIdentifier("playlistEdit.arrange")
    }

    private func row(_ e: PlaylistEntry) -> some View {
        HStack(spacing: 12) {
            ArtworkView(item: e.item, shape: e.item._type == .track ? .square : .wide, width: 44)
                .frame(width: e.item._type == .track ? 36 : 64)
            VStack(alignment: .leading) {
                Text(e.item.title).lineLimit(1)
                Text(e.item.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }

    /// A stable sort by a text key, ignoring case and accents ("The" isn't skipped).
    private func sort(_ key: (PlaylistEntry) -> String) {
        let keyed = entries.enumerated().map { (key: key($0.element), at: $0.offset, entry: $0.element) }
        entries = keyed.sorted {
            let c = $0.key.compare($1.key, options: [.caseInsensitive, .diacriticInsensitive, .numeric])
            return c == .orderedSame ? $0.at < $1.at : c == .orderedAscending
        }.map(\.entry)
    }

    private func save() {
        saving = true
        let ids = entries.map(\.entryId)
        Task {
            defer { saving = false }
            do {
                try await app.setPlaylistItems(playlist.id, entryIDs: ids)
                PlaylistChanges.shared.changed()
                saved()
                dismiss()
            } catch is CancellationError {
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Combines playlists (MUSIC-29), such as several Christmas ones into one: picked in order, into
/// a new playlist or the end of one of the person's own, with or without duplicates, and the
/// originals kept or deleted (only their own; shared ones stay).
struct CombinePlaylistsSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let playlists: [Playlist]
    /// Picked, in the order they'll go in.
    @State private var picked: [Int64] = []
    /// One of the person's playlists to add them to; nil makes a new one.
    @State private var into: Int64?
    @State private var title = ""
    @State private var skipDuplicates = true
    @State private var deleteSources = false
    @State private var working = false
    @State private var error: String?

    private var kind: Schemas.PlaylistKind? { picked.first.flatMap { id in playlists.first { $0.id == id }?.kind } }

    /// The person's own, non-smart playlists of the picked kind: where they can go.
    private var destinations: [Playlist] {
        playlists.filter { $0.readOnly != true && $0.rules == nil && (kind == nil || $0.kind == kind) }
    }

    private var ready: Bool {
        let sources = picked.filter { $0 != into }
        return into == nil ? sources.count >= 2 && !title.trimmingCharacters(in: .whitespaces).isEmpty : !sources.isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    ForEach(playlists, id: \.id) { p in
                        let at = picked.firstIndex(of: p.id)
                        let allowed = kind == nil || p.kind == kind
                        Button {
                            if let at { picked.remove(at: at) } else { picked.append(p.id) }
                            if title.isEmpty, picked.count == 1 { title = p.title }
                            if let into, !destinations.contains(where: { $0.id == into }) { self.into = nil }
                        } label: {
                            HStack {
                                Image(systemName: at == nil ? "circle" : "\(at! + 1).circle.fill")
                                    .foregroundStyle(at == nil ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.marqueeGold))
                                    .font(.title3)
                                VStack(alignment: .leading) {
                                    Text(p.title).lineLimit(1)
                                    Text("\(p.itemCount) items\(p.readOnly == true ? " · shared by \(p.ownerName ?? "someone")" : "")")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                                Spacer()
                                Image(systemName: p.kind == .audio ? "music.note.list" : "film").foregroundStyle(.secondary)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .disabled(!allowed && at == nil)
                        .accessibilityAddTraits(at == nil ? [] : .isSelected)
                    }
                } header: {
                    Text("Playlists to combine, in order")
                } footer: {
                    Text("Music and video playlists can't be combined.")
                }
                Section("Into") {
                    Picker("Into", selection: $into) {
                        Text("A new playlist").tag(Int64?.none)
                        ForEach(destinations, id: \.id) { Text($0.title).tag(Optional($0.id)) }
                    }
                    if into == nil {
                        TextField("New playlist's title", text: $title)
                            .accessibilityIdentifier("combine.title")
                    }
                }
                Section {
                    Toggle("Leave out duplicates", isOn: $skipDuplicates)
                    Toggle("Delete the originals", isOn: $deleteSources)
                } footer: {
                    Text("Only your own playlists are deleted: ones shared with you stay.")
                }
            }
            .navigationTitle("Combine Playlists")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Combine") { combine() }
                        .disabled(!ready || working)
                        .accessibilityIdentifier("combine.go")
                }
            }
            .overlay { if working { ProgressView().controlSize(.large) } }
            .alert("Couldn't combine the playlists", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(error ?? "")
            }
        }
    }

    private func combine() {
        working = true
        let sources = picked.filter { $0 != into }
        let name = title.trimmingCharacters(in: .whitespaces)
        Task {
            defer { working = false }
            do {
                _ = try await app.mergePlaylists(sources, into: into, title: into == nil ? name : nil,
                                                 skipDuplicates: skipDuplicates, deleteSources: deleteSources)
                PlaylistChanges.shared.changed()
                dismiss()
            } catch is CancellationError {
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
#endif
