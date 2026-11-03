import AVKit
import MarqueeKit
import SwiftUI

/// The windows: the main one, the mini bar (music) and the mini video window. Switching to mini
/// hides the main window rather than closing it, so what's on screen in it, a video included,
/// stays exactly as it was.
@MainActor @Observable
final class MacWindows: NSObject {
    static let shared = MacWindows()
    static let mainID = "main"
    static let minimumSize = CGSize(width: 900, height: 600)

    private(set) var isMini = false
    /// The main window is on screen (not closed, not swapped for a mini player).
    private(set) var mainVisible = false

    /// The mini player stays above other windows (View menu).
    var miniOnTop = UserDefaults.standard.object(forKey: "marquee.mac.miniOnTop") as? Bool ?? true {
        didSet {
            UserDefaults.standard.set(miniOnTop, forKey: "marquee.mac.miniOnTop")
            for panel in [musicPanel, videoPanel] { panel?.level = miniOnTop ? .floating : .normal }
        }
    }

    /// Opens the main window after it's been closed.
    @ObservationIgnored var openMain: (() -> Void)?
    @ObservationIgnored private weak var mainWindow: NSWindow?
    /// The red button or Cmd+W; SwiftUI has to be asked to open it again.
    @ObservationIgnored private var mainClosed = false
    @ObservationIgnored private var musicPanel: NSPanel?
    @ObservationIgnored private var videoPanel: NSPanel?
    @ObservationIgnored private var closingPanelsOurselves = false

    /// While music plays and the main window isn't up.
    var showsMenuBarIcon: Bool { MacPlayback.shared.music.current != nil && !mainVisible }

    func appLaunched() {
        NSWindow.allowsAutomaticWindowTabbing = false
        installSpaceKey()
        MacPlayback.shared.startTestDriver()
    }

    /// The main window, each time SwiftUI makes one.
    func adopt(_ window: NSWindow) {
        guard window !== mainWindow else { return }
        mainWindow = window
        mainVisible = !isMini
        window.tabbingMode = .disallowed
        window.collectionBehavior.insert(.fullScreenPrimary)
        NotificationCenter.default.addObserver(self, selector: #selector(mainWindowWillClose), name: NSWindow.willCloseNotification, object: window)
        NotificationCenter.default.addObserver(self, selector: #selector(mainWindowBecameKey), name: NSWindow.didBecomeKeyNotification, object: window)
    }

    /// SwiftUI reuses the closed window when it's opened again (no new view, so no `adopt`).
    @objc private func mainWindowBecameKey(_ n: Notification) {
        mainClosed = false
        mainVisible = !isMini
    }

    /// Music plays on (the menu bar icon appears); a video stops with its window.
    @objc private func mainWindowWillClose(_ n: Notification) {
        guard (n.object as? NSWindow) === mainWindow else { return }
        mainClosed = true
        mainVisible = false
        MacVideo.shared.closeActive()
    }

    /// Cmd+W: the window in front. The mini bar has no close button: it goes back to the window.
    func closeFrontWindow() {
        if let key = NSApp.keyWindow, key === musicPanel { switchToFull(); return }
        (NSApp.keyWindow ?? mainWindow)?.performClose(nil)
    }

    /// Space plays and pauses, unless something's being typed. A menu shortcut with no modifier
    /// never fires by itself, so the key is caught here on its way in.
    private func installSpaceKey() {
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard event.keyCode == 49, event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return event }
            let typing = NSApp.keyWindow?.firstResponder is NSText
            guard !typing, NSApp.keyWindow?.attachedSheet == nil else { return event }
            MainActor.assumeIsolated { MacPlayback.shared.togglePlayPause() }
            return nil
        }
    }

    // MARK: Switching

    func toggleMini() { isMini ? switchToFull() : switchToMini() }

    func switchToMini() {
        guard !isMini else { return }
        isMini = true
        if MacVideo.shared.activePlayer != nil { showVideoPanel() } else { showMusicPanel() }
        mainWindow?.orderOut(nil)
        mainVisible = false
    }

    /// Back to the full window: from a mini player, the menu bar icon or the Dock.
    func switchToFull() {
        isMini = false
        closePanels()
        showMain()
    }

