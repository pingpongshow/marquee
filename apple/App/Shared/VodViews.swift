import AVKit
import MarqueeKit
import SwiftUI

// Movies on demand ("VOD" in the UI) under Live TV (LIVE-6): the VOD tab (categories, search, sort, Continue
// Watching and a poster grid), a film's page, and full-screen playback in AVKit's player.

/// VOD on its own page (LIVE-6): the VOD entry after Live TV opens it.
struct VodPage: View {
    @State private var refresh = 0

    var body: some View {
        ScrollView {
            VodBrowseView(refresh: refresh).padding(.vertical)
        }
        .refreshable { refresh += 1 }
        // (No title on the TV: MainView hides it there, as the sidebar names the page.)
        .navigationTitle("VOD")
    }
}

struct VodBrowseView: View {
    @Environment(AppSession.self) private var app
    /// Bumped by the page's pull-to-refresh.
    var refresh = 0
    @AppStorage("marquee.vod.category") private var category = ""
    @AppStorage("marquee.vod.sort") private var sortRaw = VodSort.title.rawValue
    @State private var text = ""
    @State private var query = ""
    @State private var categories: [VodCategory] = []
    @State private var resume: [VodMovie] = []
    @State private var movies: [VodMovie] = []
    @State private var total: Int?
    @State private var loading = false
    @State private var error: String?
    @State private var status: VodStatus?
    @State private var refreshing = false

