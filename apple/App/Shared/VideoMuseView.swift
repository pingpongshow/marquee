import MarqueeKit
import SwiftUI

/// Muse for movies and shows (USER-15): what was asked, how Muse read it, and the matches.
@MainActor @Observable
final class VideoMuse {
    var prompt = ""
    private(set) var result: MuseVideoResult?
    private(set) var asked: String?
    private(set) var busy = false
    private(set) var error: String?
    private(set) var saved: String?

    func run(_ app: AppSession, library: Int64?) {
        let p = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard p.count >= 2, !busy else { return }
        busy = true
        error = nil
        saved = nil
        Task {
            defer { busy = false }
            do {
                result = try await app.museVideo(p, library: library)
                asked = p
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    /// The results as a video playlist named after the prompt.
    func save(_ app: AppSession) {
        guard let r = result, let asked, !r.items.isEmpty else { return }
        Task {
            do {
                let ids = r.items.map(\.id)
                let p = try await app.createPlaylist(title: "Muse: \(asked)", kind: .video, items: ids)
                saved = "Saved “\(p.title)”"
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// The Muse page: a prompt with example chips, then the results.
struct VideoMuseView: View {
    @Environment(AppSession.self) private var app
    var libraryID: Int64?
    @State private var muse = VideoMuse()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                promptField
                VideoMuseChips(muse: muse, libraryID: libraryID)
                VideoMuseResults(muse: muse)
            }
            .padding(.vertical)
        }
        .navigationTitle("Muse")
    }

    private var promptField: some View {
        @Bindable var muse = muse
        return HStack {
            TextField("Describe what you want to watch", text: $muse.prompt)
                .onSubmit { muse.run(app, library: libraryID) }
                #if os(iOS)
                .textFieldStyle(.roundedBorder)
                .submitLabel(.search)
                #endif
                .accessibilityIdentifier("musePrompt")
            Button { muse.run(app, library: libraryID) } label: {
                if muse.busy { ProgressView() } else { Label("Ask Muse", systemImage: "sparkles") }
            }
            .buttonStyle(.borderedProminent)
            .disabled(muse.busy || muse.prompt.trimmingCharacters(in: .whitespaces).count < 2)
        }
        .padding(.horizontal, sidePadding)
    }
}

/// Example prompts; choosing one asks Muse straight away.
struct VideoMuseChips: View {
    @Environment(AppSession.self) private var app
    let muse: VideoMuse
    var libraryID: Int64?

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: isTV ? 24 : 8) {
                ForEach(videoMuseSuggestions, id: \.self) { s in
                    Button(s) {
                        muse.prompt = s
                        muse.run(app, library: libraryID)
                    }
                    .buttonStyle(.bordered)
                    .font(.footnote)
                }
            }
            .padding(.horizontal, sidePadding)
            #if os(tvOS)
            .padding(.vertical, 20)
            #endif
        }
        #if os(tvOS)
        .scrollClipDisabled()
        #endif
    }
}

/// How Muse read the prompt, a note while it's still learning the library, and the matches.
struct VideoMuseResults: View {
    @Environment(AppSession.self) private var app
    let muse: VideoMuse

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if let e = muse.error { ErrorBanner(message: e).padding(.horizontal, sidePadding) }
            if let r = muse.result {
                VStack(alignment: .leading, spacing: 6) {
                    Label(r.understood, systemImage: "sparkles").font(.callout.weight(.semibold)).foregroundStyle(Color.marqueeGold)
                        .accessibilityIdentifier("museUnderstood")
                    if let a = r.analysed, a < 1 {
                        Text("Muse is still learning your library (\(Int((a * 100).rounded()))%)").font(.caption).foregroundStyle(.secondary)
                    }
                }
                .padding(.horizontal, sidePadding)
                if r.items.isEmpty {
                    ContentUnavailableView("Nothing matched", systemImage: "sparkles", description: Text("Try describing it another way."))
                } else {
                    HStack {
                        Text("\(r.items.count) \(r.items.count == 1 ? "match" : "matches")").font(.headline)
                        Spacer()
                        if let s = muse.saved {
                            Label(s, systemImage: "checkmark.circle.fill").font(.caption).foregroundStyle(.green).lineLimit(1)
                                .accessibilityElement(children: .combine)
                                .accessibilityIdentifier("museSaved")
                        } else {
                            Button { muse.save(app) } label: { Label("Save as Playlist", systemImage: "text.badge.plus") }
                                .buttonStyle(.bordered)
                        }
                    }
                    .padding(.horizontal, sidePadding)
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: width, maximum: width * 1.4), spacing: gap, alignment: .top)], spacing: gap) {
                        ForEach(r.items, id: \.id) { PosterCard(item: $0, width: width) }
                    }
                    .padding(.horizontal, sidePadding)
                }
            }
        }
    }

    #if os(tvOS)
    private let width: CGFloat = 230
    private let gap: CGFloat = 50
    #else
    private let width: CGFloat = 110
    private let gap: CGFloat = 14
    #endif
}
