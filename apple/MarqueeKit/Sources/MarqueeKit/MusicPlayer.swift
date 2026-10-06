import AVFoundation
import Foundation
import MarqueeAPI
import MediaPlayer
import Observation
import os
#if canImport(UIKit)
import UIKit
#endif

/// App-wide music playback with a queue (MUSIC-13). AVQueuePlayer holds the current track
/// and the one that follows, so tracks change without a gap; each track has its own
/// playback session on the server (the next one is a "preload" session until it plays).
@MainActor @Observable
public final class MusicPlayer {
    /// What's playing: an album, a playlist or a station. Stations with a radio request
    /// top themselves up as they play (MUSIC-3).
    public struct Source: Sendable {
        public let title: String
        public let radio: RadioRequest?
        public init(title: String, radio: RadioRequest? = nil) {
            self.title = title
            self.radio = radio
        }
    }

    /// Volume levelling (MUSIC-9): auto uses album gain while an album plays in order.
    public enum Levelling: String, CaseIterable, Sendable {
        case auto, track, album, off
        public var label: String {
            switch self {
            case .auto: "Automatic"
            case .track: "Track"
            case .album: "Album"
            case .off: "Off"
            }
        }
    }

    /// DJ modes (MUSIC-6): a DJ weaves a track in every few songs.
    public enum DJ: String, CaseIterable, Sendable {
        case wander, superfan, deepCuts = "deep_cuts", sameEra = "same_era"
        public var label: String {
            switch self {
            case .wander: "Wander"
            case .superfan: "Superfan"
            case .deepCuts: "Deep Cuts"
            case .sameEra: "Same Era"
            }
        }
        public var help: String {
            switch self {
            case .wander: "Similar sound, other artists"
            case .superfan: "More from the same artists"
            case .deepCuts: "The artist's tracks you play least"
            case .sameEra: "Similar sound from the same years"
            }
        }
    }

    public enum Sleep: Equatable, Sendable {
        case at(Date)
        case endOfTrack
    }

    public private(set) var queue = PlayQueue()
    public private(set) var source: Source?
    /// Sleep timer: pause at a time or when the current track ends.
    public var sleep: Sleep? { didSet { scheduleSleep() } }
    /// Show the audio quality in Now Playing and track lists (MUSIC-23); per device, off by
    /// default.
    public var showAudioQuality = UserDefaults.standard.bool(forKey: "marquee.showAudioQuality") {
        didSet { UserDefaults.standard.set(showAudioQuality, forKey: "marquee.showAudioQuality") }
    }
    /// What the server streams for each queue entry when it isn't the original file.
    public private(set) var streamed: [Int: StreamedAudio] = [:]
    /// The current track's stream when it isn't the original (a transcode for the remote
    /// quality limit, say).
    public var currentStreamed: StreamedAudio? { current.flatMap { streamed[$0.id] } }
    public var dj: DJ? = DJ(rawValue: UserDefaults.standard.string(forKey: "marquee.dj") ?? "") {
        didSet {
            UserDefaults.standard.set(dj?.rawValue, forKey: "marquee.dj")
            djCount = 0
        }
    }
    @ObservationIgnored private var djCount = 0
    @ObservationIgnored private var djBusy = false
    /// Volume levelling is always on (Automatic): album gain while an album plays in order,
    /// else track gain. It's no longer a setting.
    public let levelling: Levelling = .auto
    /// Crossfade length in seconds, 0 = off (MUSIC-9). Albums played in order stay gapless.
    public var crossfade: Int = UserDefaults.standard.integer(forKey: "marquee.crossfade") {
        didSet { UserDefaults.standard.set(crossfade, forKey: "marquee.crossfade") }
    }
    /// The equaliser (iOS/iPadOS): biquads in an audio tap on each track's mix.
    public let equalizer = Equalizer()
    /// Playing to an AirPlay device, where the equaliser may not be heard.
    public private(set) var airPlaying = false
    @ObservationIgnored private var fader: AVPlayer?
    @ObservationIgnored private var fadeTask: Task<Void, Never>?
    @ObservationIgnored private var fadingFrom: ObjectIdentifier?
    @ObservationIgnored private var fadedSession: (Loaded, Double)?
    public private(set) var playing = false
    public private(set) var time: Double = 0
    public private(set) var duration: Double = 0
    public var current: PlayQueue.Entry? { queue.current }
    /// A short message when a track couldn't play (shown in Now Playing for a few seconds).
    public private(set) var error: String?
    @ObservationIgnored private var errorTask: Task<Void, Never>?

