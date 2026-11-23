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
    /// Where `nextUp` is in the playlist's play order (nil outside a playlist).
    public private(set) var nextUpPosition: Int?
    public private(set) var activeMarker: Marker?
    public private(set) var errorMessage: String?
    /// Set when the profile's time limit or viewing hours refused or ended playback (kids'
    /// profiles): the player shows it full screen instead of an error, with nothing playing.
    public private(set) var timeLimitReason: String?
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
    /// Night mode (quieter loud scenes, clearer dialogue) is on for the current stream.
    public var nightMode: Bool { session?.nightMode ?? false }

    private let app: AppSession
    private let playlistID: Int64?
    /// A shuffled playlist's play order (item ids); nil = the playlist's own order.
    private let order: [Int64]?
    /// Where this video is in the playlist's play order, when the player was told (Up Next
    /// moved on to it); otherwise its first place there.
    private let playlistPosition: Int?
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
    /// Counts starts: one overtaken by a newer start (a quick second timing step, a skipped
    /// trailer) stops its own session instead of replacing the newer one's.
    @ObservationIgnored private var startGeneration = 0
    /// The session already being replaced because the server no longer had it.
    @ObservationIgnored private var renewing: String?
    /// Night mode switched in the player; kept for the rest of this player (nil = the
    /// person's preference, decided by the server).
    @ObservationIgnored private var chosenNightMode: Bool?
    /// Set while playing a downloaded file (no server session; progress syncs later).
    @ObservationIgnored private var offline: (itemID: Int64, downloads: Downloads)?
    @ObservationIgnored private var offlineCounted = false
    /// When a downloaded video last checked the time limit with the server.
    @ObservationIgnored private var lastLimitCheck = Date()
    public private(set) var offlineTitle: String?
    @ObservationIgnored private static var measured: (kbps: Int, at: Date)?
    /// Set by `stop()`: a start still under way (the player closed while it loaded) ends there
    /// rather than play on unseen with a server session nobody stops.
    @ObservationIgnored private var stopped = false
    /// Watches the subtitle picked in the player's own menu (PLAY-8).
    @ObservationIgnored private var subtitleWatch: NSObjectProtocol?
    /// The subtitle the player shows (-1 = none), as the server last heard it.
    @ObservationIgnored private var shownSubtitle: Int64 = -1
    /// Direct play: the audio track AVPlayer plays (the server's stream id), and the watch on
    /// the player's own Audio menu (PLAY-8).
    @ObservationIgnored private var audioWatch: NSObjectProtocol?
    /// 0 = not known. Observed: the TV's own Audio menu ticks it.
    private var shownAudio: Int64 = 0
    /// The option of the player's Audio menu that is playing, as last seen: a change from it is
    /// the person's pick. Nil until the player has settled on one.
    @ObservationIgnored private var shownAudioOption: AVMediaSelectionOption?

    public init(app: AppSession, playlistID: Int64? = nil, order: [Int64]? = nil, position: Int? = nil) {
        self.app = app
        self.playlistID = playlistID
        self.order = order
        playlistPosition = position
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

    /// The item's details when the player already has them (it checked for trailers), so
    /// starting it doesn't ask the server again.
    public func use(_ detail: ItemDetail) {
        if item == nil { item = detail }
    }

    /// Starts playback of an item. `startMs` nil resumes where the user left off.
    public func start(itemID: Int64, startMs: Int64? = nil, audio: Int64? = nil, subtitle: Int64? = nil, fileID: Int64? = nil) async {
        errorMessage = nil
        finished = false
        setNearEnd(false)
        guard !stopped, let client = app.client else { return }
        startGeneration += 1
        let generation = startGeneration
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
            let response = try await client.startPlayback(body: .json(.init(
                itemId: itemID, fileId: self.fileID, audioStreamId: audio, subtitleStreamId: subtitle, startMs: startMs, nightMode: chosenNightMode,
                maxBitrateKbps: quality == 0 ? nil : quality, measuredKbps: measured,
                subtitleOffsetMs: chosenOffsets?.subtitle, audioOffsetMs: chosenOffsets?.audio, profile: profile())))
            if case let .forbidden(e) = response, let reached = TimeLimitReached.from(try? e.body.json) {
                await reachedTimeLimit(reached)
                return
            }
            let s = try response.ok.body.json
            // Checked before and after ending the old session: an overtaken start leaves the
            // newer one's session alone and ends its own.
            if generation == startGeneration { await stopSession() }
            if stopped || generation != startGeneration {
                _ = try? await client.stopPlayback(path: .init(sessionId: s.id))
                return
            }
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
            followSubtitles(playerItem, session: s)
            followAudio(playerItem, session: s)
            if s.startMs > 0 { await player.seek(to: CMTime(seconds: Double(s.startMs) / 1000, preferredTimescale: 600)) }
            // stop() ran during the seek: it has already let go of the item and ended the session.
            // A newer start has replaced the item and the session.
            guard !stopped, generation == startGeneration else { return }
            player.play()
            loadNext(itemID)
        } catch {
            // Overtaken or called off (a newer timing step cancelled this one): not a failure.
            guard generation == startGeneration, !Task.isCancelled, (error as? URLError)?.code != .cancelled else { return }
            errorMessage = (error as? MarqueeError)?.message ?? "Couldn't start playback: \(error.localizedDescription)"
        }
    }

    // MARK: - Subtitles chosen in the player (PLAY-8)

    /// The text subtitles of the session's file in the order the server lists them as HLS
    /// renditions (by stream id; picture subtitles are burned in, not listed).
    private func renditionStreams(_ s: PlaybackSession) -> [MediaStream] {
        let files = item?.info.versions.flatMap(\.files) ?? []
        let file = files.first { $0.id == s.fileId } ?? files.first
        let image: Set<String> = ["hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "xsub"]
        return (file?.streams ?? []).filter { $0.kind == .subtitle && !image.contains($0.codec) }.sorted { $0.id < $1.id }
    }

    /// The server's stream id for a rendition in the player's subtitle menu: by its place in the
    /// list when the lists agree, else by language and the forced flag.
    private static func streamID(for option: AVMediaSelectionOption, in group: AVMediaSelectionGroup, streams: [MediaStream]) -> Int64? {
        let options = group.options
        let forced = option.hasMediaCharacteristic(.containsOnlyForcedSubtitles)
        if options.count == streams.count, let i = options.firstIndex(of: option) { return streams[i].id }
        let lang = option.extendedLanguageTag ?? option.locale?.language.languageCode?.identifier
        let candidates = streams.filter { s in
            s.forced == forced && (lang == nil || Self.sameLanguage(s.language, lang))
        }
        return candidates.first { ($0.title ?? "").isEmpty == false && option.displayName.contains($0.title!) }?.id ?? candidates.first?.id
    }

    private static func sameLanguage(_ a: String?, _ b: String?) -> Bool {
        func code(_ l: String?) -> String? {
            guard let l, !l.isEmpty, l != "und" else { return nil }
            return Locale(identifier: l).language.languageCode?.identifier(.alpha2) ?? l.lowercased()
        }
        return code(a) != nil && code(a) == code(b)
    }

    /// Shows the subtitle the server chose (its remembered choice for the show, or the person's
    /// settings) instead of AVFoundation's own pick, and tells the server when the person picks
    /// another in the player's menu, so the next episode has it too.
    private func followSubtitles(_ playerItem: AVPlayerItem, session s: PlaybackSession) {
        if let subtitleWatch { NotificationCenter.default.removeObserver(subtitleWatch) }
        subtitleWatch = nil
        shownSubtitle = (s.subtitleStreamId ?? 0) > 0 ? s.subtitleStreamId! : -1
        guard s._protocol == .hls else { return }
        let streams = renditionStreams(s)
        let chosen = s.subtitleStreamId ?? 0
        // A picture subtitle the server burns in isn't in the menu: the menu stays empty.
        let burned = chosen > 0 && !streams.contains { $0.id == chosen }
        Task { [weak self] in
            guard let group = try? await playerItem.asset.loadMediaSelectionGroup(for: .legible) else { return }
            guard let self, self.player.currentItem === playerItem, self.session?.id == s.id else { return }
            let pick = streams.firstIndex { $0.id == chosen }.flatMap { i in
                group.options.count == streams.count ? group.options[i] : nil
            } ?? group.options.first { Self.streamID(for: $0, in: group, streams: streams) == chosen && chosen > 0 }
            if pick != nil || !burned { playerItem.select(pick, in: group) }
            self.subtitleWatch = NotificationCenter.default.addObserver(forName: AVPlayerItem.mediaSelectionDidChangeNotification,
                                                                        object: playerItem, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.subtitleChanged(playerItem, group: group, streams: streams, burned: burned, session: s) }
            }
        }
    }

    private func subtitleChanged(_ playerItem: AVPlayerItem, group: AVMediaSelectionGroup, streams: [MediaStream], burned: Bool, session s: PlaybackSession) {
        guard session?.id == s.id, player.currentItem === playerItem else { return }
        let option = playerItem.currentMediaSelection.selectedMediaOption(in: group)
        // Nothing in the menu while a burned-in subtitle shows isn't "subtitles off".
        if option == nil, burned { return }
        let id: Int64 = option.map { Self.streamID(for: $0, in: group, streams: streams) ?? 0 } ?? -1
        guard id != 0, id != shownSubtitle else { return } // 0: a rendition it couldn't place
        shownSubtitle = id
        // A restart (an audio or timing change) keeps it.
        selection.subtitle = id
        guard let client = app.client else { return }
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        let state: Schemas.PlaybackProgress.StatePayload = player.timeControlStatus == .paused ? .paused : .playing
        Task { _ = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: state, subtitleStreamId: id))) }
    }

    // MARK: - Audio chosen in the player (PLAY-8)

    /// The audio tracks of the session's file, in the file's order.
    private func fileAudioStreams(_ s: PlaybackSession) -> [MediaStream] {
        let files = item?.info.versions.flatMap(\.files) ?? []
        let file = files.first { $0.id == s.fileId } ?? files.first
        return (file?.streams ?? []).filter { $0.kind == .audio }.sorted { $0.id < $1.id }
    }

    /// A file played as it is holds every audio track, and AVPlayer plays the file's default
    /// one whatever the server chose: this plays the server's choice (the show's remembered
    /// audio, or the person's language), and tells the server when the person picks another in
    /// the player's own Audio menu, so the next episode has it too. (A converted stream has the
    /// chosen track alone.)
    private func followAudio(_ playerItem: AVPlayerItem, session s: PlaybackSession) {
        if let audioWatch { NotificationCenter.default.removeObserver(audioWatch) }
        audioWatch = nil
        shownAudio = s.audioStreamId ?? 0
        shownAudioOption = nil
        guard s._protocol == .file else { return }
        let streams = fileAudioStreams(s)
        guard streams.count > 1 else { return }
        let chosen = s.audioStreamId ?? 0
        Task { [weak self] in
            guard let group = try? await playerItem.asset.loadMediaSelectionGroup(for: .audible) else { return }
            guard let self, self.player.currentItem === playerItem, self.session?.id == s.id else { return }
            let pick = streams.first { $0.id == chosen }.flatMap { Self.audioOption(for: $0, in: group, streams: streams) }
            if let pick { playerItem.select(pick, in: group) }
            // What plays now is where the person's own picks are told from: the server's choice
            // when it could be selected, else whatever the player chose (the file's default).
            // The id is that track's, not the server's choice it couldn't select: otherwise the
            // first change of any menu reported the default as the person's pick.
            self.shownAudioOption = pick ?? playerItem.currentMediaSelection.selectedMediaOption(in: group)
            if let now = self.shownAudioOption {
                self.shownAudio = Self.audioStreamID(for: now, in: group, streams: streams) ?? 0
            }
            self.audioWatch = NotificationCenter.default.addObserver(forName: AVPlayerItem.mediaSelectionDidChangeNotification,
                                                                     object: playerItem, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.audioChanged(playerItem, group: group, streams: streams, session: s) }
            }
        }
    }

    /// The server's stream for an option of the player's Audio menu: by its place when the lists
    /// agree, else by language, and only when one track alone has that language (of two
    /// English tracks, the film and its commentary, it can't say which).
    private static func audioStreamID(for option: AVMediaSelectionOption, in group: AVMediaSelectionGroup, streams: [MediaStream]) -> Int64? {
        if group.options.count == streams.count, let i = group.options.firstIndex(of: option) { return streams[i].id }
        let lang = option.extendedLanguageTag ?? option.locale?.language.languageCode?.identifier
        let same = streams.filter { sameLanguage($0.language, lang) }
        return same.count == 1 ? same[0].id : nil
    }

    /// The other way: the Audio menu's option for one of the file's tracks, as surely.
    private static func audioOption(for stream: MediaStream, in group: AVMediaSelectionGroup, streams: [MediaStream]) -> AVMediaSelectionOption? {
        if group.options.count == streams.count, let i = streams.firstIndex(where: { $0.id == stream.id }) { return group.options[i] }
        guard streams.filter({ sameLanguage($0.language, stream.language) }).count == 1 else { return nil }
        let same = group.options.filter { sameLanguage(stream.language, $0.extendedLanguageTag ?? $0.locale?.language.languageCode?.identifier) }
        return same.count == 1 ? same[0] : nil
    }

    private func audioChanged(_ playerItem: AVPlayerItem, group: AVMediaSelectionGroup, streams: [MediaStream], session s: PlaybackSession) {
        guard session?.id == s.id, player.currentItem === playerItem,
              let option = playerItem.currentMediaSelection.selectedMediaOption(in: group) else { return }
        // The player hadn't settled on a track when the watch began: this is its own first
        // choice (or ours arriving), not the person's.
        guard let before = shownAudioOption else {
            shownAudioOption = option
            shownAudio = Self.audioStreamID(for: option, in: group, streams: streams) ?? 0
            return
        }
        // Another menu changed (subtitles), or our own selection came round again.
        guard option != before else { return }
        shownAudioOption = option
        // A track it can't place isn't reported (0), and stays off the TV's Audio menu's tick.
        guard let id = Self.audioStreamID(for: option, in: group, streams: streams) else {
            shownAudio = 0
            return
        }
        guard id != shownAudio else { return }
        shownAudio = id
        // A restart (a subtitle or timing change) keeps it.
        selection.audio = id
        guard let client = app.client else { return }
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        let state: Schemas.PlaybackProgress.StatePayload = player.timeControlStatus == .paused ? .paused : .playing
        Task { _ = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: state, audioStreamId: id))) }
    }

    /// Restarts at the current position with different tracks (audio changes and image
    /// subtitles need a new session; text subtitles switch in the player's own menu).
    public func change(audio: Int64? = nil, subtitle: Int64? = nil) async {
        guard let id = session?.itemId else { return }
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        await start(itemID: id, startMs: pos, audio: audio ?? selection.audio, subtitle: subtitle ?? selection.subtitle)
    }

    /// Switches night mode, restarting at the current position like an audio change.
    public func setNightMode(_ on: Bool) async {
        guard offline == nil, let id = session?.itemId else { return }
        chosenNightMode = on
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        await start(itemID: id, startMs: pos, audio: selection.audio, subtitle: selection.subtitle)
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
        if app.client != nil, !app.isOffline { item = try? await app.item(itemID) }
        // The time limit applies to downloads too: asked when the server can be reached, else
        // the last answer kept on the device.
        await TimeLimits.shared.refresh(app)
        lastLimitCheck = Date()
        if let reason = TimeLimits.shared.offlineRefusal(app) {
            timeLimitReason = reason
            return
        }
        guard !stopped else { return }
        let playerItem = AVPlayerItem(url: file)
        playerItem.textStyleRules = app.subtitleStyle.textStyleRules
        describe(playerItem)
        observe(playerItem)
        player.replaceCurrentItem(with: playerItem)
        let resume = downloads.resumePosition(itemID)
        if resume > 0 { await player.seek(to: CMTime(seconds: Double(resume) / 1000, preferredTimescale: 600)) }
        guard !stopped else { return }
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
        stopped = true
        unfollow()
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

    /// Stops watching the player's own Subtitles and Audio menus. Each watch holds its player
    /// item, so one left behind kept a stream that had ended in memory.
    private func unfollow() {
        if let subtitleWatch { NotificationCenter.default.removeObserver(subtitleWatch) }
        subtitleWatch = nil
        if let audioWatch { NotificationCenter.default.removeObserver(audioWatch) }
        audioWatch = nil
        shownAudioOption = nil
    }

    /// The audio tracks of the file that's playing (the version chosen, not always the first),
    /// for the TV's Audio menu.
    public var audioStreams: [MediaStream] {
        let files = item?.info.versions.flatMap(\.files) ?? []
        let file = files.first { $0.id == (session?.fileId ?? fileID) } ?? files.first
        return (file?.streams ?? []).filter { $0.kind == .audio }
    }

    /// The audio track playing: the one picked in the player's own Audio menu on a file played
    /// as it is, else the one the stream was started with.
    public var playingAudioID: Int64? { shownAudio > 0 ? shownAudio : session?.audioStreamId }

    /// Hands playback to another device (Chromecast): ends this stream but stays ready for
    /// `start` to carry on here later.
    public func handOff() async {
        unfollow()
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

    /// The time limit refused or ended playback: nothing plays, the server session ends, and
    /// the player shows the reason.
    private func reachedTimeLimit(_ reached: TimeLimitReached) async {
        TimeLimits.shared.noteReached(reached, app: app)
        timeLimitReason = reached.reason
        errorMessage = nil
        offsetRestart?.cancel()
        unfollow()
        player.pause()
        player.replaceCurrentItem(with: nil)
        await stopSession()
    }

    /// A downloaded video playing while the server can be reached checks the limit every few
    /// minutes (the server doesn't see it play, so nothing else would stop it).
    private func checkOfflineLimit() {
        guard offline != nil, timeLimitReason == nil, app.client != nil, Date().timeIntervalSince(lastLimitCheck) >= 300 else { return }
        lastLimitCheck = Date()
        Task {
            await TimeLimits.shared.refresh(app)
            if let reason = TimeLimits.shared.offlineRefusal(app), timeLimitReason == nil {
                report(.paused, force: true)
                await reachedTimeLimit(TimeLimitReached(reason: reason))
            }
        }
    }

    private func stopSession() async {
        guard let s = session, let client = app.client else { return }
        session = nil
        _ = try? await client.stopPlayback(path: .init(sessionId: s.id))
    }

    private func loadNext(_ itemID: Int64) {
        Task {
            if let playlistID, let entries = try? await app.playlistItems(playlistID) {
                // Up next in the playlist (shuffled or in order); nothing after its last item.
                // From its own place when that's known: found by id alone, a video that's in the
                // playlist twice led back to what follows its first place, for ever.
                let ids = order ?? entries.map(\.item.id)
                let at = playlistPosition.flatMap { ids.indices.contains($0) && ids[$0] == itemID ? $0 : nil } ?? ids.firstIndex(of: itemID)
                let next = at.flatMap { $0 + 1 < ids.count ? $0 + 1 : nil }
                if let next, order == nil {
                    nextUp = entries[next].item
                } else {
                    nextUp = next.flatMap { i in entries.first { $0.item.id == ids[i] }?.item }
                }
                nextUpPosition = nextUp == nil ? nil : next
            } else if item?.type == .episode {
                nextUp = await app.next(after: itemID)
                nextUpPosition = nil
            } else {
                nextUp = nil
                nextUpPosition = nil
            }
        }
    }

    private func observe(_ playerItem: AVPlayerItem) {
        itemStatus?.invalidate()
        let id = ObjectIdentifier(playerItem)
        itemStatus = playerItem.observe(\.status) { [weak self] it, _ in
            guard it.status == .failed else { return }
            let message = it.error?.localizedDescription ?? "unknown error"
            let detail = Self.failureDetail(it)
            Task { @MainActor in await self?.failed(message, detail: detail, item: id) }
        }
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: playerItem, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.report(.paused, force: true)
                self?.finished = true
            }
        }
    }

    /// What went wrong, in full, for the server's log: the short message is often a generic
    /// CoreMedia code (-12927 says little), so the chain of underlying errors and the stream's
    /// last error log entries (HTTP status, the player's own comment, the file it was reading).
    nonisolated static func failureDetail(_ item: AVPlayerItem) -> String {
        var parts: [String] = []
        var next = item.error as NSError?
        while let e = next, parts.count < 4 {
            parts.append("\(e.domain) \(e.code)" + (e.localizedFailureReason.map { ": \($0)" } ?? ""))
            next = e.userInfo[NSUnderlyingErrorKey] as? NSError
        }
        for ev in item.errorLog()?.events.suffix(3) ?? [] {
            var s = "log \(ev.errorDomain) \(ev.errorStatusCode)"
            if let c = ev.errorComment, !c.isEmpty { s += " \(c)" }
            if let u = ev.uri { s += " (\(URL(string: u)?.lastPathComponent ?? u))" }
            parts.append(s)
        }
        return parts.joined(separator: "; ")
    }

    /// Reports the error to the server log and retries with a more conservative profile.
    private func failed(_ message: String, detail: String, item: ObjectIdentifier) async {
        // A stream that has since been replaced (a restart, the next video) failing late.
        guard player.currentItem.map(ObjectIdentifier.init) == item else { return }
        if let s = session, let client = app.client {
            let pos = Int64(max(0, player.currentTime().seconds) * 1000)
            let report = detail.isEmpty ? "AVPlayer: \(message)" : "AVPlayer: \(message) [\(detail)]"
            _ = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: .error, error: report)))
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
        if player.timeControlStatus == .playing { checkOfflineLimit() }
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
        return Task { [weak self] in
            let response = try? await client.reportPlayback(path: .init(sessionId: s.id), body: .json(.init(positionMs: pos, state: state)))
            guard let self, self.session?.id == s.id else { return }
            switch response {
            case .notFound? where state == .playing:
                await self.renew(s)
            case let .undocumented(code, payload)?:
                // A session running when the time runs out ends: its reports answer 403 time_limit.
                guard let reached = await TimeLimitReached.from(statusCode: code, payload: payload) else { return }
                await self.reachedTimeLimit(reached)
            default:
                break
            }
        }
    }

    /// The server no longer has the session (left paused for hours, or the server restarted):
    /// Play carries on from here with a new one, instead of stalling once the little the
    /// player had buffered runs out (Back and Resume used to be the only way out).
    private func renew(_ s: PlaybackSession) async {
        guard !stopped, offline == nil, renewing != s.id else { return }
        renewing = s.id
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        await start(itemID: s.itemId, startMs: pos, audio: selection.audio, subtitle: selection.subtitle, fileID: fileID)
    }
}
