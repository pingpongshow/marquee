import AVKit
import MarqueeKit
import SwiftUI

/// Live TV (LIVE-2): a Guide and What's On, a live preview on iPhone/iPad, and
/// full-screen watching (swipe to change channel; channel up/down on Apple TV). Movies on demand (VOD) have their own page.
struct LiveTVView: View {
    @Environment(AppSession.self) private var app
    @State private var tab: Tab
    @State private var filter = "all" // all, favorites, or a group
    @State private var groups: [String] = []
    @State private var channels: [LiveChannel] = []
    @State private var guide: [Int64: [LiveProgramme]] = [:]
    @State private var start = LiveTVView.defaultStart()
    @State private var preview: LiveChannel?
    @State private var watching: WatchTarget?
    @State private var details: (LiveProgramme, LiveChannel)?
    @State private var error: String?
    @State private var enabled: Bool?
    @State private var canRecord = false
    /// May see the Recordings tab (separate from recording since LIVE-5 sharing).
    @State private var canViewRecordings = false
    /// Why the Live TV status couldn't be read (not the same as "not set up").
    @State private var statusError: String?
    /// Bumped by pull-to-refresh, and after a recording is stopped.
    @State private var recordingsRefresh = 0
    /// Recordings stopped here: hidden from Recording now at once (the server can take a few
    /// seconds to finish them).
    @State private var stoppedRecordings: Set<Int64> = []
    /// Why the channel just tapped can't be watched (its connections are in use).
    @State private var unavailableNotice: String?
    enum Tab: String, CaseIterable { case guide = "Guide", now = "What's On", recordings = "Recordings" }
    init(initialTab: Tab = .guide) {
        _tab = State(initialValue: initialTab)
    }

    private var tabs: [Tab] {
        canViewRecordings ? [.guide, .now, .recordings] : [.guide, .now]
    }

    static func defaultStart() -> Date {
        let cal = Calendar.current
        var c = cal.dateComponents([.year, .month, .day, .hour, .minute], from: .now)
        c.minute = (c.minute ?? 0) < 30 ? 0 : 30
        return cal.date(from: c)!.addingTimeInterval(-30 * 60)
    }

