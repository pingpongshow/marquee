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

    /// Sonic Sage, Now Playing's source and lyrics (M6.5). Needs the test music library and
    /// the sonic analysis sidecar.
    func testMusicFeatures() {
        connectAndSignIn()
        openLibrary("Music")
        XCTAssertTrue(app.staticTexts["Sonic Sage"].waitForExistence(timeout: 10))
        shot("m1-discover")

        let prompt = app.textFields["Describe what you want to hear…"]
        prompt.tap()
        prompt.typeText("white noise and static hiss\n")
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20), "Sage should start a noise track")
        XCTAssertTrue(mini.label.contains("Hiss Theory") || mini.label.contains("Static Kids"), mini.label)
        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["white noise and static hiss"].exists)
        shot("m2-now-playing-sage")
        app.buttons["Up Next"].tap()
        shot("m3-up-next")
        app.buttons["Now Playing"].firstMatch.tap()
        app.swipeDown(velocity: .fast)

        // An album with an .lrc sidecar: synced lyrics.
        let artist = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
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
}
