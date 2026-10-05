import XCTest

/// Offline sync (USER-18): changes made while the server can't be reached stay on screen,
/// wait in Settings, and reach the server when the app reconnects. "Offline" is the
/// `-marquee-offline-changes` launch argument, which fails the change requests as if there
/// were no network. Uses a temporary account, deleted with its changes when the test ends.
final class OfflineSyncUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

    override func setUp() {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-marquee-reset", "-marquee-offline-changes"]
        app.launch()
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
        req.timeoutInterval = 30
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
        wait(for: [done], timeout: 40)
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

    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "uisync-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                    "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Sync", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    private func library(_ type: String, _ name: String) throws -> Int {
        let libs = try api("GET", "/libraries") as? [[String: Any]] ?? []
        return try XCTUnwrap(libs.first { $0["type"] as? String == type && $0["name"] as? String == name }?["id"] as? Int)
    }

    private func byID(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "identifier == %@", id)).firstMatch
    }

    private func signIn(_ name: String, _ password: String) {
        let address = app.textFields["Home address, e.g. 10.1.1.10:32500"]
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
        field.typeText(name)
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText(password)
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
    }

    private func openLibrary(_ name: String) {
        app.buttons["Libraries"].firstMatch.tap()
        let back = app.navigationBars.buttons["Libraries"]
        if back.waitForExistence(timeout: 2) { back.tap() }
        let lib = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
        XCTAssertTrue(lib.waitForExistence(timeout: 10))
        lib.tap()
    }

    private func openSettings() {
        app.buttons["Settings"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 10))
    }

    /// Waits for `check` to hold, polling once a second.
    private func eventually(_ seconds: TimeInterval = 20, _ check: () throws -> Bool) rethrows -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        repeat {
            if try check() { return true }
            sleep(1)
        } while Date() < deadline
        return false
    }

    /// Offline: rate a track, mark a movie watched and add it to the watchlist. The page keeps
    /// the new values, Settings counts 3 waiting; reconnected (relaunched without the offline
    /// switch), they reach the server and the row goes.
    func testChangesMadeOfflineSyncOnReconnect() throws {
        let user = try temporaryUser()
        let music = try library("music", "Music")
        let tracks = (try api("GET", "/libraries/\(music)/items?type=track&limit=500") as? [String: Any])?["items"] as? [[String: Any]] ?? []
        let track = try XCTUnwrap(tracks.first { $0["title"] as? String == "Floating 1" }?["id"] as? Int)
        let movies = try library("movies", "Movies")
        let firstMovie = ((try api("GET", "/libraries/\(movies)/items?sort=title&limit=1", token: user.token) as? [String: Any])?["items"] as? [[String: Any]])?.first
        let movie = try XCTUnwrap(firstMovie?["id"] as? Int)
        let movieTitle = try XCTUnwrap(firstMovie?["title"] as? String)
        signIn(user.name, user.password)

        // A track's row rating, from its album.
        openLibrary("Music")
        let shortcut = app.buttons["musicShowLibrary"]
        XCTAssertTrue(shortcut.waitForExistence(timeout: 15))
        shortcut.tap()
        let albums = app.buttons["musicLibrary.albums"]
        for _ in 0..<10 where !(albums.exists && albums.isHittable) { app.swipeUp() }
        albums.tap()
        let album = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        for _ in 0..<4 where !album.waitForExistence(timeout: 2) { app.swipeUp() }
        album.tap()
        let rowStars = app.buttons["rowRating.\(track)"]
        for _ in 0..<4 where !(rowStars.exists && rowStars.isHittable) { app.swipeUp() }
        XCTAssertTrue(rowStars.waitForExistence(timeout: 10))
        rowStars.tap()
        let four = app.buttons["4 stars"].firstMatch
        XCTAssertTrue(four.waitForExistence(timeout: 5))
        four.tap()
        sleep(2)
        XCTAssertEqual(rowStars.value as? String, "4 stars", "the rating stays (no revert, no alert)")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        shot("os1-rated-offline")

        // A movie: watched and on the watchlist.
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        let card = app.scrollViews.buttons.firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        card.tap()
        XCTAssertTrue(app.navigationBars[movieTitle].waitForExistence(timeout: 10), "opened \(movieTitle)")
        let watched = app.buttons["markWatched"].firstMatch
        XCTAssertTrue(watched.waitForExistence(timeout: 10))
        XCTAssertEqual(watched.label, "Mark watched")
        watched.tap()
        let list = app.buttons["watchlistToggle"].firstMatch
        XCTAssertTrue(list.waitForExistence(timeout: 5))
        XCTAssertEqual(list.label, "Watchlist")
        list.tap()
        sleep(2)
        XCTAssertEqual(watched.label, "Watched", "shown as watched")
        XCTAssertEqual(list.label, "On Watchlist", "shown on the watchlist")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        shot("os2-movie-offline")

        // Settings counts them; the server doesn't have them yet.
        openSettings()
        let pending = byID("pendingChanges")
        XCTAssertTrue(pending.waitForExistence(timeout: 5))
        XCTAssertTrue(pending.label.contains("3") || (pending.value as? String) == "3", pending.label)
        shot("os3-settings-pending")
        let before = try api("GET", "/items/\(movie)", token: user.token) as? [String: Any] ?? [:]
        XCTAssertEqual(before["viewCount"] as? Int ?? 0, 0, "not watched on the server yet")

        // Back online: the app reconnects and sends them.
        app.terminate()
        app.launchArguments = []
        app.launch()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 20))
        let synced = try eventually {
            let t = try api("GET", "/items/\(track)", token: user.token) as? [String: Any] ?? [:]
            let m = try api("GET", "/items/\(movie)", token: user.token) as? [String: Any] ?? [:]
            return t["userRating"] as? Double == 8 && (m["viewCount"] as? Int ?? 0) > 0 && m["watchlisted"] as? Bool == true
        }
        XCTAssertTrue(synced, "the rating, watched and watchlist reached the server")
        openSettings()
        XCTAssertFalse(byID("pendingChanges").waitForExistence(timeout: 3), "nothing waits any more")
        shot("os4-synced")
    }
}
