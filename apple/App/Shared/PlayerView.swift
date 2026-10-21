import AVKit
import MarqueeKit
import SwiftUI

/// Full-screen video playback.
struct PlayerView: View {
    #if os(iOS)
    @Environment(Downloads.self) private var downloads
    #endif
    @Environment(AppSession.self) private var app
    @Environment(VideoPresenter.self) private var presenter
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss
    let request: VideoRequest
    @State private var playback: VideoPlayback?
    /// Up Next, once a video with something after it has played to its end.
    @State private var upNext: UpNextContent?
    @State private var countdown: Int?
    @State private var countdownTask: Task<Void, Never>?
    /// The person is answering the rating request: the Up Next countdown waits (USER-19).
    @State private var countdownPaused = false
    /// The rating request on the Up Next screen, below the cards (USER-19).
    @State private var endPrompt: RatingPromptItem?
    /// This video's rating request has been made (on the end screen or after closing).
    @State private var prompted = false
    @State private var together: WatchTogether?
    @State private var showGroup = false
    /// Cinema trailers (PLAY-18) playing before the movie, and which one is on.
    @State private var trailers: [Item] = []
    @State private var trailerIndex: Int?
    /// Subtitle or audio timing being adjusted (PLAY-17).
    @State private var timing: TimingKind?
    /// Set when the player closes: a stream that finishes starting after that is stopped again.
    @State private var life = PlayerLife()
    #if os(iOS)
    @State private var casting = false
    @State private var castError: String?
    /// The top buttons come and go with the player's own controls.
    @State private var chrome = PlayerChrome()
    #endif

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let p = playback {
                // On the TV the player's transport bar keeps focus: it goes while Up Next is up,
                // so the remote reaches the Up Next cards.
                if !(isTV && upNext != nil) {
                    PlayerController(playback: p, together: together, onNext: playNext,
                                     trailer: trailerIndex == nil ? nil : TrailerActions(skip: nextTrailer, skipAll: skipTrailers),
                                     chrome: chromeModel)
                        .ignoresSafeArea()
                }
                overlays(p)
                #if os(iOS)
                if casting { CastingPanel(title: p.item?.title ?? "", close: close) }
                #endif
                if let content = upNext, let countdown {
                    UpNextScreen(content: content, seconds: countdown, paused: countdownPaused,
                                 playNext: playNext, replay: replay, cancel: close) {
                        if let ask = endPrompt {
                            RatingPromptCard(prompt: ask, onInteract: { countdownPaused = true }) {
                                endPrompt = nil
                                countdownPaused = false
                            }
                        }
                    }
                }
                // The time limit refused or ended playback (kids' profiles): the reason, full
                // screen, instead of an error; nothing plays behind it.
                if let reason = p.timeLimitReason { TimeLimitScreen(reason: reason, close: close) }
            } else {
                ProgressView()
            }
        }
        .task {
            music.pause() // a video takes over from the music
            Task { await app.refreshInfo() } // the rating request switch may have changed (USER-19)
            let p = VideoPlayback(app: app, playlistID: request.playlistID, order: request.order)
            playback = p
            let t = WatchTogether(app: app, player: p.player, itemID: request.itemID)
            together = t
            p.onRestart = { [weak t] in t?.restarting() }
            #if os(iOS)
            // Downloaded: play from the device (works offline, saves bandwidth).
            if let file = downloads.localURL(request.itemID) {
                await begin(p) { await p.startLocal(itemID: request.itemID, file: file, downloads: downloads) }
                return
            }
            #endif
            let reel = await cinemaReel()
            if !reel.isEmpty {
                trailers = reel
                trailerIndex = 0
                await begin(p) { await p.start(itemID: reel[0].id, startMs: 0) }
                return
            }
            await startFeature(p)
        }
        .onDisappear {
            life.closed = true
            #if os(iOS)
            let cast = CastController.shared
            cast.videoActive = false
            cast.onVideoConnected = nil
            cast.onVideoEnded = nil
            cast.onVideoFinished = nil
            #endif
            together?.leave()
            Task { await playback?.stop() }
        }
        .onChange(of: playback?.finished) { _, done in
            guard done == true, playback?.timeLimitReason == nil else { return }
            if trailerIndex != nil { nextTrailer(); return }
            if let p = playback, let next = p.nextUp {
                // The rating request sits below the cards; autoplay carries on unless the
                // person touches it (USER-19).
                if !prompted, let ask = RatingPrompt.wanted(p.item, app: app) {
                    prompted = true
                    endPrompt = ask
                }
                showUpNext(p, next: next)
            } else {
                close() // asks for a rating over the page (USER-19)
            }
        }
        #if os(iOS)
        .sheet(item: $timing) { kind in
            if let p = playback { TimingSheet(kind: kind, playback: p).presentationDetents([.height(300)]) }
        }
        #endif
        #if os(tvOS)
        .onExitCommand { close() }
        #endif
    }

    @ViewBuilder private func overlays(_ p: VideoPlayback) -> some View {
        VStack {
            #if os(iOS)
            // Hidden buttons leave the screen altogether (and the accessibility tree); the row
            // keeps its space so nothing below it moves.
            ZStack {
                if chrome.shown { topButtons(p).transition(.opacity) }
            }
            .frame(height: 44)
            .frame(maxWidth: .infinity)
            // Below AVKit's own top bar (PiP, volume), so neither covers the other.
            .padding(.horizontal)
            .padding(.top, PlayerChrome.topInset)
            .animation(.easeInOut(duration: 0.25), value: chrome.shown)
            #endif
            if let i = trailerIndex, trailers.indices.contains(i) { trailerBar(trailers[i]) }
            if let t = together, let e = t.error { Text(e).font(.footnote).foregroundStyle(.red).padding(.horizontal) }
            if let message = p.errorMessage {
                ErrorBanner(message: message).padding().frame(maxWidth: 600)
            }
            #if os(iOS)
            if let castError { ErrorBanner(message: "Couldn't cast: \(castError)").padding().frame(maxWidth: 600) }
            #endif
            Spacer()
            HStack {
                Spacer()
                #if os(iOS)
                if let m = p.activeMarker {
                    Button { p.skipMarker() } label: {
                        Text(m.kind == .intro ? "Skip Intro" : "Skip Credits").font(.headline).padding(.horizontal, 18).padding(.vertical, 10)
                            .background(.ultraThinMaterial, in: Capsule())
                    }
                    .padding(.trailing, 24).padding(.bottom, 90)
                }
                #endif
            }
        }
    }

    #if os(iOS)
    /// Close at the left; settings, Cast and Watch Together at the right.
    private func topButtons(_ p: VideoPlayback) -> some View {
        HStack {
            Button { close() } label: { PlayerBubble(systemImage: "xmark") }
                .accessibilityLabel("Close player")
            Spacer()
            if trailerIndex == nil {
                if together?.group == nil { optionsMenu(p) }
                CastButton(tint: .white).frame(width: 28, height: 28).modifier(PlayerBubbleBackground())
                if let t = together { togetherButton(t) }
            }
        }
        .buttonStyle(.plain)
        .foregroundStyle(.white)
    }
    #endif

    #if os(iOS)
    /// Chromecast (D82): when a device connects, the video moves there at the current
    /// position; when casting stops, it carries on here from where the TV was.
    private func watchCast(_ p: VideoPlayback) {
        let cast = CastController.shared
        cast.videoActive = true
        cast.onVideoConnected = { handOff(p) }
        cast.onVideoFinished = { close() }
        cast.onVideoEnded = { at in
            casting = false
            let s = p.session
            Task { await begin(p) { await p.start(itemID: request.itemID, startMs: Int64(at * 1000), audio: s?.audioStreamId, subtitle: s?.subtitleStreamId, fileID: s?.fileId) } }
        }
        if cast.device != nil { handOff(p) } // already casting when the video opened
    }

    private func handOff(_ p: VideoPlayback) {
        guard !casting, p.offlineTitle == nil else { return }
        casting = true
        let at = p.position
        let s = p.session
        let item = p.item
        Task {
            await p.handOff()
            let art = app.imageURL(item?.base.images?.backdrop ?? item?.base.images?.poster, width: 640)
            if let err = await CastController.shared.load(itemID: request.itemID, at: at, title: item?.title ?? "", subtitle: item?.base.year.map(String.init),
                                                          artwork: art, music: false, fileID: s?.fileId, audio: s?.audioStreamId, subtitleStream: s?.subtitleStreamId) {
                // Couldn't cast: say why and carry on here.
                castError = err
                casting = false
                await begin(p) { await p.start(itemID: request.itemID, startMs: Int64(at * 1000), audio: s?.audioStreamId, subtitle: s?.subtitleStreamId, fileID: s?.fileId) }
            }
        }
    }

    /// Watch together: start a group, or see who's in it and leave.
    @ViewBuilder private func togetherButton(_ t: WatchTogether) -> some View {
        if let g = t.group {
            Menu {
                Section("Watching together") {
                    ForEach(g.members, id: \.userId) { m in Label(m.name + (m.buffering ? " (loading)" : ""), systemImage: "person") }
                }
                if let by = g.lastBy, let what = g.lastAction { Text("\(by): \(what)") }
                Button("Leave the Group", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { t.leave() }
            } label: {
                Label("\(g.members.count)", systemImage: "person.2.fill").font(.headline).padding(.horizontal, 12)
                    .frame(height: 44).foregroundStyle(Color.marqueeGold).background(.ultraThinMaterial, in: Capsule())
            }
            .accessibilityLabel("Watching together")
        } else {
            Button { Task { await t.start() } } label: { PlayerBubble(systemImage: "person.2") }
            .accessibilityLabel("Watch together")
        }
    }
    #endif

    // MARK: - Cinema trailers (PLAY-18)

    /// Trailers and the pre-roll to play first: only for a movie started from the beginning,
    /// not when joining a group or casting.
    private func cinemaReel() async -> [Item] {
        guard request.groupID == nil else { return [] }
        #if os(iOS)
        if CastController.shared.device != nil { return [] }
        #endif
        var fromStart = request.startMs == 0
        if request.startMs == nil, let d = try? await app.item(request.itemID) {
            fromStart = d.type == .movie && (d.base.viewOffsetMs ?? 0) == 0
        }
        guard fromStart else { return [] }
        return await app.prerolls(request.itemID)
    }

    /// The movie itself (after any trailers), with the tracks chosen on its page.
    private func startFeature(_ p: VideoPlayback) async {
        trailerIndex = nil
        let tracks = PendingTracks.shared.take(request.itemID)
        guard await begin(p, {
            await p.start(itemID: request.itemID, startMs: request.startMs,
                          audio: tracks.audio, subtitle: tracks.subtitle, fileID: tracks.file)
        }) else { return }
        if let g = request.groupID, let t = together { await t.join(g) }
        #if os(iOS)
        watchCast(p)
        #endif
    }

    /// Starts (or restarts) the stream unless the player has closed meanwhile. One that
    /// finishes starting after it closed (a slow server, a trailer lookup) is stopped again, so
    /// nothing plays behind the page or keeps a session open on the server. False when closed.
    @discardableResult
    private func begin(_ p: VideoPlayback, _ start: () async -> Void) async -> Bool {
        let life = life // read while the view is installed
        guard !life.closed else { return false }
        await start()
        if life.closed {
            await p.stop()
            return false
        }
        return true
    }

    private func nextTrailer() {
        guard let i = trailerIndex, let p = playback else { return }
        if i + 1 < trailers.count {
            trailerIndex = i + 1
            let next = trailers[i + 1].id
            Task { await begin(p) { await p.start(itemID: next, startMs: 0) } }
        } else {
            skipTrailers()
        }
    }

    private func skipTrailers() {
        guard trailerIndex != nil, let p = playback else { return }
        trailerIndex = nil
        Task { await startFeature(p) }
    }

    /// What's showing, with Skip and Skip All.
    private func trailerBar(_ t: Item) -> some View {
        HStack(spacing: 12) {
            Text("Trailer · \(t.parentTitle ?? t.title)").font(.headline).lineLimit(1).minimumScaleFactor(0.7)
                .padding(.horizontal, 14).padding(.vertical, 8)
                .background(.ultraThinMaterial, in: Capsule())
                .accessibilityIdentifier("trailerTitle")
            Spacer()
            #if os(iOS)
            Button("Skip") { nextTrailer() }.buttonStyle(.bordered)
            Button("Skip All") { skipTrailers() }.buttonStyle(.borderedProminent)
            #endif
        }
        .padding(.horizontal)
        #if os(tvOS)
        .padding(.top, 40)
        #endif
    }

    #if os(iOS)
    /// Speed, subtitle/audio timing and night mode (PLAY-19, PLAY-17); hidden while watching
    /// together.
    private func optionsMenu(_ p: VideoPlayback) -> some View {
        Menu {
            Picker("Speed", selection: Binding(get: { p.speed }, set: { p.speed = $0 })) {
                ForEach(VideoPlayback.speeds, id: \.self) { Text(speedLabel($0)).tag($0) }
            }
            .pickerStyle(.menu)
            if p.offlineTitle == nil {
                Button("Subtitle Timing (\(offsetLabel(p.subtitleOffsetMs)))", systemImage: "captions.bubble") { timing = .subtitle }
                Button("Audio Timing (\(offsetLabel(p.audioOffsetMs)))", systemImage: "waveform") { timing = .audio }
                Section {
                    // Restarts the stream at the same position, like an audio track change.
                    Toggle(isOn: Binding(get: { p.nightMode }, set: { on in Task { await p.setNightMode(on) } })) {
                        Label("Night Mode", systemImage: "moon")
                        Text("Quieter loud scenes, clearer dialogue")
                    }
                    .accessibilityIdentifier("nightMode")
                }
            }
        } label: {
            PlayerBubble(systemImage: "gearshape")
        }
        .accessibilityLabel("Playback settings")
    }
    #endif

    // MARK: - Up Next

    /// The Up Next screen and its countdown; the next one's overview (and the show's art, when
    /// the episode has none) arrive as it shows.
    private func showUpNext(_ p: VideoPlayback, next: Item) {
        let ended = p.item
        upNext = UpNextContent(previous: ended.map { upNextEntry($0.base, overview: $0.info.summary) },
                               next: upNextEntry(next, overview: nil),
                               background: app.imageURL(next.images?.backdrop ?? ended?.base.images?.backdrop, pixels: 1920))
        startCountdown()
        Task {
            let detail = try? await app.item(next.id)
            guard upNext != nil, playback?.nextUp?.id == next.id else { return }
            if let o = detail?.info.summary { upNext?.next.overview = o }
            if upNext?.background == nil, let show = next.grandparentId, let s = try? await app.item(show), upNext != nil {
                upNext?.background = app.imageURL(s.base.images?.backdrop ?? s.base.images?.poster, pixels: 1920)
            }
        }
    }

    private func upNextEntry(_ i: Item, overview: String?) -> UpNextEntry {
        let still = app.imageURL(i.images?.thumb ?? i.images?.backdrop ?? i.images?.poster, width: isTV ? 640 : 420)
        if i._type == .episode {
            return UpNextEntry(heading: i.grandparentTitle ?? i.title, title: i.title,
                               detail: UpNextEntry.episodeLine(season: nil, seasonTitle: i.parentTitle, episode: i.index),
                               overview: overview, image: still)
        }
        return UpNextEntry(heading: i.title, title: nil, detail: i.year.map(String.init) ?? "", overview: overview, image: still)
    }

    private func startCountdown() {
        countdownTask?.cancel()
        countdown = UpNextContent.countdown
        countdownPaused = false
        countdownTask = Task {
            while let c = countdown, c > 0 {
                try? await Task.sleep(for: .seconds(1))
                if Task.isCancelled { return }
                if let now = countdown, !countdownPaused { countdown = now - 1 }
            }
            if countdown == 0 { playNext() }
        }
    }

    /// Up Next goes (playing on, playing again, or closing).
    private func endUpNext() {
        countdownTask?.cancel()
        countdownTask = nil
        countdown = nil
        endPrompt = nil
        upNext = nil
    }

    /// The one that ended, again from its start, in this player.
    private func replay() {
        guard let p = playback else { return }
        endUpNext()
        let s = p.session
        Task {
            #if os(iOS)
            if p.offlineTitle != nil, let file = downloads.localURL(request.itemID) {
                guard await begin(p, { await p.startLocal(itemID: request.itemID, file: file, downloads: downloads) }) else { return }
                await p.player.seek(to: .zero)
                return
            }
            #endif
            await begin(p) {
                await p.start(itemID: request.itemID, startMs: 0, audio: s?.audioStreamId, subtitle: s?.subtitleStreamId, fileID: s?.fileId)
            }
        }
    }

    private func playNext() {
        guard let next = playback?.nextUp else { return }
        endUpNext()
        Task {
            await playback?.stop()
            presenter.play(next.id, startMs: 0, playlistID: request.playlistID, order: request.order)
        }
    }

    /// The overlay's visibility model (iPhone/iPad only; the TV has no overlay buttons).
    private var chromeModel: PlayerChrome? {
        #if os(iOS)
        chrome
        #else
        nil
        #endif
    }

    private func close() {
        askAfterClosing()
        endUpNext()
        presenter.request = nil
        dismiss()
    }

    /// A movie or episode that played to the end, or far enough to count as watched, asks for
    /// a rating over the page once the player has gone (USER-19).
    private func askAfterClosing() {
        guard !prompted, trailerIndex == nil, let p = playback, watchedEnough(p),
              let ask = RatingPrompt.wanted(p.item, app: app) else { return }
        prompted = true
        RatingPrompt.shared.pending = ask
    }

    /// Played to the end, or 90% of the way (as the server counts a play).
    private func watchedEnough(_ p: VideoPlayback) -> Bool {
        if p.finished { return true }
        var duration = p.player.currentItem?.duration.seconds ?? .nan
        if !duration.isFinite || duration <= 0, let ms = p.item?.base.durationMs { duration = Double(ms) / 1000 }
        return duration.isFinite && duration > 0 && p.position >= duration * 0.9
    }
}

