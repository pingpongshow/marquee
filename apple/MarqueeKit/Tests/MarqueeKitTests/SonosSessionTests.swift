import Foundation
import Testing
@testable import MarqueeKit

/// A stub of the server's /speakers calls: no real speaker is involved.
@MainActor
private final class StubAPI: SpeakerAPI {
    var list: [Speaker] = []
    var status: Speaker
    var calls: [String] = []
    var failPolls = false
    var conflictOnQueue = false
    var failControl = false
    var failPlay = false
    /// The app's connection has gone: only what held on to it can still reach the server.
    var disconnected = false
    /// A play waits here until it's let go.
    var holdPlay = false
    var release: CheckedContinuation<Void, Never>?

    init(_ speaker: Speaker) { status = speaker }

    func speakers() async throws -> [Speaker] { list }

    func speaker(_ id: String) async throws -> Speaker {
        if failPolls { throw SpeakerError.failed("no answer") }
        return status
    }

    func play(_ id: String, itemIDs: [Int64], index: Int, startMs: Int64, context: String?) async throws -> Speaker {
        calls.append("play \(id) \(itemIDs) \(index) \(startMs) \(context ?? "-")")
        if holdPlay { await withCheckedContinuation { release = $0 } }
        if failPlay { throw SpeakerError.failed("no") }
        if let other = list.first(where: { $0.id == id }) { status.name = other.name }
        status.marquee = true
        status.state = .playing
        status.queueIndex = 0
        status.queueLength = itemIDs.count
        status.positionMs = startMs
        return status
    }

    func addToQueue(_ id: String, itemIDs: [Int64]) async throws -> Speaker {
        calls.append("queue \(id) \(itemIDs)")
        if conflictOnQueue { throw SpeakerError.notMarquee }
        status.queueLength = (status.queueLength ?? 0) + itemIDs.count
        return status
    }

    func stopper(_ id: String) -> @MainActor () async throws -> Void {
        { [self] in calls.append("control \(id) stop") }
    }

    func control(_ id: String, _ command: SpeakerCommand) async throws -> Speaker {
        if disconnected { throw SpeakerError.failed("Not connected") }
        calls.append("control \(id) \(command)")
        if failControl { throw SpeakerError.failed("no") }
        switch command {
        case .pause: status.state = .paused
        case .resume: status.state = .playing
        case .stop: status.state = .stopped
        case .next: status.queueIndex = (status.queueIndex ?? 0) + 1
        case .previous: status.queueIndex = max(0, (status.queueIndex ?? 0) - 1)
        case .seek(let ms): status.positionMs = ms
        case .setVolume(let v): status.volume = v
        }
        return status
    }
}

/// The music player's side, without AVFoundation.
@MainActor
private final class StubHost: SpeakerMusicHost {
    var items: [Int64] = [10, 11, 12, 13]
    var index = 1
    var time = 42.5
    var context: String? = "album"
    var attached: (any RemoteQueuePlayback)?
    var playing = false
    var duration = 0.0
    var errors: [String] = []
    var detachCount = 0
    var finished = 0
    var localResumeAt: Double?
    var speakerWaitsForMore = false
    var onFinished: (() -> Void)?
    var stoppedWaiting = 0

    var speakerQueue: (items: [Int64], index: Int) { (items, index) }
    var speakerTime: Double { time }
    var speakerContext: String? { context }

    func attachSpeaker(_ remote: any RemoteQueuePlayback) {
        attached = remote
        // As MusicPlayer's rebuild does.
        remote.play(track(items[index]), at: time, context: context)
    }

    func detachSpeaker() {
        attached = nil
        detachCount += 1
        localResumeAt = time
    }

    func remoteUpdate(playing: Bool, time: Double, duration: Double) {
        self.playing = playing
        self.time = time
        if duration > 0 { self.duration = duration }
    }

    func remoteMoved(toQueueIndex index: Int) { self.index = index }
    func remoteFinished() { finished += 1; onFinished?() }
    func stopWaitingForMore() { stoppedWaiting += 1 }
    func showError(_ message: String) { errors.append(message) }
}

private func track(_ id: Int64) -> Item {
    Item(id: id, libraryId: 1, _type: .track, title: "T\(id)", childCount: 0, leafCount: 0, available: true, matchState: .local, addedAt: Date())
}

private func room(_ id: String = "RINCON_1", name: String = "Living Room") -> Speaker {
    Speaker(id: id, name: name, model: "Sonos Arc", state: .stopped, volume: 30, marquee: false)
}

