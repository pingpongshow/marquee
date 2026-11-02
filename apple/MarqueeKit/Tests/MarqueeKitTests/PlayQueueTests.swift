import Foundation
import Testing
@testable import MarqueeKit

private func track(_ id: Int64) -> Item {
    Item(id: id, libraryId: 1, _type: .track, title: "T\(id)", childCount: 0, leafCount: 0, available: true, matchState: .local, addedAt: Date())
}
private func ids(_ q: PlayQueue) -> [Int64] { q.entries.map(\.item.id) }

@Test func loadStartsAtRequestedTrack() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3)], start: 1)
    #expect(q.current?.item.id == 2)
}

@Test func shuffleKeepsCurrentAndRestoresOrder() {
    var q = PlayQueue()
    q.load((1...6).map(track), start: 2)
    q.toggleShuffle()
    #expect(q.current?.item.id == 3)
    #expect(Array(ids(q).prefix(3)) == [1, 2, 3])
    #expect(Set(ids(q).dropFirst(3)) == [4, 5, 6])
    q.toggleShuffle()
    #expect(ids(q) == [1, 2, 3, 4, 5, 6])
    #expect(q.current?.item.id == 3)
}

@Test func playNextGoesAfterCurrentInBothOrders() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3)])
    q.toggleShuffle()
    q.playNext([track(9)])
    #expect(ids(q)[1] == 9)
    q.toggleShuffle()
    #expect(ids(q) == [1, 9, 2, 3])
}

@Test func removeAndMoveKeepCurrent() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3), track(4)], start: 2)
    q.remove(q.entries[0].id)
    #expect(q.current?.item.id == 3)
    q.move(q.entries[2].id, to: 0)
    #expect(ids(q) == [4, 2, 3])
    #expect(q.current?.item.id == 3)
}

@Test func repeatModes() {
    var q = PlayQueue()
    q.load([track(1), track(2)], start: 1)
    #expect(q.followingIndex == -1)
    q.cycleRepeat()
    #expect(q.followingIndex == 0)
    q.cycleRepeat()
    #expect(q.followingIndex == 1)
    #expect(q.skipIndex == 0)
}

@Test func removingTheCurrentLastTrackEndsTheQueue() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3)], start: 2)
    let ended = q.remove(q.entries[2].id)
    #expect(ended)
    #expect(ids(q) == [1, 2])
    // The previous track is current but the player doesn't start it by itself.
    #expect(q.current?.item.id == 2)
    #expect(q.followingIndex == -1)
}

@Test func removingTheCurrentLastTrackWithRepeatAllWraps() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3)], start: 2)
    q.repeatMode = .all
    let ended = q.remove(q.entries[2].id)
    #expect(!ended)
    #expect(q.current?.item.id == 1)
}

@Test func removingTheCurrentTrackMovesOnToTheNext() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3)], start: 1)
    let ended = q.remove(q.entries[1].id)
    #expect(!ended)
    #expect(q.current?.item.id == 3)
    // Removing the only track empties the queue.
    var one = PlayQueue()
    one.load([track(1)])
    let emptied = one.remove(one.entries[0].id)
    #expect(emptied)
    #expect(one.current == nil)
}

@Test func moveWhileShuffledIsKeptWhenShuffleGoesOff() {
    var q = PlayQueue()
    q.load((1...6).map(track), start: 0)
    q.toggleShuffle()
    // Put track 6 right after the current track (1) in the shuffled order.
    let six = q.entries.first { $0.item.id == 6 }!
    q.move(six.id, to: 1)
    #expect(ids(q)[1] == 6)
    #expect(q.current?.item.id == 1)
    q.toggleShuffle()
    // Unshuffled, it follows track 1 too, and nothing is lost or doubled.
    #expect(ids(q) == [1, 6, 2, 3, 4, 5])
    #expect(q.current?.item.id == 1)
}

@Test func moveToTheFrontWhileShuffled() {
    var q = PlayQueue()
    q.load((1...4).map(track), start: 1)
    q.toggleShuffle()
    let four = q.entries.first { $0.item.id == 4 }!
    q.move(four.id, to: 0)
    #expect(ids(q).first == 4)
    #expect(q.current?.item.id == 2)
    q.toggleShuffle()
    #expect(ids(q) == [4, 1, 2, 3])
    #expect(q.current?.item.id == 2)
}

