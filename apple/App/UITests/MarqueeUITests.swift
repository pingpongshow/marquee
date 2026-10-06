import CryptoKit
import XCTest

/// End-to-end flows against a running Marquee server (MARQUEE_TEST_SERVER, default the
/// local development server). Screenshots are attached to the test results.
final class MarqueeUITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var profile: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_PROFILE"] ?? "Kiddo" }

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

    private func connectAndSignIn() {
        let address = app.textFields["Home address, e.g. 10.1.1.10:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        shot("01-connect")
        address.tap()
        address.typeText(server)
        // At large text sizes the button sits under the keyboard: scroll it into view.
        let connect = app.buttons["Connect"]
        for _ in 0..<4 where !connect.isHittable { app.swipeUp() }
        connect.tap()
        let who = app.staticTexts["Who's watching?"]
        XCTAssertTrue(who.waitForExistence(timeout: 15))
        shot("02-who-is-watching")
        app.buttons[profile].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
        shot("03-home")
    }

    /// iPhone: Libraries tab → library. iPad: the library is its own tab / sidebar item.
    private func openLibrary(_ name: String) {
        let librariesTab = app.buttons["Libraries"].firstMatch
        if UIDevice.current.userInterfaceIdiom == .phone, librariesTab.exists {
            librariesTab.tap()
            let back = app.navigationBars.buttons["Libraries"]
            if back.waitForExistence(timeout: 2) { back.tap() }
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch.tap()
        } else {
            // iPad: libraries are listed in the sidebar.
            let item = app.cells[name].firstMatch
            if !item.exists {
                app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'sidebar'")).firstMatch.tap()
            }
            XCTAssertTrue(item.waitForExistence(timeout: 5))
            item.tap()
        }
    }

    /// From the Music landing page to a row of its Library list (Artists, Moods & Styles…).
    private func openMusicLibraryRow(_ id: String) {
        let shortcut = app.buttons["musicShowLibrary"]
        XCTAssertTrue(shortcut.waitForExistence(timeout: 15))
        // At large text sizes the shortcut can be scrolled off the quick actions: then scroll.
        if app.windows.firstMatch.frame.contains(shortcut.frame) { shortcut.tap() }
        let row = app.buttons["musicLibrary.\(id)"]
        for _ in 0..<12 where !row.exists { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        for _ in 0..<6 where !row.isHittable || row.frame.maxY > app.tabBars.firstMatch.frame.minY - 10 { app.swipeUp() }
        row.tap()
    }

    /// From the Music landing page: Library › Artists, then the artist's card in the full grid.
    private func openArtist(_ name: String) -> XCUIElement {
        openMusicLibraryRow("artists")
        XCTAssertTrue(app.navigationBars["Artists"].waitForExistence(timeout: 10))
        let artist = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
        for _ in 0..<5 where !artist.waitForExistence(timeout: 2) { app.swipeUp() }
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
        for _ in 0..<4 where artist.frame.maxY > app.tabBars.firstMatch.frame.minY - 10 { app.swipeUp() }
        return artist
    }

    /// From the Music landing page to Muse and the stations.
    private func openMuse() {
        let muse = app.buttons["musicMuse"]
        XCTAssertTrue(muse.waitForExistence(timeout: 15))
        muse.tap()
        XCTAssertTrue(app.navigationBars["Muse & Stations"].waitForExistence(timeout: 10))
    }

    func testBrowseAndPlay() {
        connectAndSignIn()
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        shot("04-movies")
        let first = app.scrollViews.buttons.firstMatch
        XCTAssertTrue(first.waitForExistence(timeout: 10))
        first.tap()
        let play = app.buttons["Play"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        shot("05-movie-detail")
        play.tap()
        let close = app.buttons["Close player"]
        XCTAssertTrue(close.waitForExistence(timeout: 10))
        sleep(6)
        shot("06-video-playing")
        close.tap()

        // Music: play an artist and open Now Playing.
        openLibrary("Music")
        XCTAssertTrue(app.navigationBars["Music"].waitForExistence(timeout: 10))
        let artist = openArtist("Calm Pads")
        artist.tap()
        let playMusic = app.buttons["Play"].firstMatch
        XCTAssertTrue(playMusic.waitForExistence(timeout: 10))
        shot("07-artist")
        playMusic.tap()
        sleep(3)
        shot("08-mini-player")
        app.buttons["Pause"].firstMatch.tap()
        shot("09-paused")
    }

    /// Muse, Now Playing's source and lyrics (M6.5). Needs the test music library and
    /// the Soundprint analysis sidecar.
    func testMusicFeatures() {
        connectAndSignIn()
        openLibrary("Music")
        openMuse()
        XCTAssertTrue(app.staticTexts["Muse"].waitForExistence(timeout: 10))
        shot("m1-discover")

        let prompt = app.textFields["Describe what you want to hear…"]
        prompt.tap()
        prompt.typeText("white noise and static hiss\n")
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20), "Muse should start a noise track")
        XCTAssertTrue(mini.label.contains("Hiss Theory") || mini.label.contains("Static Kids"), mini.label)
        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["white noise and static hiss"].exists)
        shot("m2-now-playing-muse")
        app.buttons["Up Next"].tap()
        shot("m3-up-next")
        app.buttons["Now Playing"].firstMatch.tap()
        app.swipeDown(velocity: .fast)
        app.navigationBars.buttons["Music"].firstMatch.tap() // back to the landing page

        // An album with an .lrc sidecar: synced lyrics.
        let artist = openArtist("Calm Pads")
        artist.tap()
        let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        album.tap()
        XCTAssertTrue(app.buttons["Radio"].waitForExistence(timeout: 10))
        app.buttons["Play"].firstMatch.tap()
        sleep(2)
        XCTAssertTrue(mini.label.contains("Floating 1"), mini.label)
        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        app.buttons["Lyrics"].tap()
        XCTAssertTrue(app.staticTexts["Floating on a quiet sea"].waitForExistence(timeout: 10))
        sleep(4)
        shot("m4-lyrics")
        app.buttons["Now Playing"].firstMatch.tap()
        app.buttons["3 stars"].tap()
        shot("m5-rated")
    }

    /// Collections in a movie library and the watchlist (META-7, USER-8). Needs the "Test
    /// Saga" collection on the test server.
    func testCollectionsAndWatchlist() {
        connectAndSignIn()
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        app.buttons["Sort and filter"].tap()
        app.buttons["Collections"].tap()
        let saga = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Test Saga'")).firstMatch
        XCTAssertTrue(saga.waitForExistence(timeout: 10))
        saga.tap()
        XCTAssertTrue(app.staticTexts["In this collection"].waitForExistence(timeout: 10))
        shot("c1-collection")
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH '20 Valley'")).firstMatch.tap()
        let add = app.buttons["Watchlist"]
        XCTAssertTrue(add.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Test Saga"].waitForExistence(timeout: 10), "the movie shows its collection")
        add.tap()
        XCTAssertTrue(app.buttons["On Watchlist"].waitForExistence(timeout: 5))
        shot("c2-watchlisted")
        app.buttons["On Watchlist"].tap()
        XCTAssertTrue(app.buttons["Watchlist"].waitForExistence(timeout: 5))
    }

    /// Offline downloads (M7): convert on the server, download, play from the device.
    func testDownloadAndPlayOffline() {
        connectAndSignIn()
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        let movie = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '00 Preview Test'")).firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        movie.tap()
        // Left over from an earlier run: delete it first.
        if app.buttons["Downloaded"].waitForExistence(timeout: 3) {
            app.buttons["Downloaded"].tap()
            app.buttons["Delete Download"].tap()
        }
        let download = app.buttons["Download"]
        XCTAssertTrue(download.waitForExistence(timeout: 10))
        download.tap()
        app.buttons["Medium (720p)"].tap()
        XCTAssertTrue(app.buttons["Downloaded"].waitForExistence(timeout: 120), "conversion and download finish")
        shot("d1-downloaded")

        // Libraries → Downloads → play it.
        app.buttons["Libraries"].firstMatch.tap()
        let back = app.navigationBars.buttons["Libraries"]
        if back.waitForExistence(timeout: 2) { back.tap() }
        app.staticTexts["Downloads"].firstMatch.tap()
        let entry = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '00 Preview Test'")).firstMatch
        XCTAssertTrue(entry.waitForExistence(timeout: 10))
        shot("d2-downloads")
        entry.tap()
        XCTAssertTrue(app.buttons["Close player"].waitForExistence(timeout: 10))
        sleep(4)
        shot("d3-playing-offline")
        app.buttons["Close player"].tap()

        // Clean up so the test can run again.
        XCTAssertTrue(entry.waitForExistence(timeout: 5))
        entry.press(forDuration: 1.2)
        app.buttons["Delete Download"].tap()
    }

    /// Discover (REQ-1): browse Seerr, request a title, see it under My Requests, withdraw it.
    /// Needs the server connected to Seerr and the test profile allowed to request.
    func testDiscoverAndRequest() {
        connectAndSignIn()
        if UIDevice.current.userInterfaceIdiom == .phone {
            app.buttons["Libraries"].firstMatch.tap()
            let back = app.navigationBars.buttons["Libraries"]
            if back.waitForExistence(timeout: 2) { back.tap() }
            let discover = app.buttons["Discover"].firstMatch
            XCTAssertTrue(discover.waitForExistence(timeout: 10))
            discover.tap()
        } else {
            app.buttons["Discover"].firstMatch.tap()
        }
        XCTAssertTrue(app.navigationBars["Discover"].waitForExistence(timeout: 10))
        let request = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Request '")).firstMatch
        XCTAssertTrue(request.waitForExistence(timeout: 20), "Discover lists requestable titles")
        sleep(2)
        shot("r1-discover")
        request.tap()
        let confirm = app.navigationBars["Request"].buttons["Request"]
        XCTAssertTrue(confirm.waitForExistence(timeout: 10))
        // Shows load their seasons first.
        let enabled = NSPredicate(format: "isEnabled == true")
        expectation(for: enabled, evaluatedWith: confirm)
        waitForExpectations(timeout: 10)
        shot("r2-request-sheet")
        confirm.tap()
        let withdraw = app.buttons["Withdraw"].firstMatch
        XCTAssertTrue(withdraw.waitForExistence(timeout: 15), "the request shows under My Requests")
        for _ in 0..<8 where !withdraw.isHittable { app.scrollViews.firstMatch.swipeUp() }
        shot("r3-my-requests")
        withdraw.tap()
        XCTAssertTrue(withdraw.waitForNonExistence(timeout: 10))
    }

    /// Live TV (LIVE-2/3): guide with a live preview, What's On, full-screen watching and
    /// channel up. Needs a Live TV source (scripts/fake-iptv.py in development).
    func testLiveTV() {
        connectAndSignIn()
        // Live TV is with the libraries, not a tab of its own (iPhone).
        if UIDevice.current.userInterfaceIdiom == .phone {
            XCTAssertTrue(app.tabBars.buttons["Libraries"].waitForExistence(timeout: 10))
            XCTAssertFalse(app.tabBars.buttons["Live TV"].exists, "Live TV isn't a tab")
            app.tabBars.buttons["Libraries"].tap()
            let row = app.buttons["libraries.liveTV"]
            XCTAssertTrue(row.waitForExistence(timeout: 10), "Live TV is listed with the libraries")
            shot("l0-libraries")
        }
        openLibrary("Live TV")
        XCTAssertTrue(app.navigationBars["Live TV"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Unmute"].waitForExistence(timeout: 20), "the preview tunes")
        sleep(5)
        shot("l1-guide")
        app.buttons["What's On"].firstMatch.tap()
        let watch = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Watch '")).firstMatch
        XCTAssertTrue(watch.waitForExistence(timeout: 10))
        shot("l2-whats-on")
        watch.tap()
        let close = app.buttons["Close Live TV"]
        XCTAssertTrue(close.waitForExistence(timeout: 15))
        sleep(6)
        shot("l3-watching")
        app.buttons["Channel up"].tap()
        sleep(6)
        shot("l4-channel-up")
        close.tap()
    }

    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

    /// Calls the server as the test admin (the other viewer in watch-together tests).
    @discardableResult
    private func adminAPI(_ method: String, _ path: String, _ body: [String: Any]? = nil, as userToken: String? = nil) throws -> [String: Any] {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        let token = userToken ?? admin
        var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
        req.httpMethod = method
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
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

    /// Watch together (SYNC-1): the admin starts a group over the API; this device joins from
    /// Home, then follows the admin's pause.
    func testWatchTogether() throws {
        let g = try adminAPI("POST", "/syncplay/groups", ["itemId": 359, "positionMs": 30000])
        let id = try XCTUnwrap(g["id"] as? String)
        defer { _ = try? adminAPI("POST", "/syncplay/groups/\(id)/leave") }
        connectAndSignIn()
        let join = app.buttons["Join 00 Preview Test"]
        XCTAssertTrue(join.waitForExistence(timeout: 30), "Home offers the group")
        shot("w1-home-join")
        join.tap()
        XCTAssertTrue(app.buttons["Watching together"].waitForExistence(timeout: 20))
        sleep(4)
        var state = try adminAPI("GET", "/syncplay/groups/\(id)")
        XCTAssertEqual((state["members"] as? [[String: Any]])?.count, 2)
        shot("w2-joined")
        try adminAPI("POST", "/syncplay/groups/\(id)/command", ["action": "pause", "positionMs": 60000])
        sleep(4)
        // The player followed: shows Play, and the group is still paused at the admin's spot.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        XCTAssertTrue(app.buttons["Play"].waitForExistence(timeout: 5), "the player paused with the group")
        state = try adminAPI("GET", "/syncplay/groups/\(id)")
        XCTAssertEqual(state["playing"] as? Bool, false)
        shot("w3-paused-by-admin")
        app.buttons["Close player"].tap()
    }

    /// Moods and styles (MUSIC-18): a mood page lists tracks and starts its radio.
    func testMoodsAndStyles() {
        connectAndSignIn()
        openLibrary("Music")
        openMusicLibraryRow("moods")
        let chill = app.buttons["Chill mood"]
        XCTAssertTrue(chill.waitForExistence(timeout: 15))
        for _ in 0..<5 where !chill.isHittable { app.swipeUp() }
        shot("ms1-tiles")
        chill.tap()
        XCTAssertTrue(app.buttons["Play Chill Radio"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Tracks"].waitForExistence(timeout: 20), "the mood's tracks load")
        shot("ms2-mood")
        app.buttons["Play Chill Radio"].tap()
        XCTAssertTrue(app.buttons["miniPlayer"].waitForExistence(timeout: 15))
    }

    /// The Music landing page is organised like Plexamp (MUSIC-15): quick actions, shelves and
    /// a Library list with counts. Never every artist: the only artist cards are the Top Artists
    /// shelf's (at most 20), and Library › Artists opens the full grid.
    func testMusicLandingPage() throws {
        let artistNames = try musicArtistNames()
        connectAndSignIn()
        openLibrary("Music")
        XCTAssertTrue(app.navigationBars["Music"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Shuffle All"].waitForExistence(timeout: 10), "quick actions")
        shot("mh1-landing")

        // Down the whole page, checking every artist card on the way.
        var topArtistIDs = Set<String>()
        for step in 0..<10 {
            // Artist cards read "Name, N albums"; album cards start with the album's title.
            let outside = app.buttons.matching(NSPredicate(format: "label CONTAINS ' album' AND NOT (identifier BEGINSWITH 'topArtist.')"))
            for label in outside.allElementsBoundByIndex.map(\.label) {
                XCTAssertFalse(artistNames.contains { label.hasPrefix($0 + ",") }, "artist card outside Top Artists: \(label)")
            }
            let top = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'topArtist.'"))
            topArtistIDs.formUnion(top.allElementsBoundByIndex.map(\.identifier))
            XCTAssertFalse(app.descendants(matching: .any)["libraryGrid"].exists, "no artist grid on the landing page")
            if app.buttons["musicLibrary.muse"].exists && app.buttons["musicLibrary.muse"].isHittable && step > 0 { break }
            app.swipeUp()
        }
        XCTAssertFalse(topArtistIDs.isEmpty, "the Top Artists shelf shows (the profile has plays)")
        XCTAssertLessThanOrEqual(topArtistIDs.count, 20, "Top Artists is capped")
        shot("mh2-landing-bottom")

        // The Library list, with counts.
        for id in ["artists", "albums", "songs", "playlists", "genres"] {
            XCTAssertTrue(app.buttons["musicLibrary.\(id)"].exists, "Library row: \(id)")
        }
        for id in ["artists", "albums", "songs"] {
            let count = app.descendants(matching: .any)["musicLibrary.\(id).count"]
            XCTAssertTrue(count.waitForExistence(timeout: 10), "\(id) shows its count")
            XCTAssertNotNil(Int(count.label.filter(\.isNumber)), "\(id): \(count.label)")
        }
        XCTAssertEqual(Int(app.descendants(matching: .any)["musicLibrary.artists.count"].label.filter(\.isNumber)), artistNames.count)
        XCTAssertGreaterThan(artistNames.count, topArtistIDs.count, "the home shows fewer artists than the library has")

        openMusicLibraryRow("artists")
        XCTAssertTrue(app.navigationBars["Artists"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.descendants(matching: .any)["libraryGrid"].waitForExistence(timeout: 10), "Artists is the full grid")
        XCTAssertTrue(app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Sort and filter"].exists, "the grid sorts")
        shot("mh3-artists")
        app.navigationBars.buttons["Music"].firstMatch.tap()

        openMusicLibraryRow("songs")
        XCTAssertTrue(app.navigationBars["Songs"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS 'Floating'")).firstMatch.waitForExistence(timeout: 10), "songs are listed")
        app.navigationBars.buttons["Music"].firstMatch.tap()

        openMusicLibraryRow("moods")
        XCTAssertTrue(app.navigationBars["Moods & Styles"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Chill mood"].waitForExistence(timeout: 10))
    }

    /// Every artist in the test server's Music library.
    private func musicArtistNames() throws -> [String] {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        func get(_ path: String) -> Any? {
            var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
            req.setValue("Bearer \(admin)", forHTTPHeaderField: "Authorization")
            let done = expectation(description: path)
            var out: Any?
            URLSession.shared.dataTask(with: req) { data, _, _ in
                out = data.flatMap { try? JSONSerialization.jsonObject(with: $0) }
                done.fulfill()
            }.resume()
            wait(for: [done], timeout: 30)
            return out
        }
        let libs = get("/libraries") as? [[String: Any]] ?? []
        let music = try XCTUnwrap(libs.first { $0["type"] as? String == "music" && $0["name"] as? String == "Music" }?["id"] as? Int)
        let page = get("/libraries/\(music)/items?type=artist&limit=500") as? [String: Any] ?? [:]
        return (page["items"] as? [[String: Any]] ?? []).compactMap { $0["title"] as? String }
    }

    /// The text sizes the layout tests run at: the default and a large accessibility size.
    private let textSizes: [String?] = [nil, "UICTContentSizeCategoryAccessibilityL"]

    /// Starts the app again (signed out) at a text size, and signs in.
    private func relaunch(textSize: String?) {
        app.terminate()
        app.launchArguments = ["-marquee-reset"] + (textSize.map { ["-UIPreferredContentSizeCategoryName", $0] } ?? [])
        app.launch()
        connectAndSignIn()
    }

    /// Header actions fit on any iPhone (standard and Pro Max), at the default and a large text
    /// size, in portrait and landscape: each is on screen, can be tapped, and is a normal button
    /// shape (labels never wrap letter by letter), on a playlist, an album and a collection.
    func testHeaderButtonsFit() {
        XCUIDevice.shared.orientation = .portrait
        addTeardownBlock { XCUIDevice.shared.orientation = .portrait }
        for size in textSizes {
            let tag = size == nil ? "default" : "large"
            relaunch(textSize: size)
            app.buttons["Libraries"].firstMatch.tap()
            let back = app.navigationBars.buttons["Libraries"]
            if back.waitForExistence(timeout: 2) { back.tap() }
            let playlists = app.buttons["Playlists"].firstMatch
            for _ in 0..<4 where !playlists.isHittable { app.swipeUp() }
            playlists.tap()
            let trip = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Road Trip'")).firstMatch
            XCTAssertTrue(trip.waitForExistence(timeout: 10))
            trip.tap()
            let download = app.buttons.matching(NSPredicate(format: "label == 'Download' OR label == 'Downloaded' OR label MATCHES '[0-9]+/[0-9]+'")).firstMatch
            let pin = app.buttons.matching(NSPredicate(format: "label == 'Pin to Home' OR label == 'Unpin from Home'")).firstMatch
            let play = app.buttons["Play"].firstMatch, shuffle = app.buttons["Shuffle"].firstMatch
            XCTAssertTrue(shuffle.waitForExistence(timeout: 10))
            sleep(1)
            shot("hb1-playlist-\(tag)")
            checkFits([play, shuffle, download, pin], "playlist (\(tag))")
            // Stacked on a phone: the actions start under the cover, at the left edge.
            XCTAssertLessThan(play.frame.minX, 40, "playlist (\(tag)): actions are full width, not in a column beside the cover")
            if size == nil {
                XCTAssertGreaterThan(play.frame.width, 70, "Play keeps its word, not just the icon")
                XCUIDevice.shared.orientation = .landscapeLeft
                sleep(2)
                shot("hb1-playlist-landscape")
                checkFits([play, shuffle, download, pin], "playlist (landscape)")
                XCUIDevice.shared.orientation = .portrait
                sleep(2)
            }

            // An album page.
            openLibrary("Music")
            openArtist("Calm Pads").tap()
            let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
            XCTAssertTrue(album.waitForExistence(timeout: 10))
            album.tap()
            XCTAssertTrue(app.buttons["Radio"].waitForExistence(timeout: 10))
            sleep(1)
            shot("hb2-album-\(tag)")
            checkFits([play, shuffle, app.buttons["Radio"].firstMatch, app.buttons["More"].firstMatch], "album (\(tag))")
            if size == nil { XCTAssertGreaterThan(play.frame.width, 70, "the album's Play keeps its word") }

            // A collection page.
            openLibrary("Movies")
            XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
            app.buttons["Sort and filter"].tap()
            app.buttons["Collections"].tap()
            let saga = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Test Saga'")).firstMatch
            if saga.waitForExistence(timeout: 10) {
                saga.tap()
                let pinCollection = app.buttons.matching(NSPredicate(format: "label == 'Pin to Home' OR label == 'Unpin from Home'")).firstMatch
                XCTAssertTrue(pinCollection.waitForExistence(timeout: 10))
                sleep(1)
                shot("hb3-collection-\(tag)")
                checkFits([pinCollection, app.buttons["More"].firstMatch], "collection (\(tag))")
            }
        }
    }

    /// The year-in-music row on the Music home is one compact line, and a movie's track
    /// choices are uniform one-line chips, at the default and a large text size.
    func testYearRowAndTrackChips() {
        for size in textSizes {
            let tag = size == nil ? "default" : "large"
            relaunch(textSize: size)
            openLibrary("Music")
            let row = app.descendants(matching: .any)["recapRow"]
            for _ in 0..<5 where !(row.exists && row.isHittable) { app.swipeUp() }
            XCTAssertTrue(row.waitForExistence(timeout: 15), "the Music home has the year row")
            shot("yr1-row-\(tag)")
            let window = app.windows.firstMatch.frame
            XCTAssertLessThan(row.frame.height, 60, "\(tag): the year row is compact: \(row.frame)")
            XCTAssertTrue(window.contains(row.frame), "\(tag): the year row is on screen")
            XCTAssertTrue(app.buttons["recapCard"].exists)

            openLibrary("Movies")
            XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
            var movie = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH '31 Ocean'")).firstMatch
            if !movie.waitForExistence(timeout: 5) { movie = app.scrollViews.buttons.firstMatch }
            for _ in 0..<5 where !movie.isHittable { app.swipeUp() }
            movie.tap()
            let find = app.buttons["trackChip.find"]
            for _ in 0..<5 where !(find.exists && find.isHittable) { app.swipeUp() }
            XCTAssertTrue(find.waitForExistence(timeout: 10))
            sleep(1)
            shot("tc1-chips-\(tag)")
            let chips = ["trackChip.version", "trackChip.audio", "trackChip.subtitles", "trackChip.find"]
                .map { app.buttons[$0] }.filter(\.exists)
            let heights = chips.map(\.frame.height)
            for c in chips {
                let f = c.frame
                XCTAssertFalse(c.label.isEmpty, "\(tag): a chip without a label")
                XCTAssertLessThan(f.height, 60, "\(tag): \(c.label) is one line: \(f)")
                XCTAssertGreaterThanOrEqual(f.width, f.height, "\(tag): \(c.label) isn't squeezed: \(f)")
                XCTAssertTrue(window.contains(f) || !c.isHittable, "\(tag): \(c.label) \(f)")
            }
            if let lo = heights.min(), let hi = heights.max() {
                XCTAssertLessThanOrEqual(hi - lo, 1, "\(tag): chips are the same height: \(heights)")
            }
        }
    }

    private func checkFits(_ buttons: [XCUIElement], _ page: String) {
        let window = app.windows.firstMatch.frame
        for b in buttons {
            XCTAssertTrue(b.waitForExistence(timeout: 5), "\(page): \(b)")
            let f = b.frame
            XCTAssertFalse(b.label.isEmpty, "\(page): a button without a label")
            XCTAssertTrue(window.contains(f), "\(page): \(b.label) \(f) is outside \(window)")
            XCTAssertLessThan(f.height, 60, "\(page): \(b.label) is a tall bubble \(f)")
            XCTAssertGreaterThanOrEqual(f.width, f.height * 0.9, "\(page): \(b.label) is squeezed \(f)")
            // SwiftUI menus report not hittable while on screen.
            if !["Downloaded", "More"].contains(b.label), !b.label.contains("/") {
                XCTAssertTrue(b.isHittable, "\(page): \(b.label) can be tapped")
            }
        }
    }

    /// Playlist downloads (MUSIC-19): keep the profile's "Road Trip" playlist on the device.
    func testPlaylistDownload() {
        connectAndSignIn()
        app.buttons["Libraries"].firstMatch.tap()
        let back = app.navigationBars.buttons["Libraries"]
        if back.waitForExistence(timeout: 2) { back.tap() }
        app.buttons["Playlists"].firstMatch.tap()
        let trip = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Road Trip'")).firstMatch
        XCTAssertTrue(trip.waitForExistence(timeout: 10))
        trip.tap()
        sleep(2)
        shot("pd0-playlist")
        let download = app.buttons["Download"]
        if !download.waitForExistence(timeout: 5) {
            // Left over from an earlier run.
            app.buttons["Downloaded"].tap()
            app.buttons["Remove Download"].tap()
        }
        XCTAssertTrue(download.waitForExistence(timeout: 5))
        download.tap()
        XCTAssertTrue(app.buttons["Downloaded"].waitForExistence(timeout: 60), "all of the playlist downloads")
        shot("pd1-playlist-downloaded")
        app.buttons["Downloaded"].tap()
        shot("pd2-menu")
        app.buttons["Remove Download"].tap()
        XCTAssertTrue(download.waitForExistence(timeout: 5))
    }

    /// Crossfade (MUSIC-9): with 4 s set, the next track takes over before the current ends.
    func testCrossfade() throws {
        connectAndSignIn()
        openLibrary("Music")
        let radio = app.buttons["Library Radio"]
        XCTAssertTrue(radio.waitForExistence(timeout: 15))
        radio.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        sleep(2)
        mini.tap()
        let menu = app.buttons["Crossfade off"].firstMatch
        if menu.waitForExistence(timeout: 5) {
            menu.tap()
            app.buttons["4 seconds"].tap()
        }
        XCTAssertTrue(app.buttons["Crossfade 4 seconds"].waitForExistence(timeout: 5))
        let slider = app.sliders.firstMatch
        XCTAssertTrue(slider.waitForExistence(timeout: 5))
        let titleText = app.staticTexts["nowPlayingTitle"]
        XCTAssertTrue(titleText.waitForExistence(timeout: 5))
        let title = titleText.label
        slider.adjust(toNormalizedSliderPosition: 0.6)
        sleep(1)
        // Time left, from the "-0:16" label.
        let left = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '-'")).firstMatch.label
        let parts = left.dropFirst().split(separator: ":").compactMap { Double($0) }
        let remaining = parts.count == 2 ? parts[0] * 60 + parts[1] : 0
        XCTAssertGreaterThan(remaining, 6, "seek landed far enough from the end: \(left)")
        let start = Date()
        let changed = NSPredicate(format: "label != %@", title)
        expectation(for: changed, evaluatedWith: titleText)
        waitForExpectations(timeout: remaining + 5)
        let took = Date().timeIntervalSince(start)
        // The hand-over is exactly 4 s before the end; the title check polls about once a second.
        XCTAssertLessThan(took, remaining - 1, "next track should take over ~4 s before the end (took \(took)s of \(remaining)s)")
        shot("cf1-after-crossfade")
        app.buttons["Crossfade 4 seconds"].tap()
        app.buttons["Off"].tap()
    }

    /// Your Stats (ADM-4) and Sound Journey from a track's menu (MUSIC-4).
    func testStatsAndJourney() {
        connectAndSignIn()
        app.buttons["Settings"].firstMatch.tap()
        let stats = app.buttons["Your Stats"]
        XCTAssertTrue(stats.waitForExistence(timeout: 10))
        stats.tap()
        XCTAssertTrue(app.staticTexts["Plays"].waitForExistence(timeout: 10) || app.staticTexts["Nothing played in this period."].exists)
        app.buttons["All time"].tap()
        XCTAssertTrue(app.staticTexts["Top artists"].waitForExistence(timeout: 10))
        shot("s1-stats")

        // A track's menu → Sound Journey → pick a destination.
        openLibrary("Music")
        let artist = openArtist("Calm Pads")
        artist.tap()
        let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        album.tap()
        let track = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Floating 2'")).firstMatch
        XCTAssertTrue(track.waitForExistence(timeout: 10))
        track.press(forDuration: 1.2)
        app.buttons["Sound Journey…"].tap()
        XCTAssertTrue(app.navigationBars["Sound Journey"].waitForExistence(timeout: 5))
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        search.tap()
        search.typeText("Thump")
        let dest = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Thump'")).firstMatch
        XCTAssertTrue(dest.waitForExistence(timeout: 10))
        shot("s2-journey")
        dest.tap()
        XCTAssertTrue(app.buttons["miniPlayer"].waitForExistence(timeout: 15))
        app.buttons["miniPlayer"].tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Sound Journey'")).firstMatch.exists)
        shot("s3-journey-playing")
    }

    /// RFC 6238 code for a base32 secret, `ahead` steps from now.
    private func totp(_ secret: String, ahead: Int = 0) -> String {
        let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
        var bits = 0, value = 0
        var key = [UInt8]()
        for c in secret.uppercased() {
            guard let i = alphabet.firstIndex(of: c) else { continue }
            value = (value << 5) | i
            bits += 5
            if bits >= 8 { key.append(UInt8((value >> (bits - 8)) & 0xFF)); bits -= 8 }
        }
        var step = UInt64(Int(Date().timeIntervalSince1970) / 30 + ahead).bigEndian
        let mac = Array(HMAC<Insecure.SHA1>.authenticationCode(for: Data(bytes: &step, count: 8), using: SymmetricKey(data: key)))
        let o = Int(mac[19] & 15)
        let n = (UInt32(mac[o] & 0x7F) << 24 | UInt32(mac[o + 1]) << 16 | UInt32(mac[o + 2]) << 8 | UInt32(mac[o + 3])) % 1_000_000
        return String(format: "%06u", n)
    }

    /// Two-factor sign-in (USER-9): a password account with an authenticator is asked for a code.
    func testTwoFactorSignIn() throws {
        let name = "twofa-ios-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try adminAPI("POST", "/users", ["username": name, "displayName": name, "password": pass])
        let id = try XCTUnwrap(user["id"] as? Int)
        defer { _ = try? adminAPI("DELETE", "/users/\(id)") }
        let login = try adminAPI("POST", "/auth/login", ["username": name, "password": pass, "device": ["clientId": "uitest-\(name)", "name": "UI test", "platform": "ios"]])
        let token = try XCTUnwrap(login["token"] as? String)
        let setup = try adminAPI("POST", "/auth/totp/setup", as: token)
        let secret = try XCTUnwrap(setup["secret"] as? String)
        let on = try adminAPI("POST", "/auth/totp/enable", ["code": totp(secret)], as: token)
        XCTAssertEqual((on["recoveryCodes"] as? [Any])?.count, 10)

        let address = app.textFields["Home address, e.g. 10.1.1.10:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.tap()
        address.typeText(server)
        app.buttons["Connect"].tap()
        let other = app.buttons["Sign in with username and password"]
        XCTAssertTrue(other.waitForExistence(timeout: 15))
        other.tap()
        let username = app.textFields["Username"]
        XCTAssertTrue(username.waitForExistence(timeout: 5))
        username.tap()
        username.typeText(name)
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText(pass)
        app.buttons["Sign In"].tap()
        let code = app.textFields["totpCode"]
        XCTAssertTrue(code.waitForExistence(timeout: 10))
        shot("t1-code-asked")
        code.tap()
        code.typeText(totp(secret, ahead: 1)) // the enable code's step is used up
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
        shot("t2-signed-in")
    }

    /// Calls the server as the test admin and returns a JSON array.
    private func adminList(_ path: String) throws -> [[String: Any]] {
        guard let token = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
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

    /// DVR (LIVE-5): record an upcoming programme from the guide, see it marked, then cancel
    /// it from Recordings.
    func testRecording() throws {
        // A clean DVR, and the profile allowed to record.
        for r in try adminList("/livetv/recording-rules") { try adminAPI("DELETE", "/livetv/recording-rules/\(r["id"] as! Int)") }
        for r in try adminList("/livetv/recordings") where r["status"] as? String == "scheduled" {
            try adminAPI("DELETE", "/livetv/recordings/\(r["id"] as! Int)")
        }
        let kid = try XCTUnwrap(try adminList("/users").first { $0["displayName"] as? String == profile })
        var restrictions = kid["restrictions"] as? [String: Any] ?? [:]
        restrictions["canRecord"] = true
        try adminAPI("PATCH", "/users/\(kid["id"] as! Int)", ["restrictions": restrictions])

        connectAndSignIn()
        openLibrary("Live TV")
        XCTAssertTrue(app.navigationBars["Live TV"].waitForExistence(timeout: 10))
        // Move the guide on 90 minutes, so what's on screen hasn't started, and pick one.
        XCTAssertTrue(app.buttons["Later"].waitForExistence(timeout: 10))
        app.buttons["Later"].tap()
        // Wait for the later window: nothing on now is shown any more.
        let onNow = app.buttons.matching(NSPredicate(format: "label CONTAINS 'minutes left'"))
        let moved = expectation(for: NSPredicate(format: "count == 0"), evaluatedWith: onNow)
        wait(for: [moved], timeout: 15)
        let later = app.buttons.matching(NSPredicate(format: "label MATCHES %@", ".*, [0-9]{1,2}:[0-9]{2}.(AM|PM), .*"))
        XCTAssertTrue(later.firstMatch.waitForExistence(timeout: 15))
        let screen = app.windows.firstMatch.frame
        let upcoming = try XCTUnwrap(later.allElementsBoundByIndex.first { screen.contains($0.frame) })
        upcoming.tap()
        let record = app.buttons["Record"]
        XCTAssertTrue(record.waitForExistence(timeout: 10))
        shot("r1-programme")
        record.tap()
        // What was scheduled, from the server.
        var title = ""
        for _ in 0..<20 where title.isEmpty {
            title = try adminList("/livetv/recordings").first { $0["status"] as? String == "scheduled" }?["title"] as? String ?? ""
            if title.isEmpty { sleep(1) }
        }
        XCTAssertFalse(title.isEmpty, "a recording was scheduled")
        let marked = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ AND label ENDSWITH 'will record'", title + ",")).firstMatch
        XCTAssertTrue(marked.waitForExistence(timeout: 10), "the guide marks it")
        shot("r2-guide-marked")

        app.buttons["Recordings"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Upcoming"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts[title].exists)
        shot("r3-recordings")
        app.buttons["Cancel"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Upcoming"].waitForNonExistence(timeout: 10))
    }

    // MARK: - Playback and Home extras (PLAY-17/18/19, USER-12/13, META-7, equaliser)

    /// Opens a movie by title from the Movies library.
    private func openMovie(_ prefix: String) {
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        let movie = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10))
        movie.tap()
    }

    /// Cinema trailers left on by another test would play first: skip them.
    private func skipTrailersIfAny() {
        let skipAll = app.buttons["Skip All"]
        if skipAll.waitForExistence(timeout: 3) { skipAll.tap() }
    }

    /// Speed and subtitle/audio timing from the player's settings menu (PLAY-19, PLAY-17).
    func testPlayerSpeedAndTiming() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        openMovie("00 Long Test")
        let play = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        skipTrailersIfAny()
        let settings = app.buttons["Playback settings"]
        XCTAssertTrue(settings.waitForExistence(timeout: 15))
        sleep(3)
        settings.tap()
        let speed = app.buttons["Speed"].firstMatch
        XCTAssertTrue(speed.waitForExistence(timeout: 5))
        shot("ps1-settings-menu")
        speed.tap()
        let fast = app.buttons["1.5×"].firstMatch
        XCTAssertTrue(fast.waitForExistence(timeout: 5))
        shot("ps2-speeds")
        fast.tap()
        // The menu shows the choice next time.
        settings.tap()
        app.buttons["Speed"].firstMatch.tap()
        XCTAssertTrue(app.buttons["1.5×"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["1.5×"].firstMatch.isSelected, "1.5× is the chosen speed")
        app.buttons["1.5×"].firstMatch.tap()

        // Subtitle timing: +200 ms restarts the stream with the offset.
        settings.tap()
        let subs = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Subtitle Timing'")).firstMatch
        XCTAssertTrue(subs.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Audio Timing'")).firstMatch.exists)
        subs.tap()
        let value = app.staticTexts["timingValue"]
        XCTAssertTrue(value.waitForExistence(timeout: 5))
        XCTAssertEqual(value.label, "0 ms")
        app.buttons["100 ms later"].tap()
        app.buttons["100 ms later"].tap()
        XCTAssertEqual(value.label, "+200 ms")
        shot("ps3-subtitle-timing")
        sleep(4) // the stream restarts with it
        app.buttons["Done"].tap()
        app.buttons["Close player"].tap()

        // The server remembers it for this file: playing again starts with +200 ms, at 1×.
        let again = app.buttons.matching(NSPredicate(format: "label IN {'Play', 'Resume'}")).firstMatch
        XCTAssertTrue(again.waitForExistence(timeout: 10))
        again.tap()
        skipTrailersIfAny()
        XCTAssertTrue(settings.waitForExistence(timeout: 15))
        sleep(3)
        settings.tap()
        let remembered = app.buttons["Subtitle Timing (+200 ms)"]
        XCTAssertTrue(remembered.waitForExistence(timeout: 5), "the offset was remembered")
        shot("ps4-remembered")
        app.buttons["Speed"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Normal (1×)"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Normal (1×)"].firstMatch.isSelected, "a new video starts at 1×")
        app.buttons["Normal (1×)"].firstMatch.tap()
        // Reset (so the remembered offset doesn't follow the next run).
        settings.tap()
        remembered.tap()
        XCTAssertTrue(value.waitForExistence(timeout: 5))
        app.buttons["Reset"].tap()
        XCTAssertEqual(value.label, "0 ms")
        sleep(4)
        app.buttons["Done"].tap()
        app.buttons["Close player"].tap()
    }

    /// Puts the cinema settings back when the test ends, however it ends.
    private func restoreCinema(_ before: [String: Any]) {
        let url = "http://\(server)/api/v1/settings", token = adminToken
        let body: [String: Any] = ["cinema": ["trailers": before["trailers"] as? Int ?? 0, "prerollItemId": before["prerollItemId"] as? Int ?? 0]]
        addTeardownBlock { Self.send("PATCH", url, token: token, body) }
    }

    /// Cinema trailers (PLAY-18): with one trailer on, a movie started from the beginning plays
    /// another movie's trailer first, named, with Skip All.
    func testCinemaTrailers() throws {
        let before = try adminAPI("GET", "/settings")["cinema"] as? [String: Any] ?? [:]
        try adminAPI("PATCH", "/settings", ["cinema": ["trailers": 1, "prerollItemId": 0]])
        restoreCinema(before)
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        openMovie("00 Long Test")
        let play = app.buttons["Play"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        let label = app.staticTexts["trailerTitle"]
        XCTAssertTrue(label.waitForExistence(timeout: 15), "a trailer plays first")
        XCTAssertEqual(label.label, "Trailer · 00 Preview Test")
        XCTAssertTrue(app.buttons["Skip"].exists)
        sleep(2)
        shot("ct1-trailer")
        app.buttons["Skip All"].tap()
        XCTAssertTrue(label.waitForNonExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Playback settings"].waitForExistence(timeout: 10), "the movie plays")
        sleep(2)
        shot("ct2-movie")
        app.buttons["Close player"].tap()

        // The person's own setting turns them off.
        app.buttons["Settings"].firstMatch.tap()
        let toggle = app.switches["Play trailers before movies"]
        for _ in 0..<5 where !toggle.isHittable { app.swipeUp() }
        XCTAssertTrue(toggle.waitForExistence(timeout: 5))
        XCTAssertEqual(toggle.value as? String, "1")
        toggle.switches.firstMatch.tap()
        let off = NSPredicate(format: "value == '0'")
        expectation(for: off, evaluatedWith: toggle)
        waitForExpectations(timeout: 5)
        shot("ct3-setting-off")
        sleep(1)
        toggle.switches.firstMatch.tap() // back on for other tests
        expectation(for: NSPredicate(format: "value == '1'"), evaluatedWith: toggle)
        waitForExpectations(timeout: 5)
    }

    /// Edit Home and Pin to Home (USER-12): hide a row, pin a collection, open it from its
    /// Home row, unpin it, then reset the layout.
    func testEditHomeAndPin() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        let movies = app.descendants(matching: .any)["Recently Added Movies"].firstMatch
        XCTAssertTrue(movies.waitForExistence(timeout: 10))
        app.buttons["Edit Home"].tap()
        let hide = app.buttons["Hide Recently Added Movies"]
        XCTAssertTrue(hide.waitForExistence(timeout: 10))
        shot("eh1-edit-home")
        hide.tap()
        XCTAssertTrue(app.buttons["Show Recently Added Movies"].waitForExistence(timeout: 5))
        app.buttons["Done"].tap()
        XCTAssertTrue(movies.waitForNonExistence(timeout: 10), "the hidden row is gone from Home")
        shot("eh2-home-without-row")

        // Pin the Test Saga collection.
        openLibrary("Movies")
        XCTAssertTrue(app.navigationBars["Movies"].waitForExistence(timeout: 10))
        app.buttons["Sort and filter"].tap()
        app.buttons["Collections"].tap()
        let saga = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Test Saga'")).firstMatch
        XCTAssertTrue(saga.waitForExistence(timeout: 10))
        saga.tap()
        let pin = app.buttons["Pin to Home"]
        XCTAssertTrue(pin.waitForExistence(timeout: 10))
        pin.tap()
        XCTAssertTrue(app.buttons["Unpin from Home"].waitForExistence(timeout: 5))
        shot("eh3-pinned")

        // Home shows it; its title opens the collection.
        app.buttons["Home"].firstMatch.tap()
        let row = app.buttons["Test Saga"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10), "the pinned collection is a Home row")
        shot("eh4-home-pinned")
        row.tap()
        let unpin = app.buttons["Unpin from Home"]
        XCTAssertTrue(unpin.waitForExistence(timeout: 10))
        unpin.tap()
        XCTAssertTrue(app.buttons["Pin to Home"].waitForExistence(timeout: 5))
        app.navigationBars.buttons.firstMatch.tap()

        // Back to the default layout.
        app.buttons["Edit Home"].tap()
        let reset = app.buttons["Reset to Default"]
        XCTAssertTrue(reset.waitForExistence(timeout: 10))
        reset.tap()
        XCTAssertTrue(hide.waitForExistence(timeout: 5))
        app.buttons["Done"].tap()
        XCTAssertTrue(movies.waitForExistence(timeout: 10))
    }

    /// Starts the Music library's radio and opens Now Playing.
    private func playRadioAndOpenNowPlaying() {
        openLibrary("Music")
        let radio = app.buttons["Library Radio"]
        XCTAssertTrue(radio.waitForExistence(timeout: 15))
        radio.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        sleep(2)
        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
    }

    /// Settings › Music › Equalizer.
    private func openEqualizerSettings() {
        app.buttons["Settings"].firstMatch.tap()
        let link = app.buttons["settings.equalizer"]
        for _ in 0..<8 where !link.isHittable { app.swipeUp() }
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        link.tap()
        XCTAssertTrue(app.navigationBars["Equalizer"].waitForExistence(timeout: 5))
    }

    /// The equaliser (iPhone/iPad), from Settings › Music: turn it on, pick a preset, and the
    /// music keeps playing. Now Playing has no equaliser button any more.
    func testEqualizer() {
        connectAndSignIn()
        playRadioAndOpenNowPlaying()
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Equali'")).firstMatch.exists, "no EQ button in Now Playing")
        app.swipeDown(velocity: .fast)
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForNonExistence(timeout: 5))
        openEqualizerSettings()
        let toggle = app.switches["Equalizer"]
        XCTAssertTrue(toggle.waitForExistence(timeout: 5))
        if toggle.value as? String == "0" { toggle.switches.firstMatch.tap() }
        XCTAssertEqual(toggle.value as? String, "1")
        app.buttons["Bass Boost"].tap()
        XCTAssertTrue(app.buttons["Bass Boost"].isSelected)
        let low = app.sliders["31 hertz"]
        XCTAssertTrue(low.exists)
        XCTAssertEqual(low.value as? String, "+6.0 decibels")
        shot("eq1-settings")
        // A custom band makes it "Custom".
        let k1 = app.sliders["1k hertz"]
        for _ in 0..<4 where !k1.isHittable { app.swipeUp() }
        k1.adjust(toNormalizedSliderPosition: 0.8)
        XCTAssertTrue(app.staticTexts["Custom"].waitForExistence(timeout: 5))
        app.navigationBars.buttons["Settings"].tap()
        // Settings shows it's on.
        let link = app.buttons["settings.equalizer"]
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        XCTAssertTrue(link.label.contains("Custom"), link.label)
        // Still playing through the equaliser: the clock moves.
        app.buttons["miniPlayer"].tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        let clock = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '-'")).firstMatch
        let t1 = clock.label
        sleep(3)
        XCTAssertNotEqual(clock.label, t1, "music plays on with the equaliser")
        XCTAssertFalse(app.staticTexts["musicError"].exists)
        shot("eq2-playing")
        app.buttons["np.playPause"].tap()
        app.swipeDown(velocity: .fast)
        // Off again (it's kept on the device).
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        link.tap()
        app.buttons["Flat"].tap()
        app.switches["Equalizer"].switches.firstMatch.tap()
        app.navigationBars.buttons["Settings"].tap()
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        XCTAssertTrue(link.label.contains("Off"), link.label)
    }

    /// Car mode is gone: no quick action on the music home, nothing in Now Playing.
    func testNoCarMode() {
        connectAndSignIn()
        openLibrary("Music")
        XCTAssertTrue(app.buttons["Shuffle All"].waitForExistence(timeout: 15))
        XCTAssertFalse(app.buttons["Car Mode"].exists)
        app.buttons["Shuffle All"].tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Car Mode"].exists)
        app.buttons["np.playPause"].tap()
    }

    /// A preloaded next track whose session the server dropped (simulated: the first preload's
    /// session is ended as soon as it starts) is loaded again, not skipped: Next plays it, and
    /// the clock runs.
    func testDeadPreloadIsReloaded() {
        app.terminate()
        app.launchArguments = ["-marquee-reset", "-marquee-test-dead-preload"]
        app.launch()
        connectAndSignIn()
        openLibrary("Music")
        let artist = openArtist("Calm Pads")
        artist.tap()
        let play = app.buttons["Play"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        sleep(4) // the next track preloads (and its session is ended)
        mini.tap()
        XCTAssertTrue(app.buttons["Up Next"].waitForExistence(timeout: 5))
        // What should play next.
        app.buttons["Up Next"].tap()
        let upNext = app.cells.matching(NSPredicate(format: "label != ''")).element(boundBy: 2)
        sleep(1)
        shot("dp1-up-next")
        let expected = upNext.exists ? upNext.label : ""
        app.buttons["Now Playing"].firstMatch.tap()
        let title = app.staticTexts["nowPlayingTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        let first = title.label
        app.buttons["np.next"].tap()
        expectation(for: NSPredicate(format: "label != %@", first), evaluatedWith: title)
        waitForExpectations(timeout: 15)
        if !expected.isEmpty { XCTAssertTrue(expected.contains(title.label), "the next track plays (\(title.label)), not one after it: \(expected)") }
        // It plays: the clock moves and nothing failed.
        let clock = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '-'")).firstMatch
        sleep(2)
        let t1 = clock.label
        sleep(3)
        XCTAssertNotEqual(clock.label, t1, "the reloaded track plays")
        XCTAssertFalse(app.staticTexts["musicError"].exists, "no 'Couldn't play'")
        shot("dp2-next-playing")
        app.buttons["np.playPause"].tap()
    }

    /// Signs in with a username and password from the profile picker.
    private func signIn(username: String, password: String) {
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
        field.typeText(username)
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText(password)
        app.buttons["Sign In"].tap()
        XCTAssertTrue(app.navigationBars["Home"].waitForExistence(timeout: 15))
    }

    /// A temporary account (other clients' tests share the server's profiles, and their Home
    /// layouts and remembered offsets); deleted when the test ends.
    private func temporaryUser(admin: Bool = false) throws -> (name: String, password: String) {
        let name = "ui\(admin ? "admin" : "user")-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try adminAPI("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": admin])
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", token = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: token) }
        return (name, pass)
    }

    /// Sharing (USER-13): invite a friend, share the link, see the invite and delete it; the
    /// household and friends are listed apart.
    func testInviteFriend() throws {
        let admin = try temporaryUser(admin: true)
        signIn(username: admin.name, password: admin.password)
        app.buttons["Settings"].firstMatch.tap()
        let users = app.buttons["Users and Friends"]
        XCTAssertTrue(users.waitForExistence(timeout: 10))
        users.tap()
        XCTAssertTrue(app.staticTexts["HOUSEHOLD"].waitForExistence(timeout: 10) || app.staticTexts["Household"].exists)
        XCTAssertTrue(app.staticTexts["Kiddo"].waitForExistence(timeout: 10))
        shot("inv1-users")
        // Other tests' accounts can push it below the fold.
        let invite = app.buttons["Invite a Friend"]
        for _ in 0..<8 where !(invite.exists && invite.isHittable) { app.swipeUp() }
        invite.tap()
        let note = app.textFields["Note (who it's for)"]
        XCTAssertTrue(note.waitForExistence(timeout: 5))
        note.tap()
        let label = "UITest Friend \(Int.random(in: 100...999))"
        note.typeText(label)
        app.switches["Can request titles"].switches.firstMatch.tap()
        shot("inv2-form")
        app.buttons["Create Invite"].tap()
        let link = app.staticTexts["inviteLink"]
        XCTAssertTrue(link.waitForExistence(timeout: 10))
        XCTAssertTrue(link.label.hasPrefix("http://\(server)/join/"), link.label)
        shot("inv3-link")
        // The system share sheet.
        app.buttons["Share Link"].tap()
        let copy = app.buttons["Copy"].firstMatch
        XCTAssertTrue(copy.waitForExistence(timeout: 10) || app.otherElements["ActivityListView"].waitForExistence(timeout: 2), "the share sheet opens")
        shot("inv4-share-sheet")
        if copy.exists { copy.tap() } else { app.buttons["Close"].firstMatch.tap() }
        XCTAssertTrue(app.buttons["Done"].waitForExistence(timeout: 5))
        app.buttons["Done"].tap()

        // Listed as pending; the server has it with the chosen restrictions.
        let row = app.staticTexts[label]
        for _ in 0..<8 where !(row.exists && row.isHittable) { app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        let created = try XCTUnwrap(try adminList("/invites").first { $0["note"] as? String == label })
        XCTAssertEqual((created["restrictions"] as? [String: Any])?["canRequest"] as? Bool, true)
        shot("inv5-invites")
        app.buttons["Delete invite \(label)"].tap()
        XCTAssertTrue(row.waitForNonExistence(timeout: 10))
        XCTAssertNil(try adminList("/invites").first { $0["note"] as? String == label })
    }

    /// Cinema trailer settings (admin): change the count, save, and put it back.
    func testCinemaSettings() throws {
        let before = try adminAPI("GET", "/settings")["cinema"] as? [String: Any] ?? [:]
        let was = before["trailers"] as? Int ?? 0
        restoreCinema(before)
        let admin = try temporaryUser(admin: true)
        signIn(username: admin.name, password: admin.password)
        app.buttons["Settings"].firstMatch.tap()
        let cinema = app.buttons["Cinema Trailers"]
        XCTAssertTrue(cinema.waitForExistence(timeout: 10))
        cinema.tap()
        let picker = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Trailers before movies'")).firstMatch
        XCTAssertTrue(picker.waitForExistence(timeout: 10))
        picker.tap()
        let want = was == 2 ? "3" : "2"
        app.buttons[want].firstMatch.tap()
        // A pre-roll picked by search.
        let search = app.textFields["Search for a video"]
        search.tap()
        search.typeText("Long Test")
        let result = app.buttons.matching(NSPredicate(format: "label BEGINSWITH '00 Long Test'")).firstMatch
        XCTAssertTrue(result.waitForExistence(timeout: 10))
        result.tap()
        XCTAssertTrue(app.buttons["Clear Pre-roll"].waitForExistence(timeout: 5))
        shot("cs1-cinema")
        app.buttons["Save"].tap()
        XCTAssertTrue(app.staticTexts["Saved."].waitForExistence(timeout: 10))
        let saved = try adminAPI("GET", "/settings")["cinema"] as? [String: Any] ?? [:]
        XCTAssertEqual(saved["trailers"] as? Int, Int(want))
        XCTAssertEqual(saved["prerollItemId"] as? Int, 322)
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
