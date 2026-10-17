import Foundation
import MarqueeAPI

public typealias LiveChannel = Components.Schemas.LiveChannel
public typealias LiveProgramme = Components.Schemas.LiveProgramme
public typealias LiveTvStatus = Components.Schemas.LiveTvStatus
public typealias LiveGuideRow = Operations.LiveGuide.Output.Ok.Body.JsonPayloadPayload
public typealias Recording = Components.Schemas.Recording
public typealias RecordingRule = Components.Schemas.RecordingRule
public typealias DvrUsage = Components.Schemas.DvrUsage
public typealias RecordingOwner = Components.Parameters.RecordingOwner

/// Live TV (LIVE-1..4): channels, the guide, favourites and live streams.
@MainActor
public extension AppSession {
    private var liveAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    /// Whether Live TV is usable for this profile (a source with channels it may watch).
    /// UI tests force "not usable" with -marquee-test-live-off (movies on demand too), or only
    /// the channels with -marquee-test-channels-off.
    func liveStatus() async throws -> LiveTvStatus {
        let args = ProcessInfo.processInfo.arguments
        if args.contains("-marquee-test-live-off") || args.contains("-marquee-test-channels-off") {
            return LiveTvStatus(enabled: false, channels: 0, sources: [])
        }
        return try await liveAPI.liveTvStatus().ok.body.json
    }

    /// Channels with now and next; group nil = all, favorites = only the caller's, recent =
    /// recently watched (newest first), hidden = only the channels the caller hid.
    func liveChannels(group: String? = nil, favorites: Bool = false, recent: Bool = false, hidden: Bool = false) async throws -> [LiveChannel] {
        try await liveAPI.listLiveChannels(query: .init(group: group, favorites: favorites ? true : nil, recent: recent ? true : nil, hidden: hidden ? true : nil)).ok.body.json
    }

    /// Hides a channel from the caller's guide, or shows it again (LIVE-4).
    func setHidden(_ channel: Int64, _ hidden: Bool) async throws {
        if hidden {
            _ = try await liveAPI.hideLiveChannel(path: .init(channelId: channel)).noContent
        } else {
            _ = try await liveAPI.unhideLiveChannel(path: .init(channelId: channel)).noContent
        }
    }

    func liveGroups() async throws -> [String] {
        try await liveAPI.listLiveGroups().ok.body.json.map(\.name)
    }

    func liveGuide(from: Date, to: Date, group: String? = nil, favorites: Bool = false, recent: Bool = false) async throws -> [Int64: [LiveProgramme]] {
        let rows = try await liveAPI.liveGuide(query: .init(start: from, end: to, group: group, favorites: favorites ? true : nil, recent: recent ? true : nil)).ok.body.json
        // A channel listed twice is merged rather than crashing the guide.
        return Dictionary(rows.map { ($0.channelId, $0.programmes) }, uniquingKeysWith: { $0 + $1 })
    }

    func setFavorite(_ channel: Int64, _ on: Bool) async throws {
        if on {
            _ = try await liveAPI.favoriteLiveChannel(path: .init(channelId: channel)).noContent
        } else {
            _ = try await liveAPI.unfavoriteLiveChannel(path: .init(channelId: channel)).noContent
        }
    }

