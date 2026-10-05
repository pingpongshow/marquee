import MarqueeKit
import SwiftUI

/// Muse and the stations of a music library (M6.5), opened from its landing page.
struct MuseStationsView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    @State private var status: MusicStatus?
    @State private var loaded = false
    @State private var decades: [String] = []
    @State private var prompt = ""
    @State private var busy = false
    @State private var error: String?
    /// The last Muse mix started here, to offer saving it.
    @State private var museStation: Station?
    @State private var saveRequest: SavePlaylistRequest?

    var body: some View {
        ScrollView {
            if let status, status.enabled {
                VStack(alignment: .leading, spacing: 24) {
                    if status.analyzed < status.total {
                        Label(status.available
                              ? "Listening to your music: \(status.analyzed.formatted()) of \(status.total.formatted()) tracks analysed. Radios and mixes improve as it goes."
                              : "The Soundprint analysis service isn't running, so radios and Muse are unavailable.",
                              systemImage: "sparkles")
                            .font(.footnote).foregroundStyle(.secondary)
                            .padding(.horizontal, sidePadding)
                    }
                    if let error { ErrorBanner(message: error).padding(.horizontal, sidePadding) }
                    muse
                    stations
                }
                .padding(.vertical)
                .disabled(busy)
                .savePlaylistFlow($saveRequest)
            } else if loaded {
                ContentUnavailableView("Muse is off", systemImage: "wand.and.stars",
                                       description: Text("Muse and stations need the server's Soundprint analysis."))
            } else {
                ProgressView().padding(.top, 80)
            }
        }
        .navigationTitle("Muse & Stations")
        .task { await load() }
    }

    private func load() async {
        defer { loaded = true }
        status = try? await app.musicStatus()
        guard status?.enabled == true else { return }
        let f = try? await app.filters(library: libraryID, type: .album)
        decades = (f?.decades.map(\.value) ?? []).prefix(6).map { $0 }
    }

    private var muse: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("Muse", systemImage: "wand.and.stars").font(.title3.bold())
            HStack {
                TextField("Describe what you want to hear…", text: $prompt)
                    .onSubmit { runMuse(prompt) }
                    #if os(iOS)
                    .textFieldStyle(.roundedBorder)
                    .submitLabel(.go)
                    #endif
                Button { runMuse(prompt) } label: {
                    if busy { ProgressView() } else { Label("Play", systemImage: "play.fill") }
                }
                .buttonStyle(.borderedProminent)
                .disabled(prompt.trimmingCharacters(in: .whitespaces).count < 2)
            }
            if let m = museStation {
                Button {
                    var seen = Set<Int64>()
                    let ids = m.items.map(\.id).filter { seen.insert($0).inserted }.prefix(500)
                    saveRequest = SavePlaylistRequest(title: m.title, itemIDs: Array(ids))
                } label: {
                    Label("Save “\(m.title)” as Playlist", systemImage: "text.badge.plus").lineLimit(1)
                }
                .buttonStyle(.bordered)
                .font(.footnote)
                .accessibilityIdentifier("saveMuse")
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: chipSpacing) {
                    ForEach(museSuggestions, id: \.self) { s in
                        Button(s) { prompt = s; runMuse(s) }.buttonStyle(.bordered).font(.footnote)
                    }
                }
                #if os(tvOS)
                .padding(.vertical, 20)
                #endif
            }
            #if os(tvOS)
            .scrollClipDisabled()
            #endif
        }
        .padding(.horizontal, sidePadding)
    }

    private var stations: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Stations").font(.title3.bold()).padding(.horizontal, sidePadding)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: chipSpacing) {
                    chip("Library Radio", "dot.radiowaves.left.and.right", .init(seed: .library, libraryId: libraryID))
                    chip("Favourites Radio", "heart", .init(seed: .favourites, libraryId: libraryID))
                    ForEach(musicMoods, id: \.self) { m in chip(m, "waveform", .init(seed: .mood, value: m.lowercased(), libraryId: libraryID)) }
                    ForEach(decades, id: \.self) { d in chip("\(d)s", "radio", .init(seed: .decade, value: d, libraryId: libraryID)) }
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

    private func chip(_ title: String, _ icon: String, _ req: RadioRequest) -> some View {
        Button { startRadio(req) } label: { Label(title, systemImage: icon) }
            .buttonStyle(.bordered)
            .font(.footnote)
    }

    #if os(tvOS)
    private let chipSpacing: CGFloat = 24
    #else
    private let chipSpacing: CGFloat = 8
    #endif

    private func runMuse(_ text: String) {
        let p = text.trimmingCharacters(in: .whitespaces)
        guard p.count > 1, !busy else { return }
        run {
            let station = try await app.muse(p, library: libraryID)
            music.playStation(station)
            museStation = station.items.isEmpty ? nil : station
        }
    }

    private func startRadio(_ req: RadioRequest) {
        guard !busy else { return }
        run {
            var r = req
            r.limit = 50
            music.playStation(try await app.radio(r), radio: r)
        }
    }

    private func run(_ work: @escaping () async throws -> Void) {
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do { try await work() } catch { self.error = error.localizedDescription }
        }
    }
}

