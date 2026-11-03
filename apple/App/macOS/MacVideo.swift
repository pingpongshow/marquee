import AVKit
import MarqueeKit
import MediaPlayer
import SwiftUI

/// The video playing right now, wherever it's shown: the main window, a sheet (Live TV, VOD) or
/// the mini video window. Each view showing a player tells this; there's one at a time.
@MainActor @Observable
final class MacVideo {
    static let shared = MacVideo()

    private(set) var activePlayer: AVPlayer?
    /// The library video's model (subtitles and audio tracks), when it's that sort of video.
    private(set) var playback: VideoPlayback?
    /// Closes it, whichever kind it is.
    @ObservationIgnored private var closeAction: (() -> Void)?
    /// How many views show each player: the mini window adds a second for the same one.
    @ObservationIgnored private var surfaces: [ObjectIdentifier: Int] = [:]

    /// A library video has started (the player view passes its model and close action).
    func began(player: AVPlayer, playback: VideoPlayback, close: @escaping () -> Void) {
        self.playback = playback
        closeAction = close
        become(player)
    }

    /// The library video has closed.
    func ended(player: AVPlayer) {
        guard activePlayer === player else { return }
        playback = nil
        closeAction = nil
        finish()
    }

    /// A player view appeared (Live TV, VOD and the library's all come through here).
    func surfaceAppeared(_ player: AVPlayer) {
        surfaces[ObjectIdentifier(player), default: 0] += 1
        MacPlayback.shared.outputs.register(player)
        player.volume = MacPlayback.shared.music.appVolume
        if activePlayer !== player { become(player) }
    }

    /// A player view went away. The library's video ends with PlayerView (`ended`); Live TV and
    /// VOD with the last view showing them.
    func surfaceDisappeared(_ player: AVPlayer) {
        let key = ObjectIdentifier(player)
        let left = (surfaces[key] ?? 1) - 1
        surfaces[key] = left > 0 ? left : nil
        guard left <= 0, activePlayer === player, playback == nil else { return }
        finish()
    }

    /// Set while a sheet shows a player: closing it (from the mini window, say).
    @ObservationIgnored var coverClose: (() -> Void)?

    func closeActive() {
        if let closeAction {
            closeAction()
        } else if let coverClose {
            coverClose()
        } else {
            activePlayer?.pause()
            MacPlayback.shared.video.request = nil
        }
    }

    func applyVolume(_ v: Float) { activePlayer?.volume = v }

    /// Whether something's open over the player (a menu, popover or sheet), so its overlay
    /// buttons stay up meanwhile.
    var isCovered: Bool {
        menuOpen || NSApp.keyWindow?.attachedSheet != nil
            || NSApp.windows.contains { $0.isVisible && String(describing: type(of: $0)).contains("Popover") }
    }

    @ObservationIgnored private var menuOpen = false
    @ObservationIgnored private var nowPlayingTask: Task<Void, Never>?
    /// The overlay buttons of the player showing.
    @ObservationIgnored weak var chrome: PlayerChrome?

    init() {
        NotificationCenter.default.addObserver(forName: NSMenu.didBeginTrackingNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.menuOpen = true }
        }
        NotificationCenter.default.addObserver(forName: NSMenu.didEndTrackingNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.menuOpen = false }
        }
    }

    private func become(_ player: AVPlayer) {
        activePlayer = player
        MacPlayback.shared.music.nowPlayingBorrowed = true
        MacWindows.shared.videoChanged()
        nowPlayingTask?.cancel()
        nowPlayingTask = Task { [weak self] in
            while !Task.isCancelled, let self, self.activePlayer === player {
                self.publishNowPlaying(player)
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    /// Control Center and the media keys show and control the video while one plays.
    private func publishNowPlaying(_ player: AVPlayer) {
        let d = player.currentItem?.duration.seconds ?? .nan
        let t = player.currentTime().seconds
        MPNowPlayingInfoCenter.default().nowPlayingInfo = [
            MPMediaItemPropertyTitle: playback?.item?.title ?? "Marquee",
            MPNowPlayingInfoPropertyMediaType: MPNowPlayingInfoMediaType.video.rawValue,
            MPMediaItemPropertyPlaybackDuration: d.isFinite ? d : 0,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: t.isFinite ? max(0, t) : 0,
            MPNowPlayingInfoPropertyPlaybackRate: player.timeControlStatus == .playing ? 1.0 : 0.0,
        ]
        MPNowPlayingInfoCenter.default().playbackState = player.timeControlStatus == .paused ? .paused : .playing
    }

    private func finish() {
        activePlayer = nil
        nowPlayingTask?.cancel()
        MacPlayback.shared.music.nowPlayingBorrowed = false
        // A moment: the next episode's player may take over in this same update.
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(400))
            if self.activePlayer == nil { MacWindows.shared.videoChanged() }
        }
    }
}

/// AVPlayerView for SwiftUI: the Mac's native controls, picture in picture and Now Playing.
struct MacPlayerSurface: NSViewRepresentable {
    enum Controls { case none, floating, inline }

    let player: AVPlayer
    var controls: Controls = .floating
    var chrome: PlayerChrome?

    func makeNSView(context: Context) -> AVPlayerView {
        let v = AVPlayerView()
        v.player = player
        v.videoGravity = .resizeAspect
        v.updatesNowPlayingInfoCenter = false // MacVideo tells Now Playing itself
        v.allowsPictureInPicturePlayback = true
        apply(controls, to: v)
        context.coordinator.watch(player, chrome: chrome)
        chrome?.covered = { MacVideo.shared.isCovered }
        if let chrome { MacVideo.shared.chrome = chrome }
        MacVideo.shared.surfaceAppeared(player)
        context.coordinator.shown = player
        return v
    }

