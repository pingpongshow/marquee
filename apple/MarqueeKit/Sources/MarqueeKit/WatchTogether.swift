import AVFoundation
import Foundation
import MarqueeAPI
import Observation

public typealias WatchGroup = Components.Schemas.WatchGroup
typealias WatchAction = Operations.WatchGroupCommand.Input.Body.JsonPayload.ActionPayload

@MainActor
public extension AppSession {
    /// Groups others are watching together (SYNC-1).
    func watchGroups() async throws -> [WatchGroup] {
        guard let client else { throw MarqueeError("Not connected") }
        return try await client.listWatchGroups().ok.body.json
    }
}

/// Watch together (SyncPlay, D73): keeps an AVPlayer in step with a group. The group's state
/// is long-polled; the viewer's own play, pause, seek and loading are sent as commands, and
/// changes made to follow the group aren't echoed back.
@MainActor @Observable
public final class WatchTogether {
    public private(set) var group: WatchGroup?
    public private(set) var error: String?

    @ObservationIgnored private let app: AppSession
    @ObservationIgnored private let player: AVPlayer
    @ObservationIgnored private let itemID: Int64
    @ObservationIgnored private var quietUntil = Date.distantPast
    @ObservationIgnored private var clockOffset: TimeInterval = 0
    @ObservationIgnored private var polling: Task<Void, Never>?
    @ObservationIgnored private var statusObservation: NSKeyValueObservation?
    @ObservationIgnored private var jumpObserver: NSObjectProtocol?
    @ObservationIgnored private var buffering = false
    /// Set while the player restarts its stream; cleared once it plays again.
    @ObservationIgnored private var restartingUntil = Date.distantPast

    /// How far apart players may drift before being pulled back.
    static let drift: Double = 1.5

    public init(app: AppSession, player: AVPlayer, itemID: Int64) {
        self.app = app
        self.player = player
        self.itemID = itemID
    }

    private var client: Client {
        get throws {
            guard let c = app.client else { throw MarqueeError("Not connected") }
            return c
        }
    }

    /// Starts a group at the current position.
    public func start() async {
        do {
            let pos = Int64(max(0, player.currentTime().seconds) * 1000)
            let g = try await client.createWatchGroup(body: .json(.init(itemId: itemID, positionMs: pos))).created.body.json
            begin(g)
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Joins a group someone else started.
    public func join(_ id: String) async {
        do {
            let g = try await client.joinWatchGroup(path: .init(groupId: id)).ok.body.json
            begin(g)
        } catch {
            self.error = "Couldn't join: \(error.localizedDescription)"
        }
    }

    public func leave() {
        guard let g = group else { return }
        group = nil
        stopObserving()
        let c = app.client
        Task { _ = try? await c?.leaveWatchGroup(path: .init(groupId: g.id)) }
    }

    private func begin(_ g: WatchGroup) {
        error = nil
        apply(g)
        observe()
        polling?.cancel()
        polling = Task { [weak self] in
            // A dropped connection or a reconnect retries with backoff; only the server saying
            // the group is gone (404) ends it straight away.
            var failures = 0
            while !Task.isCancelled {
                guard let self, let g = self.group else { return }
                do {
                    switch try await self.client.getWatchGroup(path: .init(groupId: g.id), query: .init(since: g.version)) {
                    case let .ok(ok):
                        let next = try ok.body.json
                        failures = 0
                        if !Task.isCancelled, self.group != nil { self.apply(next) }
                    case .notFound:
                        self.ended("The group has ended.")
                        return
                    default:
                        throw MarqueeError("Unexpected response")
                    }
                } catch {
                    if Task.isCancelled { return }
                    failures += 1
                    if failures >= 6 {
                        self.leave() // giving up: tell the server, in case it can still hear us
                        self.error = "Lost touch with the group."
                        return
                    }
                    try? await Task.sleep(for: .seconds(min(16, 1 << (failures - 1))))
                }
            }
        }
    }

    private func ended(_ message: String) {
        error = message
        group = nil
        stopObserving()
    }

    /// The player is restarting its stream (an audio change, a fallback): the pause and seek
    /// that come with it aren't the viewer's, so they aren't sent to the group.
    public func restarting() {
        restartingUntil = Date().addingTimeInterval(20)
    }

    private func apply(_ g: WatchGroup) {
        group = g
        clockOffset = g.serverTime.timeIntervalSinceNow
        let serverNow = Date().addingTimeInterval(clockOffset)
        let target = g.playing ? Double(g.positionMs) / 1000 + serverNow.timeIntervalSince(g.at) : Double(g.positionMs) / 1000
        let now = player.currentTime().seconds
        if now.isFinite, abs(now - target) > Self.drift {
            quiet()
            player.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        }
        if g.playing, player.rate == 0 {
            quiet()
            player.play()
        } else if !g.playing, player.rate != 0 {
            quiet()
            player.pause()
        }
    }

    private func quiet() { quietUntil = Date().addingTimeInterval(1.2) }
    private var mine: Bool { Date() > quietUntil && Date() > restartingUntil }

    private func observe() {
        guard statusObservation == nil else { return }
        statusObservation = player.observe(\.timeControlStatus, options: [.new]) { [weak self] p, _ in
            let status = p.timeControlStatus
            Task { @MainActor in self?.statusChanged(status) }
        }
        jumpObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.timeJumpedNotification, object: nil, queue: .main) { [weak self] note in
            Task { @MainActor in
                guard let self, (note.object as? AVPlayerItem) === self.player.currentItem, self.mine else { return }
                await self.send(.seek)
            }
        }
    }

    private func stopObserving() {
        polling?.cancel()
        polling = nil
        statusObservation?.invalidate()
        statusObservation = nil
        if let j = jumpObserver { NotificationCenter.default.removeObserver(j) }
        jumpObserver = nil
    }

    private func statusChanged(_ status: AVPlayer.TimeControlStatus) {
        guard group != nil else { return }
        switch status {
        case .waitingToPlayAtSpecifiedRate:
            if !buffering {
                buffering = true
                Task { await send(.buffering) }
            }
        case .playing:
            restartingUntil = .distantPast
            if buffering {
                buffering = false
                Task { await send(.ready) }
            } else if mine, group?.playing == false {
                Task { await send(.play) }
            }
        case .paused:
            if buffering {
                buffering = false
                Task { await send(.ready) }
            }
            if mine, group?.playing == true { Task { await send(.pause) } }
        @unknown default:
            break
        }
    }

    private func send(_ action: WatchAction) async {
        guard let g = group else { return }
        let pos = Int64(max(0, player.currentTime().seconds) * 1000)
        do {
            let next = try await client.watchGroupCommand(path: .init(groupId: g.id), body: .json(.init(action: action, positionMs: pos))).ok.body.json
            group = next
        } catch {
            self.error = error.localizedDescription
        }
    }
}
