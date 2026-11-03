import Foundation
import MarqueeAPI
import Observation

/// A Sonos room the server can play to (MUSIC-26).
public typealias Speaker = Components.Schemas.Speaker

/// What can go wrong talking to a room, as far as the player cares.
public enum SpeakerError: Error, Equatable, Sendable {
    /// 409: another app is playing there, the speaker refused the command, or (adding to the
    /// queue) it can't be reached. A fresh look at the room says which.
    case notMarquee
    /// 404: the room has gone.
    case gone
    case failed(String)
}

/// A transport command for a room.
public enum SpeakerCommand: Equatable, Sendable {
    case pause, resume, stop, next, previous
    case seek(ms: Int64)
    case setVolume(Int)
}

/// The server's /speakers calls: an abstraction so the session can run against a stub in tests
/// (and, until the server's side is finished, in the app under a test launch argument).
@MainActor
public protocol SpeakerAPI: AnyObject {
    func speakers() async throws -> [Speaker]
    func speaker(_ id: String) async throws -> Speaker
    func play(_ id: String, itemIDs: [Int64], index: Int, startMs: Int64, context: String?) async throws -> Speaker
    func addToQueue(_ id: String, itemIDs: [Int64]) async throws -> Speaker
    func control(_ id: String, _ command: SpeakerCommand) async throws -> Speaker
    /// A Stop for this room that still works once signed out: it holds on to the connection there
    /// is now, as forgetting the server drops it before the request goes.
    func stopper(_ id: String) -> @MainActor () async throws -> Void
}

/// The real calls.
@MainActor
public final class ServerSpeakerAPI: SpeakerAPI {
    private let app: AppSession
    public init(app: AppSession) { self.app = app }

    private var api: Client {
        get throws {
            guard let client = app.client else { throw SpeakerError.failed("Not connected") }
            return client
        }
    }

    public func speakers() async throws -> [Speaker] {
        try await api.listSpeakers().ok.body.json
    }

    public func speaker(_ id: String) async throws -> Speaker {
        switch try await api.getSpeaker(path: .init(speakerId: id)) {
        case .ok(let ok): return try ok.body.json
        case .notFound: throw SpeakerError.gone
        default: throw SpeakerError.failed("The server couldn't reach that room.")
        }
    }

    public func play(_ id: String, itemIDs: [Int64], index: Int, startMs: Int64, context: String?) async throws -> Speaker {
        let body = Schemas.SpeakerPlay(itemIds: itemIDs, index: index, startMs: startMs, context: context)
        switch try await api.playOnSpeaker(path: .init(speakerId: id), body: .json(body)) {
        case .ok(let ok): return try ok.body.json
        case .notFound: throw SpeakerError.gone
        default: throw SpeakerError.failed("The server couldn't play to that room.")
        }
    }

    public func addToQueue(_ id: String, itemIDs: [Int64]) async throws -> Speaker {
        switch try await api.addToSpeakerQueue(path: .init(speakerId: id), body: .json(.init(itemIds: itemIDs))) {
        case .ok(let ok): return try ok.body.json
        case .conflict: throw SpeakerError.notMarquee
        case .notFound: throw SpeakerError.gone
        default: throw SpeakerError.failed("The server couldn't add to that room's queue.")
        }
    }

    public func control(_ id: String, _ command: SpeakerCommand) async throws -> Speaker {
        try await Self.control(api, id, command)
    }

    public func stopper(_ id: String) -> @MainActor () async throws -> Void {
        let client = app.client
        return {
            guard let client else { throw SpeakerError.failed("Not connected") }
            _ = try await Self.control(client, id, .stop)
        }
    }

    private static func control(_ client: Client, _ id: String, _ command: SpeakerCommand) async throws -> Speaker {
        let input: Operations.ControlSpeaker.Input.Body.JsonPayload
        switch command {
        case .pause: input = .init(action: .pause)
        case .resume: input = .init(action: .resume)
        case .stop: input = .init(action: .stop)
        case .next: input = .init(action: .next)
        case .previous: input = .init(action: .previous)
        case .seek(let ms): input = .init(action: .seek, positionMs: ms)
        case .setVolume(let v): input = .init(action: .setVolume, volume: min(100, max(0, v)))
        }
        switch try await client.controlSpeaker(path: .init(speakerId: id), body: .json(input)) {
        case .ok(let ok): return try ok.body.json
        case .conflict: throw SpeakerError.notMarquee
        case .notFound: throw SpeakerError.gone
        default: throw SpeakerError.failed("The server couldn't control that room.")
        }
    }
}

