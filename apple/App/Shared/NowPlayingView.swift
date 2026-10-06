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
            .overlay(alignment: .bottom) { MiniPlayerProgress().padding(.horizontal, 10) }
            .padding(.horizontal, 10)
            .padding(.bottom, 4)
        }
    }
}

/// The mini player's progress line: its own view, so only it redraws as the time ticks.
private struct MiniPlayerProgress: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        GeometryReader { g in
            Rectangle().fill(Color.marqueeGold).frame(width: g.size.width * (music.duration > 0 ? min(1, music.time / music.duration) : 0), height: 2)
        }
        .frame(height: 2)
        .accessibilityHidden(true)
    }
}

/// Now Playing's position slider and times: its own view, so the time ticking redraws only
/// this, not the whole screen.
private struct MusicScrubber: View {
    @Environment(MusicPlayer.self) private var music
    /// Where the thumb is while it's dragged; nil otherwise (the player's time shows).
    @State private var scrub: Double?
    @State private var editing = false

    /// The slider's range: the track's length, or the time when it runs past it (a length
    /// from the library that's shorter than the stream).
    private var upper: Double { max(music.duration, music.time, 1) }

    /// The position shown: the drag, else the player's time, always within the range (a value
    /// outside it made the slider write it back, which looked like a drag that never ended,
    /// so the bar stayed put while the music played on, typically after a skip).
    private var shown: Double { min(max(0, scrub ?? music.time), upper) }

    var body: some View {
        VStack(spacing: 4) {
            #if os(tvOS)
            ProgressView(value: music.duration > 0 ? min(1, music.time / music.duration) : 0)
            #else
            Slider(value: Binding(get: { shown }, set: { v in
                if editing {
                    scrub = v
                } else if abs(v - music.time) > 0.5 {
                    music.seek(v) // VoiceOver's adjust: no drag, a direct move
                }
            }), in: 0...upper) { e in
                editing = e
                if !e, let s = scrub { music.seek(s); scrub = nil }
            }
            .accessibilityLabel("Position")
            .accessibilityValue("\(formatTime(seconds: shown)) of \(formatTime(seconds: music.duration))")
            #endif
            HStack {
                Text(formatTime(seconds: shown))
                Spacer()
                Text("-" + formatTime(seconds: max(0, music.duration - shown)))
            }
            .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            .accessibilityHidden(true)
        }
        // A new track never keeps the last one's drag.
        .onChange(of: music.current?.id) { scrub = nil; editing = false }
    }
}

