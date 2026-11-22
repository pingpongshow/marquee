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

    /// Opens a show from the Anime library (Search matches any word, so it can't single one out).
    private func openAnime(_ title: String) {
        app.openSection("Anime")
        let show = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", title)).firstMatch
        guard show.waitForExistence(timeout: 10), reach(show) else { return XCTFail("no \(title) in Anime") }
        remote.press(.select)
    }

    /// Back from an episode's page to its season's: the page shows at once with the focus on the
    /// episode that was opened, not a grey screen until the remote moves.
    func testBackFromEpisodePageKeepsFocus() throws {
        try signIn()
        openAnime("Test Anime")
        let season = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Season'")).firstMatch
        guard season.waitForExistence(timeout: 10), reach(season) else { return XCTFail("no season on the show's page") }
        remote.press(.select)
        let episode = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '1. '")).firstMatch
        guard episode.waitForExistence(timeout: 10), reach(episode) else { return XCTFail("no episode on the season's page") }
        let opened = focusedLabel
        remote.press(.select)
        sleep(2)
        shot("back-1-episode-page")
        remote.press(.menu)
        var seen: [String] = []
        for i in 0..<6 {
            usleep(250_000)
            seen.append(focusedLabel)
            if i == 1 { shot("back-2-after-menu") }
        }
        shot("back-3-settled")
        print("BACK-FOCUS opened=\(opened) after=\(seen)")
        XCTAssertFalse(seen.dropFirst(2).contains(""), "nothing had the focus after Back: \(seen)")
        XCTAssertEqual(seen.last, opened, "the focus didn't come back to the episode that was opened")
    }

    /// The sessions playing on the server: title → position (ms).
    private func playing() -> [String: Int] {
        guard let admin = adminToken else { return [:] }
        var req = URLRequest(url: URL(string: server + "/api/v1/playback/sessions")!)
        req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
        var out: [String: Int] = [:]
        let done = expectation(description: "sessions")
        URLSession.shared.dataTask(with: req) { data, _, _ in
            if let data, let list = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] {
                for s in list { out[s["title"] as? String ?? ""] = s["positionMs"] as? Int ?? 0 }
            }
            done.fulfill()
        }.resume()
        wait(for: [done], timeout: 10)
        return out
    }

    /// English chosen on a show's first episode: when it ends, Up Next plays the second episode
    /// by itself (no stall), in English (the show's remembered audio, PLAY-8).
    func testUpNextPlaysNextEpisodeInChosenLanguage() throws {
        try signIn()
        openAnime("Dub Test")
        let season = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Season'")).firstMatch
        guard season.waitForExistence(timeout: 10), reach(season) else { return XCTFail("no season") }
        remote.press(.select)
        let episode = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '1. '")).firstMatch
        guard episode.waitForExistence(timeout: 10), reach(episode) else { return XCTFail("no episode 1") }
        remote.press(.select)
        let audio = app.buttons["trackChip.audio"]
        guard audio.waitForExistence(timeout: 10), reach(audio) else { return XCTFail("no audio chip") }
        remote.press(.select)
        sleep(2)
        shot("upnext-0-audio-menu")
        print("UPNEXT menu: " + app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS[c] 'english' OR label CONTAINS[c] 'japan' OR label CONTAINS[c] 'automatic'"))
            .allElementsBoundByIndex.map { "\($0.elementType.rawValue):\($0.label)" }.joined(separator: " | ") + " focus=\(focusedLabel)")
        // The menu's option ("English · 1 ch · AAC"), not the page's track list ("English · AAC Mono").
        let english = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH 'English' AND label CONTAINS ' ch '")).firstMatch
        guard english.waitForExistence(timeout: 5), reach(english) else { return XCTFail("no English in the audio menu") }
        remote.press(.select)
        sleep(1)
        shot("upnext-1-english-chosen")
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        guard play.waitForExistence(timeout: 5), reach(play) else { return XCTFail("no Play") }
        remote.press(.select)
        // 45 s of episode 1, then the 10 s Up Next countdown, then episode 2 starting.
        sleep(50)
        shot("upnext-2-countdown")
        sleep(15)
        shot("upnext-3-episode-2")
        // The player reports its position about every ten seconds.
        let first = playing()
        sleep(12)
        let second = playing()
        print("UPNEXT sessions \(first) → \(second)")
        let ep2 = second.keys.first { $0.contains("Episode 2") || $0.contains("E02") }
        XCTAssertNotNil(ep2, "episode 2 isn't playing: \(second)")
        if let ep2 { XCTAssertGreaterThan(second[ep2] ?? 0, first[ep2] ?? 0, "episode 2 is stalled") }
        remote.press(.menu)
        remote.press(.menu)
    }

    /// Back from pages reached other ways: a Home card, and Back twice from an episode to its show.
    func testBackFromEpisodePageOtherPaths() throws {
        try signIn()
        func quick(_ name: String) {
            let a = XCTAttachment(screenshot: app.screenshot())
            a.name = name
            a.lifetime = .keepAlways
            add(a)
        }
        func afterBack(_ name: String) {
            remote.press(.menu)
            usleep(500_000)
            quick("paths-\(name)-a-0.5s")
            sleep(3)
            quick("paths-\(name)-b-3.5s")
            print("BACK-FOCUS \(name) focus=\(focusedLabel)")
        }
        // From Home: the first card that opens a page, then Back to Home.
        remote.press(.down)
        sleep(1)
        let homeCard = focusedLabel
        remote.press(.select)
        sleep(2)
        quick("paths-home-card-page")
        afterBack("home")
        print("BACK-FOCUS home card=\(homeCard)")
        // Show → season → episode, then Back twice.
        openAnime("Test Anime")
        let season = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Season'")).firstMatch
        guard season.waitForExistence(timeout: 10), reach(season) else { return XCTFail("no season") }
        remote.press(.select)
        let episode = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '1. '")).firstMatch
        guard episode.waitForExistence(timeout: 10), reach(episode) else { return XCTFail("no episode") }
        remote.press(.select)
        sleep(2)
        afterBack("to-season")
        sleep(1)
        afterBack("to-show")
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
