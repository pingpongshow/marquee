import XCTest

/// Bazarr subtitles, remote control, subtitle appearance, year in music, Muse for movies,
/// recommendation rows and library health (META-12, USER-14, PLAY-20, MUSIC-22, USER-15,
/// USER-16, ADM-11), against the local test server. Each test uses a temporary account (or
/// puts back what it changes), since other clients' tests share the server.
final class Batch2UITests: XCTestCase {
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

    /// Calls the server (as the admin unless a token is given) and returns the JSON.
    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> Any? {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
        req.httpMethod = method
        req.timeoutInterval = 70
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
        wait(for: [done], timeout: 80)
        return out
    }

    private func object(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> [String: Any] {
        try api(method, path, body, token: token) as? [String: Any] ?? [:]
    }

    private func list(_ path: String, token: String? = nil) throws -> [[String: Any]] {
        try api("GET", path, token: token) as? [[String: Any]] ?? []
    }

    /// A request that doesn't need an expectation, for teardown blocks (which run even when a
    /// failure stops the test, unlike defer).
    private static func send(_ method: String, _ url: String, token: String?, _ body: [String: Any]? = nil) {
        guard let token, let u = URL(string: url) else { return }
        var req = URLRequest(url: u)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        }
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { _, _, _ in done.signal() }.resume()
        _ = done.wait(timeout: .now() + 15)
    }

    /// A temporary account, deleted when the test ends.
    private func temporaryUser(admin: Bool = false) throws -> (name: String, password: String, id: Int) {
        let name = "ui2\(admin ? "admin" : "user")-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try object("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": admin])
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", token = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: token) }
        return (name, pass, id)
    }

