import XCTest

/// Suggestions on demand on Apple TV (LIVE-6, USER-16): Home's Watch on VOD row (a title opens
/// its page; Not interested from the long-press menu takes it away) and Also on VOD on a
/// movie's page. Needs the seed data the iOS VodSuggestionsUITests describe; skips without it.
final class VodSuggestionsTVUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }
    private var seedItem: Int { ProcessInfo.processInfo.environment["MARQUEE_TEST_VOD_SEED_ITEM"].flatMap(Int.init) ?? 8 }

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
        let name = "uitvvodsug-\(Int.random(in: 1000...9999))"
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
        // (MARQUEE_TEST_SERVER_NAME picks the test server out when another one is on the network.)
        let name = ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER_NAME"].flatMap { $0.isEmpty ? nil : $0 } ?? "E2E"
        let found = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name + ",")).firstMatch
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
            app.typeText(server.replacingOccurrences(of: "http://", with: ""))
            // The keyboard's Next (then Done on the optional Tailscale address, left empty),
            // with the remote: a typed return isn't always taken.
            for _ in 0..<2 {
                sleep(1)
                guard app.keyboards.firstMatch.exists else { break }
                for _ in 0..<5 { remote.press(.down) }
                remote.press(.select)
            }
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

    /// The temporary account's suggestions (titles), after making the seed movie a favourite.
    private func seed(_ token: String) throws -> [String] {
        try api("PUT", "/items/\(seedItem)/favorite", token: token)
        let req = URLRequest(url: URL(string: server + "/api/v1/livetv/vod/suggestions?kind=movie")!)
        return try titles(req, token)
    }

    private func titles(_ base: URLRequest, _ token: String) throws -> [String] {
        var req = base
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let done = expectation(description: "list")
        var out: [String] = []
        URLSession.shared.dataTask(with: req) { data, _, _ in
            let list = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [[String: Any]]) ?? []
            out = list.compactMap { ($0["movie"] as? [String: Any])?["title"] as? String }
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 30)
        return out
    }

    private func tile(_ title: String) -> XCUIElement {
        app.buttons.matching(identifier: "vod.suggestion").matching(NSPredicate(format: "label == %@", title)).firstMatch
    }

    // MARK: - Tests

    /// Watch on VOD on Home: a title opens its film's page; holding Select offers Not
    /// interested, which takes it off the row and off the server's suggestions.
    func testHomeWatchOnVodRowTV() throws {
        let token = try temporaryAccount()
        guard let first = try seed(token).first else { throw XCTSkip("no VOD suggestions (seed data missing)") }
        try signIn(as: token)
        let t = tile(first)
        XCTAssertTrue(t.waitForExistence(timeout: 15), "Watch on VOD is on Home")
        XCTAssertTrue(app.staticTexts["Watch on VOD"].exists)
        for _ in 0..<6 where !t.hasFocus && focused.frame.minX < 100 { remote.press(.right) }
        focus(t, direction: .down, tries: 4)
        for _ in 0..<4 where !t.hasFocus { remote.press(.left) }
        XCTAssertTrue(t.hasFocus, "focus is on \(focused.label)")
        sleep(1)
        shot("tv-home-watch-on-vod")
        remote.press(.select)
        let play = app.buttons["vod.play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10), "the film's page opens")
        shot("tv-vod-film-from-home")
        remote.press(.menu)
        XCTAssertTrue(t.waitForExistence(timeout: 10), "back on Home")
        for _ in 0..<6 where !t.hasFocus { Thread.sleep(forTimeInterval: 0.5) }
        XCTAssertTrue(t.hasFocus, "focus back on the tile; it's on \(focused.label)")

        remote.press(.select, forDuration: 1.5)
        let hide = app.descendants(matching: .any)["vod.notInterested"]
        XCTAssertTrue(hide.waitForExistence(timeout: 5), "the context menu offers Not interested")
        XCTAssertTrue(app.descendants(matching: .any)["Go to film"].exists)
        shot("tv-home-watch-on-vod-menu")
        remote.press(.select)
        XCTAssertTrue(t.waitForNonExistence(timeout: 10), "the tile goes")
        try eventually(10, "not suggested any more") { try !seed(token).contains(first) }
        sleep(1)
        shot("tv-home-watch-on-vod-hidden")
    }

    /// Also on VOD on the seed movie's page; a title there opens its film's page.
    func testAlsoOnVodOnMoviePageTV() throws {
        let token = try temporaryAccount()
        _ = try seed(token)
        let similar = try titles(URLRequest(url: URL(string: server + "/api/v1/items/\(seedItem)/vod-similar")!), token)
        guard let first = similar.first else { throw XCTSkip("nothing Also on VOD for the seed movie (seed data missing)") }
        let movie = try api("GET", "/items/\(seedItem)", token: token)
        let title = try XCTUnwrap(movie["title"] as? String)
        try signIn(as: token)
        app.openSection("Movies")
        let poster = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(poster.waitForExistence(timeout: 10), "\(title) is in Movies")
        remote.press(.down)
        for _ in 0..<8 where !poster.hasFocus { remote.press(.right) }
        for _ in 0..<8 where !poster.hasFocus { remote.press(.left) }
        XCTAssertTrue(poster.hasFocus, "focus is on \(focused.label)")
        remote.press(.select)
        let t = tile(first)
        XCTAssertTrue(t.waitForExistence(timeout: 15), "Also on VOD is on the movie's page")
        for _ in 0..<12 where !t.hasFocus && focused.identifier != "vod.suggestion" { remote.press(.down) }
        for _ in 0..<4 where !t.hasFocus { remote.press(.left) }
        XCTAssertTrue(t.hasFocus, "focus is on \(focused.label)")
        XCTAssertTrue(app.staticTexts["Also on VOD"].exists)
        sleep(1)
        shot("tv-movie-also-on-vod")
        remote.press(.select)
        XCTAssertTrue(app.buttons["vod.play"].waitForExistence(timeout: 10), "the film's page opens")
        shot("tv-vod-film-from-also-on-vod")
    }
}
