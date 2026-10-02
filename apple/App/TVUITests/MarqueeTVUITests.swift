import XCTest

/// Apple TV flows: find the server with Bonjour, sign in with Quick Connect (approved
/// through the API, as a phone would), then browse and play.
final class MarqueeTVUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }

    override func setUp() {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-marquee-reset"]
        app.launch()
    }

    private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    /// Moves focus with the remote until the element has it (searching up, then down).
    private func focus(_ element: XCUIElement, direction: XCUIRemote.Button = .down, tries: Int = 12) {
        let order: [XCUIRemote.Button] = direction == .down ? Array(repeating: .up, count: 8) + Array(repeating: .down, count: tries + 8) : Array(repeating: direction, count: tries)
        for d in order {
            if element.hasFocus { return }
            remote.press(d)
        }
        XCTAssertTrue(element.hasFocus, "couldn't focus \(element)")
    }

    private func approve(code: String) throws {
        guard let token = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: server + "/api/v1/auth/quickconnect/authorize")!)
        req.httpMethod = "POST"
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code])
        let done = expectation(description: "approve")
        var status = 0
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            status = (resp as? HTTPURLResponse)?.statusCode ?? 0
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        XCTAssertEqual(status, 200)
    }

    func testQuickConnectBrowseAndPlay() throws {
        // Bonjour finds the server.
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        XCTAssertTrue(found.waitForExistence(timeout: 20))
        shot("tv-01-connect")
        focus(found)
        remote.press(.select)

        // Quick Connect.
        let qc = app.buttons["Use Quick Connect"]
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        shot("tv-02-who-is-watching")
        focus(qc)
        remote.press(.select)
        let codeText = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        shot("tv-03-quick-connect")
        try approve(code: codeText.label.replacingOccurrences(of: " ", with: ""))

        // Signed in: Home.
        XCTAssertTrue(app.staticTexts["Continue Watching"].waitForExistence(timeout: 15) || app.staticTexts["Recently Added Movies"].waitForExistence(timeout: 5))
        sleep(2)
        shot("tv-04-home")

        // Open the first poster (below the row title) and play it.
        remote.press(.down)
        remote.press(.select)
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume', 'Continue'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        sleep(1)
        shot("tv-05-detail")
        focus(play, direction: .down)
        remote.press(.select)
        sleep(6)
        shot("tv-06-playing")
        remote.press(.menu)
    }
}
