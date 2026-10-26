import XCTest

/// A screenshot tour of every iPhone screen against the local test server, for reviewing the
/// layout: tabs, libraries, title pages, the player and its menus, music, Live TV, VOD, Search,
/// Settings and profiles. Each step checks the screen opened and attaches a screenshot; nothing
/// destructive is pressed (Sign Out, Delete, Reset, Rescan).
final class TourUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

    override func setUp() {
        // A tour: one screen that doesn't open shouldn't hide the rest.
        continueAfterFailure = true
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
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil) throws -> Any? {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
        req.httpMethod = method
        req.timeoutInterval = 60
        req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
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
        wait(for: [done], timeout: 90)
        return out
    }

    /// VOD on, with the local films.m3u and the fake Xtream source (fake-vod.py, ports 32621
    /// and 32622).
    private func ensureVod() throws {
        let settings = try api("GET", "/settings") as? [String: Any] ?? [:]
        var sources = (settings["integrations"] as? [String: Any])?["vodSources"] as? [[String: Any]] ?? []
        sources = sources.map { var s = $0; s["passwordSet"] = nil; return s }
        var changed = false
        if !sources.contains(where: { $0["id"] as? String == "apple-e2e" }) {
            sources.append(["id": "apple-e2e", "name": "Apple e2e films", "url": "http://127.0.0.1:32621/films.m3u", "enabled": true])
            changed = true
        }
        if !sources.contains(where: { $0["id"] as? String == "apple-e2e-xtream" }) {
            sources.append(["id": "apple-e2e-xtream", "name": "Apple e2e Xtream", "kind": "xtream", "url": "http://127.0.0.1:32622",
                            "username": "apple", "password": "apple-test", "enabled": true])
            changed = true
        }
        if changed || (settings["integrations"] as? [String: Any])?["vodEnabled"] as? Bool != true {
            try api("PATCH", "/settings", ["integrations": ["vodEnabled": true, "vodSources": sources]])
            try api("POST", "/livetv/vod/refresh")
        }
    }

    // MARK: - App helpers

    /// The admin from scripts/e2e (so the admin's Settings pages are listed).
    private func signIn() {
        app.launch()
        let address = app.textFields["Home address, e.g. 192.168.1.20:32500"]
        _ = app.staticTexts["Welcome to Marquee"].waitForExistence(timeout: 10)
        for _ in 0..<4 where !address.exists { app.swipeUp() } // at large text sizes it's further down
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        shot("00-connect")
        address.tap()
        address.typeText(server)
        app.buttons["Connect"].tap()
        let other = app.buttons["Sign in with username and password"]
        XCTAssertTrue(other.waitForExistence(timeout: 15))
        shot("00-who-is-watching")
        other.tap()
        let field = app.textFields["Username"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText("e2eadmin")
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText("e2e-local-test-pass")
        shot("00-sign-in")
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
        app.dismissSavePassword()
    }

    private func tab(_ name: String) {
        let button = app.tabBars.buttons[name].firstMatch
        XCTAssertTrue(button.waitForExistence(timeout: 10), "the \(name) tab")
        button.tap()
        // A second tap goes back to the tab's root.
        if app.navigationBars.buttons.firstMatch.exists, app.navigationBars.buttons.firstMatch.label == name { button.tap() }
    }

    /// The Libraries tab at its root (tapping the selected tab again pops to it).
    private func openLibrariesTab() {
        tab("Libraries")
        for _ in 0..<2 where !app.navigationBars["Libraries"].waitForExistence(timeout: 2) {
            app.tabBars.buttons["Libraries"].firstMatch.tap()
        }
    }

    /// Closes an open menu or popover without choosing anything.
    private func dismissMenu() {
        let region = app.otherElements["PopoverDismissRegion"].firstMatch
        if region.exists { region.tap() } else { app.coordinate(withNormalizedOffset: CGVector(dx: 0.02, dy: 0.5)).tap() }
        sleep(1)
    }

    private func openLibrary(_ name: String) {
        openLibrariesTab()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch.tap()
        XCTAssertTrue(app.navigationBars[name].waitForExistence(timeout: 10), "the \(name) library opens")
    }

    private func button(_ prefix: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
    }

    /// Scrolls until `element` is on screen and above the tab bar (and the mini player).
    @discardableResult
    private func reveal(_ element: XCUIElement, max: Int = 8) -> Bool {
        for _ in 0..<max {
            if element.exists, element.isHittable, element.frame.maxY < bottomEdge - 4 { return true }
            app.swipeUp()
        }
        return element.exists && element.isHittable
    }

    /// Top of the tab bar, or of the mini player while music plays.
    private var bottomEdge: CGFloat {
        let bar = app.tabBars.firstMatch
        var y = bar.exists ? bar.frame.minY : app.windows.firstMatch.frame.maxY
        let mini = app.buttons["miniPlayer"]
        if mini.exists { y = min(y, mini.frame.minY) }
        return y
    }

    private func back() {
        let b = app.navigationBars.buttons.element(boundBy: 0)
        if b.exists { b.tap() }
    }

    /// Opens a Settings row and screenshots its page.
    private func settingsPage(_ label: String, _ name: String, wait: TimeInterval = 2) {
        let row = app.buttons[label].firstMatch
        guard reveal(row) else { return XCTFail("Settings lists \(label)") }
        row.tap()
        XCTAssertTrue(app.navigationBars.buttons["Settings"].waitForExistence(timeout: 10), "\(label) opens")
        sleep(UInt32(wait))
        shot(name)
        app.swipeUp()
        shot(name + "-scrolled")
        app.navigationBars.buttons["Settings"].tap()
    }

    private func skipTrailersIfAny() {
        let skip = app.buttons["Skip All"]
        if skip.waitForExistence(timeout: 4) { skip.tap() }
    }

    // MARK: - Tour

    /// Home, the profile sheet, Libraries, Playlists, Search and Downloads.
    func testTour1TabsAndHome() {
        signIn()
        sleep(3)
        shot("01-home")
        app.swipeUp()
        shot("01-home-scrolled")
        app.swipeUp()
        shot("01-home-scrolled-2")
        app.swipeDown(); app.swipeDown(); app.swipeDown()

        // The avatar: profiles, Account Settings, Sign Out.
        let avatar = button("Profile:")
        XCTAssertTrue(avatar.waitForExistence(timeout: 5), "the avatar on Home")
        avatar.tap()
        XCTAssertTrue(app.buttons["Done"].waitForExistence(timeout: 5), "the profiles sheet")
        sleep(1)
        shot("02-profiles-sheet")
        app.buttons["Done"].tap()

        // A Home row's See all.
        let seeAll = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'See all' OR label BEGINSWITH 'View more' OR label == 'See All'")).firstMatch
        if seeAll.waitForExistence(timeout: 3) {
            seeAll.tap()
            sleep(2)
            shot("03-home-row-page")
            back()
        }

        openLibrariesTab()
        sleep(1)
        shot("04-libraries")
        app.swipeUp()
        shot("04-libraries-scrolled")
        for (label, name) in [("Downloads", "05-downloads"), ("Discover", "05-discover")] {
            let row = app.buttons[label].firstMatch
            if reveal(row) {
                row.tap()
                sleep(2)
                shot(name)
                back()
            }
        }

        tab("Playlists")
        sleep(2)
        shot("06-playlists")

        tab("Search")
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "the search field")
        shot("07-search-empty")
        field.tap()
        field.typeText("Valley")
        sleep(3)
        shot("07-search-results")
        app.buttons["Search"].firstMatch.tap() // the keyboard's
        sleep(1)
        shot("07-search-results-no-keyboard")
        let segment = app.buttons["Muse"].firstMatch
        if segment.exists {
            segment.tap()
            sleep(1)
            shot("07-search-muse")
        }
    }

    /// Settings and every page under it.
    func testTour2Settings() {
        signIn()
        tab("Settings")
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 10))
        sleep(1)
        shot("10-settings")
        for i in 1...4 {
            app.swipeUp()
            shot("10-settings-scrolled-\(i)")
        }
        app.swipeDown(); app.swipeDown(); app.swipeDown(); app.swipeDown(); app.swipeDown()
        settingsPage("Your Stats", "11-stats", wait: 3)
        settingsPage("Requests", "12-requests")
        settingsPage("Users and Friends", "13-users")
        settingsPage("Cinema Trailers", "14-cinema")
        settingsPage("Library Settings", "15-library-settings")
        settingsPage("Library Health", "16-library-health", wait: 4)
        settingsPage("Integrations", "17-integrations")
        settingsPage("Edit Home", "18-edit-home")
        settingsPage("Subtitle Appearance", "19-subtitle-appearance")
        settingsPage("settings.equalizer", "20-equalizer")
        settingsPage("settings.smartDownloads", "21-smart-downloads")

        // Switch Profile (a sheet).
        app.swipeDown(); app.swipeDown(); app.swipeDown(); app.swipeDown()
        let sw = app.buttons["Switch Profile"].firstMatch
        if reveal(sw) {
            sw.tap()
            XCTAssertTrue(app.buttons["Cancel"].waitForExistence(timeout: 5), "the profile switcher")
            sleep(1)
            shot("22-switch-profile")
            app.buttons["Cancel"].tap()
        }
    }

    /// Movies, a film, a collection, extras, a show, its season and an episode.
    func testTour3VideoTitles() {
        signIn()
        openLibrary("Movies")
        sleep(2)
        shot("30-movies")
        app.buttons["Sort and filter"].tap()
        sleep(1)
        shot("30-movies-sort-menu")
        let collections = app.buttons["Collections"]
        XCTAssertTrue(collections.waitForExistence(timeout: 5))
        collections.tap()
        let saga = button("Test Saga")
        XCTAssertTrue(saga.waitForExistence(timeout: 10))
        shot("31-collections")
        saga.tap()
        XCTAssertTrue(app.staticTexts["In this collection"].waitForExistence(timeout: 10))
        sleep(1)
        shot("32-collection")
        button("20 Valley").tap()
        XCTAssertTrue(app.buttons["Play"].firstMatch.waitForExistence(timeout: 10))
        sleep(2)
        shot("33-film")
        app.swipeUp()
        shot("33-film-scrolled")
        app.swipeUp()
        shot("33-film-scrolled-2")
        let more = app.buttons["itemMore"]
        if more.exists || reveal(more, max: 2) {
            app.swipeDown(); app.swipeDown(); app.swipeDown()
            if more.isHittable {
                more.tap()
                sleep(1)
                shot("33-film-more-menu")
                dismissMenu()
            }
        }

        // Extras: 00 Preview Test has a featurette, a trailer and an Extended Cut.
        tab("Search")
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText("00 Preview")
        let preview = button("00 Preview Test")
        XCTAssertTrue(preview.waitForExistence(timeout: 10))
        preview.tap()
        XCTAssertTrue(app.buttons["Play"].firstMatch.waitForExistence(timeout: 10))
        sleep(2)
        shot("34-film-with-extras")
        let extras = app.staticTexts["Extras"]
        reveal(extras)
        sleep(1)
        shot("34-extras")
        let chips = app.descendants(matching: .any)["trackChips"]
        if chips.exists {
            app.swipeDown(); app.swipeDown()
            let version = app.buttons["trackChip.version"]
            if version.exists, version.isHittable {
                version.tap()
                sleep(1)
                shot("34-version-menu")
                dismissMenu()
            }
        }

        // A show: Test Anime, a season, an episode.
        openLibrary("Anime")
        sleep(2)
        shot("35-anime")
        app.scrollViews.buttons.firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Seasons"].waitForExistence(timeout: 10) || app.staticTexts["Episodes"].waitForExistence(timeout: 2), "the show's page")
        sleep(2)
        shot("36-show")
        app.swipeUp()
        shot("36-show-scrolled")
        app.swipeDown(); app.swipeDown()
        let season = button("Season")
        if reveal(season) {
            season.tap()
            sleep(2)
            shot("37-season")
        }
        let episode = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Episode' OR label BEGINSWITH 'E1' OR label BEGINSWITH '1.'")).firstMatch
        if reveal(episode) {
            episode.tap()
            sleep(2)
            shot("38-episode")
            app.swipeUp()
            shot("38-episode-scrolled")
        }
        // Recorded TV is inside Live TV (its Recordings), toured with Live TV.
    }

    /// The player, its overlay and menus.
    func testTour4Player() {
        signIn()
        openLibrary("Movies")
        tab("Search")
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText("00 Long Test")
        let film = button("00 Long Test")
        XCTAssertTrue(film.waitForExistence(timeout: 10))
        film.tap()
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        skipTrailersIfAny()
        sleep(4)
        app.revealPlayerControls()
        shot("40-player")
        let settings = app.playerControl("Playback settings")
        if settings.waitForExistence(timeout: 5) {
            settings.tap()
            sleep(1)
            shot("41-player-settings-menu")
            let speed = app.buttons["Speed"].firstMatch
            if speed.waitForExistence(timeout: 3) {
                speed.tap()
                sleep(1)
                shot("42-player-speed")
                app.buttons["Normal (1×)"].firstMatch.tap()
            }
        }
        // AVKit's own audio and subtitle menu.
        app.revealPlayerControls()
        let media = app.buttons["Legible Options"].firstMatch
        if media.waitForExistence(timeout: 3), media.isHittable {
            media.tap()
            sleep(1)
            shot("43-player-audio-subtitles")
            dismissMenu()
        }
        // Cast's device picker (Watch together isn't pressed: it starts a group on the server).
        let cast = app.playerControl("Cast")
        if cast.exists, cast.isHittable {
            cast.tap()
            sleep(1)
            shot("44-player-cast")
            let cancel = app.buttons["Cancel"].firstMatch
            if cancel.exists { cancel.tap() } else { dismissMenu() }
            sleep(1)
        }
        XCUIDevice.shared.orientation = .landscapeLeft
        sleep(2)
        app.revealPlayerControls()
        shot("45-player-landscape")
        XCUIDevice.shared.orientation = .portrait
        sleep(1)
        app.closePlayer()
        sleep(1)
        shot("46-after-player")
    }

    /// Music: the landing page, every Library entry, an artist, an album, Now Playing and its
    /// panels, the mini player.
    func testTour5Music() {
        signIn()
        openLibrary("Music")
        sleep(2)
        shot("50-music")
        app.swipeUp()
        shot("50-music-scrolled")
        app.swipeUp()
        shot("50-music-scrolled-2")
        app.swipeDown(); app.swipeDown(); app.swipeDown()

        for (id, name) in [("artists", "51-artists"), ("albums", "52-albums"), ("songs", "53-songs"), ("playlists", "54-playlists"),
                           ("favorites", "55-favourites"), ("genres", "56-genres"), ("moods", "57-moods"), ("decades", "58-decades"),
                           ("muse", "59-muse")] {
            let shortcut = app.buttons["musicShowLibrary"]
            if shortcut.waitForExistence(timeout: 5), shortcut.isHittable { shortcut.tap() }
            let row = app.buttons["musicLibrary.\(id)"]
            guard reveal(row) else { continue } // Moods and Muse only with Soundprint
            row.tap()
            sleep(2)
            shot(name)
            if id == "genres" || id == "decades" {
                let first = app.scrollViews.buttons.firstMatch.exists ? app.scrollViews.buttons.firstMatch : app.cells.buttons.firstMatch
                if first.exists, first.isHittable {
                    first.tap()
                    sleep(2)
                    shot(name + "-one")
                    back()
                }
            }
            back()
            sleep(1)
        }

        // An artist (round artwork), an album, play.
        let shortcut = app.buttons["musicShowLibrary"]
        if shortcut.waitForExistence(timeout: 5), shortcut.isHittable { shortcut.tap() }
        let artists = app.buttons["musicLibrary.artists"]
        reveal(artists)
        artists.tap()
        let artist = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
        reveal(artist)
        artist.tap()
        XCTAssertTrue(app.buttons["Play"].firstMatch.waitForExistence(timeout: 10))
        sleep(2)
        shot("60-artist")
        app.swipeUp()
        shot("60-artist-scrolled")
        app.swipeDown(); app.swipeDown()
        let album = button("Floating")
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        reveal(album)
        album.tap()
        XCTAssertTrue(app.buttons["Radio"].waitForExistence(timeout: 10))
        sleep(2)
        shot("61-album")
        app.buttons["Play"].firstMatch.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 10))
        sleep(2)
        shot("62-album-playing-mini-player")
        app.swipeUp(); app.swipeUp()
        shot("62-album-end-above-mini-player")
        // A song row's menu.
        let row = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'songRow.' OR label BEGINSWITH 'Floating 2'")).firstMatch
        if row.exists, row.isHittable {
            row.press(forDuration: 1.2)
            sleep(1)
            shot("63-song-menu")
            dismissMenu()
        }

        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        sleep(2)
        shot("64-now-playing")
        let lyrics = app.buttons["Lyrics"]
        if lyrics.exists {
            lyrics.tap()
            sleep(3)
            shot("65-lyrics")
        }
        let upNext = app.buttons["Up Next"].firstMatch.exists ? app.buttons["Up Next"].firstMatch : app.buttons["Show Up Next"].firstMatch
        if upNext.exists {
            upNext.tap()
            sleep(1)
            shot("66-up-next")
        }
        let np = app.buttons["Now Playing"].firstMatch
        if np.exists { np.tap() }
        let more = app.buttons["np.more"]
        if more.waitForExistence(timeout: 3) {
            more.tap()
            sleep(1)
            shot("67-now-playing-more")
            dismissMenu()
            sleep(1)
        }
        app.swipeDown(velocity: .fast)
        sleep(1)
        shot("68-mini-player-on-music")
        tab("Home")
        sleep(1)
        shot("69-mini-player-on-home")
        tab("Settings")
        app.swipeUp(); app.swipeUp(); app.swipeUp(); app.swipeUp(); app.swipeUp()
        shot("69-settings-end-above-mini-player")
        app.buttons["Pause"].firstMatch.tap()
    }

    /// Live TV: the guide, What's On, Recordings and the live player.
    func testTour6LiveTV() {
        signIn()
        openLibrariesTab()
        let entry = app.buttons["libraries.liveTV"]
        XCTAssertTrue(entry.waitForExistence(timeout: 10), "Live TV is listed")
        entry.tap()
        XCTAssertTrue(app.navigationBars["Live TV"].waitForExistence(timeout: 10))
        sleep(6)
        shot("70-guide")
        app.swipeUp()
        shot("70-guide-scrolled")
        app.swipeDown()
        let filters = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'All Channels'")).firstMatch
        if filters.exists, filters.isHittable {
            filters.tap()
            sleep(1)
            shot("71-guide-filter-menu")
            dismissMenu()
        }
        app.buttons["What's On"].firstMatch.tap()
        sleep(2)
        shot("72-whats-on")
        app.buttons["Recordings"].firstMatch.tap()
        sleep(2)
        shot("73-recordings")
        app.buttons["What's On"].firstMatch.tap()
        let watch = button("Watch ")
        XCTAssertTrue(watch.waitForExistence(timeout: 10))
        watch.tap()
        XCTAssertTrue(app.buttons["Close Live TV"].waitForExistence(timeout: 15))
        sleep(6)
        shot("74-live-player")
        app.tap()
        sleep(1)
        shot("74-live-player-overlay")
        app.buttons["Close Live TV"].tap()
    }

    /// VOD Movies and VOD Shows: the pages, their menus, a film, a show and a season.
    func testTour7Vod() throws {
        try ensureVod()
        signIn()
        openLibrariesTab()
        let movies = app.buttons["libraries.vod"]
        XCTAssertTrue(movies.waitForExistence(timeout: 15), "VOD Movies is listed")
        shot("80-libraries-with-vod")
        movies.tap()
        sleep(3)
        shot("80-vod-movies")
        app.swipeUp()
        shot("80-vod-movies-scrolled")
        app.swipeDown(); app.swipeDown()
        let filter = app.buttons["vod.filter"]
        if filter.exists, filter.isHittable {
            filter.tap()
            sleep(1)
            shot("81-vod-filter-menu")
            dismissMenu()
        }
        let sort = app.buttons["vod.sort"]
        if sort.exists, sort.isHittable {
            sort.tap()
            sleep(1)
            shot("81-vod-sort-menu")
            dismissMenu()
        }
        let tile = app.buttons.matching(identifier: "vod.tile").firstMatch
        XCTAssertTrue(tile.waitForExistence(timeout: 10))
        tile.tap()
        XCTAssertTrue(app.buttons["vod.play"].waitForExistence(timeout: 10))
        sleep(2)
        shot("82-vod-film")
        app.swipeUp()
        shot("82-vod-film-scrolled")
        app.swipeDown(); app.swipeDown()
        // The VOD player.
        app.buttons["vod.play"].tap()
        XCTAssertTrue(app.videoPlayer.waitForExistence(timeout: 15), "the VOD player opens")
        sleep(4)
        app.revealPlayerControls()
        shot("82-vod-player")
        app.closePlayer()
        XCTAssertTrue(app.buttons["vod.play"].waitForExistence(timeout: 10), "back on the film's page")
        back()
        back()

        openLibrariesTab()
        let shows = app.buttons["libraries.vodShows"]
        XCTAssertTrue(shows.waitForExistence(timeout: 15), "VOD Shows is listed")
        shows.tap()
        sleep(3)
        shot("83-vod-shows")
        let show = app.buttons.matching(identifier: "vod.showTile").matching(NSPredicate(format: "label == 'Apple Test Show'")).firstMatch
        if !show.waitForExistence(timeout: 5) {
            let field = app.textFields["vod.search"]
            field.tap()
            field.typeText("Apple Test Show")
        }
        XCTAssertTrue(show.waitForExistence(timeout: 10))
        show.tap()
        XCTAssertTrue(app.buttons["vod.show.play"].waitForExistence(timeout: 30))
        sleep(2)
        shot("84-vod-show")
        app.swipeUp()
        shot("84-vod-show-scrolled")
        let season = app.buttons["vod.season"].firstMatch
        if season.exists, season.isHittable {
            season.tap()
            sleep(1)
            shot("85-vod-season")
        }
        let episode = app.buttons["vod.episode"].firstMatch
        if episode.exists, episode.isHittable {
            episode.press(forDuration: 1.2)
            sleep(1)
            shot("86-vod-episode-menu")
            dismissMenu()
        }
    }

    /// Larger text: the main pages at accessibility sizes keep their controls reachable.
    func testTour8LargeText() {
        app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityL"]
        signIn()
        sleep(2)
        shot("90-home-large-text")
        openLibrary("Music")
        sleep(2)
        shot("91-music-large-text")
        tab("Settings")
        sleep(1)
        shot("92-settings-large-text")
        openLibrary("Movies")
        button("00 Long").firstMatch.tap()
        sleep(2)
        shot("93-film-large-text")
    }
}
