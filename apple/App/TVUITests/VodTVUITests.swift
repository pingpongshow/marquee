import XCTest

/// Movies on demand ("VOD", LIVE-6) on Apple TV with the remote: the VOD tab after Live TV,
/// the film's page, playing, resuming and marking watched; hidden when not usable.
/// Needs the local VOD source the iOS VodUITests describe (.e2e/vod-apple on :32621).
final class VodTVUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }
    private let film = "Apple Test Alpha (1985)"

    override func setUp() {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-marquee-reset"]
    }

    private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }

    private var focused: XCUIElement { app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true")) }

    private func focus(_ element: XCUIElement, direction: XCUIRemote.Button, tries: Int = 10) {
        for _ in 0..<tries where !element.hasFocus { remote.press(direction) }
        XCTAssertTrue(element.hasFocus, "couldn't focus \(element); focus is on \(focused.label)")
    }

    // MARK: - Server

    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> [String: Any] {
        guard let admin = adminToken, !admin.isEmpty else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
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
        wait(for: [done], timeout: 60)
        return out
    }

    /// VOD on with the local test source; the film's id.
    private func ensureVod() throws -> Int {
        let settings = try api("GET", "/settings")
        var sources = (settings["integrations"] as? [String: Any])?["vodSources"] as? [[String: Any]] ?? []
        if !sources.contains(where: { $0["id"] as? String == "apple-e2e" }) {
            sources.append(["id": "apple-e2e", "name": "Apple e2e films", "url": "http://127.0.0.1:32621/films.m3u", "enabled": true])
        }
        try api("PATCH", "/settings", ["integrations": ["vodEnabled": true, "vodSources": sources]])
        func id() throws -> Int? {
            let page = try api("GET", "/livetv/vod/movies?q=Apple%20Test%20Alpha")
            return (page["movies"] as? [[String: Any]] ?? []).first { $0["title"] as? String == film }?["id"] as? Int
        }
        if try id() == nil { try api("POST", "/livetv/vod/refresh") }
        return try XCTUnwrap(try id(), "the local VOD source loaded")
    }

    private func movie(_ id: Int, _ token: String) throws -> [String: Any] {
        try api("GET", "/livetv/vod/movies/\(id)", token: token)
    }

    private func eventually(_ timeout: TimeInterval, _ what: String, _ check: () throws -> Bool) rethrows {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if try check() { return }
            Thread.sleep(forTimeInterval: 1)
        }
        XCTFail("timed out: \(what)")
    }

    private static func send(_ method: String, _ url: String, token: String?) {
        guard let token, let u = URL(string: url) else { return }
        var req = URLRequest(url: u)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { _, _, _ in done.signal() }.resume()
        _ = done.wait(timeout: .now() + 15)
    }

    /// A temporary account (deleted when the test ends); its token.
    private func temporaryAccount() throws -> String {
        let name = "uitvvod-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass])
        let id = try XCTUnwrap(user["id"] as? Int)
        let login = try api("POST", "/auth/login", ["username": name, "password": pass, "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "tvos"]])
        let url = server + "/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        return try XCTUnwrap(login["token"] as? String)
    }

    /// Connects (Bonjour, or the typed address) and signs in with Quick Connect, approved with
    /// the token (as the main TV tests do).
    private func signIn(as token: String) throws {
        app.launch()
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        if found.waitForExistence(timeout: 20) {
            remote.press(.up)
            for _ in 0..<4 { remote.press(.left) }
            focus(found, direction: .right, tries: 6)
            remote.press(.select)
        } else {
            let enter = app.buttons["Enter an address"]
            for _ in 0..<12 where !enter.hasFocus { remote.press(.down) }
            remote.press(.select)
            let field = app.textFields["Home address, e.g. 192.168.1.20:32500"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            for _ in 0..<8 where !field.hasFocus { remote.press(.up) }
            remote.press(.select)
            app.typeText(server.replacingOccurrences(of: "http://", with: "") + "\n")
        }
        let qc = app.buttons["Use Quick Connect"]
        let connect = app.buttons["Connect"]
        if !qc.waitForExistence(timeout: 8), connect.exists {
            for _ in 0..<10 where !connect.hasFocus { remote.press(.down) }
            remote.press(.select)
        }
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        for _ in 0..<8 where !qc.hasFocus { remote.press(.up) }
        for _ in 0..<16 where !qc.hasFocus { remote.press(.down) }
        remote.press(.select)
        let codeText = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        try api("POST", "/auth/quickconnect/authorize", ["code": codeText.label.replacingOccurrences(of: " ", with: "")], token: token)
        XCTAssertTrue(app.buttons["Home"].firstMatch.waitForExistence(timeout: 20))
    }

    // MARK: - Tests

    /// The VOD tab sits right after Live TV; a film in Continue Watching opens, resumes from
    /// its point with the remote, and Menu closes the player and records where it got to;
    /// then Mark watched.
    func testVodTabPlayResumeAndMarkWatchedTV() throws {
        let id = try ensureVod()
        let token = try temporaryAccount()
        // A position from earlier, so the film is in VOD's Continue Watching.
        let s = try api("POST", "/livetv/vod/movies/\(id)/play", token: token)
        let session = try XCTUnwrap(s["id"] as? String)
        try api("PATCH", "/livetv/vod/sessions/\(session)", ["positionMs": 20000, "durationMs": 50000], token: token)
        try api("DELETE", "/livetv/vod/sessions/\(session)", token: token)
        try signIn(as: token)

        remote.press(.up)
        let live = app.buttons["Live TV"].firstMatch
        let vod = app.buttons["VOD"].firstMatch
        XCTAssertTrue(vod.waitForExistence(timeout: 10), "VOD is a tab")
        XCTAssertTrue(live.exists)
        XCTAssertLessThan(live.frame.midX, vod.frame.midX, "VOD comes right after Live TV")
        focus(vod, direction: .right, tries: 12)
        remote.press(.select)
        let tile = app.buttons.matching(identifier: "vod.tile").matching(NSPredicate(format: "label == %@", film)).firstMatch
        XCTAssertTrue(tile.waitForExistence(timeout: 15), "the film is in Continue Watching")
        sleep(1)
        shot("tv-vod-browse")
        XCTAssertFalse(app.buttons["Guide"].exists, "VOD is its own page, not inside Live TV")
        XCTAssertFalse(app.staticTexts["VOD"].exists, "no page title faded over VOD")
        focus(tile, direction: .down)
        remote.press(.select)

        let play = app.buttons["vod.play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10), "the film's page opens")
        XCTAssertEqual(play.label, "Resume from 0:20")
        focus(play, direction: .up, tries: 4)
        shot("tv-vod-page")
        remote.press(.select)
        sleep(8)
        shot("tv-vod-playing")
        // Menu (once the transport bar is down) closes the player.
        // (The page stays in the tree under the player: wait for focus to come back to it.)
        let onPage = { ["vod.play", "vod.fromStart", "vod.watched"].contains(self.focused.identifier) }
        for _ in 0..<3 where !onPage() {
            remote.press(.menu)
            sleep(2)
        }
        XCTAssertTrue(onPage(), "back on the film's page; focus is on \(focused.label)")
        try eventually(10, "carried on from 0:20") { (try movie(id, token)["positionMs"] as? Int ?? 0) > 20000 }

        let watched = app.buttons["vod.watched"]
        focus(watched, direction: .right, tries: 4)
        remote.press(.select)
        try eventually(10, "watched on the server") { try movie(id, token)["watched"] as? Bool == true }
        try eventually(5, "the page shows it") { watched.label == "Watched" }
        shot("tv-vod-watched")
    }

    /// Live TV's page has no title faded over the guide (the top bar's tab names it), and no
    /// VOD tab inside it.
    func testLiveTVNoTitleTV() throws {
        _ = try ensureVod()
        let token = try temporaryAccount()
        try signIn(as: token)
        remote.press(.up)
        let live = app.buttons["Live TV"].firstMatch
        XCTAssertTrue(live.waitForExistence(timeout: 10))
        focus(live, direction: .right, tries: 12)
        remote.press(.select)
        XCTAssertTrue(app.buttons["Guide"].firstMatch.waitForExistence(timeout: 15), "Live TV opens")
        sleep(1)
        shot("tv-livetv")
        XCTAssertFalse(app.staticTexts["Live TV"].exists, "no Live TV title over the guide")
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label == 'VOD'")).count, 1, "VOD only in the top bar, not a Live TV tab")
    }

    /// Not usable for the profile: no VOD tab.
    func testVodHiddenWhenUnusableTV() throws {
        _ = try ensureVod()
        let token = try temporaryAccount()
        app.launchArguments += ["-marquee-test-vod-off"]
        try signIn(as: token)
        remote.press(.up)
        XCTAssertTrue(app.buttons["Live TV"].firstMatch.waitForExistence(timeout: 10))
        sleep(2)
        XCTAssertFalse(app.buttons["VOD"].exists, "no VOD tab")
    }
}