/// Whether the player has closed (a reference, so work that outlives the view can see it).
@MainActor final class PlayerLife {
    var closed = false
}

/// When the player's own overlay buttons (close, settings, Cast, Watch Together)
/// show. AVPlayerViewController has no public callback for its controls coming and going, so
/// this keeps roughly in step with them: shown when playback starts, on a tap, while paused or
/// while a menu or sheet is open; hidden after a few seconds of playing untouched, and in
/// Picture in Picture.
@MainActor @Observable
final class PlayerChrome {
    /// About when AVKit hides its own controls.
    static var hideAfter: Duration = .seconds(3)
    /// AVKit's inline top bar (PiP, volume) sits in the top 50 points or so; the overlay goes below it.
    static let topInset: CGFloat = 56

    private(set) var visible = true
    private(set) var pip = false
    var shown: Bool { visible && !pip }

    @ObservationIgnored private var playing = false
    @ObservationIgnored private var hideTask: Task<Void, Never>?
    /// True while something covers the player (a menu or a sheet): don't hide then.
    @ObservationIgnored var covered: () -> Bool = { false }

    /// A tap on the video: like AVKit, shows the controls when hidden and hides them when shown.
    func toggle() { visible ? hide() : show() }

    /// Some interaction: show, and hide again after a while if playing.
    func show() {
        if !visible { visible = true } // no needless redraws: one would cancel a menu opening
        scheduleHide()
    }

