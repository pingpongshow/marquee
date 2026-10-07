import XCTest

/// Batch 5 fixes against the local test server: Now Playing's bar jumps where it's tapped
/// (MUSIC-9), lists end above the mini player, song rows keep the duration clear of the stars,
/// and the Trailer button on a movie's page (PLAY-22).
final class Batch5UITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

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

    /// A temporary account, deleted when the test ends; returns its name, password and a token.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "ui5user-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Check", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    /// The server's playback sessions for an item (admin view: everyone's).
    private func sessions(for itemID: Int) throws -> Int {
        let list = try api("GET", "/playback/sessions") as? [[String: Any]] ?? []
        return list.filter { ($0["itemId"] as? Int) == itemID }.count
    }

    // MARK: - App helpers

    private func signIn(_ user: (name: String, password: String, token: String)) {
        app.launch()
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
        field.typeText(user.name)
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText(user.password)
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
    }

    private func openLibrary(_ name: String) {
        let librariesTab = app.buttons["Libraries"].firstMatch
        if UIDevice.current.userInterfaceIdiom == .phone, librariesTab.exists {
            librariesTab.tap()
            let back = app.navigationBars.buttons["Libraries"]
            if back.waitForExistence(timeout: 2) { back.tap() }
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch.tap()
        } else {
            let item = app.cells[name].firstMatch
            if !item.exists { app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'sidebar'")).firstMatch.tap() }
            XCTAssertTrue(item.waitForExistence(timeout: 5))
            item.tap()
        }
    }

    private func openMovie(_ prefix: String) {
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        let movie = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        movie.tap()
    }

    /// Plays the Calm Pads album "Floating" from its page (leaves the album page open).
    private func playAlbum() {
        openLibrary("Music")
        XCTAssertTrue(app.navigationBars["Music"].waitForExistence(timeout: 10))
        openMusicLibraryRow("albums")
        let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        for _ in 0..<5 where !album.waitForExistence(timeout: 2) { app.swipeUp() }
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        album.tap()
        let play = app.buttons["Play"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        XCTAssertTrue(app.buttons["miniPlayer"].waitForExistence(timeout: 20))
    }

    private func openMusicLibraryRow(_ id: String) {
        let shortcut = app.buttons["musicShowLibrary"]
        XCTAssertTrue(shortcut.waitForExistence(timeout: 15))
        shortcut.tap()
        let row = app.buttons["musicLibrary.\(id)"]
        for _ in 0..<12 where !row.exists { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        for _ in 0..<6 where !row.isHittable { app.swipeUp() }
        row.tap()
    }

    /// "1:23 of 4:56" → (83, 296).
    private func times(_ value: String) -> (Double, Double)? {
        let parts = value.components(separatedBy: " of ").map { $0.split(separator: ":").compactMap { Double($0) }.reduce(0) { $0 * 60 + $1 } }
        return parts.count == 2 ? (parts[0], parts[1]) : nil
    }

    // MARK: - A. Tap the Now Playing bar to jump

    func testScrubberTapJumps() throws {
        let user = try temporaryUser()
        signIn(user)
        playAlbum()
        app.buttons["miniPlayer"].tap()
        let pause = app.buttons["np.playPause"]
        XCTAssertTrue(pause.waitForExistence(timeout: 10))
        let slider = app.sliders["Position"]
        XCTAssertTrue(slider.waitForExistence(timeout: 5))
        sleep(2)
        pause.tap() // paused, so the position stays where it's put
        sleep(1)
        let before = try XCTUnwrap(times(slider.value as? String ?? ""), "\(String(describing: slider.value))")
        XCTAssertGreaterThan(before.1, 20, "a track with a length")
        XCTAssertLessThan(before.0 / before.1, 0.4)
        slider.coordinate(withNormalizedOffset: CGVector(dx: 0.75, dy: 0.5)).tap()
        sleep(1)
        let after = try XCTUnwrap(times(slider.value as? String ?? ""))
        shot("a1-tapped-75")
        XCTAssertEqual(after.0 / after.1, 0.75, accuracy: 0.06, "jumped to about 75%: \(after)")
        // And a drag still works: back to about a quarter.
        slider.coordinate(withNormalizedOffset: CGVector(dx: 0.7, dy: 0.5))
            .press(forDuration: 0.1, thenDragTo: slider.coordinate(withNormalizedOffset: CGVector(dx: 0.25, dy: 0.5)))
        sleep(1)
        let dragged = try XCTUnwrap(times(slider.value as? String ?? ""))
        XCTAssertEqual(dragged.0 / dragged.1, 0.25, accuracy: 0.08, "dragged to about 25%: \(dragged)")
        shot("a2-dragged-25")
    }

    // MARK: - B. Lists end above the mini player

    func testListEndsAboveMiniPlayer() throws {
        let user = try temporaryUser()
        signIn(user)
        playAlbum()
        app.navigationBars.buttons.firstMatch.tap() // back to Albums
        app.navigationBars.buttons["Music"].firstMatch.tap() // to the landing page
        XCTAssertTrue(app.navigationBars["Music"].waitForExistence(timeout: 10))
        for _ in 0..<12 { app.swipeUp(velocity: .fast) }
        sleep(2)
        let muse = app.buttons["musicLibrary.muse"]
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(muse.exists && mini.exists)
        shot("b1-landing-bottom")
        // The whole row is clear of the mini player (whose frame includes its material card).
        let bar = mini.frame.union(app.buttons["Next track"].firstMatch.frame)
        XCTAssertLessThanOrEqual(muse.frame.maxY, bar.minY, "Muse & Stations \(muse.frame) is under the mini player \(bar)")
        XCTAssertTrue(muse.isHittable)
        muse.tap()
        XCTAssertTrue(app.navigationBars["Muse & Stations"].waitForExistence(timeout: 10))
    }

    // MARK: - C. Song rows: duration and stars apart

    func testSongRowsDurationClearOfStars() throws {
        let user = try temporaryUser()
        // Rate the first few songs (one of them a half star), so the stars are drawn.
        let libs = try api("GET", "/libraries") as? [[String: Any]] ?? []
        let music = try XCTUnwrap(libs.first { $0["type"] as? String == "music" && $0["name"] as? String == "Music" }?["id"] as? Int)
        let page = try api("GET", "/libraries/\(music)/items?type=track&sort=title&limit=4", token: user.token) as? [String: Any] ?? [:]
        let ids = (page["items"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? Int }
        XCTAssertFalse(ids.isEmpty)
        for (i, id) in ids.enumerated() { try api("PUT", "/items/\(id)/rating", ["rating": i == 0 ? 9 : 10], token: user.token) }

        signIn(user)
        openLibrary("Music")
        XCTAssertTrue(app.navigationBars["Music"].waitForExistence(timeout: 10))
        openMusicLibraryRow("songs")
        XCTAssertTrue(app.navigationBars["Songs"].waitForExistence(timeout: 10))
        let first = app.buttons["songRow.\(ids[0])"]
        XCTAssertTrue(first.waitForExistence(timeout: 10))
        sleep(1)
        shot("c1-songs")
        var checked = 0
        for row in app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'songRow.'")).allElementsBoundByIndex where row.isHittable {
            let id = row.identifier.replacingOccurrences(of: "songRow.", with: "")
            let stars = app.descendants(matching: .any)["rowRating.\(id)"].firstMatch
            guard stars.exists else { continue }
            XCTAssertFalse(row.frame.intersects(stars.frame), "row \(id): \(row.frame) overlaps the stars \(stars.frame)")
            XCTAssertGreaterThanOrEqual(stars.frame.minX - row.frame.maxX, 0, "row \(id)")
            // The stars' area is wide enough for five stars plus the gap before them.
            XCTAssertGreaterThanOrEqual(stars.frame.width, 60, "row \(id): \(stars.frame)")
            XCTAssertLessThanOrEqual(stars.frame.maxX, app.windows.firstMatch.frame.maxX, "row \(id) on screen")
            checked += 1
        }
        XCTAssertGreaterThanOrEqual(checked, 3)
    }

    // MARK: - D. Trailer

    func testTrailerPlaysLocalTrailer() throws {
        let user = try temporaryUser()
        signIn(user)
        openMovie("00 Preview Test")
        let trailer = app.buttons["trailerButton"]
        XCTAssertTrue(trailer.waitForExistence(timeout: 15), "the Trailer button shows for 00 Preview Test")
        XCTAssertEqual(trailer.label, "Trailer")
        shot("d1-trailer-button")
        let before = try sessions(for: 362)
        trailer.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 15), "the player opens")
        var now = try sessions(for: 362)
        for _ in 0..<15 where now <= before {
            sleep(1)
            now = try sessions(for: 362)
        }
        XCTAssertGreaterThan(now, before, "the local trailer (362, Official Trailer) plays")
        shot("d2-trailer-playing")
        app.buttons["Close player"].tap()
    }

    func testTrailerYouTubeOpensSheet() throws {
        app.launchArguments += ["-marquee-test-youtube-trailer"]
        let user = try temporaryUser()
        signIn(user)
        openMovie("00 Preview Test")
        let trailer = app.buttons["trailerButton"]
        XCTAssertTrue(trailer.waitForExistence(timeout: 15))
        trailer.tap()
        let open = app.buttons["openInYouTube"]
        XCTAssertTrue(open.waitForExistence(timeout: 10), "the YouTube sheet opens")
        XCTAssertEqual(open.label, "Open in YouTube")
        XCTAssertTrue(app.webViews.firstMatch.waitForExistence(timeout: 10), "with the embedded player")
        XCTAssertTrue(app.navigationBars["Test Trailer"].exists, "titled with the trailer's name")
        sleep(4)
        shot("d3-youtube-sheet")
        app.buttons["Done"].tap()
        XCTAssertTrue(open.waitForNonExistence(timeout: 5))
        XCTAssertTrue(trailer.exists)
    }
}
