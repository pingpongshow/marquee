import XCTest

/// Suggestions on demand (LIVE-6, USER-16): Home's Watch on VOD row (a title opens its page;
/// Not interested takes it away) and Also on VOD on a movie's page.
///
/// Needs on-demand films with TMDB ids and a library movie whose TMDB recommendations point at
/// them: the films "Apple Test Alpha/Bravo/Charlie" (the VodUITests source) matched to TMDB
/// ids, and the library movie MARQUEE_TEST_VOD_SEED_ITEM (default 8) with an external TMDB id
/// and tmdb_recommendations rows naming those ids. A temporary account makes that movie a
/// favourite, so its recommendations become its suggestions; the test skips when there are none.
final class VodSuggestionsUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }
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

    private static func send(_ method: String, _ url: String, token: String?) {
        guard let token, let u = URL(string: url) else { return }
        var req = URLRequest(url: u)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { _, _, _ in done.signal() }.resume()
        _ = done.wait(timeout: .now() + 15)
    }

    /// A temporary account (deleted when the test ends) with the seed movie as a favourite.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "uivodsug\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int, "created a test user")
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                    "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "ios"]]) as? [String: Any] ?? [:]
        let token = try XCTUnwrap(login["token"] as? String)
        try api("PUT", "/items/\(seedItem)/favorite", token: token)
        return (name, pass, token)
    }

    /// The titles suggested to `token`, by title.
    private func suggested(_ token: String) throws -> [String] {
        let list = try api("GET", "/livetv/vod/suggestions?kind=movie", token: token) as? [[String: Any]] ?? []
        return list.compactMap { ($0["movie"] as? [String: Any])?["title"] as? String }
    }

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

    private func tile(_ title: String) -> XCUIElement {
        app.buttons.matching(identifier: "vod.suggestion").matching(NSPredicate(format: "label == %@", title)).firstMatch
    }

    /// Home's Watch on VOD row: a title opens its film's page; Not interested takes it off the
    /// row (and the server stops suggesting it).
    func testHomeWatchOnVodRow() throws {
        let user = try temporaryUser()
        let titles = try suggested(user.token)
        guard let first = titles.first else { throw XCTSkip("no VOD suggestions (seed data missing)") }
        signIn(user)

        XCTAssertTrue(app.staticTexts["Watch on VOD"].waitForExistence(timeout: 15), "the Watch on VOD row is on Home")
        let t = tile(first)
        XCTAssertTrue(t.waitForExistence(timeout: 10))
        XCTAssertTrue((t.value as? String ?? "").contains("Because you"), "the reason shows under the title: \(t.value ?? "")")
        shot("home-watch-on-vod")

        t.tap()
        XCTAssertTrue(app.buttons["vod.play"].waitForExistence(timeout: 10), "the film's page opens")
        shot("vod-film-from-home")
        app.navigationBars.buttons.element(boundBy: 0).tap()
        XCTAssertTrue(t.waitForExistence(timeout: 10))

        t.press(forDuration: 1.2)
        let notInterested = app.buttons["Not interested"]
        XCTAssertTrue(notInterested.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Go to film"].exists)
        shot("home-watch-on-vod-menu")
        notInterested.tap()
        let gone = NSPredicate(format: "exists == false")
        expectation(for: gone, evaluatedWith: t)
        waitForExpectations(timeout: 10)
        XCTAssertFalse(try suggested(user.token).contains(first), "the server no longer suggests it")
        // Home reloaded from the server: still gone.
        sleep(2)
        XCTAssertFalse(t.exists)
        shot("home-watch-on-vod-hidden")
    }

    /// Also on VOD on the seed movie's page, and a title there opens its film's page.
    func testAlsoOnVodOnMoviePage() throws {
        let user = try temporaryUser()
        let similar = try api("GET", "/items/\(seedItem)/vod-similar", token: user.token) as? [[String: Any]] ?? []
        guard let first = similar.first.flatMap({ ($0["movie"] as? [String: Any])?["title"] as? String }) else {
            throw XCTSkip("nothing Also on VOD for the seed movie (seed data missing)")
        }
        let seed = try api("GET", "/items/\(seedItem)", token: user.token) as? [String: Any] ?? [:]
        let title = try XCTUnwrap(seed["title"] as? String)
        signIn(user)
        // (A marquee:// link would relaunch the app, and -marquee-reset sign it out.)
        app.buttons["Libraries"].firstMatch.tap()
        let back = app.navigationBars.buttons["Libraries"]
        if back.waitForExistence(timeout: 2) { back.tap() }
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Movies")).firstMatch.tap()
        let movie = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title)).firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10), "\(title) is in Movies")
        movie.tap()
        let row = app.staticTexts["Also on VOD"]
        for _ in 0..<6 where !row.exists { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10), "Also on VOD is on the movie's page")
        let t = tile(first)
        for _ in 0..<3 where !t.isHittable { app.swipeUp() }
        XCTAssertTrue(t.exists)
        shot("movie-also-on-vod")
        t.tap()
        XCTAssertTrue(app.buttons["vod.play"].waitForExistence(timeout: 10), "the film's page opens")
        shot("vod-film-from-also-on-vod")
    }
}