    var body: some View {
        Group {
            if let statusError, enabled == nil {
                ContentUnavailableView {
                    Label("Couldn't reach Live TV", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(statusError)
                } actions: {
                    Button("Try Again") { Task { await loadStatus() } }
                }
            } else if enabled == nil {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if enabled == false {
                ContentUnavailableView("Live TV isn't set up", systemImage: "tv", description: Text("An admin can add Dispatcharr or an M3U playlist in the web app's Settings → Live TV."))
            } else {
                content
            }
        }
        // (No title on the TV: MainView hides it there, as the sidebar names the page.)
        .navigationTitle("Live TV")
        // Checked again each time Live TV opens: channels may have been added or lost (LIVE-1).
        .task { await loadStatus() }
        .task(id: "\(filter)|\(start.timeIntervalSince1970)") { await load() }
        // Which channels can be tuned changes as connections are used and freed.
        .task(id: enabled) {
            while enabled == true {
                do { try await Task.sleep(for: .seconds(60)) } catch { return }
                await refreshChannels()
            }
        }
        .alert("Channel unavailable", isPresented: Binding(get: { unavailableNotice != nil }, set: { if !$0 { unavailableNotice = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(unavailableNotice ?? "")
        }
        #if os(iOS)
        .fullScreenCover(item: $watching) { LiveWatchView(channels: channels, start: $0.id) }
        #else
        .fullScreenCover(item: $watching) { LiveWatchView(channels: channels, start: $0.id) }
        #endif
        .sheet(isPresented: Binding(get: { details != nil }, set: { if !$0 { details = nil } })) {
            if let (p, c) = details {
                ProgrammeSheet(programme: p, channel: c, canRecord: canRecord, onStopped: { recordingStopped($0) }) { recordingsRefresh += 1; Task { await load() } }
            }
        }
    }

    private func loadStatus() async {
        do {
            let status = try await app.liveStatus()
            enabled = status.enabled
            canRecord = status.canRecord ?? false
            canViewRecordings = status.mayViewRecordings
            statusError = nil
            if !tabs.contains(tab) { tab = .guide }
            guard enabled == true else { return }
            groups = (try? await app.liveGroups()) ?? []
            if channels.isEmpty { await load() }
        } catch is CancellationError {
        } catch {
            statusError = error.localizedDescription
        }
    }

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if canViewRecordings {
                    RecordingNowStrip(refresh: recordingsRefresh, stopped: stoppedRecordings) { recordingStopped($0) }
                    .padding(.horizontal, sidePadding)
                }
                controls
                channelContent
            }
            .padding(.vertical)
        }
        // On the scroll view, where pull-to-refresh works (it did nothing on the VStack inside).
        .refreshable {
            recordingsRefresh += 1
            if tab != .recordings { await load() }
        }
    }

    @ViewBuilder private var channelContent: some View {
        #if os(iOS)
        if tab != .recordings, let c = preview ?? channels.first(where: \.isAvailable) ?? channels.first { LivePreview(channel: c) { watching = WatchTarget(id: c.id) } }
        #endif
        if let error { Text(error).foregroundStyle(.red).padding(.horizontal, sidePadding) }
        if tab == .recordings {
            RecordingsList(refresh: recordingsRefresh, canRecord: canRecord, stopped: stoppedRecordings) { recordingStopped($0) }.padding(.horizontal, sidePadding)
        } else if channels.isEmpty, enabled == true {
            Text(filter == "favorites" ? "No favourites yet. Add some with the heart next to a channel."
                : filter == "recent" ? "Channels you watch will appear here."
                : filter == "hidden" ? "No hidden channels. Touch and hold a channel to hide it." : "No channels.")
                .foregroundStyle(.secondary).padding(.horizontal, sidePadding)
        } else if tab == .guide {
            GuideGrid(channels: channels, guide: guide, start: start, selected: preview?.id,
                      onChannel: { c in pick(c) },
                      onProgramme: { p, c in programmeTapped(p, c) },
                      onFavorite: { c in toggleFavorite(c) },
                      onHide: { c in toggleHidden(c) },
                      onRecord: canRecord ? { p, c, series in record(p, c, series: series) } : nil)
        } else {
            whatsOn
        }
    }

    /// On now: preview it (iPhone/iPad) or watch it (Apple TV); a second tap on the channel
    /// being previewed, or anything later, shows the programme's details.
    private func programmeTapped(_ p: LiveProgramme, _ c: LiveChannel) {
        // An unavailable channel's programme shows its details (which say why) instead.
        #if os(tvOS)
        if p.isOn, c.isAvailable { pick(c) } else { details = (p, c) }
        #else
        if p.isOn, c.isAvailable, (preview ?? channels.first(where: \.isAvailable))?.id != c.id { pick(c) } else { details = (p, c) }
        #endif
    }

    private func record(_ p: LiveProgramme, _ c: LiveChannel, series: Bool) {
        Task {
            do {
                try await app.record(p, on: c.id, series: series)
                await load()
            } catch {
                self.error = error.localizedDescription
            }
        }
    }

    /// iPhone/iPad preview a channel first; Apple TV goes straight to full screen.
    private func pick(_ c: LiveChannel) {
        guard c.isAvailable else { unavailableNotice = c.unavailableText; return }
        #if os(tvOS)
        watching = WatchTarget(id: c.id)
        #else
        if preview?.id == c.id { watching = WatchTarget(id: c.id) } else { preview = c }
        #endif
    }

    private var controls: some View {
        VStack(alignment: .leading, spacing: 12) {
            if tabs.count > 1 {
                Picker("View", selection: $tab) {
                    ForEach(tabs, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
            }
            if tab == .guide || tab == .now { filterBar }
        }
        .padding(.horizontal, sidePadding)
    }

    private var filterBar: some View {
        HStack(spacing: 12) {
            Menu {
                Button("All Channels") { filter = "all" }
                Button("Favorites") { filter = "favorites" }
                Button("Recently Watched") { filter = "recent" }
                Button("Hidden Channels") { filter = "hidden" }
                if !groups.isEmpty {
                    Section("Groups") { ForEach(groups, id: \.self) { g in Button(g) { filter = g } } }
                }
            } label: {
                Label(["all": "All Channels", "favorites": "Favorites", "recent": "Recently Watched", "hidden": "Hidden Channels"][filter] ?? filter,
                      systemImage: "line.3.horizontal.decrease.circle")
            }
            Spacer()
            if tab == .guide {
                Button { start = start.addingTimeInterval(-90 * 60) } label: { Image(systemName: "chevron.left") }
                    .accessibilityLabel("Earlier")
                    .disabled(start < Date().addingTimeInterval(-3 * 3600))
                Text(start, format: .dateTime.weekday(.abbreviated).hour().minute()).font(.callout).monospacedDigit()
                Button { start = start.addingTimeInterval(90 * 60) } label: { Image(systemName: "chevron.right") }
                    .accessibilityLabel("Later")
                Button("Now") { start = LiveTVView.defaultStart() }
            }
        }
    }

    private var whatsOn: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: tileWidth), spacing: 16)], spacing: 16) {
            ForEach(channels, id: \.id) { c in
                Button {
                    if c.isAvailable { watching = WatchTarget(id: c.id) } else { unavailableNotice = c.unavailableText }
                } label: {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(spacing: 10) {
                            ChannelLogo(channel: c).frame(width: 64, height: 40)
                            VStack(alignment: .leading) {
                                Text(c.now?.title ?? c.name).font(.headline).lineLimit(1)
                                Text(c.now.map { "\($0.minutesLeft)m left · \(c.name)" } ?? c.name).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                        if let p = c.now { ProgressView(value: p.progress).tint(Color.marqueeGold) }
                        if !c.isAvailable {
                            Text(c.unavailableText).font(.caption2).foregroundStyle(.secondary).lineLimit(2)
                        } else if let n = c.next {
                            Text("Next: \(n.title) at \(n.start.formatted(date: .omitted, time: .shortened))").font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 10))
                    .opacity(c.isAvailable ? 1 : 0.45)
                }
                .buttonStyle(cardButtonStyle)
                .accessibilityLabel(c.isAvailable ? "Watch \(c.name)" : "\(c.name), unavailable: \(c.unavailableText)")
            }
        }
        .padding(.horizontal, sidePadding)
    }

