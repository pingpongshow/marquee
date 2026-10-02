import MarqueeKit
import SwiftUI

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
                AsyncImage(url: app.imageURL(first, width: Int(size))) { $0.image?.resizable().scaledToFill() }
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func tile(_ i: Int) -> some View {
        AsyncImage(url: app.imageURL(playlist.imageIds[i], width: Int(size / 2))) { $0.image?.resizable().scaledToFill() }
            .frame(width: size / 2, height: size / 2).clipped()
    }
}

struct PlaylistsView: View {
    @Environment(AppSession.self) private var app
    @State private var playlists: [Playlist] = []

    var body: some View {
        ScrollView {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: cell), spacing: 16, alignment: .top)], spacing: 20) {
                ForEach(playlists, id: \.id) { p in
                    NavigationLink(value: Route.playlist(p.id)) {
                        VStack(alignment: .leading, spacing: 6) {
                            PlaylistMosaic(playlist: p, size: cell)
                            Text(p.title).font(.subheadline.weight(.medium)).lineLimit(1)
                            Text("\(p.itemCount) \(p.kind == .audio ? "tracks" : "items")").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(sidePadding)
            if playlists.isEmpty {
                ContentUnavailableView("No playlists", systemImage: "music.note.list", description: Text("Use “Add to Playlist” on any track, album, movie or episode."))
            }
        }
        .navigationTitle("Playlists")
        .task { playlists = (try? await app.playlists()) ?? [] }
        .refreshable { playlists = (try? await app.playlists()) ?? [] }
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
    let id: Int64
    @State private var playlist: Playlist?
    @State private var entries: [PlaylistEntry] = []

    var body: some View {
        List {
            if let p = playlist {
                HStack(spacing: 16) {
                    PlaylistMosaic(playlist: p, size: 120)
                    VStack(alignment: .leading, spacing: 8) {
                        Text(p.title).font(.title2.bold())
                        Text("\(p.itemCount) items · \(formatDuration(ms: p.durationMs))").foregroundStyle(.secondary)
                        HStack {
                            Button { play(0) } label: { Label("Play", systemImage: "play.fill") }.buttonStyle(.borderedProminent)
                            Button { play(0, shuffle: true) } label: { Label("Shuffle", systemImage: "shuffle") }.buttonStyle(.bordered)
                            #if os(iOS)
                            PlaylistDownloadButton(id: id)
                            #endif
                        }
                        PinToHomeButton(rowID: AppSession.pinnedRowID(playlist: id))
                    }
                }
                #if os(iOS)
                .listRowSeparator(.hidden)
                #endif
            }
            ForEach(Array(entries.enumerated()), id: \.element.entryId) { i, e in
                Button { play(i) } label: {
                    HStack(spacing: 12) {
                        ArtworkView(item: e.item, shape: e.item._type == .track ? .square : .wide, width: 60).frame(width: e.item._type == .track ? 44 : 80)
                        VStack(alignment: .leading) {
                            Text(e.item.title).lineLimit(1)
                            Text(e.item.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer()
                        Text(formatTime(seconds: Double(e.item.durationMs ?? 0) / 1000)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                .buttonStyle(.plain)
                .contextMenu { ItemMenuItems(item: e.item) }
            }
        }
        .listStyle(.plain)
        .navigationTitle(playlist?.title ?? "Playlist")
        .task { await load() }
    }

    private func load() async {
        playlist = try? await app.playlists().first { $0.id == id }
        entries = (try? await app.playlistItems(id)) ?? []
    }

    private func play(_ start: Int, shuffle: Bool = false) {
        let items = entries.map(\.item)
        guard !items.isEmpty else { return }
        if playlist?.kind == .audio {
            music.play(items, start: start, shuffle: shuffle, source: playlist?.title)
        } else {
            let first = shuffle ? items.randomElement()! : items[start]
            video.play(first.id, startMs: 0, playlistID: id)
        }
    }
}

#if os(iOS)
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