/// What a room needs of the music player: its queue and position, and somewhere to report what
/// the room is doing. MusicPlayer is one; tests use a stub.
@MainActor
public protocol SpeakerMusicHost: AnyObject {
    /// Every entry's item, in play order, and which is current (-1 for none).
    var speakerQueue: (items: [Int64], index: Int) { get }
    var speakerTime: Double { get }
    /// What the current track's play is reported as started from (MUSIC-17).
    var speakerContext: String? { get }
    /// Hands the queue to the room (calls back `play`), or takes it back.
    func attachSpeaker(_ remote: any RemoteQueuePlayback)
    func detachSpeaker()
    func remoteUpdate(playing: Bool, time: Double, duration: Double)
    /// The room moved on to this entry of the queue by itself.
    func remoteMoved(toQueueIndex index: Int)
    func remoteFinished()
    /// A radio waited in vain: it stops asking, and nothing starts by itself later.
    func stopWaitingForMore()
    func showError(_ message: String)
    /// A radio: when the room runs out, more is on its way, so it isn't over.
    var speakerWaitsForMore: Bool { get }
}

/// Pure queue bookkeeping for a room: what was sent, and how what the queue wants now differs
/// from it. A longer tail (a radio top-up) is added; anything else is sent again.
public struct SpeakerQueueState: Equatable, Sendable {
    public enum Change: Equatable, Sendable {
        case none
        case append([Int64])
        case resend([Int64])
    }

    /// At most what the server takes in one play.
    public static let limit = 2000
    /// The item ids last sent to the room.
    public private(set) var sent: [Int64] = []
    /// Which entry of the player's queue `sent[0]` is.
    public private(set) var offset = 0

    public init() {}

    /// The tail to send from `index`.
    public static func tail(_ items: [Int64], from index: Int) -> [Int64] {
        guard items.indices.contains(index) else { return [] }
        return Array(items[index...].prefix(limit))
    }

    public mutating func didSend(_ items: [Int64], offset: Int) {
        sent = items
        self.offset = offset
    }

    public mutating func didAppend(_ items: [Int64]) { sent += items }

    /// An addition didn't reach the room: the next change sends it again.
    public mutating func didFailToAppend(_ count: Int) { sent.removeLast(min(count, sent.count)) }

    public func change(items: [Int64], index: Int) -> Change {
        let want = Self.tail(items, from: index)
        let skip = index - offset
        guard skip >= 0, skip <= sent.count else { return .resend(want) }
        let have = Array(sent[skip...])
        if want == have { return .none }
        if want.count > have.count, Array(want.prefix(have.count)) == have { return .append(Array(want.dropFirst(have.count))) }
        return .resend(want)
    }

    /// The player's queue index for the room's queue index.
    public func playerIndex(forRoomIndex room: Int) -> Int { offset + room }

    public func canGoBack(roomIndex: Int?) -> Bool { (roomIndex ?? 0) > 0 }
}

/// One Mac (or other) listening to one room at a time: picks a room, hands it the queue, keeps the
/// bar in step by polling, forwards the transport, and steps aside when another app takes over.
@MainActor @Observable
public final class SonosSession: RemoteQueuePlayback {
    public enum State: Equatable, Sendable {
        case idle
        case connecting(String)
        case playing(String)
    }

    /// The rooms last listed.
    public private(set) var rooms: [Speaker] = []
    public private(set) var state: State = .idle
    /// The active room as last heard from.
    public private(set) var room: Speaker?
    /// A short message when the session ended by itself; clears after a few seconds.
    public private(set) var notice: String?
    /// The room's volume, 0–100, while connected.
    public private(set) var volume = 0

    public var activeRoomName: String? {
        switch state {
        case .idle: nil
        case .connecting(let name), .playing(let name): name
        }
    }

    /// Why the Sonos rooms aren't offered, if they aren't: no list comes back when offline.
    public var isActive: Bool { state != .idle }

    public static let pollInterval: Duration = .seconds(2)
    /// Failed polls in a row before the room counts as lost.
    public static let maxPollFailures = 4
    /// How long a radio that ran out waits in the room for its next songs.
    public static let defaultMoreWait: Duration = .seconds(45)