@MainActor
private func connected() async -> (SonosSession, StubAPI, StubHost) {
    let api = StubAPI(room()), host = StubHost()
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    await s.settled()
    return (s, api, host)
}

@MainActor @Test func pickingARoomSendsTheQueueFromTheCurrentTrackAndPosition() async {
    let (s, api, host) = await connected()
    #expect(api.calls == ["play RINCON_1 [11, 12, 13] 0 42500 album"])
    #expect(s.state == .playing("Living Room"))
    #expect(s.activeRoomName == "Living Room")
    #expect(host.attached != nil)
    #expect(host.playing)
}

@MainActor @Test func nothingToPlayMeansNoRoom() async {
    let api = StubAPI(room()), host = StubHost()
    host.index = -1
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    await s.settled()
    #expect(api.calls.isEmpty)
    #expect(s.state == .idle)
}

@MainActor @Test func pollKeepsTheBarInStep() async {
    let (s, api, host) = await connected()
    api.status.positionMs = 90_000
    api.status.durationMs = 200_000
    api.status.state = .paused
    await s.poll()
    #expect(host.time == 90)
    #expect(host.duration == 200)
    #expect(!host.playing)
}

@MainActor @Test func theRoomMovingOnMovesTheQueue() async {
    let (s, api, host) = await connected()
    api.status.queueIndex = 2 // the third of what was sent: entry 3 of the player's queue
    await s.poll()
    #expect(host.index == 3)
}

@MainActor @Test func transportGoesToTheRoom() async {
    let (s, api, _) = await connected()
    s.pause()
    await s.settled()
    s.resume()
    await s.settled()
    s.seek(12)
    await s.settled()
    #expect(s.skip(forward: true))
    await s.settled()
    s.setVolume(55)
    try? await Task.sleep(for: .milliseconds(250))
    #expect(api.calls.dropFirst() == ["control RINCON_1 pause", "control RINCON_1 resume",
                                      "control RINCON_1 seek(ms: 12000)", "control RINCON_1 next",
                                      "control RINCON_1 setVolume(55)"])
    #expect(s.volume == 55)
}

@MainActor @Test func backPastWhatWasSentIsLeftToThePlayer() async {
    let (s, api, host) = await connected()
    // The first track sent: the room has nothing before it.
    #expect(!s.skip(forward: false))
    #expect(api.calls.count == 1)
    // Which rebuilds, calling play() from the earlier entry.
    host.index = 0
    s.play(track(10), at: 0, context: nil)
    await s.settled()
    #expect(api.calls.last == "play RINCON_1 [10, 11, 12, 13] 0 0 album")
}

@MainActor @Test func aRadioTopUpIsAddedNotResent() async {
    let (s, api, host) = await connected()
    host.items += [20, 21]
    s.queueChanged()
    await s.settled()
    #expect(api.calls.last == "queue RINCON_1 [20, 21]")
    // Nothing more to add the second time.
    s.queueChanged()
    await s.settled()
    #expect(api.calls.count == 2)
}

@MainActor @Test func aTopUpAfterTheRoomMovedOnStillOnlyAdds() async {
    let (s, api, host) = await connected()
    api.status.queueIndex = 1
    await s.poll()
    #expect(host.index == 2)
    host.items += [30]
    s.queueChanged()
    await s.settled()
    #expect(api.calls.last == "queue RINCON_1 [30]")
}

@MainActor @Test func aReorderedQueueIsSentAgainFromThePosition() async {
    let (s, api, host) = await connected()
    host.items = [10, 11, 13, 12]
    host.time = 50
    s.queueChanged()
    await s.settled()
    #expect(api.calls.last == "play RINCON_1 [11, 13, 12] 0 50000 album")
}

@MainActor @Test func anotherAppTakingOverReturnsToIdleWithANotice() async {
    let (s, api, host) = await connected()
    api.status.marquee = false
    await s.poll()
    #expect(s.state == .idle)
    #expect(s.notice == "Living Room isn't playing Marquee any more.")
    #expect(host.attached == nil)
    #expect(!host.playing)
    #expect(host.errors == ["Living Room isn't playing Marquee any more."])
    // No stop is sent: the other app's music isn't ours to stop.
    #expect(!api.calls.contains { $0.contains("stop") })
}