@Test func shuffleMixesTheOpeningTrackToo() {
    var firsts = Set<Int64>()
    for _ in 0..<200 {
        var q = PlayQueue()
        q.load((1...5).map(track), shuffle: true)
        #expect(q.index == 0 && q.shuffled && Set(ids(q)) == Set(1...5))
        firsts.insert(q.current!.item.id)
    }
    #expect(firsts.count == 5)
}

@Test func stationTracksCarryTheirContext() {
    var q = PlayQueue()
    q.load([track(1), track(2)], from: .init(context: "radio:mood"))
    #expect(q.entries.allSatisfy { $0.playContext == "radio:mood" && $0.fromStation })
    // A top-up keeps the station's context; tracks queued by hand have none; the DJ's are "dj".
    q.append([track(3)], from: .init(context: "radio:mood"))
    q.append([track(4)])
    q.playNext([track(5)], dj: "wander")
    #expect(q.entries.map(\.playContext) == ["radio:mood", "dj", "radio:mood", "radio:mood", nil])
    #expect(q.entries.map(\.fromStation) == [true, false, true, true, false])

    var album = PlayQueue()
    album.load([track(1), track(2)])
    #expect(album.entries.allSatisfy { $0.playContext == nil && !$0.fromStation })
}

@Test func replacingAStationsUpcomingTracksKeepsHandQueuedOnes() {
    var q = PlayQueue()
    q.load((1...4).map(track), start: 1, from: .init(context: "radio:track"))
    q.playNext([track(9)])               // by hand: 1 2 9 3 4
    q.append([track(8)])                 // by hand at the end
    q.playNext([track(7)], dj: "wander") // the DJ's: 1 2 7 9 3 4 8
    q.replaceUpcomingStation(with: [track(20), track(21)], from: .init(context: "radio:track"))
    #expect(ids(q) == [1, 2, 7, 9, 20, 21, 8])
    #expect(q.current?.item.id == 2)
    #expect(q.entries.first { $0.item.id == 20 }?.playContext == "radio:track")
}

@Test func replacingWhileShuffledKeepsTheUnshuffledOrderInStep() {
    var q = PlayQueue()
    q.load((1...5).map(track), start: 0, from: .init(context: "radio:library"))
    q.toggleShuffle()
    q.replaceUpcomingStation(with: [track(30)], from: .init(context: "radio:library"))
    #expect(ids(q) == [1, 30])
    q.toggleShuffle()
    #expect(ids(q) == [1, 30])
    #expect(q.current?.item.id == 1)
}

// Endless radio: the server repeats tracks once everything has played.

@Test func aRepeatedTrackIsAnEntryOfItsOwn() {
    var q = PlayQueue()
    q.load([track(1), track(2)], from: .init(context: "radio:item"))
    q.append([track(1), track(2), track(1)], from: .init(context: "radio:item"))
    #expect(ids(q) == [1, 2, 1, 2, 1])
    #expect(Set(q.entries.map(\.id)).count == 5)
}

@Test func aQueueMovesThroughRepeatsOneEntryAtATime() {
    var q = PlayQueue()
    q.load([track(1), track(1), track(1)])
    let last = q.entries[2].id
    q.jump(last)
    #expect(q.index == 2)
    #expect(q.followingIndex == -1)
    q.append([track(1)])
    #expect(q.followingIndex == 3)
    q.setIndex(q.followingIndex)
    #expect(q.current?.id != last)
    // Removing one of them leaves the others.
    q.remove(q.entries[0].id)
    #expect(ids(q) == [1, 1, 1])
    #expect(q.index == 2)
}

@Test func replacingAStationsTracksOnlyTakesTheOnesStillToCome() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(1)], start: 1, from: .init(context: "radio:item"))
    q.replaceUpcomingStation(with: [track(1), track(3)], from: .init(context: "radio:item"))
    #expect(ids(q) == [1, 2, 1, 3])
    #expect(q.current?.item.id == 2)
    #expect(Set(q.entries.map(\.id)).count == 4)
}

@Test func repeatsSurviveShuffleBothWays() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(1), track(2)], start: 0, from: .init(context: nil))
    q.toggleShuffle()
    q.append([track(1)], from: .init(context: nil))
    #expect(q.entries.count == 5)
    q.toggleShuffle()
    #expect(ids(q) == [1, 2, 1, 2, 1])
}

