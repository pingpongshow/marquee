import AVFoundation
import MarqueeKit
import SwiftUI

/// The open video player, as remote control sees it.
struct RemoteVideoTarget {
    let playback: VideoPlayback
    /// Plays what's next (the next episode or playlist entry).
    let next: () -> Void
    /// Closes the player.
    let close: () -> Void
}

/// Remote control, the player side (USER-14): while the app is open (or playing music in the
/// background) it long-polls its inbox, so it's listed as a player, runs the commands it
/// gets, and reports what it's playing on every change and every 10 s while playing.
@MainActor @Observable
final class RemoteReceiver {
    static let shared = RemoteReceiver()

    /// "Controlled from …", shown for a few seconds.
    private(set) var toast: String?

    @ObservationIgnored private weak var app: AppSession?
    @ObservationIgnored private weak var music: MarqueeKit.MusicPlayer?
    @ObservationIgnored private weak var presenter: VideoPresenter?
    /// Set by the video player while it's open.
    @ObservationIgnored var video: RemoteVideoTarget?
    @ObservationIgnored private var loop: Task<Void, Never>?
    @ObservationIgnored private var reporter: Task<Void, Never>?
    @ObservationIgnored private var toastTask: Task<Void, Never>?
    @ObservationIgnored private var cursor: Int64 = 0
    /// The controller of this session (the toast shows when one first takes control).
    @ObservationIgnored private var controller: String?
    /// The last state sent: its signature, position and when.
    @ObservationIgnored private var last: (signature: String, positionMs: Int64, at: Date, playing: Bool)?

    var running: Bool { loop != nil }

