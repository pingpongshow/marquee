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
