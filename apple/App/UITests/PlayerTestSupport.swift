import XCTest

extension XCUIApplication {
    /// One of the video player's overlay buttons (close, settings, Play On, Cast, Watch
    /// Together). They hide a few seconds into playback, like AVKit's controls, and leave the
    /// accessibility tree; a tap on the video brings them back.
    func playerControl(_ label: String) -> XCUIElement {
        reveal(buttons[label])
    }

    /// A title on demand in Home's Your Watchlist row (USER-8), scrolled to: Home's rows load as
    /// they scroll in. Call it on Home.
    func vodWatchlistTile(_ title: String) -> XCUIElement {
        let tile = buttons.matching(identifier: "vod.suggestion").matching(NSPredicate(format: "label == %@", "\(title), VOD")).firstMatch
        let row = staticTexts["Your Watchlist"].firstMatch
        for _ in 0..<8 where !(row.exists && tile.exists) { swipeUp() }
        return tile
    }

    /// The video player (a film, an episode or VOD), there whether or not its controls show.
    var videoPlayer: XCUIElement {
        descendants(matching: .any).matching(NSPredicate(format: "identifier IN {'video.player', 'vod.player'}")).firstMatch
    }

    /// The player's close button: from iOS 26 AVKit's own (in its top bar), before that ours.
    /// AVKit's leaves the accessibility tree while its controls are hidden: wait for
    /// `videoPlayer` to know the player is up.
    var playerClose: XCUIElement {
        buttons.matching(NSPredicate(format: "label == 'Close player' OR identifier == 'Close Button'")).firstMatch
    }

    func revealPlayerControls() { _ = reveal(playerClose) }

    func closePlayer() { reveal(playerClose).tap() }

    private func reveal(_ button: XCUIElement) -> XCUIElement {
        // A tap shows the controls, or hides them if they were on their way in (after a
        // rotation, say): a second try then.
        for _ in 0..<2 where !(button.exists && button.isHittable) {
            // Above the middle of the video: clear of AVKit's play/pause and skip buttons.
            coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35)).tap()
            _ = button.waitForExistence(timeout: 3)
        }
        return button
    }

    /// After a username and password sign-in, iOS offers to save the password (a card over the
    /// app, a moment after Home appears); the next tap only dismisses it. Answer Not Now.
    func dismissSavePassword() {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let end = Date().addingTimeInterval(4)
        while Date() < end {
            for owner in [self, springboard] {
                let notNow = owner.buttons["Not Now"].firstMatch
                if notNow.exists, owner.staticTexts["Save Password?"].exists {
                    notNow.tap()
                    // Taps while it fades out are lost too.
                    _ = notNow.waitForNonExistence(timeout: 3)
                    Thread.sleep(forTimeInterval: 1)
                    return
                }
            }
            Thread.sleep(forTimeInterval: 0.25)
        }
    }
}