    private var sort: VodSort { VodSort(rawValue: sortRaw) ?? .title }
    private var isAdmin: Bool { app.me?.isAdmin == true }
    private static let sorts: [(VodSort, String)] = [(.title, "Title"), (.added, "Recently Added"), (.year, "Release Year"), (.rating, "Rating")]

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            controls.padding(.horizontal, sidePadding)
            if category.isEmpty, query.isEmpty, !resume.isEmpty {
                ShelfRow(title: "Continue Watching") {
                    ForEach(resume, id: \.id) { m in VodTile(movie: m, width: tileWidth) }
                }
                .accessibilityIdentifier("vod.continue")
            }
            VStack(alignment: .leading, spacing: 12) {
                if (!category.isEmpty || !query.isEmpty), let total {
                    Text("\(total) \(total == 1 ? "movie" : "movies")").font(.callout).foregroundStyle(.secondary)
                }
                if let error { ErrorBanner(message: error) }
                if movies.isEmpty {
                    if loading { ProgressView().frame(maxWidth: .infinity) }
                    else if error == nil && total != nil {
                        Text(query.isEmpty ? "No movies here yet." : "No movies match “\(query)”.").foregroundStyle(.secondary)
                    }
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: tileWidth, maximum: tileWidth * 1.4), spacing: gridSpacing, alignment: .top)],
                              alignment: .leading, spacing: gridSpacing) {
                        ForEach(movies, id: \.id) { m in
                            VodTile(movie: m, width: tileWidth)
                                .onAppear { if m.id == movies.last?.id { Task { await loadMore() } } }
                        }
                    }
                    if loading { ProgressView().frame(maxWidth: .infinity) }
                }
            }
            .padding(.horizontal, sidePadding)
            if isAdmin, let status { adminStatus(status).padding(.horizontal, sidePadding) }
        }
        .task(id: "\(category)|\(query)|\(sortRaw)|\(refresh)") { await reload() }
        .task(id: text) {
            // Search as they type, once they pause.
            try? await Task.sleep(for: .milliseconds(300))
            if !Task.isCancelled { query = text.trimmingCharacters(in: .whitespaces) }
        }
        // Back from a film or its player: its resume point or watched state may have changed.
        .onAppear { Task { await reloadResume() } }
    }

    private var controls: some View {
        HStack(spacing: 12) {
            Menu {
                Picker("Category", selection: $category) {
                    Text("All Movies").tag("")
                    ForEach(categories, id: \.name) { c in Text("\(c.name) (\(c.count))").tag(c.name) }
                }
            } label: {
                Label(category.isEmpty ? "All Movies" : category, systemImage: "line.3.horizontal.decrease.circle").lineLimit(1)
            }
            .accessibilityIdentifier("vod.category")
            TextField("Search movies", text: $text)
                #if os(iOS)
                .textFieldStyle(.roundedBorder)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .frame(maxWidth: 280)
                #else
                .frame(maxWidth: 520)
                #endif
                .submitLabel(.search)
                .accessibilityIdentifier("vod.search")
            Spacer(minLength: 0)
            Menu {
                Picker("Sort", selection: $sortRaw) {
                    ForEach(Self.sorts, id: \.0) { s, name in Text(name).tag(s.rawValue) }
                }
            } label: {
                Label(Self.sorts.first { $0.0 == sort }?.1 ?? "Sort", systemImage: "arrow.up.arrow.down")
                    #if os(iOS)
                    .labelStyle(.iconOnly)
                    #endif
            }
            .accessibilityLabel("Sort")
            .accessibilityValue(Self.sorts.first { $0.0 == sort }?.1 ?? "")
            .accessibilityIdentifier("vod.sort")
        }
    }

    /// For admins: what the sources loaded, and a way to load them again now.
    private func adminStatus(_ s: VodStatus) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("\(s.movies) movies from \(s.sources.count) \(s.sources.count == 1 ? "source" : "sources")\(s.matching ? " · matching details…" : "")")
                    .font(.footnote).foregroundStyle(.secondary)
                Spacer()
                Button(refreshing ? "Refreshing…" : "Refresh Now") {
                    refreshing = true
                    Task {
                        do { status = try await app.refreshVod() } catch { self.error = error.localizedDescription }
                        refreshing = false
                        await reload()
                    }
                }
                .disabled(refreshing)
                .font(.footnote)
                .accessibilityIdentifier("vod.refresh")
            }
            ForEach(s.sources.filter { $0.error != nil }, id: \.id) { src in
                Text("\(src.name): \(src.error ?? "")").font(.footnote).foregroundStyle(.red)
            }
        }
        .padding(.top, 8)
    }

    private func reload() async {
        loading = true
        error = nil
        async let cats = try? app.vodCategories()
        async let st = isAdmin ? try? app.vodStatus() : nil
        await reloadResume()
        do {
            let page = try await app.vodMovies(category: category.isEmpty ? nil : category, query: query.isEmpty ? nil : query, sort: sort)
            movies = page.movies
            total = page.total
        } catch is CancellationError {
        } catch {
            if !Task.isCancelled { self.error = error.localizedDescription }
        }
        if let c = await cats {
            categories = c
            // A category that went away (the source changed): back to all.
            if !category.isEmpty, !c.contains(where: { $0.name == category }) { category = "" }
        }
        if let s = await st { status = s }
        loading = false
    }

    private func reloadResume() async {
        if let r = try? await app.vodContinueWatching() { resume = r }
    }

    /// The next page, as the end of the grid comes into view.
    private func loadMore() async {
        guard !loading, let total, movies.count < total else { return }
        loading = true
        defer { loading = false }
        if let page = try? await app.vodMovies(category: category.isEmpty ? nil : category, query: query.isEmpty ? nil : query, sort: sort, offset: movies.count) {
            let have = Set(movies.map(\.id))
            movies += page.movies.filter { !have.contains($0.id) }
            self.total = page.total
        }
    }

    #if os(tvOS)
    private let tileWidth: CGFloat = 220
    private let gridSpacing: CGFloat = 48
    #else
    private let tileWidth: CGFloat = 120
    private let gridSpacing: CGFloat = 16
    #endif
}

/// A film's poster with its resume bar and watched tick, linking to its page.
struct VodTile: View {
    let movie: VodMovie
    var width: CGFloat = 130

    var body: some View {
        #if os(tvOS)
        VStack(alignment: .leading, spacing: 14) {
            NavigationLink(value: Route.vodMovie(movie.id)) { VodPoster(movie: movie, width: width) }
                .buttonStyle(.card)
                .accessibilityLabel(movie.title)
                .accessibilityIdentifier("vod.tile")
            titles
        }
        .frame(width: width)
        #else
        NavigationLink(value: Route.vodMovie(movie.id)) {
            VStack(alignment: .leading, spacing: 6) {
                VodPoster(movie: movie, width: width)
                titles
            }
            .frame(width: width)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(movie.title)
        .accessibilityAddTraits(.isButton)
        .accessibilityIdentifier("vod.tile")
        #endif
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(movie.title).font(.subheadline.weight(.medium)).lineLimit(1)
            Text(movie.metaLine).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }
    }
}

