import XCTest

/// Fix Match file names, comparing duplicate files and deleting one to the trash (ADM-11), and
/// the renamed music features (Sound Journey, DJ modes; MUSIC-4, MUSIC-6), against the local
/// test server. Each test uses a temporary account and puts back what it changes, since other
/// clients' tests share the server.
final class Batch3UITests: XCTestCase {
    private var app: XCUIApplication!
    private var server: String { ProcessInfo.processInfo.environment["MARQUEE_TEST_SERVER"] ?? "localhost:32597" }
    private var adminToken: String? { ProcessInfo.processInfo.environment["MARQUEE_TEST_ADMIN_TOKEN"].flatMap { $0.isEmpty ? nil : $0 } }

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

    // MARK: - Server helpers

    /// Calls the server as the admin and returns the JSON.
    @discardableResult
    private func api(_ method: String, _ path: String, _ body: [String: Any]? = nil, token: String? = nil) throws -> Any? {
        guard let admin = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        var req = URLRequest(url: URL(string: "http://\(server)/api/v1\(path)")!)
        req.httpMethod = method
        req.timeoutInterval = 70
        req.setValue("Bearer \(token ?? admin)", forHTTPHeaderField: "Authorization")
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
        wait(for: [done], timeout: 80)
        return out
    }

    private func object(_ method: String, _ path: String, _ body: [String: Any]? = nil) throws -> [String: Any] {
        try api(method, path, body) as? [String: Any] ?? [:]
    }

    /// A request without an expectation, for teardown blocks (which run even when a failure
    /// stops the test).
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

    /// A temporary account, deleted when the test ends.
    private func temporaryUser(admin: Bool = false) throws -> (name: String, password: String) {
        let name = "ui3\(admin ? "admin" : "user")-\(Int.random(in: 1000...9999))"
        let pass = "correct horse battery"
        let user = try object("POST", "/users", ["username": name, "displayName": name, "password": pass, "isAdmin": admin])
        let id = try XCTUnwrap(user["id"] as? Int)
        let url = "http://\(server)/api/v1/users/\(id)", token = adminToken
        addTeardownBlock { Self.send("DELETE", url, token: token) }
        return (name, pass)
    }

    /// Signs in over the API as another device of the same person; returns its token.
    private func deviceToken(_ name: String, _ pass: String) throws -> String {
        let r = try object("POST", "/auth/login", ["username": name, "password": pass,
                                                   "device": ["clientId": "uitest-\(UUID().uuidString)", "name": "UITest Check", "platform": "web"]])
        return try XCTUnwrap(r["token"] as? String)
    }