/// Full-screen Now Playing with the queue.
struct NowPlayingView: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @State private var panel: Panel = .player
    @State private var saveRequest: SavePlaylistRequest?
    #if os(iOS)
    @Environment(\.verticalSizeClass) private var verticalSize
    #endif

    enum Panel: String, CaseIterable {
        case player = "Now Playing", lyrics = "Lyrics", queue = "Up Next"
    }

    var body: some View {
        if let t = music.current?.item {
            // The backdrop sits behind the content and never sizes it: a scaled-to-fill image in
            // a ZStack made the whole screen wider than the window.
            Group {
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
                if verticalSize == .compact {
                    landscape(t)
                } else {
                    VStack(spacing: 16) {
                        Capsule().fill(.secondary).frame(width: 40, height: 5).padding(.top, 8)
                        sourceHeader
                        switch panel {
                        case .player: player(t)
                        case .lyrics: LyricsView(itemID: t.id).padding(.horizontal, 8)
                        case .queue: queueList
                        }
                        panelPicker.padding(.bottom)
                    }
                    .padding(.horizontal, 24)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
                #endif
            }
            .background { backdrop(t) }
            .savePlaylistFlow($saveRequest)
        } else {
            ContentUnavailableView("Nothing playing", systemImage: "music.note")
        }
    }

    @ViewBuilder private var sourceHeader: some View {
        #if os(iOS)
        // Where it plays, in two groups of the same width either side of what's playing, so
        // the title has the middle to itself (truncated, never under a button): another
        // Marquee app on the left; AirPlay (HomePod, Sonos…) and Chromecast on the right.
        HStack(alignment: .center, spacing: 8) {
            HStack(spacing: Self.headerSpacing) {
                // Another Marquee app (USER-14): the queue carries on there from here.
                PlayOnButton(target: handOff, compact: true)
                    .font(.system(size: 17, weight: .regular)).foregroundStyle(.secondary)
                    .headerButton()
                Spacer(minLength: 0)
            }
            .frame(width: Self.headerGroup)
            sourceTitle.frame(maxWidth: .infinity)
            HStack(spacing: Self.headerSpacing) {
                Spacer(minLength: 0)
                AirPlayButton().headerButton()
                CastButton(tint: .secondaryLabel).padding(5).headerButton()
            }
            .frame(width: Self.headerGroup)
        }
        #else
        sourceTitle
        #endif
    }

    #if os(iOS)
    /// The queue from the current track, at the current position.
    private func handOff() -> PlayOnTarget? {
        let entries = music.queue.entries
        guard music.current != nil, !entries.isEmpty else { return nil }
        let i = max(0, min(music.queue.index, entries.count - 1))
        // The server takes up to 1000 items; keep the part around what's playing.
        let from = max(0, i - 200), to = min(entries.count, from + 1000)
        return PlayOnTarget(itemIDs: entries[from..<to].map(\.item.id), index: i - from,
                            startMs: Int64(music.time * 1000), pauseHere: { music.pause() })
    }
    #endif

    #if os(iOS)
    /// Header buttons: all the same size, two to a side.
    static let headerButtonSize: CGFloat = 36
    static let headerSpacing: CGFloat = 6
    static let headerGroup: CGFloat = headerButtonSize * 2 + headerSpacing
    #endif

    @ViewBuilder private var sourceTitle: some View {
        VStack(spacing: 2) {
            Text(music.source == nil ? "NOW PLAYING" : "PLAYING FROM").font(.caption2.weight(.semibold)).foregroundStyle(.secondary)
                .lineLimit(1)
            if let s = music.source { Text(s.title).font(.footnote.weight(.semibold)).lineLimit(1).truncationMode(.tail) }
            #if os(iOS)
            if music.remote != nil, let d = CastController.shared.device {
                Text("Playing on \(d)").font(.caption.weight(.semibold)).foregroundStyle(Color.marqueeGold).lineLimit(1)
            }
            #endif
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("nowPlayingSource")
    }

    private func backdrop(_ t: Item) -> some View {
        Color.black
            .overlay {
                CachedImage(fill: app.imageURL(t.images?.poster, width: 64))
                    .blur(radius: 60).opacity(0.55)
            }
            .clipped()
            .ignoresSafeArea()
    }

    #if os(iOS)
    private var panelPicker: some View {
        Picker("Show", selection: $panel.animation()) {
            ForEach(Panel.allCases, id: \.self) { Text($0.rawValue).tag($0) }
        }
        .pickerStyle(.segmented)
        .accessibilityIdentifier("nowPlayingPanel")
    }

    /// Landscape on iPhone: artwork (or lyrics / Up Next) on the left, the controls on the
    /// right, both within the safe area.
    private func landscape(_ t: Item) -> some View {
        HStack(spacing: 28) {
            Group {
                switch panel {
                case .player:
                    artwork(t).frame(maxWidth: .infinity, maxHeight: .infinity)
                case .lyrics: LyricsView(itemID: t.id)
                case .queue: queueList
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            VStack(spacing: 10) {
                sourceHeader
                Spacer(minLength: 0)
                info(t)
                scrubber
                transport
                actions(t)
                Spacer(minLength: 0)
                panelPicker
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }
    #endif

    private func artwork(_ t: Item) -> some View {
        ArtworkView(item: t, shape: .square, width: 600)
            #if os(tvOS)
            .frame(width: artSize, height: artSize)
            .layoutPriority(1)
            #else
            .frame(maxWidth: artSize)
            #endif
            .shadow(color: .black.opacity(0.5), radius: 30, y: 10)
    }

    /// Title and artist, one line each and truncated, so long names never widen the screen.
    private func info(_ t: Item) -> some View {
        VStack(spacing: 4) {
            Text(t.title).font(.title2.bold()).lineLimit(1).truncationMode(.tail)
                .frame(maxWidth: .infinity)
                .accessibilityIdentifier("nowPlayingTitle")
            Text([t.artistCredit ?? t.grandparentTitle, t.parentTitle].compactMap { $0 }.joined(separator: " · "))
                .foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                .frame(maxWidth: .infinity)
                .accessibilityIdentifier("nowPlayingSubtitle")
            if music.showAudioQuality, let f = t.audioFormat { qualityBadge(f) }
            if let error = music.error {
                Label(error, systemImage: "exclamationmark.triangle.fill").font(.footnote).foregroundStyle(.red).lineLimit(2)
                    .accessibilityIdentifier("musicError")
            }
        }
    }

    /// "FLAC · 24-bit/96 kHz → AAC 256 kbps", one line, with a Hi-Res tag (MUSIC-23).
    private func qualityBadge(_ f: AudioFormat) -> some View {
        let text = [f.label, music.currentStreamed.map { "→ \($0.label)" }].compactMap { $0 }.joined(separator: " ")
        return HStack(spacing: 6) {
            if f.isHiRes {
                Text("Hi-Res").font(.caption2.weight(.heavy))
                    .padding(.horizontal, 5).padding(.vertical, 1)
                    .background(Color.marqueeGold.opacity(0.25), in: RoundedRectangle(cornerRadius: 4))
                    .foregroundStyle(Color.marqueeGold)
                    .fixedSize()
            }
            Text(text).font(.caption.weight(.semibold)).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("audioQuality")
    }

    private func player(_ t: Item) -> some View {
        VStack(spacing: stackSpacing) {
            Spacer(minLength: 0)
            artwork(t)
            info(t)
            scrubber
            transport
            actions(t)
            Spacer(minLength: 0)
        }
    }

    private var scrubber: some View { MusicScrubber() }

    /// Space between the transport buttons: fixed on the TV; on iPhone it shrinks to fit.
    @ViewBuilder private var gap: some View {
        #if os(tvOS)
        Color.clear.frame(width: 36, height: 1)
        #else
        Spacer(minLength: 6)
        #endif
    }

    private var transport: some View {
            HStack(spacing: 0) {
                Button { music.toggleShuffle() } label: { Image(systemName: "shuffle").foregroundStyle(music.queue.shuffled ? Color.marqueeGold : .secondary) }
                    .accessibilityLabel("Shuffle")
                    .accessibilityValue(music.queue.shuffled ? "On" : "Off")
                    .accessibilityAddTraits(music.queue.shuffled ? .isSelected : [])
                    .accessibilityIdentifier("np.shuffle")
                gap
                Button { music.previous() } label: { Image(systemName: "backward.fill").font(.title) }
                    .accessibilityLabel("Previous track")
                    .accessibilityIdentifier("np.previous")
                gap
                Button { music.toggle() } label: { Image(systemName: music.playing ? "pause.circle.fill" : "play.circle.fill").font(.system(size: playSize)) }
                    .accessibilityLabel(music.playing ? "Pause" : "Play")
                    .accessibilityIdentifier("np.playPause")
                gap
                Button { music.next() } label: { Image(systemName: "forward.fill").font(.title) }
                    .accessibilityLabel("Next track")
                    .accessibilityIdentifier("np.next")
                gap
                Button { music.cycleRepeat() } label: {
                    Image(systemName: music.queue.repeatMode == .one ? "repeat.1" : "repeat")
                        .foregroundStyle(music.queue.repeatMode == .off ? .secondary : Color.marqueeGold)
                }
                .accessibilityLabel("Repeat")
                .accessibilityValue(music.queue.repeatMode == .one ? "One" : music.queue.repeatMode == .off ? "Off" : "All")
                .accessibilityIdentifier("np.repeat")
            }
            .buttonStyle(.plain)
            #if os(tvOS)
            .frame(maxWidth: .infinity) // full width, so up/down from any button reaches the row
            .focusSection()
            #else
            .frame(maxWidth: 330)
            .frame(maxWidth: .infinity)
            #endif
    }

    private func actions(_ t: Item) -> some View {
            #if os(iOS)
            // Stars and the menus on one row when they fit, otherwise on two.
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 18) {
                    RatingStars(itemID: t.id, rating: t.userRating).id(t.id)
                    Spacer(minLength: 8)
                    actionMenus
                }
                VStack(spacing: 12) {
                    RatingStars(itemID: t.id, rating: t.userRating).id(t.id)
                    actionMenus
                }
            }
            .font(.title3)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity)
            #else
            HStack(spacing: 24) {
                Button { panel = panel == .lyrics ? .queue : .lyrics } label: {
                    Label(panel == .lyrics ? "Show Up Next" : "Lyrics", systemImage: panel == .lyrics ? "list.bullet" : "quote.bubble")
                        .labelStyle(.iconOnly)
                }
                .accessibilityLabel(panel == .lyrics ? "Show Up Next" : "Lyrics")
                SleepMenu().labelStyle(.iconOnly)
                DJMenu()
                CrossfadeMenu()
                Button { saveRequest = music.queueAsPlaylist } label: {
                    Label("Save as Playlist", systemImage: "text.badge.plus").labelStyle(.iconOnly)
                }
                .accessibilityLabel("Save as Playlist")
            }
            .font(.title3)
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity) // full width, so up/down from any button reaches the row
            .focusSection()
            #endif
    }

    #if os(iOS)
    private var actionMenus: some View {
        HStack(spacing: 18) {
            SleepMenu().labelStyle(.iconOnly)
            DJMenu()
            CrossfadeMenu()
            Menu {
                Button("Save as Playlist…", systemImage: "text.badge.plus") { saveRequest = music.queueAsPlaylist }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .accessibilityLabel("More")
            .accessibilityIdentifier("np.more")
        }
        .fixedSize()
    }
    #endif

    #if os(tvOS)
    private let artSize: CGFloat = 360
    private let stackSpacing: CGFloat = 18
    private let playSize: CGFloat = 64
    #else
    private let artSize: CGFloat = 340
    private let stackSpacing: CGFloat = 22
    private var playSize: CGFloat { verticalSize == .compact ? 52 : 64 }
    #endif

    private var queueList: some View {
        List {
            if let cur = music.current {
                Section("Now playing") { row(cur, current: true) }
            }
            Section {
                Button { saveRequest = music.queueAsPlaylist } label: {
                    Label("Save Queue as Playlist", systemImage: "text.badge.plus")
                }
                .accessibilityIdentifier("saveQueue")
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
private extension View {
    /// A Now Playing header button: a fixed square, the whole of it tappable.
    func headerButton() -> some View {
        frame(width: NowPlayingView.headerButtonSize, height: NowPlayingView.headerButtonSize)
            .contentShape(Rectangle())
    }
}

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
