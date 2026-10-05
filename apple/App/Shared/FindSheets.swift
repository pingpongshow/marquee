import MarqueeKit
import SwiftUI

/// Finds subtitles for a movie or episode: Bazarr when it manages the title (META-12), and
/// OpenSubtitles, adding the chosen file (PLAY-7).
struct SubtitleSearchSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let itemID: Int64
    let onAdded: (Int64) -> Void
    /// A Bazarr subtitle has had time to arrive (META-12).
    var onBazarr: () -> Void = {}
    @State private var language = "en"
    @State private var results: [Schemas.SubtitleResult]?
    @State private var error: String?
    @State private var busy: Int64?

    private let languages: [(String, String)] = [
        ("en", "English"), ("es", "Spanish"), ("fr", "French"), ("de", "German"), ("it", "Italian"), ("pt-BR", "Portuguese (Brazil)"),
        ("nl", "Dutch"), ("sv", "Swedish"), ("pl", "Polish"), ("ru", "Russian"), ("ja", "Japanese"), ("ko", "Korean"), ("zh-CN", "Chinese"),
    ]

    var body: some View {
        NavigationStack {
            List {
                // Bazarr first, when it manages this title (META-12).
                BazarrPanel(itemID: itemID, onArrived: onBazarr)
                Section("OpenSubtitles") {
                    Picker("Language", selection: $language) {
                        ForEach(languages, id: \.0) { Text($0.1).tag($0.0) }
                    }
                    #if os(tvOS)
                    .pickerStyle(.navigationLink) // a segmented row can't hold 13 languages
                    #endif
                    if let error { Text(error).foregroundStyle(.red) }
                    if let results {
                        if results.isEmpty { Text("No subtitles found in this language.").foregroundStyle(.secondary) }
                        ForEach(results, id: \.fileId) { r in
                            Button { download(r) } label: {
                                HStack {
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text(r.release.isEmpty ? (r.fileName ?? "Subtitle") : r.release).lineLimit(2)
                                        HStack(spacing: 10) {
                                            Text("\(r.downloads.formatted()) downloads")
                                            if r.hashMatch { Label("Made for this file", systemImage: "checkmark.seal.fill").foregroundStyle(.green) }
                                            if r.hearingImpaired { Text("SDH") }
                                            if r.aiTranslated { Text("Machine translated").foregroundStyle(.orange) }
                                        }
                                        .font(.caption).foregroundStyle(.secondary)
                                    }
                                    Spacer()
                                    if busy == r.fileId { ProgressView() } else { Image(systemName: "arrow.down.circle") }
                                }
                            }
                            .disabled(busy != nil)
                        }
                    } else if error == nil {
                        ProgressView()
                    }
                }
            }
            .navigationTitle("Find Subtitles")
            #if os(iOS)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
            #endif
            .task(id: language) {
                results = nil
                error = nil // the last language's error doesn't hide this one's results
                do { results = try await app.searchSubtitles(itemID, languages: language); error = nil } catch { self.error = error.localizedDescription }
            }
        }
    }

    private func download(_ r: Schemas.SubtitleResult) {
        busy = r.fileId
        Task {
            defer { busy = nil }
            do {
                let id = try await app.downloadSubtitle(itemID, result: r)
                onAdded(id)
                dismiss()
            } catch { self.error = error.localizedDescription }
        }
    }
}

/// Asks MainView to show the Sound Journey picker for a track.
@MainActor @Observable
final class JourneyPicker {
    static let shared = JourneyPicker()
    var from: Item?
}

/// Sound Journey (MUSIC-4): choose where to travel to from a track.
struct JourneySheet: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss
    let from: Item
    @State private var query = ""
    @State private var tracks: [Item] = []
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("A path from “\(from.title)” to another track, through music that sounds in between.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                if let error { Text(error).foregroundStyle(.red) }
                ForEach(tracks, id: \.id) { t in
                    Button { go(to: t) } label: {
                        VStack(alignment: .leading) {
                            Text(t.title)
                            Text(t.artistCredit ?? t.grandparentTitle ?? "").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .disabled(busy)
                }
            }
            .searchable(text: $query, prompt: "Search for the destination track")
            .navigationTitle("Sound Journey")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
            .task(id: query) {
                guard query.count >= 2 else { tracks = []; return }
                try? await Task.sleep(for: .milliseconds(250))
                guard !Task.isCancelled else { return }
                do {
                    let r = try await app.search(query, limit: 25)
                    guard !Task.isCancelled else { return }
                    tracks = r.groups.first { $0._type == .track }?.items.filter { $0.id != from.id } ?? []
                    error = nil
                } catch {
                    // Overtaken by the next keystroke: keep what's shown.
                    if !Task.isCancelled, !(error is CancellationError) { self.error = error.localizedDescription }
                }
            }
        }
    }

    private func go(to: Item) {
        busy = true
        Task {
            defer { busy = false }
            do {
                music.playStation(try await app.journey(from: from.id, to: to.id))
                dismiss()
            } catch { self.error = error.localizedDescription }
        }
    }
}
