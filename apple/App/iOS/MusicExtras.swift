import MarqueeKit
import SwiftUI

/// The music equaliser (iPhone/iPad): on/off, presets and ten bands of ±12 dB, kept on this
/// device. Opened from Now Playing.
struct EqualizerSheet: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let eq = music.equalizer
        NavigationStack {
            Form {
                Section {
                    Toggle("Equaliser", isOn: Binding(get: { eq.enabled }, set: { eq.enabled = $0 }))
                    if let note = music.equalizerNote {
                        Label(note, systemImage: "info.circle").font(.footnote).foregroundStyle(.secondary)
                            .accessibilityIdentifier("eqNote")
                    }
                }
                Section("Preset") {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(Equalizer.presets) { p in
                                Button(p.name) { eq.apply(p) }
                                    .buttonStyle(.bordered)
                                    .tint(eq.preset == p.name ? Color.marqueeGold : .secondary)
                                    .accessibilityAddTraits(eq.preset == p.name ? .isSelected : [])
                            }
                        }
                        .padding(.vertical, 4)
                    }
                    if eq.preset == Equalizer.custom { Text("Custom").font(.footnote).foregroundStyle(Color.marqueeGold) }
                }
                Section {
                    ForEach(Array(Equalizer.bands.enumerated()), id: \.offset) { i, hz in
                        HStack(spacing: 12) {
                            Text(Equalizer.label(hz)).font(.caption.monospacedDigit()).frame(width: 34, alignment: .trailing)
                            Slider(value: Binding(get: { eq.gains[i] }, set: { eq.setGain(i, $0) }), in: -Equalizer.maxGain...Equalizer.maxGain)
                                .accessibilityLabel("\(Equalizer.label(hz)) hertz")
                                .accessibilityValue(String(format: "%+.1f decibels", eq.gains[i]))
                            Text(String(format: "%+.1f", eq.gains[i])).font(.caption.monospacedDigit()).frame(width: 40, alignment: .trailing)
                        }
                    }
                } header: {
                    Text("Bands (dB)")
                } footer: {
                    Text("Applies to music played on this device. Gapless albums and volume levelling keep working.")
                }
                .disabled(!eq.enabled)
            }
            .navigationTitle("Equaliser")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }
}

/// The "EQ" button in Now Playing.
struct EqualizerButton: View {
    @Environment(MusicPlayer.self) private var music
    @State private var open = false

    var body: some View {
        Button { open = true } label: {
            Text("EQ").font(.callout.weight(.heavy))
                .foregroundStyle(music.equalizer.enabled ? Color.marqueeGold : Color.secondary)
        }
        .accessibilityLabel(music.equalizer.enabled ? "Equaliser on" : "Equaliser")
        .sheet(isPresented: $open) { EqualizerSheet().presentationDetents([.large]) }
    }
}

