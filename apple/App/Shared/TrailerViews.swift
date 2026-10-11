import MarqueeKit
import SwiftUI
#if os(iOS)
import WebKit
#endif

/// The Trailer button on a movie's or show's page (PLAY-22). It shows once the server says
/// there is a trailer: a local one plays in the normal player, like any extra; a YouTube one
/// plays in YouTube's embedded player, full screen inside Marquee (nothing is downloaded to
/// the server, and YouTube's terms rule out playing its stream in our own player). The TV has
/// no WebKit, so there it opens the YouTube app.
struct TrailerButton: View {
    @Environment(VideoPresenter.self) private var video
    let trailer: ItemTrailer
    /// The movie or show, for the message when YouTube can't be opened.
    let title: String
    #if os(iOS)
    @State private var youtube: YouTubeTrailer?
    #else
    @State private var unavailable = false
    #endif

    var body: some View {
        Button(action: play) { Label(label, systemImage: "film") }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("trailerButton")
            #if os(iOS)
            .fullScreenCover(item: $youtube) { YouTubeTrailerPlayer(trailer: $0) }
            #else
            .alert("Trailer", isPresented: $unavailable) {
                Button("OK", role: .cancel) {}
            } message: {
                Text("Watch the trailer on YouTube: \(title)")
            }
            #endif
    }

    /// On the TV a YouTube trailer leaves Marquee for the YouTube app, so the button says so.
    private var label: String {
        #if os(tvOS)
        trailer.source == .youtube ? "Trailer (YouTube)" : "Trailer"
        #else
        "Trailer"
        #endif
    }

    private func play() {
        switch trailer.source {
        case .local:
            if let id = trailer.itemId { video.play(id) }
        case .youtube:
            guard let key = trailer.youtubeKey.flatMap(YouTubeTrailer.validKey) else { return }
            #if os(iOS)
            youtube = YouTubeTrailer(key: key, name: trailer.name ?? title)
            #else
            // No WebKit on the TV: the YouTube app, if it's installed.
            Task { if !(await YouTubeTrailer.openApp(key)) { unavailable = true } }
            #endif
        }
    }
}

/// A YouTube trailer to show.
struct YouTubeTrailer: Identifiable {
    let key: String
    let name: String
    var id: String { key }

    /// Only a plain video id (letters, digits, - and _) goes into a URL or the page.
    static func validKey(_ key: String) -> String? {
        let ok = !key.isEmpty && key.count <= 32 && key.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") }
        return ok ? key : nil
    }

    static func watchURL(_ key: String) -> URL { URL(string: "https://www.youtube.com/watch?v=\(key)")! }

    /// Opens the YouTube app, else (on iPhone/iPad) the watch page in Safari. False when
    /// neither could be opened. On iPhone/iPad only the error fallback's button calls this.
    @MainActor
    static func openApp(_ key: String) async -> Bool {
        if let app = URL(string: "youtube://watch?v=\(key)"), await UIApplication.shared.open(app) { return true }
        return await UIApplication.shared.open(watchURL(key))
    }
}

#if os(iOS)
/// The trailer full screen in YouTube's embedded player, with a close button. Only when the
/// embed fails (the owner disallows embedding: errors 150/152/153) is there a way out to YouTube.
struct YouTubeTrailerPlayer: View {
    @Environment(\.dismiss) private var dismiss
    let trailer: YouTubeTrailer
    /// YouTube's error code, once the embed reports one.
    @State private var error: Int?