/// A daily mix: a tinted square with its name; plays the mix.
struct MixCard: View {
    let station: Station
    let action: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button(action: action) {
                ZStack {
                    LinearGradient(colors: [hue.opacity(0.9), hue.opacity(0.35)], startPoint: .topLeading, endPoint: .bottomTrailing)
                    Image(systemName: "dot.radiowaves.left.and.right").font(.system(size: width / 4)).foregroundStyle(.white.opacity(0.85))
                }
                .frame(width: width, height: width)
                .clipShape(RoundedRectangle(cornerRadius: 8))
            }
            #if os(tvOS)
            .buttonStyle(.card)
            #else
            .buttonStyle(.plain)
            #endif
            .accessibilityLabel("Play \(station.title)")
            Text(station.title).font(.subheadline.weight(.semibold)).lineLimit(1)
            if let d = station.description { Text(d).font(.caption).foregroundStyle(.secondary).lineLimit(2) }
        }
        .frame(width: width)
    }

    private var hue: Color {
        let h = Double(station.title.unicodeScalars.reduce(0) { ($0 * 31 + Int($1.value)) % 360 }) / 360 // stable across launches
        return Color(hue: h, saturation: 0.55, brightness: 0.6)
    }

    #if os(tvOS)
    private let width: CGFloat = 250
    #else
    private let width: CGFloat = 130
    #endif
}

/// Five stars (half-star steps on iPhone/iPad by tapping a star twice); 10 = loved (MUSIC-11).
struct RatingStars: View {
    @Environment(AppSession.self) private var app
    let itemID: Int64
    /// The rating the server sent; this session's changes (here or on a row) win.
    let rating: Double?
    /// Put before each star's spoken name ("Rate 3 stars" on item pages).
    var labelPrefix = ""

    private var current: Double? { RatingStore.shared.rating(itemID, rating) }

