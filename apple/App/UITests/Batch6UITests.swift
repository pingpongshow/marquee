import XCTest

/// Batch 6 against the local test server: Remove from Continue Watching (USER-6), Live TV only
/// when usable (LIVE-1), marking watched shows at once everywhere, video playlists, and lists
/// keeping their place after opening an item and going back.
final class Batch6UITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

    /// 00 Long Test (90 s), and two short generated movies.
    private let longTest = 322, xenon = 8, river = 13

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
        wait(for: [done], timeout: 30)
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

    /// A temporary account (its watch state and playlists go with it), deleted when the test ends.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "ui6user-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Check", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    private func continueIDs(_ token: String) throws -> [Int] {
        let hubs = try api("GET", "/hubs/home", token: token) as? [[String: Any]] ?? []
        let row = hubs.first { $0["id"] as? String == "continue-watching" }
        return (row?["items"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? Int }
    }

    private func viewCount(_ id: Int, _ token: String) throws -> Int {
        (try api("GET", "/items/\(id)", token: token) as? [String: Any])?["viewCount"] as? Int ?? 0
    }

    private func sessions(for itemID: Int) throws -> Int {
        let list = try api("GET", "/playback/sessions") as? [[String: Any]] ?? []
        return list.filter { ($0["itemId"] as? Int) == itemID }.count
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

    private func openLibrary(_ name: String) {
        openLibrariesTab()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch.tap()
        XCTAssertTrue(app.navigationBars[name].waitForExistence(timeout: 10))
    }

    private func poster(_ prefix: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
    }

    /// Long-presses an element and picks a context menu entry.
    private func contextMenu(_ element: XCUIElement, _ entry: String) {
        element.press(forDuration: 1.2)
        let button = app.buttons[entry].firstMatch
        XCTAssertTrue(button.waitForExistence(timeout: 5), "\(entry) is in the menu")
        button.tap()
    }

    // MARK: - A. Remove from Continue Watching

    func testRemoveFromContinueWatching() throws {
        let user = try temporaryUser()
        try api("PUT", "/items/\(longTest)/progress", ["positionMs": 60000], token: user.token)
        XCTAssertTrue(try continueIDs(user.token).contains(longTest))
        signIn(user)
        let tile = app.buttons["continue.\(longTest)"]
        XCTAssertTrue(tile.waitForExistence(timeout: 15), "00 Long Test is in Continue Watching")
        shot("a1-continue")
        contextMenu(tile, "Remove from Continue Watching")
        XCTAssertTrue(tile.waitForNonExistence(timeout: 5), "the tile goes at once")
        try eventually(10, "gone from the server's row") { try !continueIDs(user.token).contains(longTest) }
        shot("a2-removed")
        // The position is kept.
        let item = try api("GET", "/items/\(longTest)", token: user.token) as? [String: Any] ?? [:]
        XCTAssertEqual(item["viewOffsetMs"] as? Int, 60000)
    }

    // MARK: - B. Live TV only when usable

    func testLiveTVHiddenWhenUnusable() throws {
        let user = try temporaryUser()
        app.launchArguments += ["-marquee-test-live-off"]
        signIn(user)
        openLibrariesTab()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Movies'")).firstMatch.waitForExistence(timeout: 10))
        sleep(2)
        XCTAssertFalse(app.buttons["libraries.liveTV"].exists, "Live TV isn't listed")
        XCTAssertFalse(app.buttons["Live TV"].exists)
        shot("b1-no-live-tv")
    }

    // MARK: - C. Mark as watched

    /// From the item page: the button changes at once and the server has it; from a tile's
    /// context menu: the menu offers the other way next time.
    func testMarkWatchedItemPageAndTileMenu() throws {
        let user = try temporaryUser()
        signIn(user)
        openLibrary("Movies")
        poster("00 Long Test").tap()
        let mark = app.buttons["markWatched"]
        XCTAssertTrue(mark.waitForExistence(timeout: 10))
        XCTAssertEqual(mark.label, "Mark watched")
        mark.tap()
        XCTAssertTrue(app.buttons["Watched"].waitForExistence(timeout: 3), "the page shows it at once")
        try eventually(10, "watched on the server") { try viewCount(longTest, user.token) > 0 }
        shot("c1-page-watched")
        app.navigationBars.buttons.firstMatch.tap() // back to the grid
        let tile = poster("00 Long Test")
        XCTAssertTrue(tile.waitForExistence(timeout: 10))
        contextMenu(tile, "Mark Unwatched")
        try eventually(10, "unwatched on the server") { try viewCount(longTest, user.token) == 0 }
        // The menu is current: it offers Mark Watched now.
        tile.press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["Mark Watched"].waitForExistence(timeout: 5))
        app.buttons["Mark Watched"].tap()
        try eventually(10, "watched again on the server") { try viewCount(longTest, user.token) > 0 }
        tile.tap()
        XCTAssertTrue(app.buttons["Watched"].waitForExistence(timeout: 10), "the page agrees")
    }

    /// Marking watched from a Continue Watching tile takes it off the row.
    func testMarkWatchedRefreshesContinueWatching() throws {
        let user = try temporaryUser()
        try api("PUT", "/items/\(longTest)/progress", ["positionMs": 60000], token: user.token)
        signIn(user)
        let tile = app.buttons["continue.\(longTest)"]
        XCTAssertTrue(tile.waitForExistence(timeout: 15))
        contextMenu(tile, "Mark Watched")
        XCTAssertTrue(tile.waitForNonExistence(timeout: 10), "Home's rows refresh")
        XCTAssertGreaterThan(try viewCount(longTest, user.token), 0)
        XCTAssertFalse(try continueIDs(user.token).contains(longTest))
    }

    /// A show marked watched: every episode on the server, and the page says so.
    func testMarkShowWatched() throws {
        let user = try temporaryUser()
        signIn(user)
        openLibrary("Anime")
        poster("Test Anime").tap()
        let mark = app.buttons["markWatched"]
        XCTAssertTrue(mark.waitForExistence(timeout: 10))
        mark.tap()
        XCTAssertTrue(app.buttons["Watched"].waitForExistence(timeout: 3))
        try eventually(10, "every episode watched") {
            let leaves = try api("GET", "/items/1/leaves", token: user.token) as? [[String: Any]] ?? []
            return !leaves.isEmpty && leaves.allSatisfy { ($0["viewCount"] as? Int ?? 0) > 0 }
        }
        shot("c2-show-watched")
    }

    // MARK: - D. Video playlists

    func testVideoPlaylistPlays() throws {
        let user = try temporaryUser()
        let list = try api("POST", "/playlists", ["title": "UI6 Movies", "kind": "video", "itemIds": [longTest, xenon]], token: user.token) as? [String: Any] ?? [:]
        XCTAssertNotNil(list["id"])
        signIn(user)
        openLibrariesTab()
        let playlists = app.buttons["Playlists"].firstMatch
        for _ in 0..<4 where !playlists.isHittable { app.swipeUp() }
        playlists.tap()
        let card = app.buttons.matching(NSPredicate(format: "label CONTAINS 'UI6 Movies'")).firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), "the video playlist is listed")
        shot("d1-playlists")
        card.tap()
        XCTAssertTrue(app.staticTexts["00 Long Test"].waitForExistence(timeout: 10), "its items")
        let before = try sessions(for: longTest)
        app.buttons["Play"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 15), "the video player opens")
        try eventually(20, "00 Long Test plays first") { try sessions(for: longTest) > before }
        shot("d2-playing")
        app.buttons["Close player"].tap()
    }

    // MARK: - E. Lists keep their place

    func testLibraryGridKeepsPlaceAfterBack() throws {
        let user = try temporaryUser()
        signIn(user)
        openLibrary("Movies")
        XCTAssertTrue(poster("00 Long Test").waitForExistence(timeout: 10))
        for _ in 0..<6 { app.swipeUp() }
        sleep(1)
        let screen = app.windows.firstMatch.frame
        // Poster buttons read "Title, Title, Year".
        let posters = app.buttons.matching(NSPredicate(format: "label MATCHES '.+, .+, [0-9]{4}'")).allElementsBoundByIndex
        let visible = posters.filter { $0.frame.minY > screen.minY + 200 && $0.frame.maxY < screen.maxY - 200 && $0.isHittable }
        let target = try XCTUnwrap(visible.first, "a poster well down the grid (\(posters.count) posters)")
        let label = target.label
        XCTAssertFalse(label.hasPrefix("00 "), "scrolled past the top")
        shot("e1-scrolled")
        target.tap()
        XCTAssertTrue(app.buttons["markWatched"].waitForExistence(timeout: 10))
        app.navigationBars.buttons.firstMatch.tap()
        let again = app.buttons[label].firstMatch
        XCTAssertTrue(again.waitForExistence(timeout: 5))
        sleep(1)
        XCTAssertTrue(again.isHittable, "\(label) is still on screen after going back")
        XCTAssertFalse(poster("00 Long Test").isHittable, "not back at the top")
        shot("e2-back")
    }

    func testSongsKeepPlaceAfterBack() throws {
        let user = try temporaryUser()
        signIn(user)
        openLibrary("Music")
        let shortcut = app.buttons["musicShowLibrary"]
        XCTAssertTrue(shortcut.waitForExistence(timeout: 15))
        shortcut.tap()
        let row = app.buttons["musicLibrary.songs"]
        for _ in 0..<12 where !row.isHittable { app.swipeUp() }
        row.tap()
        XCTAssertTrue(app.navigationBars["Songs"].waitForExistence(timeout: 10))
        let rows = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'songRow.'"))
        XCTAssertTrue(rows.firstMatch.waitForExistence(timeout: 10))
        let first = rows.firstMatch.identifier
        for _ in 0..<3 { app.swipeUp() }
        sleep(1)
        let target = try XCTUnwrap(rows.allElementsBoundByIndex.filter { $0.isHittable }.dropFirst(2).first, "a song well down the list")
        let id = target.identifier
        XCTAssertNotEqual(id, first)
        contextMenu(target, "Go to Album")
        XCTAssertTrue(app.buttons["Play"].firstMatch.waitForExistence(timeout: 10), "the album opens")
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Songs"].waitForExistence(timeout: 5))
        sleep(1)
        XCTAssertTrue(app.buttons[id].isHittable, "the song is still on screen after going back")
        XCTAssertFalse(app.buttons[first].isHittable, "not back at the top")
    }
}
