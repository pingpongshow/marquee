import AVFoundation
#if os(tvOS)
import AVKit // AVPlayerItem.externalMetadata
#endif
import Foundation
import MarqueeAPI
import Observation

/// Quality choices (§3a): 0 = original (home) / automatic (away).
public enum QualityPreference {
    public static let options: [(kbps: Int, label: String)] = [
        (0, "Automatic / Original"), (20000, "1080p 20 Mbps"), (12000, "1080p 12 Mbps"), (8000, "1080p 8 Mbps"),
        (4000, "720p 4 Mbps"), (2000, "480p 2 Mbps"), (1000, "360p 1 Mbps"),
    ]
    public static var local: Int {
        get { UserDefaults.standard.integer(forKey: "marquee.quality.local") }
        set { UserDefaults.standard.set(newValue, forKey: "marquee.quality.local") }
    }
    public static var remote: Int {
        get { UserDefaults.standard.integer(forKey: "marquee.quality.remote") }
        set { UserDefaults.standard.set(newValue, forKey: "marquee.quality.remote") }
    }
}

/// Plays one video: starts the server session, drives AVPlayer, reports progress, and
/// knows about markers and what's next.
@MainActor @Observable
public final class VideoPlayback {
    public let player = AVPlayer()
    public private(set) var session: PlaybackSession?
    public private(set) var item: ItemDetail?
    public private(set) var nextUp: Item?
    public private(set) var activeMarker: Marker?
    public private(set) var errorMessage: String?
    public private(set) var finished = false
    public private(set) var position: Double = 0
    /// True once less than `nearEndWindow` seconds of the current video remain (its duration
    /// known). It changes only when it crosses that line, so views can watch it instead of
    /// `position` (which changes every second).
    public private(set) var nearEnd = false
    /// How close to the end `nearEnd` turns on (the Next Episode offer).
    public static let nearEndWindow: Double = 60
    /// Playback speed (PLAY-19); each video starts at 1×.
    public var speed: Float = 1 {
        didSet {
            player.defaultRate = speed
            if player.rate != 0 { player.rate = speed }
        }
    }
    public static let speeds: [Float] = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
    /// Subtitle and audio timing in ms, positive = later (PLAY-17), as the server applied them.
    public private(set) var subtitleOffsetMs = 0
    public private(set) var audioOffsetMs = 0
    public static let offsetLimit = 30000

    private let app: AppSession
    private let playlistID: Int64?
    @ObservationIgnored private var timeObserver: Any?
    @ObservationIgnored private var observations: [NSKeyValueObservation] = []
    @ObservationIgnored private var endObserver: NSObjectProtocol?
    /// The current item's status (one at a time: a restart replaces it).
    @ObservationIgnored private var itemStatus: NSKeyValueObservation?
    /// Called when the stream restarts in place (an audio change, a fallback, back from
    /// casting), so watch together doesn't take the restart's pause for the viewer's.
    @ObservationIgnored public var onRestart: (() -> Void)?
    @ObservationIgnored private var lastReport = Date.distantPast
    @ObservationIgnored private var fallback = 0
    /// The item `fallback` applies to (a failing trailer mustn't downgrade the feature).
    @ObservationIgnored private var fallbackItemID: Int64?
    /// The version being played (nil = the server's best).
    @ObservationIgnored private var fileID: Int64?
    @ObservationIgnored private var selection: (audio: Int64?, subtitle: Int64?) = (nil, nil)
    /// Offsets chosen while watching; sent on every restart of this video (nil = the
    /// server's remembered ones).
    @ObservationIgnored private var chosenOffsets: (subtitle: Int, audio: Int)?
    @ObservationIgnored private var offsetRestart: Task<Void, Never>?
    /// Set while playing a downloaded file (no server session; progress syncs later).
    @ObservationIgnored private var offline: (itemID: Int64, downloads: Downloads)?
    @ObservationIgnored private var offlineCounted = false
    public private(set) var offlineTitle: String?
    @ObservationIgnored private static var measured: (kbps: Int, at: Date)?

