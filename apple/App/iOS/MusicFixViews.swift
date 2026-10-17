import MarqueeKit
import SwiftUI

/// Fix match for a music artist or album (META-3, admin): pick the MusicBrainz artist or release
/// group it is. Its details replace the current ones; fields edited by hand stay.
struct MusicMatchSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let item: Item
    /// Called with the updated item once the match is applied.
    let onDone: () -> Void
    @State private var query = ""
    @State private var artist = ""
    @State private var results: [MusicMatchCandidate]?
    @State private var busy = false
    @State private var error: String?
    @State private var confirming: MusicMatchCandidate?

    private var isAlbum: Bool { item._type == .album }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    TextField(isAlbum ? "Album title" : "Artist name", text: $query)
                        .autocorrectionDisabled().textInputAutocapitalization(.never)
                        .submitLabel(.search).onSubmit { Task { await search() } }
                        .accessibilityIdentifier("musicMatch.query")
                    if isAlbum {
                        TextField("Artist", text: $artist)
                            .autocorrectionDisabled().textInputAutocapitalization(.never)
                            .submitLabel(.search).onSubmit { Task { await search() } }
                            .accessibilityIdentifier("musicMatch.artist")
                    }
                    Button("Search") { Task { await search() } }.disabled(busy)
                } footer: {
                    Text("Choose the MusicBrainz \(isAlbum ? "album" : "artist"). Its details replace the current ones; fields you edited stay as they are. A MusicBrainz link or ID works too.")
                }
                if let error { Text(error).foregroundStyle(.red) }
                if let results {
                    Section("Matches") {
                        if results.isEmpty {
                            Text("No matches. Try a different spelling\(isAlbum ? " or clear the artist" : "").").foregroundStyle(.secondary)
                        }
                        ForEach(results, id: \.mbid) { c in
                            Button { confirming = c } label: { row(c) }
                                .disabled(busy)
                                .accessibilityIdentifier("musicMatch.candidate")
                        }
                    }
                } else if busy {
                    ProgressView()
                }
            }
            .navigationTitle("Fix Match")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
            .overlay { if busy && results != nil { ProgressView().controlSize(.large) } }
            .confirmationDialog("Match to \(confirming?.name ?? "")?", isPresented: Binding(get: { confirming != nil }, set: { if !$0 { confirming = nil } }), titleVisibility: .visible, presenting: confirming) { c in
                Button("Use This Match") { apply(c) }
                Button("Cancel", role: .cancel) {}
            } message: { c in
                Text(details(c).isEmpty ? "Its details are looked up again from MusicBrainz." : details(c))
            }
            .task {
                query = item.title
                artist = isAlbum ? (item.parentTitle ?? item.artistCredit ?? "") : ""
                await search()
            }
        }
    }

    private func row(_ c: MusicMatchCandidate) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Text(c.name).foregroundStyle(.primary)
                if let d = c.disambiguation, !d.isEmpty { Text("(\(d))").foregroundStyle(.secondary) }
                if c.current == true {
                    Label("Current", systemImage: "checkmark").font(.caption.bold()).foregroundStyle(Color.marqueeGold)
                }
            }
            let info = details(c)
            if !info.isEmpty { Text(info).font(.caption).foregroundStyle(.secondary) }
        }
    }

    private func details(_ c: MusicMatchCandidate) -> String {
        var parts: [String] = []
        if isAlbum {
            if let a = c.artist { parts.append(a) }
            if let t = c._type { parts.append(Self.typeLabel(t)) }
            if let y = c.year { parts.append(String(y)) }
            if let n = c.trackCount, n > 0 { parts.append(n == 1 ? "1 track" : "\(n) tracks") }
        } else {
            if let t = c._type { parts.append(Self.typeLabel(t)) }
            if let country = c.country { parts.append(country) }
            if let b = c.beginYear { parts.append("\(b)–\(c.endYear.map(String.init) ?? "")") }
        }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    static func typeLabel(_ t: String) -> String {
        t == "ep" ? "EP" : t.prefix(1).uppercased() + t.dropFirst()
    }

    private func search() async {
        busy = true
        defer { busy = false }
        do {
            results = try await app.musicMatchCandidates(item.id, query: query, artist: isAlbum ? artist : nil)
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func apply(_ c: MusicMatchCandidate) {
        busy = true
        Task {
            defer { busy = false }
            do {
                try await app.applyMusicMatch(item.id, mbid: c.mbid)
                onDone()
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Move a track to another album, or a new one, without editing its file (META-3, admin). Kept
/// through rescans; "Use the file's tags" undoes it.
struct MoveTrackSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let track: Item
    let onDone: () -> Void
    private enum Mode: Hashable { case existing, new }
    @State private var mode: Mode = .existing
    @State private var query = ""
    @State private var albums: [Item]?
    @State private var albumArtist = ""
    @State private var albumTitle = ""
    @State private var moved = false
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("The file isn't changed. The track keeps its plays, ratings and favourite, and stays on the album you choose when the library is scanned again.")
                        .font(.footnote).foregroundStyle(.secondary)
                    if moved {
                        Button { undo() } label: { Label("Use the File's Tags", systemImage: "arrow.uturn.backward") }
                            .disabled(busy)
                            .accessibilityIdentifier("moveTrack.useTags")
                    }
                } footer: {
                    if moved { Text("This track was moved by hand.") }
                }
                Section {
                    Picker("Move to", selection: $mode) {
                        Text("Album in Library").tag(Mode.existing)
                        Text("New Album").tag(Mode.new)
                    }
                    .pickerStyle(.segmented)
                    .accessibilityIdentifier("moveTrack.mode")
                }
                if let error { Text(error).foregroundStyle(.red) }
                if mode == .existing { existing } else { newAlbum }
            }
            .navigationTitle("Move “\(track.title)”")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } } }
            .overlay { if busy { ProgressView().controlSize(.large) } }
            .task {
                query = track.parentTitle ?? ""
                albumArtist = track.grandparentTitle ?? track.artistCredit ?? ""
                async let detail = try? app.item(track.id)
                await search()
                moved = await detail?.info.placementOverride == true
            }
        }
    }

    @ViewBuilder private var existing: some View {
        Section {
            TextField("Search albums", text: $query)
                .autocorrectionDisabled()
                .submitLabel(.search).onSubmit { Task { await search() } }
                .accessibilityIdentifier("moveTrack.query")
            Button("Search") { Task { await search() } }.disabled(busy || query.trimmingCharacters(in: .whitespaces).isEmpty)
        }
        if let albums {
            Section("Albums") {
                if albums.isEmpty { Text("No albums found. Try another title, or make a new album.").foregroundStyle(.secondary) }
                ForEach(albums, id: \.id) { a in
                    Button { place { try await app.moveTrack(track.id, toAlbum: a.id) } } label: {
                        HStack(spacing: 12) {
                            CachedImage(fill: app.imageURL(a.images?.poster, width: 96))
                                .frame(width: 44, height: 44).background(Color.secondary.opacity(0.2)).clipShape(RoundedRectangle(cornerRadius: 4))
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 6) {
                                    Text(a.title).foregroundStyle(.primary).lineLimit(1)
                                    if a.id == track.parentId { Text("Current").font(.caption.bold()).foregroundStyle(.secondary) }
                                }
                                let sub = [a.artistCredit ?? a.parentTitle, a.year.map(String.init)].compactMap { $0 }.joined(separator: " · ")
                                if !sub.isEmpty { Text(sub).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                            }
                        }
                    }
                    .disabled(busy || a.id == track.parentId)
                    .accessibilityIdentifier("moveTrack.album")
                }
            }
        }
    }

    @ViewBuilder private var newAlbum: some View {
        Section {
            TextField("Album artist", text: $albumArtist).autocorrectionDisabled()
                .accessibilityIdentifier("moveTrack.albumArtist")
            TextField("Album title", text: $albumTitle).autocorrectionDisabled()
                .accessibilityIdentifier("moveTrack.albumTitle")
        } footer: {
            Text("An album with this artist and title is used if there is one; otherwise it's created.")
        }
        Section {
            let artist = albumArtist.trimmingCharacters(in: .whitespaces)
            let title = albumTitle.trimmingCharacters(in: .whitespaces)
            Button("Move") { place { try await app.moveTrack(track.id, albumArtist: artist, album: title) } }
                .disabled(busy || artist.isEmpty || title.isEmpty)
                .accessibilityIdentifier("moveTrack.move")
        }
    }

    private func search() async {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { albums = nil; return }
        busy = true
        defer { busy = false }
        do {
            let r = try await app.search(q, limit: 50)
            albums = (r.groups.first { $0._type == .album }?.items ?? []).filter { $0.libraryId == track.libraryId }
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func undo() { place { try await app.clearTrackPlacement(track.id) } }

    private func place(_ work: @escaping @MainActor () async throws -> ItemDetail) {
        busy = true
        Task {
            defer { busy = false }
            do {
                _ = try await work()
                onDone()
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}