    private func load() async {
        guard enabled == true else { return }
        let group = ["all", "favorites", "recent", "hidden"].contains(filter) ? nil : filter
        do {
            channels = try await app.liveChannels(group: group, favorites: filter == "favorites", recent: filter == "recent", hidden: filter == "hidden")
            guide = filter == "hidden" ? [:] : try await app.liveGuide(from: start, to: start.addingTimeInterval(GuideGrid.hours * 3600), group: group,
                                                                       favorites: filter == "favorites", recent: filter == "recent")
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// A recording in progress was stopped: hide it from Recording now, and reload (its
    /// channel's connection is free again).
    private func recordingStopped(_ id: Int64) {
        stoppedRecordings.insert(id)
        recordingsRefresh += 1
        Task { await load() }
    }

    /// Just the channel list (now/next and availability), without the guide.
    private func refreshChannels() async {
        guard enabled == true else { return }
        let group = ["all", "favorites", "recent", "hidden"].contains(filter) ? nil : filter
        if let list = try? await app.liveChannels(group: group, favorites: filter == "favorites", recent: filter == "recent", hidden: filter == "hidden") {
            channels = list
        }
    }

    private func toggleHidden(_ c: LiveChannel) {
        Task {
            try? await app.setHidden(c.id, c.hidden != true)
            await load()
        }
    }

    private func toggleFavorite(_ c: LiveChannel) {
        Task {
            try? await app.setFavorite(c.id, !c.favorite)
            await load()
        }
    }

    #if os(tvOS)
    private let tileWidth: CGFloat = 380
    private var cardButtonStyle: some PrimitiveButtonStyle { .card }
    #else
    private let tileWidth: CGFloat = 260
    private var cardButtonStyle: some PrimitiveButtonStyle { .plain }
    #endif
}

struct WatchTarget: Identifiable {
    let id: Int64
}

/// The guide: a fixed channel column beside one horizontally scrolling time grid.
struct GuideGrid: View {
    static let hours: Double = 4
    let channels: [LiveChannel]
    let guide: [Int64: [LiveProgramme]]
    let start: Date
    let selected: Int64?
    let onChannel: (LiveChannel) -> Void
    let onProgramme: (LiveProgramme, LiveChannel) -> Void
    let onFavorite: (LiveChannel) -> Void
    var onHide: ((LiveChannel) -> Void)?
    /// Record (series = every airing); nil when the viewer can't record.
    var onRecord: ((LiveProgramme, LiveChannel, Bool) -> Void)?

    #if os(tvOS)
    private let perMinute: CGFloat = 9
    private let row: CGFloat = 96
    private let column: CGFloat = 220
    #else
    private let perMinute: CGFloat = 5
    private let row: CGFloat = 64
    private let column: CGFloat = 104
    #endif

    private var width: CGFloat { CGFloat(Self.hours * 60) * perMinute }
    private func x(_ d: Date) -> CGFloat { CGFloat(d.timeIntervalSince(start) / 60) * perMinute }

    var body: some View {
        // Redrawn once a minute: the now line and "…m left" move on.
        TimelineView(.periodic(from: .now, by: 60)) { context in
            HStack(alignment: .top, spacing: 0) {
                // Lazy: only the channels on screen are built (hundreds of channels).
                LazyVStack(spacing: 0) {
                    Color.clear.frame(height: 28)
                    ForEach(channels, id: \.id) { c in channelCell(c).frame(height: row) }
                }
                .frame(width: column)
                ScrollView(.horizontal, showsIndicators: false) {
                    ZStack(alignment: .topLeading) {
                        LazyVStack(alignment: .leading, spacing: 0) {
                            timeline
                            ForEach(channels, id: \.id) { c in
                                programmes(c, now: context.date).frame(width: width, height: row, alignment: .leading)
                                    .opacity(c.isAvailable ? 1 : 0.5)
                            }
                        }
                        let now = x(context.date)
                        if now > 0 && now < width {
                            Rectangle().fill(Color.marqueeGold).frame(width: 2).offset(x: now).allowsHitTesting(false)
                                .accessibilityHidden(true)
                        }
                    }
                }
            }
        }
        .padding(.horizontal, sidePadding)
    }

    private var timeline: some View {
        HStack(spacing: 0) {
            ForEach(0..<Int(Self.hours * 2), id: \.self) { i in
                Text(start.addingTimeInterval(Double(i) * 1800), format: .dateTime.hour().minute())
                    .font(.caption).foregroundStyle(.secondary)
                    .frame(width: 30 * perMinute, alignment: .leading)
            }
        }
        .frame(height: 28)
    }

    private func channelCell(_ c: LiveChannel) -> some View {
        HStack(spacing: 6) {
            Button { onChannel(c) } label: {
                VStack(spacing: 2) {
                    ChannelLogo(channel: c).frame(height: row * 0.45)
                    if let n = c.number { Text(n).font(.caption2).foregroundStyle(.secondary) }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.plain)
            .opacity(c.isAvailable ? 1 : 0.4)
            .accessibilityLabel(c.isAvailable ? "\(selected == c.id ? "Watch" : "Preview") \(c.name)" : "\(c.name), unavailable: \(c.unavailableText)")
            Button { onFavorite(c) } label: {
                Image(systemName: c.favorite ? "heart.fill" : "heart").foregroundStyle(c.favorite ? Color.marqueeGold : .secondary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(c.favorite ? "Remove \(c.name) from favourites" : "Add \(c.name) to favourites")
        }
        .padding(.horizontal, 4)
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(selected == c.id ? Color.marqueeGold : .clear))
        .contextMenu {
            if let onHide {
                Button(c.hidden == true ? "Show in Guide" : "Hide Channel", systemImage: c.hidden == true ? "eye" : "eye.slash") { onHide(c) }
            }
        }
    }

    private func programmes(_ c: LiveChannel, now: Date) -> some View {
        ZStack(alignment: .leading) {
            ForEach(guide[c.id] ?? [], id: \.id) { p in
                let left = max(0, x(p.start))
                let right = min(width, x(p.end))
                if right > left {
                    Button { onProgramme(p, c) } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            HStack(spacing: 4) {
                                if p.recording != nil {
                                    Circle().fill(.red).frame(width: 7, height: 7)
                                        .accessibilityLabel(p.recording == .recording ? "Recording" : "Will record")
                                }
                                Text(p.title).font(.subheadline.weight(.medium)).lineLimit(1)
                            }
                            Text(p.isOn ? "\(p.minutesLeft)m left" : p.start.formatted(date: .omitted, time: .shortened))
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        .padding(.horizontal, 8)
                        .frame(width: right - left - 4, height: row - 10, alignment: .leading)
                        .background(p.isOn ? Color.secondary.opacity(0.3) : Color.secondary.opacity(0.15), in: RoundedRectangle(cornerRadius: 6))
                    }
                    .buttonStyle(.plain)
                    .contextMenu {
                        if let onRecord, p.end > .now {
                            if p.recording == nil { Button("Record", systemImage: "record.circle") { onRecord(p, c, false) } }
                            if p.series != true { Button("Record Series", systemImage: "square.stack") { onRecord(p, c, true) } }
                        }
                    }
                    .offset(x: left + 2)
                    .accessibilityLabel("\(p.title), \(p.isOn ? "\(p.minutesLeft) minutes left" : p.start.formatted(date: .omitted, time: .shortened)), \(c.name)\(p.recording != nil ? ", will record" : "")")
                }
            }
        }
    }
}

struct ChannelLogo: View {
    @Environment(AppSession.self) private var app
    let channel: LiveChannel
    var body: some View {
        CachedImage(url: app.logoURL(channel)) { phase in
            if let img = phase.image {
                img.resizable().scaledToFit()
            } else {
                Text(String(channel.name.split(separator: " ").compactMap(\.first).prefix(3)))
                    .font(.caption2.weight(.semibold)).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 4))
            }
        }
    }
}

#if os(iOS)
/// A muted live preview with "Now On" (iPhone/iPad).
struct LivePreview: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    let channel: LiveChannel
    let onExpand: () -> Void
    @State private var player = AVPlayer()
    @State private var session: String?
    @State private var muted = true
    @State private var error: String?

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            VideoPlayer(player: player).disabled(true)
            HStack(spacing: 10) {
                ChannelLogo(channel: channel).frame(width: 48, height: 28)
                Text(error ?? (channel.isAvailable ? "Now On: \(channel.now?.title ?? channel.name)" : channel.unavailableText)).font(.footnote).lineLimit(1)
                    .foregroundStyle(error == nil ? Color.white : Color.red)
                Spacer()
            }
            .padding(10)
            .background(LinearGradient(colors: [.clear, .black.opacity(0.8)], startPoint: .top, endPoint: .bottom))
        }
        .overlay(alignment: .top) {
            HStack {
                Button {
                    muted.toggle()
                    player.isMuted = muted
                    if !muted { music.pause() }
                } label: { Image(systemName: muted ? "speaker.slash.fill" : "speaker.wave.2.fill") }
                    .accessibilityLabel(muted ? "Unmute" : "Mute")
                Spacer()
                Button(action: onExpand) { Image(systemName: "arrow.up.left.and.arrow.down.right") }
                    .accessibilityLabel("Watch full screen")
            }
            .buttonStyle(.bordered).tint(.white)
            .padding(8)
        }
        .aspectRatio(16 / 9, contentMode: .fit)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .padding(.horizontal, sidePadding)
        .task(id: channel.id) { await tune() }
        .onDisappear { stop() }
    }

    private func tune() async {
        stop()
        error = nil
        guard channel.isAvailable else { return } // its connections are in use; the caption says why
        var retried = false
        while !Task.isCancelled {
            do {
                let s = try await app.playLive(channel.id)
                // Moved on (another channel, or the preview went away) while tuning: let it go.
                if Task.isCancelled { await app.stopLive(s.id); return }
                session = s.id
                player.replaceCurrentItem(with: AVPlayerItem(url: s.url))
                player.isMuted = muted
                player.play()
                error = nil
                guard let reason = await liveStreamEnd(app, player: player, url: s.url) else { return }
                session = nil // the server already ended it
                player.replaceCurrentItem(with: nil)
                if reason.hasPrefix("Stopped:") || retried { error = reason.isEmpty ? "The stream stopped." : reason; return }
                retried = true // ended for a reason tuning again fixes: once
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
                return
            }
        }
    }

    private func stop() {
        player.replaceCurrentItem(with: nil)
        if let s = session { session = nil; Task { await app.stopLive(s) } }
    }
}
#endif