    var body: some View {
        HStack(spacing: 6) {
            ForEach(1...5, id: \.self) { star in
                #if os(tvOS)
                Button { set(star) } label: { image(star) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(labelPrefix)\(star) star\(star == 1 ? "" : "s")")
                #else
                // The left half of a star gives a half star.
                image(star)
                    .contentShape(Rectangle())
                    .overlay {
                        GeometryReader { g in
                            Color.clear.contentShape(Rectangle())
                                .onTapGesture(coordinateSpace: .local) { p in
                                    pick(p.x < g.size.width / 2 ? Double(star * 2 - 1) : Double(star * 2))
                                }
                        }
                    }
                    .accessibilityAddTraits(.isButton)
                    .accessibilityLabel("\(labelPrefix)\(star) star\(star == 1 ? "" : "s")")
                    .accessibilityAction { pick(Double(star * 2)) }
                #endif
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Rating")
        .accessibilityValue(current.map { String(format: "%.1f stars", $0 / 2) } ?? "Not rated")
        .accessibilityAdjustableAction { dir in
            let r = current ?? 0
            pick(min(10, max(0, r + (dir == .increment ? 1 : -1))))
        }
    }

    private func image(_ star: Int) -> some View {
        Image(systemName: symbol(star)).foregroundStyle(symbol(star) == "star" ? Color.secondary : Color.marqueeGold)
    }

    /// Sets a rating (0–10); choosing the current value again clears it.
    private func pick(_ v: Double) {
        let old = current
        let next: Double? = (v == old || v == 0) ? nil : v
        RatingStore.shared.rate(itemID, next, was: old, app: app)
    }

    private func symbol(_ star: Int) -> String {
        let r = current ?? 0
        if r >= Double(star * 2) { return "star.fill" }
        if r >= Double(star * 2 - 1) { return "star.leadinghalf.filled" }
        return "star"
    }

    /// tvOS (no touch position): pressing a star gives it; pressing the same full star again
    /// makes it a half; pressing the half clears the rating.
    private func set(_ star: Int) {
        let full = Double(star * 2)
        let old = current
        let next: Double? = old == full ? full - 1 : old == full - 1 ? nil : full
        RatingStore.shared.rate(itemID, next, was: old, app: app)
    }
}

/// Lyrics for the playing track; synced lyrics follow the music.
struct LyricsView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let itemID: Int64
    @State private var lyrics: Lyrics?
    @State private var loaded = false

    var body: some View {
        Group {
            if let lyrics, !lyrics.lines.isEmpty {
                let active = lyrics.activeLine(at: music.time)
                ScrollViewReader { proxy in
                    ScrollView {
                        VStack(alignment: .leading, spacing: lineSpacing) {
                            ForEach(lyrics.parsedLines) { l in
                                Text(l.text.isEmpty ? "♪" : l.text)
                                    .font(lineFont)
                                    .foregroundStyle(!lyrics.synced || l.id == active ? Color.primary : Color.secondary.opacity(0.6))
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .id(l.id)
                                    #if os(iOS)
                                    .onTapGesture { if let t = l.time { music.seek(t) } }
                                    #endif
                            }
                            Text(sourceLabel(lyrics.source)).font(.caption2).foregroundStyle(.tertiary).padding(.top)
                        }
                        .padding(.vertical, 40)
                    }
                    .onChange(of: active) {
                        guard let active else { return }
                        withAnimation(.easeInOut(duration: 0.4)) { proxy.scrollTo(active, anchor: .center) }
                    }
                }
            } else if loaded {
                ContentUnavailableView("No lyrics", systemImage: "quote.bubble", description: Text("This track has no lyrics in its file or a .lrc file next to it."))
            } else {
                ProgressView()
            }
        }
        .task(id: itemID) {
            loaded = false
            lyrics = nil // the last track's lyrics don't linger
            lyrics = try? await app.lyrics(itemID)
            loaded = true
        }
    }

    private func sourceLabel(_ s: Lyrics.SourcePayload) -> String {
        switch s {
        case .embedded: "Lyrics: from the file"
        case .sidecar: "Lyrics: .lrc file"
        case .lrclib: "Lyrics: LRCLIB"
        }
    }

    #if os(tvOS)
    private let lineFont = Font.title2.bold()
    private let lineSpacing: CGFloat = 22
    #else
    private let lineFont = Font.title3.bold()
    private let lineSpacing: CGFloat = 14
    #endif
}

/// Sleep timer choices.
struct SleepMenu: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        Menu {
            ForEach([15, 30, 45, 60, 90], id: \.self) { m in
                Button("\(m) minutes") { music.sleep = .at(Date().addingTimeInterval(Double(m) * 60)) }
            }
            Button("End of track") { music.sleep = .endOfTrack }
            if music.sleep != nil { Button("Turn off", role: .destructive) { music.sleep = nil } }
        } label: {
            Label(label, systemImage: music.sleep == nil ? "moon.zzz" : "moon.zzz.fill")
        }
        .accessibilityLabel(label)
    }

