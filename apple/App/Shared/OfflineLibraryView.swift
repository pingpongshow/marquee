#if os(iOS)
import MarqueeKit
import SwiftUI

/// The bar along the top while the server can't be reached.
struct OfflineBanner: View {
    var body: some View {
        Label("Offline: showing downloads", systemImage: "wifi.slash")
            .font(.footnote.weight(.semibold))
            .lineLimit(1)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 6)
            .background { Color.marqueeGold.opacity(0.2).ignoresSafeArea(edges: .top) }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("offlineBanner")
    }
}

extension View {
    /// The bar above a screen while offline, inside the safe area and moving the screen down
    /// rather than covering it. Stacked, not a safe-area inset: an inset doesn't reach the pages
    /// in a TabView, whose navigation bars stayed under the bar.
    func offlineBanner() -> some View {
        modifier(OfflineBannerLayout())
    }
}

private struct OfflineBannerLayout: ViewModifier {
    @Environment(AppSession.self) private var app

    func body(content: Content) -> some View {
        VStack(spacing: 0) {
            if app.isOffline { OfflineBanner() }
            content
        }
        .animation(.easeInOut(duration: 0.2), value: app.isOffline)
    }
}

/// What takes the place of the tabs while the server can't be reached (iPhone/iPad): what's
/// downloaded, and Settings.
struct OfflineMainView: View {
    var body: some View {
        TabView {
            Tab("Downloads", systemImage: "arrow.down.circle") {
                NavigationStack {
                    OfflineLibraryView()
                        .miniPlayerSpace()
                        .navigationDestination(for: OfflineRoute.self) { route in
                            OfflineRouteView(route: route).miniPlayerSpace()
                        }
                }
                .overlay(alignment: .bottom) { MiniPlayerBar() }
            }
            Tab("Settings", systemImage: "gearshape") {
                NavigationStack {
                    SettingsView()
                        .miniPlayerSpace()
                        .marqueeDestinations()
                }
                .overlay(alignment: .bottom) { MiniPlayerBar() }
            }
        }
        .tabViewStyle(.sidebarAdaptable)
    }
}

/// Pages of the Offline Library. Its own, not Route: those pages need the server.
enum OfflineRoute: Hashable {
    case show(Int64)
    case album(Int64)
    case playlist(Int64)
    case songs
}

/// Only downloaded content, all of which plays from the device.
struct OfflineLibraryView: View {
    @Environment(Downloads.self) private var downloads

    var body: some View {
        let library = downloads.offlineLibrary
        List {
            if !library.movies.isEmpty {
                Section("Movies") {
                    ForEach(library.movies, id: \.id) { OfflineVideoRow(item: $0) }
                }
            }
            if !library.shows.isEmpty {
                Section("Shows") {
                    ForEach(library.shows) { show in
                        NavigationLink(value: OfflineRoute.show(show.id)) {
                            OfflineGroupRow(group: show, shape: .poster, count: "\(show.items.count) \(show.items.count == 1 ? "episode" : "episodes")")
                        }
                    }
                }
            }
            if !library.albums.isEmpty {
                Section("Albums") {
                    ForEach(library.albums) { album in
                        NavigationLink(value: OfflineRoute.album(album.id)) {
                            OfflineGroupRow(group: album, shape: .square, count: "\(album.items.count) \(album.items.count == 1 ? "song" : "songs")")
                        }
                    }
                }
            }
            if !library.songs.isEmpty {
                Section("Songs") {
                    NavigationLink(value: OfflineRoute.songs) {
                        LabeledContent("All Songs", value: "\(library.songs.count)")
                    }
                    .accessibilityIdentifier("offline.songs")
                }
            }
            if !library.playlists.isEmpty {
                Section("Playlists") {
                    ForEach(library.playlists) { p in
                        NavigationLink(value: OfflineRoute.playlist(p.id)) {
                            LabeledContent(p.title, value: "\(p.items.count)")
                        }
                    }
                }
            }
        }
        .overlay {
            if library.isEmpty {
                ContentUnavailableView("Nothing downloaded", systemImage: "arrow.down.circle",
                                       description: Text("Movies, episodes and music you download show here when the server can't be reached."))
            }
        }
        .navigationTitle("Offline Library")
    }
}

/// A page of the Offline Library, looked up afresh so a download deleted there goes at once.
private struct OfflineRouteView: View {
    @Environment(Downloads.self) private var downloads
    let route: OfflineRoute

