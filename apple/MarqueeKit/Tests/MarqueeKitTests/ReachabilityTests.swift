import Foundation
import HTTPTypes
import OpenAPIRuntime
import Testing
@testable import MarqueeKit

/// When the app counts as offline, and how it comes back.
struct ServerReachabilityTests {
    @Test func aChecksFailureBeforeTheServerEverAnsweredIsOfflineAtOnce() {
        var r = ServerReachability()
        #expect(!r.isOffline)
        r.checked(reached: false)
        #expect(r.isOffline)
    }

    @Test func oneFailedCheckOfAWorkingConnectionIsNotOffline() {
        var r = ServerReachability()
        r.checked(reached: true)
        r.checked(reached: false)
        #expect(!r.isOffline)
        r.checked(reached: false)
        #expect(r.isOffline)
    }

    @Test func aCheckThatReachesTheServerResetsTheCount() {
        var r = ServerReachability()
        r.checked(reached: true)
        r.checked(reached: false)
        r.checked(reached: true)
        r.checked(reached: false)
        #expect(!r.isOffline)
    }

    @Test func comingBackIsAutomaticOnTheNextCheckThatReachesIt() {
        var r = ServerReachability()
        r.checked(reached: false)
        #expect(r.isOffline)
        r.checked(reached: true)
        #expect(!r.isOffline)
    }

    @Test func noNetworkIsOfflineAtOnceAndItReturningIsNotEnough() {
        var r = ServerReachability()
        r.checked(reached: true)
        r.networkChanged(satisfied: false)
        #expect(r.isOffline)
        r.networkChanged(satisfied: true)
        #expect(r.isOffline, "only reaching the server brings it back")
        r.checked(reached: true)
        #expect(!r.isOffline)
    }

    @Test func aFailedCheckWithNoNetworkNeedsNoSecondOne() {
        var r = ServerReachability()
        r.checked(reached: true)
        r.networkChanged(satisfied: false)
        r.checked(reached: true)
        #expect(!r.isOffline, "a server on this device still answers without a network")
        r.checked(reached: false)
        #expect(r.isOffline)
    }

    @Test func aFailedRequestOnlyAsksForACheck() {
        var r = ServerReachability()
        r.checked(reached: true)
        #expect(r.checkAfterRequestFailure)
        #expect(!r.isOffline)
        r.networkChanged(satisfied: false)
        #expect(!r.checkAfterRequestFailure, "already offline: checks are running anyway")
    }
}

/// Only requests the network failed count; any answer from the server means it was reached.
struct ReachabilityMiddlewareTests {
    private final class Counter: @unchecked Sendable {
        private let lock = NSLock()
        private var n = 0
        var value: Int { lock.lock(); defer { lock.unlock() }; return n }
        func add() { lock.lock(); n += 1; lock.unlock() }
    }

    private func send(_ m: ReachabilityMiddleware, _ answer: @escaping @Sendable () throws -> HTTPResponse) async throws {
        let request = HTTPRequest(method: .get, scheme: "http", authority: "server", path: "/api/v1/me")
        _ = try await m.intercept(request, body: nil, baseURL: URL(string: "http://server")!, operationID: "getMe") { _, _, _ in
            (try answer(), nil)
        }
    }

    @Test func networkErrorsCount() async {
        let count = Counter()
        let m = ReachabilityMiddleware { count.add() }
        await #expect(throws: URLError.self) { try await send(m) { throw URLError(.cannotConnectToHost) } }
        #expect(count.value == 1)
    }

    @Test func httpErrorsDoNot() async throws {
        let count = Counter()
        let m = ReachabilityMiddleware { count.add() }
        for status in [HTTPResponse.Status.ok, .unauthorized, .notFound, .internalServerError, .badGateway] {
            try await send(m) { HTTPResponse(status: status) }
        }
        #expect(count.value == 0)
    }

    @Test func otherErrorsDoNot() async {
        let count = Counter()
        let m = ReachabilityMiddleware { count.add() }
        await #expect(throws: URLError.self) { try await send(m) { throw URLError(.cancelled) } }
        await #expect(throws: MarqueeError.self) { try await send(m) { throw MarqueeError("no") } }
        #expect(count.value == 0)
    }
}