@MainActor @Test func a409WhenAnotherAppHasTheRoomEndsIt() async {
    let (s, api, host) = await connected()
    api.conflictOnQueue = true
    api.status.marquee = false
    host.items += [20]
    s.queueChanged()
    await s.settled()
    #expect(s.state == .idle)
    #expect(s.notice == "Living Room isn't playing Marquee any more.")
}

@MainActor @Test func a409WhileStillOursIsARefusalNotAnEnd() async {
    let (s, api, host) = await connected()
    api.conflictOnQueue = true
    host.items += [20]
    s.queueChanged()
    await s.settled()
    #expect(s.state == .playing("Living Room"))
    #expect(s.notice == "Living Room didn't accept that.")
    // It's sent again next time.
    api.conflictOnQueue = false
    s.queueChanged()
    await s.settled()
    #expect(api.calls.last == "queue RINCON_1 [20]")
    #expect(api.calls.filter { $0 == "queue RINCON_1 [20]" }.count == 2)
}

@MainActor @Test func aFailedCommandIsNotTheEndEither() async {
    let (s, api, _) = await connected()
    api.failControl = true
    s.pause()
    await s.settled()
    #expect(s.state == .playing("Living Room"))
    #expect(s.notice == "Living Room didn't accept that.")
}

@MainActor @Test func songsTheServerDroppedDontThrowTheQueueOff() async {
    let (s, api, host) = await connected()
    // Sent [11, 12, 13]; it kept two, and is on 13, its second.
    api.status.queueLength = 2
    api.status.queueIndex = 1
    api.status.current = track(13)
    await s.poll()
    #expect(host.index == 3)
}

@MainActor @Test func noCurrentSongAndMismatchedLengthsLeaveTheQueueAlone() async {
    let (s, api, host) = await connected()
    api.status.queueLength = 2
    api.status.queueIndex = 1
    api.status.current = nil
    await s.poll()
    #expect(host.index == 1)
}

@MainActor @Test func loadingCountsAsPlaying() async {
    let (s, api, host) = await connected()
    api.status.state = .loading
    await s.poll()
    #expect(host.playing)
}

@MainActor @Test func leavingStopsTheRoomAndCarriesOnHere() async {
    let (s, api, host) = await connected()
    api.status.positionMs = 70_000
    await s.poll()
    s.leave()
    try? await Task.sleep(for: .milliseconds(50))
    #expect(api.calls.last == "control RINCON_1 stop")
    #expect(s.state == .idle)
    #expect(host.localResumeAt == 70)
    #expect(s.notice == nil)
}

@MainActor @Test func aLostRoomEndsAfterAFewFailedPolls() async {
    let (s, api, host) = await connected()
    api.failPolls = true
    for _ in 1..<SonosSession.maxPollFailures {
        await s.poll()
        #expect(s.state != .idle)
    }
    await s.poll()
    #expect(s.state == .idle)
    #expect(host.errors == ["Lost contact with Living Room."])
}

/// On the last song, about at its end.
@MainActor
private func atTheEnd(moreWait: Duration = SonosSession.defaultMoreWait) async -> (SonosSession, StubAPI, StubHost) {
    let api = StubAPI(room()), host = StubHost()
    let s = SonosSession(api: api, host: host, autoPoll: false, moreWait: moreWait)
    s.select(room())
    await s.settled()
    api.status.queueIndex = 2
    api.status.positionMs = 196_000
    api.status.durationMs = 200_000
    await s.poll()
    api.status.marquee = false
    api.status.queueIndex = nil
    api.status.queueLength = nil
    return (s, api, host)
}

@MainActor @Test func theEndOfTheQueueIsQuietlyIdle() async {
    let (s, _, host) = await atTheEnd()
    await s.poll()
    #expect(host.finished == 1)
    #expect(s.state == .idle)
    #expect(s.notice == nil)
    #expect(host.errors.isEmpty)
    #expect(host.attached == nil)
}

@MainActor @Test func aRadioWaitsInTheRoomAtTheEnd() async {
    let (s, api, host) = await atTheEnd()
    host.speakerWaitsForMore = true
    await s.poll()
    #expect(host.finished == 1)
    #expect(s.state == .playing("Living Room"))
    // Its next songs arrive: the player plays them, which goes to the room.
    host.items += [40]
    host.index = 4
    s.play(track(40), at: 0, context: nil)
    await s.settled()
    #expect(api.calls.last == "play RINCON_1 [40] 0 0 album")
}

