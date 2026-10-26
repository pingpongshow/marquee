import MarqueeKit
import SwiftUI

// Shows on demand (LIVE-6): series from Xtream Codes sources. Their tiles on the VOD Shows page
// and a show's page (backdrop, poster, details, the next episode to play, a season picker and
// its episodes, each markable watched). Episodes play in VodPlayerView, with Up Next.

/// A show's poster with how far through it the viewer is, linking to its page. In Continue
/// Watching the line under it names the next episode.
struct VodSeriesTile: View {
    let series: VodSeries
    var width: CGFloat = 130
    /// Takes the show off Continue Watching (its context menu, in the Continue Watching row).
    var onRemove: (() -> Void)?
    /// Marks its next episode (the one part-way through) watched or unwatched (its context
    /// menu, in the Continue Watching row).
    var onMark: (@MainActor (Bool) async throws -> Void)?

    var body: some View {
        #if os(tvOS)
        VStack(alignment: .leading, spacing: 14) {
            NavigationLink(value: Route.vodSeries(series.id)) { poster }
                .buttonStyle(.card)
                .accessibilityLabel(series.title)
                .accessibilityValue(series.inLibrary == true ? VodInLibraryBadge.text : "")
                .accessibilityIdentifier("vod.showTile")
                .modifier(RemoveFromContinueMenu(action: onRemove, mark: onMark))
            titles
        }
        .frame(width: width)
        #else
        NavigationLink(value: Route.vodSeries(series.id)) {
            VStack(alignment: .leading, spacing: 6) {
                poster
                titles
            }
            .frame(width: width)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(series.title)
        .accessibilityValue(series.inLibrary == true ? VodInLibraryBadge.text : "")
        .accessibilityAddTraits(.isButton)
        .accessibilityIdentifier("vod.showTile")
        .modifier(RemoveFromContinueMenu(action: onRemove, mark: onMark))
        #endif
    }

    private var poster: some View {
        VodArt(title: series.title, path: series.posterUrl, watched: series.watched, progress: series.progress, width: width,
               inLibrary: series.inLibrary == true)
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(series.title).font(.subheadline.weight(.medium)).lineLimit(1)
            Text(series.tileLine).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }
    }
}

extension VodSeries {
    /// The next episode ("S1 · E3 · resume"), else "2004 · 3 seasons".
    var tileLine: String {
        if let n = next { return n.label + ((n.positionMs ?? 0) > 0 ? " · resume" : "") }
        return metaLine
    }

    var metaLine: String {
        [year.map(String.init), seasonCount > 0 ? "\(seasonCount) \(seasonCount == 1 ? "season" : "seasons")" : nil]
            .compactMap { $0 }.joined(separator: " · ")
    }

    /// Episodes watched, as a bar, once started and until finished.
    var progress: Double? {
        guard !watched, episodeCount > 0, watchedCount > 0 else { return nil }
        return min(1, max(0.02, Double(watchedCount) / Double(episodeCount)))
    }
}

extension VodEpisode {
    /// How far through the viewer is (for the resume bar), when part-way.
    var progress: Double? {
        guard !watched, let pos = positionMs, pos > 0, let total = runtimeMs, total > 0 else { return nil }
        return min(1, max(0.02, Double(pos) / Double(total)))
    }
}

/// A show's page: backdrop, poster, details, Play or Resume the next episode, Watched, the
/// season picker with Mark Season Watched, and the season's episodes.
struct VodSeriesView: View {
    @Environment(AppSession.self) private var app
    let id: Int64
    @State private var show: VodSeriesDetail?
    @State private var error: String?
    @State private var playing: VodPlayTarget?
    /// The season picked (nil = the next episode's, else the first).
    @State private var season: Int?
    /// Watched changes shown at once, before the server confirms (by episode id; the show's under 0).
    @State private var pending: [Int64: Bool] = [:]
    /// The watchlist, shown at once when changed, before the server confirms.
    @State private var watchlisted: Bool?

