import XCTest

extension XCUIApplication {
    /// One of the video player's overlay buttons (close, settings, Play On, Cast, Watch
    /// Together). They hide a few seconds into playback, like AVKit's controls, and leave the
    /// accessibility tree; a tap on the video brings them back.
    func playerControl(_ label: String) -> XCUIElement {
        let button = buttons[label]
        if !button.exists {
            // Above the middle of the video: clear of AVKit's play/pause and skip buttons.
            coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35)).tap()
            _ = button.waitForExistence(timeout: 3)
        }
        return button
    }

    func revealPlayerControls() { _ = playerControl("Close player") }

    func closePlayer() { playerControl("Close player").tap() }
}
