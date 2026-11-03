import MarqueeKit
import SwiftUI

/// The Mac app: one main window, menus, and (while music plays with the window closed) a menu bar
/// icon. Everything that outlives a window lives in MacPlayback and MacWindows.
@main
struct MarqueeMacApp: App {
    @NSApplicationDelegateAdaptor(MacAppDelegate.self) private var delegate
    @State private var playback: MacPlayback
    @State private var windows: MacWindows

    init() {
        // The window always comes up at launch. With AppKit's saved state on, a session that ended
        // with the window closed (or the app killed) left the next one running with no window.
        UserDefaults.standard.register(defaults: ["ApplePersistenceIgnoreState": true, "NSQuitAlwaysKeepsWindows": false])
        _playback = State(initialValue: MacPlayback.shared)
        _windows = State(initialValue: MacWindows.shared)
    }

    var body: some Scene {
        Window("Marquee", id: MacWindows.mainID) {
            MacRootView()
                .macEnvironment()
                .frame(minWidth: MacWindows.minimumSize.width, minHeight: MacWindows.minimumSize.height)
                .background(MacOpenWindowCatcher())
        }
        .defaultSize(width: 1240, height: 800)
        .defaultLaunchBehavior(.presented)
        .restorationBehavior(.disabled)
        .windowToolbarStyle(.unified)
        .commands { MarqueeCommands(playback: playback, windows: windows) }

        MenuBarExtra("Marquee", systemImage: "music.note", isInserted: Binding(get: { windows.showsMenuBarIcon }, set: { _ in })) {
            MarqueeMenuBarContent()
                .macEnvironment()
        }
        .menuBarExtraStyle(.menu)
    }
}

/// Closing the window keeps the app running (a menu bar icon while music plays; the Dock reopens
/// it). Reopening from the Dock comes back to the full window.
final class MacAppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationDidFinishLaunching(_ notification: Notification) {
        MainActor.assumeIsolated { MacWindows.shared.appLaunched() }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        MainActor.assumeIsolated { MacWindows.shared.switchToFull() }
        return false
    }

    /// Nothing keeps a session open on the server. A Sonos room plays on by itself: quitting
    /// here doesn't stop it.
    func applicationWillTerminate(_ notification: Notification) {
        MainActor.assumeIsolated {
            if !MacPlayback.shared.sonos.isActive { MacPlayback.shared.music.stop() }
        }
    }
}

/// Keeps the ability to open the main window after it's been closed.
private struct MacOpenWindowCatcher: View {
    @Environment(\.openWindow) private var openWindow
    var body: some View {
        Color.clear.onAppear {
            let open = openWindow
            MacWindows.shared.openMain = { open(id: MacWindows.mainID) }
        }
    }
}

/// The first screen: what the other apps' root does, with what the iPhone app does at launch.
struct MacRootView: View {
    @Environment(AppSession.self) private var app
    @Environment(Downloads.self) private var downloads
    @Environment(SmartDownloads.self) private var smart

    var body: some View {
        RootView()
            .background(WindowReader { MacWindows.shared.adopt($0) })
            .onChange(of: app.state, initial: true) { if app.state == .signedIn { downloads.attach(app); runSmart() } }
            .onChange(of: app.client == nil) { if app.client != nil { downloads.attach(app); runSmart() } }
            .onChange(of: app.isOffline) { if !app.isOffline, app.client != nil { downloads.attach(app); runSmart() } }
            .onChange(of: app.me?.id) { runSmart() }
            .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in runSmart() }
            .preferredColorScheme(.dark)
            .tint(.accentColor)
            .formStyle(.grouped)
    }

    private func runSmart() {
        guard app.state == .signedIn, app.client != nil, !app.isOffline, app.me != nil else { return }
        Task { await smart.run(app) }
    }
}

/// Finds the NSWindow a view is in.
struct WindowReader: NSViewRepresentable {
    let found: (NSWindow) -> Void

    func makeNSView(context: Context) -> NSView {
        let v = ReaderView()
        v.found = found
        return v
    }

    func updateNSView(_ v: NSView, context: Context) {}

    private final class ReaderView: NSView {
        var found: ((NSWindow) -> Void)?
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let window { found?(window) }
        }
    }
}

// MARK: - Menus

/// Standard Mac menus plus Marquee's: Controls, and the mini player under View. What changes with
/// the app's state is in views of their own: only a view's body follows what it reads.
struct MarqueeCommands: Commands {
    let playback: MacPlayback
    let windows: MacWindows

