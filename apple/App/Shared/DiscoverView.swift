import MarqueeKit
import SwiftUI

/// Discover (REQ-1, REQ-2): trending and popular titles from Seerr, search, and requests.
struct DiscoverView: View {
    @Environment(AppSession.self) private var app
    @State private var status: RequestsStatus?
    @State private var category: DiscoverCategory = .trending
    @State private var query = ""
    @State private var items: [DiscoverItem] = []
    @State private var error: String?
    @State private var picked: DiscoverItem?
    /// The same title on demand, opened from a title's sheet (Watch now on VOD).
    @State private var vodOpen: VodRef?
    @State private var browse = DiscoverBrowse()
    @State private var filters: DiscoverFilters?
    @State private var page = 1
    @State private var hasMore = false
    @State private var loadingMore = false
    /// Bumped by every fresh load, so a page that arrives for an older list is dropped.
    @State private var generation = 0

    var body: some View {
        content
            .navigationTitle("Discover")
            .searchable(text: $query, prompt: "Find a movie or show to request")
            .task {
                status = try? await app.requestsStatus()
                filters = try? await app.discoverFilters()
            }
            .task(id: "\(category.rawValue)|\(query)|\(browse.hashValue)") { await load() }
            .sheet(item: $picked, onDismiss: { Task { await load() } }) { RequestSheet(item: $0, onWatchVod: { vodOpen = $0 }) }
            .navigationDestination(item: $vodOpen) { ref in
                if ref.kind == .movie { VodMovieView(id: ref.id) } else { VodSeriesView(id: ref.id) }
            }
    }

