import AVKit
import MarqueeKit
import SwiftUI

/// Live TV (LIVE-2): Plex-style Guide and What's On, a live preview on iPhone/iPad, and
/// full-screen watching with channel up/down.
struct LiveTVView: View {
    @Environment(AppSession.self) private var app
    @State private var tab = Tab.guide
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

    enum Tab: String, CaseIterable { case guide = "Guide", now = "What's On", recordings = "Recordings" }
    private var tabs: [Tab] { canRecord ? Tab.allCases : [.guide, .now] }

    static func defaultStart() -> Date {
        let cal = Calendar.current
        var c = cal.dateComponents([.year, .month, .day, .hour, .minute], from: .now)
        c.minute = (c.minute ?? 0) < 30 ? 0 : 30
        return cal.date(from: c)!.addingTimeInterval(-30 * 60)
    }

    var body: some View {
        Group {
            if enabled == false {
                ContentUnavailableView("Live TV isn't set up", systemImage: "tv", description: Text("An admin can add Dispatcharr or an M3U playlist in the web app's Settings → Live TV."))
            } else {
                content
            }
        }
        .navigationTitle("Live TV")
        .task {
            let status = try? await app.liveStatus()
            enabled = status?.enabled ?? false
            canRecord = status?.canRecord ?? false
            groups = (try? await app.liveGroups()) ?? []
        }
        .task(id: "\(filter)|\(start.timeIntervalSince1970)") { await load() }
        #if os(iOS)
        .fullScreenCover(item: $watching) { LiveWatchView(channels: channels, start: $0.id) }
        #else
        .fullScreenCover(item: $watching) { LiveWatchView(channels: channels, start: $0.id) }
        #endif
        .sheet(isPresented: Binding(get: { details != nil }, set: { if !$0 { details = nil } })) {
            if let (p, c) = details {
                ProgrammeSheet(programme: p, channel: c, canRecord: canRecord) { Task { await load() } }
            }
        }
    }

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                controls
                #if os(iOS)
                if tab != .recordings, let c = preview ?? channels.first { LivePreview(channel: c) { watching = WatchTarget(id: c.id) } }
                #endif
                if let error { Text(error).foregroundStyle(.red).padding(.horizontal, sidePadding) }
                if tab == .recordings {
                    RecordingsList().padding(.horizontal, sidePadding)
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
            .padding(.vertical)
        }
    }

    /// On now: preview it (iPhone/iPad) or watch it (Apple TV); a second tap on the channel
    /// being previewed, or anything later, shows the programme's details.
    private func programmeTapped(_ p: LiveProgramme, _ c: LiveChannel) {
        #if os(tvOS)
        if p.isOn { pick(c) } else { details = (p, c) }
        #else
        if p.isOn, (preview ?? channels.first)?.id != c.id { pick(c) } else { details = (p, c) }
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
        #if os(tvOS)
        watching = WatchTarget(id: c.id)
        #else
        if preview?.id == c.id { watching = WatchTarget(id: c.id) } else { preview = c }
        #endif
    }

    private var controls: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("View", selection: $tab) {
                ForEach(tabs, id: \.self) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.segmented)
            if tab != .recordings { filterBar }
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
                Button { watching = WatchTarget(id: c.id) } label: {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(spacing: 10) {
                            ChannelLogo(channel: c).frame(width: 64, height: 40)
                            VStack(alignment: .leading) {
                                Text(c.now?.title ?? c.name).font(.headline).lineLimit(1)
                                Text(c.now.map { "\($0.minutesLeft)m left · \(c.name)" } ?? c.name).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }
                        }
                        if let p = c.now { ProgressView(value: p.progress).tint(Color.marqueeGold) }
                        if let n = c.next {
                            Text("Next: \(n.title) at \(n.start.formatted(date: .omitted, time: .shortened))").font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 10))
                }
                .buttonStyle(cardButtonStyle)
                .accessibilityLabel("Watch \(c.name)")
            }
        }
        .padding(.horizontal, sidePadding)
    }

    private func load() async {
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
        HStack(alignment: .top, spacing: 0) {
            VStack(spacing: 0) {
                Color.clear.frame(height: 28)
                ForEach(channels, id: \.id) { c in channelCell(c).frame(height: row) }
            }
            .frame(width: column)
            ScrollView(.horizontal, showsIndicators: false) {
                ZStack(alignment: .topLeading) {
                    VStack(alignment: .leading, spacing: 0) {
                        timeline
                        ForEach(channels, id: \.id) { c in programmes(c).frame(width: width, height: row, alignment: .leading) }
                    }
                    let now = x(.now)
                    if now > 0 && now < width {
                        Rectangle().fill(Color.marqueeGold).frame(width: 2).offset(x: now).allowsHitTesting(false)
                            .accessibilityHidden(true)
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
            .accessibilityLabel("\(selected == c.id ? "Watch" : "Preview") \(c.name)")
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

    private func programmes(_ c: LiveChannel) -> some View {
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
        AsyncImage(url: app.logoURL(channel)) { phase in
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
                Text(error ?? "Now On: \(channel.now?.title ?? channel.name)").font(.footnote).lineLimit(1)
                    .foregroundStyle(error == nil ? Color.white : Color.red)
                Spacer()
            }
            .padding(10)
            .background(LinearGradient(colors: [.clear, .black.opacity(0.8)], startPoint: .top, endPoint: .bottom))
        }
        .overlay(alignment: .top) {
            HStack {
                Button { muted.toggle(); player.isMuted = muted } label: { Image(systemName: muted ? "speaker.slash.fill" : "speaker.wave.2.fill") }
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
        do {
            let s = try await app.playLive(channel.id)
            session = s.id
            player.replaceCurrentItem(with: AVPlayerItem(url: s.url))
            player.isMuted = muted
            player.play()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func stop() {
        player.replaceCurrentItem(with: nil)
        if let s = session { session = nil; Task { await app.stopLive(s) } }
    }
}
#endif

/// Full-screen live TV with channel up/down.
struct LiveWatchView: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    let channels: [LiveChannel]
    @State private var current: Int64
    @State private var player = AVPlayer()
    @State private var session: String?
    @State private var error: String?

    init(channels: [LiveChannel], start: Int64) {
        self.channels = channels
        _current = State(initialValue: start)
    }

    private var channel: LiveChannel? { channels.first { $0.id == current } }

    var body: some View {
        ZStack(alignment: .top) {
            Color.black.ignoresSafeArea()
            VideoPlayer(player: player).ignoresSafeArea()
                .accessibilityLabel("Live TV")
            #if os(iOS)
            header
            #endif
            if let error { Text(error).foregroundStyle(.red).padding(.top, 80) }
        }
        .task(id: current) { await tune() }
        .onDisappear { stop() }
        #if os(tvOS)
        .onMoveCommand { dir in
            if dir == .up { step(-1) } else if dir == .down { step(1) }
        }
        #endif
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
            }
            Spacer()
            Button { step(-1) } label: { Image(systemName: "chevron.up") }.accessibilityLabel("Channel up")
            Button { step(1) } label: { Image(systemName: "chevron.down.circle") }.accessibilityLabel("Channel down")
        }
        .buttonStyle(.bordered).tint(.white)
        .padding()
        .background(LinearGradient(colors: [.black.opacity(0.8), .clear], startPoint: .top, endPoint: .bottom))
    }
    #endif

    private func step(_ d: Int) {
        guard let i = channels.firstIndex(where: { $0.id == current }), !channels.isEmpty else { return }
        current = channels[(i + d + channels.count) % channels.count].id
    }

    private func tune() async {
        stop()
        do {
            let s = try await app.playLive(current)
            session = s.id
            let item = AVPlayerItem(url: s.url)
            if let c = channel { item.externalMetadata = metadata(c) }
            player.replaceCurrentItem(with: item)
            player.play()
            error = nil
        } catch {
            self.error = error.localizedDescription
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

/// A future programme's details.
struct ProgrammeSheet: View {
    let programme: LiveProgramme
    let channel: LiveChannel
    var canRecord = false
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
                if canRecord, programme.end > .now, loaded {
                    Section {
                        if programme.series == true { Text("A series recording covers this title.").font(.footnote).foregroundStyle(.secondary) }
                        if let recording {
                            Button(recording.status == .recording ? "Stop Recording" : "Don't Record", role: .destructive) {
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

/// The DVR (LIVE-5): recording now, upcoming, series and finished recordings.
struct RecordingsList: View {
    @Environment(AppSession.self) private var app
    @State private var recordings: [Recording] = []
    @State private var rules: [RecordingRule] = []
    @State private var loaded = false
    @State private var error: String?
    @State private var deleting: Recording?

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            if let error { Text(error).foregroundStyle(.red) }
            if loaded && recordings.isEmpty && rules.isEmpty {
                Text("Nothing recorded yet. Pick a programme in the guide and choose Record.").foregroundStyle(.secondary)
            }
            section("Recording Now", recordings.filter { $0.status == .recording }) { r in
                Button("Stop") { act { try await app.cancelRecording(r.id) } }
            }
            section("Upcoming", recordings.filter { $0.status == .scheduled }) { r in
                Button("Cancel") { act { try await app.cancelRecording(r.id) } }
            }
            if !rules.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Series").font(.headline)
                    ForEach(rules, id: \.id) { s in
                        HStack {
                            VStack(alignment: .leading) {
                                Text(s.title).font(.body.weight(.medium))
                                Text("\(s.channelName ?? "Any channel") · \(s.upcoming) upcoming").font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button("Stop Series") { act { try await app.stopSeries(s.id) } }
                        }
                        .padding(12)
                        .background(Color.secondary.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
                    }
                }
            }
            section("Recorded", recordings.filter { $0.status == .completed || $0.status == .failed }) { r in
                if r.status == .failed {
                    Button("Dismiss") { act { try await app.cancelRecording(r.id) } }
                } else {
                    Button("Delete", role: .destructive) { deleting = r }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog("Delete this recording?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }), presenting: deleting) { r in
            Button("Delete \(r.title)", role: .destructive) { act { try await app.cancelRecording(r.id, deleteFile: true) } }
        }
    }

    @ViewBuilder
    private func section<A: View>(_ title: String, _ list: [Recording], @ViewBuilder action: @escaping (Recording) -> A) -> some View {
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

    private func detail(_ r: Recording) -> String {
        var parts = [r.episode, r.subtitle].compactMap { $0 }
        parts.append(r.channelName)
        parts.append("\(r.start.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day().hour().minute()))–\(r.end.formatted(date: .omitted, time: .shortened))")
        if let b = r.sizeBytes, b > 0 { parts.append(ByteCountFormatter.string(fromByteCount: b, countStyle: .file)) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        do {
            recordings = try await app.recordings()
            rules = try await app.recordingRules()
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