    func hide() {
        hideTask?.cancel()
        if visible { visible = false }
    }

    func setPlaying(_ on: Bool) {
        guard on != playing else { return }
        playing = on
        if on { scheduleHide() } else { show() } // paused: stay up
    }

    func setPiP(_ on: Bool) {
        if pip != on { pip = on }
        if !on { show() }
    }

    /// Hides once playing untouched for `hideAfter`; time under a menu or sheet doesn't count,
    /// so closing one gives the full delay again.
    private func scheduleHide() {
        hideTask?.cancel()
        hideTask = Task { [weak self] in
            var last = ContinuousClock.now
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(250))
                guard !Task.isCancelled, let self else { return }
                guard self.playing else { return } // paused: shown until it plays again
                if self.covered() { last = .now; continue }
                if ContinuousClock.now - last >= Self.hideAfter { self.visible = false; return }
            }
        }
    }
}

/// Holds the AVPlayerViewController and adds it once this view has a size. Added during
/// SwiftUI's first layout pass, at zero width, its controls break their own constraints.
final class PlayerHost: UIViewController {
    let controller = AVPlayerViewController()
    var chrome: PlayerChrome?

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        guard controller.parent == nil, view.bounds.width > 0, view.bounds.height > 0 else { return }
        addChild(controller)
        controller.view.frame = view.bounds
        controller.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(controller.view)
        controller.didMove(toParent: self)
        #if os(iOS)
        // Taps on the video bring the overlay buttons back (or hide them), alongside AVKit's
        // own controls: recognised together with AVKit's gestures, without taking the touch.
        let tap = UITapGestureRecognizer(target: self, action: #selector(tapped(_:)))
        tap.cancelsTouchesInView = false
        tap.delaysTouchesBegan = false
        tap.delaysTouchesEnded = false
        tap.delegate = self
        controller.view.addGestureRecognizer(tap)
        #endif
    }