    /// Signs in over the API as another device of the same person; returns its token.
    private func deviceToken(_ name: String, _ pass: String, device: String, platform: String) throws -> String {
        let r = try object("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": device, "platform": platform]])
        return try XCTUnwrap(r["token"] as? String)
    }

    /// The device id behind a token.
    private func deviceID(of token: String) throws -> Int {
        try XCTUnwrap(try list("/devices", token: token).first { $0["current"] as? Bool == true }?["id"] as? Int)
    }

    // MARK: - App helpers

    private func connect() {
        let address = app.textFields["Home address, e.g. 10.1.1.10:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.tap()
        address.typeText(server)
        app.buttons["Connect"].tap()
    }

    private func signIn(username: String, password: String) {
        connect()
        let other = app.buttons["Sign in with username and password"]
        XCTAssertTrue(other.waitForExistence(timeout: 15))
        other.tap()
        let field = app.textFields["Username"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(username)
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText(password)
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
    }

    private func signInAsKiddo() {
        connect()
        XCTAssertTrue(app.staticTexts["Who's watching?"].waitForExistence(timeout: 15))
        app.buttons["Kiddo"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
    }

    /// iPhone: Libraries tab → library. iPad: the sidebar.
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
        for _ in 0..<5 where !movie.isHittable { app.swipeUp() }
        movie.tap()
    }

    private func openSettings() {
        app.buttons["Settings"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 10))
    }

    private func scrollTo(_ e: XCUIElement, max: Int = 8) {
        for _ in 0..<max where !(e.exists && e.isHittable) { app.swipeUp() }
    }

    /// Waits for a condition checked against the server.
    private func eventually(_ timeout: TimeInterval, _ what: @autoclosure () -> String, _ check: () throws -> Bool) rethrows {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if try check() { return }
            Thread.sleep(forTimeInterval: 1)
        }
        XCTFail("timed out: \(what())")
    }

    // MARK: - Bazarr (META-12)

    /// The item's Bazarr section: download another language (the fake Bazarr writes a .srt that
    /// shows up as a subtitle track), then search every provider and download a result.
    func testBazarrSubtitles() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        openMovie("31 Ocean")
        let find = app.buttons["trackChip.find"]
        XCTAssertTrue(find.waitForExistence(timeout: 10))
        find.tap()
        let another = app.buttons["Download Another Language…"]
        XCTAssertTrue(another.waitForExistence(timeout: 15), "Bazarr manages 31 Ocean")
        XCTAssertTrue(app.buttons["Search All Providers"].exists)
        shot("bz1-panel")
        // A language other clients' tests are unlikely to have fetched already.
        let danish = app.buttons["Danish"]
        for _ in 0..<3 where !danish.exists {
            another.tap()
            _ = danish.waitForExistence(timeout: 3)
        }
        XCTAssertTrue(danish.exists)
        danish.tap()
        let message = app.descendants(matching: .any)["bazarrMessage"]
        XCTAssertTrue(message.waitForExistence(timeout: 15))
        XCTAssertTrue(message.label.contains("Danish"), message.label)
        shot("bz2-requested")
        // The subtitle reaches the item once the server has seen the new file.
        try eventually(60, "a Danish subtitle on 31 Ocean") {
            let item = try object("GET", "/items/6")
            let streams = ((item["versions"] as? [[String: Any]])?.first?["files"] as? [[String: Any]])?.first?["streams"] as? [[String: Any]] ?? []
            return streams.contains { $0["kind"] as? String == "subtitle" && ["da", "dan"].contains($0["language"] as? String ?? "") }
        }

        // Search every provider (slow), then download the second result.
        app.buttons["Search All Providers"].tap()
        let pick = app.buttons["Download from podnapisi"]
        XCTAssertTrue(pick.waitForExistence(timeout: 70), "the search lists the fake's results")
        XCTAssertTrue(app.staticTexts["Score 96"].exists)
        shot("bz3-results")
        pick.tap()
        expectation(for: NSPredicate(format: "label CONTAINS 'downloading'"), evaluatedWith: message)
        waitForExpectations(timeout: 15)
        shot("bz4-picked")
        app.buttons["Close"].tap()
        XCTAssertTrue(find.waitForExistence(timeout: 5))
    }

    // MARK: - Remote control (USER-14)

    /// This phone as the player: another device of the same person lists it, sends play, pause,
    /// seek and stop, and sees the state it reports.
    func testRemoteControlledPhone() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        let controller = try deviceToken(user.name, user.password, device: "UITest Controller", platform: "web")
        // The app's own device (the server can still list players of deleted test accounts
        // whose ids were reused, so it's found by id, not by platform).
        let id = try XCTUnwrap(try list("/devices", token: controller).first { ["ios", "ipados"].contains($0["platform"] as? String ?? "") }?["id"] as? Int)
        try eventually(60, "the app listed as a player") {
            try list("/remote/players", token: controller).contains { $0["deviceId"] as? Int == id }
        }
        func state() throws -> [String: Any] {
            try object("GET", "/remote/players/\(id)", token: controller)["state"] as? [String: Any] ?? [:]
        }
        func command(_ body: [String: Any]) throws {
            try api("POST", "/remote/players/\(id)/commands", body, token: controller)
        }

        // Play a movie.
        try command(["type": "play", "itemIds": [322], "startMs": 5000])
        let toast = app.descendants(matching: .any).matching(identifier: "remoteToast").firstMatch
        XCTAssertTrue(toast.waitForExistence(timeout: 30), "the toast names the controller")
        XCTAssertTrue(toast.label.hasPrefix("Controlled from "), toast.label)
        let close = app.buttons["Close player"]
        XCTAssertTrue(close.waitForExistence(timeout: 15))
        let skipAll = app.buttons["Skip All"]
        if skipAll.waitForExistence(timeout: 3) { skipAll.tap() }
        shot("rc1-playing-from-remote")
        try eventually(20, "state playing 00 Long Test") {
            let s = try state()
            return s["itemId"] as? Int == 322 && s["state"] as? String == "playing"
        }
        try command(["type": "pause"])
        try eventually(15, "state paused") { try state()["state"] as? String == "paused" }
        try command(["type": "seek", "positionMs": 20000])
        var seen: [String: Any] = [:]
        try eventually(15, "position near 20 s: \(seen)") {
            seen = try state()
            let p = seen["positionMs"] as? Int ?? 0
            return p >= 18000 && p <= 23000
        }
        XCTAssertNotNil(seen["positionMs"], "\(seen)")
        try command(["type": "resume"])
        try eventually(15, "playing again") { try state()["state"] as? String == "playing" }
        shot("rc2-after-commands")
        try command(["type": "stop"])
        XCTAssertTrue(close.waitForNonExistence(timeout: 15), "stop closes the player")

        // Music: a track queue starting at the second track.
        let children = try object("GET", "/items/324/children", token: controller)["items"] as? [[String: Any]] ?? []
        let ids = children.compactMap { $0["id"] as? Int }
        XCTAssertGreaterThanOrEqual(ids.count, 2)
        try command(["type": "play", "itemIds": ids, "index": 1])
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        try eventually(20, "the second track playing") {
            let s = try state()
            return s["itemId"] as? Int == ids[1] && s["queueIndex"] as? Int == 1 && s["queueLength"] as? Int == ids.count
        }
        try command(["type": "next"])
        try eventually(20, "the third track") { try state()["itemId"] as? Int == ids[2] }
        shot("rc3-music-from-remote")
        try command(["type": "pause"])
        try eventually(15, "music paused") { try state()["state"] as? String == "paused" }
    }

    /// A fake player polling its inbox (as a TV would), so the commands this phone sends can be
    /// checked. It reports itself playing whatever it was last told to play.
    private final class FakePlayer: @unchecked Sendable {
        private let lock = NSLock()
        private var _commands: [[String: Any]] = []
        private var stopped = false
        let base: String
        let token: String
        init(base: String, token: String) { self.base = base; self.token = token }

        var commands: [[String: Any]] { lock.lock(); defer { lock.unlock() }; return _commands }
        func stop() { lock.lock(); stopped = true; lock.unlock() }
        private var isStopped: Bool { lock.lock(); defer { lock.unlock() }; return stopped }

        func start() {
            Thread {
                var cursor = 0
                var state: [String: Any] = ["state": "idle", "positionMs": 0]
                while !self.isStopped {
                    guard let r = self.call("POST", "/remote/inbox", ["cursor": cursor, "capabilities": ["video", "music"], "state": state]) as? [String: Any] else {
                        Thread.sleep(forTimeInterval: 1)
                        continue
                    }
                    cursor = r["cursor"] as? Int ?? cursor
                    for c in r["commands"] as? [[String: Any]] ?? [] {
                        self.lock.lock(); self._commands.append(c); self.lock.unlock()
                        switch c["type"] as? String {
                        case "play":
                            let id = (c["itemIds"] as? [Int])?.first ?? 0
                            let item = self.call("GET", "/items/\(id)", nil) as? [String: Any] ?? [:]
                            state = ["state": "playing", "itemId": id, "itemType": item["type"] ?? "movie", "title": item["title"] ?? "",
                                     "positionMs": 1000, "durationMs": item["durationMs"] ?? 60000, "artItemId": id]
                        case "pause": state["state"] = "paused"
                        case "resume": state["state"] = "playing"
                        case "stop": state = ["state": "stopped", "positionMs": 0]
                        default: break
                        }
                        _ = self.call("PUT", "/remote/state", state)
                    }
                }
            }.start()
        }

        private func call(_ method: String, _ path: String, _ body: [String: Any]?) -> Any? {
            var req = URLRequest(url: URL(string: base + path)!)
            req.httpMethod = method
            req.timeoutInterval = 40
            req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
            if let body {
                req.setValue("application/json", forHTTPHeaderField: "Content-Type")
                req.httpBody = try? JSONSerialization.data(withJSONObject: body)
            }
            let done = DispatchSemaphore(value: 0)
            var out: Any?
            URLSession.shared.dataTask(with: req) { data, _, _ in
                out = data.flatMap { try? JSONSerialization.jsonObject(with: $0) }
                done.signal()
            }.resume()
            _ = done.wait(timeout: .now() + 45)
            return out
        }
    }

    /// This phone as the controller: "Play on…" lists a fake TV, sends it the movie, and the
    /// remote shows what the TV reports and sends Pause.
    func testPlayOnAndRemote() throws {
        let user = try temporaryUser()
        let tvToken = try deviceToken(user.name, user.password, device: "UITest Fake TV", platform: "tvos")
        let fake = FakePlayer(base: "http://\(server)/api/v1", token: tvToken)
        fake.start()
        addTeardownBlock { fake.stop() }
        let fakeID = try deviceID(of: tvToken)
        let checker = try deviceToken(user.name, user.password, device: "UITest Check", platform: "web")
        var name = ""
        try eventually(30, "the fake TV listed") {
            name = try list("/remote/players", token: checker).first { $0["deviceId"] as? Int == fakeID }?["name"] as? String ?? ""
            return !name.isEmpty
        }
        signIn(username: user.name, password: user.password)
        openMovie("20 Valley")
        let playOn = app.buttons["Play on…"]
        XCTAssertTrue(playOn.waitForExistence(timeout: 10))
        // At the end of the actions row, which scrolls sideways on a phone.
        for _ in 0..<3 where playOn.frame.maxX > app.frame.maxX - 4 { app.buttons["Download"].firstMatch.swipeLeft() }
        playOn.tap()
        let tv = app.buttons["player-\(name)"]
        XCTAssertTrue(tv.waitForExistence(timeout: 15), "the fake TV is listed")
        shot("po1-players")
        tv.tap()
        try eventually(15, "the TV got play") { fake.commands.contains { $0["type"] as? String == "play" && ($0["itemIds"] as? [Int]) == [4] } }
        let title = app.staticTexts["remoteTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 30), "the remote shows what the TV plays")
        XCTAssertEqual(title.label, "20 Valley")
        XCTAssertEqual(app.staticTexts["remoteState"].label, "Playing")
        shot("po2-remote")
        app.buttons["Pause"].tap()
        try eventually(15, "the TV got pause") { fake.commands.contains { $0["type"] as? String == "pause" } }
        expectation(for: NSPredicate(format: "label == 'Paused'"), evaluatedWith: app.staticTexts["remoteState"])
        waitForExpectations(timeout: 30)
        app.buttons["Forward 30 seconds"].tap()
        try eventually(15, "the TV got seek") { fake.commands.contains { $0["type"] as? String == "seek" } }
        app.buttons["Stop"].tap()
        try eventually(15, "the TV got stop") { fake.commands.contains { $0["type"] as? String == "stop" } }
        shot("po3-stopped")
        app.buttons["Disconnect"].tap()
        XCTAssertTrue(tv.waitForExistence(timeout: 5), "back at the player list")
        app.buttons["Close"].tap()

        // Settings → Remote Control lists it too.
        openSettings()
        let remote = app.buttons["Remote Control"]
        scrollTo(remote)
        remote.tap()
        XCTAssertTrue(app.buttons["player-\(name)"].waitForExistence(timeout: 15))
        shot("po4-settings-remote")
    }

    // MARK: - Subtitle appearance (PLAY-20)

    func testSubtitleAppearance() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        let token = try deviceToken(user.name, user.password, device: "UITest Check", platform: "web")
        openSettings()
        let link = app.buttons["Subtitle Appearance"]
        scrollTo(link)
        link.tap()
        let preview = app.descendants(matching: .any)["subtitlePreview"]
        XCTAssertTrue(preview.waitForExistence(timeout: 10))
        XCTAssertEqual(preview.value as? String, "medium, #FFFFFF, outline, bottom")
        func choose(_ picker: String, _ option: String) {
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", picker)).firstMatch.tap()
            let o = app.buttons[option].firstMatch
            XCTAssertTrue(o.waitForExistence(timeout: 5), option)
            o.tap()
        }
        choose("Size", "Large")
        choose("Colour", "Yellow")
        choose("Background", "Solid box")
        choose("Position", "Raised")
        XCTAssertEqual(preview.value as? String, "large, #FFFF00, opaque, raised")
        shot("sa1-appearance")
        try eventually(10, "saved to preferences") {
            let style = (try object("GET", "/me", token: token)["preferences"] as? [String: Any])?["subtitleStyle"] as? [String: Any] ?? [:]
            return style["size"] as? String == "large" && style["color"] as? String == "#FFFF00"
                && style["background"] as? String == "opaque" && style["position"] as? String == "raised"
        }
        // Other preferences are kept.
        let prefs = try object("GET", "/me", token: token)["preferences"] as? [String: Any] ?? [:]
        XCTAssertEqual(prefs["cinemaTrailers"] as? Bool, true)

        // Applied: a video plays with it (and coming back shows the saved style).
        app.navigationBars.buttons.firstMatch.tap()
        openMovie("00 Long Test")
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        let skipAll = app.buttons["Skip All"]
        if skipAll.waitForExistence(timeout: 3) { skipAll.tap() }
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 15))
        sleep(3)
        shot("sa2-playing")
        app.buttons["Close player"].tap()
        openSettings()
        scrollTo(link)
        link.tap()
        XCTAssertTrue(preview.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "value == 'large, #FFFF00, opaque, raised'"), evaluatedWith: preview)
        waitForExpectations(timeout: 10)
        app.buttons["Reset to Default"].tap()
        try eventually(10, "reset") {
            let style = (try object("GET", "/me", token: token)["preferences"] as? [String: Any])?["subtitleStyle"] as? [String: Any] ?? [:]
            return style["size"] as? String ?? "medium" == "medium" && style["position"] as? String ?? "bottom" == "bottom"
        }
    }

    // MARK: - Year in music (MUSIC-22)

    /// Kiddo's recap: every page, Play your top songs and Save as playlist (the playlist is
    /// deleted afterwards unless it was already there).
    func testYearInMusic() throws {
        let kid = try object("POST", "/auth/pin", ["userId": 2, "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Recap", "platform": "web"]])
        let kidToken = try XCTUnwrap(kid["token"] as? String)
        let year = try XCTUnwrap((try api("GET", "/me/recap/years", token: kidToken) as? [Int])?.first)
        let title = "Your Top Songs \(year)"
        let existed = try list("/playlists", token: kidToken).contains { $0["title"] as? String == title }
        let base = "http://\(server)/api/v1"
        addTeardownBlock {
            guard !existed else { return }
            // Find and delete the playlist this test made.
            var req = URLRequest(url: URL(string: base + "/playlists")!)
            req.setValue("Bearer \(kidToken)", forHTTPHeaderField: "Authorization")
            let done = DispatchSemaphore(value: 0)
            var id: Int?
            URLSession.shared.dataTask(with: req) { data, _, _ in
                let all = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [[String: Any]]) ?? []
                id = all.first { $0["title"] as? String == title }?["id"] as? Int
                done.signal()
            }.resume()
            _ = done.wait(timeout: .now() + 15)
            if let id { Self.send("DELETE", base + "/playlists/\(id)", token: kidToken) }
        }

        signInAsKiddo()
        openLibrary("Music")
        let card = app.buttons["recapCard"]
        XCTAssertTrue(card.waitForExistence(timeout: 15), "the Music page offers the recap")
        shot("ym1-card")
        card.tap()
        let pages = ["minutes", "topArtist", "topArtists", "topSongs", "topAlbums", "genres", "when", "days", "discovered", "summary"]
        var seen: [String] = []
        for (i, p) in pages.enumerated() {
            let page = app.descendants(matching: .any)["recap-\(p)"]
            if p == "genres", !page.waitForExistence(timeout: 2) { continue } // no genres in the test library
            XCTAssertTrue(page.waitForExistence(timeout: 10), "page \(p)")
            seen.append(p)
            shot("ym2-\(i)-\(p)")
            if p != "summary" { app.swipeLeft() }
            sleep(1)
        }
        XCTAssertGreaterThanOrEqual(seen.count, 9)
        let save = app.buttons["Save as Playlist"]
        XCTAssertTrue(save.waitForExistence(timeout: 5))
        save.tap()
        let saved = app.descendants(matching: .any)["recapSaved"]
        XCTAssertTrue(saved.waitForExistence(timeout: 15))
        XCTAssertTrue(saved.label.contains(title), saved.label)
        XCTAssertTrue(try list("/playlists", token: kidToken).contains { $0["title"] as? String == title })
        shot("ym3-saved")
        app.buttons["Open"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars[title].waitForExistence(timeout: 10) || app.staticTexts[title].waitForExistence(timeout: 5))
        app.buttons["Close"].firstMatch.tap()
        app.buttons["Play Your Top Songs"].firstMatch.tap()
        app.buttons["Close recap"].tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20), "top songs play")
        app.buttons["Pause"].firstMatch.tap()
    }

    // MARK: - Muse for movies (USER-15)

    func testVideoMuse() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        app.buttons["Muse"].firstMatch.tap()
        let prompt = app.textFields["musePrompt"]
        XCTAssertTrue(prompt.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["family animation"].exists, "example chips")
        prompt.tap()
        prompt.typeText("movies about the ocean\n")
        let understood = app.descendants(matching: .any)["museUnderstood"]
        XCTAssertTrue(understood.waitForExistence(timeout: 30))
        XCTAssertTrue(understood.label.contains("ocean"), understood.label)
        let result = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '31 Ocean'")).firstMatch
        XCTAssertTrue(result.waitForExistence(timeout: 10))
        shot("mu1-results")
        app.buttons["Save as Playlist"].tap()
        XCTAssertTrue(app.descendants(matching: .any)["museSaved"].waitForExistence(timeout: 10))
        let token = try deviceToken(user.name, user.password, device: "UITest Check", platform: "web")
        let saved = try list("/playlists", token: token).first { $0["title"] as? String == "Muse: movies about the ocean" }
        XCTAssertEqual(saved?["kind"] as? String, "video")
        result.tap()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch.waitForExistence(timeout: 10))

        // Search's Muse mode.
        app.buttons["Search"].firstMatch.tap()
        let mode = app.buttons["Muse"].firstMatch
        XCTAssertTrue(mode.waitForExistence(timeout: 10))
        mode.tap()
        let field = app.searchFields.firstMatch
        field.tap()
        field.typeText("something from the 80s\n")
        XCTAssertTrue(app.descendants(matching: .any)["museUnderstood"].waitForExistence(timeout: 30))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label BEGINSWITH '20 Valley'")).firstMatch.waitForExistence(timeout: 10))
        shot("mu2-search-muse")
    }

    // MARK: - Recommendations (USER-16)

    /// Having watched something, Home has "Because you watched" and "Recommended for You"; Edit
    /// Home lists both rows and can hide one.
    func testRecommendationRows() throws {
        let user = try temporaryUser()
        let token = try deviceToken(user.name, user.password, device: "UITest Check", platform: "web")
        try api("POST", "/items/359/watched", token: token)
        signIn(username: user.name, password: user.password)
        let because = app.descendants(matching: .any)["Because you watched 00 Preview Test"].firstMatch
        let recommended = app.descendants(matching: .any)["Recommended for You"].firstMatch
        let hubs = try list("/hubs/home", token: token).compactMap { $0["id"] as? String }
        if !hubs.contains("recommended") && !hubs.contains(where: { $0.hasPrefix("because-") }) {
            throw XCTSkip("the server returned no recommendation rows for a new account")
        }
        XCTAssertTrue(recommended.waitForExistence(timeout: 15) || because.waitForExistence(timeout: 5))
        shot("rec1-home")
        // "More like this" on an item page.
        app.buttons["Edit Home"].tap()
        let hide = app.buttons["Hide Recommended for You"]
        XCTAssertTrue(hide.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Because You Watched"].exists)
        shot("rec2-edit-home")
        hide.tap()
        XCTAssertTrue(app.buttons["Show Recommended for You"].waitForExistence(timeout: 5))
        app.buttons["Done"].tap()
        if hubs.contains("recommended") {
            XCTAssertTrue(recommended.waitForNonExistence(timeout: 10), "the hidden row is gone")
        }
        shot("rec3-hidden")
    }

    // MARK: - Library health (ADM-11)

    func testLibraryHealth() throws {
        let admin = try temporaryUser(admin: true)
        signIn(username: admin.name, password: admin.password)
        openSettings()
        let health = app.buttons["Library Health"]
        scrollTo(health)
        health.tap()
        let missing = app.descendants(matching: .any)["check-missingSubtitles"].firstMatch
        XCTAssertTrue(missing.waitForExistence(timeout: 15))
        shot("lh1-checks")
        missing.tap()
        let actions = app.buttons["Actions for 15 Thunder"]
        XCTAssertTrue(actions.waitForExistence(timeout: 15), "15 Thunder is missing subtitles")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Missing English'")).firstMatch.exists)
        shot("lh2-issues")

        // Download with Bazarr opens the Bazarr panel for it (nothing is downloaded).
        actions.tap()
        app.buttons["Download with Bazarr…"].tap()
        XCTAssertTrue(app.buttons["Download English"].waitForExistence(timeout: 15))
        shot("lh3-bazarr")
        app.buttons["Close"].tap()

        // Ignore, then undo (put back whatever happens).
        let url = "http://\(server)/api/v1/library-health/missingSubtitles/ignored/5", token = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: token) }
        actions.tap()
        app.buttons["Ignore"].tap()
        XCTAssertTrue(actions.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["Ignored 15 Thunder"].waitForExistence(timeout: 5))
        sleep(1)
        let page = try object("GET", "/library-health/missingSubtitles")
        XCTAssertFalse((page["items"] as? [[String: Any]] ?? []).contains { ($0["item"] as? [String: Any])?["id"] as? Int == 5 }, "ignored on the server")
        shot("lh4-ignored")
        app.buttons["Undo"].tap()
        XCTAssertTrue(actions.waitForExistence(timeout: 10))
        let again = try object("GET", "/library-health/missingSubtitles")
        XCTAssertTrue((again["items"] as? [[String: Any]] ?? []).contains { ($0["item"] as? [String: Any])?["id"] as? Int == 5 }, "reported again")

        // Open goes to the item page.
        actions.tap()
        app.buttons["Open"].tap()
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch.waitForExistence(timeout: 10))
        app.navigationBars.buttons.firstMatch.tap()
        app.navigationBars.buttons.firstMatch.tap()

        // Integrations shows Bazarr's address and that its key is set (nothing is saved).
        app.navigationBars.buttons.firstMatch.tap()
        let integrations = app.buttons["Integrations"]
        scrollTo(integrations)
        integrations.tap()
        let url2 = app.textFields["bazarrURL"]
        XCTAssertTrue(url2.waitForExistence(timeout: 10))
        let settings = try object("GET", "/settings")["integrations"] as? [String: Any] ?? [:]
        expectation(for: NSPredicate(format: "value == %@", settings["bazarrUrl"] as? String ?? ""), evaluatedWith: url2)
        waitForExpectations(timeout: 10)
        XCTAssertEqual(app.secureTextFields["bazarrKey"].placeholderValue, "API key (set)")
        shot("lh5-integrations")
    }
}
