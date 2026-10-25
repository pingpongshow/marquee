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
            let field = app.textFields["Home address, e.g. 192.168.1.20:32500"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            focus(field)
            remote.press(.select)
            app.typeText(server.replacingOccurrences(of: "http://", with: "") + "\n")
            let connect = app.buttons["Connect"]
            if !app.buttons["Use Quick Connect"].waitForExistence(timeout: 8), connect.exists {
                if app.keyboards.count > 0 { remote.press(.menu) } // the keyboard held the focus
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

    /// While music plays a Now Playing bar runs along the bottom of every page: down from the page
    /// focuses it, selecting it opens Now Playing over the page, and Back returns to the page.
    func testNowPlayingBar() throws {
        try signIn()
        XCTAssertTrue(app.buttons["Music"].firstMatch.waitForExistence(timeout: 10))
        app.openSection("Music")
        // Shuffle All plays from the library itself (Library Radio needs the server's radio).
        let radio = app.buttons["Library Radio"], shuffle = app.buttons["Shuffle All"]
        XCTAssertTrue(shuffle.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["nowPlayingBar"].exists, "the bar shows with nothing playing")
        let actions = ["Library Radio", "Shuffle All", "Muse"]
        let focused = app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true"))
        for _ in 0..<8 where !actions.contains(where: { focused.label.hasPrefix($0) }) { remote.press(.down) }
        if radio.exists { focus(radio, direction: .left) }
        focus(shuffle, direction: .right)
        remote.press(.select)

        let bar = app.buttons["nowPlayingBar"]
        XCTAssertTrue(bar.waitForExistence(timeout: 20))
        XCTAssertTrue(bar.label.hasPrefix("Now Playing: "), bar.label)
        for _ in 0..<20 where !bar.hasFocus { remote.press(.down) }
        XCTAssertTrue(bar.hasFocus, "down from the page didn't reach the bar")
        sleep(1)
        shot("tv-np1-bar-focused")

        remote.press(.select)
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 10))
        sleep(1)
        shot("tv-np2-now-playing")

        remote.press(.menu)
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForNonExistence(timeout: 10))
        XCTAssertTrue(shuffle.waitForExistence(timeout: 5), "Back didn't return to the Music page")
        XCTAssertTrue(bar.exists)
        shot("tv-np3-back")
    }

    /// Stations and Now Playing with lyrics on the TV (M6.5).
    func testMusicStation() throws {
        try signIn()
        XCTAssertTrue(app.buttons["Music"].firstMatch.waitForExistence(timeout: 10))
        app.openSection("Music")
        // The landing page: quick actions first, the Library list at the bottom (MUSIC-15).
        XCTAssertTrue(app.buttons["musicLibrary.artists"].waitForExistence(timeout: 10))
        let radio = app.buttons["Library Radio"]
        guard radio.waitForExistence(timeout: 10) else {
            throw XCTSkip("Library Radio needs Soundprint's analysis, which this server doesn't have")
        }
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
        app.openSection("Now Playing")
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
    /// Home has loaded: one of the rows that comes first on it (since the 1.3.0 rows, New Releases
    /// and, near a holiday, its row, such as Halloween Favourites).
    private func homeShows() -> Bool {
        let row = app.staticTexts.matching(NSPredicate(format:
            "label IN {'Continue Watching', 'Recently Added Movies', 'Recommended for You', 'New Releases'} OR label ENDSWITH ' Favourites'")).firstMatch
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

    /// A YouTube trailer can't play inside Marquee on the TV (no WebKit), so its button says
    /// it opens YouTube.
    func testTrailerButtonSaysYouTube() throws {
        try signIn()
        app.launchArguments = ["-marquee-test-youtube-trailer"] // keeps the sign-in
        app.open(URL(string: "marquee://item/359")!)
        let trailer = app.buttons["trailerButton"]
        XCTAssertTrue(trailer.waitForExistence(timeout: 15))
        XCTAssertEqual(trailer.label, "Trailer (YouTube)")
        shot("tv-trailer-youtube-label")
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
        XCTAssertTrue(app.buttons["Movies"].firstMatch.waitForExistence(timeout: 10))
        app.openSection("Movies")
        XCTAssertTrue(app.descendants(matching: .any)["libraryGrid"].waitForExistence(timeout: 10))
        remote.press(.down)
        let sortMenu = app.descendants(matching: .any).matching(identifier: "sortMenu").firstMatch
        let muse = app.buttons["Muse"].firstMatch
        XCTAssertTrue(sortMenu.waitForExistence(timeout: 5))
        // Well down the grid.
        for _ in 0..<8 { remote.press(.down) }
        sleep(1)
        shot("tv-library-scrolled")
        // No page title faded over the grid: the sidebar names the library.
        XCTAssertFalse(app.pageTitleShows("Movies"), "no Movies title over the grid")
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

    /// Live TV is a sidebar entry placed with the libraries, after the video ones, not a fixed
    /// second entry; it opens the guide.
    func testLiveTVTabWithLibraries() throws {
        _ = try signInTemporaryUser()
        XCTAssertTrue(app.buttons["Live TV"].firstMatch.waitForExistence(timeout: 10))
        app.openSidebar()
        let live = try XCTUnwrap(app.sidebarItem("Live TV"))
        sleep(1)
        shot("tv-live-tab")
        let home = try XCTUnwrap(app.sidebarItem("Home"))
        // The test server's video libraries.
        let videos = try ["Anime", "Movies"].map { try XCTUnwrap(app.sidebarItem($0), "\($0) is listed") }
        for v in videos {
            XCTAssertLessThan(v.frame.midY, live.frame.midY, "Live TV comes after \(v.label)")
        }
        // Not straight after Home: libraries sit between them.
        let between = videos.filter { $0.frame.midY > home.frame.midY && $0.frame.midY < live.frame.midY }
        XCTAssertFalse(between.isEmpty, "Live TV isn't the second entry")
        app.openSection("Live TV")
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

    // MARK: - Batch 2 (MUSIC-22, PLAY-20)

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

    /// The year-in-music recap on the TV (MUSIC-22): Kiddo's, page by page with Next.
    func testRecapTV() throws {
        let kid = try api("POST", "/auth/pin", ["userId": 2, "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UI test", "platform": "tvos"]])
        let token = try XCTUnwrap(kid["token"] as? String)
        // The card shows only with music history: three of Floating's tracks played this year.
        let tracks = (try api("GET", "/items/324/children", token: token)["items"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? Int }
        XCTAssertFalse(tracks.isEmpty, "Floating's tracks")
        for id in tracks.prefix(3) { _ = try api("PUT", "/items/\(id)/progress", ["positionMs": 40000, "watched": true], token: token) }
        try signIn(as: token)
        XCTAssertTrue(app.buttons["Music"].firstMatch.waitForExistence(timeout: 10))
        app.openSection("Music")
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

// MARK: - Batch 6: Continue Watching, watched marks, video playlists, keeping the place

extension MarqueeTVUITests {
    /// The element with focus.
    private var focused: XCUIElement {
        app.buttons.element(matching: NSPredicate(format: "hasFocus == true"))
    }

    /// A temporary account that isn't signed in yet (deleted when the test ends); its token.
    private func temporaryAccount() throws -> String {
        let name = "uitv6-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass])
        let id = try XCTUnwrap(user["id"] as? Int)
        let login = try api("POST", "/auth/login", ["username": name, "password": pass, "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "tvos"]])
        let url = server + "/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        return try XCTUnwrap(login["token"] as? String)
    }

    private func viewCount(_ id: Int, _ token: String) throws -> Int {
        try api("GET", "/items/\(id)", token: token)["viewCount"] as? Int ?? 0
    }

    private func continueIDs(_ token: String) throws -> [Int] {
        let row = try list("/hubs/home", token: token).first { $0["id"] as? String == "continue-watching" }
        return (row?["items"] as? [[String: Any]] ?? []).compactMap { $0["id"] as? Int }
    }

    /// Long-press a Continue Watching tile: Remove from Continue Watching (USER-6).
    func testRemoveFromContinueWatchingTV() throws {
        let token = try temporaryAccount()
        try api("PUT", "/items/322/progress", ["positionMs": 60000], token: token)
        try signIn(as: token)
        let tile = app.buttons["continue.322"]
        XCTAssertTrue(tile.waitForExistence(timeout: 15), "00 Long Test is in Continue Watching")
        for _ in 0..<6 where !tile.hasFocus { remote.press(.down) }
        for _ in 0..<6 where !tile.hasFocus { remote.press(.left) }
        XCTAssertTrue(tile.hasFocus, "focus is on \(focused.label)")
        remote.press(.select, forDuration: 1.5)
        let remove = app.descendants(matching: .any)["removeFromContinue"]
        XCTAssertTrue(remove.waitForExistence(timeout: 5), "the context menu offers it")
        shot("tv-b6-continue-menu")
        remote.press(.select) // the first entry has focus
        XCTAssertTrue(tile.waitForNonExistence(timeout: 5), "the tile goes at once")
        try eventually(10, "gone from the server's row") { try !continueIDs(token).contains(322) }
        XCTAssertEqual(try api("GET", "/items/322", token: token)["viewOffsetMs"] as? Int, 60000, "the position is kept")
    }

    /// Mark watched on the item page: the button changes at once and the server has it.
    func testMarkWatchedTV() throws {
        let token = try temporaryAccount()
        try signIn(as: token)
        app.launchArguments = [] // opening the link relaunches the app: keep the sign-in
        app.open(URL(string: "marquee://item/322")!)
        let mark = app.buttons["markWatched"]
        XCTAssertTrue(mark.waitForExistence(timeout: 15))
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        focus(play, direction: .down)
        focus(mark, direction: .right, tries: 6)
        remote.press(.select)
        try eventually(10, "watched on the server") { try viewCount(322, token) > 0 }
        XCTAssertTrue(app.buttons["Watched"].waitForExistence(timeout: 5), "the page shows it")
        shot("tv-b6-watched")
        remote.press(.select) // and back
        try eventually(10, "unwatched on the server") { try viewCount(322, token) == 0 }
        XCTAssertTrue(app.buttons["Mark watched"].waitForExistence(timeout: 5))
    }

    /// A linked item's page opens over the app: Close goes back.
    private func closeLinkedPage() {
        let close = app.buttons["Close"].firstMatch
        guard close.waitForExistence(timeout: 3) else { return }
        focus(close, direction: .up)
        remote.press(.select)
        XCTAssertTrue(close.waitForNonExistence(timeout: 5), "the linked page closed")
    }

    /// The heart (USER-12) on the TV: a movie hearted with its page's focusable button shows in
    /// the Movies grid's Favourites filter; a song hearted in Now Playing is a favourite.
    func testFavoritesTV() throws {
        let token = try temporaryAccount()
        let libs = try list("/libraries", token: token)
        let movies = try XCTUnwrap(libs.first { $0["type"] as? String == "movies" }?["id"] as? Int)
        let first = try XCTUnwrap((try api("GET", "/libraries/\(movies)/items?limit=1", token: token)["items"] as? [[String: Any]])?.first)
        let movieID = try XCTUnwrap(first["id"] as? Int), movieTitle = try XCTUnwrap(first["title"] as? String)
        let music = try XCTUnwrap(libs.first { $0["type"] as? String == "music" && $0["name"] as? String == "Music" }?["id"] as? Int)
        let tracks = try api("GET", "/libraries/\(music)/items?type=track&limit=500", token: token)["items"] as? [[String: Any]] ?? []
        let track = try XCTUnwrap(tracks.first { $0["title"] as? String == "Floating 1" }?["id"] as? Int)
        func favorite(_ id: Int) throws -> Bool { try api("GET", "/items/\(id)", token: token)["favorite"] as? Bool ?? false }
        try signIn(as: token)
        app.launchArguments = [] // opening a link relaunches the app: keep the sign-in

        // The movie's page: focus the heart beside Play and select it.
        app.open(URL(string: "marquee://item/\(movieID)")!)
        let heart = app.buttons["favoriteToggle"]
        XCTAssertTrue(heart.waitForExistence(timeout: 15), "the page has a heart")
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        focus(play, direction: .down)
        focus(heart, direction: .right, tries: 8)
        XCTAssertEqual(heart.value as? String, "Off")
        remote.press(.select)
        try eventually(10, "a favourite on the server") { try favorite(movieID) }
        XCTAssertEqual(heart.value as? String, "On", "the page shows it")
        sleep(1)
        shot("tv-fav1-movie-page")
        closeLinkedPage()

        // Movies › Sort and filter › Favourites: only the hearted movie.
        app.openSection("Movies")
        let grid = app.descendants(matching: .any)["libraryGrid"]
        XCTAssertTrue(grid.waitForExistence(timeout: 10))
        remote.press(.down)
        for _ in 0..<6 { remote.press(.right) }
        remote.press(.select)
        let filter = app.descendants(matching: .any).matching(NSPredicate(format: "label == 'Favourites'")).firstMatch
        XCTAssertTrue(filter.waitForExistence(timeout: 5), "the sort and filter menu opened")
        // XCUITest doesn't report focus in the menu: up to its first row (Movies), then down past
        // Collections, the eight sorts and Unwatched only to Favourites.
        for _ in 0..<14 { remote.press(.up) }
        for _ in 0..<11 { remote.press(.down) }
        sleep(1)
        shot("tv-fav2a-menu")
        remote.press(.select)
        sleep(1)
        if filter.exists { remote.press(.menu) } // close the menu if it stayed open
        try eventually(10, "only the favourite listed") { grid.buttons.count == 1 }
        XCTAssertTrue(grid.buttons.element(boundBy: 0).label.hasPrefix(movieTitle), "the hearted movie is in Favourites")
        shot("tv-fav2-favourites-filter")

        // A song: play it from its page, then heart it in Now Playing.
        app.open(URL(string: "marquee://item/\(track)")!)
        XCTAssertTrue(play.waitForExistence(timeout: 15))
        focus(play, direction: .down)
        remote.press(.select)
        closeLinkedPage()
        XCTAssertTrue(app.buttons["Now Playing"].firstMatch.waitForExistence(timeout: 20))
        app.openSection("Now Playing")
        let npHeart = app.buttons["np.favorite"]
        XCTAssertTrue(npHeart.waitForExistence(timeout: 10), "Now Playing has a heart")
        for _ in 0..<25 where !npHeart.hasFocus {
            let f = focused.frame, h = npHeart.frame
            if f.maxY < 200 { remote.press(.down) } // still at the top
            else if f.minX > app.frame.midX { remote.press(.left) } // in the queue
            else if abs(f.midY - h.midY) < 15 { remote.press(f.midX > h.midX ? .left : .right) }
            else { remote.press(f.midY < h.midY ? .down : .up) }
        }
        XCTAssertTrue(npHeart.hasFocus, "focus reached the heart")
        XCTAssertEqual(npHeart.value as? String, "Off")
        remote.press(.select)
        try eventually(10, "the song a favourite on the server") { try favorite(track) }
        XCTAssertEqual(npHeart.value as? String, "On")
        sleep(1)
        shot("tv-fav3-now-playing")
    }

    /// Video playlists are on the Playlists tab; Play starts the first item.
    func testVideoPlaylistTV() throws {
        let token = try temporaryAccount()
        let created = try api("POST", "/playlists", ["title": "UI6 TV Movies", "kind": "video", "itemIds": [322, 8]], token: token)
        XCTAssertNotNil(created["id"])
        try signIn(as: token)
        XCTAssertTrue(app.buttons["Playlists"].firstMatch.waitForExistence(timeout: 10), "a Playlists tab")
        app.openSection("Playlists")
        let card = app.buttons["UI6 TV Movies"].firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), "the video playlist is listed")
        remote.press(.down)
        XCTAssertTrue(card.hasFocus, "focus is on \(focused.label)")
        shot("tv-b6-playlists")
        remote.press(.select)
        XCTAssertTrue(app.staticTexts["00 Long Test"].waitForExistence(timeout: 10), "its items")
        sleep(1)
        // Focus starts on the first item; the actions are above it.
        remote.press(.up) // to the header's actions
        for _ in 0..<4 where focused.label != "Play" { remote.press(.left) }
        XCTAssertEqual(focused.label, "Play")
        let before = try activeSessionIDs()
        remote.press(.select)
        try eventually(20, "a video starts") {
            let fresh = try activeSessionIDs().subtracting(before)
            return !fresh.isEmpty
        }
        let sessions = try list("/playback/sessions", token: adminToken ?? "")
        XCTAssertTrue(sessions.contains { $0["itemId"] as? Int == 322 }, "00 Long Test plays first")
        shot("tv-b6-playing")
        remote.press(.menu)
    }

    /// Back from a title, the grid is where it was, with focus on that title.
    func testLibraryKeepsFocusAfterBack() throws {
        _ = try signInTemporaryUser()
        XCTAssertTrue(app.buttons["Movies"].firstMatch.waitForExistence(timeout: 10))
        app.openSection("Movies")
        XCTAssertTrue(app.descendants(matching: .any)["libraryGrid"].waitForExistence(timeout: 10))
        remote.press(.down)
        for _ in 0..<6 { remote.press(.down) }
        // Focus can land on the pinned sort menu (not a poster): left into the grid.
        for _ in 0..<6 where !focused.exists { remote.press(.left) }
        sleep(1)
        let label = focused.label
        XCTAssertFalse(label.isEmpty || label.hasPrefix("00 "), "well down the grid: \(label)")
        shot("tv-b6-grid-before")
        remote.press(.select)
        XCTAssertTrue(app.buttons["markWatched"].waitForExistence(timeout: 15), "the title's page")
        remote.press(.menu)
        XCTAssertTrue(app.descendants(matching: .any)["libraryGrid"].waitForExistence(timeout: 10))
        sleep(2)
        shot("tv-b6-grid-after")
        XCTAssertEqual(focused.label, label, "focus is back on the title opened")
        let poster = app.buttons[label].firstMatch
        XCTAssertTrue(poster.exists && app.windows.firstMatch.frame.contains(CGPoint(x: poster.frame.midX, y: poster.frame.midY)), "and it's on screen")
    }

    /// Video playlists are also a row on Home (as well as the Playlists entry in the sidebar).
    func testVideoPlaylistsOnHomeTV() throws {
        let token = try temporaryAccount()
        _ = try api("POST", "/playlists", ["title": "UI6 Home Movies", "kind": "video", "itemIds": [322, 8]], token: token)
        try signIn(as: token)
        let card = app.buttons["UI6 Home Movies"].firstMatch
        for _ in 0..<12 where !(card.exists && card.hasFocus) { remote.press(.down) }
        XCTAssertTrue(card.hasFocus, "the playlist on Home has focus (\(focused.label))")
        shot("tv-b6-home-playlists")
        remote.press(.select)
        XCTAssertTrue(app.staticTexts["00 Long Test"].waitForExistence(timeout: 10), "it opens with its items")
    }
}

// MARK: - Batch 8: rating request when a movie or episode ends (USER-19)

extension MarqueeTVUITests {
    /// 00 Long Test resumed 10 s from the end plays out; the card over the page starts on the
    /// middle star, right moves to the fourth and OK saves 8; then it says thanks and goes.
    func testRatingPromptTV() throws {
        let token = try temporaryAccount()
        try api("PUT", "/items/322/rating", ["rating": NSNull()], token: token)
        try api("PUT", "/items/322/progress", ["positionMs": 80000], token: token)
        let settings = server + "/api/v1/settings", rating = server + "/api/v1/items/322/rating", admin = adminToken
        addTeardownBlock {
            Self.send("PATCH", settings, token: admin, ["general": ["ratingPrompt": false]])
            Self.send("PUT", rating, token: token, ["rating": NSNull()])
        }
        try signIn(as: token)
        app.launchArguments = [] // opening the link relaunches the app: keep the sign-in
        try api("PATCH", "/settings", ["general": ["ratingPrompt": true]]) // read when the player opens
        app.open(URL(string: "marquee://play/322")!)

        let card = app.descendants(matching: .any)["ratingPrompt"]
        XCTAssertTrue(card.waitForExistence(timeout: 45), "the rating request shows when the movie ends")
        let star3 = app.buttons["ratingPromptStar3"], star4 = app.buttons["ratingPromptStar4"]
        let started = NSPredicate(format: "hasFocus == true")
        wait(for: [expectation(for: started, evaluatedWith: star3)], timeout: 5)
        shot("tv-b8-rating-card")
        remote.press(.right)
        XCTAssertTrue(star4.hasFocus, "right moves to the fourth star")
        remote.press(.select)
        XCTAssertTrue(app.descendants(matching: .any)["ratingPromptThanks"].waitForExistence(timeout: 3), "a brief thanks")
        try eventually(10, "rating 8 saved") { try api("GET", "/items/322", token: token)["userRating"] as? Double == 8 }
        XCTAssertTrue(card.waitForNonExistence(timeout: 6), "the card goes")
    }
}

// MARK: - Shared DVR (LIVE-5)

extension MarqueeTVUITests {
    /// Someone else's recording says who recorded it and can't be cancelled; the signed-in
    /// user's space against their quota; no Recordings tab for a user who may not see them.
    func testSharedRecordingsTV() throws {
        // The admin records an upcoming programme: someone else's for the test user.
        var channels: [[String: Any]] = []
        let done = expectation(description: "channels")
        var req = URLRequest(url: URL(string: server + "/api/v1/livetv/channels")!)
        req.setValue("Bearer \(adminToken ?? "")", forHTTPHeaderField: "Authorization")
        URLSession.shared.dataTask(with: req) { data, _, _ in
            channels = (data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [[String: Any]]) ?? []
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 30)
        let channel = try XCTUnwrap(channels.first { ($0["next"] as? [String: Any])?["start"] != nil })
        let next = try XCTUnwrap(channel["next"] as? [String: Any])
        let scheduled = try api("POST", "/livetv/recordings", ["channelId": channel["id"] as! Int, "start": next["start"] as! String])
        if let id = (scheduled["recording"] as? [String: Any])?["id"] as? Int {
            let url = server + "/api/v1/livetv/recordings/\(id)", admin = adminToken
            addTeardownBlock { Self.send("DELETE", url, token: admin) }
        }
        let title = try XCTUnwrap(next["title"] as? String)
        let adminName = try XCTUnwrap(try api("GET", "/me")["displayName"] as? String)

        let token = try signInTemporaryUser()
        let id = try XCTUnwrap(try api("GET", "/me", token: token)["id"] as? Int)
        // A new user's restrictions, kept here (there's no single-user GET).
        var r: [String: Any] = ["allowRemote": true, "liveTv": true]
        func restrict(_ changes: [String: Any]) throws {
            r.merge(changes) { $1 }
            try api("PATCH", "/users/\(id)", ["restrictions": r])
        }
        try restrict(["canRecordDVR": true, "canViewDVR": true, "dvrQuotaGb": 50])

        app.openSection("Live TV")
        let tab = app.buttons["Recordings"].firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 15))
        let segments = ["Guide", "What's On", "Recordings"]
        for _ in 0..<10 where !segments.contains(focused.label) { remote.press(.up) }
        for _ in 0..<3 where !tab.hasFocus { remote.press(.right) }
        XCTAssertTrue(tab.hasFocus, "focus is on \(focused.label)")
        remote.press(.select)
        XCTAssertTrue(app.staticTexts[title].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Recorded by \(adminName)")).firstMatch.exists, "who recorded it")
        XCTAssertFalse(app.buttons["Cancel"].exists, "someone else's recording can't be cancelled")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label ENDSWITH 'of 50 GB used'")).firstMatch.exists, "the usage line")
        sleep(1)
        shot("tv-dvr1-shared-recording")

        // Not allowed to see recordings: no Recordings tab.
        try restrict(["canViewDVR": false])
        app.openSection("Home")
        app.openSection("Live TV")
        XCTAssertTrue(app.buttons["What's On"].firstMatch.waitForExistence(timeout: 15))
        XCTAssertFalse(app.buttons["Recordings"].exists, "no Recordings tab")
        shot("tv-dvr2-no-recordings-tab")
    }

    /// Connection limits and stopping recordings on the TV (LIVE-3, LIVE-5): with the test
    /// source limited to one connection and a recording on, the Recording now strip offers Stop
    /// and keep, the other channels are greyed out and channel up/down skips them, and
    /// Recordings switches between All recordings and Only mine. Needs the source's maxStreams at 1.
    func testRecordingNowAndConnectionLimitsTV() throws {
        func focusLabel() -> String {
            let f = app.buttons.matching(NSPredicate(format: "hasFocus == true")).firstMatch
            return f.exists ? f.label : ""
        }
        func focusFrame() -> CGRect {
            let f = app.buttons.matching(NSPredicate(format: "hasFocus == true")).firstMatch
            return f.exists ? f.frame : .zero
        }
        // Focus moves (and a segment is chosen) a moment after each press.
        func settle(_ e: XCUIElement, _ d: XCUIRemote.Button) {
            for _ in 0..<4 where !e.hasFocus {
                remote.press(d)
                usleep(600_000)
            }
            XCTAssertTrue(e.hasFocus, "focus is on \(focusLabel())")
        }
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        for r in try list("/livetv/recordings", token: admin) where r["status"] as? String == "recording" {
            try api("POST", "/livetv/recordings/\(r["id"] as! Int)/stop", ["keep": true])
        }
        let channels = try list("/livetv/channels", token: admin)
        let soon = Date().addingTimeInterval(8 * 60)
        func date(_ s: Any?) -> Date? {
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            return (s as? String).flatMap { f.date(from: $0) ?? ISO8601DateFormatter().date(from: $0) }
        }
        guard let channel = channels.first(where: { date(($0["now"] as? [String: Any])?["end"]).map { $0 > soon } ?? false }) else {
            throw XCTSkip("nothing on now runs long enough to record")
        }
        let name = try XCTUnwrap(channel["name"] as? String)
        let now = try XCTUnwrap(channel["now"] as? [String: Any])
        let other = try XCTUnwrap(channels.first { $0["id"] as? Int != channel["id"] as? Int && $0["next"] != nil })
        let next = try XCTUnwrap(other["next"] as? [String: Any])
        let theirs = try api("POST", "/livetv/recordings", ["channelId": other["id"] as! Int, "start": next["start"] as! String])
        if let tid = (theirs["recording"] as? [String: Any])?["id"] as? Int {
            let url = server + "/api/v1/livetv/recordings/\(tid)"
            addTeardownBlock { Self.send("DELETE", url, token: admin) }
        }
        let otherTitle = try XCTUnwrap(next["title"] as? String)

        let token = try signInTemporaryUser()
        let id = try XCTUnwrap(try api("GET", "/me", token: token)["id"] as? Int)
        try api("PATCH", "/users/\(id)", ["restrictions": ["allowRemote": true, "liveTv": true, "canRecordDVR": true, "canViewDVR": true]])
        let mine = try api("POST", "/livetv/recordings", ["channelId": channel["id"] as! Int, "start": now["start"] as! String], token: token)
        let recID = try XCTUnwrap((mine["recording"] as? [String: Any])?["id"] as? Int, "scheduled: \(mine)")
        let recURL = server + "/api/v1/livetv/recordings/\(recID)?deleteFile=true"
        addTeardownBlock { Self.send("DELETE", recURL, token: admin) }
        var rec: [String: Any]?
        for _ in 0..<45 where rec?["status"] as? String != "recording" {
            rec = try list("/livetv/recordings", token: admin).first { $0["id"] as? Int == recID }
            if rec?["status"] as? String != "recording" { sleep(1) }
        }
        XCTAssertEqual(rec?["status"] as? String, "recording", "the recording started: \(rec ?? [:])")
        guard let blocked = try list("/livetv/channels", token: admin).first(where: { $0["available"] as? Bool == false })?["name"] as? String else {
            throw XCTSkip("the test source isn't limited to one connection")
        }

        app.openSection("Live TV")
        let strip = app.descendants(matching: .any)["liveTV.recordingNow"].firstMatch
        XCTAssertTrue(strip.waitForExistence(timeout: 15), "the Recording now strip shows")
        let keep = app.buttons["Stop and keep"].firstMatch
        XCTAssertTrue(keep.exists)
        XCTAssertTrue(app.buttons["Stop and delete"].firstMatch.exists)
        let greyed = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(blocked), unavailable: ")).firstMatch
        XCTAssertTrue(greyed.waitForExistence(timeout: 10), "\(blocked) is marked unavailable")
        sleep(1)
        shot("tv-cl1-recording-now")

        // What's On: watch the recorded channel; channel up and down stay on it (the others
        // can't be tuned).
        let segments = ["Guide", "What's On", "Recordings"]
        for _ in 0..<10 where !segments.contains(focusLabel()) { remote.press(.down); usleep(400_000) }
        let whatsOn = app.buttons["What's On"].firstMatch
        settle(whatsOn, .right)
        remote.press(.select)
        let watch = app.buttons["Watch \(name)"]
        XCTAssertTrue(watch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(blocked), unavailable: ")).firstMatch.exists, "greyed in What's On")
        for _ in 0..<6 where !watch.hasFocus { remote.press(.down) }
        XCTAssertTrue(watch.hasFocus, "focus is on \(focusLabel())")
        sleep(1)
        shot("tv-cl2-whats-on")
        remote.press(.select)
        let player = app.descendants(matching: .any)["liveWatch.player"].firstMatch
        XCTAssertTrue(player.waitForExistence(timeout: 15))
        sleep(4)
        remote.press(.down)
        sleep(2)
        XCTAssertEqual(player.value as? String, name, "channel down skips the unavailable channels")
        remote.press(.up)
        sleep(2)
        XCTAssertEqual(player.value as? String, name, "channel up too")
        shot("tv-cl3-watching")
        remote.press(.menu)
        XCTAssertTrue(player.waitForNonExistence(timeout: 10))
        sleep(1)

        // Recordings: All recordings, then Only mine.
        let recordings = app.buttons["Recordings"].firstMatch
        for _ in 0..<10 where !segments.contains(focusLabel()) { remote.press(.up); usleep(400_000) }
        settle(recordings, .right)
        remote.press(.select)
        let all = app.buttons["All recordings"], onlyMine = app.buttons["Only mine"]
        XCTAssertTrue(all.waitForExistence(timeout: 10))
        remote.press(.down)
        settle(all, .left)
        remote.press(.select)
        XCTAssertTrue(app.staticTexts[otherTitle].waitForExistence(timeout: 10), "someone else's recording")
        shot("tv-cl4-all")
        settle(onlyMine, .right)
        remote.press(.select)
        XCTAssertTrue(app.staticTexts[otherTitle].waitForNonExistence(timeout: 10), "only mine")
        sleep(1)
        shot("tv-cl5-only-mine")
        settle(all, .left)
        remote.press(.select)

        // Stop and keep from the strip.
        let stripKeep = strip.buttons["Stop and keep"].firstMatch
        XCTAssertTrue(stripKeep.exists)
        for _ in 0..<8 where !stripKeep.hasFocus {
            remote.press(stripKeep.frame.midY < focusFrame().midY - 20 ? .up : .left)
            usleep(600_000)
        }
        XCTAssertTrue(stripKeep.hasFocus, "focus is on \(focusLabel())")
        remote.press(.select)
        XCTAssertTrue(strip.waitForNonExistence(timeout: 15), "the strip goes")
        sleep(1)
        shot("tv-cl6-stopped")
    }
}
