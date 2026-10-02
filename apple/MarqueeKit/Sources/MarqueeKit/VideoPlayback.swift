import AVFoundation
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

    private let app: AppSession
    private let playlistID: Int64?
    @ObservationIgnored private var timeObserver: Any?
    @ObservationIgnored private var observations: [NSKeyValueObservation] = []
    @ObservationIgnored private var endObserver: NSObjectProtocol?
    @ObservationIgnored private var lastReport = Date.distantPast
    @ObservationIgnored private var fallback = 0
    /// The version being played (nil = the server's best).
    @ObservationIgnored private var fileID: Int64?
    @ObservationIgnored private var selection: (audio: Int64?, subtitle: Int64?) = (nil, nil)
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
        guard let client = app.client else { return }
        if item?.id != itemID {
            item = try? await app.item(itemID)
            self.fileID = fileID
        } else if fileID != nil {
            self.fileID = fileID
        }
        selection = (audio, subtitle)
        let quality = app.isRemote ? QualityPreference.remote : QualityPreference.local
        let measured = app.isRemote && quality == 0 ? await measureKbps() : nil
        do {
            let s = try await client.startPlayback(body: .json(.init(
                itemId: itemID, fileId: self.fileID, audioStreamId: audio, subtitleStreamId: subtitle, startMs: startMs,
                maxBitrateKbps: quality == 0 ? nil : quality, measuredKbps: measured, profile: profile()))).ok.body.json
            await stopSession()
            session = s
            guard let url = app.absolute(s.url) else { throw MarqueeError("Bad stream address") }
            let playerItem = AVPlayerItem(url: url)
            playerItem.preferredForwardBufferDuration = app.isRemote ? 30 : 10
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

    /// Plays a downloaded file. Works without the server; progress is kept on the device and
    /// sent when the server is reachable.
    public func startLocal(itemID: Int64, file: URL, downloads: Downloads) async {
        errorMessage = nil
        finished = false
        offline = (itemID, downloads)
        offlineTitle = downloads.entries[itemID]?.item.title
        if app.client != nil { item = try? await app.item(itemID) }
        let playerItem = AVPlayerItem(url: file)
        observe(playerItem)
        player.replaceCurrentItem(with: playerItem)
        let resume = downloads.resumePosition(itemID)
        if resume > 0 { await player.seek(to: CMTime(seconds: Double(resume) / 1000, preferredTimescale: 600)) }
        player.play()
    }

    public func stop() async {
        report(.paused, force: true)
        player.pause()
        player.replaceCurrentItem(with: nil)
        await stopSession()
        if let timeObserver { player.removeTimeObserver(timeObserver) }
        timeObserver = nil
    }

    /// Hands playback to another device (Chromecast): ends this stream but stays ready for
    /// `start` to carry on here later.
    public func handOff() async {
        report(.paused, force: true)
        player.pause()
        player.replaceCurrentItem(with: nil)
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
        observations.append(playerItem.observe(\.status) { [weak self] it, _ in
            guard it.status == .failed else { return }
            let message = it.error?.localizedDescription ?? "unknown error"
            Task { @MainActor in await self?.failed(message) }
        })
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: playerItem, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.report(.paused, force: true)
                self?.finished = true
            }
        }
    }

    /// Reports the error to the server log and retries with a more conservative profile.
    private func failed(_ message: String) async {
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
        let ms = Int64(seconds * 1000)
        activeMarker = session?.markers.first { ms >= $0.startMs && ms < $0.endMs - 1000 }
        if Date().timeIntervalSince(lastReport) >= 10, player.timeControlStatus == .playing { report(.playing) }
    }

    private func report(_ state: Schemas.PlaybackProgress.StatePayload, force: Bool = false) {
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
            return
        }
        guard let s = session, let client = app.client else { return }
        if !force, Date().timeIntervalSince(lastReport) < 1 { return }
        lastReport = Date()
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        Task { _ = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: state))) }
    }
}
