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
    @State private var countdown: Int?
    @State private var together: WatchTogether?
    @State private var showGroup = false
    /// Cinema trailers (PLAY-18) playing before the movie, and which one is on.
    @State private var trailers: [Item] = []
    @State private var trailerIndex: Int?
    /// Subtitle or audio timing being adjusted (PLAY-17).
    @State private var timing: TimingKind?
    #if os(iOS)
    @State private var casting = false
    @State private var castError: String?
    #endif

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let p = playback {
                PlayerController(playback: p, together: together, onNext: playNext,
                                 trailer: trailerIndex == nil ? nil : TrailerActions(skip: nextTrailer, skipAll: skipTrailers))
                    .ignoresSafeArea()
                overlays(p)
                #if os(iOS)
                if casting { CastingPanel(title: p.item?.title ?? "", close: close) }
                #endif
            } else {
                ProgressView()
            }
        }
        .task {
            music.pause() // a video takes over from the music
            let p = VideoPlayback(app: app, playlistID: request.playlistID)
            playback = p
            RemoteReceiver.shared.video = RemoteVideoTarget(playback: p, next: playNext, close: close)
            let t = WatchTogether(app: app, player: p.player, itemID: request.itemID)
            together = t
            p.onRestart = { [weak t] in t?.restarting() }
            #if os(iOS)
            // Downloaded: play from the device (works offline, saves bandwidth).
            if let file = downloads.localURL(request.itemID) {
                await p.startLocal(itemID: request.itemID, file: file, downloads: downloads)
                return
            }
            #endif
            let reel = await cinemaReel()
            if !reel.isEmpty {
                trailers = reel
                trailerIndex = 0
                await p.start(itemID: reel[0].id, startMs: 0)
                return
            }
            await startFeature(p)
        }
        .onDisappear {
            #if os(iOS)
            let cast = CastController.shared
            cast.videoActive = false
            cast.onVideoConnected = nil
            cast.onVideoEnded = nil
            cast.onVideoFinished = nil
            #endif
            together?.leave()
            if let p = playback, RemoteReceiver.shared.video?.playback === p { RemoteReceiver.shared.video = nil }
            Task { await playback?.stop() }
        }
        .onChange(of: playback?.finished) { _, done in
            guard done == true else { return }
            if trailerIndex != nil { nextTrailer(); return }
            if playback?.nextUp != nil { startCountdown() } else { close() }
        }
        #if os(iOS)
        .sheet(item: $timing) { kind in
            if let p = playback { TimingSheet(kind: kind, playback: p).presentationDetents([.height(300)]) }
        }
        #endif
        #if os(tvOS)
        .onExitCommand { close() }
        #endif
        .remoteToast()
    }

    @ViewBuilder private func overlays(_ p: VideoPlayback) -> some View {
        VStack {
            #if os(iOS)
            HStack {
                Button { close() } label: {
                    Image(systemName: "xmark").font(.headline).padding(12).background(.ultraThinMaterial, in: Circle())
                }
                .accessibilityLabel("Close player")
                Spacer()
                if trailerIndex == nil {
                    if together?.group == nil { optionsMenu(p) }
                    if p.offlineTitle == nil {
                        // Another Marquee app (USER-14): carries on there from here.
                        PlayOnButton(target: {
                            guard let id = p.item?.id else { return nil }
                            return PlayOnTarget(itemIDs: [id], startMs: Int64(p.position * 1000), pauseHere: { p.player.pause() })
                        }, compact: true)
                        .font(.headline).frame(width: 44, height: 44).background(.ultraThinMaterial, in: Circle())
                    }
                    CastButton(tint: .white).frame(width: 44, height: 44).background(.ultraThinMaterial, in: Circle())
                    if let t = together { togetherButton(t) }
                }
            }
            .padding()
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
                if let next = p.nextUp, let countdown {
                    UpNextCard(next: next, seconds: countdown, play: playNext, cancel: { self.countdown = nil; close() })
                        .padding(.trailing, 40).padding(.bottom, 60)
                }
            }
        }
    }

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
            Task { await p.start(itemID: request.itemID, startMs: Int64(at * 1000), audio: s?.audioStreamId, subtitle: s?.subtitleStreamId, fileID: s?.fileId) }
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
                await p.start(itemID: request.itemID, startMs: Int64(at * 1000), audio: s?.audioStreamId, subtitle: s?.subtitleStreamId, fileID: s?.fileId)
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
                Label("\(g.members.count)", systemImage: "person.2.fill").font(.headline).padding(12)
                    .foregroundStyle(Color.marqueeGold).background(.ultraThinMaterial, in: Capsule())
            }
            .accessibilityLabel("Watching together")
        } else {
            Button { Task { await t.start() } } label: {
                Image(systemName: "person.2").font(.headline).padding(12).background(.ultraThinMaterial, in: Circle())
            }
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
        await p.start(itemID: request.itemID, startMs: request.startMs,
                      audio: tracks.audio, subtitle: tracks.subtitle, fileID: tracks.file)
        if let g = request.groupID, let t = together { await t.join(g) }
        #if os(iOS)
        watchCast(p)
        #endif
    }

    private func nextTrailer() {
        guard let i = trailerIndex, let p = playback else { return }
        if i + 1 < trailers.count {
            trailerIndex = i + 1
            Task { await p.start(itemID: trailers[i + 1].id, startMs: 0) }
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
    /// Speed and subtitle/audio timing (PLAY-19, PLAY-17); hidden while watching together.
    private func optionsMenu(_ p: VideoPlayback) -> some View {
        Menu {
            Picker("Speed", selection: Binding(get: { p.speed }, set: { p.speed = $0 })) {
                ForEach(VideoPlayback.speeds, id: \.self) { Text(speedLabel($0)).tag($0) }
            }
            .pickerStyle(.menu)
            if p.offlineTitle == nil {
                Button("Subtitle Timing (\(offsetLabel(p.subtitleOffsetMs)))", systemImage: "captions.bubble") { timing = .subtitle }
                Button("Audio Timing (\(offsetLabel(p.audioOffsetMs)))", systemImage: "waveform") { timing = .audio }
            }
        } label: {
            Image(systemName: "gearshape").font(.headline).padding(12).background(.ultraThinMaterial, in: Circle())
        }
        .accessibilityLabel("Playback settings")
    }
    #endif

    private func startCountdown() {
        countdown = 10
        Task {
            while let c = countdown, c > 0 {
                try? await Task.sleep(for: .seconds(1))
                if countdown != nil { countdown = c - 1 }
            }
            if countdown == 0 { playNext() }
        }
    }

    private func playNext() {
        guard let next = playback?.nextUp else { return }
        countdown = nil
        Task {
            await playback?.stop()
            presenter.play(next.id, startMs: 0, playlistID: request.playlistID)
        }
    }

    private func close() {
        countdown = nil
        presenter.request = nil
        dismiss()
    }
}

private struct UpNextCard: View {
    let next: Item
    let seconds: Int
    let play: () -> Void
    let cancel: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Up next in \(seconds)s").font(.caption).foregroundStyle(.secondary)
            Text(next._type == .episode ? "\(next.grandparentTitle ?? "") · \(next.subtitle) · \(next.title)" : next.title).font(.headline).lineLimit(2)
            HStack {
                Button("Play now", action: play).buttonStyle(.borderedProminent)
                Button("Cancel", action: cancel).buttonStyle(.bordered)
            }
        }
        .padding(16)
        .frame(maxWidth: 380, alignment: .leading)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 14))
    }
}

