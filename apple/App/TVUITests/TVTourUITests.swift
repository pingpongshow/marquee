import XCTest

/// A tour of the Apple TV app for review: every section, title page, the player, music, Live TV,
/// Settings and the profile, a screenshot of each ("tour-…" attachments). It keeps going after a
/// failure so one broken screen doesn't hide the rest; failures say what didn't work. Rows that
/// would sign out, delete, reset or rescan are passed over.
final class TVTourUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }
    private var focused: XCUIElement { app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true")) }
    /// What has the focus ("(icon)" for an unlabelled button), or "" when nothing does.
    private var focusedLabel: String {
        let f = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).allElementsBoundByIndex
        guard let e = f.first else { return "" }
        return e.label.isEmpty ? "(\(e.identifier.isEmpty ? "icon" : e.identifier))" : e.label
    }

    /// Whether the element has the focus: itself, or (in a list) the cell around it.
    private func isFocused(_ e: XCUIElement) -> Bool {
        if e.hasFocus { return true }
        guard e.exists, !focusedLabel.isEmpty else { return false }
        return focused.frame.contains(CGPoint(x: e.frame.midX, y: e.frame.midY))
    }

    override func setUp() {
        continueAfterFailure = true
        app = XCUIApplication()
        app.launchArguments = ["-marquee-reset"]
        app.launch()
    }

    // MARK: - Helpers

    private func shot(_ name: String) {
        sleep(2)
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "tour-" + name
        a.lifetime = .keepAlways
        add(a)
    }

    /// Moves focus onto an element: along each direction in turn.
    @discardableResult
    private func reach(_ element: XCUIElement, tries: Int = 8) -> Bool {
        for d in [XCUIRemote.Button.down, .right, .up, .left, .down] {
            for _ in 0..<tries where !isFocused(element) { remote.press(d) }
        }
        if !isFocused(element) { XCTFail("couldn't focus \(element.label); focus is on \(focusedLabel)") }
        return isFocused(element)
    }

    private func approve(code: String) throws {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: server + "/api/v1/auth/quickconnect/authorize")!)
        req.httpMethod = "POST"
        req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["code": code])
        let done = expectation(description: "approve")
        URLSession.shared.dataTask(with: req) { _, _, _ in done.fulfill() }.resume()
        wait(for: [done], timeout: 10)
    }

    /// Bonjour (else the address), Quick Connect approved as the admin, then Home.
    private func signIn() throws {
        let found = app.buttons.matching(NSPredicate(format: "label CONTAINS 'E2E'")).firstMatch
        // A server given in MARQUEE_TEST_SERVER is typed: Bonjour may find another E2E server.
        let explicit = ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] != nil
        if !explicit, found.waitForExistence(timeout: 20) {
            remote.press(.up)
            for _ in 0..<4 { remote.press(.left) }
            for _ in 0..<6 where !found.hasFocus { remote.press(.right) }
            remote.press(.select)
        } else {
            // Bonjour can miss a server on this Mac: type its address.
            let enter = app.buttons["Enter an address"]
            XCTAssertTrue(enter.waitForExistence(timeout: 5))
            reach(enter)
            remote.press(.select)
            let field = app.textFields["Home address, e.g. 192.168.1.20:32500"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            reach(field)
            remote.press(.select)
            app.typeText(server.replacingOccurrences(of: "http://", with: "") + "\n")
            let connect = app.buttons["Connect"]
            if !app.buttons["Use Quick Connect"].waitForExistence(timeout: 8), connect.exists {
                if app.keyboards.count > 0 { remote.press(.menu) } // the keyboard held the focus
                reach(connect)
                remote.press(.select)
            }
        }
        let qc = app.buttons["Use Quick Connect"]
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        guard reach(qc) else { throw XCTSkip("couldn't reach Quick Connect") }
        remote.press(.select)
        let code = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(code.waitForExistence(timeout: 10))
        try approve(code: code.label.replacingOccurrences(of: " ", with: ""))
        let home = app.staticTexts.matching(NSPredicate(format:
            "label IN {'Continue Watching', 'Recently Added Movies', 'New Releases'} OR label ENDSWITH ' Favourites'")).firstMatch
        XCTAssertTrue(home.waitForExistence(timeout: 20), "Home loads after signing in")
    }

    /// Back to where the walk started: Menu, and focus back in the page when that opened the
    /// sidebar instead (a row that opened nothing). Never Menu with the sidebar open: that leaves.
    private func back() {
        guard !app.sidebarIsOpen else { remote.press(.right); return }
        remote.press(.menu)
        sleep(1)
        if app.sidebarIsOpen { remote.press(.right); sleep(1) }
    }

    private static let unsafe = ["Sign Out", "Delete", "Remove", "Reset", "Clear", "Empty", "Restart", "Scan", "Forget",
                                 "Stop", "Cancel", "Disconnect", "Unlink", "Revoke", "Switch"]

    /// Walks down a list from the focused row: opens each row, screenshots what it opens and
    /// comes back. Rows that would change or delete something, and switches, are passed over.
    private func walk(_ prefix: String, rows: Int) {
        var seen = Set<String>()
        for i in 0..<rows {
            let label = focusedLabel
            guard !label.isEmpty, !seen.contains(label) else { break } // the end: down didn't move
            seen.insert(label)
            if focused.elementType == .switch || Self.unsafe.contains(where: { label.localizedCaseInsensitiveContains($0) }) {
                remote.press(.down)
                continue
            }
            remote.press(.select)
            shot("\(prefix)-\(String(format: "%02d", i))-\(label.prefix(36))")
            if app.state != .runningForeground { return XCTFail("the app left after \(label)") }
            if focusedLabel.isEmpty { XCTFail("nothing has the focus on \(label)'s page") }
            back()
            remote.press(.down)
        }
    }

    /// Searches and opens the first result whose label contains the text.
    private func open(searching query: String, result: String? = nil) {
        app.openSection("Search")
        let field = app.textFields["search.field"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        for _ in 0..<6 where !field.hasFocus { remote.press(.up) }
        remote.press(.select)
        sleep(1)
        // The field keeps the last search: clear it first.
        app.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 40))
        app.typeText(query)
        sleep(1)
        remote.press(.menu)
        let hit = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] %@", result ?? query)).firstMatch
        guard hit.waitForExistence(timeout: 10) else { return XCTFail("no search result for \(query)") }
        if reach(hit) { remote.press(.select) }
    }

    /// Screenshots a page and down it, a few rows at a time.
    private func page(_ name: String, scrolls: Int = 3) {
        shot(name)
        for i in 1...max(1, scrolls) {
            for _ in 0..<2 { remote.press(.down) }
            shot("\(name)-down\(i)")
        }
    }

    // MARK: - Tours

    /// Home (and its rows further down), the sidebar, every section and the profile page.
    func testTour1Sections() throws {
        try signIn()
        page("home", scrolls: 4)
        app.openSidebar()
        shot("sidebar")
        for name in ["Anime", "Movies", "Live TV", "Music", "Recorded TV", "VOD Movies", "VOD Shows", "Playlists", "Discover",
                     "Settings", "Search"] {
            app.openSidebar()
            guard app.sidebarItem(name) != nil else { continue }
            app.openSection(name)
            page("section-\(name)", scrolls: 2)
        }
        app.openSection("e2eadmin")
        shot("section-profile")
    }

    /// Film, show, season, episode, extras and collection pages.
    func testTour2TitlePages() throws {
        try signIn()
        app.openSection("Movies")
        shot("movies-grid")
        remote.press(.down)
        remote.press(.select)
        page("movie-page", scrolls: 3)
        open(searching: "00 Preview Test")
        page("preview-page", scrolls: 4)
        open(searching: "Test Saga")
        page("collection-page", scrolls: 1)
        open(searching: "Test Anime")
        page("show-page", scrolls: 3)
        // A season and an episode: down to the seasons, then the first one.
        let season = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Season'")).firstMatch
        if season.waitForExistence(timeout: 5), reach(season) {
            remote.press(.select)
            page("season-page", scrolls: 2)
            remote.press(.select)
            page("episode-page", scrolls: 2)
        }
    }

    /// A film playing: the transport bar, its menus (up from the scrubber) and the info panel
    /// (down), then back to the page.
    func testTour3Player() throws {
        try signIn()
        open(searching: "00 Long Test")
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        guard play.waitForExistence(timeout: 10), reach(play) else { return XCTFail("no Play on the film's page") }
        remote.press(.select)
        sleep(6)
        shot("player-playing")
        remote.press(.select)
        shot("player-bar")
        remote.press(.up)
        remote.press(.select)
        shot("player-menu-1")
        remote.press(.menu)
        remote.press(.right)
        remote.press(.select)
        shot("player-menu-2")
        remote.press(.menu)
        remote.press(.down)
        remote.press(.down)
        shot("player-info")
        remote.press(.menu)
        remote.press(.menu)
        remote.press(.menu)
        shot("player-closed")
        XCTAssertEqual(app.state, .runningForeground)
    }

    /// Music: the landing page, every Library entry, an artist and an album; playing, Now
    /// Playing with lyrics, and the Now Playing bar over a long list.
    func testTour4Music() throws {
        try signIn()
        app.openSection("Music")
        page("music", scrolls: 3)
        let artists = app.buttons["musicLibrary.artists"]
        if artists.waitForExistence(timeout: 5), reach(artists) {
            walk("music-library", rows: 12)
        }
        if artists.exists, reach(artists) {
            remote.press(.select)
            shot("music-artists")
            remote.press(.select)
            page("music-artist", scrolls: 2)
            let album = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Album' OR label CONTAINS[c] 'Floating'")).firstMatch
            if album.waitForExistence(timeout: 5), reach(album) {
                remote.press(.select)
                page("music-album", scrolls: 2)
                let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Shuffle'}")).firstMatch
                if play.waitForExistence(timeout: 5), reach(play) {
                    remote.press(.select)
                    shot("music-album-playing")
                }
            }
        }
        // The Now Playing bar over a long list (Songs), then Now Playing and its lyrics.
        app.openSection("Music")
        let songs = app.buttons["musicLibrary.songs"]
        if songs.waitForExistence(timeout: 5), reach(songs) {
            remote.press(.select)
            page("music-songs-with-bar", scrolls: 6)
        }
        let bar = app.buttons["nowPlayingBar"]
        if bar.waitForExistence(timeout: 10) {
            for _ in 0..<30 where !bar.hasFocus { remote.press(.down) }
            shot("music-bar-focused")
            remote.press(.select)
            shot("now-playing")
            let lyrics = app.buttons["Lyrics"]
            if lyrics.waitForExistence(timeout: 5), reach(lyrics) {
                remote.press(.select)
                shot("now-playing-lyrics")
            }
            remote.press(.menu)
            shot("now-playing-closed")
        } else {
            XCTFail("no Now Playing bar while music plays")
        }
    }

    /// Live TV: the page, the guide further down, a channel playing; Recorded TV.
    func testTour5LiveTV() throws {
        try signIn()
        app.openSidebar()
        guard app.sidebarItem("Live TV") != nil else { throw XCTSkip("no Live TV") }
        app.openSection("Live TV")
        page("livetv", scrolls: 3)
        app.openSidebar()
        if app.sidebarItem("Recorded TV") != nil {
            app.openSection("Recorded TV")
            page("recorded-tv", scrolls: 1)
        }
    }

    /// Settings: every row, opened and screenshotted.
    func testTour6Settings() throws {
        try signIn()
        app.openSection("Settings")
        shot("settings")
        for _ in 0..<3 { remote.press(.up) }
        walk("settings", rows: 40)
    }

    /// Pages that load after they open (Genres) or can be empty (Requests) still take the focus,
    /// so Back leaves them: with nothing focused it did nothing.
    func testTour8BackLeavesPages() throws {
        try signIn()
        app.openSection("Music")
        let genres = app.buttons["musicLibrary.genres"]
        XCTAssertTrue(genres.waitForExistence(timeout: 10))
        // Below the music home's shelves (more of them once music has played): further than 8 presses.
        guard reach(genres, tries: 16) else { return }
        remote.press(.select)
        sleep(3)
        shot("back-genres")
        XCTAssertFalse(focusedLabel.isEmpty, "something on Genres has the focus")
        remote.press(.menu)
        XCTAssertTrue(genres.waitForExistence(timeout: 5), "Back from Genres returns to Music")
        XCTAssertTrue(genres.isHittable, "Back from Genres returns to Music")
        let decades = app.buttons["musicLibrary.decades"]
        if decades.waitForExistence(timeout: 5), reach(decades, tries: 16) {
            remote.press(.select)
            sleep(3)
            shot("back-decades")
            XCTAssertFalse(focusedLabel.isEmpty, "something on Decades has the focus")
            remote.press(.menu)
            sleep(1)
            XCTAssertTrue(decades.isHittable, "Back from Decades returns to Music")
        }

        app.openSection("Settings")
        let requests = app.buttons["Requests"].firstMatch
        XCTAssertTrue(requests.waitForExistence(timeout: 10))
        guard reach(requests) else { return }
        remote.press(.select)
        sleep(3)
        shot("back-requests")
        XCTAssertFalse(focusedLabel.isEmpty, "something on Requests has the focus")
        remote.press(.menu)
        sleep(1)
        XCTAssertTrue(app.buttons["Your Stats"].firstMatch.isHittable, "Back from Requests returns to Settings")
        let edit = app.buttons["Edit Home"].firstMatch
        if edit.waitForExistence(timeout: 5), reach(edit) {
            remote.press(.select)
            sleep(3)
            shot("back-edit-home")
            XCTAssertFalse(focusedLabel.isEmpty, "something on Edit Home has the focus")
            remote.press(.menu)
            sleep(1)
            XCTAssertTrue(app.buttons["Your Stats"].firstMatch.isHittable, "Back from Edit Home returns to Settings")
        }
    }

    /// The profile page's entries.
    func testTour7Profile() throws {
        try signIn()
        app.openSection("e2eadmin")
        shot("profile")
        walk("profile", rows: 8)
    }
}