    @ViewBuilder private var content: some View {
        if let status, !status.enabled {
            ContentUnavailableView("Requests aren't set up", systemImage: "tray", description: Text("An admin can connect Seerr in the web app's Settings → Requests."))
        } else if let status, !status.canRequest {
            ContentUnavailableView("Ask an admin", systemImage: "lock", description: Text("An admin can let you request movies and shows."))
        } else {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    if query.isEmpty {
                        categoryChips
                        if category == .movies || category == .tv { filterMenus }
                    }
                    if let error { Text(error).foregroundStyle(.red).padding(.horizontal, sidePadding) }
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: cardWidth), spacing: gridSpacing, alignment: .top)], spacing: gridSpacing) {
                        ForEach(items, id: \.id) { it in
                            card(it).onAppear { if it.id == items.last?.id { Task { await loadMore() } } }
                        }
                    }
                    .padding(.horizontal, sidePadding)
                    if loadingMore { ProgressView().frame(maxWidth: .infinity) }
                }
                .padding(.vertical)
            }
        }
    }

    @ViewBuilder private func card(_ it: DiscoverItem) -> some View {
        let label = VStack(alignment: .leading, spacing: 4) {
            ZStack(alignment: .topLeading) {
                CachedImage(url: it.posterUrl.flatMap(URL.init(string:))) { phase in
                    if let img = phase.image {
                        img.resizable().scaledToFill()
                    } else {
                        Rectangle().fill(.secondary.opacity(0.2)).overlay(Text(it.title).font(.caption).padding(6), alignment: .bottomLeading)
                    }
                }
                .aspectRatio(2 / 3, contentMode: .fit)
                .clipShape(RoundedRectangle(cornerRadius: 8))
                if it.availability != Availability.none {
                    Text(it.availability.label)
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6).padding(.vertical, 3)
                        .background(it.availability == .available ? Color.green : Color.black.opacity(0.7), in: RoundedRectangle(cornerRadius: 4))
                        .foregroundStyle(.white)
                        .padding(6)
                }
            }
            Text(it.title).font(.caption.weight(.medium)).lineLimit(1)
            Text([it.year.map(String.init), it.mediaType == .tv ? "Series" : "Movie"].compactMap { $0 }.joined(separator: " · "))
                .font(.caption2).foregroundStyle(.secondary).lineLimit(1)
        }
        VStack(alignment: .leading, spacing: isTV ? 12 : 6) {
            if let id = it.itemId {
                NavigationLink(value: Route.item(id)) { label }.buttonStyle(cardStyle)
            } else {
                Button { picked = it } label: { label } // its details, with Request when it can be
                    .buttonStyle(cardStyle)
                    .accessibilityLabel(requestable(it) ? "Request \(it.title)" : "\(it.title), \(it.availability.label)")
            }
            // Under the year line, never beside it: on demand already (watch it there now rather
            // than wait), and Request (a show the library has part of can still be requested).
            DiscoverTileButtons(title: it.title, vod: it.vod?.value1, requestable: requestable(it)) { picked = it }
        }
    }

    private func requestable(_ it: DiscoverItem) -> Bool {
        it.availability == Availability.none || (it.mediaType == .tv && it.availability == .partial)
    }

    private static let categories: [(DiscoverCategory, String)] = [
        (.trending, "Trending"), (.movies, "Movies"), (.tv, "TV Shows"), (.upcoming, "Upcoming Movies"), (.upcomingTv, "Upcoming Shows"),
    ]

    private var categoryChips: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(Self.categories, id: \.0) { c, label in
                    Button {
                        category = c
                        browse = DiscoverBrowse()
                    } label: {
                        Text(label).font(.subheadline.weight(category == c ? .semibold : .regular))
                            .padding(.horizontal, 14).padding(.vertical, 7)
                            .background(category == c ? Color.marqueeGold : Color.secondary.opacity(0.18), in: Capsule())
                            .foregroundStyle(category == c ? Color.black : Color.primary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(category == c ? .isSelected : [])
                }
            }
            .padding(.horizontal, sidePadding)
        }
    }

    /// Network (shows) or studio (films), genre and order.
    private var filterMenus: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 10) {
                if category == .tv {
                    filterMenu("Network", all: "All Networks", options: filters?.networks ?? [], value: $browse.network)
                } else {
                    filterMenu("Studio", all: "All Studios", options: filters?.studios ?? [], value: $browse.studio)
                }
                filterMenu("Genre", all: "All Genres", options: (category == .tv ? filters?.tvGenres : filters?.movieGenres) ?? [], value: $browse.genre)
                Menu {
                    Picker("Sort", selection: $browse.sort) {
                        Text("Popular").tag(DiscoverSort.popular)
                        Text("Top Rated").tag(DiscoverSort.rating)
                        Text("Newest").tag(DiscoverSort.newest)
                        Text("A–Z").tag(DiscoverSort.title)
                    }
                } label: {
                    menuLabel(["popular": "Popular", "rating": "Top Rated", "newest": "Newest", "title": "A–Z"][browse.sort.rawValue] ?? "Sort")
                }
            }
            .padding(.horizontal, sidePadding)
        }
    }

    private func filterMenu(_ title: String, all: String, options: [NamedID], value: Binding<Int64?>) -> some View {
        Menu {
            Picker(title, selection: value) {
                Text(all).tag(Int64?.none)
                ForEach(options, id: \.id) { o in Text(o.name).tag(Int64?.some(o.id)) }
            }
        } label: {
            menuLabel(options.first { $0.id == value.wrappedValue }?.name ?? all)
        }
        .accessibilityLabel(title)
    }

    private func menuLabel(_ text: String) -> some View {
        HStack(spacing: 4) {
            Text(text)
            Image(systemName: "chevron.down").font(.caption2)
        }
        .font(.subheadline)
        .padding(.horizontal, 12).padding(.vertical, 7)
        .background(Color.secondary.opacity(0.18), in: Capsule())
    }

    /// The next page, when the last card shows.
    private func loadMore() async {
        guard hasMore, !loadingMore else { return }
        let gen = generation
        loadingMore = true
        defer { if gen == generation { loadingMore = false } }
        let q = query.trimmingCharacters(in: .whitespaces)
        guard let p = try? await (q.isEmpty ? app.discoverRequestable(category, page: page + 1, browse: browse) : app.searchRequestable(q, page: page + 1)),
              gen == generation else { return }
        page = p.page
        hasMore = p.page < p.totalPages
        let seen = Set(items.map(\.id))
        items += p.results.filter { !seen.contains($0.id) }
    }

    private func load() async {
        guard status?.enabled != false, status?.canRequest != false else { return }
        generation += 1
        loadingMore = false
        hasMore = false // no paging until the new first page is in
        let gen = generation
        do {
            let q = query.trimmingCharacters(in: .whitespaces)
            if !q.isEmpty { try await Task.sleep(for: .milliseconds(350)) }
            let p = q.isEmpty ? try await app.discoverRequestable(category, browse: browse) : try await app.searchRequestable(q)
            guard gen == generation else { return }
            items = p.results
            page = p.page
            hasMore = p.page < p.totalPages
            error = nil
        } catch is CancellationError {
        } catch {
            self.error = error.localizedDescription
        }
    }

    #if os(tvOS)
    private let cardWidth: CGFloat = 200
    private let gridSpacing: CGFloat = 40
    private var cardStyle: some PrimitiveButtonStyle { .borderless }
    #else
    private let cardWidth: CGFloat = 110
    private let gridSpacing: CGFloat = 14
    private var cardStyle: some PrimitiveButtonStyle { .plain }
    #endif
}