/// Full-screen live TV. iPhone/iPad: swipe left/right on the video to change channel, swipe
/// down from the top to close. Apple TV: the remote's up/down changes channel.
struct LiveWatchView: View {
    @Environment(AppSession.self) private var app
    @Environment(MusicPlayer.self) private var music
    @Environment(\.dismiss) private var dismiss
    let channels: [LiveChannel]
    @State private var current: Int64
    @State private var player = AVPlayer()
    @State private var session: String?
    @State private var error: String?
    #if os(iOS)
    /// The channel overlay shown briefly after a swipe changes channel.
    @State private var overlayChannel: LiveChannel?
    @State private var overlayToken = 0
    /// How far the view follows a swipe down from the top.
    @State private var pullDown: CGFloat = 0
    #endif

    init(channels: [LiveChannel], start: Int64) {
        self.channels = channels
        _current = State(initialValue: start)
    }

    private var channel: LiveChannel? { channels.first { $0.id == current } }

    var body: some View {
        #if os(iOS)
        GeometryReader { geo in
            content
                .offset(y: pullDown)
                .simultaneousGesture(swipes(geo.size))
        }
        #else
        content
            .onMoveCommand { dir in
                if dir == .up { step(-1) } else if dir == .down { step(1) }
            }
        #endif
    }