    #if os(iOS)
    @objc private func tapped(_ g: UITapGestureRecognizer) {
        guard let chrome, let root = controller.view else { return }
        // A tap on one of AVKit's buttons or the scrubber is using the controls: keep both up.
        var hit = root.hitTest(g.location(in: root), with: nil)
        while let v = hit, v !== root {
            if v is UIControl { chrome.show(); return }
            hit = v.superview
        }
        chrome.toggle()
    }

    /// Whether a menu or a sheet is over the player.
    func covered() -> Bool {
        guard let window = view.window else { return false }
        // A menu's container goes into the window, or a window of its own.
        func menu(in v: UIView, depth: Int) -> Bool {
            let name = String(describing: type(of: v))
            if name.contains("ContextMenu") || name.contains("MenuContainer") { return true }
            return depth > 0 && v.subviews.contains { menu(in: $0, depth: depth - 1) }
        }
        let windows = window.windowScene?.windows ?? [window]
        if windows.contains(where: { !$0.isHidden && menu(in: $0, depth: 3) }) { return true }
        var top = window.rootViewController
        while let next = top?.presentedViewController { top = next }
        if let top, !view.isDescendant(of: top.view) { return true }
        return false
    }
    #endif

    // The Siri Remote's focus goes to the player's controls.
    override var preferredFocusEnvironments: [any UIFocusEnvironment] { [controller] }
}

