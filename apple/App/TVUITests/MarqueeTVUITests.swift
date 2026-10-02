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

    private func approve(code: String, as userToken: String? = nil) throws {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        let token = userToken ?? admin
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
    private func signIn(as userToken: String? = nil) throws {
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
        try approve(code: codeText.label.replacingOccurrences(of: " ", with: ""), as: userToken)
        XCTAssertTrue(app.staticTexts["Continue Watching"].waitForExistence(timeout: 15) || app.staticTexts["Recently Added Movies"].waitForExistence(timeout: 5))
    }

    /// Stations and Now Playing with lyrics on the TV (M6.5).
    func testMusicStation() throws {
        try signIn()
        let musicTab = app.buttons["Music"].firstMatch
        XCTAssertTrue(musicTab.waitForExistence(timeout: 10))
        focus(musicTab, direction: .right)
        remote.press(.select)
        XCTAssertTrue(app.staticTexts["Muse"].waitForExistence(timeout: 10))
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

        // Open the first poster (below the row title) and play it. A Continue Watching tile
        // plays at once; anything else opens its page first.
        let before = try activeSessions()
        let poster = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Preview Test' OR label CONTAINS[c] 'Long Test'")).firstMatch
        if poster.waitForExistence(timeout: 5) { focus(poster) } else { remote.press(.down) }
        remote.press(.select)
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume', 'Continue'}")).firstMatch
        if play.waitForExistence(timeout: 8) {
            sleep(1)
            shot("tv-05-detail")
            focus(play, direction: .down)
            remote.press(.select)
        }
        sleep(6)
        shot("tv-06-playing")
        XCTAssertGreaterThan(try activeSessions(), before, "nothing started playing")
        remote.press(.menu)
    }

    /// How many playback sessions the server has (admin view).
    private func activeSessions() throws -> Int {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: server + "/api/v1/playback/sessions")!)
        req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
        let done = expectation(description: "sessions")
        var count = 0
        URLSession.shared.dataTask(with: req) { data, _, _ in
            count = ((try? JSONSerialization.jsonObject(with: data ?? Data())) as? [Any])?.count ?? 0
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return count
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

    // MARK: - Plex parity on the TV (USER-12, PLAY-17, PLAY-19)

    /// Calls the server; returns the JSON object.
    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> [String: Any] {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: server + "/api/v1" + path)!)
        req.httpMethod = method
        req.setValue("Bearer \(token ?? admin)", forHTTPHeaderField: "Authorization")
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let done = expectation(description: path)
        var out: [String: Any] = [:]
        URLSession.shared.dataTask(with: req) { data, _, _ in
            out = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any]) ?? [:]
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 30)
        return out
    }

    /// A temporary account (other clients' tests share the admin's Home layout and offsets),
    /// signed in on the TV with Quick Connect and deleted when the test ends.
    private func signInTemporaryUser() throws -> String {
        let name = "uitv-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass])
        let id = try XCTUnwrap(user["id"] as? Int)
        let login = try api("POST", "/auth/login", ["username": name, "password": pass, "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "tvos"]])
        let url = server + "/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let token = try XCTUnwrap(login["token"] as? String)
        try signIn(as: token)
        return token
    }

    /// Edit Home with the remote: hide a row with its button, then reset.
    func testEditHome() throws {
        let token = try signInTemporaryUser()
        // At the bottom of Home (rows load as they come into view).
        let edit = app.buttons["Edit Home"]
        for _ in 0..<20 where !(edit.exists && edit.hasFocus) { remote.press(.down) }
        XCTAssertTrue(edit.hasFocus)
        remote.press(.select)
        let hide = app.buttons["Hide Recently Added Movies"]
        XCTAssertTrue(hide.waitForExistence(timeout: 10))
        focus(hide)
        sleep(1)
        shot("tv-eh1-edit-home")
        remote.press(.select)
        XCTAssertTrue(app.buttons["Show Recently Added Movies"].waitForExistence(timeout: 5))
        let layout = try api("GET", "/me/home-layout", token: token)["rows"] as? [[String: Any]] ?? []
        XCTAssertEqual(layout.first { $0["id"] as? String == "recent-2" }?["hidden"] as? Bool, true)
        // Move that row down with its button (two along from Hide).
        let before = layout.firstIndex { $0["id"] as? String == "recent-2" } ?? -1
        remote.press(.right)
        remote.press(.right)
        XCTAssertTrue(app.buttons["Move Recently Added Movies down"].hasFocus)
        remote.press(.select)
        sleep(2)
        let moved = try api("GET", "/me/home-layout", token: token)["rows"] as? [[String: Any]] ?? []
        XCTAssertEqual(moved.firstIndex { $0["id"] as? String == "recent-2" }, before + 1, "moved down one place")
        shot("tv-eh2-changed")
        let reset = app.buttons["Reset to Default"]
        focus(reset)
        remote.press(.select)
        XCTAssertTrue(hide.waitForExistence(timeout: 5))
    }

    /// The player's transport bar: Speed and Subtitle/Audio Timing menus.
    func testPlayerMenus() throws {
        _ = try signInTemporaryUser()
        let movie = app.buttons["00 Long Test"].firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        for _ in 0..<4 where !movie.hasFocus && !app.buttons["00 Preview Test"].firstMatch.hasFocus { remote.press(.down) }
        focus(movie, direction: .right, tries: 4)
        remote.press(.select)
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        focus(play, direction: .down)
        remote.press(.select)
        sleep(8)
        // Up from the transport bar reaches the custom menu buttons.
        remote.press(.select)
        sleep(1)
        // Up from the scrubber to the custom menus: Speed, Subtitle Timing, Audio Timing. The
        // transport bar's buttons and menus aren't in the accessibility tree, so this is a smoke
        // test: the remote opens each menu and the screenshots show them.
        shot("tv-pm0-bar")
        remote.press(.up)
        sleep(1)
        remote.press(.select)
        sleep(1)
        shot("tv-pm1-speed-menu")
        remote.press(.menu)
        sleep(1)
        remote.press(.right)
        remote.press(.select)
        sleep(1)
        shot("tv-pm2-subtitle-timing-menu")
        remote.press(.menu)
        sleep(1)
        remote.press(.right)
        remote.press(.select)
        sleep(1)
        shot("tv-pm3-audio-timing-menu")
        remote.press(.menu)
        XCTAssertEqual(app.state, .runningForeground)
        remote.press(.menu)
        remote.press(.menu)
    }

    /// A request that doesn't need an expectation, for teardown blocks (which run even when a
    /// failure stops the test, unlike defer).
    private static func send(_ method: String, _ url: String, token: String?, _ body: [String: Any]? = nil) {
        guard let token, let u = URL(string: url) else { return }
        var req = URLRequest(url: u)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        }
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { _, _, _ in done.signal() }.resume()
        _ = done.wait(timeout: .now() + 15)
    }
}