    private let app: AppSession
    /// Downloaded tracks play from the device, with or without the server (MUSIC-14).
    @ObservationIgnored public var downloads: Downloads?
    @ObservationIgnored private let player = AVQueuePlayer()
    /// Server session (and loudness) per player item.
    private struct Loaded {
        let entry: Int
        let session: String
        let trackGain: Double?
        let albumGain: Double?
        let peak: Double?
        /// HLS streams can't take an audio tap (no equaliser).
        var hls = false
        /// The levelling gain in effect (linear; above 1 through the tap) and the audio track
        /// it's set on, so the crossfade tail plays at the same level.
        var gain = 1.0
        var trackID: CMPersistentTrackID?
    }
    @ObservationIgnored private var sessions: [ObjectIdentifier: Loaded] = [:]
    @ObservationIgnored private var sleepTask: Task<Void, Never>?
    @ObservationIgnored private var refilling = false
    @ObservationIgnored private var observers: [NSKeyValueObservation] = []
    @ObservationIgnored private var timeObserver: Any?
    @ObservationIgnored private var lastReport = Date.distantPast
    @ObservationIgnored private var artworkTask: Task<Void, Never>?
    /// The Now Playing artwork, kept by the item it belongs to (and the item being fetched).
    @ObservationIgnored private var artworkItem: Int64?
    @ObservationIgnored private var artworkLoading: Int64?
    @ObservationIgnored private var artwork: MPMediaItemArtwork?
    /// A failed preload is retried after a growing pause (while the current track plays).
    @ObservationIgnored private var preloadFailures = 0
    @ObservationIgnored private var preloadRetryAt = Date.distantPast
    /// Tracks in a row that wouldn't start, so a queue of unplayable tracks stops rather than spins.
    @ObservationIgnored private var startFailures = 0
    /// Playing when an interruption (a call, Siri, an alarm) began.
    @ObservationIgnored private var interruptedWhilePlaying = false
    /// Paused on purpose (not by an interruption or the end of the queue).
    @ObservationIgnored private var userPaused = false
    /// The remote device played the queue to its end: nothing to carry on with here.
    @ObservationIgnored private var remoteQueueEnded = false
    /// The entry whose preload session is being created (avoids preloading it twice).
    @ObservationIgnored private var preloading: Int?
    /// Bumped by every rebuild, so a slow session start for an older track is dropped.
    @ObservationIgnored private var buildGeneration = 0
    /// Item status observations, so a track that fails to load is noticed.
    @ObservationIgnored private var itemObservers: [ObjectIdentifier: NSKeyValueObservation] = [:]
    /// The entry already retried after a failure (one retry, then on to the next track).
    @ObservationIgnored private var retriedEntry: Int?
    /// Watches a stall of the current item (waiting for data that may never come: a stream
    /// whose session the server ended).
    @ObservationIgnored private var stallTask: Task<Void, Never>?
    @ObservationIgnored private var stallItem: ObjectIdentifier?
    /// UI tests: the first preloaded track's server session is ended at once, as when the
    /// server dropped it, so the player has to load it again.
    @ObservationIgnored private var killNextPreload = ProcessInfo.processInfo.arguments.contains("-marquee-test-dead-preload")