/// A film's poster from the server (no token needed), or a coloured placeholder with its title.
struct VodPoster: View {
    @Environment(AppSession.self) private var app
    let movie: VodMovie
    var width: CGFloat = 130

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            placeholder
            CachedImage(url: app.vodImageURL(movie.posterUrl)) { phase in
                if let image = phase.image { image.resizable().scaledToFill() }
            }
        }
        .frame(width: width, height: width * 1.5)
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        .overlay(alignment: .topTrailing) {
            if movie.watched {
                Image(systemName: "checkmark.circle.fill").symbolRenderingMode(.palette).foregroundStyle(.black, Color.marqueeGold).padding(6)
                    .accessibilityLabel("Watched")
            }
        }
        .overlay(alignment: .bottom) {
            if let p = movie.progress {
                GeometryReader { g in
                    ZStack(alignment: .leading) {
                        Rectangle().fill(.black.opacity(0.6))
                        Rectangle().fill(Color.marqueeGold).frame(width: g.size.width * p)
                    }
                }
                .frame(height: 4)
            }
        }
    }

    private var placeholder: some View {
        let hash = movie.title.unicodeScalars.reduce(UInt32(5381)) { ($0 &<< 5) &+ $0 &+ $1.value }
        let hue = Double(hash % 360) / 360
        return LinearGradient(colors: [Color(hue: hue, saturation: 0.35, brightness: 0.3), Color(hue: hue, saturation: 0.3, brightness: 0.12)],
                              startPoint: .topLeading, endPoint: .bottomTrailing)
            .overlay(alignment: .bottomLeading) {
                Text(movie.title).font(.caption.weight(.semibold)).foregroundStyle(.white.opacity(0.85)).lineLimit(3).padding(8)
            }
    }
}

extension VodMovie {
    /// "1985 · 1 hr 32 min".
    var metaLine: String {
        [year.map(String.init), formatDuration(ms: runtimeMs)].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// How far through the viewer is (for the resume bar), when part-way.
    var progress: Double? {
        guard !watched, let pos = positionMs, pos > 0, let total = runtimeMs, total > 0 else { return nil }
        return min(1, max(0.02, Double(pos) / Double(total)))
    }
}

/// What to play: a film from a position (nil = its resume point).
struct VodPlayTarget: Identifiable {
    let movie: VodMovieDetail
    let startMs: Int64?
    var id: Int64 { movie.id }
}

/// A film's page: backdrop, poster, details, Play or Resume, Play from Start and Watched.
struct VodMovieView: View {
    @Environment(AppSession.self) private var app
    let id: Int64
    @State private var movie: VodMovieDetail?
    @State private var error: String?
    @State private var playing: VodPlayTarget?
    /// Shown at once when changed, before the server confirms.
    @State private var watched: Bool?

    var body: some View {
        Group {
            if let m = movie {
                page(m)
            } else if let error {
                ContentUnavailableView {
                    Label("Couldn't load the movie", systemImage: "exclamationmark.triangle")
                } description: { Text(error) } actions: {
                    Button("Try Again") { Task { await load() } }
                }
            } else {
                ProgressView()
            }
        }
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .task { await load() }
        .fullScreenCover(item: $playing, onDismiss: { Task { await load() } }) { t in
            VodPlayerView(movie: t.movie, startMs: t.startMs)
        }
    }

    private func page(_ m: VodMovieDetail) -> some View {
        let isWatched = watched ?? m.base.watched
        let resumeMs = m.base.positionMs ?? 0
        return ScrollView {
            ZStack(alignment: .top) {
                backdrop(m)
                VStack(alignment: .leading, spacing: 24) {
                    header(m, isWatched: isWatched, resumeMs: resumeMs)
                    if let o = m.info.overview, !o.isEmpty { Text(o).font(.body).frame(maxWidth: 900, alignment: .leading) }
                    if !m.info.matched {
                        Text("Details from the source (not matched to TMDB).").font(.footnote).foregroundStyle(.secondary)
                    }
                    credits(m)
                }
                .padding(.horizontal, sidePadding)
                .padding(.top, isTV ? 60 : 24)
                .padding(.bottom, 40)
            }
        }
        .navigationTitle(isTV ? "" : m.title)
    }