/// A Discover tile's buttons, one above the other at the tile's width: a gold ▶ VOD when the
/// title is on demand, and Request when it can be requested (secondary under VOD, primary
/// alone). Small, so they never crowd the year line above them.
struct DiscoverTileButtons: View {
    let title: String
    let vod: VodRef?
    let requestable: Bool
    let request: () -> Void

    var body: some View {
        if vod != nil || requestable {
            VStack(spacing: isTV ? 10 : 4) {
                if let vod {
                    NavigationLink(value: vod.kind == .movie ? Route.vodMovie(vod.id) : Route.vodSeries(vod.id)) {
                        tileLabel("VOD", systemImage: "play.fill", prominent: true)
                    }
                    .modifier(TileButtonStyle(prominent: true))
                    .accessibilityLabel("Watch \(title) now on VOD")
                    .accessibilityIdentifier("discover.watchOnVod")
                }
                if requestable {
                    Button(action: request) {
                        tileLabel("Request", systemImage: nil, prominent: vod == nil)
                    }
                    .modifier(TileButtonStyle(prominent: vod == nil))
                    .accessibilityLabel("Request \(title)")
                    .accessibilityIdentifier("discover.request")
                }
            }
        }
    }

    @ViewBuilder private func tileLabel(_ text: String, systemImage: String?, prominent: Bool) -> some View {
        let content = HStack(spacing: 3) {
            if let systemImage { Image(systemName: systemImage).imageScale(.small) }
            Text(text).lineLimit(1)
        }
        .font(isTV ? .caption.weight(.semibold) : .caption2.weight(.semibold))
        .frame(maxWidth: .infinity)
        #if os(tvOS)
        content
        #else
        content
            .padding(.vertical, 4)
            .background(prominent ? Color.marqueeGold : Color.secondary.opacity(0.2), in: RoundedRectangle(cornerRadius: 5, style: .continuous))
            .foregroundStyle(prominent ? Color.black : Color.primary)
            .contentShape(RoundedRectangle(cornerRadius: 5, style: .continuous))
        #endif
    }
}

/// iPhone/iPad: the label draws itself. TV: the system's bordered buttons, so focus shows.
private struct TileButtonStyle: ViewModifier {
    let prominent: Bool
    func body(content: Content) -> some View {
        #if os(tvOS)
        if prominent {
            content.buttonStyle(.borderedProminent).tint(Color.marqueeGold).foregroundStyle(.black)
        } else {
            content.buttonStyle(.bordered)
        }
        #else
        content.buttonStyle(.plain)
        #endif
    }
}

extension DiscoverItem: @retroactive Identifiable {
    public var id: String { "\(mediaType.rawValue)-\(tmdbId)" }
}