    @ObservationIgnored private let api: any SpeakerAPI
    @ObservationIgnored private weak var host: (any SpeakerMusicHost)?
    @ObservationIgnored private var queueState = SpeakerQueueState()
    @ObservationIgnored private var pollTask: Task<Void, Never>?
    @ObservationIgnored private var noticeTask: Task<Void, Never>?
    @ObservationIgnored private var volumeTask: Task<Void, Never>?
    /// The sends in flight, in order; `settled()` waits for them.
    @ObservationIgnored private var work: Task<Void, Never>?
    /// Bumped by anything that changes the room, so an answer sent before it is dropped.
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var pollFailures = 0
    /// Bumped by a new play, and by whatever ends the room (leaving, losing it, reaching the end):
    /// a play from before it is stale. Commands don't bump this, only `generation`, so a Pause
    /// while connecting doesn't cancel the play it follows.
    @ObservationIgnored private var playRun = 0
    /// True from picking a room until it has answered the play.
    @ObservationIgnored private var starting = false
    /// The room a play actually went out to. A Stop goes only there: a play called off before it
    /// went out leaves whatever the room's doing (perhaps another app's music) alone.
    @ObservationIgnored private var playedRoom: String?
    /// A radio that ran out, waiting for its next songs.
    @ObservationIgnored private var moreTask: Task<Void, Never>?
    @ObservationIgnored private let moreWait: Duration
    @ObservationIgnored private let autoPoll: Bool

    public init(api: any SpeakerAPI, host: any SpeakerMusicHost, autoPoll: Bool = true, moreWait: Duration = SonosSession.defaultMoreWait) {
        self.api = api
        self.host = host
        self.autoPoll = autoPoll
        self.moreWait = moreWait
    }

    // MARK: Rooms

    /// Lists the rooms. A failure lists none (the picker then leaves Sonos out).
    public func refreshRooms() async {
        rooms = (try? await api.speakers()) ?? []
    }

    /// Clears the list while the server can't be reached.
    public func clearRooms() { rooms = [] }

    /// Local playback stops and the queue goes to this room from the current track and position.
    public func select(_ speaker: Speaker) {
        guard let host, host.speakerQueue.index >= 0 else { return }
        if room?.id == speaker.id, state != .idle { return }
        if let old = room, state != .idle {
            // From one room to another: the first stops, and the queue moves on from where it was.
            stopRoom(old.id)
            room = speaker
            volume = speaker.volume
            state = .connecting(speaker.name)
            send(host.speakerTime)
            return
        }
        room = speaker
        volume = speaker.volume
        state = .connecting(speaker.name)
        host.attachSpeaker(self) // its rebuild calls play(), which sends the queue
    }

    /// Back to this Mac: the room stops, and playback carries on here from where it was.
    public func leave() {
        guard state != .idle, let id = room?.id else { return }
        endRoom()
        stopRoom(id)
        host?.detachSpeaker()
    }

    // MARK: RemotePlayback

    public func play(_ item: Item, at seconds: Double, context: String?) {
        guard state != .idle else { return }
        send(seconds)
    }

    public func toggle() { room?.state == .paused || room?.state == .stopped ? resume() : pause() }

    public func resume() { command(.resume, playing: true) }

    public func pause() { command(.pause, playing: false) }

    public func seek(_ seconds: Double) { command(.seek(ms: Int64(max(0, seconds) * 1000)), playing: nil) }

    // MARK: RemoteQueuePlayback

    public func skip(forward: Bool) -> Bool {
        guard let id = room?.id, state != .idle else { return false }
        // Before the first track sent: the room doesn't have it, so the queue is sent again.
        if !forward, !queueState.canGoBack(roomIndex: room?.queueIndex) { return false }
        // Past the end: nothing there to skip to.
        if forward, let i = room?.queueIndex, let n = room?.queueLength, i + 1 >= n { return false }
        generation += 1
        let run = generation
        enqueue { [self] in
            do {
                let s = try await api.control(id, forward ? .next : .previous)
                if run == generation { apply(s) }
            } catch { await commandFailed(error) }
        }
        return true
    }

    public func queueChanged() {
        guard state != .idle, !starting, let id = room?.id, let host else { return }
        let q = host.speakerQueue
        switch queueState.change(items: q.items, index: q.index) {
        case .none:
            break
        case .append(let items):
            queueState.didAppend(items)
            enqueue { [self] in
                do {
                    let s = try await api.addToQueue(id, itemIDs: items)
                    if room?.id == id { room = s }
                } catch {
                    queueState.didFailToAppend(items.count)
                    await commandFailed(error)
                }
            }
        case .resend:
            send(host.speakerTime)
        }
    }