    var body: some Commands {
        SidebarCommands()
        CommandGroup(replacing: .newItem) {}
        CommandGroup(replacing: .saveItem) {
            Button("Close") { windows.closeFrontWindow() }
                .keyboardShortcut("w")
        }
        CommandGroup(replacing: .appSettings) { MacSettingsMenuItems() }
        CommandGroup(after: .sidebar) { MacMiniMenuItems() }
        CommandMenu("Controls") { MacControlsMenuItems() }
        CommandMenu("Go") {
            Button("Back") { MacNavigation.shared.goBack() }
                .keyboardShortcut("[")
            Divider()
            Button("Home") { go(MacNavigation.home) }
                .keyboardShortcut("1")
            Button("Playlists") { go(MacNavigation.playlists) }
                .keyboardShortcut("2")
            Button("Downloads") { go(MacNavigation.downloads) }
                .keyboardShortcut("3")
            Button("Search") {
                windows.showMain()
                MacNavigation.shared.focusSearch()
            }
            .keyboardShortcut("f")
        }
    }

    private func go(_ row: String) {
        windows.showMain()
        MacNavigation.shared.select(row)
    }
}

private struct MacSettingsMenuItems: View {
    @State private var playback = MacPlayback.shared

    var body: some View {
        Button("Settings…") {
            MacWindows.shared.showMain()
            MacNavigation.shared.select(MacNavigation.settings)
        }
        .keyboardShortcut(",")
        if playback.app.me?.isAdmin == true, !playback.app.isOffline {
            Button("Administration…") { MacWindows.openAdministration() }
        }
    }
}

private struct MacMiniMenuItems: View {
    @State private var windows = MacWindows.shared

    var body: some View {
        Divider()
        Button(windows.isMini ? "Switch to Full Window" : "Switch to Mini Player") { windows.toggleMini() }
            .keyboardShortcut("m", modifiers: [.command, .shift])
        Toggle("Keep Mini Player on Top", isOn: $windows.miniOnTop)
        Divider()
    }
}

private struct MacControlsMenuItems: View {
    @State private var playback = MacPlayback.shared
    private var music: MarqueeKit.MusicPlayer { playback.music }

    var body: some View {
        // Space itself is caught in MacWindows: a menu shortcut with no modifier doesn't fire.
        Button(playback.isPlayingAnything ? "Pause" : "Play") { playback.togglePlayPause() }
            .keyboardShortcut(.space, modifiers: [])
        Button("Next") { playback.next() }
            .keyboardShortcut(.rightArrow, modifiers: .command)
            .disabled(music.current == nil)
        Button("Previous") { playback.previous() }
            .keyboardShortcut(.leftArrow, modifiers: .command)
            .disabled(music.current == nil)
        Divider()
        Toggle("Shuffle", isOn: Binding(get: { music.queue.shuffled }, set: { _ in music.toggleShuffle() }))
            .disabled(music.current == nil)
        Button("Repeat: \(repeatLabel)") { music.cycleRepeat() }
            .disabled(music.current == nil)
        Divider()
        Button("Now Playing") {
            MacWindows.shared.showMain()
            MacNavigation.shared.showNowPlaying = true
        }
        .keyboardShortcut("l", modifiers: [.command])
        .disabled(music.current == nil)
    }

    private var repeatLabel: String {
        switch music.queue.repeatMode {
        case .off: "Off"
        case .all: "All"
        case .one: "One"
        }
    }
}

/// The menu bar icon's menu: what's playing, play/pause, next, open, mini, quit.
struct MarqueeMenuBarContent: View {
    @Environment(MusicPlayer.self) private var music

    var body: some View {
        if let t = music.current?.item {
            Text(t.title)
            Text(t.artistCredit ?? t.grandparentTitle ?? "")
            if let room = MacPlayback.shared.sonos.activeRoomName { Text("Playing on \(room)") }
        } else {
            Text("Nothing playing")
        }
        Divider()
        Button(music.playing ? "Pause" : "Play") { music.toggle() }
            .disabled(music.current == nil)
        Button("Next") { music.next() }
            .disabled(music.current == nil)
        Divider()
        Button("Open Marquee") { MacWindows.shared.switchToFull() }
        Button("Switch to Mini Player") { MacWindows.shared.switchToMini() }
        Divider()
        Button("Quit Marquee") { NSApp.terminate(nil) }
    }
}