@Test func stationExcludeIsOldestFirstWithEachTrackAtItsLatestPlace() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3)], from: .init(context: nil))
    q.append([track(2), track(4), track(1)], from: .init(context: nil))
    #expect(q.stationExclude() == [3, 2, 4, 1])
    #expect(PlayQueue().stationExclude().isEmpty)
}

@Test func stationExcludeKeepsOnlyTheMostRecent2000() {
    var q = PlayQueue()
    q.load((1...2500).map { track(Int64($0)) })
    let exclude = q.stationExclude()
    #expect(exclude.count == 2000)
    #expect(exclude.first == 501)
    #expect(exclude.last == 2500)
    // Repeats don't use up the limit.
    q.append((2401...2500).map { track(Int64($0)) })
    #expect(q.stationExclude().count == 2000)
    #expect(q.stationExclude().first == 501)
}

// MARK: - A thumbs down on a station (MUSIC-25)

@Test func thumbsDownOnPlayingSongMovesOnAndRemovesCopies() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3), track(2), track(4)], start: 1)
    #expect(q.removeAll(of: 2) == false)
    #expect(ids(q) == [1, 3, 4])
    #expect(q.current?.item.id == 3)
    #expect(q.upcoming.map(\.item.id) == [4])
}

@Test func thumbsDownOnAnotherSongKeepsThePlayingOne() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3), track(4), track(1)], start: 2)
    #expect(q.removeAll(of: 1) == false)
    #expect(ids(q) == [2, 3, 4])
    #expect(q.current?.item.id == 3)
}

@Test func thumbsDownOnSongNotQueuedChangesNothing() {
    var q = PlayQueue()
    q.load([track(1), track(2)], start: 1)
    #expect(q.removeAll(of: 9) == false)
    #expect(ids(q) == [1, 2])
    #expect(q.current?.item.id == 2)
}

@Test func thumbsDownOnLastTrackSaysNothingTookItsPlace() {
    var q = PlayQueue()
    q.load([track(1), track(2)], start: 1)
    #expect(q.removeAll(of: 2) == true)
    #expect(ids(q) == [1])

    var only = PlayQueue()
    only.load([track(1), track(1)])
    #expect(only.removeAll(of: 1) == true)
    #expect(only.entries.isEmpty)
    #expect(only.current == nil)
}

@Test func thumbsDownCantComeRoundAgainOnRepeatAll() {
    var q = PlayQueue()
    q.load([track(5), track(1), track(5), track(2), track(5)], start: 2)
    q.cycleRepeat()
    #expect(q.repeatMode == .all)
    #expect(q.removeAll(of: 5) == false)
    #expect(ids(q) == [1, 2])
    #expect(q.current?.item.id == 2)
    // The last track playing: wraps to the start rather than ending.
    #expect(q.removeAll(of: 2) == false)
    #expect(ids(q) == [1])
    #expect(q.current?.item.id == 1)
}

@Test func thumbsDownDoesntHoldOnRepeatOne() {
    var q = PlayQueue()
    q.load([track(1), track(2)])
    q.cycleRepeat()
    q.cycleRepeat()
    #expect(q.repeatMode == .one)
    #expect(q.removeAll(of: 1) == false)
    #expect(q.current?.item.id == 2)
}

@Test func thumbsDownCantComeBackByTurningShuffleOff() {
    var q = PlayQueue()
    q.load([track(1), track(2), track(3), track(2), track(4)], start: 1)
    q.toggleShuffle()
    #expect(q.current?.item.id == 2)
    #expect(q.removeAll(of: 2) == false)
    #expect(!ids(q).contains(2))
    q.toggleShuffle()
    #expect(ids(q) == [1, 3, 4])
    #expect(q.current != nil)
}

@Test @MainActor func onlyAStationIsASource() {
    #expect(!MusicPlayer.Source(title: "An Album").isStation)
    #expect(MusicPlayer.Source(title: "Mood", context: "mood:calm").isStation)
    #expect(MusicPlayer.Source(title: "Radio", radio: RadioRequest(seed: .item, itemId: 1, limit: 50)).isStation)
    #expect(MusicPlayer.Source(title: "A Journey", generated: true).isStation)
}