    func start(app: AppSession, music: MarqueeKit.MusicPlayer, presenter: VideoPresenter) {
        self.app = app
        self.music = music
        self.presenter = presenter
        guard loop == nil else { return }
        loop = Task { [weak self] in await self?.poll() }
        reporter = Task { [weak self] in
            while !Task.isCancelled {
                self?.reportIfChanged()
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    func stop() {
        loop?.cancel()
        reporter?.cancel()
        loop = nil
        reporter = nil
        controller = nil
        last = nil
    }

    // MARK: - Inbox

    private func poll() async {
        while !Task.isCancelled {
            guard let app, app.state == .signedIn, app.longPollClient != nil else {
                try? await Task.sleep(for: .seconds(2))
                continue
            }
            do {
                let inbox = try await app.remoteInbox(cursor: cursor, state: currentState())
                guard !Task.isCancelled else { return }
                cursor = inbox.cursor
                for c in inbox.commands { await execute(c) }
            } catch {
                if Task.isCancelled { return }
                // Refused: check the session (this signs out if it really has ended).
                if (error as? MarqueeError)?.message == "Signed out" { await app.reconnect() }
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    // MARK: - Commands

    private func execute(_ c: RemoteCommand) async {
        if let from = c.from, from != controller {
            controller = from
            show("Controlled from \(from)")
        }
        let v = video
        switch c._type {
        case .play: await play(c)
        case .pause:
            if let v { v.playback.player.pause() } else { music?.pause() }
        case .resume:
            if let v { v.playback.player.play() } else { music?.resume() }
        case .seek:
            let s = Double(c.positionMs ?? 0) / 1000
            if let v {
                await v.playback.player.seek(to: CMTime(seconds: s, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
            } else {
                music?.seek(s)
            }
        case .stop:
            if let v { v.close() } else { music?.stop() }
        case .next:
            if let v { v.next() } else { music?.next() }
        case .previous:
            if let v { await v.playback.player.seek(to: .zero) } else { music?.previous() }
        case .setAudio:
            if let v, let id = c.streamId { await v.playback.change(audio: id) }
        case .setSubtitle:
            if let v, let id = c.streamId { await v.playback.change(subtitle: id) }
        case .setVolume:
            break // the system volume belongs to the device
        }
        last = nil // report the result straight away
        try? await Task.sleep(for: .milliseconds(300))
        reportIfChanged()
    }

    /// One item plays as its own Play button would; several are a queue starting at `index`.
    private func play(_ c: RemoteCommand) async {
        guard let app, let music, let ids = c.itemIds, !ids.isEmpty else { return }
        let start = min(max(0, c.index ?? 0), ids.count - 1)
        let startMs = c.startMs
        do {
            if ids.count == 1 {
                let d = try await app.item(ids[0])
                switch d.type {
                case .movie, .episode, .video:
                    playVideo(d.id, startMs: startMs)
                case .show, .season:
                    var eps = try await app.leaves(d.id, unwatched: true)
                    if eps.isEmpty { eps = try await app.leaves(d.id) }
                    guard let ep = eps.first else { return }
                    playVideo(ep.id, startMs: nil)
                case .track:
                    video?.close()
                    music.play([d.base], at: Double(startMs ?? 0) / 1000)
                case .album, .artist, .collection:
                    let leaves = try await app.leaves(d.id)
                    if let first = leaves.first, first.isPlayableVideo {
                        playVideo(first.id, startMs: nil)
                    } else {
                        video?.close()
                        music.play(leaves, shuffle: c.shuffle ?? false, source: d.title)
                    }
                }
            } else {
                // Fetched together, kept in order.
                let items = try await withThrowingTaskGroup(of: (Int, Item).self) { group in
                    for (i, id) in ids.enumerated() { group.addTask { @MainActor in (i, try await app.item(id).base) } }
                    var out: [(Int, Item)] = []
                    for try await r in group { out.append(r) }
                    return out.sorted { $0.0 < $1.0 }.map(\.1)
                }
                if items[start].isPlayableVideo {
                    playVideo(items[start].id, startMs: startMs)
                } else {
                    video?.close()
                    music.play(items, start: start, shuffle: c.shuffle ?? false, at: Double(startMs ?? 0) / 1000)
                }
            }
        } catch {
            show("Couldn't play what was sent: \(error.localizedDescription)")
        }
    }

    private func playVideo(_ id: Int64, startMs: Int64?) {
        music?.pause()
        presenter?.play(id, startMs: startMs)
    }

    private func show(_ message: String) {
        toast = message
        toastTask?.cancel()
        toastTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(4))
            guard !Task.isCancelled else { return }
            self?.toast = nil
        }
    }

    // MARK: - State

    func currentState() -> RemotePlayerState {
        if let v = video?.playback, let item = v.item {
            let status = v.player.timeControlStatus
            let state: RemotePlayerState.StatePayload = status == .playing ? .playing : status == .waitingToPlayAtSpecifiedRate ? .buffering : .paused
            let b = item.base
            let sub: String? = b._type == .episode
                ? [b.grandparentTitle, b.parentTitle, b.index.map { "Episode \($0)" }].compactMap { $0 }.joined(separator: " · ")
                : b.year.map(String.init)
            var dur = b.durationMs
            if let d = v.player.currentItem?.duration.seconds, d.isFinite, d > 0 { dur = Int64(d * 1000) }
            return .init(state: state, itemId: item.id, itemType: b._type, title: b.title, subtitle: sub,
                         artItemId: b._type == .episode ? (b.grandparentId ?? item.id) : item.id,
                         positionMs: Int64(max(0, v.position) * 1000), durationMs: dur,
                         audioStreamId: v.session?.audioStreamId, subtitleStreamId: v.session?.subtitleStreamId)
        }
        if let music, let e = music.current {
            let t = e.item
            return .init(state: music.playing ? .playing : .paused, itemId: t.id, itemType: t._type, title: t.title,
                         subtitle: t.artistCredit ?? t.grandparentTitle, artItemId: t.parentId ?? t.id,
                         positionMs: Int64(max(0, music.time) * 1000), durationMs: Int64(music.duration * 1000),
                         queueIndex: music.queue.index, queueLength: music.queue.entries.count, shuffle: music.queue.shuffled)
        }
        return .init(state: .idle, positionMs: 0)
    }

    /// Sends the state when something changed: play/pause, the item, tracks, the queue, a seek
    /// (the position isn't where playing would have taken it), or 10 s of playing.
    private func reportIfChanged() {
        guard let app, app.state == .signedIn, loop != nil else { return }
        let s = currentState()
        let playing = s.state == .playing
        let signature = "\(s.state)|\(s.itemId ?? 0)|\(s.audioStreamId ?? 0)|\(s.subtitleStreamId ?? 0)|\(s.queueIndex ?? 0)|\(s.queueLength ?? 0)|\(s.shuffle ?? false)"
        let now = Date()
        if let last, last.signature == signature {
            let expected = last.positionMs + (last.playing ? Int64(now.timeIntervalSince(last.at) * 1000) : 0)
            let jumped = abs(s.positionMs - expected) > 2500
            let due = playing && now.timeIntervalSince(last.at) >= 10
            guard jumped || due else { return }
        }
        last = (signature, s.positionMs, now, playing)
        Task { await app.reportRemoteState(s) }
    }
}

/// "Controlled from …" over whatever is showing.
struct RemoteToast: ViewModifier {
    func body(content: Content) -> some View {
        content.overlay(alignment: .top) {
            if let t = RemoteReceiver.shared.toast {
                Label(t, systemImage: "appletvremote.gen4.fill")
                    .font(.callout.weight(.semibold))
                    .padding(.horizontal, 16).padding(.vertical, 10)
                    .background(.regularMaterial, in: Capsule())
                    .padding(.top, isTV ? 40 : 8)
                    .transition(.move(edge: .top).combined(with: .opacity))
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel(t)
                    .accessibilityIdentifier("remoteToast")
            }
        }
        .animation(.default, value: RemoteReceiver.shared.toast)
    }
}

extension View {
    func remoteToast() -> some View { modifier(RemoteToast()) }
}
