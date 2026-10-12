import XCTest

/// Movies on demand ("VOD", LIVE-6) against the local test server: the VOD entry and Live TV
/// tab follow the admin switch, Live TV with only VOD on, and browsing, playing, resuming and
/// marking a film watched.
///
/// Needs a local VOD source: `.e2e/vod-apple/films.m3u` served on 127.0.0.1:32621
/// (`python3 -m http.server 32621 --directory .e2e/vod-apple`), with three films called
/// "Apple Test Alpha/Bravo/Charlie". The tests add the source to the server's settings when
/// it isn't there, keeping any others.
final class VodUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }
    private static let sourceID = "apple-e2e"
    private static let sourceURL = "http://127.0.0.1:32621/films.m3u"
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

    // MARK: - Server helpers

    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> Any? {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
        req.httpMethod = method
        req.setValue("Bearer \(token ?? admin)", forHTTPHeaderField: "Authorization")
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let done = expectation(description: path)
        var out: Any?
        URLSession.shared.dataTask(with: req) { data, _, _ in
            out = data.flatMap { try? JSONSerialization.jsonObject(with: $0) }
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 60)
        return out
    }

    private static func send(_ method: String, _ url: String, token: String?, body: [String: Any]? = nil) {
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

    /// VOD on, with the local test source among the sources; the film's id.
    @discardableResult
    private func ensureVod() throws -> Int {
        let settings = try api("GET", "/settings") as? [String: Any] ?? [:]
        var sources = (settings["integrations"] as? [String: Any])?["vodSources"] as? [[String: Any]] ?? []
        let had = sources.contains { $0["id"] as? String == Self.sourceID }
        if !had {
            sources.append(["id": Self.sourceID, "name": "Apple e2e films", "url": Self.sourceURL, "enabled": true])
        }
        try api("PATCH", "/settings", ["integrations": ["vodEnabled": true, "vodSources": sources]])
        if try filmID() == nil { try api("POST", "/livetv/vod/refresh") }
        return try XCTUnwrap(try filmID(), "the local VOD source loaded (is .e2e/vod-apple served on :32621?)")
    }

    private func filmID() throws -> Int? {
        let page = try api("GET", "/livetv/vod/movies?q=Apple%20Test%20Alpha") as? [String: Any] ?? [:]
        return (page["movies"] as? [[String: Any]] ?? []).first { $0["title"] as? String == film }?["id"] as? Int
    }

    private func setVodEnabled(_ on: Bool) throws {
        try api("PATCH", "/settings", ["integrations": ["vodEnabled": on]])
    }

    private func movie(_ id: Int, _ token: String) throws -> [String: Any] {
        try api("GET", "/livetv/vod/movies/\(id)", token: token) as? [String: Any] ?? [:]
    }

    /// A temporary account (its watch state goes with it), deleted when the test ends.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "uivod-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest VOD", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    private func eventually(_ timeout: TimeInterval, _ what: String, _ check: () throws -> Bool) rethrows {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if try check() { return }
            Thread.sleep(forTimeInterval: 1)
        }
        XCTFail("timed out: \(what)")
    }

    // MARK: - App helpers

    private func signIn(_ user: (name: String, password: String, token: String)) {
        app.launch()
        let address = app.textFields["Home address, e.g. 192.168.1.20:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.tap()
        address.typeText(server)
        app.buttons["Connect"].tap()
        let other = app.buttons["Sign in with username and password"]
        XCTAssertTrue(other.waitForExistence(timeout: 15))
        other.tap()
        let field = app.textFields["Username"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(user.name)
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText(user.password)
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
    }

    private func openLibrariesTab() {
        app.buttons["Libraries"].firstMatch.tap()
        let back = app.navigationBars.buttons["Libraries"]
        if back.waitForExistence(timeout: 2) { back.tap() }
    }

    private var vodEntry: XCUIElement { app.buttons["libraries.vod"] }
    private var vodTab: XCUIElement { app.segmentedControls.buttons["VOD"] }

    /// Opens VOD from the Libraries list and finds the test film.
    private func openFilm() {
        openLibrariesTab()
        XCTAssertTrue(vodEntry.waitForExistence(timeout: 10), "VOD is listed")
        vodEntry.tap()
        let search = app.textFields["vod.search"]
        XCTAssertTrue(search.waitForExistence(timeout: 10), "the VOD tab opens")
        search.tap()
        search.typeText("Apple Test Alpha")
        let tile = app.buttons.matching(identifier: "vod.tile").matching(NSPredicate(format: "label == %@", film)).firstMatch
        XCTAssertTrue(tile.waitForExistence(timeout: 10), "the film is found")
        tile.tap()
        XCTAssertTrue(app.buttons["vod.play"].waitForExistence(timeout: 10), "the film's page opens")
    }

    // MARK: - Tests

    /// The VOD entry (right after Live TV) opens VOD on its own page, never as a tab inside
    /// Live TV; it shows while the admin switch is on and goes once it's off (checked again on
    /// returning to the app).
    func testVodEntryFollowsAdminSwitch() throws {
        try ensureVod()
        let admin = adminToken, url = "http://\(server)/api/v1/settings"
        addTeardownBlock { Self.send("PATCH", url, token: admin, body: ["integrations": ["vodEnabled": true]]) }
        let user = try temporaryUser()
        signIn(user)
        openLibrariesTab()
        let live = app.buttons["libraries.liveTV"]
        XCTAssertTrue(vodEntry.waitForExistence(timeout: 10), "VOD is listed when on")
        XCTAssertTrue(live.exists)
        XCTAssertLessThan(live.frame.midY, vodEntry.frame.midY, "VOD comes right after Live TV")
        vodEntry.tap()
        XCTAssertTrue(app.textFields["vod.search"].waitForExistence(timeout: 10), "VOD opens its own page")
        XCTAssertFalse(app.segmentedControls.buttons["Guide"].exists, "not inside Live TV")
        shot("1-vod-on")
        openLibrariesTab()
        live.tap()
        XCTAssertTrue(app.segmentedControls.buttons["Guide"].waitForExistence(timeout: 10))
        XCTAssertFalse(vodTab.exists, "Live TV has no VOD tab")

        try setVodEnabled(false)
        // Back in the app: checked again.
        XCUIDevice.shared.press(.home)
        sleep(1)
        app.activate()
        openLibrariesTab()
        XCTAssertTrue(live.waitForExistence(timeout: 10))
        XCTAssertTrue(vodEntry.waitForNonExistence(timeout: 10), "VOD isn't listed when off")
        shot("2-vod-off")
    }

    /// With no channels usable but VOD on, Live TV isn't listed and VOD still is.
    func testOnlyVodWithoutChannels() throws {
        try ensureVod()
        let user = try temporaryUser()
        app.launchArguments += ["-marquee-test-channels-off"]
        signIn(user)
        openLibrariesTab()
        XCTAssertTrue(vodEntry.waitForExistence(timeout: 10), "VOD is listed on its own")
        XCTAssertFalse(app.buttons["libraries.liveTV"].exists, "no Live TV without channels")
        vodEntry.tap()
        XCTAssertTrue(app.textFields["vod.search"].waitForExistence(timeout: 10))
        shot("vod-only")
    }

    /// VOD hidden when not usable for the profile (follows Live TV's permission).
    func testVodHiddenWhenUnusable() throws {
        try ensureVod()
        let user = try temporaryUser()
        app.launchArguments += ["-marquee-test-vod-off"]
        signIn(user)
        openLibrariesTab()
        XCTAssertTrue(app.buttons["libraries.liveTV"].waitForExistence(timeout: 10))
        sleep(1)
        XCTAssertFalse(vodEntry.exists)
        app.buttons["libraries.liveTV"].tap()
        XCTAssertTrue(app.segmentedControls.buttons["Guide"].waitForExistence(timeout: 10))
        XCTAssertFalse(vodTab.exists)
    }

    /// Browse to a film, play it, close: the position is recorded and the page offers Resume.
    func testBrowseOpenAndPlay() throws {
        let id = try ensureVod()
        let user = try temporaryUser()
        signIn(user)
        openFilm()
        XCTAssertEqual(app.buttons["vod.play"].label, "Play")
        shot("1-page")
        app.buttons["vod.play"].tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 10), "the player opens")
        sleep(8)
        shot("2-playing")
        app.closePlayer()
        try eventually(10, "the position is recorded") { (try movie(id, user.token)["positionMs"] as? Int ?? 0) >= 3000 }
        let resume = app.buttons["vod.play"]
        XCTAssertTrue(resume.waitForExistence(timeout: 10))
        try eventually(10, "the page offers Resume") { resume.label.hasPrefix("Resume from") }
        XCTAssertTrue(app.buttons["vod.fromStart"].exists, "and Play from Start")
        shot("3-resume")
    }

    /// A position reported earlier: the film resumes there.
    func testResumeAfterReportedPosition() throws {
        let id = try ensureVod()
        let user = try temporaryUser()
        let s = try api("POST", "/livetv/vod/movies/\(id)/play", token: user.token) as? [String: Any] ?? [:]
        let session = try XCTUnwrap(s["id"] as? String)
        try api("PATCH", "/livetv/vod/sessions/\(session)", ["positionMs": 20000, "durationMs": 50000], token: user.token)
        try api("DELETE", "/livetv/vod/sessions/\(session)", token: user.token)
        signIn(user)
        openFilm()
        let resume = app.buttons["vod.play"]
        XCTAssertEqual(resume.label, "Resume from 0:20")
        resume.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 10))
        sleep(6)
        app.closePlayer()
        // Carried on from 0:20, not from the start.
        try eventually(10, "resumed past the reported point") { (try movie(id, user.token)["positionMs"] as? Int ?? 0) > 20000 }
        shot("resumed")
    }

    /// Mark watched and unwatched from the film's page.
    func testMarkWatched() throws {
        let id = try ensureVod()
        let user = try temporaryUser()
        signIn(user)
        openFilm()
        let toggle = app.buttons["vod.watched"]
        XCTAssertEqual(toggle.label, "Mark watched")
        toggle.tap()
        try eventually(5, "the page shows it") { toggle.label == "Watched" }
        try eventually(10, "watched on the server") { try movie(id, user.token)["watched"] as? Bool == true }
        shot("watched")
        toggle.tap()
        try eventually(5, "the page shows it") { toggle.label == "Mark watched" }
        try eventually(10, "unwatched on the server") { try movie(id, user.token)["watched"] as? Bool == false }
    }
}
