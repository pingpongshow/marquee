import MarqueeKit
import SwiftUI

/// "Your Year in Music" (MUSIC-22): a card that opens the recap, with a year picker when
/// there's more than one year. Hidden when there's no music history.
struct RecapCard: View {
    @Environment(AppSession.self) private var app
    @State private var years: [Int] = []
    @State private var year: Int?
    @State private var showing: Int?

    var body: some View {
        if let year {
            HStack(spacing: 12) {
                Button { showing = year } label: { card(year) }
                    #if os(tvOS)
                    .buttonStyle(.card)
                    #else
                    .buttonStyle(.plain)
                    #endif
                    .accessibilityLabel("Your \(String(year)) in Music")
                    .accessibilityIdentifier("recapCard")
                if years.count > 1 {
                    Menu {
                        Picker("Year", selection: Binding(get: { year }, set: { self.year = $0 })) {
                            ForEach(years, id: \.self) { Text(String($0)).tag($0) }
                        }
                    } label: {
                        Label("Year", systemImage: "calendar")
                    }
                    .accessibilityLabel("Choose the year")
                }
            }
            .padding(.horizontal, sidePadding)
            #if os(tvOS)
            .focusSection()
            #endif
            .fullScreenCover(item: Binding(get: { showing.map(RecapYear.init) }, set: { showing = $0?.id })) { y in
                RecapView(year: y.id)
            }
        } else {
            Color.clear.frame(height: 0).task {
                years = (try? await app.recapYears()) ?? []
                year = years.first
            }
        }
    }

