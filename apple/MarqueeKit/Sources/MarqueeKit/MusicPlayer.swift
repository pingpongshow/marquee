import AVFoundation
import Foundation
import MarqueeAPI
import MediaPlayer
import Observation
#if canImport(UIKit)
import UIKit
#endif

/// App-wide music playback with a queue (MUSIC-13). AVQueuePlayer holds the current track
/// and the one that follows, so tracks change without a gap; each track has its own
/// playback session on the server (the next one is a "preload" session until it plays).
@MainActor @Observable
public final class MusicPlayer {
    public private(set) var queue = PlayQueue()
    public private(set) var playing = false
    public private(set) var time: Double = 0
    public private(set) var duration: Double = 0
    public var current: PlayQueue.Entry? { queue.current }

    private let app: AppSession
    @ObservationIgnored private let player = AVQueuePlayer()
    /// Server session per player item.
    @ObservationIgnored private var sessions: [ObjectIdentifier: (entry: Int, session: String)] = [:]
    @ObservationIgnored private var observers: [NSKeyValueObservation] = []
    @ObservationIgnored private var timeObserver: Any?
    @ObservationIgnored private var lastReport = Date.distantPast
    @ObservationIgnored private var artworkTask: Task<Void, Never>?
    /// The entry whose preload session is being created (avoids preloading it twice).
    @ObservationIgnored private var preloading: Int?

