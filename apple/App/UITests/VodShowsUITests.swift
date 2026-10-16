import XCTest

/// VOD Movies and VOD Shows (LIVE-6) against the local test server: the two entries, the shows
/// grid, the filters, a show's page (seasons, episodes, watched) and an episode playing into
/// Up Next.
///
/// Needs a local fake Xtream Codes server on 127.0.0.1:32622 with the shows "Apple Test Show"
/// (Drama, 2010; seasons 1-2, five 12-second episodes) and "Apple Other Show" (Comedy, 1985),
/// and the films "Apple XC Film One" (1994) and "Apple XC Film Two" (2003). The tests add it as
/// the "Apple e2e Xtream" source when it isn't there, keeping any others.
final class VodShowsUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }
    private static let sourceID = "apple-e2e-xtream"
    private let show = "Apple Test Show"

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
        req.timeoutInterval = 60
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
        wait(for: [done], timeout: 90)
        return out
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

    /// VOD on, with the fake Xtream source among the sources; the test show's id.
    @discardableResult
    private func ensureShows() throws -> Int {
        if try showID() == nil {
            let settings = try api("GET", "/settings") as? [String: Any] ?? [:]
            var sources = (settings["integrations"] as? [String: Any])?["vodSources"] as? [[String: Any]] ?? []
            if !sources.contains(where: { $0["id"] as? String == Self.sourceID }) {
                sources = sources.map { var s = $0; s["passwordSet"] = nil; return s }
                sources.append(["id": Self.sourceID, "name": "Apple e2e Xtream", "kind": "xtream", "url": "http://127.0.0.1:32622",
                                "username": "apple", "password": "apple-test", "enabled": true])
                try api("PATCH", "/settings", ["integrations": ["vodEnabled": true, "vodSources": sources]])
            }
            try api("POST", "/livetv/vod/refresh")
        }
        return try XCTUnwrap(try showID(), "the fake Xtream source loaded (is it served on :32622?)")
    }

    private func showID() throws -> Int? {
        let page = try api("GET", "/livetv/vod/series?q=Apple%20Test%20Show") as? [String: Any] ?? [:]
        return (page["series"] as? [[String: Any]] ?? []).first { $0["title"] as? String == show }?["id"] as? Int
    }

    private func series(_ id: Int, _ token: String) throws -> [String: Any] {
        try api("GET", "/livetv/vod/series/\(id)", token: token) as? [String: Any] ?? [:]
    }

    private func episodes(_ id: Int, _ token: String) throws -> [[String: Any]] {
        try series(id, token)["episodes"] as? [[String: Any]] ?? []
    }

    /// A temporary account (its watch state goes with it), deleted when the test ends.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "uivodshow-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest VOD Shows", "platform": "web"]]) as? [String: Any] ?? [:]
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

    private var moviesEntry: XCUIElement { app.buttons["libraries.vod"] }
    private var showsEntry: XCUIElement { app.buttons["libraries.vodShows"] }

    private func tile(_ title: String) -> XCUIElement {
        app.buttons.matching(identifier: "vod.showTile").matching(NSPredicate(format: "label == %@", title)).firstMatch
    }

    private func search(_ text: String) {
        let field = app.textFields["vod.search"]
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the page opens")
        field.tap()
        field.typeText(text)
    }

    /// Opens the filter menu and picks `option` under `group`.
    private func filter(_ group: String, _ option: String) {
        app.buttons["vod.filter"].tap()
        let g = app.buttons[group].firstMatch
        XCTAssertTrue(g.waitForExistence(timeout: 5), "the filter menu has \(group)")
        g.tap()
        let o = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", option)).firstMatch
        XCTAssertTrue(o.waitForExistence(timeout: 5), "\(group) offers \(option)")
        o.tap()
    }

    private func openShow() {
        openLibrariesTab()
        XCTAssertTrue(showsEntry.waitForExistence(timeout: 10), "VOD Shows is listed")
        showsEntry.tap()
        search(show)
        XCTAssertTrue(tile(show).waitForExistence(timeout: 10), "the show is found")
        tile(show).tap()
        XCTAssertTrue(app.buttons["vod.show.play"].waitForExistence(timeout: 30), "the show's page opens with its episodes")
    }

    // MARK: - Tests

    /// VOD Movies and VOD Shows, in that order after Live TV; VOD Shows opens a grid of shows.
    func testTwoEntriesAndShowsGrid() throws {
        try ensureShows()
        let user = try temporaryUser()
        signIn(user)
        openLibrariesTab()
        XCTAssertTrue(moviesEntry.waitForExistence(timeout: 10), "VOD Movies is listed")
        XCTAssertTrue(showsEntry.waitForExistence(timeout: 10), "VOD Shows is listed")
        XCTAssertTrue(moviesEntry.label.contains("VOD Movies"), moviesEntry.label)
        XCTAssertLessThan(moviesEntry.frame.midY, showsEntry.frame.midY, "VOD Shows comes after VOD Movies")
        let live = app.buttons["libraries.liveTV"]
        if live.exists { XCTAssertLessThan(live.frame.midY, moviesEntry.frame.midY, "after Live TV") }
        shot("1-entries")
        showsEntry.tap()
        XCTAssertTrue(app.navigationBars["VOD Shows"].waitForExistence(timeout: 10))
        search("Apple")
        XCTAssertTrue(tile(show).waitForExistence(timeout: 10))
        XCTAssertTrue(tile("Apple Other Show").exists)
        XCTAssertTrue(app.staticTexts["2 shows"].exists)
        shot("2-shows-grid")
    }

    /// Filters on both pages: picked from the filter menu, shown as chips, removable one at a time
    /// or all at once, and remembered per page.
    func testFilters() throws {
        try ensureShows()
        let user = try temporaryUser()
        signIn(user)
        openLibrariesTab()
        XCTAssertTrue(showsEntry.waitForExistence(timeout: 10))
        showsEntry.tap()
        search("Apple")
        XCTAssertTrue(tile("Apple Other Show").waitForExistence(timeout: 10))
        filter("Genre", "Drama (")
        let chip = app.buttons["Remove filter Drama"]
        XCTAssertTrue(chip.waitForExistence(timeout: 5), "the genre shows as a chip")
        XCTAssertTrue(tile("Apple Other Show").waitForNonExistence(timeout: 10), "the comedy is filtered out")
        XCTAssertTrue(tile(show).exists)
        filter("Decade", "2010s")
        XCTAssertTrue(app.buttons["Remove filter 2010s"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["1 show"].waitForExistence(timeout: 10))
        shot("1-shows-filtered")
        // Remembered: away and back.
        openLibrariesTab()
        showsEntry.tap()
        XCTAssertTrue(app.buttons["Remove filter Drama"].waitForExistence(timeout: 10), "kept for the page")
        app.buttons["vod.clearFilters"].tap()
        XCTAssertTrue(app.buttons["Remove filter Drama"].waitForNonExistence(timeout: 5), "Clear All")

        // VOD Movies has its own.
        openLibrariesTab()
        moviesEntry.tap()
        XCTAssertTrue(app.navigationBars["VOD Movies"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["vod.filterChip"].exists, "the shows' filters aren't the films'")
        search("Apple XC")
        let one = app.buttons.matching(identifier: "vod.tile").matching(NSPredicate(format: "label == 'Apple XC Film One'")).firstMatch
        let two = app.buttons.matching(identifier: "vod.tile").matching(NSPredicate(format: "label == 'Apple XC Film Two'")).firstMatch
        XCTAssertTrue(two.waitForExistence(timeout: 10))
        filter("Decade", "1990s")
        XCTAssertTrue(two.waitForNonExistence(timeout: 10), "2003 is filtered out")
        XCTAssertTrue(one.exists)
        filter("Rating", "Rated 7+")
        XCTAssertTrue(app.buttons["Remove filter Rated 7+"].waitForExistence(timeout: 5))
        XCTAssertTrue(one.waitForExistence(timeout: 10))
        shot("2-movies-filtered")
        app.buttons["Remove filter 1990s"].tap()
        app.buttons["Remove filter Rated 7+"].tap()
        XCTAssertTrue(two.waitForExistence(timeout: 10), "back once removed")
    }

    /// A show's page: seasons and episodes, watched by episode, season and show; the next
    /// episode follows.
    func testShowPageWatched() throws {
        let id = try ensureShows()
        let user = try temporaryUser()
        signIn(user)
        openShow()
        let play = app.buttons["vod.show.play"]
        XCTAssertEqual(play.label, "Start S1 · E1")
        // (The season picked last time on this device is kept: start from season 1.)
        app.buttons["vod.season"].tap()
        app.buttons["Season 1"].tap()
        try eventually(5, "season 1's episodes") { app.buttons.matching(identifier: "vod.episode").count == 3 }
        shot("1-show-page")
        // An episode watched: the next one is up.
        app.buttons.matching(identifier: "vod.episode.watched").element(boundBy: 0).tap()
        try eventually(10, "episode 1 watched on the server") { try episodes(id, user.token).first?["watched"] as? Bool == true }
        try eventually(10, "the next episode follows") { play.label == "Play S1 · E2" }
        // Season 2 from the picker.
        app.buttons["vod.season"].tap()
        app.buttons["Season 2"].tap()
        try eventually(5, "season 2's episodes") { app.buttons.matching(identifier: "vod.episode").count == 2 }
        app.buttons["vod.season.watched"].tap()
        try eventually(10, "season 2 watched") {
            try episodes(id, user.token).filter { $0["season"] as? Int == 2 }.allSatisfy { $0["watched"] as? Bool == true }
        }
        shot("2-season-watched")
        // The whole show, then unwatched again.
        let watched = app.buttons["vod.show.watched"]
        watched.tap()
        try eventually(10, "the show watched") { try series(id, user.token)["watched"] as? Bool == true }
        try eventually(10, "the page shows it") { watched.label == "Watched" }
        watched.tap()
        try eventually(10, "the show unwatched") { try series(id, user.token)["watchedCount"] as? Int == 0 }
        try eventually(10, "back to the start") { play.label == "Start S1 · E1" }
    }

    /// An episode plays, its end brings Up Next, and Play Now carries on with the next one; the
    /// first counts as watched and Continue Watching offers the show.
    func testEpisodePlaysIntoUpNext() throws {
        let id = try ensureShows()
        let user = try temporaryUser()
        signIn(user)
        openShow()
        app.buttons["vod.show.play"].tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 15), "the player opens")
        sleep(4)
        shot("1-episode-playing")
        let upNext = app.staticTexts["S1 · E2 · Second"]
        XCTAssertTrue(upNext.waitForExistence(timeout: 40), "Up Next at the end")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Up next in'")).firstMatch.exists, "with a countdown")
        shot("2-up-next")
        try eventually(10, "episode 1 counted watched") { try episodes(id, user.token).first?["watched"] as? Bool == true }
        app.buttons["Play now"].tap()
        XCTAssertTrue(app.staticTexts["S1 · E2 · Second"].waitForNonExistence(timeout: 10))
        sleep(4)
        shot("3-next-episode")
        // The next one plays through to its own Up Next; Cancel goes back to the show.
        XCTAssertTrue(app.staticTexts["S1 · E3 · Third"].waitForExistence(timeout: 40), "Up Next again after episode 2")
        try eventually(10, "episode 2 counted watched") { try episodes(id, user.token).dropFirst().first?["watched"] as? Bool == true }
        app.buttons["Cancel"].tap()
        let play = app.buttons["vod.show.play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10), "back on the show's page")
        try eventually(10, "the next episode is offered") { play.label == "Play S1 · E3" }
        shot("4-after")
        // Continue Watching on VOD Shows.
        openLibrariesTab()
        showsEntry.tap()
        let cont = app.descendants(matching: .any)["vod.continue"]
        XCTAssertTrue(cont.waitForExistence(timeout: 10), "Continue Watching shows the show")
        shot("5-continue")
    }
}