    public init(app: AppSession, playlistID: Int64? = nil) {
        self.app = app
        self.playlistID = playlistID
        timeObserver = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 1, preferredTimescale: 600), queue: .main) { [weak self] t in
            MainActor.assumeIsolated { self?.tick(t.seconds) }
        }
        observations.append(player.observe(\.timeControlStatus) { [weak self] p, _ in
            let paused = p.timeControlStatus == .paused
            Task { @MainActor in self?.report(paused ? .paused : .playing) }
        })
    }

    /// Profile for a fallback level: 0 = everything this device plays, 1 = no direct play,
    /// 2 = H.264 only (the last resort when the device rejects what it said it plays).
    private func profile() -> Schemas.DeviceProfile {
        var p = AppleDeviceProfile.current()
        if fallback >= 1 { p.containers = [] }
        if fallback >= 2 {
            p.videoCodecs = ["h264"]
            p.hlsVideoCodecs = ["h264"]
            p.tenBit = false
            p.hdr = []
        }
        return p
    }

    /// Download speed for automatic remote quality, cached for 10 minutes.
    private func measureKbps() async -> Int? {
        if let m = Self.measured, Date().timeIntervalSince(m.at) < 600 { return m.kbps }
        guard let base = app.baseURL else { return nil }
        var req = URLRequest(url: base.appending(path: "api/v1/playback/bandwidth-test").appending(queryItems: [.init(name: "kb", value: "2048")]))
        req.timeoutInterval = 15
        if let t = ServerStore.currentServerID.flatMap(ServerStore.token(for:)) { req.setValue("Bearer \(t)", forHTTPHeaderField: "Authorization") }
        let start = Date()
        guard let (data, _) = try? await URLSession.shared.data(for: req), data.count > 100_000 else { return nil }
        let kbps = Int(Double(data.count * 8) / 1000 / max(Date().timeIntervalSince(start), 0.05))
        Self.measured = (kbps, Date())
        return kbps
    }

    /// Starts playback of an item. `startMs` nil resumes where the user left off.
    public func start(itemID: Int64, startMs: Int64? = nil, audio: Int64? = nil, subtitle: Int64? = nil, fileID: Int64? = nil) async {
        errorMessage = nil
        finished = false
        setNearEnd(false)
        guard let client = app.client else { return }
        if fallbackItemID != itemID {
            fallback = 0
            fallbackItemID = itemID
        }
        if player.currentItem != nil || session != nil { onRestart?() }
        if item?.id != itemID {
            item = try? await app.item(itemID)
            self.fileID = fileID
            chosenOffsets = nil
            if speed != 1 { speed = 1 } // each video starts at normal speed
        } else if fileID != nil {
            self.fileID = fileID
        }
        selection = (audio, subtitle)
        let quality = app.isRemote ? QualityPreference.remote : QualityPreference.local
        let measured = app.isRemote && quality == 0 ? await measureKbps() : nil
        do {
            let s = try await client.startPlayback(body: .json(.init(
                itemId: itemID, fileId: self.fileID, audioStreamId: audio, subtitleStreamId: subtitle, startMs: startMs,
                maxBitrateKbps: quality == 0 ? nil : quality, measuredKbps: measured,
                subtitleOffsetMs: chosenOffsets?.subtitle, audioOffsetMs: chosenOffsets?.audio, profile: profile()))).ok.body.json
            await stopSession()
            session = s
            subtitleOffsetMs = s.subtitleOffsetMs ?? chosenOffsets?.subtitle ?? 0
            audioOffsetMs = s.audioOffsetMs ?? chosenOffsets?.audio ?? 0
            guard let url = app.absolute(s.url) else { throw MarqueeError("Bad stream address") }
            let playerItem = AVPlayerItem(url: url)
            playerItem.preferredForwardBufferDuration = app.isRemote ? 30 : 10
            playerItem.textStyleRules = app.subtitleStyle.textStyleRules // PLAY-20
            describe(playerItem)
            observe(playerItem)
            player.replaceCurrentItem(with: playerItem)
            if s.startMs > 0 { await player.seek(to: CMTime(seconds: Double(s.startMs) / 1000, preferredTimescale: 600)) }
            player.play()
            loadNext(itemID)
        } catch {
            errorMessage = (error as? MarqueeError)?.message ?? "Couldn't start playback: \(error.localizedDescription)"
        }
    }

    /// Restarts at the current position with different tracks (audio changes and image
    /// subtitles need a new session; text subtitles switch in the player's own menu).
    public func change(audio: Int64? = nil, subtitle: Int64? = nil) async {
        guard let id = session?.itemId else { return }
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        await start(itemID: id, startMs: pos, audio: audio ?? selection.audio, subtitle: subtitle ?? selection.subtitle)
    }

    /// Changes the subtitle or audio timing. The stream restarts at the same position with the
    /// new offsets (the server remembers them for this file); quick repeated steps restart once.
    public func setOffsets(subtitle: Int? = nil, audio: Int? = nil) {
        guard offline == nil, let id = session?.itemId ?? item?.id else { return }
        let limit = Self.offsetLimit
        let sub = min(limit, max(-limit, subtitle ?? subtitleOffsetMs))
        let aud = min(limit, max(-limit, audio ?? audioOffsetMs))
        chosenOffsets = (sub, aud)
        subtitleOffsetMs = sub
        audioOffsetMs = aud
        offsetRestart?.cancel()
        offsetRestart = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(700))
            guard let self, !Task.isCancelled else { return }
            let pos = Int64(max(0, self.player.currentTime().seconds) * 1000)
            await self.start(itemID: id, startMs: pos, audio: self.selection.audio, subtitle: self.selection.subtitle)
        }
    }

    /// Plays a downloaded file. Works without the server; progress is kept on the device and
    /// sent when the server is reachable.
    public func startLocal(itemID: Int64, file: URL, downloads: Downloads) async {
        errorMessage = nil
        finished = false
        setNearEnd(false)
        offline = (itemID, downloads)
        offlineTitle = downloads.entries[itemID]?.item.title
        if app.client != nil { item = try? await app.item(itemID) }
        let playerItem = AVPlayerItem(url: file)
        playerItem.textStyleRules = app.subtitleStyle.textStyleRules
        describe(playerItem)
        observe(playerItem)
        player.replaceCurrentItem(with: playerItem)
        let resume = downloads.resumePosition(itemID)
        if resume > 0 { await player.seek(to: CMTime(seconds: Double(resume) / 1000, preferredTimescale: 600)) }
        player.play()
    }

    /// Apple TV: title, description and artwork for the player's info panel and the system's
    /// Now Playing; without them the system asks for artwork and gets none.
    private func describe(_ playerItem: AVPlayerItem) {
        #if os(tvOS)
        guard let d = item else { return }
        func meta(_ id: AVMetadataIdentifier, _ value: any NSCopying & NSObjectProtocol, type: String? = nil) -> AVMetadataItem {
            let m = AVMutableMetadataItem()
            m.identifier = id
            m.value = value
            m.extendedLanguageTag = "und"
            if let type { m.dataType = type }
            return m
        }
        var items = [meta(.commonIdentifierTitle, d.title as NSString)]
        let subtitle = d.type == .episode ? d.base.grandparentTitle : d.base.year.map(String.init)
        if let subtitle { items.append(meta(.iTunesMetadataTrackSubTitle, subtitle as NSString)) }
        if let summary = d.info.summary, !summary.isEmpty { items.append(meta(.commonIdentifierDescription, summary as NSString)) }
        playerItem.externalMetadata = items
        let art = d.type == .episode ? (d.base.images?.thumb ?? d.base.images?.poster) : d.base.images?.poster
        guard let url = app.imageURL(art, pixels: 600) else { return }
        Task { [weak playerItem] in
            guard let (data, _) = try? await URLSession.shared.data(from: url), let playerItem else { return }
            playerItem.externalMetadata = items + [meta(.commonIdentifierArtwork, data as NSData, type: kCMMetadataBaseDataType_JPEG as String)]
        }
        #endif
    }

    public func stop() async {
        offsetRestart?.cancel()
        // The final position reaches the server before the session ends.
        let last = report(.paused, force: true)
        player.pause()
        player.replaceCurrentItem(with: nil)
        await last?.value
        await stopSession()
        if let timeObserver { player.removeTimeObserver(timeObserver) }
        timeObserver = nil
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = nil
        itemStatus?.invalidate()
        itemStatus = nil
    }

    /// Hands playback to another device (Chromecast): ends this stream but stays ready for
    /// `start` to carry on here later.
    public func handOff() async {
        let last = report(.paused, force: true)
        player.pause()
        player.replaceCurrentItem(with: nil)
        await last?.value
        await stopSession()
    }

    public func skipMarker() {
        guard let m = activeMarker else { return }
        player.seek(to: CMTime(seconds: Double(m.endMs) / 1000 + 0.5, preferredTimescale: 600))
    }

    // MARK: - Internals

    private func stopSession() async {
        guard let s = session, let client = app.client else { return }
        session = nil
        _ = try? await client.stopPlayback(path: .init(sessionId: s.id))
    }

    private func loadNext(_ itemID: Int64) {
        Task {
            if let playlistID, let entries = try? await app.playlistItems(playlistID),
               let i = entries.firstIndex(where: { $0.item.id == itemID }), i + 1 < entries.count {
                nextUp = entries[i + 1].item
            } else if item?.type == .episode {
                nextUp = await app.next(after: itemID)
            } else {
                nextUp = nil
            }
        }
    }

    private func observe(_ playerItem: AVPlayerItem) {
        itemStatus?.invalidate()
        let id = ObjectIdentifier(playerItem)
        itemStatus = playerItem.observe(\.status) { [weak self] it, _ in
            guard it.status == .failed else { return }
            let message = it.error?.localizedDescription ?? "unknown error"
            Task { @MainActor in await self?.failed(message, item: id) }
        }
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: playerItem, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.report(.paused, force: true)
                self?.finished = true
            }
        }
    }

    /// Reports the error to the server log and retries with a more conservative profile.
    private func failed(_ message: String, item: ObjectIdentifier) async {
        // A stream that has since been replaced (a restart, the next video) failing late.
        guard player.currentItem.map(ObjectIdentifier.init) == item else { return }
        if let s = session, let client = app.client {
            let pos = Int64(max(0, player.currentTime().seconds) * 1000)
            _ = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: .error, error: "AVPlayer: \(message)")))
        }
        guard fallback < 2, let id = session?.itemId else {
            errorMessage = "This video can't be played on this device (\(message))."
            return
        }
        fallback += 1
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        await start(itemID: id, startMs: pos, audio: selection.audio, subtitle: selection.subtitle)
    }

    private func tick(_ seconds: Double) {
        guard seconds.isFinite else { return }
        position = seconds
        if let d = player.currentItem?.duration, d.isNumeric {
            setNearEnd(d.seconds - seconds < Self.nearEndWindow)
        } else {
            setNearEnd(false)
        }
        let ms = Int64(seconds * 1000)
        activeMarker = session?.markers.first { ms >= $0.startMs && ms < $0.endMs - 1000 }
        if Date().timeIntervalSince(lastReport) >= 10, player.timeControlStatus == .playing { report(.playing) }
    }

    private func setNearEnd(_ value: Bool) {
        if nearEnd != value { nearEnd = value }
    }

    /// Sends the position; returns the request so callers can wait for it.
    @discardableResult
    private func report(_ state: Schemas.PlaybackProgress.StatePayload, force: Bool = false) -> Task<Void, Never>? {
        if let offline {
            // Offline: remember the position; queue it for the server when paused or done.
            let pos = Int64(max(0, player.currentTime().seconds) * 1000)
            let dur = player.currentItem?.duration.seconds ?? 0
            let watched = finished || (dur.isFinite && dur > 0 && Double(pos) / 1000 >= dur * 0.9)
            if watched {
                if !offlineCounted { offline.downloads.recordProgress(itemID: offline.itemID, positionMs: pos, watched: true) }
                offlineCounted = true // one play per viewing
            } else if state == .paused || force {
                offline.downloads.recordProgress(itemID: offline.itemID, positionMs: pos, watched: false)
            }
            return nil
        }
        guard let s = session, let client = app.client else { return nil }
        if !force, Date().timeIntervalSince(lastReport) < 1 { return nil }
        lastReport = Date()
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        return Task { _ = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: state))) }
    }
}
