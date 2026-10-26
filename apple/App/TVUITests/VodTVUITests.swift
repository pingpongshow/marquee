import XCTest

/// Movies on demand ("VOD", LIVE-6) on Apple TV with the remote: VOD in the sidebar after Live TV,
/// the film's page, playing, resuming and marking watched; hidden when not usable.
/// Needs the local VOD source the iOS VodUITests describe (.e2e/vod-apple on :32621).
final class VodTVUITests: XCTestCase {
    private var app: XCUIApplication!
    private let remote = XCUIRemote.shared
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "http://localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"] }
    private let film = "Apple Test Alpha (1985)"

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

    private var focused: XCUIElement { app.descendants(matching: .any).element(matching: NSPredicate(format: "hasFocus == true")) }

    private func focus(_ element: XCUIElement, direction: XCUIRemote.Button, tries: Int = 10) {
        for _ in 0..<tries where !element.hasFocus { remote.press(direction) }
        XCTAssertTrue(element.hasFocus, "couldn't focus \(element); focus is on \(focused.label)")
    }

    // MARK: - Server

    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> [String: Any] {
        guard let admin = adminToken, !admin.isEmpty else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
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
        wait(for: [done], timeout: 60)
        return out
    }

    /// VOD on with the local test source; the film's id.
    private func ensureVod() throws -> Int {
        let settings = try api("GET", "/settings")
        var sources = (settings["integrations"] as? [String: Any])?["vodSources"] as? [[String: Any]] ?? []
        if !sources.contains(where: { $0["id"] as? String == "apple-e2e" }) {
            sources.append(["id": "apple-e2e", "name": "Apple e2e films", "url": "http://127.0.0.1:32621/films.m3u", "enabled": true])
        }
        try api("PATCH", "/settings", ["integrations": ["vodEnabled": true, "vodSources": sources]])
        func id() throws -> Int? {
            let page = try api("GET", "/livetv/vod/movies?q=Apple%20Test%20Alpha")
            return (page["movies"] as? [[String: Any]] ?? []).first { $0["title"] as? String == film }?["id"] as? Int
        }
        if try id() == nil { try api("POST", "/livetv/vod/refresh") }
        return try XCTUnwrap(try id(), "the local VOD source loaded")
    }

    private func movie(_ id: Int, _ token: String) throws -> [String: Any] {
        try api("GET", "/livetv/vod/movies/\(id)", token: token)
    }

    private func eventually(_ timeout: TimeInterval, _ what: String, _ check: () throws -> Bool) rethrows {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if try check() { return }
            Thread.sleep(forTimeInterval: 1)
        }
        XCTFail("timed out: \(what)")
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

    /// A temporary account (deleted when the test ends); its token.
    private func temporaryAccount() throws -> String {
        let name = "uitvvod-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try api("POST", "/users", ["username": name, "displayName": name, "password": pass])
        let id = try XCTUnwrap(user["id"] as? Int)
        let login = try api("POST", "/auth/login", ["username": name, "password": pass, "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "tvos"]])
        let url = server + "/api/v1/users/\(id)", admin = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: admin) }
        return try XCTUnwrap(login["token"] as? String)
    }

    /// Connects (Bonjour, or the typed address) and signs in with Quick Connect, approved with
    /// the token (as the main TV tests do).
    private func signIn(as token: String) throws {
        app.launch()
        // (MARQUEE_TEST_SERVER_NAME picks the test server out when another one is on the network.)
        let name = ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER_NAME"].flatMap { $0.isEmpty ? nil : $0 } ?? "E2E"
        let found = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name + ",")).firstMatch
        if found.waitForExistence(timeout: 20) {
            remote.press(.up)
            for _ in 0..<4 { remote.press(.left) }
            focus(found, direction: .right, tries: 6)
            remote.press(.select)
        } else {
            let enter = app.buttons["Enter an address"]
            for _ in 0..<12 where !enter.hasFocus { remote.press(.down) }
            remote.press(.select)
            let field = app.textFields["Home address, e.g. 192.168.1.20:32500"]
            XCTAssertTrue(field.waitForExistence(timeout: 5))
            for _ in 0..<8 where !field.hasFocus { remote.press(.up) }
            remote.press(.select)
            app.typeText(server.replacingOccurrences(of: "http://", with: "") + "\n")
        }
        let qc = app.buttons["Use Quick Connect"]
        let connect = app.buttons["Connect"]
        if !qc.waitForExistence(timeout: 8), connect.exists {
            for _ in 0..<10 where !connect.hasFocus { remote.press(.down) }
            remote.press(.select)
        }
        XCTAssertTrue(qc.waitForExistence(timeout: 15))
        for _ in 0..<8 where !qc.hasFocus { remote.press(.up) }
        for _ in 0..<16 where !qc.hasFocus { remote.press(.down) }
        remote.press(.select)
        let codeText = app.staticTexts.matching(NSPredicate(format: "label MATCHES '^([A-Z0-9] ){5}[A-Z0-9]$'")).firstMatch
        XCTAssertTrue(codeText.waitForExistence(timeout: 10))
        try api("POST", "/auth/quickconnect/authorize", ["code": codeText.label.replacingOccurrences(of: " ", with: "")], token: token)
        XCTAssertTrue(app.buttons["Home"].firstMatch.waitForExistence(timeout: 20))
    }

    // MARK: - Tests


    /// VOD sits right after Live TV in the sidebar; a film in Continue Watching opens, resumes from
    /// its point with the remote, and Menu closes the player and records where it got to;
    /// then Mark watched.
    func testVodTabPlayResumeAndMarkWatchedTV() throws {
        let id = try ensureVod()
        let token = try temporaryAccount()
        // A position from earlier, so the film is in VOD's Continue Watching.
        let s = try api("POST", "/livetv/vod/movies/\(id)/play", token: token)
        let session = try XCTUnwrap(s["id"] as? String)
        try api("PATCH", "/livetv/vod/sessions/\(session)", ["positionMs": 20000, "durationMs": 50000], token: token)
        try api("DELETE", "/livetv/vod/sessions/\(session)", token: token)
        try signIn(as: token)

        app.openSidebar()
        let live = try XCTUnwrap(app.sidebarItem("Live TV"))
        let vod = try XCTUnwrap(app.sidebarItem("VOD Movies"), "VOD Movies is in the sidebar")
        XCTAssertLessThan(live.frame.midY, vod.frame.midY, "VOD comes right after Live TV")
        app.openSection("VOD Movies")
        let tiles = app.buttons.matching(identifier: "vod.tile").matching(NSPredicate(format: "label == %@", film))
        XCTAssertTrue(tiles.firstMatch.waitForExistence(timeout: 15), "the film is on the page")
        sleep(1)
        // The film is in the grid too: Continue Watching's tile is the one higher up the page.
        let tile = tiles.allElementsBoundByIndex.min { $0.frame.minY < $1.frame.minY } ?? tiles.firstMatch
        XCTAssertGreaterThan(tiles.count, 1, "the film is in Continue Watching")
        sleep(1)
        shot("tv-vod-browse")
        XCTAssertFalse(app.buttons["Guide"].exists, "VOD is its own page, not inside Live TV")
        XCTAssertFalse(app.pageTitleShows("VOD Movies"), "no page title faded over VOD")
        // The tile can be above or below where focus starts.
        // On a row the focus can sit on the tile's cell (no label) rather than the button: either counts.
        let onTile = { tile.hasFocus || (self.focused.exists && self.focused.frame.contains(CGPoint(x: tile.frame.midX, y: tile.frame.midY))) }
        for d in [XCUIRemote.Button.down, .up, .left] { for _ in 0..<6 where !onTile() { remote.press(d) } }
        XCTAssertTrue(onTile(), "couldn't focus \(film); focus is on \(focused.label)")
        remote.press(.select)

        let play = app.buttons["vod.play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10), "the film's page opens")
        XCTAssertEqual(play.label, "Resume from 0:20")
        focus(play, direction: .up, tries: 4)
        shot("tv-vod-page")
        remote.press(.select)
        sleep(8)
        shot("tv-vod-playing")
        // Menu (once the transport bar is down) closes the player.
        // (The page stays in the tree under the player: wait for focus to come back to it.)
        let onPage = { ["vod.play", "vod.fromStart", "vod.watched"].contains(self.focused.identifier) }
        for _ in 0..<3 where !onPage() {
            remote.press(.menu)
            sleep(2)
        }
        XCTAssertTrue(onPage(), "back on the film's page; focus is on \(focused.label)")
        try eventually(10, "carried on from 0:20") { (try movie(id, token)["positionMs"] as? Int ?? 0) > 20000 }

        let watched = app.buttons["vod.watched"]
        focus(watched, direction: .right, tries: 4)
        remote.press(.select)
        try eventually(10, "watched on the server") { try movie(id, token)["watched"] as? Bool == true }
        try eventually(5, "the page shows it") { watched.label == "Watched" }
        shot("tv-vod-watched")
    }

    /// Live TV's page has no title faded over the guide (the sidebar names it), and no VOD
    /// tab inside it.
    func testLiveTVNoTitleTV() throws {
        _ = try ensureVod()
        let token = try temporaryAccount()
        try signIn(as: token)
        app.openSection("Live TV")
        XCTAssertTrue(app.buttons["Guide"].firstMatch.waitForExistence(timeout: 15), "Live TV opens")
        sleep(1)
        shot("tv-livetv")
        XCTAssertFalse(app.pageTitleShows("Live TV"), "no Live TV title over the guide")
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label == 'VOD Movies'")).count, 1, "VOD only in the sidebar, not a Live TV tab")
    }

    /// Not usable for the profile: no VOD tab.
    func testVodHiddenWhenUnusableTV() throws {
        _ = try ensureVod()
        let token = try temporaryAccount()
        app.launchArguments += ["-marquee-test-vod-off"]
        try signIn(as: token)
        app.openSidebar()
        XCTAssertNotNil(app.sidebarItem("Live TV"))
        sleep(2)
        XCTAssertFalse(app.buttons["VOD Movies"].exists, "no VOD tab")
    }

    /// No channels (Live TV hidden) but VOD on: VOD is still in the sidebar and opens.
    func testVodTabWithoutChannelsTV() throws {
        _ = try ensureVod()
        let token = try temporaryAccount()
        app.launchArguments += ["-marquee-test-channels-off"]
        try signIn(as: token)
        app.openSidebar()
        XCTAssertTrue(app.buttons["VOD Movies"].firstMatch.waitForExistence(timeout: 15), "VOD is listed without Live TV")
        XCTAssertFalse(app.buttons["Live TV"].exists, "no Live TV without channels")
        shot("tv-vod-no-live")
        app.openSection("VOD Movies")
        XCTAssertTrue(app.buttons.matching(identifier: "vod.tile").firstMatch.waitForExistence(timeout: 15), "VOD opens")
    }

    /// Back opens the sidebar (the TV's tab list): Home, the Libraries section (each library,
    /// Live TV, VOD), then Playlists, Settings and Search. Choosing VOD opens it, and Back from
    /// there opens the sidebar again.
    func testSidebarOpensWithBackTV() throws {
        _ = try ensureVod()
        let token = try temporaryAccount()
        try signIn(as: token)
        sleep(2)
        // (Open while Home was still loading, with nothing to focus: Right closes it.)
        for _ in 0..<3 where app.sidebarIsOpen {
            remote.press(.right)
            sleep(1)
        }
        XCTAssertFalse(app.sidebarIsOpen, "collapsed on Home")
        shot("tv-sidebar-collapsed")

        remote.press(.menu)
        for _ in 0..<10 where !app.sidebarIsOpen { Thread.sleep(forTimeInterval: 0.5) }
        XCTAssertTrue(app.sidebarIsOpen, "Back opened the sidebar")
        XCTAssertEqual(app.state, .runningForeground, "still in the app")
        // The video libraries, then Live TV and VOD (video too), then Music. (The DVR's Recorded TV
        // library is inside Live TV.)
        let names = ["Home", "Anime", "Movies", "Live TV", "VOD Movies", "VOD Shows", "Music", "Playlists", "Settings", "Search"]
        let items = try names.map { try XCTUnwrap(app.sidebarItem($0), "\($0) is in the sidebar") }
        for (a, b) in zip(items, items.dropFirst()) {
            XCTAssertLessThan(a.frame.midY, b.frame.midY, "\(a.label) comes before \(b.label)")
        }
        let header = app.staticTexts["Libraries"].firstMatch
        XCTAssertTrue(header.exists, "the Libraries header")
        XCTAssertTrue(header.frame.midY > items[0].frame.midY && header.frame.midY < items[1].frame.midY, "between Home and the libraries")
        shot("tv-sidebar-open")

        app.openSection("VOD Movies")
        XCTAssertTrue(app.buttons.matching(identifier: "vod.tile").firstMatch.waitForExistence(timeout: 15), "VOD opens")
        for _ in 0..<6 where app.sidebarIsOpen { Thread.sleep(forTimeInterval: 0.5) }
        XCTAssertFalse(app.sidebarIsOpen, "the sidebar closed onto VOD")
        sleep(1)
        shot("tv-sidebar-vod")

        remote.press(.menu)
        for _ in 0..<10 where !app.sidebarIsOpen { Thread.sleep(forTimeInterval: 0.5) }
        XCTAssertTrue(app.sidebarIsOpen, "Back from VOD opened the sidebar")
        XCTAssertEqual(app.state, .runningForeground)
        shot("tv-sidebar-open-from-vod")
    }

    /// The search screen: no "Search" title under the keyboard, and the field clear of the
    /// sidebar's collapsed button.
    func testSearchScreenLayoutTV() throws {
        let token = try temporaryAccount()
        try signIn(as: token)
        app.openSection("Search")
        sleep(2)
        shot("tv-search")
        XCTAssertFalse(app.pageTitleShows("Search"), "no Search title")
        let field = app.textFields["search.field"]
        XCTAssertTrue(field.exists, "the search field")
        XCTAssertGreaterThan(field.frame.minX, 200, "clear of the sidebar's button")
        // Typing searches.
        for _ in 0..<6 where !field.hasFocus { remote.press(.up) }
        remote.press(.select)
        sleep(1)
        app.typeText("Long Test")
        sleep(1)
        remote.press(.menu) // done typing
        let hit = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] '00 Long Test'")).firstMatch
        XCTAssertTrue(hit.waitForExistence(timeout: 10), "results appear")
        shot("tv-search-results")
        // Back still opens the sidebar from Search.
        app.openSidebar()
        XCTAssertTrue(app.sidebarIsOpen, "Back opens the sidebar from Search")
        shot("tv-search-sidebar")
    }

    // MARK: - VOD Shows

    /// VOD on with the fake Xtream source (127.0.0.1:32622, as VodShowsUITests describes); the show's id.
    @discardableResult
    private func ensureShows() throws -> Int {
        func id() throws -> Int? {
            let page = try api("GET", "/livetv/vod/series?q=Apple%20Test%20Show")
            return (page["series"] as? [[String: Any]] ?? []).first { $0["title"] as? String == "Apple Test Show" }?["id"] as? Int
        }
        if try id() == nil {
            let settings = try api("GET", "/settings")
            var sources = (settings["integrations"] as? [String: Any])?["vodSources"] as? [[String: Any]] ?? []
            if !sources.contains(where: { $0["id"] as? String == "apple-e2e-xtream" }) {
                sources = sources.map { var s = $0; s["passwordSet"] = nil; return s }
                sources.append(["id": "apple-e2e-xtream", "name": "Apple e2e Xtream", "kind": "xtream", "url": "http://127.0.0.1:32622",
                                "username": "apple", "password": "apple-test", "enabled": true])
                try api("PATCH", "/settings", ["integrations": ["vodEnabled": true, "vodSources": sources]])
            }
            try api("POST", "/livetv/vod/refresh")
        }
        return try XCTUnwrap(try id(), "the fake Xtream source loaded")
    }

    /// Moves focus onto an element: down/up to its row, then across.
    private func focusOn(_ element: XCUIElement, tries: Int = 6) {
        for d in [XCUIRemote.Button.down, .left, .up, .right] {
            for _ in 0..<tries where !element.hasFocus { remote.press(d) }
        }
        XCTAssertTrue(element.hasFocus, "couldn't focus \(element); focus is on \(focused.label)")
    }

    /// VOD Movies and VOD Shows in the sidebar; the shows grid with the filter bubble at the top
    /// right; a show's page; an episode playing to Up Next and on into the next one.
    func testVodShowsTV() throws {
        let id = try ensureShows()
        let token = try temporaryAccount()
        try signIn(as: token)

        app.openSidebar()
        let movies = try XCTUnwrap(app.sidebarItem("VOD Movies"), "VOD Movies is in the sidebar")
        let shows = try XCTUnwrap(app.sidebarItem("VOD Shows"), "VOD Shows is in the sidebar")
        XCTAssertLessThan(movies.frame.midY, shows.frame.midY, "VOD Shows after VOD Movies")
        if let live = app.sidebarItem("Live TV") { XCTAssertLessThan(live.frame.midY, movies.frame.midY) }
        shot("tv-shows-sidebar")
        app.openSection("VOD Shows")
        let tile = app.buttons.matching(identifier: "vod.showTile").matching(NSPredicate(format: "label == 'Apple Test Show'")).firstMatch
        XCTAssertTrue(tile.waitForExistence(timeout: 15), "the shows grid")
        let filter = app.buttons["vod.filter"]
        XCTAssertTrue(filter.exists, "the filter bubble")
        XCTAssertGreaterThan(filter.frame.minX, 1500, "at the top right")
        XCTAssertLessThan(filter.frame.minY, 300, "at the top right")
        sleep(1)
        shot("tv-shows-grid")

        // The filter menu.
        // (The menu's focus lands on its unlabelled button: check where focus is instead.)
        let onFilter = { abs(self.focused.frame.midX - filter.frame.midX) < 40 && abs(self.focused.frame.midY - filter.frame.midY) < 40 }
        for _ in 0..<6 where !onFilter() { remote.press(.right) }
        XCTAssertTrue(onFilter(), "focus reaches the filter bubble")
        remote.press(.select)
        XCTAssertTrue(app.descendants(matching: .any)["Genre"].firstMatch.waitForExistence(timeout: 5), "the filter menu")
        XCTAssertTrue(app.descendants(matching: .any)["Decade"].exists && app.descendants(matching: .any)["Rating"].exists && app.descendants(matching: .any)["Watched"].exists)
        shot("tv-shows-filter-menu")
        remote.press(.menu)
        sleep(1)
        XCTAssertTrue(onFilter(), "back on the filter bubble")
        // Left across the controls to the category menu (not on into the sidebar), then down.
        for _ in 0..<5 where focused.frame.minX > 300 { remote.press(.left) }
        remote.press(.down) // the grid's first show
        focus(tile, direction: .right, tries: 4)
        remote.press(.select)
        let play = app.buttons["vod.show.play"]
        XCTAssertTrue(play.waitForExistence(timeout: 30), "the show's page")
        XCTAssertEqual(play.label, "Start S1 · E1")
        XCTAssertTrue(app.buttons.matching(identifier: "vod.episode").firstMatch.exists, "the episodes (lazy: those on screen)")
        sleep(1)
        shot("tv-show-page")
        focus(app.buttons.matching(identifier: "vod.episode").firstMatch, direction: .down, tries: 8)
        sleep(1)
        shot("tv-show-episodes")
        for _ in 0..<10 where !play.hasFocus && focused.identifier != "vod.show.watched" { remote.press(.up) }
        focus(play, direction: .left, tries: 3)
        remote.press(.select)
        sleep(5)
        shot("tv-episode-playing")
        let upNext = app.buttons.matching(NSPredicate(format: "identifier == 'upNext.next' AND label CONTAINS 'Second'")).firstMatch
        XCTAssertTrue(upNext.waitForExistence(timeout: 40), "Up Next at the end")
        sleep(1)
        shot("tv-up-next")
        XCTAssertTrue(upNext.hasFocus, "Playing next has the focus; focus is on \(focused.identifier) \(focused.label)")
        remote.press(.select)
        XCTAssertTrue(upNext.waitForNonExistence(timeout: 10))
        try eventually(10, "episode 1 counted watched") {
            ((try api("GET", "/livetv/vod/series/\(id)", token: token)["episodes"] as? [[String: Any]])?.first?["watched"] as? Bool) == true
        }
        let third = app.buttons.matching(NSPredicate(format: "identifier == 'upNext.next' AND label CONTAINS 'Third'")).firstMatch
        XCTAssertTrue(third.waitForExistence(timeout: 40), "the next episode played to its Up Next")
        let cancel = app.buttons["upNext.cancel"]
        if !cancel.hasFocus { focusOn(cancel, tries: 6) }
        remote.press(.select)
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        try eventually(10, "the page offers the next episode") { play.label == "Play S1 · E3" }
        shot("tv-show-after")
    }

    /// A film the library already has: its poster's green border stays with it under focus, and
    /// the In my library filter narrows the grid. Needs the fake Xtream film "10 Xenon" (2014),
    /// which the e2e Movies library has too.
    func testInLibraryBorderAndFilterTV() throws {
        try ensureShows()
        let token = try temporaryAccount()
        try signIn(as: token)
        app.openSection("VOD Movies")
        let film = { (title: String) in
            self.app.buttons.matching(identifier: "vod.tile").matching(NSPredicate(format: "label == %@", title)).firstMatch
        }
        let owned = film("10 Xenon"), other = film("Apple XC Film One")
        XCTAssertTrue(owned.waitForExistence(timeout: 15), "the film the library has")
        XCTAssertTrue(other.waitForExistence(timeout: 5))
        XCTAssertEqual(owned.value as? String, "In your library")
        XCTAssertNotEqual(other.value as? String, "In your library")
        let filter = app.buttons["vod.filter"]
        let onFilter = { abs(self.focused.frame.midX - filter.frame.midX) < 40 && abs(self.focused.frame.midY - filter.frame.midY) < 40 }
        for _ in 0..<6 where !onFilter() { remote.press(.right) }
        XCTAssertTrue(onFilter(), "focus reaches the filter bubble")
        for _ in 0..<5 where focused.frame.minX > 300 { remote.press(.left) }
        remote.press(.down)
        focus(owned, direction: .right, tries: 4)
        sleep(1)
        shot("tv-in-library-focused")
        focus(other, direction: .right, tries: 4)
        sleep(1)
        shot("tv-in-library-unfocused")

        // In my library: Yes, from the filter menu.
        for _ in 0..<3 { remote.press(.up) }
        for _ in 0..<8 where !onFilter() { remote.press(.right) }
        XCTAssertTrue(onFilter(), "back on the filter bubble")
        remote.press(.select)
        let group = app.descendants(matching: .any)["In my library"].firstMatch
        XCTAssertTrue(group.waitForExistence(timeout: 5), "the filter menu offers In my library")
        // (Menu items' focus shows on unlabelled elements: compare where focus is.)
        let on = { (e: XCUIElement) in abs(self.focused.frame.midY - e.frame.midY) < 30 && self.focused.frame.minX <= e.frame.midX && e.frame.midX <= self.focused.frame.maxX }
        for _ in 0..<12 where !on(group) { remote.press(.down) }
        XCTAssertTrue(on(group), "focus on In my library; it's at \(focused.frame)")
        shot("tv-in-library-menu")
        remote.press(.select)
        let yes = app.descendants(matching: .any)["In my library: Yes"].firstMatch
        XCTAssertTrue(yes.waitForExistence(timeout: 5))
        for d in [XCUIRemote.Button.down, .up] { for _ in 0..<4 where !on(yes) { remote.press(d) } }
        XCTAssertTrue(on(yes), "focus on Yes; it's at \(focused.frame)")
        remote.press(.select)
        XCTAssertTrue(app.buttons["Remove filter In my library"].waitForExistence(timeout: 10), "shown as a chip")
        XCTAssertTrue(other.waitForNonExistence(timeout: 10), "only titles the library has")
        XCTAssertTrue(owned.exists)
        sleep(1)
        shot("tv-in-library-filtered")
    }
}