@MainActor @Test func repeatSendsTheQueueAgainFromTheEnd() async {
    let (s, api, host) = await atTheEnd()
    host.onFinished = { [weak s] in
        host.index = 0
        s?.play(track(10), at: 0, context: nil)
    }
    await s.poll()
    await s.settled()
    #expect(s.state == .playing("Living Room"))
    #expect(api.calls.last == "play RINCON_1 [10, 11, 12, 13] 0 0 album")
}

@MainActor @Test func marqueeGoingFalseMidSongIsATakeOver() async {
    let (s, api, host) = await connected()
    api.status.queueIndex = 2
    api.status.positionMs = 50_000
    api.status.durationMs = 200_000
    await s.poll()
    api.status.marquee = false
    await s.poll()
    #expect(s.state == .idle)
    #expect(s.notice != nil)
    #expect(host.finished == 0)
}

@MainActor @Test func movingToAnotherRoomStopsTheFirst() async {
    let (s, api, host) = await connected()
    host.time = 10
    api.list = [room("RINCON_2", name: "Kitchen")]
    s.select(room("RINCON_2", name: "Kitchen"))
    await s.settled()
    try? await Task.sleep(for: .milliseconds(50))
    #expect(api.calls.contains("control RINCON_1 stop"))
    #expect(api.calls.contains { $0.hasPrefix("play RINCON_2 [11, 12, 13] 0 10000") })
    #expect(s.state == .playing("Kitchen"))
}

@MainActor @Test func roomsListAndEmptyList() async {
    let api = StubAPI(room()), host = StubHost()
    let s = SonosSession(api: api, host: host, autoPoll: false)
    await s.refreshRooms()
    #expect(s.rooms.isEmpty)
    api.list = [room()]
    await s.refreshRooms()
    #expect(s.rooms.map(\.name) == ["Living Room"])
}

/// Lets a play get as far as the stub, where it waits.
@MainActor
private func untilHeld(_ api: StubAPI) async {
    for _ in 0..<200 where api.release == nil { try? await Task.sleep(for: .milliseconds(5)) }
}

@MainActor @Test func aCommandWhileConnectingDoesntStrandTheRoom() async {
    let api = StubAPI(room()), host = StubHost()
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    s.pause()
    await s.settled()
    #expect(api.calls == ["play RINCON_1 [11, 12, 13] 0 42500 album", "control RINCON_1 pause"])
    #expect(s.state == .playing("Living Room"))
    // It isn't stuck starting: polls and top-ups work.
    api.status.positionMs = 9000
    await s.poll()
    #expect(host.time == 9)
    host.items += [20]
    s.queueChanged()
    await s.settled()
    #expect(api.calls.last == "queue RINCON_1 [20]")
}

@MainActor @Test func aFailedPlayIsNoticedEvenAfterACommand() async {
    let api = StubAPI(room()), host = StubHost()
    api.failPlay = true
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    s.pause()
    await s.settled()
    #expect(s.state == .idle)
    #expect(host.attached == nil)
}

@MainActor @Test func aPollWhileConnectingDoesntCountAsATakeOver() async {
    let (s, api, host) = await connected() // the session only holds it weakly
    api.list = [room("RINCON_2", name: "Kitchen")]
    api.holdPlay = true
    s.select(room("RINCON_2", name: "Kitchen"))
    await untilHeld(api)
    api.status.marquee = false // the new room isn't ours yet
    await s.poll()
    #expect(s.state == .connecting("Kitchen"))
    api.holdPlay = false
    api.release?.resume()
    await s.settled()
    #expect(s.state == .playing("Kitchen"))
    #expect(host.attached != nil)
}

@MainActor @Test func leavingBeforeThePlayWentOutSendsNothing() async {
    let api = StubAPI(room()), host = StubHost()
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    s.leave()
    await s.settled()
    #expect(api.calls.isEmpty) // no play, and no Stop for whatever the room's doing
    #expect(s.state == .idle)
}

@MainActor @Test func aPlayInFlightCantLandAfterTheStop() async {
    let api = StubAPI(room()), host = StubHost()
    api.holdPlay = true
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    await untilHeld(api)
    s.leave()
    api.holdPlay = false
    api.release?.resume()
    await s.settled()
    #expect(api.calls == ["play RINCON_1 [11, 12, 13] 0 42500 album", "control RINCON_1 stop"])
    #expect(s.state == .idle)
}