    @ViewBuilder private func backdrop(_ m: VodMovieDetail) -> some View {
        if let url = app.vodImageURL(m.info.backdropUrl) {
            CachedImage(url: url) { phase in
                if let image = phase.image { image.resizable().scaledToFill() }
            }
            .frame(height: isTV ? 600 : 300)
            .frame(maxWidth: .infinity)
            .clipped()
            .opacity(0.35)
            .mask(LinearGradient(colors: [.black, .clear], startPoint: .center, endPoint: .bottom))
            .accessibilityHidden(true)
        }
    }

    private func header(_ m: VodMovieDetail, isWatched: Bool, resumeMs: Int64) -> some View {
        let poster = VodPoster(movie: m.base, width: isTV ? 300 : 130)
        let details = VStack(alignment: .leading, spacing: 10) {
            if let c = m.base.category { Text(c).font(.caption).foregroundStyle(.secondary) }
            Text(m.title).font(isTV ? .title2.bold() : .title.bold()).fixedSize(horizontal: false, vertical: true)
            let meta = [m.base.year.map(String.init), m.base.contentRating, formatDuration(ms: m.base.runtimeMs)].compactMap { $0 }.filter { !$0.isEmpty }
            if !meta.isEmpty { Text(meta.joined(separator: " · ")).font(.callout).foregroundStyle(.secondary) }
            if !m.info.genres.isEmpty { Text(m.info.genres.joined(separator: ", ")).font(.footnote).foregroundStyle(.secondary) }
            if let r = m.base.rating {
                HStack(spacing: 6) {
                    Text("TMDB").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    Text(String(format: "%.1f", r)).font(.callout.weight(.semibold))
                }
                .padding(.horizontal, 8).padding(.vertical, 4)
                .background(Color.secondary.opacity(0.18), in: RoundedRectangle(cornerRadius: 6))
                .accessibilityElement(children: .combine)
                .accessibilityLabel("TMDB score \(String(format: "%.1f", r))")
            }
            if let t = m.info.tagline, !t.isEmpty { Text(t).font(.callout.italic()).foregroundStyle(.secondary) }
            actions(m, isWatched: isWatched, resumeMs: resumeMs).padding(.top, 6)
        }
        return Group {
            if isTV {
                HStack(alignment: .top, spacing: 48) { poster; details }
            } else {
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .top, spacing: 20) { poster; details.frame(minWidth: 280, alignment: .leading) }
                    VStack(alignment: .leading, spacing: 16) { poster; details }
                }
            }
        }
    }

    private func actions(_ m: VodMovieDetail, isWatched: Bool, resumeMs: Int64) -> some View {
        let resume = resumeMs > 0
        return HStack(spacing: 12) {
            Button { playing = VodPlayTarget(movie: m, startMs: nil) } label: {
                Label(resume ? "Resume from \(formatTime(seconds: Double(resumeMs) / 1000))" : "Play", systemImage: "play.fill")
            }
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier("vod.play")
            if resume {
                Button { playing = VodPlayTarget(movie: m, startMs: 0) } label: {
                    Label("Play from Start", systemImage: "arrow.counterclockwise")
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("vod.fromStart")
            }
            Button { toggleWatched(m, now: !isWatched) } label: {
                Label(isWatched ? "Watched" : "Mark watched", systemImage: isWatched ? "checkmark.circle.fill" : "checkmark.circle")
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("vod.watched")
        }
        #if os(iOS)
        .fixedSize(horizontal: false, vertical: true)
        #endif
    }

    @ViewBuilder private func credits(_ m: VodMovieDetail) -> some View {
        if let d = m.info.director, !d.isEmpty {
            (Text("Directed by ").foregroundStyle(.secondary) + Text(d).fontWeight(.medium)).font(.callout)
        }
        if !m.info.cast.isEmpty {
            VStack(alignment: .leading, spacing: 10) {
                Text("Cast").font(.title3.bold())
                LazyVGrid(columns: [GridItem(.adaptive(minimum: isTV ? 320 : 150), alignment: .topLeading)], alignment: .leading, spacing: 10) {
                    ForEach(Array(m.info.cast.enumerated()), id: \.offset) { _, c in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(c.name).font(.callout.weight(.medium)).lineLimit(1)
                            if let ch = c.character { Text(ch).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                        }
                        #if os(tvOS)
                        .focusable() // the remote can scroll down through the cast
                        #endif
                    }
                }
            }
        }
    }

    private func toggleWatched(_ m: VodMovieDetail, now: Bool) {
        watched = now
        Task {
            do {
                try await app.setVodWatched(m.id, now)
                await load()
            } catch {
                self.error = error.localizedDescription
            }
            watched = nil
        }
    }

    private func load() async {
        do {
            movie = try await app.vodMovie(id)
            error = nil
        } catch is CancellationError {
        } catch {
            if movie == nil { self.error = error.localizedDescription }
        }
    }
}

/// Full-screen playback of a film: the source's own HLS through the server, seekable, in
/// AVKit's player. Its own player, not the library's: no trailers, rating request, Up Next or
/// watch together for films on demand.
struct VodPlayerView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss
    let movie: VodMovieDetail
    /// nil: from the viewer's resume point.
    let startMs: Int64?
    @State private var player = AVPlayer()
    @State private var session: String?
    @State private var error: String?
    @State private var loading = true
    @State private var closing = false
    @State private var observers: [NSObjectProtocol] = []
    @State private var rateWatch: NSKeyValueObservation?
    #if os(iOS)
    @State private var chrome = PlayerChrome()
    #endif

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            VodPlayerController(player: player, chrome: chromeModel)
                .ignoresSafeArea()
                .accessibilityIdentifier("vod.player")
            if loading && error == nil { ProgressView().tint(.white) }
            VStack {
                #if os(iOS)
                ZStack {
                    if chrome.shown { topBar.transition(.opacity) }
                }
                .frame(height: 44)
                .padding(.horizontal)
                .padding(.top, PlayerChrome.topInset)
                .animation(.easeInOut(duration: 0.25), value: chrome.shown)
                #endif
                if let error { ErrorBanner(message: error).padding().frame(maxWidth: 600) }
                Spacer()
            }
        }
        .task { await start() }
        .onDisappear { finish() }
        #if os(tvOS)
        .onExitCommand { close() }
        #endif
    }

    #if os(iOS)
    private var topBar: some View {
        HStack(spacing: 12) {
            Button { close() } label: { PlayerBubble(systemImage: "xmark") }
                .accessibilityLabel("Close player")
            VStack(alignment: .leading, spacing: 2) {
                Text(movie.title).font(.headline).lineLimit(1)
                if !movie.base.metaLine.isEmpty { Text(movie.base.metaLine).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
            }
            .shadow(radius: 3)
            Spacer()
        }
        .buttonStyle(.plain)
        .foregroundStyle(.white)
    }
    #endif

    private var chromeModel: PlayerChrome? {
        #if os(iOS)
        chrome
        #else
        nil
        #endif
    }

    private func start() async {
        music.pause() // a film takes over from the music
        do {
            let s = try await app.playVod(movie.id)
            if Task.isCancelled || closing { await app.stopVod(s.id); return }
            session = s.id
            let item = AVPlayerItem(url: s.url)
            item.externalMetadata = metadata()
            player.replaceCurrentItem(with: item)
            watch(item)
            let from = startMs ?? s.startMs
            if from > 0 {
                // Seek once the stream is ready, then play from there.
                for _ in 0..<100 where item.status == .unknown { try? await Task.sleep(for: .milliseconds(100)) }
                if item.status == .readyToPlay {
                    await player.seek(to: CMTime(value: from, timescale: 1000), toleranceBefore: .zero, toleranceAfter: .positiveInfinity)
                }
            }
            if item.status == .failed { throw MarqueeError(item.error?.localizedDescription ?? "The movie couldn't be played") }
            player.play()
            loading = false
            // Where they are, every 10 seconds while playing.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(10))
                if Task.isCancelled { break }
                if player.timeControlStatus == .playing { await report() }
            }
        } catch {
            loading = false
            if !Task.isCancelled { self.error = error.localizedDescription }
        }
    }

    /// Reports on pause, and goes back to the film's page at the end.
    private func watch(_ item: AVPlayerItem) {
        let end = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: item, queue: .main) { _ in
            Task { @MainActor in close() }
        }
        let failed = NotificationCenter.default.addObserver(forName: AVPlayerItem.failedToPlayToEndTimeNotification, object: item, queue: .main) { _ in
            Task { @MainActor in error = "The movie stopped playing." }
        }
        observers = [end, failed]
        rateWatch = player.observe(\.timeControlStatus, options: [.new]) { p, _ in
            let paused = p.timeControlStatus == .paused
            Task { @MainActor in
                if paused { await report() }
            }
        }
    }

    private var position: Double { player.currentTime().seconds }

    private var durationMs: Int64? {
        let d = player.currentItem?.duration.seconds ?? .nan
        return d.isFinite && d > 0 ? Int64(d * 1000) : nil
    }

    private func report() async {
        guard let session, position.isFinite, position > 0 else { return }
        await app.reportVod(session, positionMs: Int64(position * 1000), durationMs: durationMs)
    }

    private func close() {
        finish()
        dismiss()
    }

    /// Records where they stopped, then ends the session (the position is reported against it).
    private func finish() {
        guard !closing else { return }
        closing = true
        rateWatch = nil
        observers.forEach(NotificationCenter.default.removeObserver)
        observers = []
        let id = session
        session = nil
        let atEnd = player.currentItem.map { item in
            let d = item.duration.seconds
            return d.isFinite && d > 0 && position >= d - 1
        } ?? false
        let pos = atEnd ? (durationMs ?? Int64(position * 1000)) : Int64(max(0, position) * 1000)
        let dur = durationMs
        player.pause()
        player.replaceCurrentItem(with: nil)
        guard let id else { return }
        Task {
            if pos > 0 { await app.reportVod(id, positionMs: pos, durationMs: dur) }
            await app.stopVod(id)
        }
    }

    private func metadata() -> [AVMetadataItem] {
        let title = AVMutableMetadataItem()
        title.identifier = .commonIdentifierTitle
        title.value = movie.title as NSString
        var out: [AVMetadataItem] = [title]
        if let o = movie.info.overview {
            let desc = AVMutableMetadataItem()
            desc.identifier = .commonIdentifierDescription
            desc.value = o as NSString
            out.append(desc)
        }
        return out
    }
}

