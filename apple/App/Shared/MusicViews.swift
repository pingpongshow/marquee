import MarqueeKit
import SwiftUI

/// Muse, stations and daily mixes at the top of a music library (M6.5).
struct MusicDiscoverView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let libraryID: Int64
    @State private var status: MusicStatus?
    @State private var mixes: [Station] = []
    @State private var decades: [String] = []
    @State private var prompt = ""
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        if let status, status.enabled {
            VStack(alignment: .leading, spacing: 24) {
                if status.analyzed < status.total {
                    Label(status.available
                          ? "Listening to your music: \(status.analyzed.formatted()) of \(status.total.formatted()) tracks analysed. Radios and mixes improve as it goes."
                          : "The sonic analysis service isn't running, so radios and Muse are unavailable.",
                          systemImage: "sparkles")
                        .font(.footnote).foregroundStyle(.secondary)
                        .padding(.horizontal, sidePadding)
                }
                if let error { ErrorBanner(message: error).padding(.horizontal, sidePadding) }
                muse
                stations
                if !mixes.isEmpty {
                    ShelfRow(title: "Mixes for you") {
                        ForEach(mixes, id: \.title) { m in MixCard(station: m) { music.playStation(m) } }
                    }
                }
            }
            .disabled(busy)
        } else {
            Color.clear.frame(height: 0).task { await load() }
        }
    }

    private func load() async {
        status = try? await app.musicStatus()
        guard status?.enabled == true else { return }
        mixes = (try? await app.mixes(library: libraryID)) ?? []
        decades = ((try? await app.filters(library: libraryID, type: .album))?.decades.map(\.value) ?? []).prefix(6).map { $0 }
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
        run { music.playStation(try await app.muse(p, library: libraryID)) }
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
    @State var rating: Double?

    var body: some View {
        HStack(spacing: 6) {
            ForEach(1...5, id: \.self) { star in
                #if os(tvOS)
                Button { set(star) } label: { image(star) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(star) star\(star == 1 ? "" : "s")")
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
                    .accessibilityLabel("\(star) star\(star == 1 ? "" : "s")")
                    .accessibilityAction { pick(Double(star * 2)) }
                #endif
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Rating")
        .accessibilityValue(rating.map { String(format: "%.1f stars", $0 / 2) } ?? "Not rated")
        .accessibilityAdjustableAction { dir in
            let r = rating ?? 0
            pick(min(10, max(0, r + (dir == .increment ? 1 : -1))))
        }
    }

    private func image(_ star: Int) -> some View {
        Image(systemName: symbol(star)).foregroundStyle(symbol(star) == "star" ? Color.secondary : Color.marqueeGold)
    }

    /// Sets a rating (0–10); choosing the current value again clears it.
    private func pick(_ v: Double) {
        let next: Double? = (v == rating || v == 0) ? nil : v
        rating = next
        Task { try? await app.rate(itemID, next) }
    }

    private func symbol(_ star: Int) -> String {
        let r = rating ?? 0
        if r >= Double(star * 2) { return "star.fill" }
        if r >= Double(star * 2 - 1) { return "star.leadinghalf.filled" }
        return "star"
    }

    /// tvOS (no touch position): pressing a star gives it; pressing the same full star again
    /// makes it a half; pressing the half clears the rating.
    private func set(_ star: Int) {
        let full = Double(star * 2)
        let next: Double? = rating == full ? full - 1 : rating == full - 1 ? nil : full
        rating = next
        Task { try? await app.rate(itemID, next) }
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

/// Volume levelling choice (MUSIC-9).
struct LevellingMenu: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        @Bindable var music = music
        Menu {
            Picker("Volume levelling", selection: $music.levelling) {
                ForEach(MusicPlayer.Levelling.allCases, id: \.self) { Text($0.label).tag($0) }
            }
        } label: {
            Label("Volume levelling: \(music.levelling.label)", systemImage: "slider.vertical.3")
                .labelStyle(.iconOnly)
        }
        .accessibilityLabel("Volume levelling: \(music.levelling.label)")
    }
}

/// Guest DJ choice (MUSIC-6).
struct DJMenu: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        @Bindable var music = music
        Menu {
            Picker("Guest DJ", selection: $music.dj) {
                Text("Off").tag(MusicPlayer.DJ?.none)
                ForEach(MusicPlayer.DJ.allCases, id: \.self) { d in
                    Text(d.label).tag(MusicPlayer.DJ?.some(d))
                }
            }
        } label: {
            Label(music.dj?.label ?? "Guest DJ", systemImage: music.dj == nil ? "person.wave.2" : "person.wave.2.fill")
                .labelStyle(.iconOnly)
        }
        .accessibilityLabel(music.dj.map { "Guest DJ: \($0.label)" } ?? "Guest DJ: off")
    }
}
