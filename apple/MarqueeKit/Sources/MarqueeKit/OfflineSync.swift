import Foundation
import HTTPTypes
import MarqueeAPI
import Observation
import OpenAPIRuntime

/// A change the person made that hasn't reached the server yet (USER-18): a rating, watched
/// state, watchlist, comment, or progress from an offline play. Tagged with the server and
/// person it belongs to, and the time it was made, which the server uses to ignore it when a
/// newer change of the same kind was made elsewhere.
public struct PendingChange: Codable, Equatable, Sendable {
    public enum Kind: String, Codable, Sendable { case rating, watched, watchlist, comment, progress }

    public var kind: Kind
    public var itemID: Int64
    /// When the person made the change.
    public var at: Date
    /// Server id and person (nil only on records from before they were tagged: whoever is signed in).
    public var server: String?
    public var user: Int64?
    /// rating: 0–10, nil clears.
    public var rating: Double?
    /// watched: true = watched; watchlist: true = on the list.
    public var on: Bool?
    /// comment: the text ("" removes it), or nil with `reviewUser` to delete that person's review.
    public var comment: String?
    public var reviewUser: Int64?
    /// progress (an offline play).
    public var positionMs: Int64?
    public var watched: Bool?

    public init(kind: Kind, itemID: Int64, at: Date = Date(), server: String? = nil, user: Int64? = nil,
                rating: Double? = nil, on: Bool? = nil, comment: String? = nil, reviewUser: Int64? = nil,
                positionMs: Int64? = nil, watched: Bool? = nil) {
        self.kind = kind
        self.itemID = itemID
        self.at = at
        self.server = server
        self.user = user
        self.rating = rating
        self.on = on
        self.comment = comment
        self.reviewUser = reviewUser
        self.positionMs = positionMs
        self.watched = watched
    }

    public static func rating(_ item: Int64, _ rating: Double?) -> PendingChange { .init(kind: .rating, itemID: item, rating: rating) }
    public static func watched(_ item: Int64, _ on: Bool) -> PendingChange { .init(kind: .watched, itemID: item, on: on) }
    public static func watchlist(_ item: Int64, _ on: Bool) -> PendingChange { .init(kind: .watchlist, itemID: item, on: on) }
    public static func comment(_ item: Int64, _ text: String) -> PendingChange { .init(kind: .comment, itemID: item, comment: text) }
    public static func deleteReview(_ item: Int64, user: Int64) -> PendingChange { .init(kind: .comment, itemID: item, reviewUser: user) }
    public static func progress(_ item: Int64, positionMs: Int64, watched: Bool) -> PendingChange {
        .init(kind: .progress, itemID: item, positionMs: positionMs, watched: watched)
    }

    func belongs(server: String, user: Int64) -> Bool {
        (self.server == nil || self.server == server) && (self.user == nil || self.user == user)
    }

    /// Whether `other` replaces this one: same person and server, same item and kind. Finished
    /// offline plays each count as a play, so only unfinished progress is replaced.
    func replaced(by other: PendingChange) -> Bool {
        guard server == other.server, user == other.user, itemID == other.itemID, kind == other.kind else { return false }
        switch kind {
        case .progress: return watched != true && other.watched != true
        case .comment: return (reviewUser ?? user) == (other.reviewUser ?? other.user)
        default: return true
        }
    }
}

/// What to do with a queued change after trying to send it.
public enum SyncOutcome: Equatable, Sendable { case sent, drop, keep }

/// The queue itself: ordered, coalesced, persisted as JSON. A value type, so it's testable on
/// its own.
public struct PendingQueue: Codable, Equatable, Sendable {
    public private(set) var entries: [PendingChange] = []

    public init(_ entries: [PendingChange] = []) { self.entries = entries }

    /// Adds a change at the end, replacing an earlier one for the same item and kind.
    public mutating func add(_ change: PendingChange) {
        entries.removeAll { $0.replaced(by: change) }
        entries.append(change)
    }

    /// The changes that belong to this server and person, oldest first.
    public func flushable(server: String, user: Int64) -> [PendingChange] {
        entries.filter { $0.belongs(server: server, user: user) }
    }

    public func count(server: String, user: Int64) -> Int { flushable(server: server, user: user).count }

    /// The newest waiting change of a kind for an item (what the UI should show).
    public func latest(_ kind: PendingChange.Kind, item: Int64, server: String, user: Int64) -> PendingChange? {
        entries.last { $0.kind == kind && $0.itemID == item && $0.belongs(server: server, user: user) }
    }