/// Car mode: big artwork, huge transport buttons and quick-start tiles, on a
/// black screen that stays awake. Works in either orientation.
struct CarModeView: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    /// The music library for the tiles (nil: the first one).
    var libraryID: Int64?
    @State private var library: Int64?
    @State private var mix: Station?
    @State private var busy: String?
    @State private var error: String?
    @State private var liked: [Int64: Bool] = [:]
    /// Button scale for the space there is (the transport row is 420 pt at full size).
    @State private var scale: CGFloat = 1
    @State private var tileHeight: CGFloat = 64

    var body: some View {
        GeometryReader { g in
            let wide = g.size.width > g.size.height
            let room = wide ? g.size.width * 0.55 : g.size.width - 40
            ZStack {
                Color.black.ignoresSafeArea()
                VStack(spacing: wide ? 10 : 22) {
                    HStack {
                        Text("CAR MODE").font(.caption.weight(.heavy)).foregroundStyle(Color.marqueeGold)
                        Spacer()
                        Button { dismiss() } label: {
                            Image(systemName: "xmark").font(.title2.bold()).foregroundStyle(.white)
                                .frame(width: 60, height: 60).background(Color.white.opacity(0.15), in: Circle())
                        }
                        .accessibilityLabel("Exit car mode")
                    }
                    if wide {
                        HStack(spacing: 28) {
                            nowPlaying(art: min(g.size.height * 0.45, 220))
                            VStack(spacing: 18) {
                                controls
                                tiles
                            }
                        }
                    } else {
                        nowPlaying(art: min(g.size.width * 0.72, 340))
                        controls
                        Spacer(minLength: 0)
                        tiles
                    }
                    if let error { Text(error).font(.callout.bold()).foregroundStyle(.red).lineLimit(2) }
                }
                .padding(.horizontal, 20)
                .padding(.vertical, 8)
            }
            .onAppear { fit(room, wide) }
            .onChange(of: g.size) { fit(room, wide) }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("carMode")
        .preferredColorScheme(.dark)
        .statusBarHidden()
        .onAppear { UIApplication.shared.isIdleTimerDisabled = true }
        .onDisappear { UIApplication.shared.isIdleTimerDisabled = false }
        .task { await load() }
    }

    @ViewBuilder private func nowPlaying(art: CGFloat) -> some View {
        VStack(spacing: 10) {
            if let t = music.current?.item {
                ArtworkView(item: t, shape: .square, width: art).frame(width: art, height: art)
                Text(t.title).font(.title.bold()).foregroundStyle(.white).lineLimit(1).minimumScaleFactor(0.6)
                    .accessibilityIdentifier("carTitle")
                Text(t.artistCredit ?? t.grandparentTitle ?? "").font(.title3.weight(.semibold)).foregroundStyle(.white.opacity(0.7)).lineLimit(1)
            } else {
                RoundedRectangle(cornerRadius: 12).fill(Color.white.opacity(0.08)).frame(width: art, height: art)
                    .overlay { Image(systemName: "car.fill").font(.system(size: art / 4)).foregroundStyle(.white.opacity(0.5)) }
                Text("Pick something to play").font(.title2.bold()).foregroundStyle(.white)
            }
        }
        .frame(maxWidth: .infinity)
    }

    private func fit(_ room: CGFloat, _ wide: Bool) {
        scale = min(1, room / 420) * (wide ? 0.8 : 1)
        tileHeight = wide ? 52 : 64
    }

    private var controls: some View {
        HStack(spacing: 22 * scale) {
            big("backward.fill", "Previous track", size: 84 * scale) { music.previous() }
            big(music.playing ? "pause.fill" : "play.fill", music.playing ? "Pause" : "Play", size: 120 * scale) { music.toggle() }
            big("forward.fill", "Next track", size: 84 * scale) { music.next() }
            if let t = music.current?.item {
                let on = liked[t.id] ?? ((t.userRating ?? 0) >= 10)
                big(on ? "heart.fill" : "heart", on ? "Unlike" : "Like", size: 64 * scale, tint: on ? Color.marqueeGold : .white) {
                    liked[t.id] = !on
                    Task { try? await app.rate(t.id, on ? nil : 10) }
                }
            }
        }
        .disabled(music.current == nil)
    }

    private func big(_ icon: String, _ label: String, size: CGFloat, tint: Color = .white, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: icon).font(.system(size: size * 0.4, weight: .bold)).foregroundStyle(tint)
                .frame(width: size, height: size).background(Color.white.opacity(0.14), in: Circle())
        }
        .accessibilityLabel(label)
    }

    private var tiles: some View {
        LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
            tile("Library Radio", "dot.radiowaves.left.and.right") {
                guard let library else { throw MarqueeError("There's no music library.") }
                let r = RadioRequest(seed: .library, libraryId: library, limit: 50)
                music.playStation(try await app.radio(r), radio: r)
            }
            tile(mix?.title ?? "Daily Mix", "sparkles") {
                guard let mix else { throw MarqueeError("No mixes yet: they appear once your music is analysed.") }
                music.playStation(mix)
            }
            tile("Recently Played", "clock.arrow.circlepath") {
                guard let library, let hub = try await app.hubs().first(where: { $0.id == "played-\(library)" }), !hub.items.isEmpty else {
                    throw MarqueeError("Nothing played recently.")
                }
                var tracks: [Item] = []
                for album in hub.items.prefix(4) { tracks += (try? await app.leaves(album.id)) ?? [] }
                music.play(tracks, source: "Recently Played")
            }
            tile("Shuffle All", "shuffle") {
                guard let library else { throw MarqueeError("There's no music library.") }
                let tracks = try await app.items(library: library, sort: .random, limit: 200, type: .track).items
                music.play(tracks, source: "Shuffle All")
            }
        }
    }

    private func tile(_ title: String, _ icon: String, _ work: @escaping () async throws -> Void) -> some View {
        Button {
            guard busy == nil else { return }
            busy = title
            error = nil
            Task {
                defer { busy = nil }
                do { try await work() } catch { self.error = error.localizedDescription }
            }
        } label: {
            HStack(spacing: 10) {
                if busy == title { ProgressView().tint(.black) } else { Image(systemName: icon).font(.title2.bold()) }
                Text(title).font(.title3.bold()).lineLimit(1).minimumScaleFactor(0.6).layoutPriority(1)
                Spacer(minLength: 0)
            }
            .foregroundStyle(.black)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, minHeight: tileHeight)
            .background(Color.marqueeGold, in: RoundedRectangle(cornerRadius: 14))
        }
        .accessibilityLabel(title)
        .accessibilityIdentifier("carTile")
    }

    private func load() async {
        library = libraryID
        if library == nil { library = (try? await app.libraries())?.first { $0._type == .music }?.id }
        guard let library else { return }
        mix = (try? await app.mixes(library: library))?.first { !$0.items.isEmpty }
    }
}

/// Opens car mode full screen.
struct CarModeButton<L: View>: View {
    var libraryID: Int64?
    @ViewBuilder var label: () -> L
    @State private var open = false

    var body: some View {
        Button { open = true } label: { label() }
            .fullScreenCover(isPresented: $open) { CarModeView(libraryID: libraryID) }
    }
}

extension CarModeButton where L == Label<Text, Image> {
    init(libraryID: Int64? = nil) {
        self.init(libraryID: libraryID) { Label("Car Mode", systemImage: "car.fill") }
    }
}
