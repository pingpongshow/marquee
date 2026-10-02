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
    @Environment(\.dismiss) private var dismiss
    let request: VideoRequest
    @State private var playback: VideoPlayback?
    @State private var countdown: Int?
    @State private var together: WatchTogether?
    @State private var showGroup = false

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let p = playback {
                PlayerController(playback: p, together: together, onNext: playNext)
                    .ignoresSafeArea()
                overlays(p)
            } else {
                ProgressView()
            }
        }
        .task {
            let p = VideoPlayback(app: app, playlistID: request.playlistID)
            playback = p
            let t = WatchTogether(app: app, player: p.player, itemID: request.itemID)
            together = t
            let tracks = PendingTracks.shared.take(request.itemID)
            #if os(iOS)
            // Downloaded: play from the device (works offline, saves bandwidth).
            if let file = downloads.localURL(request.itemID) {
                await p.startLocal(itemID: request.itemID, file: file, downloads: downloads)
                return
            }
            #endif
            await p.start(itemID: request.itemID, startMs: request.startMs, audio: tracks.audio, subtitle: tracks.subtitle, fileID: tracks.file)
            if let g = request.groupID { await t.join(g) }
        }
        .onDisappear {
            together?.leave()
            Task { await playback?.stop() }
        }
        .onChange(of: playback?.finished) { _, done in
            guard done == true else { return }
            if playback?.nextUp != nil { startCountdown() } else { close() }
        }
        #if os(tvOS)
        .onExitCommand { close() }
        #endif
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
                if let t = together { togetherButton(t) }
            }
            .padding()
            #endif
            if let t = together, let e = t.error { Text(e).font(.footnote).foregroundStyle(.red).padding(.horizontal) }
            if let message = p.errorMessage {
                ErrorBanner(message: message).padding().frame(maxWidth: 600)
            }
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

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let vc = AVPlayerViewController()
        vc.player = playback.player
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
        if let m = playback.activeMarker {
            actions.append(UIAction(title: m.kind == .intro ? "Skip Intro" : "Skip Credits") { _ in playback.skipMarker() })
        }
        if playback.nextUp != nil, let item = playback.player.currentItem, item.duration.isNumeric,
           item.duration.seconds - playback.position < 60 {
            actions.append(UIAction(title: "Next Episode", image: UIImage(systemName: "forward.end.fill")) { _ in onNext() })
        }
        if vc.contextualActions.map(\.title) != actions.map(\.title) { vc.contextualActions = actions }
        vc.transportBarCustomMenuItems = audioMenu() + togetherMenu()
        #endif
    }

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