    func updateNSView(_ v: AVPlayerView, context: Context) {
        if v.player !== player {
            if let old = context.coordinator.shown { MacVideo.shared.surfaceDisappeared(old) }
            v.player = player
            context.coordinator.watch(player, chrome: chrome)
            MacVideo.shared.surfaceAppeared(player)
            context.coordinator.shown = player
        }
        apply(controls, to: v)
    }

    static func dismantleNSView(_ v: AVPlayerView, coordinator: Coordinator) {
        let p = coordinator.shown
        v.player = nil
        coordinator.stopWatching()
        if let p { MacVideo.shared.surfaceDisappeared(p) }
    }

    private func apply(_ c: Controls, to v: AVPlayerView) {
        let style: AVPlayerViewControlsStyle = switch c {
        case .none: .none
        case .floating: .floating
        case .inline: .inline
        }
        if v.controlsStyle != style { v.controlsStyle = style }
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    final class Coordinator {
        var shown: AVPlayer?
        private var rate: NSKeyValueObservation?

        /// Playing or paused, for the overlay buttons (they stay up while paused).
        func watch(_ player: AVPlayer, chrome: PlayerChrome?) {
            rate = player.observe(\.timeControlStatus, options: [.initial, .new]) { [weak chrome] p, _ in
                let playing = p.timeControlStatus != .paused
                Task { @MainActor in chrome?.setPlaying(playing) }
            }
        }

        func stopWatching() { rate = nil }
    }
}

/// The library video's picture and controls (PlayerView's), with the overlay buttons shown as the
/// pointer moves.
struct PlayerController: View {
    let playback: VideoPlayback
    var together: WatchTogether?
    let onNext: () -> Void
    var trailer: TrailerActions?
    var chrome: PlayerChrome?

    var body: some View {
        MacPlayerSurface(player: playback.player, controls: .floating, chrome: chrome)
            .onContinuousHover { _ in chrome?.show() }
            .accessibilityElement(children: .contain)
    }
}

/// The player's own buttons on the Mac: subtitles and audio, where the sound goes, full screen,
/// and the switch to the mini video window.
struct MacVideoTopButtons: View {
    let playback: VideoPlayback

    var body: some View {
        MacTracksMenu(playback: playback)
        MacOutputButton(scope: .video)
            .modifier(PlayerBubbleBackground())
        Button { NSApp.keyWindow?.toggleFullScreen(nil) } label: { PlayerBubble(systemImage: "arrow.up.left.and.arrow.down.right") }
            .accessibilityLabel("Full Screen")
        Button { MacWindows.shared.switchToMini() } label: { PlayerBubble(systemImage: "pip.enter") }
            .accessibilityLabel("Switch to Mini Player")
            .accessibilityIdentifier("video.mini")
    }
}

/// Subtitles and audio tracks. Text subtitles switch in the stream; audio changes and image
/// subtitles restart at the same position (VideoPlayback.change).
struct MacTracksMenu: View {
    let playback: VideoPlayback

    private var streams: [Schemas.MediaStream] { playback.item?.info.versions.first?.files.first?.streams ?? [] }

    var body: some View {
        let audio = streams.filter { $0.kind == .audio }
        let subs = streams.filter { $0.kind == .subtitle }
        if playback.offlineTitle == nil, audio.count > 1 || !subs.isEmpty {
            Menu {
                if !subs.isEmpty {
                    Section("Subtitles") {
                        Button("Off") { Task { await playback.change(subtitle: -1) } }
                        ForEach(subs, id: \.id) { s in
                            Button(trackTitle(s)) { Task { await playback.change(subtitle: s.id) } }
                        }
                    }
                }
                if audio.count > 1 {
                    Section("Audio") {
                        ForEach(audio, id: \.id) { s in
                            Toggle(trackTitle(s), isOn: Binding(get: { s.id == playback.session?.audioStreamId },
                                                                set: { _ in Task { await playback.change(audio: s.id) } }))
                        }
                    }
                }
            } label: {
                PlayerBubble(systemImage: "captions.bubble")
            }
            .menuStyle(.button)
            .menuIndicator(.hidden)
            .buttonStyle(.plain)
            .fixedSize()
            .accessibilityLabel("Subtitles and Audio")
            .accessibilityIdentifier("video.tracks")
        }
    }

    private func trackTitle(_ s: Schemas.MediaStream) -> String {
        [s.title ?? languageName(s.language), s.codec.uppercased()].joined(separator: " · ")
    }
}

/// The small floating video window: the same player, moved over without reloading.
struct MacMiniVideo: View {
    @State private var video = MacVideo.shared
    @State private var hovering = false

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black
            if let p = video.activePlayer {
                MacPlayerSurface(player: p, controls: .inline)

            }
            if hovering {
                HStack(spacing: 6) {
                    MacOutputButton(scope: .video).modifier(MiniVideoBubble())
                    Button { MacWindows.shared.switchToFull() } label: {
                        Image(systemName: "arrow.up.left.and.arrow.down.right").modifier(MiniVideoBubble())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Back to Full Window")
                    .accessibilityIdentifier("miniVideo.expand")
                }
                .foregroundStyle(.white)
                .padding(8)
                .transition(.opacity)
            }
        }
        .animation(.easeInOut(duration: 0.15), value: hovering)
        .onHover { hovering = $0 }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("miniVideo")
    }
}

private struct MiniVideoBubble: ViewModifier {
    func body(content: Content) -> some View {
        content.frame(width: 28, height: 28).background(.ultraThinMaterial, in: Circle()).contentShape(Circle())
    }
}

/// Live TV's AirPlay button: the Mac's route picker on the player showing.
struct AirPlayButton: View {
    var body: some View {
        MacRoutePicker(player: MacVideo.shared.activePlayer)
    }
}