@MainActor @Test func switchingRoomsStopsAfterThePlayInFlight() async {
    let api = StubAPI(room()), host = StubHost()
    api.holdPlay = true
    let s = SonosSession(api: api, host: host, autoPoll: false)
    s.select(room())
    await untilHeld(api)
    api.holdPlay = false
    api.list = [room("RINCON_2", name: "Kitchen")]
    s.select(room("RINCON_2", name: "Kitchen"))
    api.release?.resume()
    await s.settled()
    #expect(api.calls.map { String($0.prefix(20)) } == ["play RINCON_1 [11, 1", "control RINCON_1 sto", "play RINCON_2 [11, 1"])
    #expect(s.state == .playing("Kitchen"))
}

@MainActor @Test func theStopStillGoesOnceTheConnectionHasGone() async {
    let (s, api, host) = await connected()
    let stopping = s.queueStopped()
    api.disconnected = true // forgetting the server drops the client straight after
    await stopping?.value
    #expect(api.calls.last == "control RINCON_1 stop")
    #expect(host.attached == nil)
}

@MainActor @Test func aRadioThatGetsNoMoreStopsPretending() async {
    let (s, _, host) = await atTheEnd(moreWait: .milliseconds(10))
    host.speakerWaitsForMore = true
    await s.poll()
    #expect(s.state == .playing("Living Room"))
    try? await Task.sleep(for: .milliseconds(100))
    #expect(s.state == .idle)
    #expect(s.notice == "Nothing more to play in Living Room.")
    #expect(host.stoppedWaiting == 1)
    #expect(host.attached == nil)
}

@MainActor @Test func aRadiosNextSongsCallOffTheWait() async {
    let (s, _, host) = await atTheEnd(moreWait: .milliseconds(50))
    host.speakerWaitsForMore = true
    await s.poll()
    host.items += [40]
    host.index = 4
    s.play(track(40), at: 0, context: nil)
    await s.settled()
    try? await Task.sleep(for: .milliseconds(120))
    #expect(s.state == .playing("Living Room"))
    #expect(host.stoppedWaiting == 0)
}

@Test func queueStateRetriesWhatFailed() {
    var q = SpeakerQueueState()
    q.didSend([1, 2], offset: 0)
    q.didAppend([3])
    q.didFailToAppend(1)
    #expect(q.change(items: [1, 2, 3], index: 0) == .append([3]))
}

@Test func queueStateDiffs() {
    var q = SpeakerQueueState()
    q.didSend([1, 2, 3], offset: 4)
    #expect(q.change(items: [0, 0, 0, 0, 1, 2, 3], index: 4) == .none)
    #expect(q.change(items: [0, 0, 0, 0, 1, 2, 3, 5], index: 5) == .append([5]))
    #expect(q.change(items: [0, 0, 0, 0, 1, 3, 2], index: 4) == .resend([1, 3, 2]))
    // Went back before what was sent.
    #expect(q.change(items: [0, 0, 0, 0, 1, 2, 3], index: 3) == .resend([0, 1, 2, 3]))
    #expect(q.playerIndex(forRoomIndex: 2) == 6)
}

/// A room picked with nothing playing waits, and takes the music from the first note; picking a
/// Mac output calls it off.
@MainActor @Test func aRoomPickedWithNothingPlayingWaitsForMusic() async {
    let api = StubAPI(room()), host = StubHost()
    let s = SonosSession(api: api, host: host, autoPoll: false)
    let items = host.items, index = host.index
    host.items = []
    host.index = -1
    #expect(s.takeWaitingMusic() == false, "nothing waits yet")
    s.select(room())
    #expect(s.waitingRoom != nil)
    #expect(host.attached == nil, "nothing to send yet")
    #expect(s.isActive == false)
    #expect(s.takeWaitingMusic() == false, "no music yet: it keeps waiting")
    #expect(s.waitingRoom != nil)
    host.items = items
    host.index = index
    #expect(s.takeWaitingMusic() == true)
    #expect(s.waitingRoom == nil)
    #expect(host.attached != nil)
    #expect(s.takeWaitingMusic() == false, "only once")
    await s.settled()

    let empty = StubHost()
    empty.items = []
    empty.index = -1
    let waiting = SonosSession(api: api, host: empty, autoPoll: false)
    waiting.select(room())
    waiting.clearWaitingRoom()
    empty.items = items
    empty.index = index
    #expect(waiting.takeWaitingMusic() == false, "called off")
    #expect(empty.attached == nil)
}