    var body: some View {
        ZStack(alignment: .topLeading) {
            Color.black.ignoresSafeArea()
            if let error {
                failed(error)
            } else {
                YouTubeEmbed(key: trailer.key, onError: { error = $0 })
                    .ignoresSafeArea(edges: .bottom)
                    .accessibilityIdentifier("youtubePlayer")
            }
            Button { dismiss() } label: {
                Image(systemName: "xmark").font(.headline).foregroundStyle(.white)
                    .frame(width: 44, height: 44).background(.ultraThinMaterial, in: Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Close trailer")
            .accessibilityIdentifier("closeTrailer")
            .padding()
        }
        .preferredColorScheme(.dark)
        .statusBarHidden()
        .task {
            if ProcessInfo.processInfo.arguments.contains("-marquee-test-youtube-embed-error") { error = 150 }
        }
    }

    private func failed(_ code: Int) -> some View {
        VStack(spacing: 16) {
            Image(systemName: "exclamationmark.triangle").font(.largeTitle).foregroundStyle(.secondary)
            Text(trailer.name).font(.headline).multilineTextAlignment(.center)
            Text([100, 101, 150, 152, 153].contains(code)
                 ? "This trailer can't be played in Marquee: its owner only allows it on YouTube."
                 : "This trailer couldn't be played (YouTube error \(code)).")
                .font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
            Button {
                Task { _ = await YouTubeTrailer.openApp(trailer.key) }
            } label: {
                Label("Watch on YouTube", systemImage: "play.rectangle")
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("watchOnYouTube")
        }
        .padding(32)
        .frame(maxWidth: 480, maxHeight: .infinity)
        .frame(maxWidth: .infinity)
    }
}

/// YouTube's privacy-enhanced embed in a web view. It's loaded from a small page with a base
/// URL, so the iframe's request carries a referrer (YouTube refuses embeds without one). Links
/// in the player (the YouTube logo, the title, "Watch on YouTube") never leave the web view, so
/// iOS can't hand them to the YouTube app; the player's errors come back through `onError`.
private struct YouTubeEmbed: UIViewRepresentable {
    let key: String
    let onError: (Int) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onError: onError) }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.allowsInlineMediaPlayback = true
        config.mediaTypesRequiringUserActionForPlayback = []
        config.allowsPictureInPictureMediaPlayback = true
        config.userContentController.add(context.coordinator, name: "marquee")
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.uiDelegate = context.coordinator
        web.isOpaque = false
        web.backgroundColor = .black
        web.scrollView.backgroundColor = .black
        web.scrollView.isScrollEnabled = false
        web.scrollView.contentInsetAdjustmentBehavior = .never
        web.loadHTMLString(Self.page(key), baseURL: Self.origin)
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {}

    static func dismantleUIView(_ web: WKWebView, coordinator: Coordinator) {
        web.configuration.userContentController.removeScriptMessageHandler(forName: "marquee")
        web.loadHTMLString("", baseURL: nil) // stop the sound when the player closes
    }

    static let origin = URL(string: "https://marquee.app")!

    static func embedURL(_ key: String) -> String {
        "https://www.youtube-nocookie.com/embed/\(key)?autoplay=1&playsinline=1&rel=0&modestbranding=1&fs=1&enablejsapi=1&origin=\(origin.absoluteString)"
    }

    static func page(_ key: String) -> String {
        """
        <!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
        <style>html,body{margin:0;height:100%;background:#000;overflow:hidden}iframe{position:absolute;inset:0;width:100%;height:100%;border:0}</style>
        </head><body><iframe id="player" src="\(embedURL(key))" allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
        referrerpolicy="strict-origin-when-cross-origin" allowfullscreen></iframe>
        <script>
        function post(m){try{window.webkit.messageHandlers.marquee.postMessage(m)}catch(e){}}
        function onYouTubeIframeAPIReady(){new YT.Player('player',{events:{onError:function(e){post({error:Number(e.data)})}}})}
        var s=document.createElement('script');s.src='https://www.youtube.com/iframe_api';document.head.appendChild(s);
        </script></body></html>
        """
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
        let onError: (Int) -> Void
        init(onError: @escaping (Int) -> Void) { self.onError = onError }

        /// Our page and everything inside the embed's frames load; anything that would take the
        /// page itself elsewhere (youtube.com links, which iOS opens in the YouTube app) doesn't.
        func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                     decisionHandler: @escaping @MainActor (WKNavigationActionPolicy) -> Void) {
            decisionHandler(Self.allows(action.request.url, mainFrame: action.targetFrame?.isMainFrame) ? .allow : .cancel)
        }

        /// nil `mainFrame`: a link asking for a new window.
        static func allows(_ url: URL?, mainFrame: Bool?) -> Bool {
            guard let url, let mainFrame else { return false }
            if !mainFrame { return url.scheme == "https" || url.scheme == "about" }
            return url.absoluteString == "about:blank" || url.host == YouTubeEmbed.origin.host
        }

        /// Links that open a new window stay unopened.
        func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                     for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? { nil }

        func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
            guard let body = message.body as? [String: Any], let code = (body["error"] as? NSNumber)?.intValue else { return }
            onError(code)
        }
    }
}
#endif
