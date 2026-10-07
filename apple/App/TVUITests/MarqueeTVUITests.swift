import XCTest

/// Apple TV flows: find the server with Bonjour, sign in with Quick Connect (approved
/// through the API, as a phone would), then browse and play.
final class MarqueeTVUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }

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

    /// Moves focus with the remote until the element has it (searching up, then down).
    private func focus(_ element: XCUIElement, direction: XCUIRemote.Button = .down, tries: Int = 12) {
        let order: [XCUIRemote.Button] = direction == .down ? Array(repeating: .up, count: 8) + Array(repeating: .down, count: tries + 8) : Array(repeating: direction, count: tries)
        for d in order {
            if element.hasFocus { return }
            remote.press(d)
        }
        XCTAssertTrue(element.hasFocus, "couldn't focus \(element)")
    }

    /// Other servers on the network may be listed beside ours: go to the row of server
    /// cards, start at its left end and move right.
    private func focusServer(_ element: XCUIElement) {
        remote.press(.up)
        for _ in 0..<4 { remote.press(.left) }
        focus(element, direction: .right, tries: 6)
    }

    private func approve(code: String, as userToken: String? = nil) throws {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        let token = userToken ?? admin
        var req = URLRequest(url: URL(string: server + "/api/v1/auth/quickconnect/authorize")!)
        req.httpMethod = "POST"
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code])
        let done = expectation(description: "approve")
        var status = 0
        URLSession.shared.dataTask(with: req) { _, resp, _ in
            status = (resp as? HTTPURLResponse)?.statusCode ?? 0
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        XCTAssertEqual(status, 200)
    }

    /// Connects with Bonjour and signs in with Quick Connect, ending on Home.
    private func signIn(as userToken: String? = nil) throws {
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        // A test server on this Mac shares the mDNS port with the system responder, so
        // Bonjour can miss it: fall back to typing its address.
        if found.waitForExistence(timeout: 20) {
            focusServer(found)
            remote.press(.select)
        } else {
            let enter = app.buttons["Enter an address"]
            focus(enter)
            remote.press(.select)
            let field = app.textFields["Home address, e.g. 10.1.1.10:32500"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            focus(field)
            remote.press(.select)
            app.typeText(server.replacingOccurrences(of: "http://", with: "") + "\n")
            let connect = app.buttons["Connect"]
            if !app.buttons["Use Quick Connect"].waitForExistence(timeout: 8), connect.exists {
                focus(connect)
                remote.press(.select)
            }
        }
        let qc = app.buttons["Use Quick Connect"]
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        focus(qc)
        remote.press(.select)
        let codeText = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        try approve(code: codeText.label.replacingOccurrences(of: " ", with: ""), as: userToken)
        XCTAssertTrue(homeShows())
    }

    /// Stations and Now Playing with lyrics on the TV (M6.5).
    func testMusicStation() throws {
        try signIn()
        let musicTab = app.buttons["Music"].firstMatch
        XCTAssertTrue(musicTab.waitForExistence(timeout: 10))
        focus(musicTab, direction: .right)
        remote.press(.select)
        // The landing page: quick actions first, the Library list at the bottom (MUSIC-15).
        XCTAssertTrue(app.buttons["musicLibrary.artists"].waitForExistence(timeout: 10))
        let radio = app.buttons["Library Radio"]
        XCTAssertTrue(radio.waitForExistence(timeout: 10))
        // Down to the quick actions (focus lands wherever is closest), then left along them.
        let actions = ["Library Radio", "Shuffle All", "Muse"]
        let focused = app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true"))
        for _ in 0..<8 where !actions.contains(where: { focused.label.hasPrefix($0) }) { remote.press(.down) }
        focus(radio, direction: .left)
        sleep(1)
        shot("tv-m1-discover")
        remote.press(.select)

        let nowPlaying = app.buttons["Now Playing"].firstMatch
        XCTAssertTrue(nowPlaying.waitForExistence(timeout: 20))
        for _ in 0..<6 where !nowPlaying.hasFocus { remote.press(.up) }
        focus(nowPlaying, direction: .right)
        remote.press(.select)
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 10))
        sleep(2)
        shot("tv-m2-now-playing")
        let lyrics = app.buttons["Lyrics"]
        if lyrics.waitForExistence(timeout: 5) {
            // Out of the queue (right half) to the player's controls, then down to the bottom row.
            let focused = app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true"))
            for _ in 0..<15 where !lyrics.hasFocus {
                let f = focused.frame
                if f.maxY < 200 { remote.press(.down) } // still in the tab bar
                else if f.minX > app.frame.midX || abs(f.midY - lyrics.frame.midY) < 10 { remote.press(.left) }
                else { remote.press(.down) }
            }
            XCTAssertTrue(lyrics.hasFocus)
            remote.press(.select)
            XCTAssertTrue(app.staticTexts["Lyrics"].waitForExistence(timeout: 5))
            sleep(2)
            shot("tv-m3-lyrics")
        }
    }

    func testQuickConnectBrowseAndPlay() throws {
        // Bonjour finds the server.
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        XCTAssertTrue(found.waitForExistence(timeout: 20))
        shot("tv-01-connect")
        focusServer(found)
        remote.press(.select)

        // Quick Connect.
        let qc = app.buttons["Use Quick Connect"]
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        shot("tv-02-who-is-watching")
        focus(qc)
        remote.press(.select)
        let codeText = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        shot("tv-03-quick-connect")
        try approve(code: codeText.label.replacingOccurrences(of: " ", with: ""))

        // Signed in: Home.
        XCTAssertTrue(homeShows())
        sleep(2)
        shot("tv-04-home")

        // Open the first poster (below the row title) and play it. A Continue Watching tile
        // plays at once; anything else opens its page first.
        let before = try activeSessionIDs()
        let poster = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Preview Test' OR label CONTAINS[c] 'Long Test'")).firstMatch
        if poster.waitForExistence(timeout: 5) { focus(poster) } else { remote.press(.down) }
        remote.press(.select)
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume', 'Continue'}")).firstMatch
        if play.waitForExistence(timeout: 8) {
            sleep(1)
            shot("tv-05-detail")
            focus(play, direction: .down)
            remote.press(.select)
        }
        sleep(6)
        shot("tv-06-playing")
        // A new session, not a higher count: another session (an earlier test's, timing out)
        // can end meanwhile and leave the count where it was.
        var fresh = try activeSessionIDs().subtracting(before)
        for _ in 0..<10 where fresh.isEmpty {
            sleep(1)
            fresh = try activeSessionIDs().subtracting(before)
        }
        XCTAssertFalse(fresh.isEmpty, "nothing started playing")
        remote.press(.menu)
    }

    /// Signed in: Home's rows show (which come first depends on the server's Home layout).
    private func homeShows() -> Bool {
        let row = app.staticTexts.matching(NSPredicate(format: "label IN {'Continue Watching', 'Recently Added Movies', 'Recommended for You'}")).firstMatch
        return row.waitForExistence(timeout: 20)
    }

    /// How many playback sessions the server has (admin view).
    private func activeSessionIDs() throws -> Set<String> {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: server + "/api/v1/playback/sessions")!)
        req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
        let done = expectation(description: "sessions")
        var ids = Set<String>()
        URLSession.shared.dataTask(with: req) { data, _, _ in
            let list = ((try? JSONSerialization.jsonObject(with: data ?? Data())) as? [[String: Any]]) ?? []
            ids = Set(list.compactMap { ($0["id"] ?? $0["sessionId"]).map { "\($0)" } })
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return ids
    }

    /// Top Shelf links (marquee://item/<id>) open the item's page.
    func testTopShelfLink() throws {
        try signIn()
        app.launchArguments = [] // opening the link relaunches the app: keep the sign-in
        app.open(URL(string: "marquee://item/359")!)
        let title = app.staticTexts["00 Preview Test"].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch.waitForExistence(timeout: 5))
        shot("tv-topshelf-link")
    }

    /// The Trailer button on a movie's page (PLAY-22) plays its local trailer (362, Official
    /// Trailer) in the normal player.
    func testTrailerButtonPlaysLocalTrailer() throws {
        try signIn()
        app.launchArguments = [] // opening the link relaunches the app: keep the sign-in
        app.open(URL(string: "marquee://item/359")!)
        let trailer = app.buttons["trailerButton"]
        XCTAssertTrue(trailer.waitForExistence(timeout: 15), "the Trailer button shows for 00 Preview Test")
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 5))
        sleep(1)
        focus(play, direction: .down)
        focus(trailer, direction: .right, tries: 4)
        shot("tv-trailer-1-page")
        let before = try trailerSessions()
        remote.press(.select)
        var now = try trailerSessions()
        for _ in 0..<15 where now <= before {
            sleep(1)
            now = try trailerSessions()
        }
        XCTAssertGreaterThan(now, before, "the local trailer plays")
        shot("tv-trailer-2-playing")
        remote.press(.menu)
    }

    /// The server's playback sessions of the local trailer (item 362).
    private func trailerSessions() throws -> Int {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: server + "/api/v1/playback/sessions")!)
        req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
        let done = expectation(description: "sessions")
        var n = 0
        URLSession.shared.dataTask(with: req) { data, _, _ in
            let list = ((try? JSONSerialization.jsonObject(with: data ?? Data())) as? [[String: Any]]) ?? []
            n = list.filter { ($0["itemId"] as? Int) == 362 }.count
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return n
    }

    /// Find subtitles on the title's page, beside its other track options: Bazarr's section
    /// (download another language) and OpenSubtitles in one sheet, driven with the remote.
    func testFindSubtitlesTV() throws {
        try signInTemporaryUser()
        app.launchArguments = []
        app.open(URL(string: "marquee://item/6")!) // 31 Ocean, managed by the fake Bazarr
        let find = app.buttons["trackChip.find"]
        XCTAssertTrue(find.waitForExistence(timeout: 15))
        sleep(1)
        // Play, down to the track chips' row, then right along it to Find subtitles.
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        focus(play, direction: .down)
        remote.press(.down)
        for _ in 0..<4 where !find.hasFocus { remote.press(.right) }
        XCTAssertTrue(find.hasFocus, "Find subtitles focused")
        shot("tv-subs-1-page")
        remote.press(.select)
        let another = app.buttons["Download Another Language…"]
        XCTAssertTrue(another.waitForExistence(timeout: 15), "Bazarr's section in the sheet")
        XCTAssertTrue(app.buttons["Search All Providers"].exists)
        XCTAssertTrue(app.staticTexts["OpenSubtitles"].exists || app.otherElements["OpenSubtitles"].exists)
        shot("tv-subs-2-sheet")
        // Focus starts on Search All Providers; list rows don't report focus, so step to the next.
        remote.press(.down)
        remote.press(.select)
        let swedish = app.buttons["Swedish"].firstMatch
        XCTAssertTrue(swedish.waitForExistence(timeout: 5), "the language choices")
        focus(swedish, direction: .down)
        remote.press(.select)
        let message = app.descendants(matching: .any).matching(identifier: "bazarrMessage").firstMatch
        XCTAssertTrue(message.waitForExistence(timeout: 15))
        XCTAssertTrue(message.label.contains("Swedish"), message.label)
        shot("tv-subs-3-requested")
        remote.press(.menu)
        XCTAssertTrue(find.waitForExistence(timeout: 5))
    }

    /// A library's Muse and sort-and-filter buttons stay at the top right while the grid
    /// scrolls (tvOS hides the navigation bar), and Right from the grid reaches them.
    func testLibraryButtonsStayPinned() throws {
        _ = try signInTemporaryUser()
        remote.press(.up)
        let tab = app.buttons["Movies"].firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 10))
        focus(tab, direction: .right, tries: 6)
        remote.press(.select)
        XCTAssertTrue(app.descendants(matching: .any)["libraryGrid"].waitForExistence(timeout: 10))
        remote.press(.down)
        let sortMenu = app.descendants(matching: .any).matching(identifier: "sortMenu").firstMatch
        let muse = app.buttons["Muse"].firstMatch
        XCTAssertTrue(sortMenu.waitForExistence(timeout: 5))
        // Well down the grid.
        for _ in 0..<8 { remote.press(.down) }
        sleep(1)
        shot("tv-library-scrolled")
        let window = app.windows.firstMatch.frame
        for b in [muse, sortMenu] {
            XCTAssertTrue(b.exists, "\(b) still there")
            XCTAssertTrue(b.frame.minY >= 0 && b.frame.maxY < window.height * 0.5 && b.frame.minX > window.width * 0.7,
                          "\(b) pinned top right: \(b.frame) in \(window)")
        }
        // Right along the row reaches the sort menu (XCUITest doesn't report focus on these
        // buttons, so open it): five columns, then the button.
        for _ in 0..<6 { remote.press(.right) }
        remote.press(.select)
        sleep(1)
        shot("tv-library-menu")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Recently added")).firstMatch.waitForExistence(timeout: 5), "the sort and filter menu opened")
        remote.press(.menu)
        shot("tv-library-button-focused")
    }

    /// Live TV is a tab placed with the libraries, after the video ones, not a fixed second
    /// tab; it opens the guide.
    func testLiveTVTabWithLibraries() throws {
        _ = try signInTemporaryUser()
        remote.press(.up)
        let live = app.buttons["Live TV"].firstMatch
        XCTAssertTrue(live.waitForExistence(timeout: 10))
        sleep(1)
        shot("tv-live-tab")
        let home = app.buttons["Home"].firstMatch
        XCTAssertTrue(home.exists)
        // The test server's video libraries.
        let videos = ["Anime", "Movies", "Recorded TV"].map { app.buttons[$0].firstMatch }
        for v in videos {
            XCTAssertTrue(v.exists, "\(v) is a tab")
            XCTAssertLessThan(v.frame.midX, live.frame.midX, "Live TV comes after \(v.label)")
        }
        // Not straight after Home: libraries sit between them.
        let between = videos.filter { $0.frame.midX > home.frame.midX && $0.frame.midX < live.frame.midX }
        XCTAssertFalse(between.isEmpty, "Live TV isn't the second tab")
        focus(live, direction: .right, tries: 10)
        remote.press(.select)
        XCTAssertTrue(app.buttons["What's On"].firstMatch.waitForExistence(timeout: 15), "the Live TV screen opens")
        shot("tv-live-open")
    }

    // MARK: - Playback and profile extras on the TV (USER-12, PLAY-17, PLAY-19)

    /// Calls the server; returns the JSON object.
    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> [String: Any] {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
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
        wait(for: [done], timeout: 30)
        return out
    }

    /// A temporary account (other clients' tests share the admin's Home layout and offsets),
    /// signed in on the TV with Quick Connect and deleted when the test ends.
    private func signInTemporaryUser() throws -> String {
        let name = "uitv-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass])
        let id = try XCTUnwrap(user["id"] as? Int)
        let login = try api("POST", "/auth/login", ["username": name, "password": pass, "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "tvos"]])
        let url = server + "/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        let token = try XCTUnwrap(login["token"] as? String)
        try signIn(as: token)
        return token
    }

    /// Edit Home with the remote: hide a row with its button, then reset.
    func testEditHome() throws {
        let token = try signInTemporaryUser()
        // At the bottom of Home (rows load as they come into view).
        let edit = app.buttons["Edit Home"]
        // Edit Home sits at the left of the last line, beside Muse.
        let focusedNow = app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true"))
        for _ in 0..<20 where !(edit.exists && edit.hasFocus) { remote.press(focusedNow.label == "Muse" ? .left : .down) }
        XCTAssertTrue(edit.hasFocus, "focus is on \(focusedNow.label)")
        remote.press(.select)
        let hide = app.buttons["Hide Recently Added Movies"]
        XCTAssertTrue(hide.waitForExistence(timeout: 10))
        focus(hide)
        sleep(1)
        shot("tv-eh1-edit-home")
        remote.press(.select)
        XCTAssertTrue(app.buttons["Show Recently Added Movies"].waitForExistence(timeout: 5))
        let layout = try api("GET", "/me/home-layout", token: token)["rows"] as? [[String: Any]] ?? []
        XCTAssertEqual(layout.first { $0["id"] as? String == "recent-2" }?["hidden"] as? Bool, true)
        // Move that row down with its button (two along from Hide).
        let before = layout.firstIndex { $0["id"] as? String == "recent-2" } ?? -1
        remote.press(.right)
        remote.press(.right)
        XCTAssertTrue(app.buttons["Move Recently Added Movies down"].hasFocus)
        remote.press(.select)
        sleep(2)
        let moved = try api("GET", "/me/home-layout", token: token)["rows"] as? [[String: Any]] ?? []
        XCTAssertEqual(moved.firstIndex { $0["id"] as? String == "recent-2" }, before + 1, "moved down one place")
        shot("tv-eh2-changed")
        let reset = app.buttons["Reset to Default"]
        focus(reset)
        remote.press(.select)
        XCTAssertTrue(hide.waitForExistence(timeout: 5))
    }

    /// The player's transport bar: Speed and Subtitle/Audio Timing menus.
    func testPlayerMenus() throws {
        _ = try signInTemporaryUser()
        let movie = app.buttons["00 Long Test"].firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        for _ in 0..<4 where !movie.hasFocus && !app.buttons["00 Preview Test"].firstMatch.hasFocus { remote.press(.down) }
        focus(movie, direction: .right, tries: 4)
        remote.press(.select)
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        focus(play, direction: .down)
        remote.press(.select)
        sleep(8)
        // Up from the transport bar reaches the custom menu buttons.
        remote.press(.select)
        sleep(1)
        // Up from the scrubber to the custom menus: Speed, Subtitle Timing, Audio Timing. The
        // transport bar's buttons and menus aren't in the accessibility tree, so this is a smoke
        // test: the remote opens each menu and the screenshots show them.
        shot("tv-pm0-bar")
        remote.press(.up)
        sleep(1)
        remote.press(.select)
        sleep(1)
        shot("tv-pm1-speed-menu")
        remote.press(.menu)
        sleep(1)
        remote.press(.right)
        remote.press(.select)
        sleep(1)
        shot("tv-pm2-subtitle-timing-menu")
        remote.press(.menu)
        sleep(1)
        remote.press(.right)
        remote.press(.select)
        sleep(1)
        shot("tv-pm3-audio-timing-menu")
        remote.press(.menu)
        XCTAssertEqual(app.state, .runningForeground)
        remote.press(.menu)
        remote.press(.menu)
    }

    // MARK: - Batch 2 (USER-14, MUSIC-22, PLAY-20)

    private func list(_ path: String, token: String) throws -> [[String: Any]] {
        var req = URLRequest(url: URL(string: server + "/api/v1" + path)!)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let done = expectation(description: path)
        var out: [[String: Any]] = []
        URLSession.shared.dataTask(with: req) { data, _, _ in
            out = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [[String: Any]]) ?? []
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 30)
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

    /// The Apple TV as a remote-controlled player (USER-14): another device of the same person
    /// sends play, pause, seek and stop, and sees the state the TV reports.
    func testRemoteControlledTV() throws {
        let controller = try signInTemporaryUser() // the token that approved Quick Connect: another device
        let tvID = try XCTUnwrap(try list("/devices", token: controller).first { $0["current"] as? Bool != true && $0["platform"] as? String == "tvos" }?["id"] as? Int)
        try eventually(60, "the TV listed as a player") {
            try list("/remote/players", token: controller).contains { $0["deviceId"] as? Int == tvID }
        }
        func state() throws -> [String: Any] {
            try api("GET", "/remote/players/\(tvID)", token: controller)["state"] as? [String: Any] ?? [:]
        }
        func command(_ body: [String: Any]) throws { try api("POST", "/remote/players/\(tvID)/commands", body, token: controller) }
        try command(["type": "play", "itemIds": [322], "startMs": 5000])
        try eventually(30, "playing 00 Long Test") {
            let s = try state()
            if s["itemId"] as? Int != 322 { return false } // a cinema trailer may play first
            return s["state"] as? String == "playing"
        }
        sleep(2)
        shot("tv-rc1-playing")
        try command(["type": "pause"])
        try eventually(15, "paused") { try state()["state"] as? String == "paused" }
        try command(["type": "seek", "positionMs": 30000])
        try eventually(15, "at 30 s") {
            let p = try state()["positionMs"] as? Int ?? 0
            return p >= 28000 && p <= 33000
        }
        shot("tv-rc2-paused")
        try command(["type": "stop"])
        try eventually(15, "stopped") { (try state()["itemId"] as? Int) == nil }
        XCTAssertEqual(app.state, .runningForeground)
    }

    /// The year-in-music recap on the TV (MUSIC-22): Kiddo's, page by page with Next.
    func testRecapTV() throws {
        let kid = try api("POST", "/auth/pin", ["userId": 2, "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UI test", "platform": "tvos"]])
        try signIn(as: try XCTUnwrap(kid["token"] as? String))
        let musicTab = app.buttons["Music"].firstMatch
        XCTAssertTrue(musicTab.waitForExistence(timeout: 10))
        focus(musicTab, direction: .right)
        remote.press(.select)
        let card = app.buttons["recapCard"]
        XCTAssertTrue(card.waitForExistence(timeout: 15))
        sleep(1)
        // The card comes after the quick actions and the first shelves.
        for _ in 0..<4 { remote.press(.up) }
        for _ in 0..<8 where !card.hasFocus { remote.press(.down) }
        sleep(1)
        shot("tv-ym0-music")
        XCTAssertTrue(card.hasFocus)
        remote.press(.select)
        XCTAssertTrue(app.descendants(matching: .any)["recap-minutes"].waitForExistence(timeout: 15))
        sleep(1)
        shot("tv-ym1-minutes")
        let next = app.buttons["Next"]
        XCTAssertTrue(next.waitForExistence(timeout: 5))
        focus(next, direction: .right, tries: 4)
        var pages = 1
        for _ in 0..<12 where !app.descendants(matching: .any)["recap-summary"].exists {
            remote.press(.select)
            sleep(1)
            pages += 1
            if !next.hasFocus { focus(next, direction: .right, tries: 4) }
        }
        XCTAssertTrue(app.descendants(matching: .any)["recap-summary"].exists)
        XCTAssertGreaterThanOrEqual(pages, 9)
        shot("tv-ym2-summary")
        remote.press(.menu)
        XCTAssertTrue(card.waitForExistence(timeout: 10))
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
}