    var body: some View {
        let library = downloads.offlineLibrary
        switch route {
        case let .show(id):
            OfflineShowView(show: library.shows.first { $0.id == id })
        case let .album(id):
            if let album = library.albums.first(where: { $0.id == id }) {
                OfflineTracksView(title: album.title, tracks: album.items)
            }
        case let .playlist(id):
            if let p = library.playlists.first(where: { $0.id == id }) {
                if p.isAudio {
                    OfflineTracksView(title: p.title, tracks: p.items)
                } else {
                    List { ForEach(p.items, id: \.id) { OfflineVideoRow(item: $0) } }
                        .navigationTitle(p.title)
                }
            }
        case .songs:
            OfflineTracksView(title: "All Songs", tracks: library.songs)
        }
    }
}

private struct OfflineGroupRow: View {
    let group: OfflineLibrary.Group
    let shape: PosterShape
    let count: String

    var body: some View {
        HStack(spacing: 12) {
            ArtworkView(item: group.items[0], shape: shape, width: 48).frame(width: 48)
            VStack(alignment: .leading, spacing: 2) {
                Text(group.title).lineLimit(1)
                Text([group.subtitle, count].compactMap { $0 }.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }
}

/// A show's downloaded episodes, by season.
private struct OfflineShowView: View {
    let show: OfflineLibrary.Group?

    var body: some View {
        let episodes = show?.items ?? []
        // Already in season order: each season's episodes are together.
        let seasons = episodes.reduce(into: [(name: String, items: [Item])]()) { list, e in
            let name = e.parentTitle ?? ""
            if list.last?.name == name { list[list.count - 1].items.append(e) } else { list.append((name, [e])) }
        }
        List {
            ForEach(seasons, id: \.name) { season in
                Section(season.name) {
                    ForEach(season.items, id: \.id) { OfflineVideoRow(item: $0) }
                }
            }
        }
        .navigationTitle(show?.title ?? "")
    }
}

/// A movie or episode on the device: plays where it was left.
private struct OfflineVideoRow: View {
    @Environment(VideoPresenter.self) private var video
    @Environment(Downloads.self) private var downloads
    let item: Item

    var body: some View {
        Button { video.play(item.id) } label: {
            HStack(spacing: 12) {
                ArtworkView(item: item, shape: .poster, width: 48).frame(width: 48)
                VStack(alignment: .leading, spacing: 2) {
                    Text(item._type == .episode ? "\(item.index ?? 0). \(item.title)" : item.title).lineLimit(2)
                    Text(formatDuration(ms: item.durationMs)).font(.caption).foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .swipeActions {
            Button("Delete", role: .destructive) { downloads.remove(item.id) }
        }
        .accessibilityIdentifier("offline.video.\(item.id)")
    }
}

/// Songs on the device (an album, a playlist, or all of them), with Play and Shuffle. Hearts and
/// stars given here wait to reach the server (USER-18).
private struct OfflineTracksView: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(Downloads.self) private var downloads
    let title: String
    let tracks: [Item]

    var body: some View {
        List {
            HStack(spacing: 10) {
                Button { music.play(tracks, source: title) } label: { Label("Play", systemImage: "play.fill") }
                    .buttonStyle(.borderedProminent)
                    .accessibilityIdentifier("offline.play")
                Button { music.play(tracks, shuffle: true, source: title) } label: { Label("Shuffle", systemImage: "shuffle") }
                    .buttonStyle(.bordered)
            }
            // In a list a label's icon takes the accent colour: gold on Play's gold.
            .labelStyle(AdaptiveLabelStyle(iconOnly: false))
            .listRowSeparator(.hidden)
            ForEach(Array(tracks.enumerated()), id: \.element.id) { i, t in
                HStack(spacing: 4) {
                    Button { music.play(tracks, start: i, source: title) } label: {
                        HStack(spacing: 12) {
                            ArtworkView(item: t, shape: .square, width: 44).frame(width: 44)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(t.title).lineLimit(1).foregroundStyle(music.current?.item.id == t.id ? Color.marqueeGold : .primary)
                                Text(t.artistCredit ?? t.grandparentTitle ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer(minLength: 0)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("offline.song.\(t.id)")
                    RowFavorite(item: t)
                    RowRating(item: t, gap: 4)
                }
                .swipeActions {
                    Button("Delete", role: .destructive) { downloads.remove(t.id) }
                }
            }
        }
        .listStyle(.plain)
        .navigationTitle(title)
    }
}
#endif
