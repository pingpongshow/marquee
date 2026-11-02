import Foundation
import HTTPTypes
import OpenAPIRuntime

/// Whether the server can be reached, from what the app sees of the network and the server.
/// A value type, so it's testable on its own; AppSession publishes `isOffline` from it.
///
/// Offline when the network is gone, or when the address checks fail: at once before the
/// server has answered this launch, on the second check in a row after that, so one slow
/// answer doesn't take the library away. Only a check that reaches the server brings it back
/// (the network returning doesn't mean the server can be reached).
public struct ServerReachability: Equatable, Sendable {
    public private(set) var isOffline = false
    private var failedChecks = 0
    private var everReached = false
    private var networkUp = true

    /// Failed checks in a row that take a working connection offline.
    static let checksToGoOffline = 2

    public init() {}

    public mutating func networkChanged(satisfied: Bool) {
        networkUp = satisfied
        if !satisfied { isOffline = true }
    }

    /// The address checks finished: `reached` is whether any of the addresses answered.
    public mutating func checked(reached: Bool) {
        if reached {
            failedChecks = 0
            everReached = true
            isOffline = false
            return
        }
        failedChecks += 1
        if !everReached || !networkUp || failedChecks >= Self.checksToGoOffline { isOffline = true }
    }

    /// An API request failed for want of the network: never offline by itself, only worth
    /// checking the addresses (unless they're being checked anyway, while offline).
    public var checkAfterRequestFailure: Bool { !isOffline }
}

/// Tells the session when an API request fails for want of the network. Answers from the
/// server, whatever their status, don't count: it was reached.
struct ReachabilityMiddleware: ClientMiddleware {
    let networkFailed: @Sendable () -> Void

    func intercept(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String,
                   next: @Sendable (HTTPRequest, HTTPBody?, URL) async throws -> (HTTPResponse, HTTPBody?)) async throws -> (HTTPResponse, HTTPBody?) {
        do {
            return try await next(request, body, baseURL)
        } catch {
            if OfflineSync.isNetworkFailure(error) { networkFailed() }
            throw error
        }
    }
}