    private var content: some View {
        ZStack(alignment: .top) {
            Color.black.ignoresSafeArea()
            VideoPlayer(player: player).ignoresSafeArea()
                .accessibilityLabel("Live TV")
                .accessibilityValue(channel?.name ?? "")
                .accessibilityIdentifier("liveWatch.player")
            #if os(iOS)
            header
            if let c = overlayChannel { channelOverlay(c).frame(maxHeight: .infinity) }
            #endif
            if let error { Text(error).foregroundStyle(.red).padding(.top, 80) }
        }
        .task(id: current) { await tune() }
        .onDisappear { stop() }
    }

    #if os(iOS)
    private var header: some View {
        HStack(spacing: 12) {
            Button { dismiss() } label: { Image(systemName: "chevron.down") }.accessibilityLabel("Close Live TV")
            if let c = channel {
                ChannelLogo(channel: c).frame(width: 56, height: 32)
                VStack(alignment: .leading) {
                    Text(c.now?.title ?? c.name).font(.headline).lineLimit(1)
                    Text([c.name, c.next.map { "Next: \($0.title)" }].compactMap { $0 }.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("liveWatch.nowPlaying")
            }
            Spacer()
        }
        .buttonStyle(.bordered).tint(.white)
        .padding()
        .background(LinearGradient(colors: [.black.opacity(0.8), .clear], startPoint: .top, endPoint: .bottom))
    }

    /// Number, logo and name of the channel being switched to.
    private func channelOverlay(_ c: LiveChannel) -> some View {
        HStack(spacing: 14) {
            if let n = c.number { Text(n).font(.title.monospacedDigit().bold()) }
            ChannelLogo(channel: c).frame(width: 72, height: 40)
            Text(c.name).font(.title3.weight(.semibold)).lineLimit(1)
        }
        .foregroundStyle(.white)
        .padding(.horizontal, 20).padding(.vertical, 14)
        .background(.black.opacity(0.7), in: RoundedRectangle(cornerRadius: 14))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(c.name)
        .accessibilityValue(c.number.map { "Channel \($0)" } ?? "")
        .accessibilityIdentifier("liveWatch.channelOverlay")
        .allowsHitTesting(false)
        .transition(.opacity)
    }

    /// Swipe left/right to change channel; swipe down from the top quarter to close. Runs
    /// alongside the player's own gestures, so a tap still shows or hides its controls; a
    /// horizontal swipe along the bottom (where the player's scrub bar sits) is left to it.
    private func swipes(_ size: CGSize) -> some Gesture {
        DragGesture(minimumDistance: 20, coordinateSpace: .local)
            .onChanged { v in
                let fromTop = v.startLocation.y < size.height * 0.25
                let dx = v.translation.width, dy = v.translation.height
                if fromTop, dy > 0, dy > abs(dx) { pullDown = dy } else if pullDown != 0 { pullDown = 0 }
            }
            .onEnded { v in
                let fromTop = v.startLocation.y < size.height * 0.25
                let dx = v.translation.width, dy = v.translation.height
                let fling = v.predictedEndTranslation.height
                if fromTop, dy > abs(dx), dy > size.height * 0.2 || fling > size.height * 0.4 {
                    dismiss()
                    return
                }
                withAnimation(.spring(duration: 0.25)) { pullDown = 0 }
                let inScrubBar = v.startLocation.y > size.height - 110
                guard !inScrubBar, abs(dx) > 60, abs(dx) > abs(dy) * 1.5 else { return }
                step(dx < 0 ? 1 : -1)
                showOverlay()
            }
    }

    private func showOverlay() {
        overlayToken += 1
        let token = overlayToken
        withAnimation(.easeOut(duration: 0.15)) { overlayChannel = channel }
        Task {
            try? await Task.sleep(for: .seconds(2.5))
            if token == overlayToken { withAnimation(.easeIn(duration: 0.3)) { overlayChannel = nil } }
        }
    }
    #endif

    /// The next channel up or down, skipping those that can't be tuned right now.
    private func step(_ d: Int) {
        guard let i = channels.firstIndex(where: { $0.id == current }), !channels.isEmpty else { return }
        for n in 1..<max(2, channels.count) {
            let c = channels[((i + d * n) % channels.count + channels.count) % channels.count]
            if c.isAvailable || c.id == current { current = c.id; return }
        }
    }

    private func tune() async {
        stop()
        var retried = false
        while !Task.isCancelled {
            do {
                let s = try await app.playLive(current)
                // Changed channel or closed while tuning: stop the session that just started.
                if Task.isCancelled { await app.stopLive(s.id); return }
                session = s.id
                let item = AVPlayerItem(url: s.url)
                if let c = channel { item.externalMetadata = metadata(c) }
                player.replaceCurrentItem(with: item)
                music.pause() // live TV takes over from the music
                player.play()
                error = nil
                // The server ended the stream: "Stopped: …" is shown; any other reason means
                // tuning the channel again works, which is tried once.
                guard let reason = await liveStreamEnd(app, player: player, url: s.url) else { return }
                session = nil
                player.replaceCurrentItem(with: nil)
                if reason.hasPrefix("Stopped:") || retried { error = reason.isEmpty ? "The stream stopped." : reason; return }
                retried = true
            } catch {
                if !Task.isCancelled { self.error = error.localizedDescription }
                return
            }
        }
    }

    private func metadata(_ c: LiveChannel) -> [AVMetadataItem] {
        let title = AVMutableMetadataItem()
        title.identifier = .commonIdentifierTitle
        title.value = (c.now?.title ?? c.name) as NSString
        let sub = AVMutableMetadataItem()
        sub.identifier = .iTunesMetadataTrackSubTitle
        sub.value = c.name as NSString
        return [title, sub]
    }

    private func stop() {
        player.replaceCurrentItem(with: nil)
        if let s = session { session = nil; Task { await app.stopLive(s) } }
    }
}

/// Waits until the live stream the player is on has ended and returns the server's reason
/// (see `liveEndReason`); nil when cancelled first. Only asks while the player isn't playing.
@MainActor
func liveStreamEnd(_ app: AppSession, player: AVPlayer, url: URL) async -> String? {
    while !Task.isCancelled {
        do { try await Task.sleep(for: .seconds(3)) } catch { return nil }
        guard player.currentItem?.status == .failed || player.timeControlStatus != .playing else { continue }
        if let reason = await app.liveEndReason(url) { return Task.isCancelled ? nil : reason }
    }
    return nil
}

/// A future programme's details.
struct ProgrammeSheet: View {
    let programme: LiveProgramme
    let channel: LiveChannel
    var canRecord = false
    var onStopped: (Int64) -> Void = { _ in }
    var onChange: () -> Void = {}
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var recording: Recording?
    @State private var loaded = false
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                HStack { ChannelLogo(channel: channel).frame(width: 64, height: 36); Text(channel.name).foregroundStyle(.secondary) }
                Text(programme.title).font(.title3.bold())
                Text("\(programme.start.formatted(.dateTime.weekday(.wide).hour().minute())) – \(programme.end.formatted(date: .omitted, time: .shortened))")
                if let e = programme.episode { Text([e, programme.subtitle].compactMap { $0 }.joined(separator: " · ")).foregroundStyle(.secondary) }
                if let d = programme.description { Text(d) }
                if !channel.isAvailable { Label(channel.unavailableText, systemImage: "exclamationmark.circle").foregroundStyle(.secondary) }
                if canRecord, programme.end > .now, loaded {
                    Section {
                        if programme.series == true { Text("A series recording covers this title.").font(.footnote).foregroundStyle(.secondary) }
                        if let recording, recording.canDelete == false {
                            // Someone else's: they (or an admin) decide, so no cancel here.
                            Text("\(recording.status == .recording ? "Being recorded" : "Will be recorded")\(recording.ownerName.map { " by \($0)" } ?? "")")
                                .font(.footnote).foregroundStyle(.secondary)
                        } else if let recording, recording.status == .recording {
                            Text("Recording · \(recording.progressLabel())").font(.footnote).foregroundStyle(.secondary)
                            StopRecordingButtons(recording: recording, onDone: { onStopped(recording.id); dismiss() }, onError: { error = $0 })
                        } else if let recording {
                            Button("Don't Record", role: .destructive) {
                                act { try await app.cancelRecording(recording.id) }
                            }
                        } else {
                            Button("Record", systemImage: "record.circle") { act { try await app.record(programme, on: channel.id) } }
                        }
                        if programme.series != true {
                            Button("Record Series", systemImage: "square.stack") { act { try await app.record(programme, on: channel.id, series: true) } }
                        }
                        if let error { Text(error).foregroundStyle(.red) }
                    }
                    .disabled(busy)
                }
            }
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task {
                guard canRecord else { return }
                recording = try? await app.recordings().first {
                    $0.channelId == channel.id && $0.start == programme.start && ($0.status == .scheduled || $0.status == .recording)
                }
                loaded = true
            }
        }
    }

    private func act(_ f: @escaping () async throws -> Void) {
        busy = true
        Task {
            do {
                try await f()
                onChange()
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
        }
    }
}

