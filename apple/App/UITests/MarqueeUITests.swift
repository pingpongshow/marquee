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
        app.buttons["Connect"].tap()
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
        let artist = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
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
    /// the sonic analysis sidecar.
    func testMusicFeatures() {
        connectAndSignIn()
        openLibrary("Music")
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

        // An album with an .lrc sidecar: synced lyrics.
        let artist = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
        // The Music page has grown (mixes, moods, styles): bring the card clear of the tab bar.
        for _ in 0..<4 where artist.frame.maxY > app.tabBars.firstMatch.frame.minY - 10 { app.swipeUp() }
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
        XCTAssertTrue(app.staticTexts["Test Saga"].exists, "the movie shows its collection")
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

    /// Your Stats (ADM-4) and Sonic Adventure from a track's menu (MUSIC-4).
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
        app.buttons["Live TV"].firstMatch.tap()
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
        XCTAssertLessThan(took, remaining - 2, "next track should take over ~4 s before the end (took \(took)s of \(remaining)s)")
        shot("cf1-after-crossfade")
        app.buttons["Crossfade 4 seconds"].tap()
        app.buttons["Off"].tap()
    }

    func testStatsAndAdventure() {
        connectAndSignIn()
        app.buttons["Settings"].firstMatch.tap()
        let stats = app.buttons["Your Stats"]
        XCTAssertTrue(stats.waitForExistence(timeout: 10))
        stats.tap()
        XCTAssertTrue(app.staticTexts["Plays"].waitForExistence(timeout: 10) || app.staticTexts["Nothing played in this period."].exists)
        app.buttons["All time"].tap()
        XCTAssertTrue(app.staticTexts["Top artists"].waitForExistence(timeout: 10))
        shot("s1-stats")

        // A track's menu → Sonic Adventure → pick a destination.
        openLibrary("Music")
        let artist = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
        artist.tap()
        let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        album.tap()
        let track = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Floating 2'")).firstMatch
        XCTAssertTrue(track.waitForExistence(timeout: 10))
        track.press(forDuration: 1.2)
        app.buttons["Sonic Adventure…"].tap()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        search.tap()
        search.typeText("Thump")
        let dest = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Thump'")).firstMatch
        XCTAssertTrue(dest.waitForExistence(timeout: 10))
        shot("s2-adventure")
        dest.tap()
        XCTAssertTrue(app.buttons["miniPlayer"].waitForExistence(timeout: 15))
        app.buttons["miniPlayer"].tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        shot("s3-adventure-playing")
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
        app.buttons["Live TV"].firstMatch.tap()
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
}