    var body: some View {
        Group {
            if let s = show {
                page(s)
            } else if let error {
                ContentUnavailableView {
                    Label("Couldn't load the show", systemImage: "exclamationmark.triangle")
                } description: { Text(error) } actions: {
                    Button("Try Again") { Task { await load() } }
                }
            } else {
                // The first open fetches the episodes from the source.
                VStack(spacing: 12) {
                    ProgressView()
                    Text("Loading the episodes…").font(.callout).foregroundStyle(.secondary)
                }
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

    private var seasonKey: String { "marquee.vod.shows.season.\(id)" }

    /// The season shown: the one picked if the show still has it, else the next episode's, else the first.
    private func current(_ s: VodSeriesDetail) -> VodSeason? {
        let seasons = s.info.seasons
        return seasons.first { $0.season == season } ?? seasons.first { $0.season == s.base.next?.season } ?? seasons.first
    }

    private func page(_ s: VodSeriesDetail) -> some View {
        let cur = current(s)
        let episodes = s.info.episodes.filter { $0.season == cur?.season }
        return ScrollView {
            ZStack(alignment: .top) {
                backdrop(s)
                VStack(alignment: .leading, spacing: 24) {
                    header(s)
                    if let o = s.info.overview, !o.isEmpty { Text(o).font(.body).frame(maxWidth: 900, alignment: .leading) }
                    if let e = s.info.episodesError, s.info.episodes.isEmpty {
                        ErrorBanner(message: "Couldn't get the episodes: \(e)")
                    }
                    if let cur {
                        seasonBar(s, current: cur)
                        LazyVStack(alignment: .leading, spacing: isTV ? 24 : 14) {
                            ForEach(episodes, id: \.id) { e in episodeRow(e) }
                        }
                    }
                }
                .padding(.horizontal, sidePadding)
                .padding(.top, isTV ? 60 : 24)
                .padding(.bottom, 40)
            }
        }
        .navigationTitle(isTV ? "" : s.title)
    }

    @ViewBuilder private func backdrop(_ s: VodSeriesDetail) -> some View {
        if let url = app.vodImageURL(s.info.backdropUrl) {
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

    private func header(_ s: VodSeriesDetail) -> some View {
        let poster = VodArt(title: s.title, path: s.base.posterUrl, watched: s.base.watched, width: isTV ? 300 : 130)
        let details = VStack(alignment: .leading, spacing: 10) {
            if let c = s.base.category { Text(c).font(.caption).foregroundStyle(.secondary) }
            Text(s.title).font(isTV ? .title2.bold() : .title.bold()).fixedSize(horizontal: false, vertical: true)
            if s.base.inLibrary == true { VodInLibraryBadge() }
            let meta = [s.base.year.map(String.init), s.base.contentRating,
                        s.base.seasonCount > 0 ? "\(s.base.seasonCount) \(s.base.seasonCount == 1 ? "season" : "seasons")" : nil]
                .compactMap { $0 }.filter { !$0.isEmpty }
            if !meta.isEmpty { Text(meta.joined(separator: " · ")).font(.callout).foregroundStyle(.secondary) }
            if !s.info.genres.isEmpty { Text(s.info.genres.joined(separator: ", ")).font(.footnote).foregroundStyle(.secondary) }
            if let r = s.base.rating {
                HStack(spacing: 6) {
                    Text(s.info.matched ? "TMDB" : "Rating").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    Text(String(format: "%.1f", r)).font(.callout.weight(.semibold))
                }
                .padding(.horizontal, 8).padding(.vertical, 4)
                .background(Color.secondary.opacity(0.18), in: RoundedRectangle(cornerRadius: 6))
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Rating \(String(format: "%.1f", r))")
            }
            actions(s).padding(.top, 6)
            VodFollowControl(show: s) // new episodes saved automatically
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

    private func actions(_ s: VodSeriesDetail) -> some View {
        let next = s.base.next
        let resumeMs = next?.positionMs ?? 0
        let watched = pending[0] ?? s.base.watched
        // As a library title's: labelled while they fit, else the secondary ones icon-only.
        return HeaderActions {
            if let next {
                Button { playing = VodPlayTarget(episode: next, startMs: nil) } label: {
                    Label(resumeMs > 0 ? "Resume \(next.label) from \(formatTime(seconds: Double(resumeMs) / 1000))"
                                       : "\(s.base.watchedCount > 0 ? "Play" : "Start") \(next.label)", systemImage: "play.fill")
                }
                .buttonStyle(.borderedProminent)
                .accessibilityIdentifier("vod.show.play")
                if resumeMs > 0 {
                    Button { playing = VodPlayTarget(episode: next, startMs: 0) } label: {
                        Label("Play from Start", systemImage: "arrow.counterclockwise")
                    }
                    .buttonStyle(.bordered)
                    .accessibilityIdentifier("vod.show.fromStart")
                }
            }
        } secondary: {
            let onList = watchlisted ?? s.info.watchlisted ?? false
            Button { toggleWatchlist(s, now: !onList) } label: {
                Label(onList ? "On Watchlist" : "Watchlist", systemImage: onList ? "bookmark.fill" : "bookmark")
            }
            .buttonStyle(.bordered)
            .accessibilityLabel(onList ? "On Watchlist" : "Watchlist")
            .accessibilityIdentifier("vod.show.watchlist")
            if s.base.episodeCount > 0 {
                Button { setWatched(key: 0, now: !watched) { try await app.setVodSeriesWatched(s.id, !watched) } } label: {
                    Label(watched ? "Watched" : "Mark watched", systemImage: watched ? "checkmark.circle.fill" : "checkmark.circle")
                }
                .buttonStyle(.bordered)
                .accessibilityIdentifier("vod.show.watched")
            }
        }
        #if os(iOS)
        .fixedSize(horizontal: false, vertical: true)
        #endif
    }

    /// The season picker and Mark Season Watched.
    private func seasonBar(_ s: VodSeriesDetail, current cur: VodSeason) -> some View {
        let done = pending[-Int64(cur.season) - 1] ?? (cur.episodeCount > 0 && cur.watchedCount >= cur.episodeCount)
        let markSeason = Button {
            setWatched(key: -Int64(cur.season) - 1, now: !done) { try await app.setVodSeriesWatched(s.id, season: cur.season, !done) }
        } label: {
            Label(done ? "Season watched" : "Mark season watched", systemImage: done ? "checkmark.circle.fill" : "checkmark.circle")
        }
        .accessibilityIdentifier("vod.season.watched")
        return Group {
            #if os(tvOS)
            VStack(alignment: .leading, spacing: 20) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 20) {
                        ForEach(s.info.seasons, id: \.season) { z in
                            Button { pick(z.season) } label: {
                                if z.season == cur.season {
                                    Label(z.name, systemImage: "checkmark").fontWeight(.bold)
                                } else {
                                    Text(z.name)
                                }
                            }
                            .accessibilityAddTraits(z.season == cur.season ? .isSelected : [])
                            .accessibilityIdentifier("vod.season")
                        }
                        markSeason
                    }
                    .padding(.vertical, 20)
                }
                .scrollClipDisabled()
                .focusSection()
                Text("\(cur.episodeCount) \(cur.episodeCount == 1 ? "episode" : "episodes")").font(.callout).foregroundStyle(.secondary)
            }
            #else
            HStack(spacing: 12) {
                Menu {
                    Picker("Season", selection: Binding(get: { cur.season }, set: { pick($0) })) {
                        ForEach(s.info.seasons, id: \.season) { z in Text(z.name).tag(z.season) }
                    }
                } label: {
                    HStack(spacing: 4) {
                        Text(cur.name).font(.title3.bold())
                        Image(systemName: "chevron.up.chevron.down").font(.footnote.bold())
                    }
                }
                .accessibilityIdentifier("vod.season")
                Text("\(cur.episodeCount) \(cur.episodeCount == 1 ? "episode" : "episodes")").font(.footnote).foregroundStyle(.secondary)
                Spacer()
                markSeason.buttonStyle(.bordered).font(.footnote)
            }
            #endif
        }
    }

    private func episodeRow(_ e: VodEpisode) -> some View {
        let watched = pending[e.id] ?? e.watched
        let still = VodArt(title: e.title, path: e.stillUrl, watched: false, progress: e.progress,
                           width: isTV ? 320 : 150, aspect: 9.0 / 16.0)
        let meta = [e.airDate, (e.runtimeMs ?? 0) >= 60_000 ? formatDuration(ms: e.runtimeMs) : nil].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
        let text = VStack(alignment: .leading, spacing: 4) {
            Text("\(e.episode). \(e.title)").font(isTV ? .headline : .subheadline.weight(.semibold)).lineLimit(2)
            if !meta.isEmpty { Text(meta).font(.caption).foregroundStyle(.secondary) }
            if let o = e.overview, !o.isEmpty { Text(o).font(.caption).foregroundStyle(.secondary).lineLimit(isTV ? 3 : 2) }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        let toggle = Button { setWatched(key: e.id, now: !watched) { try await app.setVodEpisodeWatched(e.id, !watched) } } label: {
            Image(systemName: watched ? "checkmark.circle.fill" : "checkmark.circle")
                .font(isTV ? .title3 : .title3)
                .foregroundStyle(watched ? Color.marqueeGold : .secondary)
        }
        .accessibilityLabel(watched ? "Mark unwatched" : "Mark watched")
        .accessibilityValue(watched ? "Watched" : "Not watched")
        .accessibilityIdentifier("vod.episode.watched")
        #if os(tvOS)
        return HStack(alignment: .center, spacing: 30) {
            Button { playing = VodPlayTarget(episode: e, startMs: nil) } label: {
                HStack(alignment: .top, spacing: 24) { still; text }.padding(16)
            }
            .buttonStyle(.card)
            .accessibilityLabel("\(e.label) \(e.title)")
            .accessibilityIdentifier("vod.episode")
            .contextMenu { episodeMarks(e, watched: watched) }
            toggle
        }
        #else
        return HStack(alignment: .center, spacing: 12) {
            Button { playing = VodPlayTarget(episode: e, startMs: nil) } label: {
                HStack(alignment: .top, spacing: 12) { still; text }.contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("\(e.label) \(e.title)")
            .accessibilityAddTraits(.isButton)
            .accessibilityIdentifier("vod.episode")
            .contextMenu {
                Button("Play", systemImage: "play.fill") { playing = VodPlayTarget(episode: e, startMs: nil) }
                if (e.positionMs ?? 0) > 0 {
                    Button("Play from Start", systemImage: "arrow.counterclockwise") { playing = VodPlayTarget(episode: e, startMs: 0) }
                }
                episodeMarks(e, watched: watched)
            }
            toggle.buttonStyle(.plain)
        }
        #endif
    }

    /// An episode's Mark Watched (unless watched with nothing part-way through) and Mark
    /// Unwatched (when watched or part-way through) for its menu: both when it's in progress.
    @ViewBuilder private func episodeMarks(_ e: VodEpisode, watched: Bool) -> some View {
        let started = pending[e.id] == nil && (e.positionMs ?? 0) > 0
        if !watched || started {
            Button("Mark Watched", systemImage: "checkmark.circle") { setWatched(key: e.id, now: true) { try await app.setVodEpisodeWatched(e.id, true) } }
        }
        if watched || started {
            Button("Mark Unwatched", systemImage: "circle") { setWatched(key: e.id, now: false) { try await app.setVodEpisodeWatched(e.id, false) } }
        }
    }

    private func pick(_ s: Int) {
        season = s
        UserDefaults.standard.set(s, forKey: seasonKey)
    }

    /// Shows the change at once, saves it, then reloads the show (counts and next episode).
    /// The watchlist (USER-8): shows at once, back as it was if the server refuses.
    private func toggleWatchlist(_ s: VodSeriesDetail, now: Bool) {
        watchlisted = now
        Task {
            do {
                try await app.setVodSeriesWatchlisted(s.id, now)
                HomeChanges.shared.version += 1 // the Your Watchlist row
                await load()
                watchlisted = nil
            } catch {
                watchlisted = !now
                ActionError.shared.message = error.localizedDescription
            }
        }
    }

    private func setWatched(key: Int64, now: Bool, _ save: @escaping () async throws -> Void) {
        pending[key] = now
        Task {
            do {
                try await save()
                HomeChanges.shared.version += 1 // Continue Watching changes either way
                await load()
            } catch {
                ActionError.shared.message = error.localizedDescription
            }
            pending[key] = nil
        }
    }

    private func load() async {
        if season == nil, UserDefaults.standard.object(forKey: seasonKey) != nil {
            season = UserDefaults.standard.integer(forKey: seasonKey)
        }
        do {
            show = try await app.vodSeries(id)
            error = nil
        } catch is CancellationError {
        } catch {
            if show == nil { self.error = error.localizedDescription }
        }
    }
}