/// Stop and keep / Stop and delete for a recording in progress (owner or admin); deleting
/// asks first.
struct StopRecordingButtons: View {
    @Environment(AppSession.self) private var app
    let recording: Recording
    var onDone: () -> Void = {}
    var onError: (String) -> Void = { _ in }
    @State private var busy = false
    @State private var confirmDelete = false

    var body: some View {
        HStack(spacing: 10) {
            Button("Stop and keep", systemImage: "stop.circle") { stop(keep: true) }
            #if os(tvOS)
            // A destructive bordered button is red on red on the TV; the confirmation says it.
            Button("Stop and delete", systemImage: "trash") { confirmDelete = true }
            #else
            Button("Stop and delete", systemImage: "trash", role: .destructive) { confirmDelete = true }
            #endif
        }
        .buttonStyle(.bordered)
        #if os(iOS)
        .controlSize(.small)
        #endif
        .disabled(busy)
        .confirmationDialog("Stop recording \(recording.title) and delete what has been recorded so far?",
                            isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Stop and delete", role: .destructive) { stop(keep: false) }
            Button("Cancel", role: .cancel) {}
        }
    }

    private func stop(keep: Bool) {
        busy = true
        Task {
            do {
                try await app.stopRecording(recording.id, keep: keep)
                onDone()
            } catch {
                onError(error.localizedDescription)
            }
            busy = false
        }
    }
}

