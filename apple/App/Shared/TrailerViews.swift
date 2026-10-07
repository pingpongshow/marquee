import MarqueeKit
import SwiftUI
#if os(iOS)
import WebKit
#endif

/// The Trailer button on a movie's or show's page (PLAY-22). It shows once the server says
/// there is a trailer: a local one plays in the normal player, like any extra; a YouTube one
/// plays from YouTube (nothing is downloaded to the server).
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
        Button(action: play) { Label("Trailer", systemImage: "film") }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("trailerButton")
            #if os(iOS)
            .sheet(item: $youtube) { YouTubeTrailerSheet(trailer: $0) }
            #else
            .alert("Trailer", isPresented: $unavailable) {
                Button("OK", role: .cancel) {}
            } message: {
                Text("Watch the trailer on YouTube: \(title)")
            }
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
    /// neither could be opened.
    @MainActor
    static func openApp(_ key: String) async -> Bool {
        if let app = URL(string: "youtube://watch?v=\(key)"), await UIApplication.shared.open(app) { return true }
        return await UIApplication.shared.open(watchURL(key))
    }
}

#if os(iOS)
/// The trailer in YouTube's embedded player, with a way out to the YouTube app.
struct YouTubeTrailerSheet: View {
    @Environment(\.dismiss) private var dismiss
    let trailer: YouTubeTrailer

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                YouTubeEmbed(key: trailer.key)
                    .aspectRatio(16 / 9, contentMode: .fit)
                    .background(Color.black)
                    .clipShape(RoundedRectangle(cornerRadius: 12))
                    .accessibilityIdentifier("youtubePlayer")
                Button {
                    Task { _ = await YouTubeTrailer.openApp(trailer.key) }
                } label: {
                    Label("Open in YouTube", systemImage: "play.rectangle")
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("openInYouTube")
                Spacer(minLength: 0)
            }
            .padding()
            .navigationTitle(trailer.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
        }
        .accessibilityIdentifier("youtubeTrailerSheet")
    }
}

/// YouTube's privacy-enhanced embed in a web view. It's loaded from a small page with a base
/// URL, so the iframe's request carries a referrer (YouTube refuses embeds without one).
private struct YouTubeEmbed: UIViewRepresentable {
    let key: String

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.allowsInlineMediaPlayback = true
        config.mediaTypesRequiringUserActionForPlayback = []
        let web = WKWebView(frame: .zero, configuration: config)
        web.isOpaque = false
        web.backgroundColor = .black
        web.scrollView.isScrollEnabled = false
        web.loadHTMLString(Self.page(key), baseURL: Self.origin)
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {}

    static func dismantleUIView(_ web: WKWebView, coordinator: ()) {
        web.loadHTMLString("", baseURL: nil) // stop the sound when the sheet closes
    }

    static let origin = URL(string: "https://marquee.app")!

    static func page(_ key: String) -> String {
        let src = "https://www.youtube-nocookie.com/embed/\(key)?autoplay=1&playsinline=1&rel=0&origin=\(origin.absoluteString)"
        return """
        <!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">
        <style>html,body{margin:0;height:100%;background:#000}iframe{position:absolute;inset:0;width:100%;height:100%;border:0}</style>
        </head><body><iframe src="\(src)" allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
        referrerpolicy="strict-origin-when-cross-origin" allowfullscreen></iframe></body></html>
        """
    }
}
#endif
