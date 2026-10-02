import Foundation
import MarqueeAPI

public typealias HomeLayoutRow = Components.Schemas.HomeLayoutRow
public typealias Invite = Components.Schemas.Invite
public typealias UserRestrictions = Components.Schemas.UserRestrictions
public typealias CinemaSettings = Components.Schemas.CinemaSettings

/// Home rows (USER-12), cinema trailers (PLAY-18), sharing (USER-13) and the admin
/// settings these need.
@MainActor
public extension AppSession {
    private var personalAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    // MARK: - Home rows (USER-12)

    /// Every row Home can show, in the person's order, with hidden and pinned ones.
    func homeLayout() async throws -> [HomeLayoutRow] {
        try await personalAPI.getHomeLayout().ok.body.json.rows
    }

    /// Saves the order and what's hidden; pinned rows not in the list are unpinned. An empty
    /// list resets to the default.
    @discardableResult
    func saveHomeLayout(_ rows: [HomeLayoutRow]) async throws -> [HomeLayoutRow] {
        let body = rows.map { HomeLayoutRow(id: $0.id, hidden: $0.hidden ?? false) }
        switch try await personalAPI.setHomeLayout(body: .json(.init(rows: body))) {
        case .ok(let ok): return try ok.body.json.rows
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't save Home.")
        default: throw MarqueeError("Couldn't save Home.")
        }
    }

    /// The Home row id for a pinned collection or playlist.
    static func pinnedRowID(collection: Int64? = nil, playlist: Int64? = nil) -> String {
        if let collection { return "collection-\(collection)" }
        return "playlist-\(playlist ?? 0)"
    }

    /// Whether a collection or playlist is pinned to Home.
    func isPinned(_ rowID: String) async -> Bool {
        ((try? await homeLayout()) ?? []).contains { $0.id == rowID }
    }

    /// Pins a collection or playlist to Home (added at the end), or unpins it.
    func setPinned(_ rowID: String, _ on: Bool) async throws {
        var rows = try await homeLayout()
        if on {
            guard !rows.contains(where: { $0.id == rowID }) else { return }
            rows.append(HomeLayoutRow(id: rowID, hidden: false))
        } else {
            rows.removeAll { $0.id == rowID }
        }
        try await saveHomeLayout(rows)
    }

    // MARK: - Cinema trailers (PLAY-18)

    /// What plays before a movie started from the beginning: trailers, then the pre-roll.
    func prerolls(_ itemID: Int64) async -> [Item] {
        guard case let .ok(ok) = try? await personalAPI.listPrerolls(path: .init(itemId: itemID)) else { return [] }
        return (try? ok.body.json) ?? []
    }

    /// The person's "Play trailers before movies" choice (default on).
    var cinemaTrailersPreference: Bool { me?.preferences.cinemaTrailers ?? true }

    func setCinemaTrailers(_ on: Bool) async throws {
        // Preferences are replaced as a whole, so the others are sent back unchanged.
        var prefs = me?.preferences ?? .init()
        prefs.cinemaTrailers = on
        switch try await personalAPI.updateMe(body: .json(.init(preferences: prefs))) {
        case .ok: await refreshMe()
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't save.")
        default: throw MarqueeError("Couldn't save.")
        }
    }

    /// Server-wide cinema settings (admin).
    func cinemaSettings() async throws -> CinemaSettings {
        try await personalAPI.getSettings().ok.body.json.cinema ?? .init(trailers: 0)
    }

    /// Saves the trailer count (0–5) and the pre-roll (0 clears it).
    func saveCinemaSettings(trailers: Int, prerollItemID: Int64) async throws {
        switch try await personalAPI.updateSettings(body: .json(.init(cinema: .init(trailers: trailers, prerollItemId: prerollItemID)))) {
        case .ok: return
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't save the settings.")
        case .forbidden: throw MarqueeError("Only admins can change this.")
        default: throw MarqueeError("Couldn't save the settings.")
        }
    }

    // MARK: - Users and sharing (USER-13)

    func users() async throws -> [User] { try await personalAPI.listUsers().ok.body.json }

    func invites() async throws -> [Invite] { try await personalAPI.listInvites().ok.body.json }

    /// Creates an invite; returns it with the link to send (the address this app uses + path).
    func createInvite(note: String, expiresDays: Int, restrictions: UserRestrictions) async throws -> (invite: Invite, link: URL?) {
        let body = Schemas.InviteCreate(note: note.isEmpty ? nil : note, expiresDays: expiresDays, restrictions: restrictions)
        switch try await personalAPI.createInvite(body: .json(body)) {
        case .created(let c):
            let made = try c.body.json
            return (made.invite, absoluteLink(made.path))
        case .badRequest(let r): throw MarqueeError((try? r.body.json.message) ?? "Couldn't create the invite.")
        case .forbidden: throw MarqueeError("Only admins can invite friends.")
        default: throw MarqueeError("Couldn't create the invite.")
        }
    }

    func deleteInvite(_ id: Int64) async throws {
        switch try await personalAPI.deleteInvite(path: .init(inviteId: id)) {
        case .noContent, .notFound: return
        default: throw MarqueeError("Couldn't delete the invite.")
        }
    }

    /// A page on the server (the join link), at the address this app is using.
    private func absoluteLink(_ path: String) -> URL? {
        guard let baseURL else { return nil }
        return URL(string: path, relativeTo: baseURL)?.absoluteURL
    }
}

public extension Components.Schemas.Invite {
    enum Status: Equatable { case pending, used(String), expired }

    var status: Status {
        if usedAt != nil { return .used(usedBy ?? "someone") }
        if let e = expiresAt, e < Date() { return .expired }
        return .pending
    }
}
