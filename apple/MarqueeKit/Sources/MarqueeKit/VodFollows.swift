import Foundation
import MarqueeAPI

public typealias VodFollow = Components.Schemas.VodFollow

/// Following is turned off on the server (VodSaveSettings.followEnabled): the follow answered 409.
public struct VodFollowingOff: LocalizedError, Sendable {
    public let message: String
    public var errorDescription: String? { message }
}

/// Following series on demand: new episodes are saved to the library as they appear, within
/// the follow's size limit and the person's DVR quota.
@MainActor
public extension AppSession {
    private var followAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    /// The signed-in person's follows.
    func vodFollows() async throws -> [VodFollow] {
        switch try await followAPI.listVodFollows() {
        case let .ok(ok): return try ok.body.json
        case .forbidden: throw MarqueeError("Shows on demand aren't available on this profile")
        default: throw MarqueeError("Couldn't load the shows you follow")
        }
    }

    /// Follows a series, or changes its follow. `maxGB` 0 = no own limit, nil = the server's
    /// default (new) or unchanged; `includeExisting` saves the episodes already there too (new
    /// follows only); `destination` is a save folder's name.
    func followVodSeries(_ id: Int64, maxGB: Double?, includeExisting: Bool? = nil, destination: String? = nil) async throws -> VodFollow {
        let body = Components.Schemas.VodFollowRequest(maxGb: maxGB, includeExisting: includeExisting, destination: destination)
        switch try await followAPI.followVodSeries(path: .init(seriesId: id), body: .json(body)) {
        case let .ok(ok): return try ok.body.json
        case let .conflict(e):
            throw VodFollowingOff(message: (try? e.body.json.message) ?? "Following shows isn't turned on on this server.")
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "You can't follow shows. An admin can allow recording in Settings → Users.")
        case .notFound: throw MarqueeError("That show isn't available any more")
        default: throw MarqueeError("Couldn't follow the show")
        }
    }

    /// Stops following (episodes already saved stay in the library).
    func unfollowVodSeries(_ id: Int64) async throws {
        switch try await followAPI.unfollowVodSeries(path: .init(seriesId: id)) {
        case .noContent, .notFound: return
        default: throw MarqueeError("Couldn't stop following the show")
        }
    }

    /// Admins: whether following is turned on, and the size limit a new follow starts with (GB).
    /// nil for everyone else (the settings are admins' only).
    func vodFollowSettings() async -> (enabled: Bool, defaultMaxGB: Double)? {
        guard me?.isAdmin == true, let client, case let .ok(ok) = try? await client.getSettings(),
              let s = try? ok.body.json else { return nil }
        return (s.vodSave?.followEnabled ?? false, s.vodSave?.followDefaultMaxGb ?? 0)
    }
}

public extension VodFollow {
    /// "following", "full", "quota", "paused" (others may come): those that aren't saving now.
    var isPaused: Bool { status != "following" }

    /// "3 episodes · 4.2 GB of 10 GB"
    var summary: String {
        var parts = ["\(episodesSaved) \(episodesSaved == 1 ? "episode" : "episodes")"]
        let used = ByteCountFormatter.string(fromByteCount: usedBytes, countStyle: .decimal)
        if let max = maxGb, max > 0 {
            parts.append("\(used) of \(max.formatted(.number.precision(.fractionLength(0...1)))) GB")
        } else {
            parts.append(used)
        }
        if let q = episodesQueued, q > 0 { parts.append("\(q) queued") }
        return parts.joined(separator: " · ")
    }

    /// The status for people (the server's text, else a plain one).
    var statusLabel: String {
        if let t = statusText, !t.isEmpty { return t }
        switch status {
        case "following": return "Following"
        case "full": return "Paused: its size limit is reached"
        case "quota": return "Paused: your recording space is full"
        case "paused": return "Paused"
        default: return status.capitalized
        }
    }
}