    func showMain() {
        if isMini { isMini = false; closePanels() }
        activate()
        if let mainWindow, !mainClosed {
            mainWindow.makeKeyAndOrderFront(nil)
        } else {
            openMain?()
        }
        mainVisible = true
    }

    /// A video started or ended while mini: the mini player follows.
    func videoChanged() {
        guard isMini else { return }
        if MacVideo.shared.activePlayer != nil {
            musicPanel?.orderOut(nil)
            showVideoPanel()
        } else if videoPanel != nil {
            // The video ended in the mini window: back to the full window.
            switchToFull()
        }
    }

    private func closePanels() {
        closingPanelsOurselves = true
        musicPanel?.orderOut(nil)
        musicPanel = nil
        videoPanel?.contentView = nil // lets go of the player view now
        videoPanel?.close()
        videoPanel = nil
        closingPanelsOurselves = false
    }

    private func showMusicPanel() {
        let panel = musicPanel ?? makeMusicPanel()
        musicPanel = panel
        panel.level = miniOnTop ? .floating : .normal
        panel.orderFrontRegardless()
        panel.makeKey()
    }

    private func showVideoPanel() {
        let panel = videoPanel ?? makeVideoPanel()
        videoPanel = panel
        panel.level = miniOnTop ? .floating : .normal
        panel.orderFrontRegardless()
        panel.makeKey()
    }

    private func makeMusicPanel() -> NSPanel {
        let size = CGSize(width: 680, height: 84)
        let panel = MacMiniPanel(contentRect: CGRect(origin: .zero, size: size), styleMask: [.borderless, .fullSizeContentView], backing: .buffered, defer: false)
        panel.isReleasedWhenClosed = false
        panel.isMovableByWindowBackground = true
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.title = "Marquee Mini Player"
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.contentView = NSHostingView(rootView: MacMiniBar().macEnvironment().preferredColorScheme(.dark))
        panel.setFrameAutosaveName("MarqueeMiniBar")
        if !panel.setFrameUsingName("MarqueeMiniBar") { panel.center() }
        panel.setContentSize(size)
        return panel
    }

    private func makeVideoPanel() -> NSPanel {
        let size = CGSize(width: 480, height: 270)
        let panel = MacMiniPanel(contentRect: CGRect(origin: .zero, size: size), styleMask: [.titled, .closable, .resizable, .fullSizeContentView], backing: .buffered, defer: false)
        panel.isReleasedWhenClosed = false
        panel.titleVisibility = .hidden
        panel.titlebarAppearsTransparent = true
        panel.isMovableByWindowBackground = true
        panel.contentAspectRatio = CGSize(width: 16, height: 9)
        panel.contentMinSize = CGSize(width: 320, height: 180)
        panel.backgroundColor = .black
        panel.title = "Marquee Mini Video"
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.delegate = self
        panel.contentView = NSHostingView(rootView: MacMiniVideo().macEnvironment().preferredColorScheme(.dark))
        panel.setFrameAutosaveName("MarqueeMiniVideo")
        if !panel.setFrameUsingName("MarqueeMiniVideo") { panel.center() }
        return panel
    }

    /// A cover's size: nearly the whole main window.
    var coverSize: CGSize {
        let w = mainWindow?.contentLayoutRect.size ?? CGSize(width: 1100, height: 700)
        return CGSize(width: max(640, w.width - 60), height: max(420, w.height - 60))
    }

    /// Comes forward, as the person asked; automated test runs leave the Mac's focus alone.
    private func activate() {
        #if DEBUG
        if ProcessInfo.processInfo.arguments.contains("-marquee-test-driver") { return }
        #endif
        NSApp.activate(ignoringOtherApps: true)
    }

    // MARK: Administration

    /// The server's web app, in the default browser.
    static func openAdministration() {
        guard let base = MacPlayback.shared.app.baseURL else { return }
        NSWorkspace.shared.open(base.appending(path: "settings"))
    }
}

extension MacWindows: NSWindowDelegate {
    /// The mini video window closed by its close button: the video stops with it.
    func windowWillClose(_ notification: Notification) {
        guard (notification.object as? NSWindow) === videoPanel, !closingPanelsOurselves else { return }
        videoPanel = nil
        MacVideo.shared.closeActive()
        if isMini { switchToFull() }
    }
}

/// A borderless panel that can still take the keyboard (for popovers and shortcuts).
final class MacMiniPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}
