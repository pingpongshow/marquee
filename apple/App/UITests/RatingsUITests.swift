import XCTest

/// Ratings and favourites carried over from Plex (stars on rows and item pages, the Favorites
/// page, the My rating sort), and header layout checks on iPhone (a stacked movie header, a More
/// button as tall as its neighbours). Each test uses a temporary account, so the ratings it adds
/// go with it.
final class RatingsUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

    override func setUp() {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-marquee-reset"]
        app.launch()
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

    private static func send(_ method: String, _ url: String, token: String?, json: String? = nil) {
        guard let token, let u = URL(string: url) else { return }
        var req = URLRequest(url: u)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let json {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = Data(json.utf8)
        }
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { _, _, _ in done.signal() }.resume()
        _ = done.wait(timeout: .now() + 15)
    }

    /// A temporary account (deleted when the test ends, with its ratings) and its API token.
    private func temporaryUser() throws -> (name: String, password: String, token: String) {
        let name = "uirate-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                    "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Ratings", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    /// The id of a track in the Music library by title.
    private func trackID(_ title: String) throws -> Int {
        let libs = try api("GET", "/libraries") as? [[String: Any]] ?? []
        let music = try XCTUnwrap(libs.first { $0["type"] as? String == "music" && $0["name"] as? String == "Music" }?["id"] as? Int)
        let page = try api("GET", "/libraries/\(music)/items?type=track&limit=500") as? [String: Any] ?? [:]
        let items = page["items"] as? [[String: Any]] ?? []
        return try XCTUnwrap(items.first { $0["title"] as? String == title }?["id"] as? Int, "no track \(title)")
    }

    // MARK: - App helpers

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

    private func byID(_ id: String, _ type: XCUIElement.ElementType = .any) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "identifier == %@", id)).firstMatch
    }

    private func openMusicRow(_ id: String) {
        let shortcut = app.buttons["musicShowLibrary"]
        XCTAssertTrue(shortcut.waitForExistence(timeout: 15))
        shortcut.tap()
        let row = app.buttons["musicLibrary.\(id)"]
        for _ in 0..<10 where !(row.exists && row.isHittable) { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        for _ in 0..<4 where row.frame.maxY > app.tabBars.firstMatch.frame.minY - 10 { app.swipeUp() }
        row.tap()
    }

    // MARK: - Tests

    /// A rated track shows its stars on its album's track row and in Favorites; the album page
    /// has tap-to-rate stars; Songs sorts by My rating.
    func testRatedTrackStarsFavoritesAndSort() throws {
        let user = try temporaryUser()
        let track = try trackID("Floating 1")
        try api("PUT", "/items/\(track)/rating", ["rating": 9], token: user.token)
        signIn(user.name, user.password)

        // The Music home: Favorites in the Library list, with its count.
        openLibrary("Music")
        openMusicRow("favorites")
        XCTAssertTrue(app.navigationBars["Favorites"].waitForExistence(timeout: 10))
        let fav = byID("favoriteTrack.\(track)")
        XCTAssertTrue(fav.waitForExistence(timeout: 10), "the rated track is in Favorites")
        XCTAssertEqual(app.buttons["rowRating.\(track)"].value as? String, "4½ stars", "its stars show")
        XCTAssertTrue(app.buttons["Shuffle"].exists && app.buttons["Play"].exists)
        shot("rt1-favorites")

        // The album: stars on the track row, changeable there, and the album's own rating.
        app.navigationBars.buttons.firstMatch.tap()
        openMusicRow("albums")
        let album = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        for _ in 0..<4 where !album.waitForExistence(timeout: 2) { app.swipeUp() }
        album.tap()
        let rowStars = app.buttons["rowRating.\(track)"]
        for _ in 0..<4 where !(rowStars.exists && rowStars.isHittable) { app.swipeUp() }
        XCTAssertTrue(rowStars.waitForExistence(timeout: 10), "the rated track's row shows its stars")
        XCTAssertEqual(rowStars.value as? String, "4½ stars")
        shot("rt2-album")
        // Rate it 4 stars from the row; the server has it.
        rowStars.tap()
        let four = app.buttons["4 stars"].firstMatch
        XCTAssertTrue(four.waitForExistence(timeout: 5), "the row's rating menu opens")
        four.tap()
        let saved = Date().addingTimeInterval(10)
        var rating: Double?
        repeat {
            rating = (try api("GET", "/items/\(track)", token: user.token) as? [String: Any])?["userRating"] as? Double
            if rating != 8 { sleep(1) }
        } while rating != 8 && Date() < saved
        XCTAssertEqual(rating, 8, "the row's rating reached the server")
        XCTAssertEqual(rowStars.value as? String, "4 stars")
        // An unrated track's row offers an outline star.
        let other = try trackID("Floating 2")
        let unrated = app.buttons["rowRating.\(other)"]
        if unrated.exists { XCTAssertEqual(unrated.value as? String, "Not rated") }
        for _ in 0..<4 { app.swipeDown() }
        XCTAssertTrue(byID("itemRating").waitForExistence(timeout: 5), "the album page has rating stars")

        // Songs sorted by My rating: the rated track first.
        app.navigationBars.buttons.firstMatch.tap()
        app.navigationBars.buttons.firstMatch.tap()
        openMusicRow("songs")
        XCTAssertTrue(app.navigationBars["Songs"].waitForExistence(timeout: 10))
        byID("sortMenu").tap()
        app.buttons["My rating"].tap()
        let first = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'songRow.'")).firstMatch
        XCTAssertTrue(first.waitForExistence(timeout: 10))
        let deadline = Date().addingTimeInterval(10)
        while first.identifier != "songRow.\(track)", Date() < deadline { sleep(1) }
        XCTAssertEqual(first.identifier, "songRow.\(track)", "the rated track sorts first")
        shot("rt3-songs-my-rating")
    }

    /// iPhone: a movie's header stacks the poster above the title (no mid-word breaks), and More
    /// is as tall as the buttons beside it.
    func testMovieHeaderStackedAndMoreHeight() throws {
        guard UIDevice.current.userInterfaceIdiom == .phone else { throw XCTSkip("iPhone layout") }
        let user = try temporaryUser()
        signIn(user.name, user.password)
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        let movie = app.scrollViews.buttons.firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        movie.tap()
        XCTAssertTrue(byID("itemHeader.stacked").waitForExistence(timeout: 10), "the header stacks on iPhone")
        XCTAssertFalse(byID("itemHeader.beside").exists)
        let title = byID("itemTitle")
        XCTAssertGreaterThan(title.frame.width, app.windows.firstMatch.frame.width * 0.5, "the title has the full width")
        XCTAssertTrue(byID("itemRating").exists, "the movie can be rated")
        let more = app.buttons["More"].firstMatch
        let play = app.buttons.matching(NSPredicate(format: "label == 'Play' OR label == 'Resume'")).firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 5) && play.exists)
        sleep(1)
        shot("mh1-movie-header")
        let neighbours = [play] + ["Watchlist", "On Watchlist", "Mark watched", "Watched", "Download"].map { app.buttons[$0].firstMatch }.filter(\.exists)
        for b in neighbours {
            XCTAssertEqual(more.frame.height, b.frame.height, accuracy: 1, "More \(more.frame) vs \(b.label) \(b.frame)")
        }
    }

    // MARK: - Community ratings and comments

    private var kidToken: String? {
        let path = ProcessInfo.processInfo.environment["MARQUEE_TEST_KID_TOKEN_FILE"]
            ?? "/private/tmp/claude-501/-Users-stephen-Desktop-Media-Server/444e0e4f-966a-4478-8095-b26cddc3cd0c/scratchpad/kid.token"
        return (try? String(contentsOfFile: path, encoding: .utf8))?.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Two people rate an album (over the API); Kiddo, signed in on the phone, sees the community
    /// average and the other's comment, then posts, edits and deletes a comment of their own.
    func testCommunityRatingsAndComments() throws {
        guard let kid = kidToken, !kid.isEmpty else { throw XCTSkip("no Kiddo token") }
        let admin = try XCTUnwrap(adminToken)
        let libs = try api("GET", "/libraries") as? [[String: Any]] ?? []
        let music = try XCTUnwrap(libs.first { $0["type"] as? String == "music" && $0["name"] as? String == "Music" }?["id"] as? Int)
        let albums = (try api("GET", "/libraries/\(music)/items?type=album&limit=100") as? [String: Any])?["items"] as? [[String: Any]] ?? []
        let album = try XCTUnwrap(albums.first { $0["title"] as? String == "Pulse" }?["id"] as? Int)
        let adminID = try XCTUnwrap((try api("GET", "/me") as? [String: Any])?["id"] as? Int)
        let kidID = try XCTUnwrap((try api("GET", "/me", token: kid) as? [String: Any])?["id"] as? Int)
        let base = "http://\(server)/api/v1/items/\(album)"
        addTeardownBlock {
            Self.send("DELETE", "\(base)/reviews/\(adminID)", token: admin)
            Self.send("DELETE", "\(base)/reviews/\(kidID)", token: admin)
            // Comments go with the reviews; the ratings are cleared separately.
            Self.send("PUT", "\(base)/rating", token: admin, json: #"{"rating":null}"#)
            Self.send("PUT", "\(base)/rating", token: kid, json: #"{"rating":null}"#)
        }
        try api("PUT", "/items/\(album)/rating", ["rating": 8], token: admin)
        try api("PUT", "/items/\(album)/reviews", ["comment": "Admin likes this one"], token: admin)
        try api("PUT", "/items/\(album)/rating", ["rating": 6], token: kid)

        // Kiddo on the phone.
        let address = app.textFields["Home address, e.g. 10.1.1.10:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.tap()
        address.typeText(server)
        app.buttons["Connect"].tap()
        let who = app.buttons["Kiddo"].firstMatch
        XCTAssertTrue(who.waitForExistence(timeout: 15))
        who.tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
        openLibrary("Music")
        openMusicRow("albums")
        let card = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Pulse'")).firstMatch
        for _ in 0..<4 where !card.waitForExistence(timeout: 2) { app.swipeUp() }
        card.tap()

        // The page: your rating, and the community's (8 and 6: 3.5 stars from 2).
        XCTAssertTrue(byID("itemRating").waitForExistence(timeout: 10))
        let summary = byID("communitySummary")
        XCTAssertTrue(summary.waitForExistence(timeout: 5))
        XCTAssertEqual(summary.label, "Community rating 3.5 stars, 2 ratings")
        shot("cr1-item-community")

        // Ratings & Comments.
        byID("communityRow").tap()
        let adminRow = byID("review.\(adminID)")
        XCTAssertTrue(adminRow.waitForExistence(timeout: 10), "the other person's review is listed")
        XCTAssertTrue(adminRow.label.contains("Admin likes this one"), adminRow.label)
        XCTAssertTrue(byID("review.\(kidID)").label.contains("You"), "your own row is tagged You")
        let field = byID("commentField")
        field.tap()
        field.typeText("Great for car rides")
        byID("postComment").tap()
        let comment = { () -> String? in
            let r = (try? self.api("GET", "/items/\(album)/reviews", token: kid)) as? [String: Any]
            let mine = (r?["reviews"] as? [[String: Any]])?.first { $0["mine"] as? Bool == true }
            return mine?["comment"] as? String
        }
        var c: String?
        for _ in 0..<10 { c = comment(); if c == "Great for car rides" { break }; sleep(1) }
        XCTAssertEqual(c, "Great for car rides", "the comment was posted")
        XCTAssertTrue(byID("review.\(kidID)").label.contains("Great for car rides"))
        shot("cr2-posted")

        // Edit, then delete.
        field.tap()
        field.typeText(" and long walks")
        byID("postComment").tap()
        for _ in 0..<10 { c = comment(); if c == "Great for car rides and long walks" { break }; sleep(1) }
        XCTAssertEqual(c, "Great for car rides and long walks", "the comment was edited")
        byID("deleteComment").tap()
        for _ in 0..<10 { c = comment(); if c == nil || c == "" { break }; sleep(1) }
        XCTAssertTrue(c == nil || c == "", "the comment was deleted")
        XCTAssertFalse(byID("deleteComment").waitForExistence(timeout: 3))

        // Rating from the sheet updates the average shown on the page.
        app.buttons["Rate 5 stars"].firstMatch.tap()
        var kidRating: Double?
        for _ in 0..<10 {
            kidRating = (try api("GET", "/items/\(album)", token: kid) as? [String: Any])?["userRating"] as? Double
            if let r = kidRating, r != 6 { break }
            sleep(1)
        }
        let k = try XCTUnwrap(kidRating, "the sheet's rating reached the server")
        XCTAssertNotEqual(k, 6)
        app.buttons["Done"].tap()
        let want = "Community rating \(((8 + k) / 4).formatted(.number.precision(.fractionLength(1)))) stars, 2 ratings"
        let deadline = Date().addingTimeInterval(10)
        while summary.label != want, Date() < deadline { sleep(1) }
        XCTAssertEqual(summary.label, want)
        shot("cr3-updated-average")
    }
}
