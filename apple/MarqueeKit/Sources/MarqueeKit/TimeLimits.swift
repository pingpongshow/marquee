import Foundation
import MarqueeAPI
import Observation
import OpenAPIRuntime

public typealias TimeLimitStatus = Components.Schemas.TimeLimitStatus

/// Video was refused, or stopped, because the profile's time limit or viewing hours say so
/// (kids' profiles): the server's 403 with error code time_limit. `reason` is shown as it is.
public struct TimeLimitReached: LocalizedError, Equatable, Sendable {
    public let reason: String
    public init(reason: String?) {
        let r = reason?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        self.reason = r.isEmpty ? Self.defaultReason : r
    }
    public var errorDescription: String? { reason }
    public static let defaultReason = "Time's up for today."
    public static let code = "time_limit"

    /// From a documented error body (a 403's): nil unless it's a time limit.
    public static func from(_ error: Components.Schemas._Error?) -> TimeLimitReached? {
        guard let error, error.code == code else { return nil }
        return TimeLimitReached(reason: error.message)
    }

    /// From an undocumented answer (progress reports don't declare their 403): nil unless it's
    /// a 403 time limit.
    public static func from(statusCode: Int, payload: UndocumentedPayload) async -> TimeLimitReached? {
        guard statusCode == 403, let body = payload.body,
              let data = try? await Data(collecting: body, upTo: 64 * 1024) else { return nil }
        return from(json: data)
    }

    /// From an error body's bytes.
    public static func from(json data: Data) -> TimeLimitReached? {
        from(try? JSONDecoder().decode(Components.Schemas._Error.self, from: data))
    }
}

/// The signed-in profile's time limit today (GET /me/time-limit), kept on the device per server
/// and person so downloaded videos follow it while offline.
@MainActor @Observable
public final class TimeLimits {
    public static let shared = TimeLimits()

    /// The last status known for the signed-in person (nil: never asked, or no answer yet).
    public private(set) var status: TimeLimitStatus?
    @ObservationIgnored private var key: String?

    private init() {}

    private static func storageKey(server: String, user: Int64) -> String { "marquee.timeLimit.\(server)|\(user)" }

    /// Loads what was kept for whoever is signed in now (another person: theirs, or nothing).
    private func select(_ app: AppSession) {
        guard let server = app.server?.id, let user = app.me?.id else { return }
        let k = Self.storageKey(server: server, user: user)
        guard k != key else { return }
        key = k
        status = UserDefaults.standard.data(forKey: k).flatMap { try? JSONDecoder().decode(TimeLimitStatus.self, from: $0) }
    }

    /// The status kept for the signed-in person, without asking the server.
    public func cached(_ app: AppSession) -> TimeLimitStatus? {
        select(app)
        return status
    }

    /// Asks the server (when it can be reached) and keeps the answer. Returns the status known
    /// afterwards: the fresh one, else the one kept from before.
    @discardableResult
    public func refresh(_ app: AppSession) async -> TimeLimitStatus? {
        select(app)
        guard let client = app.client else { return status }
        let asked = key
        guard case let .ok(ok) = try? await client.myTimeLimit(), let fresh = try? ok.body.json else { return status }
        guard asked == key else { return status } // another person signed in meanwhile
        store(fresh)
        return fresh
    }

    /// Playback was refused or stopped for the time limit: remembered (offline plays are refused
    /// too until the server says otherwise).
    public func noteReached(_ reached: TimeLimitReached, app: AppSession) {
        select(app)
        var s = status ?? TimeLimitStatus(limited: true, allowedNow: false)
        s.limited = true
        s.allowedNow = false
        s.reason = reached.reason
        if s.minutesLeft != nil { s.minutesLeft = 0 }
        store(s)
    }

    /// Why a downloaded video mustn't play now (the last status known), or nil when it may.
    public func offlineRefusal(_ app: AppSession, now: Date = Date()) -> String? {
        Self.refusal(cached(app), now: now)
    }

    private func store(_ s: TimeLimitStatus) {
        status = s
        if let key, let data = try? JSONEncoder().encode(s) { UserDefaults.standard.set(data, forKey: key) }
    }

    /// Why video can't play according to a status, or nil when it can: a status that refused
    /// it until a time now past no longer does.
    public nonisolated static func refusal(_ status: TimeLimitStatus?, now: Date = Date()) -> String? {
        guard let s = status, s.limited, !s.allowedNow else { return nil }
        if let resets = s.resetsAt, resets <= now { return nil }
        return TimeLimitReached(reason: s.reason).reason
    }

    /// "45 min left today", "1 hr 5 min left today", for a profile with a daily limit.
    public nonisolated static func leftLabel(_ status: TimeLimitStatus?) -> String? {
        guard let s = status, s.limited else { return nil }
        if !s.allowedNow { return TimeLimitReached(reason: s.reason).reason }
        guard let left = s.minutesLeft else {
            if let from = s.allowedFrom, let until = s.allowedUntil { return "Viewing hours \(from)–\(until)" }
            return nil
        }
        let m = max(0, left)
        if m < 60 { return "\(m) min left today" }
        return m % 60 == 0 ? "\(m / 60) hr left today" : "\(m / 60) hr \(m % 60) min left today"
    }
}
