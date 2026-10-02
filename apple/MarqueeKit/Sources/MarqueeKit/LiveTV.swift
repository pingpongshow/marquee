import Foundation
import MarqueeAPI

public typealias LiveChannel = Components.Schemas.LiveChannel
public typealias LiveProgramme = Components.Schemas.LiveProgramme
public typealias LiveTvStatus = Components.Schemas.LiveTvStatus
public typealias LiveGuideRow = Operations.LiveGuide.Output.Ok.Body.JsonPayloadPayload
public typealias Recording = Components.Schemas.Recording
public typealias RecordingRule = Components.Schemas.RecordingRule

/// Live TV (LIVE-1..4): channels, the guide, favourites and live streams.
@MainActor
public extension AppSession {
    private var liveAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    func liveStatus() async throws -> LiveTvStatus {
        try await liveAPI.liveTvStatus().ok.body.json
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
        return Dictionary(uniqueKeysWithValues: rows.map { ($0.channelId, $0.programmes) })
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
        default: throw MarqueeError("Couldn't tune the channel")
        }
    }

    func stopLive(_ session: String) async {
        _ = try? await liveAPI.stopLiveSession(path: .init(sessionId: session))
    }

    /// A channel's logo (served without a token).
    func logoURL(_ c: LiveChannel) -> URL? { absolute(c.logoUrl) }

    // MARK: DVR (LIVE-5)

    func recordings() async throws -> [Recording] {
        try await liveAPI.listRecordings().ok.body.json
    }

    func recordingRules() async throws -> [RecordingRule] {
        try await liveAPI.listRecordingRules().ok.body.json
    }

    /// Records a programme, or every airing of its title (on any channel) when `series` is set.
    func record(_ p: LiveProgramme, on channel: Int64, series: Bool = false) async throws {
        switch try await liveAPI.scheduleRecording(body: .json(.init(channelId: channel, start: p.start, series: series ? true : nil, anyChannel: series ? true : nil))) {
        case .created: return
        case let .serviceUnavailable(e): throw MarqueeError((try? e.body.json.message) ?? "Recording isn't available")
        case let .notFound(e): throw MarqueeError((try? e.body.json.message) ?? "That programme isn't in the guide")
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
        _ = try await liveAPI.deleteRecordingRule(path: .init(ruleId: id)).noContent
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
