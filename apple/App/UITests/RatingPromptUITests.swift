import XCTest

/// The rating request when a movie or episode ends (USER-19): 00 Long Test (90 s) resumed near
/// its end plays out, the player closes and a card over the item page asks for stars. Each
/// test uses a temporary account; the server-wide switch is turned on for the test and back
/// off at the end.
final class RatingPromptUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }
    private let longTest = 322

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
        let name = "uiask-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": false]) as? [String: Any] ?? [:]
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let login = try api("POST", "/auth/login", ["username": name, "password": pass,
                                                    "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Rating Prompt", "platform": "web"]]) as? [String: Any] ?? [:]
        return (name, pass, try XCTUnwrap(login["token"] as? String))
    }

    /// Turns the server's rating requests on or off; always back off when the test ends.
    /// Other clients' tests share the server and switch it too, so tests set it again just
    /// before it matters.
    private func setServerPrompt(_ on: Bool) throws {
        try api("PATCH", "/settings", ["general": ["ratingPrompt": on]])
        guard !restoreRegistered else { return }
        restoreRegistered = true
        let url = "http://\(server)/api/v1/settings", admin = adminToken
        addTeardownBlock { Self.send("PATCH", url, token: admin, json: #"{"general":{"ratingPrompt":false}}"#) }
    }
    private var restoreRegistered = false

    /// Clears the person's rating of 00 Long Test and leaves it 10 s from the end.
    private func prepare(_ token: String) throws {
        try api("PUT", "/items/\(longTest)/rating", ["rating": NSNull()], token: token)
        try api("PUT", "/items/\(longTest)/progress", ["positionMs": 80000], token: token)
        let url = "http://\(server)/api/v1/items/\(longTest)/rating"
        addTeardownBlock { Self.send("PUT", url, token: token, json: #"{"rating":null}"#) }
    }

    private func userRating(_ token: String) throws -> Double? {
        (try api("GET", "/items/\(longTest)", token: token) as? [String: Any])?["userRating"] as? Double
    }

    private func preferences(_ token: String) throws -> [String: Any] {
        (try api("GET", "/me", token: token) as? [String: Any])?["preferences"] as? [String: Any] ?? [:]
    }

    private func eventually(_ timeout: TimeInterval, _ what: String, _ check: () throws -> Bool) rethrows {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if try check() { return }
            Thread.sleep(forTimeInterval: 0.5)
        }
        XCTAssertTrue(try check(), what)
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

    private var playButton: XCUIElement {
        app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
    }

    /// Opens 00 Long Test, resumes it 10 s from the end and waits for the player to open.
    private func playToEnd(serverPrompt: Bool) throws {
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        let movie = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '00 Long Test'")).firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        movie.tap()
        XCTAssertTrue(playButton.waitForExistence(timeout: 10))
        try setServerPrompt(serverPrompt) // the player reads it when it opens
        playButton.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 15), "the player opened")
    }

    private func byID(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "identifier == %@", id)).firstMatch
    }

    // MARK: - Tests

    /// The movie plays out, the player closes and the card asks; 4 stars saves 8 at once,
    /// says thanks and goes.
    func testPromptAfterMovieEndsSavesStars() throws {
        let user = try temporaryUser()
        try setServerPrompt(true)
        try prepare(user.token)
        signIn(user)
        try playToEnd(serverPrompt: true)

        let card = byID("ratingPrompt")
        XCTAssertTrue(card.waitForExistence(timeout: 45), "the rating request shows when the movie ends")
        XCTAssertTrue(playButton.exists, "over the item page")
        XCTAssertEqual(byID("ratingPromptTitle").label, "00 Long Test")
        XCTAssertTrue(byID("ratingPromptNotNow").exists)
        XCTAssertTrue(byID("ratingPromptNever").exists)
        shot("rp1-card")

        // The right half of the fourth star: four full stars.
        byID("ratingPromptStar4").coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)).tap()
        XCTAssertTrue(byID("ratingPromptThanks").waitForExistence(timeout: 3), "a brief thanks")
        shot("rp2-thanks")
        try eventually(10, "rating 8 saved") { try userRating(user.token) == 8 }
        XCTAssertTrue(card.waitForNonExistence(timeout: 6), "the card goes")
    }

    /// Don't ask again turns the person's preference off (and Settings shows it off).
    func testDontAskAgainTurnsPreferenceOff() throws {
        let user = try temporaryUser()
        try setServerPrompt(true)
        try prepare(user.token)
        XCTAssertNotEqual(try preferences(user.token)["ratingPrompt"] as? Bool, false)
        signIn(user)
        try playToEnd(serverPrompt: true)

        let card = byID("ratingPrompt")
        XCTAssertTrue(card.waitForExistence(timeout: 45))
        byID("ratingPromptNever").tap()
        XCTAssertTrue(card.waitForNonExistence(timeout: 5), "the card goes")
        try eventually(10, "the preference is off") { try preferences(user.token)["ratingPrompt"] as? Bool == false }
        XCTAssertNil(try userRating(user.token), "no rating saved")

        // Settings → the personal switch, off (shown while the server has it on).
        try setServerPrompt(true)
        app.buttons["Settings"].firstMatch.tap()
        let toggle = app.switches["ratingPromptPreference"]
        for _ in 0..<6 where !toggle.exists { app.swipeUp() }
        XCTAssertTrue(toggle.waitForExistence(timeout: 5), "Ask me to rate after watching is in Settings")
        XCTAssertEqual(toggle.value as? String, "0")
        shot("rp3-settings-off")

        // Restore the preference.
        var prefs = try preferences(user.token)
        prefs["ratingPrompt"] = true
        try api("PATCH", "/me", ["preferences": prefs], token: user.token)
        try eventually(10, "the preference is back on") { try preferences(user.token)["ratingPrompt"] as? Bool == true }
    }

    /// With the server's switch off, nothing asks.
    func testNoPromptWhenServerOff() throws {
        let user = try temporaryUser()
        try setServerPrompt(false)
        try prepare(user.token)
        signIn(user)
        try playToEnd(serverPrompt: false)

        XCTAssertTrue(app.buttons["Close player"].waitForNonExistence(timeout: 45), "the player closes at the end")
        XCTAssertTrue(playButton.waitForExistence(timeout: 10), "back on the item page")
        sleep(3)
        XCTAssertFalse(byID("ratingPrompt").exists, "no rating request")
        shot("rp4-no-card")
        XCTAssertNil(try userRating(user.token))
    }
}
