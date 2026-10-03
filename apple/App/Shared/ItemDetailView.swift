import MarqueeKit
import SwiftUI

/// Detail page for any item: header, play buttons, contents, cast and related titles.
struct ItemDetailView: View {
    @Environment(AppSession.self) private var app
    @Environment(VideoPresenter.self) private var video
    @Environment(MusicPlayer.self) private var music
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif
    let id: Int64
    @State private var detail: ItemDetail?
    @State private var children: [Item] = []
    @State private var popular: [Item] = []
    @State private var allPopular = false
    @State private var related: [Item] = []
    @State private var soundsLike: [Item] = []
    @State private var series: [(collection: Item, members: [Item])] = []
    @State private var watchlisted = false
    @State private var error: String?
    @State private var audioID: Int64?
    @State private var subtitleID: Int64?
    @State private var fileID: Int64?
    @State private var findingSubs = false
    /// A Play, Shuffle or Radio that failed, shown as an alert.
    @State private var actionError: String?

    var body: some View {
        ScrollView {
            if let d = detail {
                VStack(alignment: .leading, spacing: 28) {
                    header(d)
                    contents(d)
                    if let extras = d.info.extras, !extras.isEmpty {
                        ShelfRow(title: "Extras") {
                            ForEach(extras, id: \.id) { x in ExtraCard(extra: x) { video.play(x.id) } }
                        }
                    }
                    cast(d)
                    ForEach(series, id: \.collection.id) { s in
                        ShelfRow(title: s.collection.title, destination: .item(s.collection.id)) {
                            ForEach(s.members, id: \.id) { PosterCard(item: $0) }
                        }
                    }
                    if !soundsLike.isEmpty {
                        ShelfRow(title: "Sounds like this") {
                            ForEach(soundsLike, id: \.id) { PosterCard(item: $0) }
                        }
                    }
                    if !related.isEmpty {
                        ShelfRow(title: d.type == .artist || d.type == .album ? "Similar in your library" : "More like this") {
                            ForEach(related, id: \.id) { PosterCard(item: $0) }
                        }
                    }
                }
                .padding(.bottom, 40)
            } else if let error {
                ErrorBanner(message: error).padding()
            } else {
                ProgressView().padding(.top, 80)
            }
        }
        .background(alignment: .top) { backdrop }
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .navigationTitle(detail?.title ?? "")
        .task { await load() }
        .alert("Couldn't play", isPresented: Binding(get: { actionError != nil }, set: { if !$0 { actionError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(actionError ?? "")
        }
        .onChange(of: video.request) { if video.request == nil { Task { await load() } } }
    }

    private func load() async {
        do {
            let d = try await app.item(id)
            detail = d
            if d.base.childCount > 0 { children = try await app.children(id) }
            if [.movie, .show, .artist, .album].contains(d.type) { related = (try? await app.related(id)) ?? [] }
            if d.type == .artist { popular = (try? await app.popularTracks(id)) ?? [] }
            if d.type == .artist || d.type == .album { soundsLike = (try? await app.soundsLike(id, limit: 15)) ?? [] }
            watchlisted = d.base.watchlisted ?? false
            var found: [(collection: Item, members: [Item])] = []
            for c in d.info.collections ?? [] {
                let members = ((try? await app.children(c.id)) ?? []).filter { $0.id != id }
                if !members.isEmpty { found.append((c, members)) }
            }
            series = found
        } catch {
            self.error = error.localizedDescription
        }
    }

    // MARK: - Header

    @ViewBuilder private var backdrop: some View {
        if let art = detail?.base.images?.backdrop {
            AsyncImage(url: app.imageURL(art, width: 1280)) { phase in
                if let image = phase.image {
                    image.resizable().scaledToFill()
                        .frame(height: backdropHeight).clipped()
                        .overlay(LinearGradient(colors: [.clear, Color(white: 0.04)], startPoint: .center, endPoint: .bottom))
                        .opacity(0.45)
                }
            }
            .frame(height: backdropHeight)
            .ignoresSafeArea()
        }
    }

    private var compact: Bool {
        #if os(iOS)
        sizeClass == .compact
        #else
        false
        #endif
    }

    #if os(tvOS)
    private let backdropHeight: CGFloat = 700
    private let posterWidth: CGFloat = 340
    #else
    private let backdropHeight: CGFloat = 360
    private let posterWidth: CGFloat = 150
    #endif

    private func header(_ d: ItemDetail) -> some View {
        let shape: PosterShape = d.type == .episode ? .wide : (d.type == .album || d.type == .artist || d.type == .track) ? .square : .poster
        // iPhone portrait: music and collections stack the art above the title, so neither is
        // squeezed into a narrow column.
        let stacked = compact && [.album, .artist, .collection].contains(d.type)
        let layout = stacked ? AnyLayout(VStackLayout(alignment: .leading, spacing: 14)) : AnyLayout(HStackLayout(alignment: .bottom, spacing: 20))
        return VStack(alignment: .leading, spacing: 16) {
            layout {
                ArtworkView(item: d.base, shape: shape, width: shape == .wide ? posterWidth * 1.6 : posterWidth)
                    .frame(width: shape == .wide ? posterWidth * 1.6 : posterWidth)
                    .shadow(radius: 10)
                VStack(alignment: .leading, spacing: 6) {
                    if d.type == .episode, let show = d.base.grandparentTitle, let showID = d.base.grandparentId {
                        NavigationLink(value: Route.item(showID)) { Text(show).font(.headline).foregroundStyle(.secondary) }.buttonStyle(.plain)
                    }
                    Text(d.title).font(.largeTitle.bold()).lineLimit(3)
                    if d.info.smartRules != nil {
                        // Members follow rules set on the web (META-7).
                        Label("Smart collection", systemImage: "sparkles").font(.caption.weight(.semibold)).foregroundStyle(Color.marqueeGold)
                            .accessibilityIdentifier("smartCollection")
                    }
                    if let artist = d.base.artistCredit, d.type != .artist { Text(artist).font(.title3).foregroundStyle(.secondary) }
                    Text(metaLine(d)).font(.subheadline).foregroundStyle(.secondary)
                    if d.type == .album, music.showAudioQuality, let f = sharedAudioFormat {
                        // Every track has the same format: say it once (MUSIC-23).
                        Text(f.label).font(.caption.weight(.semibold)).foregroundStyle(.secondary).lineLimit(1)
                            .accessibilityIdentifier("albumQuality")
                    }
                    if !d.info.genres.isEmpty { Text(d.info.genres.joined(separator: ", ")).font(.caption).foregroundStyle(.tertiary) }
                    ratings(d)
                }
            }
            // Labels stay on one line; when they don't fit, the secondary actions go icon-only.
            HeaderActions { primaryButtons(d) } secondary: { secondaryButtons(d) }
                .accessibilityIdentifier("itemActions")
            if let tagline = d.info.tagline, !tagline.isEmpty { Text(tagline).italic().foregroundStyle(.secondary) }
            if let summary = d.info.summary, !summary.isEmpty {
                Text(summary).font(.body).foregroundStyle(.primary.opacity(0.9)).frame(maxWidth: 900, alignment: .leading)
            }
            if d.base.isPlayableVideo { trackPickers(d) }
        }
        .padding(.horizontal, sidePadding)
        .padding(.top, 24)
    }

    /// The album's format when all its tracks share it.
    private var sharedAudioFormat: AudioFormat? {
        let formats = children.filter { $0._type == .track }.map(\.audioFormat)
        guard let first = formats.first ?? nil, formats.allSatisfy({ $0?.shortLabel == first.shortLabel }) else { return nil }
        return first
    }

    private func metaLine(_ d: ItemDetail) -> String {
        var parts: [String] = []
        if d.type == .episode, let season = d.base.parentTitle { parts.append("\(season) · Episode \(d.base.index ?? 0)") }
        if let y = d.base.year { parts.append(String(y)) }
        if let r = d.info.contentRating, !r.isEmpty { parts.append(r) }
        let dur = formatDuration(ms: d.base.durationMs)
        if !dur.isEmpty, d.type != .show, d.type != .artist { parts.append(dur) }
        if d.type == .show || d.type == .artist || d.type == .season { parts.append(d.base.subtitle) }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder private func ratings(_ d: ItemDetail) -> some View {
        if let r = d.info.ratings, r.imdb != nil || r.rottenTomatoes != nil || r.metacritic != nil || r.anilist != nil {
            // Each chip stays on one line ("61%", never "61" over "%"); when the row is too narrow
            // for all of them, they stack.
            let chips = Group {
                if let imdb = r.imdb { Label(String(format: "%.1f", imdb), systemImage: "star.fill").foregroundStyle(.yellow) }
                if let rt = r.rottenTomatoes {
                    Label("\(rt)%", systemImage: "leaf.fill").foregroundStyle(rt >= 60 ? .red : .green).accessibilityIdentifier("rating.rottenTomatoes")
                }
                if let mc = r.metacritic { Text("MC \(mc)").foregroundStyle(.secondary) }
                if let al = r.anilist { Text("AniList \(al)%").foregroundStyle(.secondary) } // anime (META-2)
            }
            .lineLimit(1)
            .fixedSize()
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) { chips }
                VStack(alignment: .leading, spacing: 4) { chips }
            }
            .font(.caption.bold())
        }
    }

    @ViewBuilder private func primaryButtons(_ d: ItemDetail) -> some View {
        switch d.type {
        case .movie, .episode, .video:
            let resume = (d.base.viewOffsetMs ?? 0) > 0
            Button { video.play(d.id) } label: {
                Label(resume ? "Resume" : "Play", systemImage: "play.fill")
            }
            .buttonStyle(.borderedProminent)
        case .show, .season:
            Button {
                run {
                    guard let ep = try await app.leaves(d.id, unwatched: true).first else { throw MarqueeError("There are no unwatched episodes.") }
                    video.play(ep.id)
                }
            } label: {
                Label((d.base.watchedLeafCount ?? 0) > 0 ? "Continue" : "Play", systemImage: "play.fill")
            }
            .buttonStyle(.borderedProminent)
        case .album, .artist:
            Button { run { music.play(try await app.leaves(d.id), source: d.title) } } label: { Label("Play", systemImage: "play.fill") }
                .buttonStyle(.borderedProminent)
            Button { run { music.play(try await app.leaves(d.id), shuffle: true, source: d.title) } } label: { Label("Shuffle", systemImage: "shuffle") }
                .buttonStyle(.bordered)
        case .track:
            Button { music.play([d.base]) } label: { Label("Play", systemImage: "play.fill") }.buttonStyle(.borderedProminent)
        case .collection:
            PinToHomeButton(rowID: AppSession.pinnedRowID(collection: d.id))
        }
    }

    @ViewBuilder private func secondaryButtons(_ d: ItemDetail) -> some View {
        if [.movie, .episode, .video].contains(d.type), (d.base.viewOffsetMs ?? 0) > 0 {
            Button { video.play(d.id, startMs: 0) } label: { Label("From start", systemImage: "arrow.counterclockwise") }.buttonStyle(.bordered)
        }
        if [.album, .artist, .track].contains(d.type) { radioButton(d) }
        #if os(iOS)
        if [.movie, .episode, .video, .season, .show, .album, .artist, .track].contains(d.type) {
            DownloadButton(item: d.base)
        }
        #endif
        if [.movie, .show, .episode, .video].contains(d.type) {
            Button {
                watchlisted.toggle()
                let on = watchlisted
                Task { do { try await app.setWatchlist(d.id, on) } catch { watchlisted = !on } }
            } label: {
                Label(watchlisted ? "On Watchlist" : "Watchlist", systemImage: watchlisted ? "bookmark.fill" : "bookmark")
            }
            .buttonStyle(.bordered)
        }
        if d.base.isPlayableVideo || d.type == .show || d.type == .season {
            Button {
                Task {
                    try? await app.setWatched(d.id, !d.base.watched)
                    await load()
                }
            } label: {
                Label(d.base.watched ? "Watched" : "Mark watched", systemImage: d.base.watched ? "checkmark.circle.fill" : "checkmark.circle")
            }
            .buttonStyle(.bordered)
        }
        #if os(iOS)
        if [.movie, .episode, .video, .season, .show, .album, .artist, .track].contains(d.type) {
            // Remote control (USER-14): play this on another of your Marquee apps.
            PlayOnButton(target: { PlayOnTarget(itemIDs: [d.id]) }).buttonStyle(.bordered)
        }
        #endif
        Menu { ItemMenuItems(item: d.base) } label: { Image(systemName: "ellipsis").padding(.horizontal, 4) }
            .buttonStyle(.bordered)
            .accessibilityLabel("More")
    }

    private func radioButton(_ d: ItemDetail) -> some View {
        Button {
            let req = RadioRequest(seed: .item, itemId: d.id, limit: 50)
            run {
                let st = try await app.radio(req)
                guard !st.items.isEmpty else { throw MarqueeError("Couldn't find anything to play for this station.") }
                music.playStation(st, radio: req)
            }
        } label: {
            Label("Radio", systemImage: "dot.radiowaves.left.and.right")
        }
        .buttonStyle(.bordered)
    }

    /// Runs a play action, showing what went wrong instead of doing nothing.
    private func run(_ work: @escaping @MainActor () async throws -> Void) {
        Task {
            do { try await work() } catch is CancellationError {} catch { actionError = error.localizedDescription }
        }
    }

    /// Audio and subtitle choices for videos (the player's own menu also switches text
    /// subtitles while playing).
    @ViewBuilder private func trackPickers(_ d: ItemDetail) -> some View {
        let versions = d.info.versions
        let chosen = versions.first { $0.files.first?.id == fileID } ?? versions.first
        let streams = chosen?.files.first?.streams ?? []
        let audio = streams.filter { $0.kind == .audio }
        let subs = streams.filter { $0.kind == .subtitle }
        if audio.count > 1 || !subs.isEmpty || versions.count > 1 || (!isTV && (d.type == .movie || d.type == .episode)) {
            HStack(spacing: 16) {
                if versions.count > 1 {
                    // 4K and 1080p, or a director's cut (LIB-7); streams differ per file.
                    Picker("Version", selection: $fileID) {
                        Text("Best version").tag(Int64?.none)
                        ForEach(versions, id: \.id) { v in Text(versionLabel(v)).tag(v.files.first.map { Int64?.some($0.id) } ?? nil) }
                    }
                    .onChange(of: fileID) {
                        audioID = nil
                        subtitleID = nil
                        PendingTracks.shared.set(item: d.id, audio: nil, subtitle: nil, file: fileID)
                    }
                }
                if audio.count > 1 {
                    Picker("Audio", selection: $audioID) {
                        Text("Automatic").tag(Int64?.none)
                        ForEach(audio, id: \.id) { s in Text(streamLabel(s)).tag(Int64?.some(s.id)) }
                    }
                }
                if !subs.isEmpty {
                    Picker("Subtitles", selection: $subtitleID) {
                        Text("Automatic").tag(Int64?.none)
                        Text("Off").tag(Int64?.some(-1))
                        ForEach(subs, id: \.id) { s in Text(streamLabel(s)).tag(Int64?.some(s.id)) }
                    }
                }
                #if os(iOS)
                if d.type == .movie || d.type == .episode {
                    Button("Find Subtitles…", systemImage: "captions.bubble") { findingSubs = true }
                }
                #endif
            }
            .pickerStyle(.menu)
            .font(.callout)
            .sheet(isPresented: $findingSubs) {
                SubtitleSearchSheet(itemID: d.id, onAdded: { stream in
                    subtitleID = stream
                    Task { await load() }
                }, onBazarr: { Task { await load() } })
            }
            .onChange(of: audioID) { PendingTracks.shared.set(item: d.id, audio: audioID, subtitle: subtitleID, file: fileID) }
            .onChange(of: subtitleID) { PendingTracks.shared.set(item: d.id, audio: audioID, subtitle: subtitleID, file: fileID) }
        }
    }

    private func versionLabel(_ v: Schemas.MediaVersion) -> String {
        let f = v.files.first
        var tech: [String] = []
        if let h = f?.height {
            tech.append((f?.width ?? 0) >= 3200 || h >= 2000 ? "4K" : h >= 1000 ? "1080p" : h >= 700 ? "720p" : "SD")
        }
        if let hdr = f?.hdrFormat { tech.append(hdr == .dolbyVision ? "Dolby Vision" : hdr.rawValue.uppercased()) }
        if let c = f?.videoCodec { tech.append(["hevc": "HEVC", "h264": "H.264", "av1": "AV1"][c] ?? c.uppercased()) }
        let t = tech.joined(separator: " · ")
        return v.label.isEmpty ? (t.isEmpty ? "Version" : t) : (t.isEmpty ? v.label : "\(v.label) (\(t))")
    }

    private func streamLabel(_ s: Schemas.MediaStream) -> String {
        var parts = [s.title ?? languageName(s.language)]
        if s.kind == .audio, let ch = s.channels { parts.append(ch >= 6 ? "\(ch - 1).1" : ch == 2 ? "Stereo" : "\(ch) ch") }
        parts.append(s.codec.uppercased())
        if s.forced { parts.append("Forced") }
        if s.hearingImpaired { parts.append("SDH") }
        return parts.joined(separator: " · ")
    }

    // MARK: - Contents

    @ViewBuilder private func contents(_ d: ItemDetail) -> some View {
        if !children.isEmpty {
            switch d.type {
            case .artist:
                if !popular.isEmpty { popularSection(d) }
                // Sections by release type (MusicBrainz, META-3).
                ForEach(releaseSections(children), id: \.title) { section in
                    ShelfRow(title: section.title) {
                        ForEach(section.items, id: \.id) { PosterCard(item: $0) }
                    }
                }
            case .show, .collection:
                ShelfRow(title: d.type == .show ? "Seasons" : "In this collection") {
                    ForEach(children, id: \.id) { PosterCard(item: $0) }
                }
            case .season:
                VStack(alignment: .leading, spacing: 4) {
                    Text("Episodes").font(.title3.bold()).padding(.horizontal, sidePadding)
                    ForEach(children, id: \.id) { ep in EpisodeRow(episode: ep) }
                }
            case .album:
                VStack(alignment: .leading, spacing: 0) {
                    Text("Tracks").font(.title3.bold()).padding(.horizontal, sidePadding).padding(.bottom, 6)
                    ForEach(Array(children.enumerated()), id: \.element.id) { i, t in
                        TrackRow(track: t, album: d.base) { music.play(children.filter { $0._type == .track }, start: i, source: d.title) }
                    }
                }
            default:
                EmptyView()
            }
        }
    }

    /// The artist's most-listened tracks (ListenBrainz, MUSIC-15): five, or all ten.
    @ViewBuilder private func popularSection(_ d: ItemDetail) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Popular").font(.title3.bold()).padding(.horizontal, sidePadding).padding(.bottom, 6)
            ForEach(Array((allPopular ? popular : Array(popular.prefix(5))).enumerated()), id: \.element.id) { i, t in
                Button { music.play(popular, start: i, source: "\(d.title) – Popular") } label: {
                    HStack(spacing: 14) {
                        Text("\(i + 1)").font(.callout.monospacedDigit()).foregroundStyle(.secondary).frame(width: 28, alignment: .trailing)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(t.title).lineLimit(1).foregroundStyle(music.current?.item.id == t.id ? Color.marqueeGold : .primary)
                            if let album = t.parentTitle { Text(album).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                        }
                        Spacer()
                    }
                    .padding(.horizontal, sidePadding).padding(.vertical, 8)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            if popular.count > 5 {
                Button(allPopular ? "Show Fewer" : "Show All \(popular.count)") { allPopular.toggle() }
                    .font(.callout).padding(.horizontal, sidePadding).padding(.top, 4)
            }
        }
    }

    @ViewBuilder private func cast(_ d: ItemDetail) -> some View {
        let actors = d.info.credits.filter { $0.role == .actor }.prefix(20)
        if !actors.isEmpty {
            ShelfRow(title: "Cast") {
                ForEach(Array(actors), id: \.personId) { c in
                    NavigationLink(value: Route.person(c.personId)) {
                        VStack(spacing: 6) {
                            ZStack {
                                Circle().fill(Color.gray.opacity(0.3))
                                Image(systemName: "person.fill").foregroundStyle(.secondary)
                                if c.hasPhoto == true {
                                    AsyncImage(url: app.personPhotoURL(c.personId, width: Int(castSize))) { $0.image?.resizable().scaledToFill() }
                                }
                            }
                            .frame(width: castSize, height: castSize).clipShape(Circle())
                            Text(c.name).font(.caption.weight(.medium)).lineLimit(1)
                            if let ch = c.character { Text(ch).font(.caption2).foregroundStyle(.secondary).lineLimit(1) }
                        }
                        .frame(width: castSize + 10)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    #if os(tvOS)
    private let castSize: CGFloat = 150
    #else
    private let castSize: CGFloat = 80
    #endif
}

/// Track choices made on the detail page, used when that item is played next.
@MainActor final class PendingTracks {
    static let shared = PendingTracks()
    private var choices: [Int64: (audio: Int64?, subtitle: Int64?, file: Int64?)] = [:]
    func set(item: Int64, audio: Int64?, subtitle: Int64?, file: Int64? = nil) { choices[item] = (audio, subtitle, file) }
    func take(_ item: Int64) -> (audio: Int64?, subtitle: Int64?, file: Int64?) { choices.removeValue(forKey: item) ?? (nil, nil, nil) }
}

func languageName(_ code: String?) -> String {
    guard let code, !code.isEmpty, code != "und" else { return "Unknown" }
    return Locale.current.localizedString(forLanguageCode: code) ?? code.uppercased()
}

struct EpisodeRow: View {
    @Environment(VideoPresenter.self) private var video
    let episode: Item
    #if os(tvOS)
    private let thumb: CGFloat = 320
    #else
    private let thumb: CGFloat = 140
    #endif

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Button { video.play(episode.id) } label: {
                ArtworkView(item: episode, shape: .wide, width: thumb)
                    .frame(width: thumb)
                    .overlay { Image(systemName: "play.circle.fill").font(.title).foregroundStyle(.white.opacity(0.85)) }
            }
            .buttonStyle(.plain)
            NavigationLink(value: Route.item(episode.id)) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("\(episode.index ?? 0). \(episode.title)").font(.headline).lineLimit(2)
                    Text(formatDuration(ms: episode.durationMs)).font(.caption).foregroundStyle(.secondary)
                    if episode.watched { Label("Watched", systemImage: "checkmark").font(.caption2).foregroundStyle(Color.marqueeGold) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, sidePadding)
        .padding(.vertical, 6)
        .contextMenu { ItemMenuItems(item: episode) }
    }
}

struct TrackRow: View {
    @Environment(MusicPlayer.self) private var music
    let track: Item
    var album: Item?
    let play: () -> Void

    var body: some View {
        Button(action: play) {
            HStack(spacing: 14) {
                Text(track.index.map(String.init) ?? "").font(.callout.monospacedDigit()).foregroundStyle(.secondary).frame(width: 28, alignment: .trailing)
                VStack(alignment: .leading, spacing: 2) {
                    Text(track.title).lineLimit(1).foregroundStyle(music.current?.item.id == track.id ? Color.marqueeGold : .primary)
                    if let credit = track.artistCredit, credit != album?.artistCredit { Text(credit).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                }
                Spacer()
                if music.showAudioQuality, let f = track.audioFormat {
                    Text(f.shortLabel).font(.caption2).foregroundStyle(.tertiary).lineLimit(1).fixedSize()
                        .accessibilityIdentifier("trackQuality")
                }
                Text(formatTime(seconds: Double(track.durationMs ?? 0) / 1000)).font(.callout.monospacedDigit()).foregroundStyle(.secondary)
            }
            .padding(.horizontal, sidePadding)
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .contextMenu { ItemMenuItems(item: track) }
    }
}

/// A trailer, featurette or other extra (LIB-8): plays when chosen.
struct ExtraCard: View {
    let extra: Item
    let play: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button(action: play) {
                ArtworkView(item: extra, shape: .wide, width: width)
                    .overlay { Image(systemName: "play.circle.fill").font(.title).foregroundStyle(.white.opacity(0.85)) }
            }
            #if os(tvOS)
            .buttonStyle(.card)
            #else
            .buttonStyle(.plain)
            #endif
            .accessibilityLabel("Play \(extra.title)")
            Text(extra.title).font(.subheadline.weight(.semibold)).lineLimit(1)
            Text(kind).font(.caption).foregroundStyle(.secondary)
        }
        .frame(width: width)
    }

    private var kind: String {
        switch extra.extraType {
        case .trailer: "Trailer"
        case .featurette: "Featurette"
        case .behindTheScenes: "Behind the scenes"
        case .deletedScene: "Deleted scene"
        case .interview: "Interview"
        case .scene: "Scene"
        case .short: "Short"
        default: "Extra"
        }
    }

    #if os(tvOS)
    private let width: CGFloat = 380
    #else
    private let width: CGFloat = 220
    #endif
}