    public init(app: AppSession) {
        self.app = app
        player.actionAtItemEnd = .advance
        observers.append(player.observe(\.currentItem, options: [.old, .new]) { [weak self] _, change in
            let old = change.oldValue.flatMap { $0 }.map(ObjectIdentifier.init)
            Task { @MainActor in self?.currentItemChanged(previous: old) }
        })
        observers.append(player.observe(\.timeControlStatus) { [weak self] p, _ in
            let status = p.timeControlStatus
            Task { @MainActor in self?.statusChanged(status) }
        })
        installTimeObserver()
        NotificationCenter.default.addObserver(forName: AVPlayerItem.failedToPlayToEndTimeNotification, object: nil, queue: .main) { [weak self] n in
            guard let item = n.object as? AVPlayerItem else { return }
            let id = ObjectIdentifier(item)
            MainActor.assumeIsolated { self?.itemFailed(id) }
        }
        // A stream whose session the server ended answers 404 (or 410): load it again rather
        // than wait on it.
        NotificationCenter.default.addObserver(forName: AVPlayerItem.newErrorLogEntryNotification, object: nil, queue: .main) { [weak self] n in
            guard let item = n.object as? AVPlayerItem, let code = item.errorLog()?.events.last?.errorStatusCode, code == 404 || code == 410 else { return }
            let id = ObjectIdentifier(item)
            MainActor.assumeIsolated { self?.itemFailed(id) }
        }
        setUpRemoteCommands()
        equalizer.onChange = { [weak self] in
            guard let self else { return }
            for item in self.player.items() { Task { await self.applyLevel(item) } }
        }
        #if os(iOS)
        NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.updateRoute() }
        }
        updateRoute()
        #endif
        #if os(iOS) || os(tvOS)
        NotificationCenter.default.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { [weak self] n in
            let type = (n.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt).flatMap(AVAudioSession.InterruptionType.init)
            let options = AVAudioSession.InterruptionOptions(rawValue: (n.userInfo?[AVAudioSessionInterruptionOptionKey] as? UInt) ?? 0)
            MainActor.assumeIsolated { self?.interrupted(type, shouldResume: options.contains(.shouldResume)) }
        }
        NotificationCenter.default.addObserver(forName: AVAudioSession.mediaServicesWereResetNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.mediaServicesReset() }
        }
        #endif
        // Signing out, switching profile or forgetting the server ends the music.
        app.onUserChange = { [weak self] in self?.stop() }
    }

    /// The position, from the player twice a second. Installed again whenever the track
    /// changes, so the clock never stays stuck on an observer that stopped firing.
    private func installTimeObserver() {
        if let timeObserver { player.removeTimeObserver(timeObserver) }
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] t in
            MainActor.assumeIsolated {
                guard let self, self.remote == nil else { return }
                self.time = t.seconds.isFinite ? max(0, t.seconds) : 0
                if let d = self.player.currentItem?.duration.seconds, d.isFinite, d > 0 { self.duration = d }
                if Date().timeIntervalSince(self.lastReport) > 15, self.playing { self.report("playing") }
                // Full volume whenever no crossfade is running (one never leaves it silent).
                if self.fadeTask == nil, self.fader == nil, self.player.volume != 1 { self.player.volume = 1 }
                self.maybeCrossfade()
                self.retryPreloadIfDue()
            }
        }
    }

    /// Playing, paused or waiting for data. `playing` is what the person asked for (waiting
    /// counts as playing); Now Playing's clock only runs while audio actually plays.
    private func statusChanged(_ status: AVPlayer.TimeControlStatus) {
        guard remote == nil else { return }
        let isPlaying = status != .paused
        if playing != isPlaying {
            playing = isPlaying
            report(isPlaying ? "playing" : "paused")
        }
        if status == .playing, player.currentItem != nil {
            let t = player.currentTime().seconds
            if t.isFinite { time = max(0, t) }
        }
        watchStall(status)
        updateNowPlaying()
    }

    /// The current track waits for data: after a few seconds, check its address; a stream
    /// that's gone (404/410) is loaded again at once, and one stuck for half a minute too.
    private func watchStall(_ status: AVPlayer.TimeControlStatus) {
        guard status == .waitingToPlayAtSpecifiedRate, player.reasonForWaitingToPlay != .noItemToPlay, let item = player.currentItem else {
            stallTask?.cancel()
            stallTask = nil
            stallItem = nil
            return
        }
        let id = ObjectIdentifier(item)
        guard stallTask == nil || stallItem != id else { return }
        stallTask?.cancel()
        stallItem = id
        let url = (item.asset as? AVURLAsset)?.url
        stallTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(8))
            guard let self, !Task.isCancelled, self.stalled(id) else { return }
            var gone = false
            if let url, !url.isFileURL { gone = await Self.streamGone(url) }
            guard !Task.isCancelled, self.stalled(id) else { return }
            if !gone {
                try? await Task.sleep(for: .seconds(22))
                guard !Task.isCancelled, self.stalled(id) else { return }
            }
            self.stallTask = nil
            self.stallItem = nil
            self.itemFailed(id)
        }
    }

    private func stalled(_ id: ObjectIdentifier) -> Bool {
        remote == nil && player.timeControlStatus == .waitingToPlayAtSpecifiedRate && player.currentItem.map(ObjectIdentifier.init) == id
    }

    /// Whether a stream's address answers 404 or 410 (its session is gone). Only the response
    /// headers are read.
    nonisolated private static func streamGone(_ url: URL) async -> Bool {
        var r = URLRequest(url: url, timeoutInterval: 6)
        r.setValue("bytes=0-0", forHTTPHeaderField: "Range")
        guard let (bytes, response) = try? await URLSession.shared.bytes(for: r) else { return false }
        bytes.task.cancel()
        guard let h = response as? HTTPURLResponse else { return false }
        return h.statusCode == 404 || h.statusCode == 410
    }

    // MARK: - Queue actions

    /// Plays items; `source` names what they are (an album or playlist) for Now Playing.
    /// `at` starts the first track part-way (a queue handed over from another device).
    public func play(_ items: [Item], start: Int = 0, shuffle: Bool = false, source: String? = nil, at seconds: Double = 0) {
        self.source = source.map { Source(title: $0) }
        queue.load(items, start: start, shuffle: shuffle)
        retriedEntry = nil
        startFailures = 0
        rebuild(at: max(0, seconds))
    }

    /// Plays a generated station; with a radio request it keeps topping itself up.
    public func playStation(_ station: Station, radio: RadioRequest? = nil) {
        guard !station.items.isEmpty else { return }
        source = Source(title: station.title, radio: radio)
        queue.load(station.items, start: 0, shuffle: false)
        retriedEntry = nil
        startFailures = 0
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
        let ended = queue.remove(id)
        if ended {
            // The last track was removed while playing: the queue has ended (the track before
            // it doesn't start again by itself).
            rebuild(autoplay: false, load: false)
        } else {
            wasCurrent ? rebuild() : refreshFollowing()
        }
    }

    public func move(_ id: Int, to: Int) {
        queue.move(id, to: to)
        refreshFollowing()
    }

    public func jump(_ id: Int) {
        queue.jump(id)
        retriedEntry = nil
        startFailures = 0
        rebuild()
    }

    public func next() {
        let i = queue.skipIndex
        guard i >= 0 else { return }
        // The next track is already preloaded: move on to it rather than start it again.
        let items = player.items()
        if remote == nil, items.count >= 2, let cur = player.currentItem, items[0] === cur,
           sessions[ObjectIdentifier(items[1])]?.entry == queue.entries[i].id, items[1].status != .failed {
            cancelFade()
            report("paused")
            endSession(for: cur) // reports where it was skipped, and isn't counted as finished
            retriedEntry = nil
            player.advanceToNextItem()
            if !playing { resume() }
            return
        }
        queue.setIndex(i)
        rebuild()
    }

    public func previous() {
        if time > 3 { seek(0); return }
        queue.setIndex(max(0, queue.index - 1))
        rebuild()
    }

    public func toggle() {
        if let remote {
            // The remote device finished the queue: play it again from the current track.
            if remoteQueueEnded { rebuild() } else { remote.toggle() }
            return
        }
        playing ? pause() : resume()
    }

    public func resume() {
        if let remote {
            if remoteQueueEnded { rebuild() } else { remote.resume() }
            return
        }
        // The queue played to the end (or the track failed): start the current entry again.
        if player.currentItem == nil, queue.current != nil { startFailures = 0; rebuild(); return }
        userPaused = false
        activateAudioSession()
        player.play()
    }

    /// Pauses here, or on the Chromecast while casting. Video and live TV call this too.
    public func pause() {
        if let remote { remote.pause(); return }
        userPaused = true
        cancelFade()
        player.pause()
    }

    public func seek(_ seconds: Double) {
        if let remote { remote.seek(seconds); time = seconds; return }
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
        cancelFade()
        buildGeneration += 1
        remote?.pause()
        report("paused")
        for item in player.items() { endSession(for: item) }
        player.removeAllItems()
        queue = PlayQueue()
        source = nil
        sleep = nil
        playing = false
        time = 0
        duration = 0
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }

    /// Shows a short message in Now Playing (a track that wouldn't play, a Chromecast error).
    public func showError(_ message: String) {
        error = message
        errorTask?.cancel()
        errorTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(8))
            guard !Task.isCancelled else { return }
            self?.error = nil
        }
    }

    // MARK: - Player items

    private func makeItem(_ entry: PlayQueue.Entry, preload: Bool) async -> AVPlayerItem? {
        if let local = downloads?.localURL(entry.item.id) {
            let item = AVPlayerItem(url: local)
            streamed[entry.id] = nil
            // Loudness kept when it was downloaded (or fetched since).
            let g = downloads?.gains(entry.item.id)
            sessions[ObjectIdentifier(item)] = Loaded(entry: entry.id, session: "", trackGain: g?.track, albumGain: g?.album, peak: g?.peak)
            watch(item)
            await applyLevel(item)
            return item
        }
        guard let client = app.client else { return nil }
        guard let s = try? await client.startPlayback(body: .json(.init(itemId: entry.item.id, startMs: 0, preload: preload,
                                                                         profile: AppleDeviceProfile.current()))).ok.body.json,
              let url = app.absolute(s.url) else { return nil }
        if preload, killNextPreload {
            killNextPreload = false
            _ = try? await client.stopPlayback(path: .init(sessionId: s.id))
        }
        let item = AVPlayerItem(url: url)
        streamed[entry.id] = StreamedAudio(s.decision)
        sessions[ObjectIdentifier(item)] = Loaded(entry: entry.id, session: s.id, trackGain: s.trackGainDb, albumGain: s.albumGainDb, peak: s.peak,
                                                  hls: url.path().hasSuffix(".m3u8"))
        watch(item)
        // The level is set before the item reaches the player: the first sample is levelled.
        await applyLevel(item)
        return item
    }

    /// Notices when an item fails to load (a dropped connection, a stale address).
    private func watch(_ item: AVPlayerItem) {
        let id = ObjectIdentifier(item)
        itemObservers[id] = item.observe(\.status) { [weak self] item, _ in
            guard item.status == .failed else { return }
            Task { @MainActor in self?.itemFailed(id) }
        }
    }

    /// The current track failed: try once more from where it was (with a fresh session at the
    /// current address), then move on to the next track.
    private func itemFailed(_ id: ObjectIdentifier) {
        guard remote == nil else { return }
        // The track lined up next failed while it waited (its session ended, say): load it
        // again with a fresh session shortly, rather than skip it when its turn comes.
        let items = player.items()
        if items.count >= 2, ObjectIdentifier(items[1]) == id {
            endSession(for: items[1])
            player.remove(items[1])
            preloadFailures += 1
            preloadRetryAt = Date().addingTimeInterval(min(60, 2 * pow(2, Double(min(preloadFailures, 6) - 1))))
            return
        }
        guard let item = player.currentItem, ObjectIdentifier(item) == id, let entry = queue.current else { return }
        stallTask?.cancel()
        stallTask = nil
        stallItem = nil
        let at = time
        if retriedEntry != entry.id {
            retriedEntry = entry.id
            let gen = buildGeneration
            Task {
                await app.reconnect() // the network may have changed under us
                guard gen == buildGeneration, queue.current?.id == entry.id, remote == nil else { return }
                rebuild(at: at)
            }
            return
        }
        showError("Couldn't play \(entry.item.title).")
        if queue.skipIndex >= 0 {
            next()
        } else {
            cancelFade()
            for item in player.items() { endSession(for: item) }
            player.removeAllItems()
            playing = false
            updateNowPlaying()
        }
    }

    private func endSession(for item: AVPlayerItem) {
        itemObservers.removeValue(forKey: ObjectIdentifier(item))
        guard let s = sessions.removeValue(forKey: ObjectIdentifier(item)), !s.session.isEmpty else { return }
        let pos = item === player.currentItem ? Int64(max(0, player.currentTime().seconds) * 1000) : 0
        let client = app.client
        Task {
            _ = try? await client?.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: pos, state: .paused)))
            _ = try? await client?.stopPlayback(path: .init(sessionId: s.session))
        }
    }

    /// Replaces everything in the player with the current entry and what follows it.
    /// `autoplay` false loads it paused; `load` false only clears the player (the queue ended).
    private func rebuild(at start: Double = 0, autoplay: Bool = true, load: Bool = true) {
        cancelFade()
        buildGeneration += 1
        let gen = buildGeneration
        remoteQueueEnded = false
        report("paused")
        for item in player.items() { endSession(for: item) }
        player.removeAllItems()
        time = start
        duration = Double(queue.current?.item.durationMs ?? 0) / 1000
        guard let entry = queue.current else { playing = false; updateNowPlaying(); return }
        guard load else {
            remote?.pause()
            playing = false
            time = 0
            updateNowPlaying()
            return
        }
        if !autoplay { playing = false }
        updateNowPlaying()
        if let remote {
            remote.play(entry.item, at: start)
            return
        }
        Task {
            guard let item = await makeItem(entry, preload: false) else {
                guard gen == buildGeneration, queue.current?.id == entry.id, remote == nil else { return }
                startFailed(entry, autoplay: autoplay)
                return
            }
            // Skipped again (or casting) while the session started: end it rather than leak it.
            guard gen == buildGeneration, queue.current?.id == entry.id, remote == nil else {
                endSession(for: item)
                return
            }
            startFailures = 0
            player.insert(item, after: nil)
            if start > 0 { await player.seek(to: CMTime(seconds: start, preferredTimescale: 600)) }
            guard gen == buildGeneration else { return }
            if autoplay {
                resume()
            } else {
                playing = false
                updateNowPlaying()
            }
            refreshFollowing()
        }
    }

    /// A track wouldn't start (no session, or not downloaded while offline): say so and move
    /// on to the next track that can play, or stop.
    private func startFailed(_ entry: PlayQueue.Entry, autoplay: Bool) {
        showError("Couldn't play \(entry.item.title).")
        startFailures += 1
        if startFailures < min(queue.entries.count, 10), let i = nextPlayable() {
            queue.setIndex(i)
            rebuild(autoplay: autoplay)
        } else {
            startFailures = 0
            playing = false
            updateNowPlaying()
        }
    }

    /// The next entry worth trying after the current one: while the server can't be reached,
    /// the next downloaded track; otherwise the next track (wrapping with repeat on).
    private func nextPlayable() -> Int? {
        let n = queue.entries.count
        guard n > 1, queue.index >= 0 else { return nil }
        let offline = app.client == nil || app.lastError != nil
        let wrap = queue.repeatMode != .off
        for step in 1..<n {
            let i = queue.index + step
            if i >= n && !wrap { break }
            let j = i % n
            if !offline || downloads?.localURL(queue.entries[j].item.id) != nil { return j }
        }
        return nil
    }

    // MARK: - Remote playback (Chromecast, D82)

    /// Set while the queue plays on another device: tracks go there one by one and transport
    /// commands are forwarded; this player keeps the queue. Cleared, playback carries on here.
    public var remote: (any RemotePlayback)? {
        didSet {
            guard (remote == nil) != (oldValue == nil) else { return }
            let at = time
            if remote != nil {
                cancelFade()
                player.pause()
                rebuild(at: at)
            } else if remoteQueueEnded {
                // The remote device played the queue to its end: nothing carries on here.
                remoteQueueEnded = false
                playing = false
                time = 0
                updateNowPlaying()
            } else if queue.current != nil {
                // Carries on here only if it was playing there.
                rebuild(at: at, autoplay: playing)
            }
        }
    }

    /// The remote device's state.
    public func remoteUpdate(playing: Bool, time: Double, duration: Double) {
        guard remote != nil else { return }
        self.playing = playing
        self.time = time
        if duration > 0 { self.duration = duration }
        updateNowPlaying()
    }

    /// The remote device finished a track: on to what follows, as when a track ends here.
    public func remoteFinished() {
        guard remote != nil else { return }
        let i = queue.followingIndex
        guard i >= 0 else {
            playing = false
            remoteQueueEnded = true
            if sleep == .endOfTrack { sleep = nil }
            updateNowPlaying()
            return
        }
        queue.setIndex(i)
        rebuild()
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
            let made = await makeItem(want, preload: true)
            if preloading == want.id { preloading = nil }
            guard let item = made else {
                // Try again later (the current track keeps playing meanwhile): 2, 4, 8… s, up to a minute.
                preloadFailures += 1
                preloadRetryAt = Date().addingTimeInterval(min(60, 2 * pow(2, Double(min(preloadFailures, 6) - 1))))
                return
            }
            // The queue changed while it loaded (play next, shuffle, a move or a removal): this
            // track no longer follows, so drop it and preload the one that does.
            let fi = queue.followingIndex
            guard remote == nil, player.currentItem === cur, player.items().count == 1,
                  queue.entries.indices.contains(fi), queue.entries[fi].id == want.id else {
                endSession(for: item)
                if remote == nil, player.currentItem === cur { refreshFollowing() }
                return
            }
            // (Failures are counted until the track starts, so one that keeps failing backs off.)
            player.insert(item, after: cur)
            // It failed while loading (a dead address): load it again shortly.
            if item.status == .failed { itemFailed(ObjectIdentifier(item)) }
        }
    }

    /// Retries a preload that failed, once its pause is up.
    private func retryPreloadIfDue() {
        guard remote == nil, preloadFailures > 0, preloading == nil, Date() >= preloadRetryAt,
              player.currentItem != nil, player.items().count == 1, queue.followingIndex >= 0 else { return }
        refreshFollowing()
    }

    private func currentItemChanged(previous: ObjectIdentifier?) {
        if let previous { itemObservers.removeValue(forKey: previous) }
        // A track that played to its end still has its session here: skips, rebuilds and
        // stops end theirs before the player lets go of the item.
        let ended = previous.map { sessions[$0] != nil } ?? false
        // Sleep at the end of the track: checked before the crossfade hand-over, and pause()
        // stops the fading tail too.
        if ended, sleep == .endOfTrack, player.currentItem != nil {
            sleep = nil
            pause()
        }
        // Fading out on the crossfade player: its session ends when the fade does.
        if let previous, previous == fadingFrom, let s = sessions.removeValue(forKey: previous) {
            fadedSession = (s, duration)
            afterTrackChange()
            return
        }
        // The previous track finished (or was skipped): close its session.
        if let previous, let s = sessions[previous], s.session.isEmpty {
            // A downloaded track: count the play (if most of it played) for the next sync.
            sessions.removeValue(forKey: previous)
            if let e = queue.entries.first(where: { $0.id == s.entry }), duration > 0, time >= duration * 0.5 {
                downloads?.recordProgress(itemID: e.item.id, positionMs: Int64(time * 1000), watched: true)
            }
        } else if let previous, let s = sessions.removeValue(forKey: previous) {
            let client = app.client
            let durMs = Int64(duration * 1000)
            Task {
                _ = try? await client?.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: durMs, state: .paused)))
                _ = try? await client?.stopPlayback(path: .init(sessionId: s.session))
            }
        }
        guard player.currentItem != nil else {
            guard ended, remote == nil, queue.current != nil else { return }
            let fi = queue.followingIndex
            if fi >= 0 {
                // Nothing was lined up after it (its preload failed or was still loading):
                // start the next track now rather than stop mid-album.
                queue.setIndex(fi)
                if sleep == .endOfTrack {
                    sleep = nil
                    rebuild(autoplay: false)
                } else {
                    rebuild()
                }
            } else {
                // Reached the end of the queue; a sleep timer for the end of the track is done too.
                playing = false
                if sleep == .endOfTrack { sleep = nil }
                updateNowPlaying()
            }
            return
        }
        afterTrackChange()
    }

    private func afterTrackChange() {
        guard let item = player.currentItem, let s = sessions[ObjectIdentifier(item)] else { return }
        // The queue moves to the item's entry first, so a failure below is that track's (it
        // used to restart the previous track at its end).
        if let i = queue.entries.firstIndex(where: { $0.id == s.entry }), i != queue.index { queue.setIndex(i) }
        // A new track gets its own retry.
        if retriedEntry != s.entry { retriedEntry = nil }
        time = max(0, player.currentTime().seconds.isFinite ? player.currentTime().seconds : 0)
        duration = Double(queue.current?.item.durationMs ?? 0) / 1000
        // A fresh observer for each track (see installTimeObserver).
        installTimeObserver()
        // A preloaded track that failed while waiting its turn: a fresh session, from the start.
        if item.status == .failed { itemFailed(ObjectIdentifier(item)); return }
        preloadFailures = 0
        preloadRetryAt = .distantPast
        // The queue changed since it was loaded (now in an album run, or not): level it again.
        if s.trackID != nil, abs(levelGain(s) - s.gain) > 0.001 { Task { await applyLevel(item) } }
        report("playing")
        updateNowPlaying()
        refreshFollowing()
        topUpRadio()
        guestDJ()
    }

    /// After every third of your own tracks, asks the DJ for one to play next.
    private func guestDJ() {
        guard let dj, !djBusy, let cur = queue.current else { return }
        if cur.dj != nil { djCount = 0; return }
        djCount += 1
        let nextIsDJ = queue.upcoming.first?.dj != nil
        guard djCount >= 3, !nextIsDJ, let client = app.client else { return }
        djBusy = true
        let exclude = queue.entries.map(\.item.id)
        Task {
            defer { djBusy = false }
            guard let pick = try? await client.musicDJ(body: .json(.init(trackId: cur.item.id, mode: .init(rawValue: dj.rawValue)!, exclude: exclude))).ok.body.json,
                  queue.current?.id == cur.id else { return }
            queue.playNext([pick], dj: dj.rawValue)
            djCount = 0
            refreshFollowing()
        }
    }

    // MARK: - Stations, sleep and levelling

    /// Continues a radio station when fewer than five tracks are left.
    private func topUpRadio() {
        guard var radio = source?.radio, !refilling, queue.entries.count - queue.index <= 5 else { return }
        refilling = true
        let exclude = queue.entries.map(\.item.id)
        radio.exclude = exclude
        radio.limit = 25
        let title = source?.title
        Task {
            defer { refilling = false }
            guard let st = try? await app.radio(radio), source?.title == title else { return }
            let fresh = st.items.filter { !exclude.contains($0.id) }
            if !fresh.isEmpty { addToQueue(fresh) }
        }
    }

    private func scheduleSleep() {
        sleepTask?.cancel()
        guard case let .at(date) = sleep else { return }
        sleepTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(max(0, date.timeIntervalSinceNow)))
            guard !Task.isCancelled, let self else { return }
            self.pause()
            self.sleep = nil
        }
    }

    // MARK: - Crossfade

    /// A few times a second: when the track is about to end, a second player takes over its
    /// tail and fades it out while the queue moves on and fades in.
    private func maybeCrossfade() {
        let secs = Double(crossfade)
        // Starts a few seconds early: the tail loads, lines up and runs silently in step, then
        // takes over exactly `secs` before the end (loading can take seconds on a slow server).
        guard secs > 0, fadeTask == nil, playing, duration > secs * 3, duration - time <= secs + 4, duration - time > 1,
              player.items().count >= 2, let item = player.currentItem, let url = (item.asset as? AVURLAsset)?.url else { return }
        // Consecutive tracks of one album stay gapless.
        if let s = sessions[ObjectIdentifier(item)], let i = queue.entries.firstIndex(where: { $0.id == s.entry }),
           queue.entries.indices.contains(i + 1), let a = queue.entries[i].item.parentId, queue.entries[i + 1].item.parentId == a { return }
        let tail = AVPlayerItem(url: url)
        // The same level (a boost too), with a tap of its own (a tap can't serve two items).
        if let l = sessions[ObjectIdentifier(item)], let trackID = l.trackID {
            tail.audioMix = mix(trackID: trackID, gain: l.gain, eq: equalizer.enabled && !l.hls)
        }
        let f = AVPlayer(playerItem: tail)
        f.automaticallyWaitsToMinimizeStalling = false
        fader = f
        fadingFrom = ObjectIdentifier(item)
        fadeTask = Task { [weak self] in
            // Load the tail paused, line it up with where the track is, then start it there.
            var waits = 0
            while tail.status == .unknown, waits < 80, !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(25))
                waits += 1
            }
            guard let self, !Task.isCancelled else { return }
            guard tail.status == .readyToPlay, self.player.currentItem === item else { self.endFade(); return }
            // Loading starts seconds ahead, so there's room to line up generously.
            var lead = 1.0
            var lined = false
            for _ in 0..<3 {
                let target = self.player.currentTime().seconds + lead
                guard target.isFinite, target < self.duration - 0.5 else { break }
                _ = await f.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
                // Preroll fills the tail's buffers so it starts the moment it's asked to (only
                // allowed once the player is ready, or AVFoundation throws).
                if f.status == .readyToPlay, f.rate == 0 { _ = await f.preroll(atRate: 1) }
                guard !Task.isCancelled else { return }
                guard self.player.currentItem === item else { break }
                let wait = target - self.player.currentTime().seconds
                if wait >= -0.02 {
                    if wait > 0 { try? await Task.sleep(for: .seconds(wait)) }
                    lined = true
                    break
                }
                lead *= 2 // the seek took longer than allowed for: try again further ahead
            }
            guard !Task.isCancelled else { return }
            // The track ended while the tail loaded (it has moved on by itself), or the tail
            // couldn't be lined up: no crossfade this time.
            guard lined, self.player.currentItem === item else { self.endFade(); return }
            f.volume = 0 // silent until the hand-over
            f.play()
            waits = 0
            while f.timeControlStatus != .playing, waits < 40, !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(25))
                waits += 1
            }
            guard !Task.isCancelled else { return }
            // Hand over only once the tail is audibly playing, and only from the track it copies.
            guard f.timeControlStatus == .playing, self.player.currentItem === item else { self.endFade(); return }
            // In step and silent: hand over when the fade should begin.
            while self.duration - self.player.currentTime().seconds > secs, self.player.currentItem === item, !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(50))
            }
            guard !Task.isCancelled else { return }
            guard self.player.currentItem === item, f.timeControlStatus == .playing else { self.endFade(); return }
            f.volume = 1
            self.player.volume = 0
            self.player.advanceToNextItem()
            let steps = Int(secs * 25)
            for i in 1...steps {
                if Task.isCancelled { break }
                let t = Double(i) / Double(steps)
                self.player.volume = Float(sin(t * .pi / 2)) // equal power keeps the loudness steady
                f.volume = Float(cos(t * .pi / 2))
                try? await Task.sleep(for: .milliseconds(40))
            }
            // Cancelled: cancelFade() has already tidied up (and a new fade may have started).
            if !Task.isCancelled { self.endFade() }
        }
    }

    /// Stops a crossfade in progress: the fading tail goes quiet and the queue plays at full volume.
    private func cancelFade() {
        guard fadeTask != nil || fader != nil else { return }
        fadeTask?.cancel()
        endFade()
    }

    private func endFade() {
        fader?.pause()
        fader = nil
        fadeTask = nil
        fadingFrom = nil
        player.volume = 1
        guard let (s, dur) = fadedSession else { return }
        fadedSession = nil
        if s.session.isEmpty {
            if let e = queue.entries.first(where: { $0.id == s.entry }) {
                downloads?.recordProgress(itemID: e.item.id, positionMs: Int64(dur * 1000), watched: true)
            }
            return
        }
        let client = app.client
        Task {
            _ = try? await client?.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: Int64(dur * 1000), state: .paused)))
            _ = try? await client?.stopPlayback(path: .init(sessionId: s.session))
        }
    }

    /// The levelling gain for an item (linear): automatic levelling uses album gain while an
    /// album plays in order, else track gain; quiet tracks are raised (through the tap) where
    /// the stream can take one.
    private func levelGain(_ l: Loaded) -> Double {
        let useAlbum: Bool
        switch levelling {
        case .off: return 1
        case .album: useAlbum = true
        case .track: useAlbum = false
        case .auto:
            let i = queue.entries.firstIndex { $0.id == l.entry }
            useAlbum = i.map { Loudness.useAlbumGain(albums: queue.entries.map(\.item.parentId), index: $0) } ?? false
        }
        return Loudness.gain(trackDb: l.trackGain, albumDb: l.albumGain, peak: l.peak, useAlbum: useAlbum, allowBoost: !l.hls)
    }

    /// Sets an item's level from its loudness, with the equaliser's tap when it's on. Awaited
    /// before the item is handed to the player, so playback starts at the right level (it
    /// used to start loud and drop once the mix arrived). A track whose audio takes too long
    /// to describe is levelled as soon as it can be.
    private func applyLevel(_ item: AVPlayerItem) async {
        let id = ObjectIdentifier(item)
        guard let l = sessions[id] else { return }
        if let trackID = l.trackID {
            setMix(item, trackID: trackID)
            return
        }
        let asset = item.asset
        let load = Task { try? await asset.loadTracks(withMediaType: .audio).first?.trackID }
        if let trackID = await Self.firstValue(of: load, within: 6) {
            guard sessions[id] != nil else { return }
            sessions[id]?.trackID = trackID
            setMix(item, trackID: trackID)
            return
        }
        // Slow to load: level it when it's known (it may start at the file's own level).
        Task {
            guard let trackID = await load.value, sessions[id] != nil else { return }
            sessions[id]?.trackID = trackID
            setMix(item, trackID: trackID)
        }
    }

    /// A task's value, or nil when it takes longer than `seconds` (the task carries on).
    nonisolated private static func firstValue<T: Sendable>(of task: Task<T?, Never>, within seconds: Double) async -> T? {
        let once = OSAllocatedUnfairLock(initialState: false)
        return await withCheckedContinuation { (c: CheckedContinuation<T?, Never>) in
            let finish: @Sendable (T?) -> Void = { v in
                if once.withLock({ done in defer { done = true }; return !done }) { c.resume(returning: v) }
            }
            Task { finish(await task.value) }
            Task {
                try? await Task.sleep(for: .seconds(seconds))
                finish(nil)
            }
        }
    }

    private func setMix(_ item: AVPlayerItem, trackID: CMPersistentTrackID) {
        let id = ObjectIdentifier(item)
        guard let l = sessions[id] else { return }
        let gain = levelGain(l)
        sessions[id]?.gain = gain
        item.audioMix = mix(trackID: trackID, gain: gain, eq: equalizer.enabled && !l.hls)
    }

    /// An audio mix: the levelling volume (cuts), and a tap for the equaliser and for boosts
    /// (a mix's volume can't go above 1).
    private func mix(trackID: CMPersistentTrackID, gain: Double, eq: Bool) -> AVAudioMix {
        let p = AVMutableAudioMixInputParameters()
        p.trackID = trackID
        p.setVolume(Float(min(gain, 1)), at: .zero)
        if eq || gain > 1 { p.audioTapProcessor = equalizer.makeTap(gain: max(gain, 1)) }
        let mix = AVMutableAudioMix()
        mix.inputParameters = [p]
        return mix
    }


    /// Why the equaliser can't be heard right now, if it can't.
    public var equalizerNote: String? {
        if remote != nil { return "The equaliser doesn't apply while casting: the speaker plays the music itself." }
        if let item = player.currentItem, sessions[ObjectIdentifier(item)]?.hls == true {
            return "This track streams in a format the equaliser can't adjust."
        }
        if airPlaying { return "The equaliser may not apply while playing over AirPlay." }
        return nil
    }

    #if os(iOS)
    private func updateRoute() {
        airPlaying = AVAudioSession.sharedInstance().currentRoute.outputs.contains { $0.portType == .airPlay }
    }
    #endif

    #if os(iOS) || os(tvOS)
    /// A phone call, Siri or an alarm took the audio: carry on afterwards if the music was
    /// playing and the system says to.
    private func interrupted(_ type: AVAudioSession.InterruptionType?, shouldResume: Bool) {
        guard remote == nil else { return }
        switch type {
        case .began:
            interruptedWhilePlaying = playing || (!userPaused && player.currentItem != nil && player.timeControlStatus != .paused)
            cancelFade()
        case .ended:
            let resume = interruptedWhilePlaying && shouldResume
            interruptedWhilePlaying = false
            guard resume, queue.current != nil else { return }
            activateAudioSession()
            if player.currentItem == nil { rebuild(at: time) } else { player.play() }
        default:
            break
        }
    }

    /// The system's media services restarted: every player item is gone, so load the
    /// current track again where it was.
    private func mediaServicesReset() {
        guard remote == nil, queue.current != nil else { return }
        rebuild(at: time, autoplay: playing)
    }
    #endif

    private func report(_ state: String) {
        guard let item = player.currentItem, let s = sessions[ObjectIdentifier(item)], !s.session.isEmpty, let client = app.client else { return }
        lastReport = Date()
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        let st: Schemas.PlaybackProgress.StatePayload = state == "playing" ? .playing : .paused
        Task { _ = try? await client.reportPlayback(path: .init(sessionId: s.session), body: .json(.init(positionMs: pos, state: st))) }
    }

    // MARK: - System integration

    private func activateAudioSession() {
        #if os(iOS)
        // Long-form audio: AirPlay 2 speakers (HomePod, Sonos and other AirPlay 2 speakers,
        // including several at once) are offered as routes for music, as in Apple Music.
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .default, policy: .longFormAudio)
        try? AVAudioSession.sharedInstance().setActive(true)
        #elseif os(tvOS)
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
        try? AVAudioSession.sharedInstance().setActive(true)
        #endif
    }

    private func setUpRemoteCommands() {
        let c = MPRemoteCommandCenter.shared()
        c.playCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.resume() }; return .success }
        c.pauseCommand.addTarget { [weak self] _ in MainActor.assumeIsolated { self?.pause() }; return .success }
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
        // The lock screen runs its clock from the rate: 1 only while audio actually plays here,
        // so a track waiting for data doesn't look like it's playing silently.
        var elapsed = time
        var rate = playing ? 1.0 : 0.0
        if remote == nil, player.currentItem != nil {
            let t = player.currentTime().seconds
            if t.isFinite { elapsed = max(0, t) }
            rate = player.timeControlStatus == .playing ? 1 : 0
        }
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: t.title,
            MPMediaItemPropertyArtist: t.artistCredit ?? t.grandparentTitle ?? "",
            MPMediaItemPropertyAlbumTitle: t.parentTitle ?? "",
            MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: elapsed,
            MPNowPlayingInfoPropertyPlaybackRate: rate,
            MPNowPlayingInfoPropertyMediaType: MPNowPlayingInfoMediaType.audio.rawValue,
        ]
        // Artwork belongs to an item id; it's fetched once per track, not restarted on every update.
        if artworkItem == t.id, let artwork {
            info[MPMediaItemPropertyArtwork] = artwork
        } else if artworkLoading != t.id {
            loadArtwork(for: t)
        }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    private func loadArtwork(for t: Item) {
        artworkTask?.cancel()
        artworkLoading = t.id
        artworkItem = nil
        artwork = nil
        guard let url = app.imageURL(t.images?.poster, pixels: 600) else { return }
        let id = t.id
        artworkTask = Task { [weak self] in
            // A failed fetch isn't retried for this track (updates come every second while casting).
            guard let (data, _) = try? await URLSession.shared.data(from: url), !Task.isCancelled else { return }
            #if canImport(UIKit)
            // Decoded off the main thread.
            let image = await Task.detached(priority: .utility) { () -> UIImage? in
                guard let image = UIImage(data: data) else { return nil }
                return image.preparingForDisplay() ?? image
            }.value
            guard let self, let image, !Task.isCancelled, self.artworkLoading == id else { return }
            let art = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
            self.artwork = art
            self.artworkItem = id
            guard self.queue.current?.item.id == id else { return }
            var info = MPNowPlayingInfoCenter.default().nowPlayingInfo ?? [:]
            info[MPMediaItemPropertyArtwork] = art
            MPNowPlayingInfoCenter.default().nowPlayingInfo = info
            #endif
        }
    }
}

/// Another device playing the queue (Chromecast, D82), driven by MusicPlayer.
@MainActor
public protocol RemotePlayback: AnyObject {
    func play(_ item: Item, at seconds: Double)
    func toggle()
    func resume()
    func pause()
    func seek(_ seconds: Double)
}
