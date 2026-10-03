import MarqueeKit
import SwiftUI

/// What "Play on…" sends: items (one, or a track queue starting at `index`), where to start,
/// and how to stop here once it plays there.
struct PlayOnTarget {
    var itemIDs: [Int64]
    var index: Int?
    var startMs: Int64?
    var pauseHere: (() -> Void)?
}

/// "Play on…" (USER-14): lists your other open Marquee apps and plays this there, then opens
/// a remote for that player. Chromecast stays on its own Cast button.
struct PlayOnButton: View {
    /// Worked out when the button is pressed (the position then).
    let target: () -> PlayOnTarget?
    var compact = false
    @State private var sent: Sent?

    /// What was worked out when the button was pressed (nil target: just control a player).
    struct Sent: Identifiable {
        let id = UUID()
        let target: PlayOnTarget?
    }

    var body: some View {
        Button {
            sent = Sent(target: target())
        } label: {
            if compact {
                Image(systemName: "tv.and.hifispeaker.fill")
            } else {
                Label("Play on…", systemImage: "tv.and.hifispeaker.fill")
            }
        }
        .accessibilityLabel("Play on…")
        .sheet(item: $sent) { s in
            NavigationStack { RemotePlayersView(target: s.target, dismissable: true) }
        }
    }
}

