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

    /// Other servers on the network may be listed beside ours: go to the row of server
    /// cards, start at its left end and move right.
    private func focusServer(_ element: XCUIElement) {
        remote.press(.up)
        for _ in 0..<4 { remote.press(.left) }
        focus(element, direction: .right, tries: 6)
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

    /// Connects with Bonjour and signs in with Quick Connect, ending on Home.
    private func signIn() throws {
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        // A test server on this Mac shares the mDNS port with the system responder, so
        // Bonjour can miss it: fall back to typing its address.
        if found.waitForExistence(timeout: 20) {
            focusServer(found)
            remote.press(.select)
        } else {
            let enter = app.buttons["Enter an address"]
            focus(enter)
            remote.press(.select)
            let field = app.textFields["Home address, e.g. 10.1.1.10:32500"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            focus(field)
            remote.press(.select)
            app.typeText(server.replacingOccurrences(of: "http://", with: "") + "\n")
            let connect = app.buttons["Connect"]
            if !app.buttons["Use Quick Connect"].waitForExistence(timeout: 8), connect.exists {
                focus(connect)
                remote.press(.select)
            }
        }
        let qc = app.buttons["Use Quick Connect"]
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        focus(qc)
        remote.press(.select)
        let codeText = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        try approve(code: codeText.label.replacingOccurrences(of: " ", with: ""))
        XCTAssertTrue(app.staticTexts["Continue Watching"].waitForExistence(timeout: 15) || app.staticTexts["Recently Added Movies"].waitForExistence(timeout: 5))
    }

    /// Stations and Now Playing with lyrics on the TV (M6.5).
    func testMusicStation() throws {
        try signIn()
        let musicTab = app.buttons["Music"].firstMatch
        XCTAssertTrue(musicTab.waitForExistence(timeout: 10))
        focus(musicTab, direction: .right)
        remote.press(.select)
        XCTAssertTrue(app.staticTexts["Sonic Sage"].waitForExistence(timeout: 10))
        let radio = app.buttons["Library Radio"]
        XCTAssertTrue(radio.waitForExistence(timeout: 10))
        // Down to the stations row (focus lands wherever is closest), then left along it.
        let stations = ["Library Radio", "Favourites Radio", "Chill", "Energetic", "Focus", "Melancholy", "Party", "Romantic", "Dreamy", "Aggressive"]
        let focused = app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true"))
        for _ in 0..<8 where !stations.contains(where: { focused.label.hasPrefix($0) }) { remote.press(.down) }
        focus(radio, direction: .left)
        sleep(1)
        shot("tv-m1-discover")
        remote.press(.select)

        let nowPlaying = app.buttons["Now Playing"].firstMatch
        XCTAssertTrue(nowPlaying.waitForExistence(timeout: 20))
        for _ in 0..<6 where !nowPlaying.hasFocus { remote.press(.up) }
        focus(nowPlaying, direction: .right)
        remote.press(.select)
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 10))
        sleep(2)
        shot("tv-m2-now-playing")
        let lyrics = app.buttons["Lyrics"]
        if lyrics.waitForExistence(timeout: 5) {
            // Out of the queue (right half) to the player's controls, then down to the bottom row.
            let focused = app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true"))
            for _ in 0..<15 where !lyrics.hasFocus {
                let f = focused.frame
                if f.maxY < 200 { remote.press(.down) } // still in the tab bar
                else if f.minX > app.frame.midX || abs(f.midY - lyrics.frame.midY) < 10 { remote.press(.left) }
                else { remote.press(.down) }
            }
            XCTAssertTrue(lyrics.hasFocus)
            remote.press(.select)
            XCTAssertTrue(app.staticTexts["Lyrics"].waitForExistence(timeout: 5))
            sleep(2)
            shot("tv-m3-lyrics")
        }
    }

    func testQuickConnectBrowseAndPlay() throws {
        // Bonjour finds the server.
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        XCTAssertTrue(found.waitForExistence(timeout: 20))
        shot("tv-01-connect")
        focusServer(found)
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

    /// Top Shelf links (marquee://item/<id>) open the item's page.
    func testTopShelfLink() throws {
        try signIn()
        app.launchArguments = [] // opening the link relaunches the app: keep the sign-in
        app.open(URL(string: "marquee://item/359")!)
        let title = app.staticTexts["00 Preview Test"].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch.waitForExistence(timeout: 5))
        shot("tv-topshelf-link")
    }
}