/// Recordings in progress, at the top of every Live TV tab: channel, title, time in and
/// left, and Stop and keep / Stop and delete for whoever may (LIVE-5).
struct RecordingNowStrip: View {
    @Environment(AppSession.self) private var app
    /// Changed by the page's pull-to-refresh and after a stop elsewhere.
    var refresh = 0
    /// Stopped on this page (the server may still say recording for a moment).
    var stopped: Set<Int64> = []
    var onStopped: (Int64) -> Void = { _ in }
    @State private var all: [Recording] = []
    private var active: [Recording] { all.filter { !stopped.contains($0.id) } }
    @State private var error: String?

    var body: some View {
        // Redrawn every 30 seconds for the time in and left.
        TimelineView(.periodic(from: .now, by: 30)) { context in
            if !active.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    Label {
                        Text("Recording now").font(.headline)
                    } icon: {
                        Circle().fill(.red).frame(width: 10, height: 10)
                    }
                    ForEach(active, id: \.id) { r in
                        VStack(alignment: .leading, spacing: 6) {
                            Text(r.title).font(.body.weight(.medium)).lineLimit(1)
                            Text(([r.channelName, r.progressLabel(now: context.date)] + [r.ownerName].compactMap { $0 }).joined(separator: " · "))
                                .font(.caption).foregroundStyle(.secondary).lineLimit(2)
                            if r.canDelete != false {
                                StopRecordingButtons(recording: r, onDone: { onStopped(r.id) }, onError: { error = $0 })
                            }
                        }
                        .accessibilityElement(children: .contain)
                    }
                    if let error { Text(error).font(.caption).foregroundStyle(.red) }
                }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.red.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).stroke(Color.red.opacity(0.4)))
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("liveTV.recordingNow")
            }
        }
        .task(id: refresh) {
            // Checked again every 30 seconds: recordings start and finish on their own.
            while !Task.isCancelled {
                await load()
                do { try await Task.sleep(for: .seconds(30)) } catch { return }
            }
        }
    }

    private func load() async {
        guard let list = try? await app.recordings() else { return }
        all = list.filter { $0.status == .recording }
        if active.isEmpty { error = nil }
    }
}

/// The DVR (LIVE-5): recording now, upcoming, series and finished recordings.
struct RecordingsList: View {
    @Environment(AppSession.self) private var app
    @Environment(VideoPresenter.self) private var video
    /// Changed by the page's pull-to-refresh.
    var refresh = 0
    /// Show the signed-in user's recording space (only for those who may record).
    var canRecord = false
    /// Stopped on this page (the server may still say recording for a moment).
    var stopped: Set<Int64> = []
    /// After a recording in progress is stopped (the page refreshes the strip and the list).
    var onStopped: (Int64) -> Void = { _ in }
    /// All recordings, or only the ones the signed-in user scheduled (remembered on the device).
    @AppStorage("liveTV.recordingsOwner") private var ownerSetting = RecordingOwner.all.rawValue
    @State private var recordings: [Recording] = []
    @State private var usage: DvrUsage?
    @State private var rules: [RecordingRule] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var deleting: Recording?