/// The players you can control. With a target, choosing one plays it there first.
struct RemotePlayersView: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    var target: PlayOnTarget?
    var dismissable = false
    @State private var players: [RemotePlayer]?
    @State private var error: String?
    @State private var controlling: RemotePlayer?
    @State private var busy: Int64?

    var body: some View {
        List {
            if let error { ErrorBanner(message: error) }
            if let players {
                if players.isEmpty {
                    ContentUnavailableView("No players", systemImage: "tv.and.hifispeaker.fill",
                                           description: Text("Open Marquee on a TV, phone, tablet or browser signed in as you, and it appears here."))
                }
                Section {
                    ForEach(players, id: \.deviceId) { p in
                        Button { choose(p) } label: { row(p) }
                            .disabled(busy != nil)
                            .accessibilityIdentifier("player-\(p.name)")
                    }
                } footer: {
                    if !players.isEmpty {
                        Text(target == nil ? "Choose a player to control it." : "Choose where to play. Chromecast devices are on the Cast button.")
                    }
                }
            } else {
                ProgressView()
            }
        }
        .navigationTitle(target == nil ? "Remote Control" : "Play on…")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if dismissable { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
        }
        .navigationDestination(item: $controlling) { RemoteControlView(player: $0) }
        .task {
            // Kept fresh while open: players come and go.
            while !Task.isCancelled {
                do { players = try await app.remotePlayers(); error = nil } catch { if !Task.isCancelled { self.error = error.localizedDescription } }
                try? await Task.sleep(for: .seconds(5))
            }
        }
    }

    private func row(_ p: RemotePlayer) -> some View {
        HStack(spacing: 14) {
            Image(systemName: p.platformIcon).font(.title2).foregroundStyle(Color.marqueeGold).frame(width: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(p.name).foregroundStyle(.primary)
                Text(p.userId == app.me?.id ? p.nowPlayingLine : "\(p.userName) · \(p.nowPlayingLine)")
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer()
            if busy == p.deviceId { ProgressView() } else { Image(systemName: "chevron.right").font(.footnote).foregroundStyle(.tertiary) }
        }
    }

    private func choose(_ p: RemotePlayer) {
        guard let target, !target.itemIDs.isEmpty else { controlling = p; return }
        busy = p.deviceId
        Task {
            defer { busy = nil }
            do {
                try await app.sendRemote(p.deviceId, RemoteCommand(_type: .play, itemIds: target.itemIDs, index: target.index, startMs: target.startMs))
                target.pauseHere?()
                controlling = p
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// A remote for one player: what's playing, transport, tracks and volume (USER-14).
struct RemoteControlView: View {
    @Environment(AppSession.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State var player: RemotePlayer
    @State private var gone = false
    @State private var error: String?
    /// When the state arrived, to move the position on while it plays.
    @State private var stateAt = Date()
    @State private var scrub: Double?
    @State private var volume: Double?
    @State private var detail: ItemDetail?
    @State private var art: Item?

    private var state: RemotePlayerState? { player.state }
    private var playing: Bool { state?.state == .playing }

    var body: some View {
        ScrollView {
            VStack(spacing: 22) {
                if gone {
                    ErrorBanner(message: "\(player.name) isn't available any more.")
                }
                if let error { ErrorBanner(message: error) }
                header
                if let s = state, s.itemId != nil, s.state != .idle, s.state != .stopped {
                    nowPlaying(s)
                } else {
                    ContentUnavailableView("Nothing playing", systemImage: "play.slash",
                                           description: Text("Use Play on… from a title, album or Now Playing to send something to \(player.name)."))
                }
            }
            .padding()
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
        }
        .navigationTitle(player.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Disconnect") { dismiss() }
            }
        }
        .task { await follow() }
        .task(id: state?.itemId) { await loadItem() }
    }

    private var header: some View {
        HStack(spacing: 10) {
            Image(systemName: player.platformIcon).foregroundStyle(Color.marqueeGold)
            Text(player.userId == app.me?.id ? player.name : "\(player.name) · \(player.userName)").font(.subheadline.weight(.semibold))
            Spacer()
        }
    }

    @ViewBuilder private func nowPlaying(_ s: RemotePlayerState) -> some View {
        Group {
            if let art {
                ArtworkView(item: art, shape: art._type == .track || art._type == .album || art._type == .artist ? .square : .poster, width: 260)
                    .frame(maxWidth: 260)
                    .shadow(radius: 12)
            } else {
                RoundedRectangle(cornerRadius: 12).fill(Color.secondary.opacity(0.2)).frame(width: 200, height: 200)
            }
        }
        VStack(spacing: 4) {
            Text(s.title ?? "").font(.title2.bold()).multilineTextAlignment(.center).accessibilityIdentifier("remoteTitle")
            if let sub = s.subtitle, !sub.isEmpty { Text(sub).foregroundStyle(.secondary).multilineTextAlignment(.center) }
            Text(stateLabel(s)).font(.caption.weight(.semibold)).foregroundStyle(Color.marqueeGold).accessibilityIdentifier("remoteState")
        }
        progress(s)
        transport(s)
        tracks
        if let v = s.volume {
            HStack {
                Image(systemName: "speaker.fill")
                Slider(value: Binding(get: { volume ?? v }, set: { volume = $0 }), in: 0...1) { editing in
                    if !editing, let vol = volume { send(.init(_type: .setVolume, volume: vol)) }
                }
                .accessibilityLabel("Volume")
                Image(systemName: "speaker.wave.3.fill")
            }
            .foregroundStyle(.secondary)
        }
        Button(role: .destructive) { send(.init(_type: .stop)) } label: { Label("Stop", systemImage: "stop.fill") }
            .buttonStyle(.bordered)
    }

    private func stateLabel(_ s: RemotePlayerState) -> String {
        switch s.state {
        case .playing: "Playing"
        case .paused: "Paused"
        case .buffering: "Loading…"
        case .idle, .stopped: "Stopped"
        }
    }

    @ViewBuilder private func progress(_ s: RemotePlayerState) -> some View {
        let duration = Double(s.durationMs ?? 0) / 1000
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            let pos = scrub ?? livePosition(s, at: ctx.date)
            VStack(spacing: 4) {
                Slider(value: Binding(get: { pos }, set: { scrub = $0 }), in: 0...max(duration, 1)) { editing in
                    if !editing, let target = scrub {
                        send(.init(_type: .seek, positionMs: Int64(target * 1000)))
                    }
                }
                .disabled(duration <= 0)
                .accessibilityLabel("Position")
                HStack {
                    Text(formatTime(seconds: pos)).accessibilityIdentifier("remotePosition")
                    Spacer()
                    Text(formatTime(seconds: duration))
                }
                .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            }
        }
    }

    private func livePosition(_ s: RemotePlayerState, at date: Date) -> Double {
        var p = Double(s.positionMs) / 1000
        if s.state == .playing { p += date.timeIntervalSince(stateAt) }
        let d = Double(s.durationMs ?? 0) / 1000
        return d > 0 ? min(p, d) : p
    }

    private func transport(_ s: RemotePlayerState) -> some View {
        HStack(spacing: 30) {
            Button { send(.init(_type: .previous)) } label: { Image(systemName: "backward.fill").font(.title2) }
                .accessibilityLabel("Previous")
            Button { seek(by: -10, s) } label: { Image(systemName: "gobackward.10").font(.title2) }
                .accessibilityLabel("Back 10 seconds")
            Button { send(.init(_type: playing ? .pause : .resume)) } label: {
                Image(systemName: playing ? "pause.circle.fill" : "play.circle.fill").font(.system(size: 60))
            }
            .accessibilityLabel(playing ? "Pause" : "Play")
            Button { seek(by: 30, s) } label: { Image(systemName: "goforward.30").font(.title2) }
                .accessibilityLabel("Forward 30 seconds")
            Button { send(.init(_type: .next)) } label: { Image(systemName: "forward.fill").font(.title2) }
                .accessibilityLabel("Next")
        }
        .buttonStyle(.plain)
        .foregroundStyle(.primary)
    }

    /// Audio and subtitle pickers, from the item's streams.
    @ViewBuilder private var tracks: some View {
        let streams = detail?.info.versions.first?.files.first?.streams ?? []
        let audio = streams.filter { $0.kind == .audio }
        let subs = streams.filter { $0.kind == .subtitle }
        if detail?.base.isPlayableVideo == true, audio.count > 1 || !subs.isEmpty {
            HStack(spacing: 16) {
                if audio.count > 1 {
                    Menu {
                        ForEach(audio, id: \.id) { a in
                            Button { send(.init(_type: .setAudio, streamId: a.id)) } label: {
                                if a.id == state?.audioStreamId { Label(streamName(a), systemImage: "checkmark") } else { Text(streamName(a)) }
                            }
                        }
                    } label: { Label("Audio", systemImage: "speaker.wave.2") }
                }
                if !subs.isEmpty {
                    Menu {
                        Button { send(.init(_type: .setSubtitle, streamId: -1)) } label: {
                            if state?.subtitleStreamId == nil || state?.subtitleStreamId == -1 { Label("Off", systemImage: "checkmark") } else { Text("Off") }
                        }
                        ForEach(subs, id: \.id) { t in
                            Button { send(.init(_type: .setSubtitle, streamId: t.id)) } label: {
                                if t.id == state?.subtitleStreamId { Label(streamName(t), systemImage: "checkmark") } else { Text(streamName(t)) }
                            }
                        }
                    } label: { Label("Subtitles", systemImage: "captions.bubble") }
                }
            }
            .buttonStyle(.bordered)
        }
    }

    private func streamName(_ s: Schemas.MediaStream) -> String {
        var parts = [s.title ?? languageName(s.language)]
        if s.forced { parts.append("Forced") }
        if s.hearingImpaired { parts.append("SDH") }
        return parts.joined(separator: " · ")
    }

    private func seek(by seconds: Double, _ s: RemotePlayerState) {
        let now = livePosition(s, at: Date())
        let d = Double(s.durationMs ?? 0) / 1000
        let to = max(0, d > 0 ? min(d, now + seconds) : now + seconds)
        send(.init(_type: .seek, positionMs: Int64(to * 1000)))
    }

    private func send(_ c: RemoteCommand) {
        Task {
            do {
                try await app.sendRemote(player.deviceId, c)
                error = nil
            } catch is RemotePlayerGone {
                gone = true
            } catch {
                self.error = error.localizedDescription
            }
            if c._type == .seek { scrub = nil }
            if c._type == .setVolume { volume = nil }
        }
    }

    /// Long-polls the player's state.
    private func follow() async {
        if let fresh = try? await app.remotePlayer(player.deviceId) { update(fresh) }
        while !Task.isCancelled {
            do {
                update(try await app.remotePlayer(player.deviceId, since: player.version))
                gone = false
            } catch is RemotePlayerGone {
                gone = true
                try? await Task.sleep(for: .seconds(3))
            } catch {
                if Task.isCancelled { return }
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    private func update(_ p: RemotePlayer) {
        player = p
        stateAt = Date()
        if scrub != nil, p.state?.state != .buffering { scrub = nil }
    }

    private func loadItem() async {
        guard let id = state?.itemId else { detail = nil; art = nil; return }
        detail = try? await app.item(id)
        if let artID = state?.artItemId, artID != id {
            art = (try? await app.item(artID))?.base ?? detail?.base
        } else {
            art = detail?.base
        }
    }
}
