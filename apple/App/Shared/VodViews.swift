import AVKit
import MarqueeKit
import SwiftUI

// Movies and shows on demand under Live TV (LIVE-6): the VOD Movies and VOD Shows pages
// (categories, search, sort, filters, Continue Watching and a poster grid), a film's page, and
// full-screen playback in AVKit's player (films and episodes, with Up Next between episodes).
// A show's page is in VodShowsViews.swift.

/// VOD Movies and VOD Shows (LIVE-6), each its own page after Live TV: categories, search,
/// sort, filters, Continue Watching and a poster grid.
struct VodPage: View {
    /// Shows (series) rather than films.
    var shows = false

    var body: some View {
        VodBrowseView(shows: shows)
            // (No title on the TV: MainView hides it there, as the sidebar names the page.)
            .navigationTitle(shows ? "VOD Shows" : "VOD Movies")
    }
}

struct VodBrowseView: View {
    @Environment(AppSession.self) private var app
    let shows: Bool
    @AppStorage private var category: String
    @AppStorage private var sortRaw: String
    /// The page's filters (VodFilters as JSON), remembered per page.
    @AppStorage private var filtersRaw: String
    @State private var refresh = 0
    @State private var text = ""
    @State private var query = ""
    @State private var categories: [VodCategory] = []
    @State private var genres: [VodCategory] = []
    @State private var resume: [VodMovie] = []
    @State private var resumeShows: [VodSeries] = []
    @State private var movies: [VodMovie] = []
    @State private var series: [VodSeries] = []
    @State private var total: Int?
    @State private var loading = false
    @State private var error: String?
    @State private var status: VodStatus?
    @State private var refreshing = false

    init(shows: Bool) {
        self.shows = shows
        let prefix = shows ? "marquee.vod.shows." : "marquee.vod."
        _category = AppStorage(wrappedValue: "", prefix + "category")
        _sortRaw = AppStorage(wrappedValue: VodSort.title.rawValue, prefix + "sort")
        _filtersRaw = AppStorage(wrappedValue: "", prefix + "filters")
    }

    private var sort: VodSort { VodSort(rawValue: sortRaw) ?? .title }
    private var filters: VodFilters { VodFilters(stored: filtersRaw) }
    private var isAdmin: Bool { app.me?.isAdmin == true }
    private var noun: (one: String, many: String) { shows ? ("show", "shows") : ("movie", "movies") }
    private var count: Int { shows ? series.count : movies.count }
    /// The sources with something on this page (the source filter shows when there are two or more).
    private var sources: [VodStatus.SourcesPayloadPayload] {
        (status?.sources ?? []).filter { shows ? $0.series > 0 : $0.movies > 0 }
    }
    private static let sorts: [(VodSort, String)] = [(.title, "Title"), (.added, "Recently Added"), (.year, "Release Year"), (.rating, "Rating")]

    var body: some View {
        #if os(tvOS)
        // As in the libraries: the filter menu stays pinned at the top right while the grid
        // scrolls (Right from the grid's edge reaches it).
        scroll
            .safeAreaInset(edge: .trailing, alignment: .top, spacing: 0) {
                filterMenu
                    .labelStyle(.iconOnly)
                    .padding(.top, 30)
                    .padding(.trailing, 50)
                    .frame(maxHeight: .infinity, alignment: .top)
                    .focusSection()
            }
        #else
        scroll
            .refreshable { refresh += 1 }
            .toolbar { ToolbarItem { filterMenu } }
        #endif
    }

