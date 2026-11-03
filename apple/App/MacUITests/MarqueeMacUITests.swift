import XCTest

/// A small, focused check of the Mac app's interactions: the output picker, the mini player, the
/// menus' shortcuts and context menus. Runs against scripts/e2e/with-server.sh with the app already
/// signed in or not; sound goes to whichever device MARQUEE_TEST_OUTPUT names, quietly, and Sonos
/// rooms are simulated in the app: nothing reaches a real speaker.
final class MarqueeMacUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments += ["-marquee-test-speakers", "YES", "-marquee-test-volume", "0.02",
                                "-marquee.mac.outputUID", ProcessInfo.processInfo.environment["MARQUEE_TEST_OUTPUT"] ?? "BlackHole2ch_UID"]
        app.launch()
        signInIfNeeded()
    }

    override func tearDown() {
        app.terminate()
    }

    private func signInIfNeeded() {
        if app.descendants(matching: .any)["nowPlayingBar"].waitForExistence(timeout: 8) { return }
        let address = app.textFields["Home address, e.g. 192.168.1.20:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.click()
        address.typeText("127.0.0.1:32597")
        app.buttons["Connect"].click()
        let other = app.buttons["Sign in with username and password"]
        XCTAssertTrue(other.waitForExistence(timeout: 15))
        other.click()
        let name = app.textFields["Username"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.click()
        name.typeText("e2eadmin")
        app.secureTextFields["Password"].click()
        app.secureTextFields["Password"].typeText("e2e-local-test-pass\r")
        XCTAssertTrue(app.descendants(matching: .any)["nowPlayingBar"].waitForExistence(timeout: 15))
    }

    private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }

    /// The first thing in the Music library, shuffled.
    private func startMusic() {
        let sidebar = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'sidebar.library-'"))
        XCTAssertTrue(sidebar.firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Music"].firstMatch.click()
        let shuffle = app.buttons["Shuffle All"].firstMatch
        XCTAssertTrue(shuffle.waitForExistence(timeout: 10))
        shuffle.click()
        XCTAssertTrue(element("bar.openNowPlaying").waitForExistence(timeout: 15))
    }

    /// Keys go to the frontmost app: never type unless that's this one, so someone else's work on
    /// the same Mac is left alone.
    private func type(_ key: String, _ flags: XCUIElement.KeyModifierFlags = []) throws {
        app.activate()
        _ = app.wait(for: .runningForeground, timeout: 3)
        guard app.state == .runningForeground else { throw XCTSkip("Marquee isn't frontmost; not typing into another app") }
        app.typeKey(key, modifierFlags: flags)
    }

    func testOutputPickerListsDevicesAndRoutesThisAppAlone() {
        startMusic()
        element("output.button").click()
        let row = element("output.device.BlackHole2ch_UID")
        XCTAssertTrue(row.waitForExistence(timeout: 5), "the picker lists real audio devices")
        XCTAssertTrue(element("output.default").exists)
        XCTAssertTrue(element("output.airplay").exists)
        XCTAssertTrue(element("output.sonos.SIM_ROOM").waitForExistence(timeout: 5), "the room is listed")
        XCTAssertTrue(element("output.sonosNote").exists)
        row.click()
        XCTAssertEqual(element("output.button").value as? String, "BlackHole 2ch")
    }

    func testPickingARoomAndComingBack() {
        startMusic()
        element("output.button").click()
        let room = element("output.sonos.SIM_ROOM")
        XCTAssertTrue(room.waitForExistence(timeout: 5))
        room.click()
        expectation(for: NSPredicate(format: "value == 'Test Room'"), evaluatedWith: element("output.button"))
        waitForExpectations(timeout: 5)
        // Picker open again: a Mac output brings it back here.
        if !element("output.default").exists { element("output.button").click() }
        element("output.device.BlackHole2ch_UID").click()
        expectation(for: NSPredicate(format: "value == 'BlackHole 2ch'"), evaluatedWith: element("output.button"))
        waitForExpectations(timeout: 5)
    }

    func testMiniPlayerByShortcutAndBack() throws {
        startMusic()
        try type("m", [.command, .shift])
        let expand = element("mini.expand")
        XCTAssertTrue(expand.waitForExistence(timeout: 5), "the mini bar replaces the window\n\(app.debugDescription)")
        XCTAssertTrue(element("bar.playPause").exists)
        expand.click()
        XCTAssertTrue(element("nowPlayingBar").waitForExistence(timeout: 5))
    }

    func testSpacePlaysAndPausesButTypesInSearch() throws {
        startMusic()
        let play = element("bar.playPause")
        let before = play.label
        try type(" ")
        expectation(for: NSPredicate(format: "label != %@", before), evaluatedWith: play)
        waitForExpectations(timeout: 5)
        let paused = play.label
        // Cmd+F goes to the search field; a space there is a space, and the music stays as it is.
        try type("f", .command)
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.click()
        field.typeText("a b")
        XCTAssertEqual(field.value as? String, "a b")
        XCTAssertEqual(play.label, paused)
    }

    func testRightClickingAPosterOffersItsMenu() throws {
        app.staticTexts["Music"].firstMatch.click()
        let album = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Floating'")).firstMatch
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        album.rightClick()
        XCTAssertTrue(app.menuItems["Add to Playlist…"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.menuItems["Start Radio"].exists)
        app.menuItems["Start Radio"].firstMatch.hover()
        try type(XCUIKeyboardKey.escape.rawValue)
    }

    func testMenusFollowWhatsPlaying() throws {
        startMusic()
        app.menuBars.menuBarItems["Controls"].click()
        XCTAssertTrue(app.menuItems["Pause"].waitForExistence(timeout: 3), "Controls says Pause while playing")
        XCTAssertTrue(app.menuItems["Next"].isEnabled)
        try type(XCUIKeyboardKey.escape.rawValue)
        app.menuBars.menuBarItems["Marquee"].click()
        XCTAssertTrue(app.menuItems["Administration…"].waitForExistence(timeout: 3), "admins have it in the menu")
        XCTAssertTrue(app.menuItems["Settings…"].exists)
        try type(XCUIKeyboardKey.escape.rawValue)
    }

    func testArrowKeysAndReturnInASongList() throws {
        app.staticTexts["Music"].firstMatch.click()
        let album = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Floating'")).firstMatch
        XCTAssertTrue(album.waitForExistence(timeout: 10))
        album.click()
        let row = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Floating 1'")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.click() // plays it, and the row takes the focus
        let bar = element("bar.openNowPlaying")
        XCTAssertTrue(bar.waitForExistence(timeout: 10))
        try type(XCUIKeyboardKey.downArrow.rawValue)
        try type(XCUIKeyboardKey.return.rawValue)
        let next = NSPredicate(format: "label CONTAINS 'Floating 2'")
        expectation(for: next, evaluatedWith: app.buttons["bar.openNowPlaying"].firstMatch)
        waitForExpectations(timeout: 10)
    }

    func testLyricsAndUpNextInNowPlaying() {
        startMusic()
        element("bar.openNowPlaying").click()
        let lyrics = app.radioButtons["Lyrics"].firstMatch
        XCTAssertTrue(lyrics.waitForExistence(timeout: 5))
        XCTAssertTrue(element("np.dislike").exists, "the thumbs down")
        lyrics.click()
        app.radioButtons["Up Next"].firstMatch.click()
        XCTAssertTrue(element("saveQueue").waitForExistence(timeout: 5))
        app.buttons["Done"].firstMatch.click()
    }

    func testAddToPlaylistFromTheBar() {
        startMusic()
        element("bar.addToPlaylist").click()
        XCTAssertTrue(app.textFields["Name"].firstMatch.waitForExistence(timeout: 5), app.debugDescription)
    }

    func testLiveTVWatchFullAndClose() {
        app.staticTexts["Live TV"].firstMatch.click()
        let full = app.buttons["Watch full screen"].firstMatch
        XCTAssertTrue(full.waitForExistence(timeout: 15))
        full.click()
        let close = app.buttons["Close Live TV"].firstMatch
        XCTAssertTrue(close.waitForExistence(timeout: 10))
        sleep(2)
        close.click()
        XCTAssertTrue(close.waitForNonExistence(timeout: 5))
    }

    func testVideoSwitchesToTheMiniWindowWithoutReloading() throws {
        app.staticTexts["Movies"].firstMatch.click()
        let movie = app.buttons.matching(NSPredicate(format: "label CONTAINS '00 Long Test'")).firstMatch
        XCTAssertTrue(movie.waitForExistence(timeout: 10), app.debugDescription)
        movie.click()
        let play = app.buttons.matching(NSPredicate(format: "(label BEGINSWITH 'Play' OR label BEGINSWITH 'Resume') AND NOT identifier BEGINSWITH 'bar.'")).firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10), app.debugDescription)
        play.click()
        sleep(3)
        app.windows.firstMatch.hover()
        XCTAssertTrue(app.buttons["Close player"].firstMatch.waitForExistence(timeout: 5), "the overlay comes up under the pointer")
        XCTAssertTrue(element("output.button").exists)
        XCTAssertTrue(app.buttons["Watch together"].firstMatch.exists)
        try type("m", [.command, .shift])
        XCTAssertTrue(element("miniVideo").waitForExistence(timeout: 5), "the small floating window shows the video")
        sleep(2)
        try type("m", [.command, .shift])
        XCTAssertTrue(element("video.player").waitForExistence(timeout: 5))
    }
}
