#if !os(tvOS)
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
        let failed = all.filter { if case .failed = $0.state { return true }; return false }
        let busy = all.filter { e in
            if case .failed = e.state { return false }
            return e.state != .done
        }
        let videos = all.filter { $0.state == .done && $0.item._type != .track }
        let tracks = all.filter { $0.state == .done && $0.item._type == .track }
        List {
            if app.isOffline {
                Label("You're offline. Downloads play without the server; what you watch syncs when you're back.", systemImage: "wifi.slash")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            SmartDownloadsSection()
            if !failed.isEmpty {
                Section {
                    ForEach(failed) { row($0) }
                } header: {
                    HStack {
                        Text("Couldn't download")
                        Spacer()
                        if failed.count > 1 {
                            Button("Try All Again") { Task { await downloads.retryFailed() } }
                                .accessibilityIdentifier("downloads.retryAll")
                        }
                    }
                }
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
                    .frame(maxWidth: .infinity)
            } else {
                Text("\(ByteCountFormatter.string(fromByteCount: downloads.totalBytes, countStyle: .file)) on this device")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Downloads")
    }

    private func row(_ e: Downloads.Entry) -> some View {
        HStack(spacing: 8) {
            Button { open(e) } label: {
                HStack(spacing: 12) {
                    poster(e)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(e.item.title).font(.body.weight(.medium)).lineLimit(1)
                        HStack(spacing: 4) {
                            if e.smart != nil {
                                Image(systemName: "sparkles").accessibilityLabel("Smart download")
                            }
                            Text(subtitle(e))
                        }
                        .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        status(e)
                    }
                    Spacer(minLength: 0)
                }
            }
            .buttonStyle(.plain)
            if e.item._type == .track, e.state == .done { TrackRadioButton(track: e.item) }
            if case .failed = e.state {
                Button { ActionError.run { try await downloads.retry(e.id) } } label: {
                    Label("Try Again", systemImage: "arrow.clockwise").labelStyle(.iconOnly).frame(minWidth: 36, minHeight: 36)
                }
                .buttonStyle(.borderless)
                .accessibilityLabel("Try Again")
                .accessibilityIdentifier("download.retry.\(e.id)")
            }
        }
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
            e.item.year.map(String.init) ?? ""
        }
    }

    @ViewBuilder private func status(_ e: Downloads.Entry) -> some View {
        switch e.state {
        case let .preparing(p):
            ProgressView(value: p) { Text("Preparing on the server… \(Int(p * 100))%").font(.caption2) }
        case let .downloading(p):
            ProgressView(value: p) { Text("Downloading… \(Int(p * 100))%").font(.caption2) }
        case .done:
            let quality = e.quality == .original ? nil : e.quality.label(e.item._type == .track ? .song : .video)
            Text([ByteCountFormatter.string(fromByteCount: e.size, countStyle: .file), quality].compactMap { $0 }.joined(separator: " · "))
                .font(.caption2).foregroundStyle(.tertiary)
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

/// The download control on detail pages: one tap downloads at the quality chosen in Settings,
/// then it shows progress or "Downloaded".
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
                if e.smart != nil {
                    // A smart download becomes the person's own: never removed automatically.
                    Button("Keep Download", systemImage: "pin") { downloads.markSmart(item.id, kind: nil, owner: nil) }
                }
                if case let .failed(message) = e.state {
                    Text(message)
                    Button("Try Again", systemImage: "arrow.clockwise") { retry() }
                }
                Button("Delete Download", systemImage: "trash", role: .destructive) { downloads.remove(item.id) }
            } label: {
                switch e.state {
                case .done: Label("Downloaded", systemImage: "arrow.down.circle.fill")
                case let .preparing(p), let .downloading(p): Label("\(Int(p * 100))%", systemImage: "arrow.down.circle.dotted")
                case .failed: Label("Failed", systemImage: "exclamationmark.circle")
                }
            }
            .buttonStyle(.bordered)
        } else {
            Button { start() } label: { Label("Download", systemImage: "arrow.down.circle") }.buttonStyle(.bordered)
        }
    }

    /// Seasons, shows, albums and artists download everything inside.
    private var many: some View {
        let title = item._type == .season ? "Download Season" : item._type == .show ? "Download All" : "Download"
        return Button { startAll() } label: { Label(title, systemImage: "arrow.down.circle") }.buttonStyle(.bordered)
    }

    private func start() {
        Task {
            do { try await downloads.download(item) } catch { self.error = error.localizedDescription }
        }
    }

    private func retry() {
        Task {
            do { try await downloads.retry(item.id) } catch { self.error = error.localizedDescription }
        }
    }

    private func startAll() {
        Task {
            guard let items = try? await app.leaves(item.id) else { error = "Couldn't list what's inside."; return }
            await downloads.download(children: items)
        }
    }
}

/// Settings → Downloads: the quality for songs or for videos, with a rough idea of the size of
/// each and the one recommended for this device marked. What's already downloaded stays as it is.
struct DownloadQualityView: View {
    @Environment(Downloads.self) private var downloads
    let kind: Downloads.Kind

    private var name: String { kind == .song ? "song" : "video" }

    var body: some View {
        let current = downloads.quality(kind), recommended = downloads.recommendedQuality(kind)
        Form {
            Section {
                ForEach(Downloads.Quality.allCases, id: \.self) { q in
                    Button { downloads.setQuality(q, for: kind) } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 8) {
                                    Text(q.label(kind)).foregroundStyle(.primary)
                                    if q == recommended {
                                        Text("Recommended").font(.caption2.weight(.semibold))
                                            .padding(.horizontal, 6).padding(.vertical, 2)
                                            .background(Color.marqueeGold.opacity(0.25), in: Capsule())
                                            .foregroundStyle(Color.marqueeGold)
                                    }
                                }
                                Text(q.sizeGuide(kind)).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if q == current { Image(systemName: "checkmark").foregroundStyle(.tint) }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(q == current ? .isSelected : [])
                    .accessibilityIdentifier("downloadQuality.\(name).\(q.rawValue)")
                }
            } footer: {
                Text("New \(name) downloads use this. Anything but Original is converted on the server first. What's already downloaded stays as it is.")
            }
        }
        .navigationTitle(kind == .song ? "Song Quality" : "Video Quality")
    }
}
#endif
