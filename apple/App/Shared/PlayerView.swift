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

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let p = playback {
                PlayerController(playback: p, onNext: playNext)
                    .ignoresSafeArea()
                overlays(p)
            } else {
                ProgressView()
            }
        }
        .task {
            let p = VideoPlayback(app: app, playlistID: request.playlistID)
            playback = p
            let tracks = PendingTracks.shared.take(request.itemID)
            #if os(iOS)
            // Downloaded: play from the device (works offline, saves bandwidth).
            if let file = downloads.localURL(request.itemID) {
                await p.startLocal(itemID: request.itemID, file: file, downloads: downloads)
                return
            }
            #endif
            await p.start(itemID: request.itemID, startMs: request.startMs, audio: tracks.audio, subtitle: tracks.subtitle, fileID: tracks.file)
        }
        .onDisappear { Task { await playback?.stop() } }
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
            }
            .padding()
            #endif
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
        vc.transportBarCustomMenuItems = audioMenu()
        #endif
    }

    #if os(tvOS)
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
