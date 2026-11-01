import HTTPTypes
import OpenAPIRuntime
import XCTest
@testable import MarqueeKit

/// Requests ending the last person's playback go with their token, though it has changed.
final class AuthMiddlewareTests: XCTestCase {
    private func sentToken(_ middleware: AuthMiddleware) async throws -> String? {
        let request = HTTPRequest(method: .post, scheme: "http", authority: "server", path: "/api/v1/playback/x/stop")
        // The fake server answers with the Authorization header it was sent.
        let (response, _) = try await middleware.intercept(request, body: nil, baseURL: URL(string: "http://server")!, operationID: "stopPlayback") { req, _, _ in
            var answer = HTTPResponse(status: .noContent)
            if let auth = req.headerFields[.authorization] { answer.headerFields[.authorization] = auth }
            return (answer, nil)
        }
        return response.headerFields[.authorization]
    }

    func testUsesTheCurrentToken() async throws {
        let seen = try await sentToken(AuthMiddleware(token: { "next" }))
        XCTAssertEqual(seen, "Bearer next")
    }

    func testTasksStartedWhileLeavingKeepTheLeavingToken() async throws {
        let middleware = AuthMiddleware(token: { "next" })
        // As MusicPlayer.stop() does inside onUserChange: a task started then, sent later.
        let task = AuthMiddleware.$leaving.withValue("leaving") {
            Task { try await self.sentToken(middleware) }
        }
        let seen = try await task.value
        XCTAssertEqual(seen, "Bearer leaving")
        let after = try await sentToken(middleware)
        XCTAssertEqual(after, "Bearer next", "only while leaving")
    }

    func testSignedOutSendsNoneAfterwards() async throws {
        let seen = try await sentToken(AuthMiddleware(token: { nil }))
        XCTAssertNil(seen)
    }
}