    private func card(_ year: Int) -> some View {
        HStack(spacing: 16) {
            Image(systemName: "sparkles").font(.system(size: isTV ? 54 : 30, weight: .bold))
            VStack(alignment: .leading, spacing: 2) {
                Text("Your Year in Music").font(isTV ? .title2.bold() : .headline.weight(.heavy))
                Text("Your \(String(year)), in songs, artists and minutes").font(.caption).opacity(0.85)
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right").font(.headline)
        }
        .foregroundStyle(.white)
        .padding(isTV ? 30 : 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(LinearGradient(colors: [Color(red: 0.95, green: 0.3, blue: 0.45), Color(red: 0.45, green: 0.2, blue: 0.85)],
                                   startPoint: .topLeading, endPoint: .bottomTrailing),
                    in: RoundedRectangle(cornerRadius: 16))
    }
}

struct RecapYear: Identifiable { let id: Int }

/// The recap as a story: full-screen pages, swiped or tapped through (on the TV, left/right).
struct RecapView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss
    let year: Int
    @State private var recap: ListeningRecap?
    @State private var error: String?
    @State private var page = 0
    @State private var saving = false
    @State private var saved: Playlist?
    @State private var openPlaylist: Playlist?

    enum Page: String, Hashable {
        case minutes, topArtist, topArtists, topSongs, topAlbums, genres, when, days, discovered, summary
    }

    private var pages: [Page] {
        guard let r = recap else { return [] }
        var out: [Page] = [.minutes]
        if !r.topArtists.isEmpty { out += [.topArtist, .topArtists] }
        if !r.topTracks.isEmpty { out.append(.topSongs) }
        if !r.topAlbums.isEmpty { out.append(.topAlbums) }
        if !r.topGenres.isEmpty { out.append(.genres) }
        out += [.when, .days, .discovered, .summary]
        return out
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let r = recap {
                story(r)
            } else if let error {
                VStack(spacing: 16) {
                    ErrorBanner(message: error)
                    Button("Close") { dismiss() }
                }
                .padding()
            } else {
                ProgressView()
            }
        }
        .preferredColorScheme(.dark)
        .task {
            do { recap = try await app.recap(year: year) } catch { self.error = error.localizedDescription }
        }
        #if os(iOS)
        .statusBarHidden()
        .sheet(item: $openPlaylist) { p in
            NavigationStack {
                PlaylistView(id: p.id)
                    .marqueeDestinations()
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { openPlaylist = nil } } }
            }
        }
        #endif
    }

    // MARK: - The story

    #if os(iOS)
    @ViewBuilder private func story(_ r: ListeningRecap) -> some View {
        let all = pages
        TabView(selection: $page) {
            ForEach(Array(all.enumerated()), id: \.element) { i, p in
                pageView(p, r, index: i, count: all.count)
                    .tag(i)
            }
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .ignoresSafeArea()
        .overlay(alignment: .top) { topBar(count: all.count) }
        .overlay(alignment: .bottom) { savedBanner }
    }
    #else
    /// The TV: one page at a time, with Previous and Next (and left/right on the remote).
    @ViewBuilder private func story(_ r: ListeningRecap) -> some View {
        let all = pages
        ZStack(alignment: .bottom) {
            pageView(all[min(page, all.count - 1)], r, index: page, count: all.count)
                .id(page)
                .transition(.opacity)
            HStack(spacing: 30) {
                Button { go(-1) } label: { Label("Previous", systemImage: "chevron.left") }.disabled(page == 0)
                Text("\(page + 1) of \(all.count)").font(.callout).foregroundStyle(.white.opacity(0.7))
                Button { go(1) } label: { Label("Next", systemImage: "chevron.right") }.disabled(page >= all.count - 1)
                Button("Close") { dismiss() }
            }
            .padding(.bottom, 50)
            .focusSection()
            savedBanner.padding(.bottom, 140)
        }
        .onMoveCommand { dir in
            if dir == .left { go(-1) } else if dir == .right { go(1) }
        }
        .onExitCommand { dismiss() }
    }

    private func go(_ d: Int) {
        withAnimation(.easeInOut(duration: 0.4)) { page = max(0, min(pages.count - 1, page + d)) }
    }
    #endif

    private func topBar(count: Int) -> some View {
        HStack(spacing: 10) {
            HStack(spacing: 4) {
                ForEach(0..<count, id: \.self) { i in
                    Capsule().fill(.white.opacity(i <= page ? 0.95 : 0.3)).frame(height: 3)
                }
            }
            Button { dismiss() } label: {
                Image(systemName: "xmark").font(.headline).foregroundStyle(.white).padding(10).background(.black.opacity(0.25), in: Circle())
            }
            .accessibilityLabel("Close recap")
        }
        .padding(.horizontal)
        .padding(.top, 8)
    }

    @ViewBuilder private var savedBanner: some View {
        if let saved {
            HStack {
                Label("Saved “\(saved.title)”", systemImage: "checkmark.circle.fill").lineLimit(1)
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel("Saved “\(saved.title)”")
                    .accessibilityIdentifier("recapSaved")
                Spacer()
                #if os(iOS)
                Button("Open") { openPlaylist = saved }.bold()
                #endif
            }
            .foregroundStyle(.white)
            .padding()
            .background(.black.opacity(0.6), in: RoundedRectangle(cornerRadius: 14))
            .padding()
            .frame(maxWidth: 700)
            .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }

    /// A full-screen page with its colours, and taps on its background that move on (right
    /// two thirds) or back (left third), like a story.
    private func pageView(_ p: Page, _ r: ListeningRecap, index: Int, count: Int) -> some View {
        let colors = palette(index)
        return ZStack {
            LinearGradient(colors: colors, startPoint: .topLeading, endPoint: .bottomTrailing).ignoresSafeArea()
            #if os(iOS)
            GeometryReader { g in
                Color.clear.contentShape(Rectangle())
                    .onTapGesture(coordinateSpace: .local) { loc in
                        withAnimation { page = loc.x < g.size.width / 3 ? max(0, page - 1) : min(count - 1, page + 1) }
                    }
            }
            #endif
            content(p, r)
                .padding(.horizontal, isTV ? 160 : 28)
                .padding(.top, isTV ? 60 : 70)
                .padding(.bottom, isTV ? 170 : 40)
                .frame(maxWidth: isTV ? 1500 : 640)
        }
        .foregroundStyle(.white)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("recap-\(p.rawValue)")
    }

    private func palette(_ i: Int) -> [Color] {
        let all: [[Color]] = [
            [Color(red: 0.98, green: 0.33, blue: 0.42), Color(red: 0.47, green: 0.16, blue: 0.75)],
            [Color(red: 0.12, green: 0.75, blue: 0.55), Color(red: 0.05, green: 0.3, blue: 0.45)],
            [Color(red: 1.0, green: 0.7, blue: 0.15), Color(red: 0.9, green: 0.3, blue: 0.2)],
            [Color(red: 0.25, green: 0.4, blue: 1.0), Color(red: 0.1, green: 0.1, blue: 0.35)],
            [Color(red: 0.85, green: 0.25, blue: 0.75), Color(red: 0.25, green: 0.1, blue: 0.4)],
            [Color(red: 0.2, green: 0.8, blue: 0.95), Color(red: 0.15, green: 0.25, blue: 0.6)],
            [Color(red: 0.96, green: 0.74, blue: 0.27), Color(red: 0.55, green: 0.3, blue: 0.1)],
        ]
        return all[i % all.count]
    }

    // MARK: - Pages

    @ViewBuilder private func content(_ p: Page, _ r: ListeningRecap) -> some View {
        switch p {
        case .minutes:
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker("In \(String(r.year)) you listened for")
                Text(r.minutes.formatted()).font(.system(size: isTV ? 200 : 96, weight: .black)).minimumScaleFactor(0.4).lineLimit(1)
                Text(r.minutes == 1 ? "minute" : "minutes").font(isTV ? .largeTitle.bold() : .title.bold())
                Text("\(r.plays.formatted()) plays · \(r.tracks.formatted()) songs · \(r.artists.formatted()) artists")
                    .font(isTV ? .title3 : .headline).opacity(0.85)
                Spacer()
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        case .topArtist:
            if let top = r.topArtists.first {
                VStack(spacing: 20) {
                    Spacer()
                    kicker("Your top artist")
                    ArtworkView(item: top.item, shape: .square, width: isTV ? 420 : 240)
                        .frame(width: isTV ? 420 : 240)
                        .shadow(color: .black.opacity(0.4), radius: 20, y: 10)
                    Text(top.item.title).font(.system(size: isTV ? 76 : 40, weight: .black)).multilineTextAlignment(.center).lineLimit(2).minimumScaleFactor(0.5)
                    Text("\(top.plays.formatted()) plays · \(top.minutes.formatted()) minutes").font(isTV ? .title3 : .headline).opacity(0.85)
                    Spacer()
                }
            }
        case .topArtists:
            ranked("Your top artists", Array(r.topArtists.prefix(5)), square: true)
        case .topSongs:
            VStack(alignment: .leading, spacing: isTV ? 14 : 10) {
                Spacer(minLength: 0)
                kicker("Your top songs")
                ForEach(Array(r.topTracks.prefix(isTV ? 5 : 8).enumerated()), id: \.offset) { i, e in
                    Button { music.play(r.topTracks.map(\.item), start: i, source: "Your Top Songs \(String(r.year))") } label: {
                        HStack(spacing: 14) {
                            Text("\(i + 1)").font(.title3.weight(.black).monospacedDigit()).frame(width: 34, alignment: .trailing)
                            ArtworkView(item: e.item, shape: .square, width: isTV ? 80 : 48).frame(width: isTV ? 80 : 48)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(e.item.title).font(.headline).lineLimit(1)
                                Text(e.item.artistCredit ?? e.item.grandparentTitle ?? "").font(.caption).opacity(0.8).lineLimit(1)
                            }
                            Spacer()
                            Image(systemName: "play.circle.fill").font(.title2)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Play \(e.item.title)")
                }
                playTopSongs(r).padding(.top, 8)
                Spacer(minLength: 0)
            }
        case .topAlbums:
            VStack(alignment: .leading, spacing: 16) {
                Spacer(minLength: 0)
                kicker("Your top albums")
                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 14), count: isTV ? 5 : 2), spacing: 14) {
                    ForEach(Array(r.topAlbums.prefix(isTV ? 5 : 4).enumerated()), id: \.offset) { i, e in
                        VStack(alignment: .leading, spacing: 6) {
                            ArtworkView(item: e.item, shape: .square, width: isTV ? 240 : 150)
                            Text("\(i + 1). \(e.item.title)").font(.subheadline.bold()).lineLimit(1)
                            Text(e.item.artistCredit ?? e.item.parentTitle ?? "").font(.caption).opacity(0.8).lineLimit(1)
                        }
                    }
                }
                Spacer(minLength: 0)
            }
        case .genres:
            let top = Array(r.topGenres.prefix(6))
            let most = Double(top.map(\.plays).max() ?? 1)
            VStack(alignment: .leading, spacing: 14) {
                Spacer()
                kicker("Your genres")
                ForEach(Array(top.enumerated()), id: \.offset) { i, g in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(g.name).font(i == 0 ? .title.weight(.black) : .headline)
                        GeometryReader { geo in
                            Capsule().fill(.white.opacity(i == 0 ? 0.95 : 0.6)).frame(width: max(8, geo.size.width * Double(g.plays) / most))
                        }
                        .frame(height: i == 0 ? 14 : 8)
                    }
                }
                Spacer()
            }
        case .when:
            VStack(alignment: .leading, spacing: 20) {
                Spacer()
                kicker("When you listen")
                if let peak = r.byHour.indices.max(by: { r.byHour[$0] < r.byHour[$1] }), r.byHour[peak] > 0 {
                    Text("Mostly around \(hourLabel(peak))").font(.system(size: isTV ? 60 : 32, weight: .black))
                }
                bars(r.byHour.map(Double.init), labels: ["12a", "6a", "12p", "6p"], height: isTV ? 200 : 120)
                Text("By month").font(.headline).padding(.top, 8)
                bars(r.byMonth.map(Double.init), labels: ["Jan", "Apr", "Jul", "Oct"], height: isTV ? 160 : 90)
                Spacer()
            }
        case .days:
            VStack(alignment: .leading, spacing: 26) {
                Spacer()
                kicker("Your biggest day")
                if let d = r.topDay {
                    Text(dayLabel(d.date)).font(.system(size: isTV ? 70 : 38, weight: .black)).minimumScaleFactor(0.5)
                    Text("\(d.minutes.formatted()) minutes of music").font(.title3.bold()).opacity(0.9)
                } else {
                    Text("Every day counts").font(.title.bold())
                }
                kicker("Longest streak").padding(.top, 20)
                Text("\(r.longestStreakDays) \(r.longestStreakDays == 1 ? "day" : "days") in a row")
                    .font(.system(size: isTV ? 70 : 38, weight: .black)).minimumScaleFactor(0.5)
                Spacer()
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        case .discovered:
            VStack(alignment: .leading, spacing: 18) {
                Spacer()
                kicker("New discoveries")
                Text(r.newArtists.formatted()).font(.system(size: isTV ? 200 : 96, weight: .black))
                Text(r.newArtists == 1 ? "new artist found its way to you" : "new artists found their way to you").font(.title2.bold())
                if let first = r.firstTrack {
                    HStack(spacing: 12) {
                        ArtworkView(item: first, shape: .square, width: 56).frame(width: 56)
                        VStack(alignment: .leading) {
                            Text("Your first song of the year").font(.caption).opacity(0.8)
                            Text(first.title).font(.headline)
                            Text(first.artistCredit ?? first.grandparentTitle ?? "").font(.caption).opacity(0.8)
                        }
                    }
                    .padding(.top, 20)
                }
                Spacer()
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        case .summary:
            summary(r)
        }
    }

    private func summary(_ r: ListeningRecap) -> some View {
        VStack(spacing: 18) {
            Spacer(minLength: 0)
            Text("Your \(String(r.year)) in Music").font(.system(size: isTV ? 64 : 34, weight: .black)).multilineTextAlignment(.center)
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 16) {
                stat("Minutes", r.minutes.formatted())
                stat("Plays", r.plays.formatted())
                stat("Top artist", r.topArtists.first?.item.title ?? "–")
                stat("Top song", r.topTracks.first?.item.title ?? "–")
                stat("Top genre", r.topGenres.first?.name ?? "–")
                stat("Longest streak", "\(r.longestStreakDays) d")
            }
            .padding()
            .background(.black.opacity(0.25), in: RoundedRectangle(cornerRadius: 18))
            if !r.topTracks.isEmpty {
                VStack(spacing: 12) {
                    playTopSongs(r)
                    Button { save(r) } label: {
                        HStack {
                            if saving { ProgressView() }
                            Label("Save as Playlist", systemImage: "text.badge.plus")
                        }
                        .frame(maxWidth: isTV ? nil : .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(.black.opacity(0.35))
                    .foregroundStyle(.white)
                    .disabled(saving)
                }
            }
            Spacer(minLength: 0)
        }
    }

    private func playTopSongs(_ r: ListeningRecap) -> some View {
        Button {
            music.play(r.topTracks.map(\.item), source: "Your Top Songs \(String(r.year))")
        } label: {
            Label("Play Your Top Songs", systemImage: "play.fill").frame(maxWidth: isTV ? nil : .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(.white)
        .foregroundStyle(.black)
    }

    private func save(_ r: ListeningRecap) {
        saving = true
        Task {
            defer { saving = false }
            do {
                let p = try await app.saveRecapPlaylist(year: r.year)
                withAnimation { saved = p }
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    private func kicker(_ s: String) -> some View {
        Text(s.uppercased()).font((isTV ? Font.title3 : .subheadline).weight(.heavy)).tracking(1.5).opacity(0.85)
    }

    private func stat(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label.uppercased()).font(.caption2.weight(.heavy)).opacity(0.75)
            Text(value).font(.title3.weight(.black)).lineLimit(1).minimumScaleFactor(0.6)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func ranked(_ title: String, _ entries: [RecapEntry], square: Bool) -> some View {
        VStack(alignment: .leading, spacing: isTV ? 18 : 14) {
            Spacer(minLength: 0)
            kicker(title)
            ForEach(Array(entries.enumerated()), id: \.offset) { i, e in
                HStack(spacing: 16) {
                    Text("\(i + 1)").font(.system(size: isTV ? 50 : 30, weight: .black).monospacedDigit()).frame(width: isTV ? 60 : 36, alignment: .trailing)
                    ArtworkView(item: e.item, shape: .square, width: isTV ? 100 : 56).frame(width: isTV ? 100 : 56)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(e.item.title).font(i == 0 ? .title2.weight(.black) : .headline).lineLimit(1)
                        Text("\(e.plays.formatted()) plays").font(.caption).opacity(0.8)
                    }
                    Spacer()
                }
            }
            Spacer(minLength: 0)
        }
    }

    private func bars(_ values: [Double], labels: [String], height: CGFloat) -> some View {
        let most = max(values.max() ?? 1, 1)
        return VStack(spacing: 4) {
            HStack(alignment: .bottom, spacing: 3) {
                ForEach(Array(values.enumerated()), id: \.offset) { _, v in
                    RoundedRectangle(cornerRadius: 2).fill(.white.opacity(v == most ? 1 : 0.6))
                        .frame(height: max(3, height * v / most))
                }
            }
            .frame(height: height, alignment: .bottom)
            HStack {
                ForEach(labels, id: \.self) { l in Text(l).font(.caption2).opacity(0.75).frame(maxWidth: .infinity, alignment: .leading) }
            }
        }
        .accessibilityHidden(true)
    }

    private func hourLabel(_ h: Int) -> String {
        var c = DateComponents()
        c.hour = h
        guard let d = Calendar.current.date(from: c) else { return "\(h):00" }
        return d.formatted(date: .omitted, time: .shortened)
    }

    private func dayLabel(_ iso: String) -> String {
        guard let d = try? Date(iso, strategy: .iso8601.year().month().day()) else { return iso }
        return d.formatted(.dateTime.weekday(.wide).month(.wide).day())
    }
}