#if os(iOS)
/// One round bubble per overlay button: a single material background, no system button
/// chrome around it.
struct PlayerBubble: View {
    let systemImage: String
    var body: some View {
        Image(systemName: systemImage).font(.headline).modifier(PlayerBubbleBackground())
    }
}

struct PlayerBubbleBackground: ViewModifier {
    func body(content: Content) -> some View {
        content
            .frame(width: 44, height: 44)
            .background(.ultraThinMaterial, in: Circle())
            .contentShape(Circle())
            .contentShape(.hoverEffect, Circle())
    }
}

/// Picture in Picture hides the overlay buttons; coming back shows them.
extension PlayerController.Coordinator: AVPlayerViewControllerDelegate {
    func playerViewControllerWillStartPictureInPicture(_ vc: AVPlayerViewController) {
        Task { @MainActor in chrome?.setPiP(true) }
    }

    func playerViewControllerDidStopPictureInPicture(_ vc: AVPlayerViewController) {
        Task { @MainActor in chrome?.setPiP(false) }
    }
}

extension PlayerHost: UIGestureRecognizerDelegate {
    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
}
#endif

/// AVPlayerViewController: native controls, PiP, AirPlay, subtitle menu and Now Playing.
struct PlayerController: UIViewControllerRepresentable {
    let playback: VideoPlayback
    var together: WatchTogether?
    let onNext: () -> Void
    /// Set while a cinema trailer plays.
    var trailer: TrailerActions?
    /// The overlay buttons' visibility, kept in step with taps, play/pause and PiP.
    var chrome: PlayerChrome?