    /// Removes a change once the server took it or rejected it for good; keeps it otherwise.
    public mutating func resolve(_ change: PendingChange, _ outcome: SyncOutcome) {
        guard outcome != .keep, let i = entries.firstIndex(of: change) else { return }
        entries.remove(at: i)
    }

    /// From the server's answer (nil = no answer): sent, rejected for good (a 4xx other than
    /// sign-in, timeout or rate limiting: the item is gone, say), or worth trying again.
    public static func outcome(status: Int?) -> SyncOutcome {
        guard let status else { return .keep }
        switch status {
        case 200..<300: return .sent
        case 401, 408, 429: return .keep
        case 400..<500: return .drop
        default: return .keep
        }
    }

    public static func load(from url: URL) -> PendingQueue {
        guard let data = try? Data(contentsOf: url) else { return .init() }
        return (try? JSONDecoder().decode(PendingQueue.self, from: data)) ?? .init()
    }

    public func save(to url: URL) throws {
        try JSONEncoder().encode(self).write(to: url, options: .atomic)
    }
}

/// Fails the per-person change requests as if there were no network (UI tests: the
/// `-marquee-offline-changes` launch argument), so the offline path can be exercised against a
/// running server.
struct OfflineChangesMiddleware: ClientMiddleware {
    static let operations: Set<String> = [
        Operations.RateItem.id, Operations.MarkWatched.id, Operations.MarkUnwatched.id,
        Operations.AddToWatchlist.id, Operations.RemoveFromWatchlist.id,
        Operations.SetReview.id, Operations.DeleteReview.id, Operations.SyncProgress.id,
    ]
    func intercept(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String,
                   next: @Sendable (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)) async throws -> (HTTPResponse, HTTPBody?) {
        if Self.operations.contains(operationID) { throw URLError(.notConnectedToInternet) }
        return try await next(request, body, baseURL)
    }
}

/// Keeps changes made while the server can't be reached and sends them when it can (USER-18).
/// One queue on the device, scoped per server and person; offline plays of downloads go
/// through it too.
@MainActor @Observable
public final class OfflineSync {
    public static let shared = OfflineSync()

    /// Set by the `-marquee-offline-changes` launch argument (UI tests).
    public static let simulateOffline = ProcessInfo.processInfo.arguments.contains("-marquee-offline-changes")

    public private(set) var queue: PendingQueue
    /// When the queue was last sent (something reached the server).
    public private(set) var lastSync: Date?
    public private(set) var isSyncing = false
    /// Bumped after a sync sent something: screens reload to show the server's truth.
    public private(set) var generation = 0
    /// The last time a change was kept on the device (the "Saved offline" hint).
    public private(set) var lastQueued: Date?

    @ObservationIgnored weak var app: AppSession?
    @ObservationIgnored private let fileURL: URL
    @ObservationIgnored private var flushing = false
    @ObservationIgnored private var flushAgain = false
    @ObservationIgnored private var retryTask: Task<Void, Never>?
    @ObservationIgnored private var failures = 0
    private static let io = DispatchQueue(label: "app.marquee.sync.io", qos: .utility)
    private static let lastSyncKey = "marquee.sync.lastSync"

