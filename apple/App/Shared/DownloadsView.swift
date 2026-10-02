#if os(iOS)
import MarqueeKit
import SwiftUI

/// Everything downloaded to this device (M7, MUSIC-14). Works offline.
struct DownloadsView: View {
    @Environment(Downloads.self) private var downloads
    @Environment(VideoPresenter.self) private var video
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app

    var body: some View {
        let all = downloads.entries.values.sorted { $0.added > $1.added }
        let busy = all.filter { $0.state != .done }
        let videos = all.filter { $0.state == .done && $0.item._type != .track }
        let tracks = all.filter { $0.state == .done && $0.item._type == .track }
        List {
            if app.client == nil {
                Label("You're offline. Downloads play without the server; what you watch syncs when you're back.", systemImage: "wifi.slash")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if !busy.isEmpty {
                Section("In progress") { ForEach(busy) { row($0) } }
            }
            if !videos.isEmpty {
                Section("Movies and TV") { ForEach(videos) { row($0) } }
            }
            if !tracks.isEmpty {
                Section {
                    ForEach(tracks) { row($0) }
                } header: {
                    HStack {
                        Text("Music")
                        Spacer()
                        Button("Play all") { music.play(tracks.map(\.item), source: "Downloads") }
                        Button("Shuffle") { music.play(tracks.map(\.item), shuffle: true, source: "Downloads") }
                    }
                }
            }
            if all.isEmpty {
                ContentUnavailableView("No downloads", systemImage: "arrow.down.circle",
                                       description: Text("Download movies, episodes and music from their pages to watch and listen offline."))
            } else {
                Text("\(ByteCountFormatter.string(fromByteCount: downloads.totalBytes, countStyle: .file)) on this device")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Downloads")
    }

    private func row(_ e: Downloads.Entry) -> some View {
        Button { open(e) } label: {
            HStack(spacing: 12) {
                poster(e)
                VStack(alignment: .leading, spacing: 3) {
                    Text(e.item.title).font(.body.weight(.medium)).lineLimit(1)
                    Text(subtitle(e)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    status(e)
                }
                Spacer(minLength: 0)
            }
        }
        .buttonStyle(.plain)
        .swipeActions {
            Button("Delete", role: .destructive) { downloads.remove(e.id) }
        }
        .contextMenu {
            Button("Delete Download", systemImage: "trash", role: .destructive) { downloads.remove(e.id) }
        }
    }

    @ViewBuilder private func poster(_ e: Downloads.Entry) -> some View {
        let w: CGFloat = e.item._type == .track ? 48 : 64
        Group {
            if let url = downloads.posterURL(e.id), let img = UIImage(contentsOfFile: url.path) {
                Image(uiImage: img).resizable().scaledToFill()
            } else {
                Rectangle().fill(.secondary.opacity(0.2))
                    .overlay(Image(systemName: e.item._type == .track ? "music.note" : "film").foregroundStyle(.secondary))
            }
        }
        .frame(width: w, height: e.item._type == .track ? w : w * 1.5)
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }

    private func subtitle(_ e: Downloads.Entry) -> String {
        switch e.item._type {
        case .episode:
            [e.item.grandparentTitle, e.item.parentTitle].compactMap { $0 }.joined(separator: " · ")
        case .track:
            [e.item.artistCredit ?? e.item.grandparentTitle, e.item.parentTitle].compactMap { $0 }.joined(separator: " · ")
        default:
            [e.item.year.map(String.init), e.quality == .original ? nil : e.quality.label].compactMap { $0 }.joined(separator: " · ")
        }
    }

    @ViewBuilder private func status(_ e: Downloads.Entry) -> some View {
        switch e.state {
        case let .preparing(p):
            ProgressView(value: p) { Text("Preparing on the server… \(Int(p * 100))%").font(.caption2) }
        case let .downloading(p):
            ProgressView(value: p) { Text("Downloading… \(Int(p * 100))%").font(.caption2) }
        case .done:
            Text(ByteCountFormatter.string(fromByteCount: e.size, countStyle: .file)).font(.caption2).foregroundStyle(.tertiary)
        case let .failed(msg):
            Text(msg).font(.caption2).foregroundStyle(.red).lineLimit(2)
        }
    }

    private func open(_ e: Downloads.Entry) {
        guard e.state == .done else { return }
        if e.item._type == .track {
            let tracks = downloads.entries.values.filter { $0.state == .done && $0.item._type == .track && $0.item.parentId == e.item.parentId }
                .map(\.item).sorted { ($0.disc ?? 0, $0.index ?? 0) < ($1.disc ?? 0, $1.index ?? 0) }
            music.play(tracks, start: tracks.firstIndex { $0.id == e.id } ?? 0, source: e.item.parentTitle)
        } else {
            video.play(e.id)
        }
    }
}

/// The download control on detail pages: pick a quality, then shows progress or "Downloaded".
struct DownloadButton: View {
    @Environment(Downloads.self) private var downloads
    @Environment(AppSession.self) private var app
    let item: Item
    @State private var error: String?

    var body: some View {
        Group {
            if [.movie, .episode, .video, .track].contains(item._type) {
                single
            } else {
                many
            }
        }
        .alert("Couldn't download", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button("OK") {}
        } message: { Text(error ?? "") }
    }

    @ViewBuilder private var single: some View {
        if let e = downloads.entries[item.id] {
            Menu {
                Button("Delete Download", systemImage: "trash", role: .destructive) { downloads.remove(item.id) }
            } label: {
                switch e.state {
                case .done: Label("Downloaded", systemImage: "arrow.down.circle.fill")
                case let .preparing(p), let .downloading(p): Label("\(Int(p * 100))%", systemImage: "arrow.down.circle.dotted")
                case .failed: Label("Failed", systemImage: "exclamationmark.circle")
                }
            }
            .buttonStyle(.bordered)
        } else if item._type == .track {
            Button { start(.original) } label: { Label("Download", systemImage: "arrow.down.circle") }.buttonStyle(.bordered)
        } else {
            Menu {
                ForEach(Downloads.Quality.allCases, id: \.self) { q in Button(q.label) { start(q) } }
            } label: { Label("Download", systemImage: "arrow.down.circle") }
            .buttonStyle(.bordered)
        }
    }

    /// Seasons, shows, albums and artists download everything inside.
    @ViewBuilder private var many: some View {
        let music = item._type == .album || item._type == .artist
        if music {
            Button { startAll(.original) } label: { Label("Download", systemImage: "arrow.down.circle") }.buttonStyle(.bordered)
        } else {
            Menu {
                ForEach(Downloads.Quality.allCases, id: \.self) { q in Button(q.label) { startAll(q) } }
            } label: { Label(item._type == .season ? "Download Season" : "Download All", systemImage: "arrow.down.circle") }
            .buttonStyle(.bordered)
        }
    }

    private func start(_ q: Downloads.Quality) {
        Task {
            do { try await downloads.download(item, quality: q) } catch { self.error = error.localizedDescription }
        }
    }

    private func startAll(_ q: Downloads.Quality) {
        Task {
            guard let items = try? await app.leaves(item.id) else { error = "Couldn't list what's inside."; return }
            await downloads.download(children: items, quality: q)
        }
    }
}
#endif