    func makeUIViewController(context: Context) -> PlayerHost {
        let host = PlayerHost()
        let vc = host.controller
        vc.player = playback.player
        host.chrome = chrome
        #if os(iOS)
        vc.delegate = context.coordinator
        context.coordinator.chrome = chrome
        context.coordinator.watch(playback.player)
        chrome?.covered = { [weak host] in host?.covered() ?? false }
        #endif
        // Speed has its own menu, which watch together can hide (PLAY-19).
        vc.speeds = []
        #if os(iOS)
        vc.updatesNowPlayingInfoCenter = true
        vc.allowsPictureInPicturePlayback = true
        vc.canStartPictureInPictureAutomaticallyFromInline = true
        vc.entersFullScreenWhenPlaybackBegins = false
        #endif
        // Movie mode: Apple TV offers Enhance Dialogue and similar audio options for it.
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        return host
    }

    func updateUIViewController(_ host: PlayerHost, context: Context) {
        let vc = host.controller
        if vc.player !== playback.player { vc.player = playback.player }
        #if os(iOS)
        context.coordinator.watch(playback.player)
        #endif
        #if os(tvOS)
        // Siri Remote: "Skip Intro" and "Next Episode" appear as contextual actions.
        var actions: [UIAction] = []
        if let trailer {
            actions = [UIAction(title: "Skip Trailer") { _ in trailer.skip() },
                       UIAction(title: "Skip All Trailers") { _ in trailer.skipAll() }]
        } else if let m = playback.activeMarker {
            actions.append(UIAction(title: m.kind == .intro ? "Skip Intro" : "Skip Credits") { _ in playback.skipMarker() })
        }
        // nearEnd flips once in the last minute; reading the position here rebuilt the menus
        // every second and closed any that were open.
        if trailer == nil, playback.nextUp != nil, playback.nearEnd {
            actions.append(UIAction(title: "Next Episode", image: UIImage(systemName: "forward.end.fill")) { _ in onNext() })
        }
        if vc.contextualActions.map(\.title) != actions.map(\.title) { vc.contextualActions = actions }
        // The transport bar menus are replaced only when what they show changes.
        let signature = menuSignature
        if context.coordinator.menuSignature != signature {
            context.coordinator.menuSignature = signature
            vc.transportBarCustomMenuItems = trailer != nil ? [] : speedMenu() + timingMenus() + audioMenu() + togetherMenu()
        }
        #endif
    }

    final class Coordinator: NSObject {
        var menuSignature: String?
        #if os(iOS)
        weak var chrome: PlayerChrome?
        private weak var watched: AVPlayer?
        private var rate: NSKeyValueObservation?

