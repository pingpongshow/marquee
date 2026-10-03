import MarqueeKit
import SwiftUI

#if os(iOS)
/// Hands background download events (the system relaunching the app) to the download manager.
final class AppDelegate: NSObject, UIApplicationDelegate {
    static var downloads: Downloads?
    func application(_ application: UIApplication, handleEventsForBackgroundURLSession identifier: String,
                     completionHandler: @escaping () -> Void) {
        MainActor.assumeIsolated { AppDelegate.downloads?.backgroundCompletion = completionHandler }
    }
}
#endif

@main
struct MarqueeApp: App {
    @State private var app: AppSession
    @State private var music: MusicPlayer
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var downloads: Downloads
    #endif

    init() {
        // Artwork responses are immutable, so a large cache makes browsing instant.
        URLCache.shared = URLCache(memoryCapacity: 128 << 20, diskCapacity: 2 << 30)
        // UI tests start from a clean slate.
        if ProcessInfo.processInfo.arguments.contains("-marquee-reset") {
            for s in ServerStore.servers { ServerStore.forget(s.id) }
            for key in ["marquee.showAudioQuality", "marquee.dj"] { UserDefaults.standard.removeObject(forKey: key) }
        }
        let session = AppSession()
        let player = MusicPlayer(app: session)
        _app = State(initialValue: session)
        _music = State(initialValue: player)
        #if os(iOS)
        let dl = Downloads()
        player.downloads = dl
        AppDelegate.downloads = dl
        _downloads = State(initialValue: dl)
        Shared.app = session
        Shared.music = player
        Shared.downloads = dl
        CastController.setUp(app: session, music: player) // Chromecast (D82)
        #endif
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .environment(music)
                #if os(iOS)
                .environment(downloads)
                .tint(.accentColor) // tvOS keeps the system's white focus style
                .onChange(of: app.state, initial: true) { if app.state == .signedIn { downloads.attach(app) } }
                .onChange(of: app.client == nil) { if app.client != nil { downloads.attach(app) } }
                #endif
                .preferredColorScheme(.dark)
        }
    }
}

/// Server → sign-in → app.
struct RootView: View {
    @Environment(AppSession.self) private var app

    var body: some View {
        switch app.state {
        case .noServer: ConnectView()
        case .connecting: ConnectingView()
        case .signedOut: SignInView()
        case .signedIn: MainView()
        }
    }
}

struct ConnectingView: View {
    @Environment(AppSession.self) private var app
    var body: some View {
        VStack(spacing: 16) {
            ProgressView()
            Text("Connecting to \(app.server?.name ?? "your server")…").foregroundStyle(.secondary)
        }
    }
}
