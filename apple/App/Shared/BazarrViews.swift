import MarqueeKit
import SwiftUI

/// Bazarr subtitles for a movie or episode (META-12), as List sections: the languages it has
/// and wants, a search of every provider, and a download in another language. Shown only
/// when Bazarr is set up and knows the title.
struct BazarrPanel: View {
    @Environment(AppSession.self) private var app
    let itemID: Int64
    /// Called when a requested subtitle has had time to arrive (refresh the item's tracks).
    var onArrived: () -> Void = {}
    @State private var status: BazarrStatus?
    @State private var candidates: [BazarrCandidate]?
    @State private var searching = false
    @State private var choosing = false
    @State private var busy: String?
    @State private var message: String?
    @State private var error: String?

    var body: some View {
        if let s = status, s.configured, s.managed {
            Section {
                if let message {
                    Label(message, systemImage: "checkmark.circle.fill").foregroundStyle(.green).font(.callout)
                        .accessibilityElement(children: .combine)
                        .accessibilityLabel(message)
                        .accessibilityIdentifier("bazarrMessage")
                }
                if let error { Text(error).foregroundStyle(.red).font(.callout) }
                if let e = s.error { Text(e).foregroundStyle(.orange).font(.callout) }
                ForEach(Array(s.languages.enumerated()), id: \.offset) { _, l in languageRow(l) }
                Button { search() } label: {
                    HStack {
                        Label("Search All Providers", systemImage: "magnifyingglass")
                        Spacer()
                        if searching { ProgressView() }
                    }
                }
                .disabled(searching)
                Button { choosing = true } label: {
                    Label("Download Another Language…", systemImage: "plus.bubble")
                }
                .disabled(busy != nil)
                .confirmationDialog("Download a subtitle in", isPresented: $choosing, titleVisibility: .visible) {
                    ForEach(commonSubtitleLanguages, id: \.code) { l in
                        Button(l.name) { download(code: l.code, name: l.name) }
                    }
                }
            } header: {
                Text("Bazarr")
            } footer: {
                if searching { Text("Asking every subtitle provider. This can take up to a minute.") }
            }
            if let candidates {
                Section("Search results") {
                    if candidates.isEmpty { Text("No subtitles found.").foregroundStyle(.secondary) }
                    ForEach(Array(candidates.enumerated()), id: \.offset) { _, c in candidateRow(c) }
                }
            }
        } else {
            Color.clear.frame(height: 0)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
                .task(id: itemID) { await load() }
        }
    }

    private func languageRow(_ l: Schemas.BazarrSubtitleLanguage) -> some View {
        // Bazarr sometimes names a language by its code only.
        let base = l.name.isEmpty || l.name == l.code2 || l.name == l.code3 ? languageName(l.code2) : l.name
        let name = base + (l.forced ? " (forced)" : "") + (l.hi ? " (SDH)" : "")
        return HStack {
            if l.have {
                Label(name, systemImage: "checkmark.circle.fill")
                    .accessibilityLabel("\(name), downloaded")
                Spacer()
            } else {
                let key = "lang-\(l.code2)-\(l.forced)-\(l.hi)"
                #if os(tvOS)
                Button { download(code: l.code2, name: name, forced: l.forced, hi: l.hi, key: key) } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(name)
                            Text("Wanted · Download").font(.caption).foregroundStyle(.orange)
                        }
                        Spacer()
                        if busy == key { ProgressView() } else { Image(systemName: "arrow.down.circle") }
                    }
                }
                .disabled(busy != nil)
                .accessibilityLabel("Download \(name)")
                #else
                VStack(alignment: .leading, spacing: 2) {
                    Text(name)
                    Text("Wanted").font(.caption).foregroundStyle(.orange)
                }
                Spacer()
                if busy == key {
                    ProgressView()
                } else {
                    Button("Download") { download(code: l.code2, name: name, forced: l.forced, hi: l.hi, key: key) }
                        .buttonStyle(.bordered)
                        .disabled(busy != nil)
                        .accessibilityLabel("Download \(name)")
                }
                #endif
            }
        }
    }

    private func candidateRow(_ c: BazarrCandidate) -> some View {
        let key = "pick-\(c.provider)-\(c.subtitle)"
        let info = VStack(alignment: .leading, spacing: 3) {
            Text(c.release?.isEmpty == false ? c.release! : c.provider).lineLimit(2)
            HStack(spacing: 8) {
                Text(c.provider)
                Text("Score \(c.score)")
                Text(languageName(c.language))
                if c.hi == true { Text("SDH") }
                if c.forced == true { Text("Forced") }
            }
            .font(.caption).foregroundStyle(.secondary)
        }
        #if os(tvOS)
        return Button { pick(c, key: key) } label: {
            HStack {
                info
                Spacer()
                if busy == key { ProgressView() } else { Image(systemName: "arrow.down.circle") }
            }
        }
        .disabled(busy != nil)
        .accessibilityLabel("Download from \(c.provider)")
        #else
        return HStack {
            info
            Spacer()
            if busy == key {
                ProgressView()
            } else {
                Button { pick(c, key: key) } label: { Image(systemName: "arrow.down.circle") }
                    .buttonStyle(.borderless)
                    .disabled(busy != nil)
                    .accessibilityLabel("Download from \(c.provider)")
            }
        }
        #endif
    }

    private func load() async {
        do { status = try await app.bazarrStatus(itemID) } catch { status = nil }
    }

    private func search() {
        searching = true
        error = nil
        Task {
            defer { searching = false }
            do { candidates = try await app.bazarrSearch(itemID) } catch { self.error = error.localizedDescription }
        }
    }

    private func download(code: String, name: String, forced: Bool = false, hi: Bool = false, key: String? = nil) {
        busy = key ?? "lang-\(code)"
        error = nil
        Task {
            defer { busy = nil }
            do {
                try await app.bazarrDownload(itemID, language: code, forced: forced, hi: hi)
                requested("Bazarr is fetching the \(name) subtitle. It'll appear in a moment.")
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func pick(_ c: BazarrCandidate, key: String) {
        busy = key
        error = nil
        Task {
            defer { busy = nil }
            do {
                try await app.bazarrPick(itemID, c)
                requested("Bazarr is downloading it. It'll appear in a moment.")
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    /// A 202: say it's on its way, then look again once Bazarr has had time to save it.
    private func requested(_ text: String) {
        message = text
        let arrived = onArrived
        Task {
            try? await Task.sleep(for: .seconds(20))
            arrived()
            await load()
        }
    }
}

#if os(iOS)
/// Bazarr on its own (library health's "Download with Bazarr").
struct BazarrSheet: View {
    @Environment(\.dismiss) private var dismiss
    let item: Item
    var onArrived: () -> Void = {}

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(item._type == .episode ? "\(item.grandparentTitle ?? "") · \(item.title)" : item.title).font(.headline)
                }
                BazarrPanel(itemID: item.id, onArrived: onArrived)
            }
            .navigationTitle("Subtitles")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
        }
    }
}
#endif
