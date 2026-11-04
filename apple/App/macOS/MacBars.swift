import MarqueeKit
import SwiftUI

/// Position and times, shared by the bar and the mini bar: its own view, so only it redraws as the
/// time ticks. A plain Mac slider; the position seeks when it's let go.
struct MacScrubber: View {
    @Environment(MusicPlayer.self) private var music
    @State private var drag: Double?
    var compact = false

    private var upper: Double { max(music.duration, music.time, 1) }
    private var shown: Double { min(max(0, drag ?? music.time), upper) }

    var body: some View {
        HStack(spacing: 8) {
            Text(formatTime(seconds: shown)).frame(minWidth: compact ? 34 : 40, alignment: .trailing)
            Slider(value: Binding(get: { shown }, set: { drag = $0 }), in: 0...upper) { editing in
                if !editing, let d = drag {
                    music.seek(d)
                    drag = nil
                }
            }
            .controlSize(.mini)
            .tint(Color.marqueeGold)
            .disabled(music.current == nil)
            .accessibilityLabel("Position")
            .accessibilityIdentifier("bar.scrubber")
            Text(formatTime(seconds: music.duration)).frame(minWidth: compact ? 34 : 40, alignment: .leading)
        }
        .font(.caption2.monospacedDigit())
        .foregroundStyle(.secondary)
        .onChange(of: music.current?.id) { drag = nil }
    }
}

/// A transport button that lights up under the pointer.
struct MacBarButton: View {
    let systemImage: String
    let label: String
    var identifier: String?
    var size: CGFloat = 14
    var isOn = false
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.system(size: size))
                .foregroundStyle(isOn ? Color.marqueeGold : hovering ? .primary : .primary.opacity(0.85))
                .frame(width: 28, height: 28)
                .background(hovering ? Color.primary.opacity(0.1) : .clear, in: Circle())
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(label)
        .accessibilityLabel(label)
        .accessibilityIdentifier(identifier ?? "bar.\(label)")
    }
}

/// Previous, play/pause, next.
struct MacTransport: View {
    @Environment(MusicPlayer.self) private var music
    var large = true

    var body: some View {
        HStack(spacing: 2) {
            MacBarButton(systemImage: "backward.fill", label: "Previous", identifier: "bar.previous", size: 13) { music.previous() }
                .disabled(music.current == nil)
            MacBarButton(systemImage: music.playing ? "pause.fill" : "play.fill", label: music.playing ? "Pause" : "Play",
                         identifier: "bar.playPause", size: large ? 20 : 16) { music.toggle() }
                .disabled(music.current == nil)
            MacBarButton(systemImage: "forward.fill", label: "Next", identifier: "bar.next", size: 13) { music.next() }
                .disabled(music.current == nil)
        }
    }
}