    private var label: String {
        switch music.sleep {
        case nil: "Sleep timer"
        case .endOfTrack: "Stops after this track"
        case let .at(d): "Stops at \(d.formatted(date: .omitted, time: .shortened))"
        }
    }
}

/// DJ choice (MUSIC-6).
struct DJMenu: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        @Bindable var music = music
        Menu {
            Picker("DJ", selection: $music.dj) {
                Text("Off").tag(MusicPlayer.DJ?.none)
                ForEach(MusicPlayer.DJ.allCases, id: \.self) { d in
                    Text(d.label).tag(MusicPlayer.DJ?.some(d))
                }
            }
        } label: {
            Label(music.dj.map { "DJ: \($0.label)" } ?? "DJ", systemImage: music.dj == nil ? "person.wave.2" : "person.wave.2.fill")
                .labelStyle(.iconOnly)
        }
        .accessibilityLabel(music.dj.map { "DJ: \($0.label)" } ?? "DJ: off")
    }
}

/// Moods and styles as tiles (MUSIC-18); each opens its page.
struct MoodsAndStyles: View {
    let libraryID: Int64
    let styles: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            section("Moods", musicMoods, kind: .mood)
            if !styles.isEmpty { section("Styles", styles, kind: .style) }
        }
    }

    private func section(_ title: String, _ names: [String], kind: MoodStyleView.Kind) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title).font(.title3.bold()).padding(.horizontal, sidePadding)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: tileSpacing) {
                    ForEach(names, id: \.self) { n in
                        NavigationLink(value: Route.moodStyle(library: libraryID, mood: kind == .mood, name: n)) {
                            Text(n).font(.subheadline.weight(.semibold)).foregroundStyle(.white)
                                .frame(width: tileWidth, height: tileWidth * 0.55, alignment: .bottomLeading)
                                .padding(10)
                                .background(LinearGradient(colors: [tint(n), tint(n).opacity(0.4)], startPoint: .topLeading, endPoint: .bottomTrailing),
                                            in: RoundedRectangle(cornerRadius: 10))
                        }
                        #if os(tvOS)
                        .buttonStyle(.card)
                        #else
                        .buttonStyle(.plain)
                        #endif
                        .accessibilityLabel("\(n) \(kind == .mood ? "mood" : "style")")
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

    private func tint(_ s: String) -> Color {
        let h = Double(s.unicodeScalars.reduce(7) { ($0 * 37 + Int($1.value)) % 360 }) / 360
        return Color(hue: h, saturation: 0.6, brightness: 0.55)
    }

    #if os(tvOS)
    private let tileWidth: CGFloat = 220
    private let tileSpacing: CGFloat = 30
    #else
    private let tileWidth: CGFloat = 120
    private let tileSpacing: CGFloat = 10
    #endif
}

/// A mood or style page: its radio, albums and a track sampler (MUSIC-18).
struct MoodStyleView: View {
    enum Kind { case mood, style }
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    let kind: Kind
    let name: String
    @State private var tracks: [Item] = []
    @State private var albums: [Item] = []
    @State private var error: String?
    @State private var loaded = false

    private var request: RadioRequest {
        kind == .mood ? .init(seed: .mood, value: name.lowercased(), libraryId: libraryID, limit: 30)
            : .init(seed: .genre, value: name, libraryId: libraryID, limit: 30)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                Text(kind == .mood ? "MOOD" : "STYLE").font(.caption.weight(.semibold)).foregroundStyle(Color.marqueeGold)
                    .padding(.horizontal, sidePadding)
                Button { startRadio() } label: { Label("Play \(name) Radio", systemImage: "dot.radiowaves.left.and.right") }
                    .buttonStyle(.borderedProminent)
                    .padding(.horizontal, sidePadding)
                if let error { Text(error).foregroundStyle(.red).padding(.horizontal, sidePadding) }
                if !albums.isEmpty {
                    ShelfRow(title: kind == .mood ? "Albums with This Feel" : "Albums") {
                        ForEach(albums, id: \.id) { PosterCard(item: $0) }
                    }
                }
                if !tracks.isEmpty {
                    VStack(alignment: .leading, spacing: 0) {
                        Text("Tracks").font(.title3.bold()).padding(.bottom, 8)
                        ForEach(Array(tracks.enumerated()), id: \.element.id) { i, t in
                            HStack(spacing: 4) {
                                Button { music.play(tracks, start: i, source: name) } label: {
                                    HStack {
                                        Text("\(i + 1)").font(.caption.monospacedDigit()).foregroundStyle(.secondary).frame(width: 24, alignment: .trailing)
                                        VStack(alignment: .leading) {
                                            Text(t.title).lineLimit(1)
                                            Text([t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · "))
                                                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                                        }
                                        Spacer()
                                    }
                                    .padding(.vertical, 6)
                                    .contentShape(Rectangle())
                                }
                                .buttonStyle(.plain)
                                RowRating(item: t)
                            }
                        }
                    }
                    .padding(.horizontal, sidePadding)
                } else if !loaded {
                    ProgressView().frame(maxWidth: .infinity).padding()
                } else if albums.isEmpty, error == nil {
                    ContentUnavailableView("Nothing here yet", systemImage: kind == .mood ? "theatermasks" : "guitars",
                                           description: Text(kind == .mood ? "No tracks have this feel yet. Analysis may still be running."
                                                                           : "No tracks have this style."))
                }
                if loaded, error != nil {
                    Button("Try Again") { Task { await load() } }.buttonStyle(.bordered).padding(.horizontal, sidePadding)
                }
            }
            .padding(.vertical)
        }
        .navigationTitle(name)
        // Once: coming back from an album doesn't reshuffle the station.
        .task { if !loaded { await load() } }
    }

    private func startRadio() {
        Task {
            do {
                var r = request
                r.limit = 50
                music.playStation(try await app.radio(r), radio: r)
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func load() async {
        defer { loaded = true }
        error = nil
        do {
            tracks = try await app.radio(request).items
            if kind == .style {
                albums = try await app.items(library: libraryID, sort: ._hyphen_rating, limit: 40, genre: name, type: .album).items
            } else {
                // The station's albums, in order of appearance.
                var seen = Set<Int64>()
                albums = tracks.compactMap { t in
                    guard let p = t.parentId, seen.insert(p).inserted else { return nil }
                    var album = t
                    album.id = p
                    album._type = .album
                    album.title = t.parentTitle ?? t.title
                    album.parentTitle = t.artistCredit ?? t.grandparentTitle
                    return album
                }
            }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// Crossfade: off or 2–12 seconds (MUSIC-9); albums played in order stay gapless.
struct CrossfadeMenu: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        @Bindable var music = music
        Menu {
            Picker("Crossfade", selection: $music.crossfade) {
                Text("Off").tag(0)
                ForEach([2, 4, 6, 8, 12], id: \.self) { Text("\($0) seconds").tag($0) }
            }
        } label: {
            Label(music.crossfade == 0 ? "Crossfade off" : "Crossfade \(music.crossfade) seconds", systemImage: "arrow.left.arrow.right")
                .labelStyle(.iconOnly)
                .foregroundStyle(music.crossfade == 0 ? Color.secondary : Color.marqueeGold)
        }
        .accessibilityLabel(music.crossfade == 0 ? "Crossfade off" : "Crossfade \(music.crossfade) seconds")
    }
}