    private var ownerFilter: RecordingOwner { RecordingOwner(rawValue: ownerSetting) ?? .all }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Picker("Show", selection: $ownerSetting) {
                Text("All recordings").tag(RecordingOwner.all.rawValue)
                Text("Only mine").tag(RecordingOwner.me.rawValue)
            }
            .pickerStyle(.segmented)
            .frame(maxWidth: 420)
            .accessibilityIdentifier("recordings.owner")
            if let usage, usage.quotaBytes > 0 || usage.usedBytes > 0 { usageMeter(usage) }
            if let error { Text(error).foregroundStyle(.red) }
            if loaded && recordings.isEmpty && rules.isEmpty {
                Text(ownerFilter == .me ? "You haven't recorded anything yet. Pick a programme in the guide and choose Record."
                    : "Nothing recorded yet. Pick a programme in the guide and choose Record.").foregroundStyle(.secondary)
            }
            section("Recording Now", recordings.filter { $0.status == .recording && !stopped.contains($0.id) }, below: { r in
                if mine(r.canDelete) {
                    StopRecordingButtons(recording: r, onDone: { onStopped(r.id) }, onError: { error = $0 })
                }
            }) { _ in EmptyView() }
            section("Upcoming", recordings.filter { $0.status == .scheduled }) { r in
                if mine(r.canDelete) { Button("Cancel") { act { try await app.cancelRecording(r.id) } } }
            }
            if !rules.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Series").font(.headline)
                    ForEach(rules, id: \.id) { s in
                        HStack {
                            VStack(alignment: .leading) {
                                Text(s.title).font(.body.weight(.medium))
                                Text(([s.channelName ?? "Any channel", "\(s.upcoming) upcoming"] + [owner(s.ownerName, userId: s.userId)].compactMap { $0 })
                                    .joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
                                if s.paused == true {
                                    Text("Paused: \(s.ownerName ?? "its owner") may no longer record").font(.caption).foregroundStyle(.orange)
                                }
                            }
                            Spacer()
                            if mine(s.canDelete) { Button("Stop Series") { act { try await app.stopSeries(s.id) } } }
                        }
                        .padding(12)
                        .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
                    }
                }
            }
            section("Recorded", recordings.filter { $0.status == .completed || $0.status == .failed }, below: { r in
                if r.status == .completed, let item = r.itemId { playRow(r, item: item) }
            }) { r in
                if !mine(r.canDelete) {
                    EmptyView()
                } else if r.status == .failed {
                    Button("Dismiss") { act { try await app.cancelRecording(r.id) } }
                } else {
                    Button("Delete", role: .destructive) { deleting = r }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .task(id: "\(refresh)|\(ownerSetting)") { await load() }
        .confirmationDialog("Delete this recording?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), presenting: deleting) { r in
            Button("Delete \(r.title)", role: .destructive) { act { try await app.cancelRecording(r.id, deleteFile: true) } }
        }
    }

    @ViewBuilder
    private func section<A: View, B: View>(_ title: String, _ list: [Recording],
                                           @ViewBuilder below: @escaping (Recording) -> B = { _ in EmptyView() },
                                           @ViewBuilder action: @escaping (Recording) -> A) -> some View {
        if !list.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text(title).font(.headline)
                ForEach(list, id: \.id) { r in
                    HStack(spacing: 10) {
                        if r.status == .recording { Circle().fill(.red).frame(width: 9, height: 9) }
                        VStack(alignment: .leading, spacing: 2) {
                            if let item = r.itemId {
                                NavigationLink(value: Route.item(item)) { Text(r.title).font(.body.weight(.medium)) }
                            } else {
                                Text(r.title).font(.body.weight(.medium))
                            }
                            Text(detail(r)).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                            if r.status == .failed { Text("Failed: \(r.error ?? "unknown error")").font(.caption).foregroundStyle(.red) }
                            if r.stoppedEarly == true { Text("Stopped early").font(.caption.weight(.medium)).foregroundStyle(.orange) }
                            below(r).padding(.top, 4)
                        }
                        Spacer()
                        action(r)
                    }
                    .padding(12)
                    .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
                }
            }
        }
    }

    /// Resume (or Play) a finished recording, with how much has been watched.
    private func playRow(_ r: Recording, item: Int64) -> some View {
        let offset = r.viewOffsetMs ?? 0, total = r.durationMs ?? 0
        return HStack(spacing: 10) {
            Button(offset > 0 ? "Resume" : "Play", systemImage: "play.fill") {
                video.play(item, startMs: offset > 0 ? offset : 0)
            }
            .buttonStyle(.borderedProminent)
            .tint(Color.marqueeGold)
            #if os(iOS)
            .controlSize(.small)
            #endif
            if offset > 0, total > 0 {
                ProgressView(value: min(1, Double(offset) / Double(total)))
                    .tint(Color.marqueeGold)
                    .frame(maxWidth: 160)
                    .accessibilityLabel("Watched so far")
                Text("\(formatTime(seconds: Double(offset) / 1000)) of \(formatTime(seconds: Double(total) / 1000))")
                    .font(.caption).foregroundStyle(.secondary).monospacedDigit()
            } else if r.watched == true {
                Text("Watched").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    private func detail(_ r: Recording) -> String {
        var parts = [r.episode, r.subtitle].compactMap { $0 }
        parts.append(r.channelName)
        if r.status == .recording {
            parts.append(r.progressLabel())
        } else {
            parts.append("\(r.start.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day().hour().minute()))–\(r.end.formatted(date: .omitted, time: .shortened))")
        }
        if let b = r.sizeBytes, b > 0 { parts.append(ByteCountFormatter.string(fromByteCount: b, countStyle: .file)) }
        if let o = owner(r.ownerName, userId: r.userId) { parts.append(o) }
        return parts.joined(separator: " · ")
    }

    /// Only whoever scheduled it, or an admin, may cancel or delete (older servers don't say: allow).
    private func mine(_ canDelete: Bool?) -> Bool { canDelete != false }

    /// "Recorded by Alex"; nothing when it's the signed-in user's own.
    private func owner(_ name: String?, userId: Int64?) -> String? {
        guard let name, !name.isEmpty else { return nil }
        if let userId, userId == app.me?.id { return nil }
        if userId == nil, name == app.me?.displayName { return nil }
        return "Recorded by \(name)"
    }

    /// "12.4 of 50 GB used" with a thin bar when there's a quota, and what being over it means.
    private func usageMeter(_ u: DvrUsage) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(u.label).font(.subheadline).foregroundStyle(u.overQuota ? Color.red : Color.secondary)
            if u.quotaBytes > 0 {
                ProgressView(value: u.fraction)
                    .tint(u.overQuota ? .red : u.fraction > 0.85 ? .orange : Color.marqueeGold)
                    .accessibilityLabel("Recording space used")
            }
            if let m = u.overQuotaMessage { Text(m).font(.caption).foregroundStyle(.red) }
        }
        .frame(maxWidth: 420, alignment: .leading)
        .accessibilityIdentifier("recordings.usage")
    }

    private func load() async {
        do {
            recordings = try await app.recordings(owner: ownerFilter)
            rules = try await app.recordingRules(owner: ownerFilter)
            usage = canRecord ? try? await app.recordingUsage() : nil
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }

    private func act(_ f: @escaping () async throws -> Void) {
        Task {
            do { try await f() } catch { self.error = error.localizedDescription }
            await load()
        }
    }
}
