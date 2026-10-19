import MarqueeKit
import SwiftUI
#if os(tvOS)
import TVServices
#endif

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
    /// Smart downloads: next episodes and favourite music kept on the device.
    @State private var smart: SmartDownloads
    @Environment(\.scenePhase) private var scenePhase
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
        _smart = State(initialValue: SmartDownloads(downloads: dl))
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
                .environment(smart)
                .tint(.accentColor) // tvOS keeps the system's white focus style
                .onChange(of: app.state, initial: true) { if app.state == .signedIn { downloads.attach(app); runSmart() } }
                .onChange(of: app.client == nil) { if app.client != nil { downloads.attach(app); runSmart() } }
                // No background refresh: smart downloads catch up whenever the app comes back.
                .onChange(of: scenePhase) { old, new in if old != new, new == .active { runSmart() } }
                .onChange(of: app.me?.id) { runSmart() }
                #endif
                .preferredColorScheme(.dark)
        }
    }

    #if os(iOS)
    private func runSmart() {
        guard app.state == .signedIn, app.client != nil, app.me != nil else { return }
        Task { await smart.run(app) }
    }
    #endif
}

/// Server → sign-in → app.
struct RootView: View {
    @Environment(AppSession.self) private var app
    /// The last person signed in (server and user), to notice another one: a profile switch,
    /// signing out and in as someone else, or another server.
    @State private var lastUser: String?
    /// Bumped when another person signs in: the app's pages start again for them (a profile
    /// switch stays signed in, so Home, the tabs and pushed pages kept the last person's).
    @State private var userGeneration = 0

    var body: some View {
        Group {
            switch app.state {
            case .noServer: ConnectView()
            case .connecting: ConnectingView()
            case .signedOut: SignInView()
            case .signedIn: MainView(generation: userGeneration)
            }
        }
        // What one person did this session (watched marks, ratings, favourites, Not interested,
        // the search) isn't shown to the next.
        .onChange(of: app.me.map { "\(app.server?.id ?? "-")|\($0.id)" }) { _, new in
            guard let new, new != lastUser else { return }
            if lastUser != nil {
                Self.forgetSessionState()
                userGeneration += 1
            }
            lastUser = new
        }
        #if os(tvOS)
        // Signed out: the Top Shelf no longer shows their Continue Watching.
        .onChange(of: app.state, initial: true) {
            guard app.state == .signedOut || app.state == .noServer, TopShelfSnapshot.load() != nil else { return }
            TopShelfSnapshot.clear()
            TVTopShelfContentProvider.topShelfContentDidChange()
        }
        #endif
    }

    static func forgetSessionState() {
        WatchStore.shared.reset()
        RatingStore.shared.reset()
        FavoriteStore.shared.reset()
        WatchlistStore.shared.reset()
        VodSuggestionHides.shared.reset()
        SearchText.shared.query = ""
        RatingPrompt.shared.pending = nil
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
