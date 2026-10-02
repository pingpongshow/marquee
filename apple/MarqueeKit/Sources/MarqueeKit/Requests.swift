import Foundation
import MarqueeAPI

public typealias DiscoverItem = Components.Schemas.DiscoverItem
public typealias MediaRequest = Components.Schemas.MediaRequest
public typealias RequestsStatus = Components.Schemas.RequestsStatus
public typealias RequestableShow = Components.Schemas.RequestableShow
public typealias Availability = Components.Schemas.Availability
public typealias DiscoverCategory = Operations.DiscoverRequestable.Input.Query.CategoryPayload

/// Requests through Seerr (REQ-1, REQ-2): find titles that aren't here and ask for them;
/// admins approve each one.
@MainActor
public extension AppSession {
    private var requestsAPI: Client {
        get throws {
            guard let client else { throw MarqueeError("Not connected") }
            return client
        }
    }

    func requestsStatus() async throws -> RequestsStatus {
        try await requestsAPI.requestsStatus().ok.body.json
    }

    func searchRequestable(_ q: String, page: Int = 1) async throws -> [DiscoverItem] {
        switch try await requestsAPI.searchRequestable(query: .init(q: q, page: page)) {
        case let .ok(ok): return try ok.body.json.results
        case let .badGateway(e): throw MarqueeError((try? e.body.json.message) ?? "Seerr didn't answer")
        case let .serviceUnavailable(e): throw MarqueeError((try? e.body.json.message) ?? "Requests aren't set up")
        default: throw MarqueeError("Couldn't search")
        }
    }

    func discoverRequestable(_ category: DiscoverCategory, page: Int = 1) async throws -> [DiscoverItem] {
        switch try await requestsAPI.discoverRequestable(query: .init(category: category, page: page)) {
        case let .ok(ok): return try ok.body.json.results
        case let .badGateway(e): throw MarqueeError((try? e.body.json.message) ?? "Seerr didn't answer")
        case let .serviceUnavailable(e): throw MarqueeError((try? e.body.json.message) ?? "Requests aren't set up")
        default: throw MarqueeError("Couldn't load Discover")
        }
    }

    func requestableShow(_ tmdbID: Int64) async throws -> RequestableShow {
        try await requestsAPI.requestableShow(path: .init(tmdbId: tmdbID)).ok.body.json
    }

    /// Asks for a movie, or seasons of a show (empty = every season not here yet).
    func request(_ item: DiscoverItem, seasons: [Int] = []) async throws -> MediaRequest {
        let body = Components.Schemas.MediaRequestCreate(tmdbId: item.tmdbId, mediaType: item.mediaType == .tv ? .tv : .movie,
                                                         seasons: item.mediaType == .tv ? seasons : nil)
        switch try await requestsAPI.createRequest(body: .json(body)) {
        case let .created(c): return try c.body.json
        case let .conflict(e): throw MarqueeError((try? e.body.json.message) ?? "Already requested")
        case let .forbidden(e): throw MarqueeError((try? e.body.json.message) ?? "You can't request titles")
        case let .badRequest(e): throw MarqueeError((try? e.body.json.message) ?? "Couldn't request that")
        default: throw MarqueeError("Couldn't request that")
        }
    }

    /// The caller's requests, or everyone's (admins).
    func requests(all: Bool = false) async throws -> [MediaRequest] {
        try await requestsAPI.listRequests(query: .init(scope: all ? .all : .mine)).ok.body.json
    }

    func cancelRequest(_ id: Int64) async throws {
        _ = try await requestsAPI.cancelRequest(path: .init(requestId: id)).noContent
    }

    func approveRequest(_ id: Int64) async throws -> MediaRequest {
        try await requestsAPI.approveRequest(path: .init(requestId: id)).ok.body.json
    }

    func declineRequest(_ id: Int64, reason: String?) async throws -> MediaRequest {
        try await requestsAPI.declineRequest(path: .init(requestId: id), body: .json(.init(reason: reason))).ok.body.json
    }
}

public extension Components.Schemas.Availability {
    var label: String {
        switch self {
        case .none: ""
        case .pending: "Waiting for approval"
        case .requested: "Requested"
        case .processing: "On its way"
        case .partial: "Partly available"
        case .available: "In library"
        }
    }
}

public extension Components.Schemas.RequestState {
    var label: String {
        switch self {
        case .pending: "Waiting for approval"
        case .approved: "Approved"
        case .declined: "Declined"
        case .failed: "Failed"
        case .available: "Available"
        }
    }
}