    public func queueStopped() -> Task<Void, Never>? {
        guard state != .idle, let id = room?.id else { return nil }
        endRoom()
        let stop = stopRoom(id)
        host?.detachSpeaker()
        return stop
    }

    // MARK: Volume

    /// Sets the room's volume; quick changes (a slider being dragged) send the last one.
    public func setVolume(_ v: Int) {
        guard let id = room?.id, state != .idle else { return }
        volume = min(100, max(0, v))
        let target = volume
        volumeTask?.cancel()
        volumeTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(150))
            guard let self, !Task.isCancelled else { return }
            do { _ = try await self.api.control(id, .setVolume(target)) } catch { await self.commandFailed(error) }
        }
    }

    // MARK: Polling

    /// One poll: public so tests can drive it. Runs every couple of seconds while connected.
    public func poll() async {
        // Not while a play is on its way: until it's answered the room may not be ours yet, and that
        // isn't another app taking over.
        guard let id = room?.id, state != .idle, !starting else { return }
        let run = generation
        do {
            let s = try await api.speaker(id)
            pollFailures = 0
            if run == generation, room?.id == id { apply(s) }
        } catch SpeakerError.gone {
            if run == generation { lost(reason: "\(room?.name ?? "The room") isn't available any more.") }
        } catch {
            guard run == generation else { return }
            pollFailures += 1
            if pollFailures >= Self.maxPollFailures { lost(reason: "Lost contact with \(room?.name ?? "the room").") }
        }
    }

    /// Waits for what's been sent (for tests).
    public func settled() async {
        while let w = work {
            await w.value
            if work == nil || work == w { break }
        }
    }

    // MARK: Internals

    /// Sends the queue from the current track, at this position.
    private func send(_ seconds: Double) {
        guard let id = room?.id, let host else { return }
        let q = host.speakerQueue
        let tail = SpeakerQueueState.tail(q.items, from: q.index)
        guard !tail.isEmpty else { return }
        generation += 1
        let run = generation
        playRun += 1
        let play = playRun
        moreTask?.cancel()
        queueState.didSend(tail, offset: q.index)
        let context = host.speakerContext
        starting = true
        enqueue { [self] in
            // Left, or another play came, before this one went out.
            guard play == playRun else { return }
            playedRoom = id
            do {
                let s = try await api.play(id, itemIDs: tail, index: 0, startMs: Int64(max(0, seconds) * 1000), context: context)
                guard play == playRun, room?.id == id else { return }
                starting = false
                state = .playing(s.name)
                startPolling()
                // A command sent since has the newer news: its own answer or the next poll says.
                if run == generation { apply(s) }
                // The queue may have grown (a top-up) while the first play was on its way.
                queueChanged()
            } catch {
                guard play == playRun else { return }
                starting = false
                lost(reason: "Couldn't play in \(room?.name ?? "that room").")
            }
        }
    }

    /// Stops a room, after anything already on its way there: a play still in flight mustn't land
    /// after the Stop and leave the room playing. Only if a play went out to it.
    @discardableResult
    private func stopRoom(_ id: String) -> Task<Void, Never>? {
        volumeTask?.cancel()
        let stop = api.stopper(id)
        enqueue { [self] in
            guard playedRoom == id else { return }
            playedRoom = nil
            try? await stop()
        }
        return work
    }

    /// The session's own state goes back to idle; whatever was on its way is stale.
    private func endRoom() {
        generation += 1
        playRun += 1
        moreTask?.cancel()
        endPolling()
        state = .idle
        room = nil
        starting = false
    }

    /// Runs after what's already been sent, so a skip can't overtake the play it follows.
    private func enqueue(_ job: @escaping @MainActor () async -> Void) {
        let before = work
        work = Task { @MainActor in
            await before?.value
            await job()
        }
    }

    private func command(_ c: SpeakerCommand, playing: Bool?) {
        guard let id = room?.id, state != .idle else { return }
        generation += 1
        let run = generation
        enqueue { [self] in
            do {
                let s = try await api.control(id, c)
                if run == generation { apply(s) }
            } catch { await commandFailed(error) }
        }
        if let playing, let host { host.remoteUpdate(playing: playing, time: host.speakerTime, duration: 0) }
    }

    private func apply(_ s: Speaker) {
        guard let host else { return }
        let before = room
        room = s
        volume = s.volume
        guard s.marquee else {
            if Self.wasAtTheEnd(before) { reachedTheEnd() } else { lost(reason: "\(s.name) isn't playing Marquee any more.") }
            return
        }
        follow(s, host: host)
        host.remoteUpdate(playing: s.state == .playing || s.state == .loading,
                          time: Double(s.positionMs ?? 0) / 1000, duration: Double(s.durationMs ?? 0) / 1000)
    }

    /// The room moved on by itself (or by a skip here): the queue follows. The server may have
    /// dropped songs from what it was sent (thumbed down, say), so its index only lines up when
    /// the lengths do; otherwise the song itself says where it is. It can be absent too (a song
    /// this person can't see), and then the queue stays put until one says.
    private func follow(_ s: Speaker, host: any SpeakerMusicHost) {
        let q = host.speakerQueue
        let at: Int?
        if let i = s.queueIndex, s.queueLength == queueState.sent.count {
            at = queueState.playerIndex(forRoomIndex: i)
        } else if let song = s.current?.id, !(q.items.indices.contains(q.index) && q.items[q.index] == song) {
            at = q.items.indices.first { $0 > q.index && q.items[$0] == song }
                ?? q.items.indices.last { $0 < q.index && $0 >= queueState.offset && q.items[$0] == song }
        } else {
            at = nil
        }
        if let at, at != q.index, q.items.indices.contains(at) { host.remoteMoved(toQueueIndex: at) }
    }

    /// `marquee` goes false for several reasons: the end of the queue is the one that isn't
    /// news. It was on the last song, about at its end (polls are seconds apart).
    private static func wasAtTheEnd(_ last: Speaker?) -> Bool {
        guard let last, last.marquee, let i = last.queueIndex, let n = last.queueLength, i + 1 >= n,
              let duration = last.durationMs, duration > 0 else { return false }
        return duration - (last.positionMs ?? 0) <= 8000
    }

    /// The room played its queue out. With repeat on the player sends it again; a radio waits here
    /// for its next songs; otherwise it's over, and nothing plays on here either.
    private func reachedTheEnd() {
        guard let host else { return }
        generation += 1
        playRun += 1
        let run = playRun
        endPolling()
        playedRoom = nil // it ended there by itself: nothing to stop
        host.remoteFinished()
        guard run == playRun else { return } // sent again (repeat)
        guard host.speakerWaitsForMore else {
            endRoom()
            host.detachSpeaker()
            return
        }
        // A radio: its next songs usually come in moments, and go to the room. If they don't, the
        // room isn't "playing" anything, so this doesn't pretend for long.
        let wait = moreWait
        moreTask = Task { [weak self] in
            try? await Task.sleep(for: wait)
            guard !Task.isCancelled, let self, run == self.playRun else { return }
            let name = self.room?.name ?? "the room"
            self.endRoom()
            self.say("Nothing more to play in \(name).")
            self.host?.stopWaitingForMore()
            self.host?.detachSpeaker()
        }
    }

    /// A command didn't work. That alone doesn't end things: a 409 can be the speaker refusing
    /// one command, and a 5xx can be a moment's trouble. A fresh look at the room decides.
    private func commandFailed(_ error: any Error) async {
        if case SpeakerError.gone = error {
            lost(reason: "\(room?.name ?? "The room") isn't available any more.")
            return
        }
        guard let name = room?.name else { return }
        await poll()
        if state != .idle { say("\(name) didn't accept that.") }
    }

    /// The room isn't ours any more: nothing plays, and the queue stays here, paused, where it was.
    private func lost(reason: String) {
        guard state != .idle else { return }
        endRoom()
        playedRoom = nil // no longer ours to stop
        say(reason)
        host?.remoteUpdate(playing: false, time: host?.speakerTime ?? 0, duration: 0)
        host?.detachSpeaker()
        host?.showError(reason)
    }

    private func say(_ message: String) {
        notice = message
        noticeTask?.cancel()
        noticeTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(6))
            guard !Task.isCancelled else { return }
            self?.notice = nil
        }
    }

    private func startPolling() {
        guard autoPoll, pollTask == nil else { return }
        pollFailures = 0
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: SonosSession.pollInterval)
                guard !Task.isCancelled, let self else { return }
                await self.poll()
            }
        }
    }

    private func endPolling() {
        pollTask?.cancel()
        pollTask = nil
        pollFailures = 0
    }
}