    public init(app: AppSession) {
        self.app = app
        player.actionAtItemEnd = .advance
        observers.append(player.observe(\.currentItem, options: [.old, .new]) { [weak self] _, change in
            let old = change.oldValue.flatMap { $0 }.map(ObjectIdentifier.init)
            Task { @MainActor in self?.currentItemChanged(previous: old) }
        })
        observers.append(player.observe(\.timeControlStatus) { [weak self] p, _ in
            let isPlaying = p.timeControlStatus != .paused
            Task { @MainActor in
                guard let self, self.playing != isPlaying else { return }
                self.playing = isPlaying
                self.report(isPlaying ? "playing" : "paused")
                self.updateNowPlaying()
            }
        })
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] t in
            MainActor.assumeIsolated {
                guard let self else { return }
                self.time = t.seconds.isFinite ? t.seconds : 0
                if let d = self.player.currentItem?.duration.seconds, d.isFinite { self.duration = d }
                if Date().timeIntervalSince(self.lastReport) > 15, self.playing { self.report("playing") }
            }
        }
        setUpRemoteCommands()
    }

    // MARK: - Queue actions

    public func play(_ items: [Item], start: Int = 0, shuffle: Bool = false) {
        queue.load(items, start: start, shuffle: shuffle)
        rebuild()
    }

    public func playNext(_ items: [Item]) {
        let wasEmpty = queue.current == nil
        queue.playNext(items)
        wasEmpty ? rebuild() : refreshFollowing()
    }

    public func addToQueue(_ items: [Item]) {
        let wasEmpty = queue.current == nil
        queue.append(items)
        wasEmpty ? rebuild() : refreshFollowing()
    }

    public func remove(_ id: Int) {
        let wasCurrent = queue.current?.id == id
        queue.remove(id)
        wasCurrent ? rebuild() : refreshFollowing()
    }

    public func move(_ id: Int, to: Int) {
        queue.move(id, to: to)
        refreshFollowing()
    }

    public func jump(_ id: Int) {
        queue.jump(id)
        rebuild()
    }

    public func next() {
        let i = queue.skipIndex
        guard i >= 0 else { return }
        queue.setIndex(i)
        rebuild()
    }

    public func previous() {
        if time > 3 { seek(0); return }
        queue.setIndex(max(0, queue.index - 1))
        rebuild()
    }

    public func toggle() { playing ? player.pause() : resume() }

    public func resume() {
        activateAudioSession()
        player.play()
    }

    public func seek(_ seconds: Double) {
        player.seek(to: CMTime(seconds: seconds, preferredTimescale: 600))
        time = seconds
        updateNowPlaying()
    }

    public func toggleShuffle() {
        queue.toggleShuffle()
        refreshFollowing()
    }

    public func cycleRepeat() {
        queue.cycleRepeat()
        refreshFollowing()
    }

    public func stop() {
        report("paused")
        for item in player.items() { endSession(for: item) }
        player.removeAllItems()
        queue = PlayQueue()
        playing = false
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }

    // MARK: - Player items

    private func makeItem(_ entry: PlayQueue.Entry, preload: Bool) async -> AVPlayerItem? {
        guard let client = app.client else { return nil }
        guard let s = try? await client.startPlayback(body: .json(.init(itemId: entry.item.id, startMs: 0, preload: preload,
                                                                         profile: AppleDeviceProfile.current()))).ok.body.json,
              let url = app.absolute(s.url) else { return nil }
        let item = AVPlayerItem(url: url)
        sessions[ObjectIdentifier(item)] = (entry.id, s.id)
        return item
    }

    private func endSession(for item: AVPlayerItem) {
        guard let s = sessions.removeValue(forKey: ObjectIdentifier(item)) else { return }
        let pos = item === player.currentItem ? Int64(max(0, player.currentTime().seconds) * 1000) : 0
        let client = app.client
        Task {
            _ = try? await client?.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: pos, state: .paused)))
            _ = try? await client?.stopPlayback(path: .init(sessionId: s.session))
        }
    }

    /// Replaces everything in the player with the current entry and what follows it.
    private func rebuild() {
        report("paused")
        for item in player.items() { endSession(for: item) }
        player.removeAllItems()
        time = 0
        duration = Double(queue.current?.item.durationMs ?? 0) / 1000
        guard let entry = queue.current else { playing = false; updateNowPlaying(); return }
        updateNowPlaying()
        Task {
            guard let item = await makeItem(entry, preload: false), queue.current?.id == entry.id else { return }
            player.insert(item, after: nil)
            resume()
            refreshFollowing()
        }
    }

    /// Makes sure the item after the current one is the entry that should follow.
    private func refreshFollowing() {
        let items = player.items()
        let fi = queue.followingIndex
        let want = queue.entries.indices.contains(fi) ? queue.entries[fi] : nil
        // Drop queued items that no longer follow.
        for item in items.dropFirst() where sessions[ObjectIdentifier(item)]?.entry != want?.id || items.firstIndex(of: item) != 1 {
            endSession(for: item)
            player.remove(item)
        }
        guard let want, player.items().count == 1, let cur = player.currentItem, preloading != want.id else { return }
        preloading = want.id
        Task {
            defer { preloading = nil }
            guard let item = await makeItem(want, preload: true) else { return }
            guard player.currentItem === cur, player.items().count == 1 else {
                endSession(for: item)
                return
            }
            player.insert(item, after: cur)
        }
    }

    private func currentItemChanged(previous: ObjectIdentifier?) {
        // The previous track finished (or was skipped): close its session.
        if let previous, let s = sessions.removeValue(forKey: previous) {
            let client = app.client
            let durMs = Int64(duration * 1000)
            Task {
                _ = try? await client?.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: durMs, state: .paused)))
                _ = try? await client?.stopPlayback(path: .init(sessionId: s.session))
            }
        }
        guard let item = player.currentItem, let s = sessions[ObjectIdentifier(item)] else {
            if player.currentItem == nil, queue.current != nil, previous != nil, queue.followingIndex < 0 {
                playing = false // reached the end of the queue
            }
            return
        }
        if let i = queue.entries.firstIndex(where: { $0.id == s.entry }), i != queue.index { queue.setIndex(i) }
        time = 0
        duration = Double(queue.current?.item.durationMs ?? 0) / 1000
        report("playing")
        updateNowPlaying()
        refreshFollowing()
    }

    private func report(_ state: String) {
        guard let item = player.currentItem, let s = sessions[ObjectIdentifier(item)], let client = app.client else { return }
        lastReport = Date()
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        let st: Schemas.PlaybackProgress.StatePayload = state == "playing" ? .playing : .paused
        Task { _ = try? await client.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: pos, state: st))) }
    }

    // MARK: - System integration

    private func activateAudioSession() {
        #if os(iOS) || os(tvOS)
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
        try? AVAudioSession.sharedInstance().setActive(true)
        #endif
    }

    private func setUpRemoteCommands() {
        let c = MPRemoteCommandCenter.shared()
        c.playCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.resume() }; return .success }
        c.pauseCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.player.pause() }; return .success }
        c.togglePlayPauseCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.toggle() }; return .success }
        c.nextTrackCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.next() }; return .success }
        c.previousTrackCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.previous() }; return .success }
        c.changePlaybackPositionCommand.addTarget { [weak self] e in
            guard let e = e as? MPChangePlaybackPositionCommandEvent else { return .commandFailed }
            MainActor.assumeIsolated { self?.seek(e.positionTime) }
            return .success
        }
    }

    private func updateNowPlaying() {
        guard let t = queue.current?.item else {
            MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
            return
        }
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: t.title,
            MPMediaItemPropertyArtist: t.artistCredit ?? t.grandparentTitle ?? "",
            MPMediaItemPropertyAlbumTitle: t.parentTitle ?? "",
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: time,
            MPNowPlayingInfoPropertyPlaybackRate: playing ? 1.0 : 0.0,
            MPNowPlayingInfoPropertyMediaType: MPNowPlayingInfoMediaType.audio.rawValue,
        ]
        if let old = MPNowPlayingInfoCenter.default().nowPlayingInfo,
           old[MPMediaItemPropertyTitle] as? String == t.title, let art = old[MPMediaItemPropertyArtwork] {
            info[MPMediaItemPropertyArtwork] = art
        } else {
            loadArtwork(for: t)
        }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    private func loadArtwork(for t: Item) {
        artworkTask?.cancel()
        guard let url = app.imageURL(t.images?.poster, width: 512) else { return }
        artworkTask = Task {
            guard let (data, _) = try? await URLSession.shared.data(from: url), !Task.isCancelled else { return }
            #if canImport(UIKit)
            guard let image = UIImage(data: data) else { return }
            let art = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
            var info = MPNowPlayingInfoCenter.default().nowPlayingInfo ?? [:]
            guard info[MPMediaItemPropertyTitle] as? String == t.title else { return }
            info[MPMediaItemPropertyArtwork] = art
            MPNowPlayingInfoCenter.default().nowPlayingInfo = info
            #endif
        }
    }
}