    /// Starts a channel; returns the session id and the stream's URL.
    func playLive(_ channel: Int64) async throws -> (id: String, url: URL) {
        switch try await liveAPI.playLiveChannel(path: .init(channelId: channel), body: .json(.init(profile: AppleDeviceProfile.current()))) {
        case let .ok(ok):
            let s = try ok.body.json
            guard let url = absolute(s.url) else { throw MarqueeError("Not connected") }
            return (s.id, url)
        case let .badGateway(e): throw MarqueeError((try? e.body.json.message) ?? "The channel isn't available right now")
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "Live TV isn't available here")
        // no_tuner: every connection to the channel's source is in use.
        case let .conflict(e): throw MarqueeError((try? e.body.json.message) ?? "No connection is free for this channel right now")
        default: throw MarqueeError("Couldn't tune the channel")
        }
    }

    /// Why a live stream ended: once it has, its playlist answers 410 with a plain-text
    /// reason. nil while it's still going (or the server can't be reached). A reason starting
    /// "Stopped:" is for the viewer; after any other, tuning the channel again works.
    nonisolated func liveEndReason(_ url: URL) async -> String? {
        var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 10)
        req.httpMethod = "GET"
        guard let (data, resp) = try? await URLSession.shared.data(for: req),
              (resp as? HTTPURLResponse)?.statusCode == 410 else { return nil }
        return (String(data: data, encoding: .utf8) ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    func stopLive(_ session: String) async {
        _ = try? await liveAPI.stopLiveSession(path: .init(sessionId: session))
    }

    /// A channel's logo (served without a token).
    func logoURL(_ c: LiveChannel) -> URL? { absolute(c.logoUrl) }

    // MARK: DVR (LIVE-5)

    /// Recordings the caller may see; `.me` = only the ones they scheduled.
    func recordings(owner: RecordingOwner? = nil) async throws -> [Recording] {
        try await liveAPI.listRecordings(query: .init(owner: owner)).ok.body.json
    }

    func recordingRules(owner: RecordingOwner? = nil) async throws -> [RecordingRule] {
        try await liveAPI.listRecordingRules(query: .init(owner: owner)).ok.body.json
    }

    /// Stops a recording in progress, keeping what's been recorded so far or deleting it.
    func stopRecording(_ id: Int64, keep: Bool) async throws {
        switch try await liveAPI.stopRecording(path: .init(recordingId: id), body: .json(.init(keep: keep))) {
        case .noContent: return
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "Only whoever scheduled this recording, or an admin, can stop it")
        case .conflict: throw MarqueeError("That recording has already finished")
        case .notFound: throw MarqueeError("That recording no longer exists")
        default: throw MarqueeError("Couldn't stop the recording")
        }
    }

    /// Records a programme, or every airing of its title (on any channel) when `series` is set.
    func record(_ p: LiveProgramme, on channel: Int64, series: Bool = false) async throws {
        switch try await liveAPI.scheduleRecording(body: .json(.init(channelId: channel, start: p.start, series: series ? true : nil, anyChannel: series ? true : nil))) {
        case .created: return
        case let .serviceUnavailable(e): throw MarqueeError((try? e.body.json.message) ?? "Recording isn't available")
        case let .notFound(e): throw MarqueeError((try? e.body.json.message) ?? "That programme isn't in the guide")
        // quota_exceeded: the server says how much space is used and what to do.
        case let .conflict(e): throw MarqueeError((try? e.body.json.message) ?? "You're out of recording space. Delete some recordings to record more.")
        case .forbidden: throw MarqueeError("You can't record. An admin can allow it in Settings → Users.")
        default: throw MarqueeError("Couldn't schedule the recording")
        }
    }

    /// Cancels an upcoming recording, stops one in progress, or removes a finished one.
    func cancelRecording(_ id: Int64, deleteFile: Bool = false) async throws {
        switch try await liveAPI.cancelRecording(path: .init(recordingId: id), query: .init(deleteFile: deleteFile ? true : nil)) {
        case .noContent: return
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "Not allowed")
        default: throw MarqueeError("Couldn't change the recording")
        }
    }

    func stopSeries(_ id: Int64) async throws {
        switch try await liveAPI.deleteRecordingRule(path: .init(ruleId: id)) {
        case .noContent: return
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "Only whoever set up this series, or an admin, can stop it")
        default: throw MarqueeError("Couldn't stop the series")
        }
    }

    /// The signed-in user's recording space and quota (LIVE-5).
    func recordingUsage() async throws -> DvrUsage {
        try await liveAPI.getRecordingUsage().ok.body.json
    }
}

public extension LiveTvStatus {
    /// May see the Recordings tab; older servers don't say, and recording implied it.
    var mayViewRecordings: Bool { canViewRecordings ?? canRecord ?? false }
}

public extension DvrUsage {
    private static func gb(_ b: Int64) -> String {
        String(format: b >= 10_000_000_000 ? "%.0f" : "%.1f", Double(b) / 1e9)
    }

    /// "12.4 of 50 GB used", or "12.4 GB used" with no quota.
    var label: String {
        quotaBytes > 0 ? "\(Self.gb(usedBytes)) of \(Self.gb(quotaBytes)) GB used" : "\(Self.gb(usedBytes)) GB used"
    }

    /// Share of the quota used, 0...1 (0 with no quota).
    var fraction: Double { quotaBytes > 0 ? min(1, Double(usedBytes) / Double(quotaBytes)) : 0 }

    /// What being over the quota means, or nil while under it.
    var overQuotaMessage: String? {
        guard overQuota else { return nil }
        return quotaAction == .deleteOldest
            ? "You're over your recording space; your oldest watched recordings will be removed to make room."
            : "You're over your recording space; new recordings won't start until you delete some."
    }
}

public extension LiveChannel {
    /// False while every connection the channel needs is in use (absent = available).
    var isAvailable: Bool { available != false }
    /// Why the channel can't be watched right now.
    var unavailableText: String { unavailableReason ?? "This channel isn't available right now." }
}

public extension Recording {
    /// "12 min in · 48 min left" for a recording in progress.
    func progressLabel(now: Date = .now) -> String {
        func fmt(_ s: TimeInterval) -> String {
            let m = max(0, Int((s / 60).rounded()))
            return m >= 60 ? "\(m / 60) h \(m % 60) min" : "\(m) min"
        }
        let left = end.timeIntervalSince(now)
        return "\(fmt(now.timeIntervalSince(start))) in · \(left > 0 ? "\(fmt(left)) left" : "finishing")"
    }
}

public extension LiveProgramme {
    var minutesLeft: Int { max(0, Int(end.timeIntervalSinceNow / 60)) }
    var isOn: Bool { start <= Date() && end > Date() }
    var progress: Double {
        let total = end.timeIntervalSince(start)
        return total > 0 ? min(1, max(0, Date().timeIntervalSince(start) / total)) : 0
    }
}