/// AVPlayerViewController: native controls, PiP, AirPlay, subtitle menu and Now Playing.
struct PlayerController: UIViewControllerRepresentable {
    let playback: VideoPlayback
    var together: WatchTogether?
    let onNext: () -> Void
    /// Set while a cinema trailer plays.
    var trailer: TrailerActions?

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let vc = AVPlayerViewController()
        vc.player = playback.player
        // Speed has its own menu, which watch together can hide (PLAY-19).
        vc.speeds = []
        #if os(iOS)
        vc.updatesNowPlayingInfoCenter = true
        vc.allowsPictureInPicturePlayback = true
        vc.canStartPictureInPictureAutomaticallyFromInline = true
        vc.entersFullScreenWhenPlaybackBegins = false
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        #endif
        return vc
    }

    func updateUIViewController(_ vc: AVPlayerViewController, context: Context) {
        if vc.player !== playback.player { vc.player = playback.player }
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

    final class Coordinator {
        var menuSignature: String?
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    #if os(tvOS)
    /// Everything the transport bar menus depend on.
    private var menuSignature: String {
        let audio = playback.item?.info.versions.first?.files.first?.streams.filter { $0.kind == .audio }.map { String($0.id) } ?? []
        let group = together?.group.map { g in "\(g.id):" + g.members.map(\.name).joined(separator: ",") } ?? "-"
        return [trailer != nil ? "T" : "", "\(playback.speed)", "\(playback.subtitleOffsetMs)", "\(playback.audioOffsetMs)",
                playback.offlineTitle == nil ? "" : "off", audio.joined(separator: ","), "\(playback.session?.audioStreamId ?? -1)",
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

    /// Audio tracks (switching restarts the stream at the same position).
    private func audioMenu() -> [UIMenuElement] {
        let streams = playback.item?.info.versions.first?.files.first?.streams.filter { $0.kind == .audio } ?? []
        guard streams.count > 1 else { return [] }
        let current = playback.session?.audioStreamId
        let actions = streams.map { s in
            UIAction(title: [s.title ?? languageName(s.language), s.codec.uppercased()].joined(separator: " · "),
                     state: s.id == current ? .on : .off) { _ in Task { await playback.change(audio: s.id) } }
        }
        return [UIMenu(title: "Audio", image: UIImage(systemName: "speaker.wave.2"), children: actions)]
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