        /// Playing or paused, for the overlay buttons (they stay up while paused).
        func watch(_ player: AVPlayer) {
            guard watched !== player else { return }
            watched = player
            rate = player.observe(\.timeControlStatus, options: [.initial, .new]) { [weak self] p, _ in
                let playing = p.timeControlStatus != .paused
                Task { @MainActor in self?.chrome?.setPlaying(playing) }
            }
        }
        #endif
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    #if os(tvOS)
    /// Everything the transport bar menus depend on.
    private var menuSignature: String {
        let audio = playback.item?.info.versions.first?.files.first?.streams.filter { $0.kind == .audio }.map { String($0.id) } ?? []
        let group = together?.group.map { g in "\(g.id):" + g.members.map(\.name).joined(separator: ",") } ?? "-"
        return [trailer != nil ? "T" : "", "\(playback.speed)", "\(playback.subtitleOffsetMs)", "\(playback.audioOffsetMs)",
                playback.offlineTitle == nil ? "" : "off", audio.joined(separator: ","), "\(playback.session?.audioStreamId ?? -1)",
                playback.nightMode ? "night" : "",
                together == nil ? "" : "t", group].joined(separator: "|")
    }
    #endif

    #if os(tvOS)
    /// Watch together from the Siri Remote's transport bar.
    private func togetherMenu() -> [UIMenuElement] {
        guard let t = together else { return [] }
        if let g = t.group {
            let people = g.members.map { UIAction(title: $0.name, attributes: .disabled) { _ in } }
            return [UIMenu(title: "Watching Together (\(g.members.count))", image: UIImage(systemName: "person.2.fill"),
                           children: people + [UIAction(title: "Leave the Group", attributes: .destructive) { _ in t.leave() }])]
        }
        return [UIAction(title: "Watch Together", image: UIImage(systemName: "person.2")) { _ in Task { await t.start() } }]
    }

    /// Playback speed (PLAY-19); not while watching together.
    private func speedMenu() -> [UIMenuElement] {
        guard together?.group == nil else { return [] }
        let actions = VideoPlayback.speeds.map { v in
            UIAction(title: speedLabel(v), state: playback.speed == v ? .on : .off) { _ in playback.speed = v }
        }
        return [UIMenu(title: "Speed", image: UIImage(systemName: "gauge.with.dots.needle.67percent"), children: actions)]
    }

    /// Subtitle and audio timing in 100 ms steps (PLAY-17); not while watching together.
    private func timingMenus() -> [UIMenuElement] {
        guard together?.group == nil, playback.offlineTitle == nil else { return [] }
        func menu(_ title: String, _ icon: String, _ value: Int, _ set: @escaping (Int) -> Void) -> UIMenu {
            UIMenu(title: "\(title) (\(offsetLabel(value)))", image: UIImage(systemName: icon), children: [
                UIAction(title: "100 ms Earlier", image: UIImage(systemName: "minus")) { _ in set(value - 100) },
                UIAction(title: "100 ms Later", image: UIImage(systemName: "plus")) { _ in set(value + 100) },
                UIAction(title: "Reset", attributes: value == 0 ? .disabled : []) { _ in set(0) },
            ])
        }
        // Icons apart from the player's own Subtitles and Audio buttons beside them.
        return [menu("Subtitle Timing", "timer", playback.subtitleOffsetMs) { playback.setOffsets(subtitle: $0) },
                menu("Audio Timing", "metronome", playback.audioOffsetMs) { playback.setOffsets(audio: $0) }]
    }

    /// Audio tracks and night mode (switching either restarts the stream at the same position).
    private func audioMenu() -> [UIMenuElement] {
        guard playback.offlineTitle == nil else { return [] }
        let streams = playback.item?.info.versions.first?.files.first?.streams.filter { $0.kind == .audio } ?? []
        let current = playback.session?.audioStreamId
        let tracks: [UIMenuElement] = streams.count > 1 ? [UIMenu(options: .displayInline, children: streams.map { s in
            UIAction(title: [s.title ?? languageName(s.language), s.codec.uppercased()].joined(separator: " · "),
                     state: s.id == current ? .on : .off) { _ in Task { await playback.change(audio: s.id) } }
        })] : []
        let on = playback.nightMode
        let night = UIAction(title: "Night Mode", subtitle: "Quieter loud scenes, clearer dialogue", image: UIImage(systemName: "moon"),
                             state: on ? .on : .off) { _ in Task { await playback.setNightMode(!on) } }
        night.accessibilityIdentifier = "nightMode"
        return [UIMenu(title: "Audio", image: UIImage(systemName: "speaker.wave.2"),
                       children: tracks + [UIMenu(options: .displayInline, children: [night])])]
    }
    #endif
}

/// Skip buttons for a cinema trailer (the TV shows them as contextual actions).
struct TrailerActions {
    let skip: () -> Void
    let skipAll: () -> Void
}

enum TimingKind: String, Identifiable {
    case subtitle, audio
    var id: String { rawValue }
}

func speedLabel(_ v: Float) -> String {
    v == 1 ? "Normal (1×)" : "\(v.formatted(.number.precision(.fractionLength(0...2))))×"
}

/// "+100 ms", "−250 ms", "0 ms".
func offsetLabel(_ ms: Int) -> String {
    ms == 0 ? "0 ms" : ms > 0 ? "+\(ms) ms" : "−\(-ms) ms"
}

#if os(iOS)
/// Subtitle or audio timing: −/+ 100 ms, the current value and Reset (PLAY-17).
struct TimingSheet: View {
    let kind: TimingKind
    let playback: VideoPlayback
    @Environment(\.dismiss) private var dismiss