    /// Waits for a condition checked against the server.
    private func eventually(_ timeout: TimeInterval, _ what: @autoclosure () -> String, _ check: () throws -> Bool) rethrows {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if try check() { return }
            Thread.sleep(forTimeInterval: 1)
        }
        XCTFail("timed out: \(what())")
    }

    // MARK: - App helpers

    private func connect() {
        let address = app.textFields["Home address, e.g. 10.1.1.10:32500"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.tap()
        address.typeText(server)
        app.buttons["Connect"].tap()
    }

    private func signIn(username: String, password: String) {
        connect()
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

    private func openSettings() {
        app.buttons["Settings"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 10))
    }

    private func scrollTo(_ e: XCUIElement, max: Int = 8) {
        for _ in 0..<max where !(e.exists && e.isHittable) { app.swipeUp() }
    }

    private func back() { app.navigationBars.buttons.firstMatch.tap() }

    /// An element by accessibility identifier (paths are longer than a plain query allows).
    private func byID(_ id: String, _ type: XCUIElement.ElementType = .any) -> XCUIElement {
        app.descendants(matching: type).matching(NSPredicate(format: "identifier == %@", id)).firstMatch
    }

    /// Settings → Library Health → a check.
    private func openCheck(_ id: String) {
        let health = app.buttons["Library Health"]
        scrollTo(health)
        health.tap()
        let check = app.descendants(matching: .any)["check-\(id)"].firstMatch
        XCTAssertTrue(check.waitForExistence(timeout: 15))
        check.tap()
    }

    /// iPhone: Libraries tab → library. iPad: the sidebar.
    private func openLibrary(_ name: String) {
        let librariesTab = app.buttons["Libraries"].firstMatch
        if UIDevice.current.userInterfaceIdiom == .phone, librariesTab.exists {
            librariesTab.tap()
            let back = app.navigationBars.buttons["Libraries"]
            if back.waitForExistence(timeout: 2) { back.tap() }
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch.tap()
        } else {
            let item = app.cells[name].firstMatch
            if !item.exists { app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'sidebar'")).firstMatch.tap() }
            XCTAssertTrue(item.waitForExistence(timeout: 5))
            item.tap()
        }
    }

    // MARK: - Fix Match shows the file name

    func testFixMatchShowsFileName() throws {
        let detail = try object("GET", "/items/5")
        let versions = detail["versions"] as? [[String: Any]] ?? []
        let path = try XCTUnwrap((versions.first?["files"] as? [[String: Any]])?.first?["path"] as? String, "admins get file paths")
        let base = (path as NSString).lastPathComponent

        let admin = try temporaryUser(admin: true)
        signIn(username: admin.name, password: admin.password)
        openSettings()
        openCheck("missingSubtitles")
        let actions = app.buttons["Actions for 15 Thunder"]
        XCTAssertTrue(actions.waitForExistence(timeout: 15), "15 Thunder is missing subtitles")
        actions.tap()
        app.buttons["Fix Match…"].tap()
        XCTAssertTrue(app.navigationBars["Fix Match"].waitForExistence(timeout: 10))
        let file = byID("file-\(path)")
        XCTAssertTrue(file.waitForExistence(timeout: 10), "the sheet shows the file")
        XCTAssertTrue(file.label.contains(base), file.label)
        XCTAssertTrue(file.label.contains((path as NSString).deletingLastPathComponent.split(separator: "/").last.map(String.init) ?? ""), "and its folder")
        XCTAssertTrue(app.staticTexts["File"].exists || app.staticTexts["FILE"].exists)
        shot("fm1-file-name")
        app.buttons["Cancel"].tap()
    }

    // MARK: - Duplicates: compare and delete to trash

    func testDuplicateCompareAndDelete() throws {
        guard let token = adminToken else { throw XCTSkip("MARQUEE_TEST_ADMIN_TOKEN not set") }
        let libraries = try api("GET", "/libraries") as? [[String: Any]] ?? []
        let movies = try XCTUnwrap(libraries.first { $0["name"] as? String == "Movies" })
        let libraryID = try XCTUnwrap(movies["id"] as? Int)
        let root = try XCTUnwrap((movies["paths"] as? [String])?.first)
        let fm = FileManager.default
        let original = "\(root)/36 Valley (1962)"
        let copy = "\(root)/36 Valley (apple copy)"
        let copyFile = "\(copy)/36 Valley (1962).mp4"
        try XCTSkipUnless(fm.fileExists(atPath: original), "the test library has 36 Valley")
        let allowedBefore = (try object("GET", "/settings")["library"] as? [String: Any])?["allowMediaDeletion"] as? Bool ?? false

        // Put everything back however the test ends: the setting, the copy, its trash, a rescan.
        let base = "http://\(server)/api/v1"
        addTeardownBlock {
            Self.send("PATCH", "\(base)/settings", token: token, ["library": ["allowMediaDeletion": allowedBefore]])
            try? fm.removeItem(atPath: copy)
            let trash = "\(root)/.marquee-trash"
            for day in (try? fm.contentsOfDirectory(atPath: trash)) ?? [] {
                let dayDir = "\(trash)/\(day)"
                try? fm.removeItem(atPath: "\(dayDir)/36 Valley (apple copy)")
                if (try? fm.contentsOfDirectory(atPath: dayDir))?.isEmpty == true { try? fm.removeItem(atPath: dayDir) }
            }
            if (try? fm.contentsOfDirectory(atPath: trash))?.isEmpty == true { try? fm.removeItem(atPath: trash) }
            Self.send("POST", "\(base)/libraries/\(libraryID)/scan", token: token)
        }
        try? fm.removeItem(atPath: copy)
        try fm.copyItem(atPath: original, toPath: copy)
        try api("PATCH", "/settings", ["library": ["allowMediaDeletion": false]])
        try api("POST", "/libraries/\(libraryID)/scan")

        // The server reports both files as duplicates.
        var issueItemID = 0
        try eventually(60, "the copy shows up as a duplicate") {
            let page = try object("GET", "/library-health/duplicates")
            for issue in page["items"] as? [[String: Any]] ?? [] {
                let paths = (issue["files"] as? [[String: Any]] ?? []).compactMap { ($0["file"] as? [String: Any])?["path"] as? String }
                if paths.contains(copyFile) && paths.contains("\(original)/36 Valley (1962).mp4") {
                    issueItemID = (issue["item"] as? [String: Any])?["id"] as? Int ?? 0
                    return true
                }
            }
            return false
        }

        let admin = try temporaryUser(admin: true)
        signIn(username: admin.name, password: admin.password)
        openSettings()
        openCheck("duplicates")
        let compare = app.buttons["compare-\(issueItemID)"]
        XCTAssertTrue(compare.waitForExistence(timeout: 15))
        compare.tap()
        XCTAssertTrue(app.navigationBars["Compare Files"].waitForExistence(timeout: 10))
        XCTAssertTrue(byID("file-\(copyFile)").waitForExistence(timeout: 5))
        XCTAssertTrue(byID("file-\(original)/36 Valley (1962).mp4").exists)
        for row in ["Size", "Resolution", "Video", "HDR", "Bitrate", "Audio", "Subtitles", "Duration", "Container", "Added"] {
            XCTAssertTrue(app.staticTexts[row].exists, "\(row) row")
        }
        // Deleting is off: a note, and no Delete buttons.
        XCTAssertTrue(app.descendants(matching: .any)["deletionOffNote"].waitForExistence(timeout: 10))
        XCTAssertFalse(byID("delete-\(copyFile)", .button).exists)
        shot("dup1-compare")

        // Turn it on in Settings → Library Settings.
        back(); back(); back()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 10))
        let libSettings = app.buttons["Library Settings"]
        scrollTo(libSettings)
        libSettings.tap()
        let toggle = app.switches["allowMediaDeletion"]
        XCTAssertTrue(toggle.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "isEnabled == true"), evaluatedWith: toggle)
        waitForExpectations(timeout: 10)
        XCTAssertEqual(toggle.value as? String, "0")
        toggle.switches.firstMatch.exists ? toggle.switches.firstMatch.tap() : toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.5)).tap()
        try eventually(10, "the setting is saved") {
            (try object("GET", "/settings")["library"] as? [String: Any])?["allowMediaDeletion"] as? Bool == true
        }
        shot("dup2-setting")
        back()

        // Delete the copy.
        openCheck("duplicates")
        XCTAssertTrue(compare.waitForExistence(timeout: 15))
        compare.tap()
        let delete = byID("delete-\(copyFile)", .button)
        XCTAssertTrue(delete.waitForExistence(timeout: 10))
        XCTAssertTrue(delete.isEnabled)
        XCTAssertFalse(app.descendants(matching: .any)["deletionOffNote"].exists)
        delete.tap()
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        XCTAssertTrue(alert.label.contains("36 Valley (1962).mp4"), alert.label)
        XCTAssertTrue(alert.staticTexts.matching(NSPredicate(format: "label CONTAINS '.marquee-trash' AND label CONTAINS '30 days'")).firstMatch.exists)
        shot("dup3-confirm")
        alert.buttons["Delete"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Moved'")).firstMatch.waitForExistence(timeout: 15))
        XCTAssertFalse(delete.exists, "the deleted file leaves the comparison")
        // The last copy can't be deleted from here.
        let last = byID("delete-\(original)/36 Valley (1962).mp4", .button)
        XCTAssertTrue(last.exists)
        XCTAssertFalse(last.isEnabled)
        shot("dup4-deleted")

        // The file is in the library's trash, and the duplicate is gone.
        XCTAssertFalse(fm.fileExists(atPath: copyFile))
        let trashed = ((try? fm.contentsOfDirectory(atPath: "\(root)/.marquee-trash")) ?? [])
            .map { "\(root)/.marquee-trash/\($0)/36 Valley (apple copy)/36 Valley (1962).mp4" }
        XCTAssertTrue(trashed.contains { fm.fileExists(atPath: $0) }, "moved to .marquee-trash")
        XCTAssertTrue(fm.fileExists(atPath: "\(original)/36 Valley (1962).mp4"), "the original stays")
        try eventually(30, "the duplicate is resolved") {
            let page = try object("GET", "/library-health/duplicates")
            return !(page["items"] as? [[String: Any]] ?? []).contains { issue in
                (issue["files"] as? [[String: Any]] ?? []).contains { ($0["file"] as? [String: Any])?["path"] as? String == copyFile }
            }
        }
    }

    // MARK: - Music renames (MUSIC-4, MUSIC-6)

    /// The DJ menu offers the renamed modes, and the server takes each of them.
    func testDJModes() throws {
        // The server accepts every new mode name.
        let album = try XCTUnwrap((try object("GET", "/search?q=Floating%201&limit=5")["groups"] as? [[String: Any]])?
            .first { $0["type"] as? String == "track" }?["items"] as? [[String: Any]])
        let trackID = try XCTUnwrap(album.first?["id"] as? Int)
        for mode in ["wander", "superfan", "deep_cuts", "same_era"] {
            let r = try object("POST", "/music/dj", ["trackId": trackID, "mode": mode])
            XCTAssertNotEqual(r["code"] as? String, "bad_request", "\(mode): \(r)")
        }

        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        openLibrary("Music")
        let shortcut = app.buttons["musicShowLibrary"]
        XCTAssertTrue(shortcut.waitForExistence(timeout: 15))
        shortcut.tap()
        let artists = app.buttons["musicLibrary.artists"]
        XCTAssertTrue(artists.waitForExistence(timeout: 10))
        for _ in 0..<6 where !artists.isHittable || artists.frame.maxY > app.tabBars.firstMatch.frame.minY - 10 { app.swipeUp() }
        artists.tap()
        let artist = app.scrollViews.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Calm Pads'")).firstMatch
        for _ in 0..<5 where !artist.waitForExistence(timeout: 2) { app.swipeUp() }
        XCTAssertTrue(artist.waitForExistence(timeout: 10))
        for _ in 0..<4 where artist.frame.maxY > app.tabBars.firstMatch.frame.minY - 10 { app.swipeUp() }
        artist.tap()
        let floating = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Floating'")).firstMatch
        XCTAssertTrue(floating.waitForExistence(timeout: 10))
        floating.tap()
        XCTAssertTrue(app.buttons["Radio"].waitForExistence(timeout: 10))
        app.buttons["Play"].firstMatch.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15))
        mini.tap()
        XCTAssertTrue(app.staticTexts["PLAYING FROM"].waitForExistence(timeout: 5))

        let dj = app.buttons["DJ: off"]
        XCTAssertTrue(dj.waitForExistence(timeout: 5))
        dj.tap()
        for mode in ["Wander", "Superfan", "Deep Cuts", "Same Era"] {
            XCTAssertTrue(app.buttons[mode].waitForExistence(timeout: 5), mode)
        }
        XCTAssertFalse(app.buttons["Stretch"].exists || app.buttons["DJ Stretch"].exists)
        shot("dj1-modes")
        app.buttons["Same Era"].tap()
        XCTAssertTrue(app.buttons["DJ: Same Era"].waitForExistence(timeout: 5))
        app.buttons["DJ: Same Era"].tap()
        app.buttons["Wander"].tap()
        XCTAssertTrue(app.buttons["DJ: Wander"].waitForExistence(timeout: 5))
        shot("dj2-wander")
        app.buttons["DJ: Wander"].tap()
        app.buttons["Off"].tap()
        XCTAssertTrue(app.buttons["DJ: off"].waitForExistence(timeout: 5))
        app.buttons["Pause"].firstMatch.tap()
    }
    // MARK: - Artwork and the Now Playing layout

    /// The album with very long names and real cover art, made by
    /// scripts/make-ui-test-fixtures.sh.
    private func openLongNamesAlbum() throws {
        let found = try object("GET", "/search?q=Corcovado&limit=5")["groups"] as? [[String: Any]] ?? []
        let track = found.first { $0["type"] as? String == "track" }.flatMap { ($0["items"] as? [[String: Any]])?.first }
        try XCTSkipIf(track == nil || (track?["images"] as? [String: Any])?["poster"] == nil,
                      "run scripts/make-ui-test-fixtures.sh against the test server first")
        app.buttons["Search"].firstMatch.tap()
        let field = app.searchFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10))
        field.tap()
        field.typeText("Getz")
        let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Getz/Gilberto'")).firstMatch
        XCTAssertTrue(album.waitForExistence(timeout: 15))
        album.tap()
    }

    /// Posters and covers load (they once stayed on the titled placeholder), on the item page
    /// and in Now Playing.
    func testArtworkLoads() throws {
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)
        try openLongNamesAlbum()
        let art = app.images["artwork.loaded"].firstMatch
        XCTAssertTrue(art.waitForExistence(timeout: 15), "the album cover loads")
        XCTAssertGreaterThan(art.frame.width, 50)
        shot("art1-album")
        app.buttons["Play"].firstMatch.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15))
        mini.tap()
        XCTAssertTrue(app.staticTexts["nowPlayingTitle"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.images.matching(identifier: "artwork.loaded").allElementsBoundByIndex.contains { $0.frame.width > 150 },
                      "Now Playing shows the cover")
        shot("art2-now-playing")
        app.buttons["np.playPause"].tap()
    }

    /// Every Now Playing control fits on screen with a very long title and artist, in portrait
    /// and landscape.
    func testNowPlayingLayoutLongNames() throws {
        let user = try temporaryUser()
        addTeardownBlock { XCUIDevice.shared.orientation = .portrait }
        signIn(username: user.name, password: user.password)
        try openLongNamesAlbum()
        let play = app.buttons["Play"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15))
        mini.tap()
        let title = app.staticTexts["nowPlayingTitle"]
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        XCTAssertTrue(title.label.hasPrefix("Corcovado"), title.label)

        for orientation in [UIDeviceOrientation.portrait, .landscapeLeft] {
            XCUIDevice.shared.orientation = orientation
            // Wait for the rotation to finish.
            let wantWide = orientation.isLandscape
            for _ in 0..<20 {
                let f = app.windows.firstMatch.frame
                if (f.width > f.height) == wantWide { break }
                usleep(250_000)
            }
            sleep(2)
            let name = orientation == .portrait ? "portrait" : "landscape"
            let window = app.windows.firstMatch.frame
            shot("np-\(name)-before")
            let controls: [XCUIElement] = [
                app.buttons["np.playPause"], app.buttons["np.shuffle"], app.buttons["np.repeat"],
                app.buttons["np.previous"], app.buttons["np.next"],
                app.buttons["1 star"], app.buttons["5 stars"],
                app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Sleep'")).firstMatch,
                app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'DJ:'")).firstMatch,
                app.segmentedControls["nowPlayingPanel"],
                app.staticTexts["nowPlayingTitle"], app.staticTexts["nowPlayingSubtitle"],
            ]
            for c in controls {
                XCTAssertTrue(c.waitForExistence(timeout: 5), "\(name): \(c)")
                XCTAssertTrue(window.contains(c.frame), "\(name): \(c.identifier.isEmpty ? c.label : c.identifier) \(c.frame) is outside \(window)")
                // SwiftUI menus report not hittable while on screen; they're checked by opening
                // them below.
                if c.elementType != .staticText, !c.label.hasPrefix("Sleep"), !c.label.hasPrefix("DJ:") {
                    XCTAssertTrue(c.isHittable, "\(name): \(c.label) can be tapped: \(c.debugDescription)")
                }
            }
            // The menus open and work: set a sleep timer and a DJ, then turn both off.
            func menu(_ prefix: String) -> XCUIElement {
                app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
            }
            func open(_ e: XCUIElement) { e.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap() }
            open(menu("Sleep"))
            XCTAssertTrue(app.buttons["End of track"].waitForExistence(timeout: 5), "\(name): the sleep menu opens")
            app.buttons["End of track"].tap()
            XCTAssertTrue(menu("Stops after this track").waitForExistence(timeout: 5))
            open(menu("Stops after this track"))
            app.buttons["Turn off"].tap()
            XCTAssertTrue(menu("Sleep").waitForExistence(timeout: 5))
            open(menu("DJ:"))
            XCTAssertTrue(app.buttons["Same Era"].waitForExistence(timeout: 5), "\(name): the DJ menu opens")
            app.buttons["Same Era"].tap()
            XCTAssertTrue(app.buttons["DJ: Same Era"].waitForExistence(timeout: 5))
            open(app.buttons["DJ: Same Era"])
            app.buttons["Off"].tap()
            XCTAssertTrue(app.buttons["DJ: off"].waitForExistence(timeout: 5))
            let slider = app.sliders.firstMatch
            XCTAssertTrue(window.contains(slider.frame), "\(name): seek bar \(slider.frame)")
            shot("np-\(name)")
        }
        XCUIDevice.shared.orientation = .portrait
        sleep(2)
        app.buttons["np.playPause"].tap()
    }
    /// Now Playing's header: no car mode or equaliser buttons; Play On, AirPlay and Cast are
    /// the same size and overlap neither each other nor the (long, truncated) source title,
    /// in portrait and landscape.
    func testNowPlayingHeaderTidy() throws {
        let user = try temporaryUser()
        addTeardownBlock { XCUIDevice.shared.orientation = .portrait }
        signIn(username: user.name, password: user.password)
        try openLongNamesAlbum()
        let play = app.buttons["Play"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        play.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15))
        mini.tap()
        let source = app.otherElements["nowPlayingSource"]
        XCTAssertTrue(source.waitForExistence(timeout: 10))
        for orientation in [UIDeviceOrientation.portrait, .landscapeLeft] {
            XCUIDevice.shared.orientation = orientation
            let wantWide = orientation.isLandscape
            for _ in 0..<20 {
                let f = app.windows.firstMatch.frame
                if (f.width > f.height) == wantWide { break }
                usleep(250_000)
            }
            sleep(2)
            let name = orientation == .portrait ? "portrait" : "landscape"
            shot("nph-\(name)")
            XCTAssertFalse(app.buttons["Car Mode"].exists, "\(name): no car mode button")
            XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Equali'")).firstMatch.exists, "\(name): no EQ button")
            let window = app.windows.firstMatch.frame
            var buttons = [app.buttons["Play on…"].firstMatch, app.buttons["AirPlay"].firstMatch]
            let cast = app.descendants(matching: .any)["castButton"].firstMatch
            if cast.exists, !cast.frame.isEmpty { buttons.append(cast) }
            for b in buttons { XCTAssertTrue(b.waitForExistence(timeout: 5), "\(name): \(b)") }
            // The title's texts, and the area they sit in.
            let texts = source.staticTexts.allElementsBoundByIndex.map(\.frame) + [source.frame]
            XCTAssertTrue(source.staticTexts["PLAYING FROM"].exists, "\(name): the source title shows")
            for (i, b) in buttons.enumerated() {
                let f = b.frame
                XCTAssertTrue(window.contains(f), "\(name): \(b.label) \(f) on screen")
                // Same size as the first (AirPlay's picker draws its own button inside).
                XCTAssertEqual(f.width, buttons[0].frame.width, accuracy: 1.5, "\(name): \(b.label) is the same size")
                XCTAssertEqual(f.height, buttons[0].frame.height, accuracy: 1.5, "\(name): \(b.label) is the same size")
                for t in texts { XCTAssertFalse(f.intersects(t), "\(name): \(b.label) \(f) overlaps the title \(t)") }
                for other in buttons[(i + 1)...] {
                    XCTAssertFalse(f.intersects(other.frame), "\(name): \(b.label) overlaps \(other.label)")
                }
            }
            // Centred: the title's middle is the header's middle.
            let header = buttons.map(\.frame).reduce(source.frame) { $0.union($1) }
            XCTAssertEqual(source.frame.midX, header.midX, accuracy: 3, "\(name): the title is centred")
        }
        XCUIDevice.shared.orientation = .portrait
        sleep(2)
        app.buttons["np.playPause"].tap()
    }

    // MARK: - Save a Muse mix as a playlist

    /// Start a Muse mix, save it as a playlist (title from the prompt), open it; Now Playing's
    /// menu and Up Next offer the same.
    func testSaveMuseAsPlaylist() throws {
        let user = try temporaryUser()
        let token = try deviceToken(user.name, user.password)
        let base = "http://\(server)/api/v1"
        addTeardownBlock {
            // The playlists go with the account too; delete them explicitly anyway.
            var req = URLRequest(url: URL(string: "\(base)/playlists")!)
            req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
            let done = DispatchSemaphore(value: 0)
            var ids: [Int] = []
            URLSession.shared.dataTask(with: req) { data, _, _ in
                let list = data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [[String: Any]] ?? []
                ids = list.compactMap { $0["id"] as? Int }
                done.signal()
            }.resume()
            _ = done.wait(timeout: .now() + 15)
            for id in ids { Self.send("DELETE", "\(base)/playlists/\(id)", token: token) }
        }
        signIn(username: user.name, password: user.password)
        openLibrary("Music")
        let muse = app.buttons["musicMuse"]
        XCTAssertTrue(muse.waitForExistence(timeout: 15))
        muse.tap()
        let prompt = app.textFields["Describe what you want to hear…"]
        XCTAssertTrue(prompt.waitForExistence(timeout: 10))
        prompt.tap()
        prompt.typeText("white noise and static hiss\n")
        XCTAssertTrue(app.buttons["miniPlayer"].waitForExistence(timeout: 20), "Muse starts")
        let save = app.buttons["saveMuse"]
        XCTAssertTrue(save.waitForExistence(timeout: 10))
        save.tap()
        let alert = app.alerts["Save as Playlist"]
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        let field = alert.textFields.firstMatch
        XCTAssertEqual(field.value as? String, "white noise and static hiss", "titled after the prompt")
        // The title can be changed: replace it.
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.97, dy: 0.5)).tap()
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 40) + "white noise and static hiss (saved)")
        shot("pl1-title")
        alert.buttons["Save"].tap()
        let saved = app.alerts["Saved “white noise and static hiss (saved)”"]
        XCTAssertTrue(saved.waitForExistence(timeout: 10))

        // On the server: an audio playlist with the mix's tracks in order.
        let playlists = try api("GET", "/playlists", token: token) as? [[String: Any]] ?? []
        let p = try XCTUnwrap(playlists.first { $0["title"] as? String == "white noise and static hiss (saved)" })
        XCTAssertEqual(p["kind"] as? String, "audio")
        let id = try XCTUnwrap(p["id"] as? Int)
        let count = p["itemCount"] as? Int ?? 0
        XCTAssertGreaterThan(count, 0, "the playlist has the mix's tracks")
        let items = try api("GET", "/playlists/\(id)/items", token: token)
        let entries = (items as? [[String: Any]]) ?? ((items as? [String: Any])?["items"] as? [[String: Any]]) ?? []
        XCTAssertEqual(entries.count, count)
        let ids = entries.compactMap { ($0["item"] as? [String: Any])?["id"] as? Int ?? $0["id"] as? Int }
        XCTAssertEqual(Set(ids).count, ids.count, "each track once")

        saved.buttons["Open"].tap()
        XCTAssertTrue(app.navigationBars["white noise and static hiss (saved)"].waitForExistence(timeout: 10)
                      || app.staticTexts["white noise and static hiss (saved)"].waitForExistence(timeout: 5))
        shot("pl2-opened")
        app.buttons["Close"].firstMatch.tap()

        // Now Playing's "More" menu and Up Next offer it for the queue.
        app.buttons["miniPlayer"].tap()
        let more = app.buttons["np.more"]
        XCTAssertTrue(more.waitForExistence(timeout: 10))
        more.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        let fromMenu = app.buttons["Save as Playlist…"]
        XCTAssertTrue(fromMenu.waitForExistence(timeout: 5))
        fromMenu.tap()
        let again = app.alerts["Save as Playlist"]
        XCTAssertTrue(again.waitForExistence(timeout: 5))
        XCTAssertEqual(again.textFields.firstMatch.value as? String, "white noise and static hiss")
        again.buttons["Cancel"].tap()
        app.buttons["Up Next"].tap()
        XCTAssertTrue(app.buttons["saveQueue"].waitForExistence(timeout: 5))
        shot("pl3-up-next")
        app.buttons["Now Playing"].firstMatch.tap()
        app.buttons["np.playPause"].tap()
    }
    // MARK: - Audio quality (MUSIC-23)

    /// With "Show audio quality" on, Now Playing shows the format (a 24-bit/96 kHz FLAC is
    /// Hi-Res) and the album's tracks show theirs; off, both are gone.
    func testAudioQualityBadge() throws {
        let found = try object("GET", "/search?q=Hi-Res%20Tone&limit=5")["groups"] as? [[String: Any]] ?? []
        let tone = found.first { $0["type"] as? String == "track" }.flatMap { ($0["items"] as? [[String: Any]])?.first }
        try XCTSkipIf((tone?["audioFormat"] as? [String: Any])?["codec"] as? String != "flac",
                      "needs the 24-bit/96 kHz FLAC in Test Artist 1/Album 1 (2001)")
        let user = try temporaryUser()
        signIn(username: user.name, password: user.password)

        func setQuality(_ on: Bool) {
            openSettings()
            let toggle = app.switches["showAudioQuality"]
            scrollTo(toggle)
            XCTAssertTrue(toggle.waitForExistence(timeout: 5))
            if (toggle.value as? String == "1") != on {
                toggle.switches.firstMatch.exists ? toggle.switches.firstMatch.tap() : toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.5)).tap()
            }
            XCTAssertEqual(toggle.value as? String, on ? "1" : "0")
        }
        func openAlbum() {
            app.buttons["Search"].firstMatch.tap()
            // The Search tab keeps the album open from before.
            if app.navigationBars["Album 1 (2001)"].waitForExistence(timeout: 3) { return }
            let field = app.searchFields.firstMatch
            XCTAssertTrue(field.waitForExistence(timeout: 10))
            field.tap()
            if let old = field.value as? String, !old.isEmpty, old != field.placeholderValue {
                field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: old.count))
            }
            field.typeText("Album 1")
            // The untagged FLAC sits in its own album, named after its folder.
            let album = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Album 1 (2001),'")).firstMatch
            XCTAssertTrue(album.waitForExistence(timeout: 15))
            album.tap()
        }
        let badge = app.descendants(matching: .any)["audioQuality"].firstMatch

        setQuality(true)
        openAlbum()
        let track = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Hi-Res Tone'")).firstMatch
        XCTAssertTrue(track.waitForExistence(timeout: 10))
        XCTAssertTrue(track.label.contains("FLAC 24/96"), track.label)
        let albumQuality = app.descendants(matching: .any)["albumQuality"].firstMatch
        XCTAssertTrue(albumQuality.exists, "all the album's tracks share a format")
        XCTAssertEqual(albumQuality.label, "FLAC · 24-bit/96 kHz")
        shot("aq1-tracks")
        track.tap()
        let mini = app.buttons["miniPlayer"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15))
        mini.tap()
        XCTAssertTrue(badge.waitForExistence(timeout: 10))
        XCTAssertTrue(badge.label.contains("FLAC · 24-bit/96 kHz"), badge.label)
        XCTAssertTrue(badge.label.contains("Hi-Res"), badge.label)
        XCTAssertTrue(app.windows.firstMatch.frame.contains(badge.frame), "fits on screen")
        shot("aq2-now-playing")
        app.buttons["np.playPause"].tap()
        app.swipeDown(velocity: .fast)

        // Off: no badge, no track labels.
        setQuality(false)
        openAlbum()
        XCTAssertTrue(track.waitForExistence(timeout: 10))
        XCTAssertFalse(track.label.contains("FLAC"), track.label)
        XCTAssertFalse(app.descendants(matching: .any)["albumQuality"].exists)
        mini.tap()
        XCTAssertTrue(app.staticTexts["nowPlayingTitle"].waitForExistence(timeout: 10))
        XCTAssertFalse(badge.exists)
        shot("aq3-off")
    }
}
