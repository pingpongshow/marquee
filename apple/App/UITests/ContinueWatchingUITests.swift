import XCTest

/// Continue Watching with films and episodes on demand (LIVE-6, USER-12) against the local test
/// server: VOD tiles in Home's row among the library's, the View more tile and the Continue
/// Watching page (paged as it scrolls), playing an entry on demand where it stopped, and removal.
///
/// Needs the fake Xtream Codes server on 127.0.0.1:32622 that VodShowsUITests describes; the
/// tests add it as the "Apple e2e Xtream" source when it isn't there. Each test's watch state
/// belongs to a temporary account, deleted afterwards.
final class ContinueWatchingUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }
    private static let sourceID = "apple-e2e-xtream"

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

    private func filmID() throws -> Int? {
        let page = try api("GET", "/livetv/vod/movies?q=Apple%20XC%20Film%20One") as? [String: Any] ?? [:]
        return (page["movies"] as? [[String: Any]] ?? []).first { $0["title"] as? String == "Apple XC Film One" }?["id"] as? Int
    }

    private func showID() throws -> Int? {
        let page = try api("GET", "/livetv/vod/series?q=Apple%20Test%20Show") as? [String: Any] ?? [:]
        return (page["series"] as? [[String: Any]] ?? []).first { $0["title"] as? String == "Apple Test Show" }?["id"] as? Int
    }

    /// VOD on with the fake Xtream source among the sources: the film's and the show's ids.
    private func ensureSource() throws -> (film: Int, show: Int) {
        if try filmID() == nil || showID() == nil {
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
        return (try XCTUnwrap(try filmID(), "the fake Xtream source loaded (is it served on :32622?)"), try XCTUnwrap(try showID()))
    }

    /// A temporary account (its watch state goes with it), deleted when the test ends.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "uicontinue-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Continue", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    /// Plays a film or episode on demand for a moment, as a player would: stopped at `positionMs`.
    private func vodProgress(_ path: String, positionMs: Int, durationMs: Int, token: String) throws {
        let session = try XCTUnwrap((try api("POST", path, [:], token: token) as? [String: Any])?["id"] as? String, "\(path) started")
        try api("PATCH", "/livetv/vod/sessions/\(session)", ["positionMs": positionMs, "durationMs": durationMs], token: token)
        try api("DELETE", "/livetv/vod/sessions/\(session)", token: token)
    }

    /// `library` library movies part-way through (an hour ago and before), then the show's first
    /// episode and the film on demand (newest): the keys of the episode and film entries.
    @discardableResult
    private func makeProgress(_ token: String, library: Int) throws -> (episode: String, film: String, episodeID: Int) {
        let ids = try ensureSource()
        let page = try api("GET", "/libraries/2/items?type=movie&sort=title&limit=200", token: token) as? [String: Any] ?? [:]
        let movies = (page["items"] as? [[String: Any]] ?? []).filter { ($0["durationMs"] as? Int ?? 0) > 0 }.prefix(library)
        XCTAssertEqual(movies.count, library, "enough library movies")
        let iso = ISO8601DateFormatter()
        for (n, m) in movies.enumerated() {
            let id = try XCTUnwrap(m["id"] as? Int)
            let dur = m["durationMs"] as? Int ?? 2000
            try api("PUT", "/items/\(id)/progress", ["positionMs": max(1, dur * 2 / 5),
                                                    "playedAt": iso.string(from: Date().addingTimeInterval(-3600 - Double(n) * 60))], token: token)
        }
        // The show's episodes are fetched when it's first opened.
        let show = try api("GET", "/livetv/vod/series/\(ids.show)", token: token) as? [String: Any] ?? [:]
        let first = try XCTUnwrap((show["episodes"] as? [[String: Any]] ?? []).first { $0["season"] as? Int == 1 && $0["episode"] as? Int == 1 })
        let episodeID = try XCTUnwrap(first["id"] as? Int)
        // (Positions under 10 seconds aren't kept; the clip is 12 seconds long.)
        try vodProgress("/livetv/vod/episodes/\(episodeID)/play", positionMs: 10000, durationMs: 0, token: token)
        sleep(1) // newer than the episode
        try vodProgress("/livetv/vod/movies/\(ids.film)/play", positionMs: 10000, durationMs: 30000, token: token)
        return ("vodEpisode:\(episodeID)", "vodMovie:\(ids.film)", episodeID)
    }

    private func continueKeys(_ token: String) throws -> [String] {
        let page = try api("GET", "/hubs/continue-watching?limit=200", token: token) as? [String: Any] ?? [:]
        return (page["entries"] as? [[String: Any]] ?? []).compactMap { $0["key"] as? String }
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

    private func tile(_ key: String) -> XCUIElement { app.buttons["continue.\(key)"] }

    /// The row's title, a link to the page when there's more than the row holds.
    private var rowTitle: XCUIElement { app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Continue Watching'")).firstMatch }

    private func contextMenu(_ element: XCUIElement, _ entry: String) {
        element.press(forDuration: 1.2)
        let button = app.buttons[entry].firstMatch
        XCTAssertTrue(button.waitForExistence(timeout: 5), "\(entry) is in the menu")
        button.tap()
    }

    // MARK: - Tests

    /// The row mixes films and episodes on demand with library titles in the server's order and
    /// ends with View more; View more and the row's title open the page, which pages in the rest
    /// as it scrolls.
    func testRowViewMoreAndPage() throws {
        let user = try temporaryUser()
        let keys = try makeProgress(user.token, library: 55)
        let all = try continueKeys(user.token)
        XCTAssertEqual(all.count, 57)
        XCTAssertEqual(Array(all.prefix(2)), [keys.film, keys.episode], "on demand newest")
        signIn(user)

        let film = tile(keys.film), episode = tile(keys.episode)
        XCTAssertTrue(film.waitForExistence(timeout: 15), "the film on demand is in the row")
        XCTAssertTrue(episode.exists, "and the episode")
        XCTAssertTrue(film.label.contains("Apple XC Film One"), film.label)
        XCTAssertTrue(episode.label.contains("Apple Test Show") && episode.label.contains("S1 · E1"), episode.label)
        XCTAssertLessThan(film.frame.minX, episode.frame.minX, "in the server's order")
        shot("1-row")

        // View more at the row's end.
        let viewMore = app.buttons["continue.viewMore"]
        let rowY = film.frame.midY
        for _ in 0..<20 where !(viewMore.exists && viewMore.isHittable) {
            let from = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: app.frame.width - 20, dy: rowY))
            from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: -(app.frame.width - 60), dy: 0)))
        }
        XCTAssertTrue(viewMore.waitForExistence(timeout: 5), "View more ends the row")
        XCTAssertTrue(viewMore.label.contains("View more"))
        shot("2-view-more")
        viewMore.tap()
        XCTAssertTrue(app.navigationBars["Continue Watching"].waitForExistence(timeout: 10), "the page opens")
        XCTAssertTrue(tile(keys.film).waitForExistence(timeout: 10), "with the films on demand")
        shot("3-page")

        // Scrolling to the end loads the rest (the page takes 50 at a time).
        let lastKey = try XCTUnwrap(all.last)
        let lastID = String(lastKey.dropFirst("item:".count))
        let last = app.buttons["continue.\(lastID)"]
        for _ in 0..<60 where !last.exists { app.swipeUp(velocity: .fast) }
        XCTAssertTrue(last.waitForExistence(timeout: 10), "the oldest entry loads in")
        shot("4-page-end")

        // The row's title opens the page too.
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 10))
        app.swipeDown()
        let title = rowTitle
        XCTAssertTrue(title.waitForExistence(timeout: 10), "the row's title is a link")
        title.tap()
        XCTAssertTrue(app.navigationBars["Continue Watching"].waitForExistence(timeout: 10))
    }

    /// Tapping an episode on demand resumes it in the VOD player, which ends in Up Next; a film
    /// on demand plays too. Back on Home the row is fresh.
    func testPlayVodFromRow() throws {
        let user = try temporaryUser()
        let keys = try makeProgress(user.token, library: 2)
        signIn(user)
        let episode = tile(keys.episode)
        XCTAssertTrue(episode.waitForExistence(timeout: 15))
        XCTAssertFalse(app.buttons["continue.viewMore"].exists, "no View more when the row holds everything")
        episode.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 15), "the VOD player opens")
        XCTAssertTrue(app.staticTexts["Apple Test Show · S1 · E1"].waitForExistence(timeout: 5), "with the episode")
        sleep(3)
        shot("1-episode-playing")
        let upNext = app.buttons.matching(NSPredicate(format: "identifier == 'upNext.next' AND label CONTAINS 'Second'")).firstMatch
        XCTAssertTrue(upNext.waitForExistence(timeout: 40), "Up Next at its end")
        shot("2-up-next")
        app.buttons["upNext.cancel"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 10), "back on Home")
        // Watched now: it leaves the row, which reloads.
        try eventually(15, "the watched episode leaves the row") { !tile(keys.episode).exists }

        let film = tile(keys.film)
        XCTAssertTrue(film.waitForExistence(timeout: 10))
        film.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 20), "the film plays")
        XCTAssertTrue(app.staticTexts["Apple XC Film One"].waitForExistence(timeout: 5))
        sleep(3)
        shot("3-film-playing")
        // The player's controls hide while it plays: a tap brings them back.
        let close = app.buttons["Close player"]
        if !close.exists { app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap() }
        XCTAssertTrue(close.waitForExistence(timeout: 5))
        close.tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 10))
    }

    /// Remove from Continue Watching on a film on demand (Home's row) and an episode (the page),
    /// each with its own endpoint; the library's still works.
    func testRemoveVod() throws {
        let user = try temporaryUser()
        let keys = try makeProgress(user.token, library: 21)
        signIn(user)
        let film = tile(keys.film)
        XCTAssertTrue(film.waitForExistence(timeout: 15))
        contextMenu(film, "Remove from Continue Watching")
        XCTAssertTrue(film.waitForNonExistence(timeout: 10), "gone from the row")
        try eventually(10, "gone on the server") { try !continueKeys(user.token).contains(keys.film) }
        shot("1-film-removed")

        // On the page: the episode (its show goes).
        let title = rowTitle
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        title.tap()
        XCTAssertTrue(app.navigationBars["Continue Watching"].waitForExistence(timeout: 10))
        let episode = tile(keys.episode)
        XCTAssertTrue(episode.waitForExistence(timeout: 10))
        contextMenu(episode, "Remove from Continue Watching")
        XCTAssertTrue(episode.waitForNonExistence(timeout: 10), "gone from the page")
        try eventually(10, "gone on the server") { try !continueKeys(user.token).contains { $0.hasPrefix("vodEpisode:") } }
        shot("2-episode-removed")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        // 21 left: the row's 20 and View more; then a library one removed leaves 20, no View more.
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 10))
        let first = try XCTUnwrap(try continueKeys(user.token).first)
        let libraryTile = app.buttons["continue.\(first.dropFirst("item:".count))"]
        XCTAssertTrue(libraryTile.waitForExistence(timeout: 10))
        contextMenu(libraryTile, "Remove from Continue Watching")
        try eventually(10, "the library title gone on the server") { try !continueKeys(user.token).contains(first) }
        XCTAssertTrue(libraryTile.waitForNonExistence(timeout: 10))
    }

    /// The VOD Movies and VOD Shows pages' Continue Watching rows: a long press offers Remove from
    /// Continue Watching; the film goes, and the show goes (by its next episode), here and on the
    /// server, and the row with them (its only title).
    func testRemoveFromVodPagesRows() throws {
        let user = try temporaryUser()
        let keys = try makeProgress(user.token, library: 0)
        signIn(user)
        for (entry, identifier, title, key) in [("libraries.vod", "vod.tile", "Apple XC Film One", keys.film),
                                                ("libraries.vodShows", "vod.showTile", "Apple Test Show", "vodEpisode:")] {
            app.buttons["Libraries"].firstMatch.tap()
            let back = app.navigationBars.buttons["Libraries"]
            if back.waitForExistence(timeout: 2) { back.tap() }
            let link = app.buttons[entry]
            XCTAssertTrue(link.waitForExistence(timeout: 10), "\(entry) is listed")
            link.tap()
            let tiles = app.buttons.matching(identifier: identifier).matching(NSPredicate(format: "label == %@", title))
            // In the row (and in the grid below it, when it's loaded that far).
            try eventually(15, "\(title) in Continue Watching") { app.descendants(matching: .any)["vod.continue"].exists && tiles.count >= 1 }
            let count = tiles.count
            shot("vod-row-\(identifier)-before")
            let row = try XCTUnwrap(tiles.allElementsBoundByIndex.min { $0.frame.minY < $1.frame.minY })
            contextMenu(row, "Remove from Continue Watching")
            try eventually(10, "\(title) gone from the row") { tiles.count == count - 1 && !app.descendants(matching: .any)["vod.continue"].exists }
            try eventually(10, "gone on the server") { try !continueKeys(user.token).contains { $0.hasPrefix(key) } }
            shot("vod-row-\(identifier)-after")
        }
    }
}