    public static var defaultURL: URL {
        let d = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)
        return d.appending(path: "pending-changes.json")
    }

    public convenience init() {
        self.init(fileURL: Self.defaultURL, legacyProgressURL: Downloads.directory.appending(path: "pending.json"))
    }

    /// `legacyProgressURL`: the offline-play queue Downloads kept before, folded in once.
    public init(fileURL: URL, legacyProgressURL: URL?) {
        self.fileURL = fileURL
        queue = PendingQueue.load(from: fileURL)
        lastSync = UserDefaults.standard.object(forKey: Self.lastSyncKey) as? Date
        if let legacy = legacyProgressURL, let data = try? Data(contentsOf: legacy) {
            for p in (try? JSONDecoder().decode([Downloads.PendingProgress].self, from: data)) ?? [] {
                queue.add(.init(kind: .progress, itemID: p.itemID, at: p.at, server: p.server, user: p.user,
                                positionMs: p.positionMs, watched: p.watched))
            }
            try? queue.save(to: fileURL)
            try? FileManager.default.removeItem(at: legacy)
        }
    }

    // MARK: - Who

    private static func lastUserKey(_ server: String) -> String { "marquee.sync.lastUser.\(server)" }

    /// Remembers who is signed in to a server, so changes made while /me can't be asked
    /// (offline at launch) are still tagged with the right person.
    static func rememberUser(_ user: Int64, server: String) {
        UserDefaults.standard.set(user, forKey: lastUserKey(server))
        UserDefaults.standard.set(user, forKey: "marquee.downloads.lastUser")
    }

    /// The server and person changes belong to now.
    var scope: (server: String, user: Int64)? {
        guard let server = app?.server?.id ?? ServerStore.currentServerID else { return nil }
        if let me = app?.me?.id { return (server, me) }
        // Signed out: nobody's changes are shown or sent.
        if app != nil, app?.state == .signedOut || app?.state == .noServer { return nil }
        let stored = (UserDefaults.standard.object(forKey: Self.lastUserKey(server)) as? NSNumber)
            ?? (UserDefaults.standard.object(forKey: "marquee.downloads.lastUser") as? NSNumber)
        return stored.map { (server, $0.int64Value) }
    }

    /// Changes waiting for the signed-in person.
    public var pendingCount: Int {
        guard let s = scope else { return 0 }
        return queue.count(server: s.server, user: s.user)
    }

    // MARK: - What the UI shows

    private func latest(_ kind: PendingChange.Kind, _ item: Int64) -> PendingChange? {
        guard let s = scope else { return nil }
        return queue.latest(kind, item: item, server: s.server, user: s.user)
    }

    /// A waiting rating: .some(nil) when the person cleared it.
    public func pendingRating(_ item: Int64) -> Double?? {
        latest(.rating, item).map { $0.rating }
    }

    public func pendingWatched(_ item: Int64) -> Bool? { latest(.watched, item)?.on }

    public func pendingWatchlist(_ item: Int64) -> Bool? { latest(.watchlist, item)?.on }

    /// A waiting comment of your own ("" = removed).
    public func pendingComment(_ item: Int64) -> String? {
        guard let c = latest(.comment, item), c.reviewUser == nil || c.reviewUser == scope?.user else { return nil }
        return c.comment ?? ""
    }

    // MARK: - Making changes

    /// Sends a change now, or keeps it to send later when the server can't be reached (no
    /// network, a timeout, the server down, offline). Returns true when the server took it,
    /// false when it was kept; throws when the server refused it.
    @discardableResult
    public func submit(_ change: PendingChange) async throws -> Bool {
        var change = change
        if change.server == nil, change.user == nil, let s = scope {
            change.server = s.server
            change.user = s.user
        }
        guard let client = app?.client else { enqueue(change); return false }
        let status: Int, message: String?
        do {
            (status, message) = try await Self.send(change, client)
        } catch {
            guard Self.isNetworkFailure(error) else { throw error }
            enqueue(change)
            return false
        }
        switch PendingQueue.outcome(status: status) {
        case .sent:
            // The server is reachable: anything waiting can go too.
            if !queue.entries.isEmpty { Task { await flush() } }
            return true
        case .keep:
            enqueue(change)
            return false
        case .drop:
            throw MarqueeError(message ?? (status == 404 ? "This item is gone." : "The server refused the change."))
        }
    }

    /// Keeps a change to send later (offline plays, or a change that couldn't be sent).
    public func enqueue(_ change: PendingChange) {
        var change = change
        if change.server == nil, change.user == nil, let s = scope {
            change.server = s.server
            change.user = s.user
        }
        queue.add(change)
        lastQueued = Date()
        save()
        scheduleRetry()
    }

    // MARK: - Sending

    /// Sends what's waiting for the signed-in person, in order. Sent changes and those the
    /// server rejected for good go; on a network error the rest wait and are tried again
    /// later (backing off); on 401 they wait for sign-in.
    public func flush() async {
        guard !flushing else { flushAgain = true; return }
        flushing = true
        isSyncing = true
        defer { flushing = false; isSyncing = false }
        repeat {
            flushAgain = false
            guard let app, app.state == .signedIn, app.client != nil, !queue.entries.isEmpty else { return }
            if app.me == nil { await app.refreshMe() }
            guard let client = app.client, let server = app.server?.id, let user = app.me?.id else { return }
            var sent = false, unreachable = false
            for change in queue.flushable(server: server, user: user) {
                let status: Int?
                do {
                    status = try await Self.send(change, client).status
                } catch {
                    if Self.isNetworkFailure(error) { unreachable = true; break }
                    status = nil // not an answer from the server: try again later
                }
                let outcome = PendingQueue.outcome(status: status)
                if outcome == .sent { sent = true }
                queue.resolve(change, outcome)
                if outcome == .keep {
                    // 401 waits for sign-in; anything else (no answer, 5xx, rate limiting) retries.
                    unreachable = status != 401
                    break
                }
            }
            save()
            if sent {
                lastSync = Date()
                UserDefaults.standard.set(lastSync, forKey: Self.lastSyncKey)
                generation += 1
            }
            if unreachable {
                failures += 1
                scheduleRetry()
                return
            }
            failures = 0
        } while flushAgain
    }

    /// Tries again later while changes wait: 5 s, then doubling up to 5 minutes.
    private func scheduleRetry() {
        guard retryTask == nil else { return }
        let delay = min(300, 5 * pow(2, Double(min(failures, 6))))
        retryTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(delay))
            guard let self, !Task.isCancelled else { return }
            self.retryTask = nil
            guard self.pendingCount > 0 else { return }
            await self.flush()
        }
    }

    private func save() {
        let q = queue, url = fileURL
        Self.io.async { try? q.save(to: url) }
    }

    /// Sends one change with the time it was made. Returns the status and the server's message.
    nonisolated static func send(_ c: PendingChange, _ client: Client) async throws -> (status: Int, message: String?) {
        switch c.kind {
        case .rating:
            switch try await client.rateItem(path: .init(itemId: c.itemID), body: .json(.init(rating: c.rating, at: c.at))) {
            case .noContent: return (204, nil)
            case let .badRequest(r): return (400, try? r.body.json.message)
            case .unauthorized: return (401, nil)
            case .notFound: return (404, nil)
            case let .undocumented(code, _): return (code, nil)
            }
        case .watched:
            if c.on == true {
                switch try await client.markWatched(path: .init(itemId: c.itemID), query: .init(at: c.at)) {
                case .noContent: return (204, nil)
                case .unauthorized: return (401, nil)
                case .notFound: return (404, nil)
                case let .undocumented(code, _): return (code, nil)
                }
            }
            switch try await client.markUnwatched(path: .init(itemId: c.itemID), query: .init(at: c.at)) {
            case .noContent: return (204, nil)
            case .unauthorized: return (401, nil)
            case .notFound: return (404, nil)
            case let .undocumented(code, _): return (code, nil)
            }
        case .watchlist:
            if c.on == true {
                switch try await client.addToWatchlist(path: .init(itemId: c.itemID), query: .init(at: c.at)) {
                case .noContent: return (204, nil)
                case .unauthorized: return (401, nil)
                case .notFound: return (404, nil)
                case let .undocumented(code, _): return (code, nil)
                }
            }
            switch try await client.removeFromWatchlist(path: .init(itemId: c.itemID), query: .init(at: c.at)) {
            case .noContent: return (204, nil)
            case .unauthorized: return (401, nil)
            case .notFound: return (404, nil)
            case let .undocumented(code, _): return (code, nil)
            }
        case .comment:
            if let text = c.comment {
                switch try await client.setReview(path: .init(itemId: c.itemID), body: .json(.init(comment: text, at: c.at))) {
                case .noContent: return (204, nil)
                case let .badRequest(r): return (400, try? r.body.json.message)
                case .unauthorized: return (401, nil)
                case .notFound: return (404, nil)
                case let .undocumented(code, _): return (code, nil)
                }
            }
            switch try await client.deleteReview(path: .init(itemId: c.itemID, userId: c.reviewUser ?? c.user ?? 0), query: .init(at: c.at)) {
            case .noContent: return (204, nil)
            case .unauthorized: return (401, nil)
            case .forbidden: return (403, "Only an administrator can remove someone else's comment.")
            case .notFound: return (404, nil)
            case let .undocumented(code, _): return (code, nil)
            }
        case .progress:
            switch try await client.syncProgress(path: .init(itemId: c.itemID),
                                                 body: .json(.init(positionMs: c.positionMs ?? 0, watched: c.watched ?? false, playedAt: c.at))) {
            case .noContent: return (204, nil)
            case .unauthorized: return (401, nil)
            case .notFound: return (404, nil)
            case let .undocumented(code, _): return (code, nil)
            }
        }
    }

    /// Errors that mean the server couldn't be reached (as opposed to an answer from it).
    nonisolated static func isNetworkFailure(_ error: Error) -> Bool {
        if let ce = error as? ClientError { return isNetworkFailure(ce.underlyingError) }
        return Downloads.isNetworkError(error)
    }
}