    private var value: Int { kind == .subtitle ? playback.subtitleOffsetMs : playback.audioOffsetMs }

    var body: some View {
        NavigationStack {
            VStack(spacing: 18) {
                Text(offsetLabel(value)).font(.system(size: 40, weight: .bold).monospacedDigit())
                    .accessibilityIdentifier("timingValue")
                Text(value == 0 ? "As in the file" : value > 0 ? "\(kind == .subtitle ? "Subtitles" : "Sound") later" : "\(kind == .subtitle ? "Subtitles" : "Sound") earlier")
                    .font(.subheadline).foregroundStyle(.secondary)
                HStack(spacing: 28) {
                    Button { set(value - 100) } label: { Image(systemName: "minus").font(.title2.bold()).frame(width: 64, height: 44) }
                        .accessibilityLabel("100 ms earlier")
                    Button("Reset") { set(0) }.disabled(value == 0)
                    Button { set(value + 100) } label: { Image(systemName: "plus").font(.title2.bold()).frame(width: 64, height: 44) }
                        .accessibilityLabel("100 ms later")
                }
                .buttonStyle(.bordered)
                Text("The video picks up where you are with the new timing, and Marquee remembers it for this file.")
                    .font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            .padding()
            .navigationTitle(kind == .subtitle ? "Subtitle Timing" : "Audio Timing")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
    }

    private func set(_ ms: Int) {
        if kind == .subtitle { playback.setOffsets(subtitle: ms) } else { playback.setOffsets(audio: ms) }
    }
}

/// Shown over the player while the video plays on a Cast device: what's casting and its controls.
struct CastingPanel: View {
    let title: String
    let close: () -> Void
    @State private var drag: Double?

    var body: some View {
        let cast = CastController.shared
        ZStack {
            Color.black.ignoresSafeArea()
            VStack(spacing: 14) {
                HStack {
                    Button(action: close) { Image(systemName: "xmark").font(.headline).padding(12).background(.ultraThinMaterial, in: Circle()) }
                        .accessibilityLabel("Close player")
                    Spacer()
                    CastButton(tint: .white).frame(width: 44, height: 44)
                }
                Spacer()
                Image(systemName: "tv.and.mediabox").font(.system(size: 44)).foregroundStyle(Color.marqueeGold)
                Text("Playing on \(cast.device ?? "Chromecast")").foregroundStyle(.secondary)
                Text(title).font(.title2.bold()).multilineTextAlignment(.center).lineLimit(2)
                if cast.duration > 0 {
                    Slider(value: Binding(get: { drag ?? cast.position }, set: { drag = $0 }), in: 0...cast.duration) { editing in
                        if !editing, let d = drag { cast.seekTo(d); drag = nil }
                    }
                    HStack {
                        Text(formatTime(cast.position)).font(.caption.monospacedDigit())
                        Spacer()
                        Text(formatTime(cast.duration)).font(.caption.monospacedDigit())
                    }
                    .foregroundStyle(.secondary)
                }
                HStack(spacing: 44) {
                    Button { cast.seekTo(max(0, cast.position - 10)) } label: { Image(systemName: "gobackward.10") }.accessibilityLabel("Back 10 seconds")
                    Button { cast.toggle() } label: { Image(systemName: cast.playing ? "pause.fill" : "play.fill").font(.system(size: 44)) }
                        .accessibilityLabel(cast.playing ? "Pause" : "Play")
                    Button { cast.seekTo(cast.position + 30) } label: { Image(systemName: "goforward.30") }.accessibilityLabel("Forward 30 seconds")
                }
                .font(.title)
                .foregroundStyle(.white)
                Spacer()
            }
            .padding()
            .frame(maxWidth: 600)
        }
    }

    private func formatTime(_ s: Double) -> String {
        let t = Int(s.rounded())
        return t >= 3600 ? String(format: "%d:%02d:%02d", t / 3600, (t % 3600) / 60, t % 60) : String(format: "%d:%02d", t / 60, t % 60)
    }
}
#endif
