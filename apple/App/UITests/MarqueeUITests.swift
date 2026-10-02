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

    func testBrowseAndPlay() {
        connectAndSignIn()
        // Movies library grid (iPhone: through the Libraries tab).
        app.tabBars.buttons["Libraries"].firstMatch.tap()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Movies'")).firstMatch.tap()
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
        app.tabBars.buttons["Libraries"].firstMatch.tap()
        let back = app.navigationBars.buttons["Libraries"]
        if back.waitForExistence(timeout: 2) { back.tap() }
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Music'")).firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Music"].waitForExistence(timeout: 10))
        app.scrollViews.buttons.firstMatch.tap()
        let playMusic = app.buttons["Play"].firstMatch
        XCTAssertTrue(playMusic.waitForExistence(timeout: 10))
        shot("07-artist")
        playMusic.tap()
        sleep(3)
        shot("08-mini-player")
        app.buttons["Pause"].firstMatch.tap()
        shot("09-paused")
    }
}
