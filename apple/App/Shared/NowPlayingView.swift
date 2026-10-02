import AVKit
import MarqueeKit
import SwiftUI

/// The bar above the tab bar while music plays (iPhone/iPad).
struct MiniPlayerBar: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @Environment(\.openNowPlaying) private var openNowPlaying

    var body: some View {
        if let t = music.current?.item {
            HStack(spacing: 12) {
                Button { openNowPlaying() } label: {
                    HStack(spacing: 12) {
                        ArtworkView(item: t, shape: .square, width: 44).frame(width: 44)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(t.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                            Text(t.artistCredit ?? t.grandparentTitle ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer(minLength: 0)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("miniPlayer")
                Button { music.toggle() } label: { Image(systemName: music.playing ? "pause.fill" : "play.fill").font(.title3).frame(width: 36, height: 36) }
                    .buttonStyle(.plain)
                    .accessibilityLabel(music.playing ? "Pause" : "Play")
                Button { music.next() } label: { Image(systemName: "forward.fill").font(.title3).frame(width: 36, height: 36) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Next track")
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
            .overlay(alignment: .bottom) {
                GeometryReader { g in
                    Rectangle().fill(Color.marqueeGold).frame(width: g.size.width * (music.duration > 0 ? music.time / music.duration : 0), height: 2)
                }
                .frame(height: 2)
                .padding(.horizontal, 10)
            }
            .padding(.horizontal, 10)
            .padding(.bottom, 4)
        }
    }
}

/// Full-screen Now Playing with the queue.
struct NowPlayingView: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @State private var panel: Panel = .player
    @State private var scrub: Double?

    enum Panel: String, CaseIterable {
        case player = "Now Playing", lyrics = "Lyrics", queue = "Up Next"
    }

    var body: some View {
        if let t = music.current?.item {
            ZStack {
                backdrop(t)
                #if os(tvOS)
                HStack(spacing: 80) {
                    VStack(spacing: 30) {
                        sourceHeader
                        player(t)
                    }
                    .frame(maxWidth: 700)
                    VStack(alignment: .leading, spacing: 20) {
                        Text(panel == .lyrics ? "Lyrics" : "Up Next").font(.title3.bold())
                        if panel == .lyrics { LyricsView(itemID: t.id).frame(maxHeight: .infinity) } else { queueList }
                    }
                    .frame(width: 700)
                    .frame(maxHeight: .infinity, alignment: .top)
                }
                .padding(.horizontal, 80)
                .padding(.vertical, 30)
                .onAppear { if panel == .player { panel = .queue } }
                #else
                VStack(spacing: 16) {
                    Capsule().fill(.secondary).frame(width: 40, height: 5).padding(.top, 8)
                    sourceHeader
                    switch panel {
                    case .player: player(t)
                    case .lyrics: LyricsView(itemID: t.id).padding(.horizontal, 8)
                    case .queue: queueList
                    }
                    Picker("Show", selection: $panel.animation()) {
                        ForEach(Panel.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)
                    .padding(.bottom)
                }
                .padding(.horizontal, 24)
                #endif
            }
        } else {
            ContentUnavailableView("Nothing playing", systemImage: "music.note")
        }
    }

    @ViewBuilder private var sourceHeader: some View {
        #if os(iOS)
        // Where it plays: AirPlay (HomePod, Sonos…) and Chromecast, beside what's playing.
        HStack(alignment: .center) {
            // Car mode and the equaliser on the left, balancing the outputs on the right.
            HStack(spacing: 6) {
                CarModeButton().labelStyle(.iconOnly).font(.callout).foregroundStyle(.secondary).frame(width: 32, height: 32)
                EqualizerButton().frame(width: 32, height: 32)
            }
            .frame(width: 72, alignment: .leading)
            Spacer(minLength: 0)
            sourceTitle
            Spacer(minLength: 0)
            HStack(spacing: 6) {
                AirPlayButton().frame(width: 32, height: 32)
                CastButton(tint: .secondaryLabel).frame(width: 32, height: 32)
            }
            .frame(width: 72, alignment: .trailing)
        }
        #else
        sourceTitle
        #endif
    }

    @ViewBuilder private var sourceTitle: some View {
        VStack(spacing: 2) {
            Text(music.source == nil ? "NOW PLAYING" : "PLAYING FROM").font(.caption2.weight(.semibold)).foregroundStyle(.secondary)
            if let s = music.source { Text(s.title).font(.footnote.weight(.semibold)).lineLimit(1) }
            #if os(iOS)
            if music.remote != nil, let d = CastController.shared.device {
                Text("Playing on \(d)").font(.caption.weight(.semibold)).foregroundStyle(Color.marqueeGold).lineLimit(1)
            }
            #endif
        }
    }

    private func backdrop(_ t: Item) -> some View {
        ZStack {
            Color.black
            AsyncImage(url: app.imageURL(t.images?.poster, width: 64)) { $0.image?.resizable().scaledToFill() }
                .blur(radius: 60).opacity(0.55)
        }
        .ignoresSafeArea()
    }

    private func player(_ t: Item) -> some View {
        VStack(spacing: stackSpacing) {
            Spacer(minLength: 0)
            ArtworkView(item: t, shape: .square, width: 600)
                #if os(tvOS)
                .frame(width: artSize, height: artSize)
                .layoutPriority(1)
                #else
                .frame(maxWidth: artSize)
                #endif
                .shadow(color: .black.opacity(0.5), radius: 30, y: 10)
            VStack(spacing: 4) {
                Text(t.title).font(.title2.bold()).lineLimit(1).accessibilityIdentifier("nowPlayingTitle")
                Text([t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · ")).foregroundStyle(.secondary).lineLimit(1)
                if let error = music.error {
                    Label(error, systemImage: "exclamationmark.triangle.fill").font(.footnote).foregroundStyle(.red).lineLimit(2)
                        .accessibilityIdentifier("musicError")
                }
            }
            VStack(spacing: 4) {
                #if os(tvOS)
                ProgressView(value: music.duration > 0 ? music.time / music.duration : 0)
                #else
                Slider(value: Binding(get: { scrub ?? music.time }, set: { scrub = $0 }), in: 0...max(music.duration, 1)) { editing in
                    if !editing, let s = scrub { music.seek(s); scrub = nil }
                }
                #endif
                HStack {
                    Text(formatTime(seconds: scrub ?? music.time))
                    Spacer()
                    Text("-" + formatTime(seconds: max(0, music.duration - (scrub ?? music.time))))
                }
                .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
            HStack(spacing: 36) {
                Button { music.toggleShuffle() } label: { Image(systemName: "shuffle").foregroundStyle(music.queue.shuffled ? Color.marqueeGold : .secondary) }
                Button { music.previous() } label: { Image(systemName: "backward.fill").font(.title) }
                Button { music.toggle() } label: { Image(systemName: music.playing ? "pause.circle.fill" : "play.circle.fill").font(.system(size: 64)) }
                Button { music.next() } label: { Image(systemName: "forward.fill").font(.title) }
                Button { music.cycleRepeat() } label: {
                    Image(systemName: music.queue.repeatMode == .one ? "repeat.1" : "repeat")
                        .foregroundStyle(music.queue.repeatMode == .off ? .secondary : Color.marqueeGold)
                }
            }
            .buttonStyle(.plain)
            #if os(tvOS)
            .frame(maxWidth: .infinity) // full width, so up/down from any button reaches the row
            .focusSection()
            #endif
            HStack(spacing: 24) {
                #if os(iOS)
                RatingStars(itemID: t.id, rating: t.userRating).id(t.id)
                Spacer()
                #endif
                #if os(tvOS)
                Button { panel = panel == .lyrics ? .queue : .lyrics } label: {
                    Label(panel == .lyrics ? "Show Up Next" : "Lyrics", systemImage: panel == .lyrics ? "list.bullet" : "quote.bubble")
                        .labelStyle(.iconOnly)
                }
                .accessibilityLabel(panel == .lyrics ? "Show Up Next" : "Lyrics")
                #endif
                SleepMenu().labelStyle(.iconOnly)
                DJMenu()
                LevellingMenu()
                CrossfadeMenu()
            }
            .font(.title3)
            .foregroundStyle(.secondary)
            #if os(tvOS)
            .frame(maxWidth: .infinity) // full width, so up/down from any button reaches the row
            .focusSection()
            #endif
            Spacer(minLength: 0)
        }
    }

    #if os(tvOS)
    private let artSize: CGFloat = 360
    private let stackSpacing: CGFloat = 18
    #else
    private let artSize: CGFloat = 340
    private let stackSpacing: CGFloat = 22
    #endif

    private var queueList: some View {
        List {
            if let cur = music.current {
                Section("Now playing") { row(cur, current: true) }
            }
            Section("Up next") {
                ForEach(Array(music.queue.upcoming)) { e in row(e, current: false) }
                    #if os(iOS)
                    .onMove { from, to in
                        guard let f = from.first else { return }
                        let entries = Array(music.queue.upcoming)
                        let target = music.queue.index + 1 + (to > f ? to - 1 : to)
                        music.move(entries[f].id, to: target)
                    }
                    .onDelete { offsets in
                        let entries = Array(music.queue.upcoming)
                        for i in offsets { music.remove(entries[i].id) }
                    }
                    #endif
            }
        }
        #if os(iOS)
        .environment(\.editMode, .constant(.active))
        .scrollContentBackground(.hidden)
        #endif
    }

    private func row(_ e: PlayQueue.Entry, current: Bool) -> some View {
        Button { if !current { music.jump(e.id) } } label: {
            HStack(spacing: 12) {
                ArtworkView(item: e.item, shape: .square, width: 44).frame(width: 44)
                VStack(alignment: .leading) {
                    HStack(spacing: 6) {
                        if e.dj != nil {
                            Text("DJ").font(.caption2.bold()).padding(.horizontal, 4).background(Color.marqueeGold.opacity(0.25), in: RoundedRectangle(cornerRadius: 3))
                        }
                        Text(e.item.title).lineLimit(1).foregroundStyle(current ? Color.marqueeGold : .primary)
                    }
                    Text(e.item.artistCredit ?? e.item.grandparentTitle ?? "").font(.caption).foregroundStyle(Color.secondary).lineLimit(1)
                }
            }
        }
        .buttonStyle(.plain)
    }
}

#if os(iOS)
/// The system AirPlay picker: AirPlay 2 speakers (HomePod, Sonos…), Apple TV and Bluetooth.
struct AirPlayButton: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView {
        let v = AVRoutePickerView()
        v.tintColor = .secondaryLabel
        v.activeTintColor = UIColor(Color.marqueeGold)
        v.prioritizesVideoDevices = false
        v.accessibilityLabel = "AirPlay"
        return v
    }

    func updateUIView(_ uiView: AVRoutePickerView, context: Context) {}
}
#endif
