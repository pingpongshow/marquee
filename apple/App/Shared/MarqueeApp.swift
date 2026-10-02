import MarqueeKit
import SwiftUI

@main
struct MarqueeApp: App {
    @State private var app: AppSession
    @State private var music: MusicPlayer

    init() {
        // Artwork responses are immutable, so a large cache makes browsing instant.
        URLCache.shared = URLCache(memoryCapacity: 128 << 20, diskCapacity: 2 << 30)
        // UI tests start from a clean slate.
        if ProcessInfo.processInfo.arguments.contains("-marquee-reset") {
            for s in ServerStore.servers { ServerStore.forget(s.id) }
        }
        let session = AppSession()
        _app = State(initialValue: session)
        _music = State(initialValue: MusicPlayer(app: session))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .environment(music)
                #if os(iOS)
                .tint(.accentColor) // tvOS keeps the system's white focus style
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
