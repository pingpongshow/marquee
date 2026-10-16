import XCTest

/// Continue Watching with films and episodes on demand on Apple TV (LIVE-6, USER-12): VOD tiles
/// in Home's row, View more at its end opening the focusable Continue Watching grid, an entry on
/// demand resuming in the VOD player, and Remove from Continue Watching on a long press.
/// Needs the fake Xtream Codes server on 127.0.0.1:32622 (see the iOS ContinueWatchingUITests),
/// added as the "Apple e2e Xtream" source when it isn't there.
final class ContinueWatchingTVUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }
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
        let name = "uitvcontinue-\(Int.random(in: 1000...9999))"
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

    /// VOD sits right after Live TV in the sidebar; a film in Continue Watching opens, resumes from
    /// its point with the remote, and Menu closes the player and records where it got to;
    /// then Mark watched.

    private func filmID() throws -> Int? {
        let page = try api("GET", "/livetv/vod/movies?q=Apple%20XC%20Film%20One")
        return (page["movies"] as? [[String: Any]] ?? []).first { $0["title"] as? String == "Apple XC Film One" }?["id"] as? Int
    }

    private func showID() throws -> Int? {
        let page = try api("GET", "/livetv/vod/series?q=Apple%20Test%20Show")
        return (page["series"] as? [[String: Any]] ?? []).first { $0["title"] as? String == "Apple Test Show" }?["id"] as? Int
    }

    /// VOD on with the fake Xtream source: the film's and the show's ids.
    private func ensureSource() throws -> (film: Int, show: Int) {
        if try filmID() == nil || showID() == nil {
            let settings = try api("GET", "/settings")
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

    private func vodProgress(_ path: String, positionMs: Int, token: String) throws {
        let session = try XCTUnwrap(try api("POST", path, [:], token: token)["id"] as? String, "\(path) started")
        try api("PATCH", "/livetv/vod/sessions/\(session)", ["positionMs": positionMs, "durationMs": 0], token: token)
        try api("DELETE", "/livetv/vod/sessions/\(session)", token: token)
    }

    /// `library` library movies part-way through (an hour ago and before), then the show's first
    /// episode and the film on demand (newest): their entry keys.
    private func makeProgress(_ token: String, library: Int) throws -> (episode: String, film: String) {
        let ids = try ensureSource()
        let page = try api("GET", "/libraries/2/items?type=movie&sort=title&limit=200", token: token)
        let movies = (page["items"] as? [[String: Any]] ?? []).filter { ($0["durationMs"] as? Int ?? 0) > 0 }.prefix(library)
        let iso = ISO8601DateFormatter()
        for (n, m) in movies.enumerated() {
            let dur = m["durationMs"] as? Int ?? 2000
            try api("PUT", "/items/\(try XCTUnwrap(m["id"] as? Int))/progress",
                    ["positionMs": max(1, dur * 2 / 5), "playedAt": iso.string(from: Date().addingTimeInterval(-3600 - Double(n) * 60))], token: token)
        }
        let show = try api("GET", "/livetv/vod/series/\(ids.show)", token: token)
        let first = try XCTUnwrap((show["episodes"] as? [[String: Any]] ?? []).first { $0["season"] as? Int == 1 && $0["episode"] as? Int == 1 })
        let episode = try XCTUnwrap(first["id"] as? Int)
        try vodProgress("/livetv/vod/episodes/\(episode)/play", positionMs: 10000, token: token) // (under 10 s isn't kept)
        sleep(1)
        try vodProgress("/livetv/vod/movies/\(ids.film)/play", positionMs: 10000, token: token)
        return ("vodEpisode:\(episode)", "vodMovie:\(ids.film)")
    }

    private func continueKeys(_ token: String) throws -> [String] {
        let page = try api("GET", "/hubs/continue-watching?limit=200", token: token)
        return (page["entries"] as? [[String: Any]] ?? []).compactMap { $0["key"] as? String }
    }

    // MARK: - Tests

    /// The row with the entries on demand first and View more at its end; View more opens the
    /// grid, where focus moves through the tiles; the film resumes in the VOD player; a long press
    /// removes the episode.
    func testRowViewMorePlayAndRemoveTV() throws {
        let token = try temporaryAccount()
        let keys = try makeProgress(token, library: 21)
        try signIn(as: token)
        let film = app.buttons["continue.\(keys.film)"], episode = app.buttons["continue.\(keys.episode)"]
        XCTAssertTrue(film.waitForExistence(timeout: 15), "the film on demand is in the row")
        XCTAssertTrue(episode.exists)
        for _ in 0..<6 where !film.hasFocus { remote.press(.down) }
        for _ in 0..<6 where !film.hasFocus { remote.press(.left) }
        XCTAssertTrue(film.hasFocus, "focus is on \(focused.label)")
        shot("tv-1-row")

        // View more, at the row's end.
        let viewMore = app.buttons["continue.viewMore"]
        // (Lazily made: it exists once the row scrolls near it.)
        for _ in 0..<24 where !(viewMore.exists && viewMore.hasFocus) { remote.press(.right) }
        XCTAssertTrue(viewMore.exists && viewMore.hasFocus, "View more ends the row; focus is on \(focused.label)")
        XCTAssertTrue(viewMore.label.contains("View more"))
        shot("tv-2-view-more")
        remote.press(.select)
        let grid = app.descendants(matching: .any)["continue.grid"]
        XCTAssertTrue(grid.waitForExistence(timeout: 10), "the Continue Watching page opens")
        try eventually(10, "focus in the grid") { focused.identifier.hasPrefix("continue.") }
        let before = focused.identifier
        remote.press(.down)
        try eventually(5, "focus moves down the grid") { focused.identifier != before && focused.identifier.hasPrefix("continue.") }
        shot("tv-3-page")
        remote.press(.menu)
        XCTAssertTrue(grid.waitForNonExistence(timeout: 10), "back on Home")

        // The film resumes in the VOD player.
        for _ in 0..<24 where !(film.exists && film.hasFocus) { remote.press(.left) }
        XCTAssertTrue(film.hasFocus, "focus is on \(focused.label)")
        remote.press(.select)
        XCTAssertTrue(app.descendants(matching: .any)["vod.player"].waitForExistence(timeout: 20), "the VOD player opens")
        sleep(4)
        shot("tv-4-film-playing")
        remote.press(.menu)
        XCTAssertTrue(film.waitForExistence(timeout: 15), "back on Home")

        // Remove the episode with a long press.
        for _ in 0..<6 where !film.hasFocus && !episode.hasFocus { remote.press(.down) }
        focus(episode, direction: .right, tries: 4)
        remote.press(.select, forDuration: 1.5)
        let remove = app.descendants(matching: .any)["removeFromContinue"]
        XCTAssertTrue(remove.waitForExistence(timeout: 5), "the context menu offers it")
        shot("tv-5-menu")
        remote.press(.select)
        XCTAssertTrue(episode.waitForNonExistence(timeout: 10), "the tile goes")
        try eventually(10, "gone on the server") { try !continueKeys(token).contains(keys.episode) }
    }

    /// The VOD Movies and VOD Shows pages' Continue Watching rows: holding Select on the row's
    /// tile offers Remove from Continue Watching; the film goes, and the show goes (by its next
    /// episode), here and on the server, and the row with them (its only title).
    func testRemoveFromVodPagesRowsTV() throws {
        let token = try temporaryAccount()
        let keys = try makeProgress(token, library: 0)
        try signIn(as: token)
        for (section, identifier, title, key) in [("VOD Movies", "vod.tile", "Apple XC Film One", keys.film),
                                                  ("VOD Shows", "vod.showTile", "Apple Test Show", "vodEpisode:")] {
            app.openSection(section)
            let tiles = app.buttons.matching(identifier: identifier).matching(NSPredicate(format: "label == %@", title))
            try eventually(15, "\(title) in Continue Watching") { app.descendants(matching: .any)["vod.continue"].exists && tiles.count >= 1 }
            let count = tiles.count
            // Down from the controls reaches the row first.
            for _ in 0..<8 where focused.identifier != identifier { remote.press(.down) }
            for _ in 0..<4 where focused.label != title { remote.press(.left) }
            let row = try XCTUnwrap(tiles.allElementsBoundByIndex.min { $0.frame.minY < $1.frame.minY })
            XCTAssertTrue(row.hasFocus, "the row's tile has focus; focus is on \(focused.identifier) \(focused.label)")
            shot("tv-vod-row-\(identifier)-before")
            remote.press(.select, forDuration: 1.5)
            let remove = app.descendants(matching: .any)["removeFromContinue"]
            XCTAssertTrue(remove.waitForExistence(timeout: 5), "the context menu offers it")
            shot("tv-vod-row-\(identifier)-menu")
            remote.press(.select)
            try eventually(10, "\(title) gone from the row") { tiles.count == count - 1 && !app.descendants(matching: .any)["vod.continue"].exists }
            try eventually(10, "gone on the server") { try !continueKeys(token).contains { $0.hasPrefix(key) } }
            shot("tv-vod-row-\(identifier)-after")
        }
    }
}
