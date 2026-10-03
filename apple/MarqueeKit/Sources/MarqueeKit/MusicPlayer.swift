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
    public var levelling: Levelling = Levelling(rawValue: UserDefaults.standard.string(forKey: "marquee.levelling") ?? "") ?? .auto {
        didSet {
            UserDefaults.standard.set(levelling.rawValue, forKey: "marquee.levelling")
            for item in player.items() { applyLevel(item) }
        }
    }
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
    }
    @ObservationIgnored private var sessions: [ObjectIdentifier: Loaded] = [:]
    @ObservationIgnored private var sleepTask: Task<Void, Never>?
    @ObservationIgnored private var refilling = false
    @ObservationIgnored private var observers: [NSKeyValueObservation] = []
    @ObservationIgnored private var timeObserver: Any?
    @ObservationIgnored private var lastReport = Date.distantPast
    @ObservationIgnored private var artworkTask: Task<Void, Never>?
    /// The entry whose preload session is being created (avoids preloading it twice).
    @ObservationIgnored private var preloading: Int?
    /// Bumped by every rebuild, so a slow session start for an older track is dropped.
    @ObservationIgnored private var buildGeneration = 0
    /// Item status observations, so a track that fails to load is noticed.
    @ObservationIgnored private var itemObservers: [ObjectIdentifier: NSKeyValueObservation] = [:]
    /// The entry already retried after a failure (one retry, then on to the next track).
    @ObservationIgnored private var retriedEntry: Int?

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
                guard let self, self.remote == nil, self.playing != isPlaying else { return }
                self.playing = isPlaying
                self.report(isPlaying ? "playing" : "paused")
                self.updateNowPlaying()
            }
        })
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] t in
            MainActor.assumeIsolated {
                guard let self, self.remote == nil else { return }
                self.time = t.seconds.isFinite ? t.seconds : 0
                if let d = self.player.currentItem?.duration.seconds, d.isFinite { self.duration = d }
                if Date().timeIntervalSince(self.lastReport) > 15, self.playing { self.report("playing") }
                self.maybeCrossfade()
            }
        }
        NotificationCenter.default.addObserver(forName: AVPlayerItem.failedToPlayToEndTimeNotification, object: nil, queue: .main) { [weak self] n in
            guard let item = n.object as? AVPlayerItem else { return }
            let id = ObjectIdentifier(item)
            MainActor.assumeIsolated { self?.itemFailed(id) }
        }
        setUpRemoteCommands()
        equalizer.onChange = { [weak self] in
            guard let self else { return }
            for item in self.player.items() { self.applyLevel(item) }
        }
        #if os(iOS)
        NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.updateRoute() }
        }
        updateRoute()
        #endif
        // Signing out, switching profile or forgetting the server ends the music.
        app.onUserChange = { [weak self] in self?.stop() }
    }

    // MARK: - Queue actions

    /// Plays items; `source` names what they are (an album or playlist) for Now Playing.
    /// `at` starts the first track part-way (a queue handed over from another device).
    public func play(_ items: [Item], start: Int = 0, shuffle: Bool = false, source: String? = nil, at seconds: Double = 0) {
        self.source = source.map { Source(title: $0) }
        queue.load(items, start: start, shuffle: shuffle)
        retriedEntry = nil
        rebuild(at: max(0, seconds))
    }

    /// Plays a generated station; with a radio request it keeps topping itself up.
    public func playStation(_ station: Station, radio: RadioRequest? = nil) {
        guard !station.items.isEmpty else { return }
        source = Source(title: station.title, radio: radio)
        queue.load(station.items, start: 0, shuffle: false)
        retriedEntry = nil
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
        retriedEntry = nil
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

    public func toggle() {
        if let remote { remote.toggle(); return }
        playing ? pause() : resume()
    }

    public func resume() {
        if let remote { remote.resume(); return }
        // The queue played to the end (or the track failed): start the current entry again.
        if player.currentItem == nil, queue.current != nil { rebuild(); return }
        activateAudioSession()
        player.play()
    }

    /// Pauses here, or on the Chromecast while casting. Video and live TV call this too.
    public func pause() {
        if let remote { remote.pause(); return }
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
            sessions[ObjectIdentifier(item)] = Loaded(entry: entry.id, session: "", trackGain: nil, albumGain: nil, peak: nil)
            applyLevel(item) // no loudness data, but the equaliser's tap
            watch(item)
            return item
        }
        guard let client = app.client else { return nil }
        guard let s = try? await client.startPlayback(body: .json(.init(itemId: entry.item.id, startMs: 0, preload: preload,
                                                                         profile: AppleDeviceProfile.current()))).ok.body.json,
              let url = app.absolute(s.url) else { return nil }
        let item = AVPlayerItem(url: url)
        streamed[entry.id] = StreamedAudio(s.decision)
        sessions[ObjectIdentifier(item)] = Loaded(entry: entry.id, session: s.id, trackGain: s.trackGainDb, albumGain: s.albumGainDb, peak: s.peak,
                                                  hls: url.path().hasSuffix(".m3u8"))
        applyLevel(item)
        watch(item)
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
        guard remote == nil, let item = player.currentItem, ObjectIdentifier(item) == id, let entry = queue.current else { return }
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
    private func rebuild(at start: Double = 0) {
        cancelFade()
        buildGeneration += 1
        let gen = buildGeneration
        report("paused")
        for item in player.items() { endSession(for: item) }
        player.removeAllItems()
        time = start
        duration = Double(queue.current?.item.durationMs ?? 0) / 1000
        guard let entry = queue.current else { playing = false; updateNowPlaying(); return }
        updateNowPlaying()
        if let remote {
            remote.play(entry.item, at: start)
            return
        }
        Task {
            guard let item = await makeItem(entry, preload: false) else { return }
            // Skipped again (or casting) while the session started: end it rather than leak it.
            guard gen == buildGeneration, queue.current?.id == entry.id, remote == nil else {
                endSession(for: item)
                return
            }
            player.insert(item, after: nil)
            if start > 0 { await player.seek(to: CMTime(seconds: start, preferredTimescale: 600)) }
            resume()
            refreshFollowing()
        }
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
            } else if queue.current != nil {
                rebuild(at: at)
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
        guard i >= 0 else { playing = false; return }
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
        if let previous { itemObservers.removeValue(forKey: previous) }
        // Sleep at the end of the track: checked before the crossfade hand-over, and pause()
        // stops the fading tail too.
        if previous != nil, sleep == .endOfTrack, player.currentItem != nil {
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
            if queue.current != nil, previous != nil, queue.followingIndex < 0 {
                playing = false // reached the end of the queue
            }
            return
        }
        afterTrackChange()
    }

    private func afterTrackChange() {
        guard let item = player.currentItem, let s = sessions[ObjectIdentifier(item)] else { return }
        // A preloaded track that failed while waiting its turn.
        if item.status == .failed { itemFailed(ObjectIdentifier(item)); return }
        if let i = queue.entries.firstIndex(where: { $0.id == s.entry }), i != queue.index { queue.setIndex(i) }
        time = 0
        duration = Double(queue.current?.item.durationMs ?? 0) / 1000
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
        guard secs > 0, fadeTask == nil, playing, duration > secs * 3, duration - time <= secs,
              player.items().count >= 2, let item = player.currentItem, let url = (item.asset as? AVURLAsset)?.url else { return }
        // Consecutive tracks of one album stay gapless.
        if let s = sessions[ObjectIdentifier(item)], let i = queue.entries.firstIndex(where: { $0.id == s.entry }),
           queue.entries.indices.contains(i + 1), let a = queue.entries[i].item.parentId, queue.entries[i + 1].item.parentId == a { return }
        let tail = AVPlayerItem(url: url)
        // The same level, with an equaliser tap of its own (a tap can't serve two items).
        if let p = item.audioMix?.inputParameters.first {
            var level: Float = 1, end: Float = 1
            var range = CMTimeRange()
            p.getVolumeRamp(for: .zero, startVolume: &level, endVolume: &end, timeRange: &range)
            tail.audioMix = mix(trackID: p.trackID, gain: level, tap: p.audioTapProcessor != nil)
        }
        let f = AVPlayer(playerItem: tail)
        f.seek(to: CMTime(seconds: time + 0.2, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        f.play()
        fader = f
        fadingFrom = ObjectIdentifier(item)
        fadeTask = Task { [weak self] in
            // Hand over only once the tail is audibly playing.
            for _ in 0..<60 where f.timeControlStatus != .playing { try? await Task.sleep(for: .milliseconds(25)) }
            guard let self, !Task.isCancelled else { return }
            guard f.timeControlStatus == .playing else { self.endFade(); return }
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

    /// Sets an item's volume from its loudness. AVFoundation can only turn audio down, so
    /// quiet tracks play at full volume rather than being boosted.
    private func applyLevel(_ item: AVPlayerItem) {
        guard let l = sessions[ObjectIdentifier(item)] else { return }
        var gain: Float = 1
        if levelling != .off {
            let i = queue.entries.firstIndex { $0.id == l.entry }
            let albumRun = i.map { i in
                let album = queue.entries[i].item.parentId
                return album != nil && [i - 1, i + 1].contains { queue.entries.indices.contains($0) && queue.entries[$0].item.parentId == album }
            } ?? false
            let db = levelling == .album || (levelling == .auto && albumRun) ? (l.albumGain ?? l.trackGain) : l.trackGain
            if let db {
                var g = pow(10, db / 20)
                if let peak = l.peak, peak > 0 { g = min(g, 1 / peak) }
                gain = Float(min(g, 1))
            }
        }
        let tap = equalizer.enabled && !l.hls
        let asset = item.asset
        Task {
            guard let track = try? await asset.loadTracks(withMediaType: .audio).first else { return }
            item.audioMix = mix(trackID: track.trackID, gain: gain, tap: tap)
        }
    }

    /// An audio mix: the levelling volume, and the equaliser's tap when it's on.
    private func mix(trackID: CMPersistentTrackID, gain: Float, tap: Bool) -> AVAudioMix {
        let p = AVMutableAudioMixInputParameters()
        p.trackID = trackID
        p.setVolume(gain, at: .zero)
        if tap { p.audioTapProcessor = equalizer.makeTap() }
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

/// Another device playing the queue (Chromecast, D82), driven by MusicPlayer.
@MainActor
public protocol RemotePlayback: AnyObject {
    func play(_ item: Item, at seconds: Double)
    func toggle()
    func resume()
    func pause()
    func seek(_ seconds: Double)
}