    private var scroll: some View {
        ScrollView {
            content.padding(.vertical)
        }
        .task(id: "\(category)|\(query)|\(sortRaw)|\(filtersRaw)|\(refresh)") { await reload() }
        .task(id: text) {
            // Search as they type, once they pause.
            try? await Task.sleep(for: .milliseconds(300))
            if !Task.isCancelled { query = text.trimmingCharacters(in: .whitespaces) }
        }
        // Back from a page or its player: resume points or watched state may have changed.
        .onAppear { Task { await reloadResume() } }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 20) {
            controls.padding(.horizontal, sidePadding)
            if filters.isActive { activeFilters.padding(.horizontal, sidePadding) }
            if category.isEmpty, query.isEmpty, !filters.isActive {
                if shows, !resumeShows.isEmpty {
                    ShelfRow(title: "Continue Watching") {
                        ForEach(resumeShows, id: \.id) { s in
                            VodSeriesTile(series: s, width: tileWidth, onRemove: s.next.map { e in { removeShow(s, episodeId: e.id) } })
                        }
                    }
                    .accessibilityIdentifier("vod.continue")
                } else if !shows, !resume.isEmpty {
                    ShelfRow(title: "Continue Watching") {
                        ForEach(resume, id: \.id) { m in VodTile(movie: m, width: tileWidth, onRemove: { removeMovie(m) }) }
                    }
                    .accessibilityIdentifier("vod.continue")
                }
            }
            VStack(alignment: .leading, spacing: 12) {
                if (!category.isEmpty || !query.isEmpty || filters.isActive), let total {
                    Text("\(total) \(total == 1 ? noun.one : noun.many)").font(.callout).foregroundStyle(.secondary)
                        .accessibilityIdentifier("vod.total")
                }
                if let error { ErrorBanner(message: error) }
                if count == 0 {
                    if loading { ProgressView().frame(maxWidth: .infinity) }
                    else if error == nil && total != nil {
                        Text(!query.isEmpty ? "No \(noun.many) match “\(query)”." : filters.isActive ? "Nothing matches these filters." : "No \(noun.many) here yet.")
                            .foregroundStyle(.secondary)
                    }
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: tileWidth, maximum: tileWidth * 1.4), spacing: gridSpacing, alignment: .top)],
                              alignment: .leading, spacing: gridSpacing) {
                        if shows {
                            ForEach(series, id: \.id) { s in
                                VodSeriesTile(series: s, width: tileWidth)
                                    .onAppear { if s.id == series.last?.id { Task { await loadMore() } } }
                            }
                        } else {
                            ForEach(movies, id: \.id) { m in
                                VodTile(movie: m, width: tileWidth)
                                    .onAppear { if m.id == movies.last?.id { Task { await loadMore() } } }
                            }
                        }
                    }
                    if loading { ProgressView().frame(maxWidth: .infinity) }
                }
            }
            .padding(.horizontal, sidePadding)
            if isAdmin, let status { adminStatus(status).padding(.horizontal, sidePadding) }
        }
    }

    private var controls: some View {
        HStack(spacing: 12) {
            Menu {
                Picker("Category", selection: $category) {
                    Text(shows ? "All Shows" : "All Movies").tag("")
                    ForEach(categories, id: \.name) { c in Text("\(c.name) (\(c.count))").tag(c.name) }
                }
            } label: {
                Label(category.isEmpty ? (shows ? "All Shows" : "All Movies") : category, systemImage: "square.grid.2x2").lineLimit(1)
            }
            .accessibilityIdentifier("vod.category")
            TextField(shows ? "Search shows" : "Search movies", text: $text)
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

    // MARK: Filters

    /// One of the filters, read from and written back to the stored ones.
    private func binding<T>(_ path: WritableKeyPath<VodFilters, T>) -> Binding<T> {
        Binding(get: { filters[keyPath: path] },
                set: { v in
                    var f = filters
                    f[keyPath: path] = v
                    filtersRaw = f.isActive ? f.stored : ""
                })
    }

    /// Watched state, genre, decade, minimum rating and source, like the library's filter menu.
    private var filterMenu: some View {
        Menu {
            Picker("Watched", selection: binding(\.watched)) {
                Text("Watched or not").tag(VodFilters.Watched?.none)
                ForEach(VodFilters.Watched.allCases, id: \.self) { w in
                    Text(VodFilters.label(w, shows: shows)).tag(VodFilters.Watched?.some(w))
                }
            }
            .pickerStyle(.menu)
            if !genres.isEmpty {
                Picker("Genre", selection: binding(\.genre)) {
                    Text("Any genre").tag(String?.none)
                    ForEach(genres, id: \.name) { g in Text("\(g.name) (\(g.count))").tag(String?.some(g.name)) }
                }
                .pickerStyle(.menu)
            }
            Picker("Decade", selection: binding(\.decade)) {
                Text("Any decade").tag(Int?.none)
                ForEach(VodFilters.decades, id: \.self) { d in Text("\(String(d))s").tag(Int?.some(d)) }
            }
            .pickerStyle(.menu)
            Picker("Rating", selection: binding(\.minRating)) {
                Text("Any rating").tag(Int?.none)
                ForEach(VodFilters.ratings, id: \.self) { r in Text("Rated \(r)+").tag(Int?.some(r)) }
            }
            .pickerStyle(.menu)
            if sources.count > 1 {
                Picker("Source", selection: binding(\.sourceId)) {
                    Text("All sources").tag(String?.none)
                    ForEach(sources, id: \.id) { s in Text(s.name).tag(String?.some(s.id)) }
                }
                .pickerStyle(.menu)
            }
            if filters.isActive {
                Button("Clear Filters", systemImage: "xmark.circle", role: .destructive) { filtersRaw = "" }
            }
        } label: {
            Label("Filter", systemImage: filters.isActive ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
        }
        .accessibilityLabel("Filter")
        .accessibilityValue(filters.isActive ? "On" : "Off")
        .accessibilityIdentifier("vod.filter")
    }

    /// The filters in use, each removable, and Clear All.
    private var activeFilters: some View {
        let f = filters
        var chips: [(String, () -> Void)] = []
        if let w = f.watched { chips.append((VodFilters.label(w, shows: shows), { binding(\.watched).wrappedValue = nil })) }
        if let g = f.genre { chips.append((g, { binding(\.genre).wrappedValue = nil })) }
        if let d = f.decade { chips.append(("\(String(d))s", { binding(\.decade).wrappedValue = nil })) }
        if let r = f.minRating { chips.append(("Rated \(r)+", { binding(\.minRating).wrappedValue = nil })) }
        if let s = f.sourceId { chips.append((sources.first { $0.id == s }?.name ?? "Source", { binding(\.sourceId).wrappedValue = nil })) }
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 10) {
                ForEach(Array(chips.enumerated()), id: \.offset) { _, chip in
                    Button(action: chip.1) {
                        HStack(spacing: 6) {
                            Text(chip.0).lineLimit(1)
                            Image(systemName: "xmark").font(.caption2.weight(.bold))
                        }
                        .font(isTV ? .callout : .footnote)
                    }
                    #if os(iOS)
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.capsule)
                    .tint(Color.marqueeGold)
                    #endif
                    .accessibilityLabel("Remove filter \(chip.0)")
                    .accessibilityIdentifier("vod.filterChip")
                }
                Button("Clear All") { filtersRaw = "" }
                    #if os(iOS)
                    .buttonStyle(.borderless)
                    .font(.footnote)
                    #endif
                    .accessibilityIdentifier("vod.clearFilters")
            }
            #if os(tvOS)
            .padding(.vertical, 12)
            #endif
        }
        #if os(tvOS)
        .scrollClipDisabled()
        #endif
    }

    /// For admins: what the sources loaded, and a way to load them again now.
    private func adminStatus(_ s: VodStatus) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                let n = shows ? s.series : s.movies
                Text("\(n) \(n == 1 ? noun.one : noun.many) from \(sources.count) \(sources.count == 1 ? "source" : "sources")\(s.matching ? " · matching details…" : "")")
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

    // MARK: Loading

    private func reload() async {
        loading = true
        error = nil
        async let cats = try? (shows ? app.vodSeriesCategories() : app.vodCategories())
        async let gens = try? (shows ? app.vodSeriesGenres() : app.vodGenres())
        async let st = try? app.vodStatus()
        await reloadResume()
        do {
            try await fetch(offset: 0, replace: true)
        } catch is CancellationError {
        } catch {
            if !Task.isCancelled { self.error = error.localizedDescription }
        }
        if let c = await cats {
            categories = c
            // A category that went away (the source changed): back to all.
            if !category.isEmpty, !c.contains(where: { $0.name == category }) { category = "" }
        }
        if let g = await gens { genres = g }
        if let s = await st { status = s }
        loading = false
    }

    private func fetch(offset: Int, replace: Bool) async throws {
        let cat = category.isEmpty ? nil : category, q = query.isEmpty ? nil : query
        if shows {
            let page = try await app.vodSeriesList(category: cat, query: q, sort: sort, filters: filters, offset: offset)
            if replace {
                series = page.series
            } else {
                let have = Set(series.map(\.id))
                series += page.series.filter { !have.contains($0.id) }
            }
            total = page.total
        } else {
            let page = try await app.vodMovies(category: cat, query: q, sort: sort, filters: filters, offset: offset)
            if replace {
                movies = page.movies
            } else {
                let have = Set(movies.map(\.id))
                movies += page.movies.filter { !have.contains($0.id) }
            }
            total = page.total
        }
    }

    private func reloadResume() async {
        if shows {
            if let r = try? await app.vodSeriesContinueWatching() { resumeShows = r }
        } else {
            if let r = try? await app.vodContinueWatching() { resume = r }
        }
    }

    /// Takes a film off Continue Watching: gone from the row at once, back if the server says no;
    /// then this row and Home's come from the server again.
    private func removeMovie(_ m: VodMovie) {
        let before = resume
        resume.removeAll { $0.id == m.id }
        ActionError.run {
            do {
                try await app.removeVodMovieFromContinueWatching(m.id)
            } catch {
                resume = before
                throw error
            }
            HomeChanges.shared.version += 1
            await reloadResume()
        }
    }

    /// Takes a show off Continue Watching (by its next episode, which hides the whole show until
    /// it's watched again), as for a film.
    private func removeShow(_ s: VodSeries, episodeId: Int64) {
        let before = resumeShows
        resumeShows.removeAll { $0.id == s.id }
        ActionError.run {
            do {
                try await app.removeVodEpisodeFromContinueWatching(episodeId)
            } catch {
                resumeShows = before
                throw error
            }
            HomeChanges.shared.version += 1
            await reloadResume()
        }
    }

    /// The next page, as the end of the grid comes into view.
    private func loadMore() async {
        guard !loading, let total, count < total else { return }
        loading = true
        defer { loading = false }
        try? await fetch(offset: count, replace: false)
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
    /// Takes it off Continue Watching (its context menu, in the Continue Watching row).
    var onRemove: (() -> Void)?

    var body: some View {
        #if os(tvOS)
        VStack(alignment: .leading, spacing: 14) {
            NavigationLink(value: Route.vodMovie(movie.id)) { VodPoster(movie: movie, width: width) }
                .buttonStyle(.card)
                .accessibilityLabel(movie.title)
                .accessibilityIdentifier("vod.tile")
                .modifier(RemoveFromContinueMenu(action: onRemove))
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
        .modifier(RemoveFromContinueMenu(action: onRemove))
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
    let movie: VodMovie
    var width: CGFloat = 130

    var body: some View {
        VodArt(title: movie.title, path: movie.posterUrl, watched: movie.watched, progress: movie.progress, width: width)
    }
}

/// VOD artwork (a poster, or a 16:9 still when `aspect` says so) with a watched tick and a
/// progress bar, over a coloured placeholder with the title until it loads.
struct VodArt: View {
    @Environment(AppSession.self) private var app
    let title: String
    let path: String?
    var watched = false
    /// 0…1, shown as a bar along the bottom.
    var progress: Double?
    var width: CGFloat = 130
    /// Height over width (1.5 for posters).
    var aspect: CGFloat = 1.5

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            placeholder
            CachedImage(url: app.vodImageURL(path)) { phase in
                if let image = phase.image { image.resizable().scaledToFill() }
            }
        }
        .frame(width: width, height: width * aspect)
        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
        .overlay(alignment: .topTrailing) {
            if watched {
                Image(systemName: "checkmark.circle.fill").symbolRenderingMode(.palette).foregroundStyle(.black, Color.marqueeGold).padding(6)
                    .accessibilityLabel("Watched")
            }
        }
        .overlay(alignment: .bottom) {
            if let p = progress {
                GeometryReader { g in
                    ZStack(alignment: .leading) {
                        Rectangle().fill(.black.opacity(0.6))
                        Rectangle().fill(Color.marqueeGold).frame(width: g.size.width * p)
                    }
                }
                .frame(height: 4)
                .clipShape(UnevenRoundedRectangle(bottomLeadingRadius: 8, bottomTrailingRadius: 8))
            }
        }
    }

    private var placeholder: some View {
        let hash = title.unicodeScalars.reduce(UInt32(5381)) { ($0 &<< 5) &+ $0 &+ $1.value }
        let hue = Double(hash % 360) / 360
        return LinearGradient(colors: [Color(hue: hue, saturation: 0.35, brightness: 0.3), Color(hue: hue, saturation: 0.3, brightness: 0.12)],
                              startPoint: .topLeading, endPoint: .bottomTrailing)
            .overlay(alignment: .bottomLeading) {
                Text(title).font(.caption.weight(.semibold)).foregroundStyle(.white.opacity(0.85)).lineLimit(3).padding(8)
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
            VodPlayerView(target: t)
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

/// Something on demand to play: a film or a show's episode.
enum VodPlayable: Hashable {
    case movie(VodMovieDetail)
    case episode(VodEpisode)

    var key: String {
        switch self {
        case .movie(let m): "movie-\(m.id)"
        case .episode(let e): "episode-\(e.id)"
        }
    }

    var title: String {
        switch self {
        case .movie(let m): m.title
        case .episode(let e): "\(e.seriesTitle) · \(e.label)"
        }
    }

    var subtitle: String {
        switch self {
        case .movie(let m): m.base.metaLine
        case .episode(let e): [e.title, (e.runtimeMs ?? 0) >= 60_000 ? formatDuration(ms: e.runtimeMs) : ""].filter { !$0.isEmpty }.joined(separator: " · ")
        }
    }

    var overview: String? {
        switch self {
        case .movie(let m): m.info.overview
        case .episode(let e): e.overview
        }
    }

    var isEpisode: Bool { if case .episode = self { true } else { false } }
}

/// What to play, from a position (nil = its resume point).
struct VodPlayTarget: Identifiable {
    let item: VodPlayable
    let startMs: Int64?
    var id: String { item.key }

    init(movie: VodMovieDetail, startMs: Int64?) {
        item = .movie(movie)
        self.startMs = startMs
    }

    init(episode: VodEpisode, startMs: Int64?) {
        item = .episode(episode)
        self.startMs = startMs
    }
}

/// Full-screen playback of a film or an episode: the source's HLS through the server (or its
/// file made into HLS), seekable, in AVKit's player. Its own player, not the library's: no
/// trailers, rating request or watch together on demand. Episodes end with Up Next: a
/// countdown to the next one in viewing order (the server's), as library shows do.
struct VodPlayerView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss
    /// What's playing (an episode's Up Next replaces it in place).
    @State private var current: VodPlayable
    /// nil: from the viewer's resume point.
    @State private var startMs: Int64?
    @State private var player = AVPlayer()
    @State private var session: String?
    @State private var error: String?
    @State private var loading = true
    @State private var closing = false
    @State private var observers: [NSObjectProtocol] = []
    @State private var rateWatch: NSKeyValueObservation?
    /// The episode after this one, fetched as it starts.
    @State private var next: VodEpisode?
    @State private var countdown: Int?
    #if os(iOS)
    @State private var chrome = PlayerChrome()
    #endif

    init(movie: VodMovieDetail, startMs: Int64?) {
        _current = State(initialValue: .movie(movie))
        _startMs = State(initialValue: startMs)
    }

    init(target: VodPlayTarget) {
        _current = State(initialValue: target.item)
        _startMs = State(initialValue: target.startMs)
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            // On the TV the player's transport bar keeps focus: it goes while Up Next is up,
            // so the remote reaches Play Now and Cancel.
            if !(isTV && countdown != nil) {
                VodPlayerController(player: player, chrome: chromeModel)
                    .ignoresSafeArea()
                    .accessibilityIdentifier("vod.player")
            }
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
                if let next, let countdown {
                    HStack {
                        Spacer()
                        UpNextCard(title: "\(next.label) · \(next.title)", seconds: countdown, play: playNext,
                                   cancel: { self.countdown = nil; close() })
                    }
                    .padding(.trailing, 40).padding(.bottom, 60)
                }
            }
        }
        .task(id: current.key) { await start() }
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
                Text(current.title).font(.headline).lineLimit(1)
                if !current.subtitle.isEmpty { Text(current.subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
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
        error = nil
        loading = true
        next = nil
        do {
            let s: (id: String, url: URL, startMs: Int64)
            switch current {
            case .movie(let m): s = try await app.playVod(m.id)
            case .episode(let e):
                s = try await app.playVodEpisode(e.id, startMs: startMs)
                Task { next = try? await app.vodNextEpisode(e.id) }
            }
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
            if item.status == .failed { throw MarqueeError(item.error?.localizedDescription ?? "This couldn't be played") }
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

    /// Reports on pause; at the end, Up Next for an episode with one after it, else back to the page.
    private func watch(_ item: AVPlayerItem) {
        let end = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: item, queue: .main) { _ in
            Task { @MainActor in ended() }
        }
        let failed = NotificationCenter.default.addObserver(forName: AVPlayerItem.failedToPlayToEndTimeNotification, object: item, queue: .main) { _ in
            Task { @MainActor in error = current.isEpisode ? "The episode stopped playing." : "The movie stopped playing." }
        }
        observers = [end, failed]
        rateWatch = player.observe(\.timeControlStatus, options: [.new]) { p, _ in
            let paused = p.timeControlStatus == .paused
            Task { @MainActor in
                if paused { await report() }
            }
        }
    }

    private func ended() {
        guard current.isEpisode, next != nil else { close(); return }
        endSession() // counts the episode watched and ends its session
        startCountdown()
    }

    private func startCountdown() {
        countdown = 10
        Task {
            while let c = countdown, c > 0 {
                try? await Task.sleep(for: .seconds(1))
                if let now = countdown { countdown = now - 1 }
            }
            if countdown == 0 { playNext() }
        }
    }

    /// The next episode, in this player, from its start.
    private func playNext() {
        guard let next, !closing else { return }
        countdown = nil
        endSession()
        startMs = 0
        current = .episode(next)
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
        countdown = nil
        finish()
        dismiss()
    }

    private func finish() {
        guard !closing else { return }
        closing = true
        endSession()
    }

    /// Records where they stopped, then ends the session (the position is reported against it).
    private func endSession() {
        rateWatch = nil
        observers.forEach(NotificationCenter.default.removeObserver)
        observers = []
        let id = session
        session = nil
        let atEnd = player.currentItem.map { item in
            let d = item.duration.seconds
            return d.isFinite && d > 0 && position >= d - 1
        } ?? false
        let pos = atEnd ? (durationMs ?? Int64(position * 1000)) : Int64(max(0, position.isFinite ? position : 0) * 1000)
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
        title.value = current.title as NSString
        var out: [AVMetadataItem] = [title]
        if let o = current.overview ?? (current.isEpisode ? current.subtitle : nil) {
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