/// What's playing and what's next: click to play one, drag to reorder.
struct MacQueueList: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app

    var body: some View {
        List {
            if let current = music.current {
                Section("Now Playing") { row(current, current: true) }
            }
            Section("Up Next") {
                let upcoming = Array(music.queue.upcoming)
                if upcoming.isEmpty {
                    Text("Nothing else is queued.").foregroundStyle(.secondary)
                }
                ForEach(upcoming) { row($0, current: false) }
                    .onMove { from, to in
                        guard let f = from.first else { return }
                        music.move(upcoming[f].id, to: music.queue.index + 1 + (to > f ? to - 1 : to))
                    }
            }
        }
        .accessibilityIdentifier("queue.list")
    }

    private func row(_ e: PlayQueue.Entry, current: Bool) -> some View {
        HStack(spacing: 10) {
            Button { if !current { music.jump(e.id) } } label: {
                HStack(spacing: 10) {
                    ArtworkView(item: e.item, shape: .square, width: 36).frame(width: 36)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(e.item.title).lineLimit(1).foregroundStyle(current ? Color.marqueeGold : .primary)
                        Text(e.item.artistCredit ?? e.item.grandparentTitle ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if !current {
                Button { music.remove(e.id) } label: { Image(systemName: "xmark").font(.caption).foregroundStyle(.secondary) }
                    .buttonStyle(.plain)
                    .help("Remove from Up Next")
                    .accessibilityLabel("Remove \(e.item.title) from Up Next")
            }
        }
        .contextMenu {
            if !current { Button("Play Now") { music.jump(e.id) } }
            ItemMenuItems(item: e.item)
            if !current {
                Divider()
                Button("Remove from Queue", role: .destructive) { music.remove(e.id) }
            }
        }
    }
}

/// + (add to playlist), Track Radio, ♡ and ★ for the song playing, the song list, then the
/// output picker.
struct MacSongActions: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(AppSession.self) private var app
    @State private var adding = false
    @State private var showingQueue = false
    var showsDislike = false

    var body: some View {
        HStack(spacing: 2) {
            if let t = music.current?.item {
                if !app.offlineUI {
                    MacBarButton(systemImage: "plus", label: "Add to Playlist", identifier: "bar.addToPlaylist") { adding = true }
                        .popover(isPresented: $adding, arrowEdge: .top) {
                            AddToPlaylistSheet(item: t).macEnvironment().frame(width: 360, height: 420).preferredColorScheme(.dark)
                        }
                    MacBarButton(systemImage: "music.note", label: "Track Radio", identifier: "bar.trackRadio") {
                        music.startRadio(itemID: t.id, app: app)
                    }
                }
                RowFavorite(item: t, size: 15, target: 28, identifier: "bar.favorite")
                // Its own identity per track, so a menu left open as the track changes can't rate the next one.
                RowRating(item: t, compact: true).id(t.id)
                MacBarButton(systemImage: "list.bullet", label: "Up Next", identifier: "bar.queue", isOn: showingQueue) { showingQueue.toggle() }
                    .popover(isPresented: $showingQueue, arrowEdge: .top) {
                        MacQueueList().macEnvironment().frame(width: 360, height: 460).preferredColorScheme(.dark)
                    }
            }
            MacOutputButton(scope: .music)
        }
    }
}

/// Art, title, transport, scrubber, +, heart, rating, output picker and the switch to mini,
/// along the bottom of the full window.
struct MacNowPlayingBar: View {
    @Environment(MusicPlayer.self) private var music
    @Environment(\.openNowPlaying) private var openNowPlaying

    var body: some View {
        HStack(spacing: 16) {
            info.frame(minWidth: 180, maxWidth: 300, alignment: .leading)
            VStack(spacing: 2) {
                MacTransport()
                MacScrubber()
            }
            .frame(minWidth: 240, maxWidth: 560)
            .frame(maxWidth: .infinity)
            HStack(spacing: 2) {
                MacSongActions()
                MacBarButton(systemImage: "rectangle.compress.vertical", label: "Switch to Mini Player", identifier: "bar.mini") {
                    MacWindows.shared.switchToMini()
                }
            }
            .fixedSize()
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .frame(height: 66)
        .background(.bar)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("nowPlayingBar")
    }

    private var info: some View {
        Group {
            if let t = music.current?.item {
                Button { openNowPlaying() } label: { MacTrackInfo(item: t, artSize: 46) }
                    .buttonStyle(.plain)
                    .help("Show Now Playing")
                    .accessibilityIdentifier("bar.openNowPlaying")
            } else {
                HStack(spacing: 10) {
                    RoundedRectangle(cornerRadius: 6).fill(Color.secondary.opacity(0.2)).frame(width: 46, height: 46)
                        .overlay { Image(systemName: "music.note").foregroundStyle(.secondary) }
                    Text("Not Playing").foregroundStyle(.secondary)
                }
                .accessibilityIdentifier("bar.idle")
            }
        }
    }
}

/// Art, song, artist · album; the room's name or a short notice under them when there's one.
struct MacTrackInfo: View {
    @Environment(MusicPlayer.self) private var music
    let item: Item
    var artSize: CGFloat
    @State private var playback = MacPlayback.shared

    var body: some View {
        HStack(spacing: 10) {
            ArtworkView(item: item, shape: .square, width: artSize).frame(width: artSize)
            VStack(alignment: .leading, spacing: 1) {
                Text(item.title).font(.callout.weight(.semibold)).lineLimit(1)
                    .accessibilityIdentifier("bar.title")
                Text([item.artistCredit ?? item.grandparentTitle, item.parentTitle].compactMap { $0 }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    .accessibilityIdentifier("bar.subtitle")
                if let room = playback.sonos.activeRoomName {
                    Label("Playing on \(room)", systemImage: "hifispeaker.fill")
                        .font(.caption2.weight(.semibold)).foregroundStyle(Color.marqueeGold).lineLimit(1)
                        .accessibilityIdentifier("bar.room")
                } else if let note = playback.sonos.notice ?? music.error {
                    Text(note).font(.caption2).foregroundStyle(.orange).lineLimit(1)
                        .accessibilityIdentifier("bar.notice")
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
    }
}

/// The slim floating music player:
/// [art] Song / Artist · Album, ⏮ ▶ ⏭, + ♡ ★ output, expand, with a scrubber and time.
struct MacMiniBar: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        VStack(spacing: 2) {
            HStack(spacing: 12) {
                Group {
                    if let t = music.current?.item {
                        MacTrackInfo(item: t, artSize: 42)
                    } else {
                        Text("Not Playing").foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                MacTransport(large: false).fixedSize()
                MacSongActions().fixedSize()
                MacBarButton(systemImage: "arrow.up.left.and.arrow.down.right", label: "Back to Full Window", identifier: "mini.expand") {
                    MacWindows.shared.switchToFull()
                }
            }
            MacScrubber(compact: true)
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .frame(minWidth: 680, maxWidth: .infinity, minHeight: 84, maxHeight: .infinity)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(.white.opacity(0.08)))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("miniBar")
    }
}

/// Under the pointer: a soft highlight behind a row.
struct MacRowHover: ViewModifier {
    @State private var hovering = false

    func body(content: Content) -> some View {
        content
            .background(hovering ? Color.primary.opacity(0.07) : .clear, in: RoundedRectangle(cornerRadius: 6))
            .onHover { hovering = $0 }
    }
}

/// Under the pointer: a poster lifts a little.
struct MacPosterHover: ViewModifier {
    @State private var hovering = false

    func body(content: Content) -> some View {
        content
            .scaleEffect(hovering ? 1.03 : 1)
            .shadow(color: .black.opacity(hovering ? 0.45 : 0), radius: 8, y: 4)
            .animation(.easeOut(duration: 0.12), value: hovering)
            .onHover { hovering = $0 }
    }
}

/// What rows of one list share: their order, and which has the focus.
struct MacListFocus {
    let ids: [Int64]
    let focus: FocusState<Int64?>.Binding
}

extension EnvironmentValues {
    @Entry var macListFocus: MacListFocus?
}

/// A list the arrow keys move along.
struct MacFocusList: ViewModifier {
    let ids: [Int64]
    @FocusState private var focus: Int64?

    func body(content: Content) -> some View {
        content.environment(\.macListFocus, MacListFocus(ids: ids, focus: $focus))
    }
}

/// Keyboard focus for a row or poster: it's reachable with Tab, a click puts it there, the arrows
/// move along its list, and Return does what a click does.
struct MacKeyboardRow: ViewModifier {
    let id: Int64?
    let activate: () -> Void
    @Environment(\.macListFocus) private var list
    @FocusState private var alone: Bool

    private var focused: Bool {
        if let list, let id { return list.focus.wrappedValue == id }
        return alone
    }

    func body(content: Content) -> some View {
        Group {
            if let list, let id {
                common(content)
                    .focused(list.focus, equals: id)
                    .simultaneousGesture(TapGesture().onEnded { list.focus.wrappedValue = id })
                    .onMoveCommand { move($0, list, from: id) }
            } else {
                common(content)
                    .focused($alone)
            }
        }
        .overlay {
            if focused { RoundedRectangle(cornerRadius: 6).strokeBorder(Color.marqueeGold.opacity(0.8), lineWidth: 1.5).allowsHitTesting(false) }
        }
        .onKeyPress(.return) {
            activate()
            return .handled
        }
    }

    /// .edit: reachable without Full Keyboard Access turned on.
    private func common(_ content: Content) -> some View {
        content
            .focusable(true, interactions: .edit)
            .focusEffectDisabled()
    }

    private func move(_ direction: MoveCommandDirection, _ list: MacListFocus, from id: Int64) {
        guard let i = list.ids.firstIndex(of: id) else { return }
        switch direction {
        case .down where i + 1 < list.ids.count: list.focus.wrappedValue = list.ids[i + 1]
        case .up where i > 0: list.focus.wrappedValue = list.ids[i - 1]
        default: break
        }
    }
}
