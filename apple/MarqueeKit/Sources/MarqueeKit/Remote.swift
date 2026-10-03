import Foundation
import MarqueeAPI

public typealias RemotePlayer = Components.Schemas.RemotePlayer
public typealias RemotePlayerState = Components.Schemas.RemotePlayerState
public typealias RemoteCommand = Components.Schemas.RemoteCommand

/// The player went away (closed, signed out, or no longer polling).
public struct RemotePlayerGone: LocalizedError, Sendable {
    public var errorDescription: String? { "That player isn't available any more." }
}

/// Remote control (USER-14): every app with a player can be controlled from another, and
/// phones, tablets and the web can control the others.
@MainActor
public extension AppSession {
    // MARK: - Being a player

    /// Waits up to 25 s for commands for this device; polling is what lists it as a player.
    func remoteInbox(cursor: Int64, state: RemotePlayerState?) async throws -> Schemas.RemoteInbox {
        guard let c = longPollClient else { throw MarqueeError("Not connected") }
        switch try await c.remoteInbox(body: .json(.init(cursor: cursor, capabilities: [.video, .music], state: state))) {
        case .ok(let ok): return try ok.body.json
        case .unauthorized: throw MarqueeError("Signed out")
        default: throw MarqueeError("The server didn't answer")
        }
    }

    /// Reports what this device is playing.
    func reportRemoteState(_ state: RemotePlayerState) async {
        _ = try? await client?.reportRemoteState(body: .json(state))
    }

    // MARK: - Controlling another

    /// Your other open apps (admins: everyone's).
    func remotePlayers() async throws -> [RemotePlayer] {
        guard let client else { throw MarqueeError("Not connected") }
        return try await client.listRemotePlayers().ok.body.json
    }

    /// A player's state; with `since`, waits up to 25 s for it to change.
    func remotePlayer(_ id: Int64, since: Int64? = nil) async throws -> RemotePlayer {
        guard let c = since == nil ? client : longPollClient else { throw MarqueeError("Not connected") }
        switch try await c.getRemotePlayer(path: .init(deviceId: id), query: .init(since: since)) {
        case .ok(let ok): return try ok.body.json
        case .notFound: throw RemotePlayerGone()
        default: throw MarqueeError("The server didn't answer")
        }
    }

    func sendRemote(_ id: Int64, _ command: RemoteCommand) async throws {
        guard let client else { throw MarqueeError("Not connected") }
        switch try await client.sendRemoteCommand(path: .init(deviceId: id), body: .json(command)) {
        case .noContent: return
        case .notFound: throw RemotePlayerGone()
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "The player can't do that.")
        default: throw MarqueeError("Couldn't reach the player.")
        }
    }
}

public extension Components.Schemas.RemotePlayer {
    /// An SF Symbol for the device's platform.
    var platformIcon: String {
        switch platform {
        case "tvos", "androidtv": "appletv"
        case "ios", "android": "iphone"
        case "ipados": "ipad"
        case "web": "macbook"
        default: "play.rectangle"
        }
    }

    /// "Playing Floating 1", "Paused: 20 Valley", or "Nothing playing".
    var nowPlayingLine: String {
        guard let s = state, let title = s.title, s.state != .idle, s.state != .stopped else { return "Nothing playing" }
        return s.state == .paused ? "Paused: \(title)" : "Playing \(title)"
    }
}