/// A title's details, and asking for a movie or chosen seasons of a show.
struct RequestSheet: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let item: DiscoverItem
    /// Opens the same title on demand (the sheet closes first).
    var onWatchVod: ((VodRef) -> Void)?
    @State private var show: RequestableShow?
    @State private var ratings: TitleRatings?
    @State private var seasons: Set<Int> = []
    @State private var busy = false
    @State private var done = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                header
                // On demand: the server sends it only when the library hasn't all of it.
                if let v = item.vod?.value1, let onWatchVod {
                    Section {
                        Button {
                            dismiss()
                            onWatchVod(v)
                        } label: {
                            Label("Watch now on VOD", systemImage: "play.fill")
                        }
                        .accessibilityIdentifier("request.watchOnVod")
                    } footer: {
                        Text("It's on demand already, so you can watch it now.")
                    }
                }
                if canRequest && item.mediaType == .tv { seasonsSection }
                if let error { Text(error).foregroundStyle(.red) }
                if done { Label("Requested", systemImage: "checkmark.circle.fill").foregroundStyle(.green) }
            }
            .navigationTitle(canRequest ? "Request" : "Details")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(canRequest ? "Cancel" : "Done") { dismiss() } }
                if canRequest {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Request") { submit() }
                            .disabled(busy || done || (item.mediaType == .tv && seasons.isEmpty))
                    }
                }
            }
            .task { await loadShow() }
            .task { ratings = try? await app.titleRatings(item) }
        }
    }

    private var header: some View {
        Section {
            HStack(alignment: .top, spacing: 14) {
                CachedImage(fill: item.posterUrl.flatMap(URL.init(string:))).background(Color.secondary.opacity(0.2))
                    .frame(width: 90, height: 135)
                    .clipShape(RoundedRectangle(cornerRadius: 6))
                VStack(alignment: .leading, spacing: 6) {
                    Text(item.title).font(.headline)
                    Text(subtitle).font(.caption).foregroundStyle(.secondary)
                    let chips = app.ratingChips(item, ratings)
                    if !chips.isEmpty {
                        HStack(spacing: 10) {
                            ForEach(chips, id: \.label) { c in
                                (Text(c.label + " ").foregroundStyle(.secondary) + Text(c.value).bold()).font(.caption)
                            }
                        }
                        .accessibilityElement(children: .combine)
                    }
                    Text(item.overview ?? "No description available.").font(.caption)
                }
            }
        } footer: {
            if canRequest { Text("An admin approves requests before they're downloaded.") }
        }
    }

    private var canRequest: Bool {
        item.availability == Availability.none || (item.mediaType == .tv && item.availability == .partial)
    }

    private var subtitle: String {
        var parts: [String] = []
        if let y = item.year { parts.append(String(y)) }
        parts.append(item.mediaType == .tv ? "Series" : "Movie")
        if item.availability != Availability.none { parts.append(item.availability.label) }
        return parts.joined(separator: " · ")
    }

    private var seasonsSection: some View {
        Section("Seasons") {
            if let show {
                ForEach(show.seasons, id: \.number) { s in seasonRow(s) }
            } else {
                ProgressView()
            }
        }
    }

    private func seasonRow(_ s: RequestableShow.SeasonsPayloadPayload) -> some View {
        let open = s.availability == Availability.none
        let binding = Binding<Bool>(
            get: { open && seasons.contains(s.number) },
            set: { on in if on { seasons.insert(s.number) } else { seasons.remove(s.number) } }
        )
        let detail = open ? "\(s.episodeCount ?? 0) episodes" : s.availability.label
        return Toggle(isOn: binding) {
            VStack(alignment: .leading) {
                Text(s.name ?? "Season \(s.number)")
                Text(detail).font(.caption).foregroundStyle(.secondary)
            }
        }
        .disabled(!open)
    }

    private func loadShow() async {
        guard item.mediaType == .tv else { return }
        show = try? await app.requestableShow(item.tmdbId)
        seasons = Set(show?.seasons.filter { $0.availability == Availability.none }.map(\.number) ?? [])
    }

    private func submit() {
        busy = true
        Task {
            defer { busy = false }
            do {
                _ = try await app.request(item, seasons: item.mediaType == .tv ? seasons.sorted() : [])
                done = true
                try? await Task.sleep(for: .seconds(1))
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// Admins: approve or decline what people asked for.
struct RequestApprovalsView: View {
    @Environment(AppSession.self) private var app
    @State private var list: [MediaRequest] = []
    @State private var declining: MediaRequest?
    @State private var reason = ""
    @State private var error: String?

    var body: some View {
        List {
            if let error { Text(error).foregroundStyle(.red) }
            let pending = list.filter { $0.status == .pending }
            Section("Waiting for approval") {
                if pending.isEmpty { Text("Nothing to approve.").foregroundStyle(.secondary) }
                ForEach(pending, id: \.id) { r in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(r.year.map { "\(r.title) (\($0))" } ?? r.title).font(.headline)
                        Text("\(r.userName) · \(r.mediaType == .tv ? (r.seasons.isEmpty ? "All seasons" : "Seasons \(r.seasons.map(String.init).joined(separator: ", "))") : "Movie")")
                            .font(.caption).foregroundStyle(.secondary)
                        HStack {
                            Button("Approve") { act { _ = try await app.approveRequest(r.id) } }.buttonStyle(.borderedProminent)
                            Button("Decline", role: .destructive) { reason = ""; declining = r }.buttonStyle(.bordered)
                        }
                    }
                }
            }
            let done = list.filter { $0.status != .pending }.prefix(30)
            if !done.isEmpty {
                Section("Recent") {
                    ForEach(Array(done), id: \.id) { r in
                        LabeledContent(r.title, value: "\(r.userName) · \(r.status.label)")
                    }
                }
            }
        }
        .navigationTitle("Requests")
        .task { await load() }
        .alert("Decline \(declining?.title ?? "")?", isPresented: Binding(get: { declining != nil }, set: { if !$0 { declining = nil } })) {
            TextField("Reason (optional)", text: $reason)
            Button("Decline", role: .destructive) {
                if let r = declining { act { _ = try await app.declineRequest(r.id, reason: reason.isEmpty ? nil : reason) } }
            }
            Button("Cancel", role: .cancel) {}
        }
    }

    private func load() async {
        do { list = try await app.requests(all: true); error = nil } catch { self.error = error.localizedDescription }
    }

    private func act(_ work: @escaping () async throws -> Void) {
        Task {
            do { try await work() } catch { self.error = error.localizedDescription }
            await load()
        }
    }
}