/// AVPlayerViewController for a film: native controls, scrubbing, PiP and AirPlay.
private struct VodPlayerController: UIViewControllerRepresentable {
    let player: AVPlayer
    var chrome: PlayerChrome?

    func makeUIViewController(context: Context) -> PlayerHost {
        let host = PlayerHost()
        let vc = host.controller
        vc.player = player
        host.chrome = chrome
        #if os(iOS)
        vc.updatesNowPlayingInfoCenter = true
        vc.allowsPictureInPicturePlayback = true
        vc.entersFullScreenWhenPlaybackBegins = false
        chrome?.covered = { [weak host] in host?.covered() ?? false }
        context.coordinator.watch(player, chrome: chrome)
        #endif
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
        return host
    }

    func updateUIViewController(_ host: PlayerHost, context: Context) {
        if host.controller.player !== player { host.controller.player = player }
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    final class Coordinator {
        #if os(iOS)
        private var rate: NSKeyValueObservation?
        /// Playing or paused, for the overlay buttons (they stay up while paused).
        func watch(_ player: AVPlayer, chrome: PlayerChrome?) {
            rate = player.observe(\.timeControlStatus, options: [.initial, .new]) { [weak chrome] p, _ in
                let playing = p.timeControlStatus != .paused
                Task { @MainActor in chrome?.setPlaying(playing) }
            }
        }
        #endif
    }
}
