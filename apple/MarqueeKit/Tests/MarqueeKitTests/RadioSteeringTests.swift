import Foundation
import Testing
@testable import MarqueeKit

/// #expect can't take a mutating call.
private func movedOn(_ s: inout RadioSteering, from id: Int64, at time: Double, duration: Double) -> Bool {
    s.movedOn(from: id, at: time, duration: duration)
}

@Test func quickSkipsAndPlaysThroughAreTold() {
    #expect(RadioSteering.isQuickSkip(at: 10, duration: 200))
    #expect(!RadioSteering.isQuickSkip(at: 31, duration: 200))
    #expect(!RadioSteering.isQuickSkip(at: 25, duration: 40)) // past half way
    #expect(RadioSteering.isPlayedThrough(at: 160, duration: 200))
    #expect(!RadioSteering.isPlayedThrough(at: 159, duration: 200))
    #expect(!RadioSteering.isPlayedThrough(at: 10, duration: 0))
}

@Test func twoQuickSkipsInARowTopUpEarly() {
    var s = RadioSteering()
    #expect(!movedOn(&s, from: 1, at: 5, duration: 200))
    #expect(movedOn(&s, from: 2, at: 5, duration: 200))
    #expect(s.skipped == [1, 2])
    s.toppedUp()
    #expect(!movedOn(&s, from: 3, at: 5, duration: 200))
    // Played through: the run is over, and the track counts as finished.
    s.finish(4)
    #expect(!movedOn(&s, from: 5, at: 5, duration: 200))
    #expect(s.finished == [4])
    // A skip part-way isn't quick, and breaks the run too.
    #expect(!movedOn(&s, from: 6, at: 90, duration: 200))
    #expect(!movedOn(&s, from: 7, at: 5, duration: 200))
    #expect(movedOn(&s, from: 8, at: 5, duration: 200))
    // Moving on after 80% is a play-through, not a skip.
    #expect(!movedOn(&s, from: 9, at: 190, duration: 200))
    #expect(s.finished == [4, 9])
    #expect(s.skipped == [1, 2, 3, 5, 7, 8])
}

@Test func steeringKeepsTheLatestHundredAndFillsTheRequest() {
    var s = RadioSteering()
    for id in 1...150 { _ = s.movedOn(from: Int64(id), at: 1, duration: 100) }
    s.finish(7)
    #expect(s.skipped.count == 100)
    #expect(s.skipped.first == 51 && s.skipped.last == 150)
    s.finish(150) // the listener's last word counts: no longer a skip
    #expect(!s.skipped.contains(150))
    var r = RadioRequest(seed: .library)
    s.apply(to: &r)
    #expect(r.skipped == s.skipped)
    #expect(r.finished == [7, 150])
    var empty = RadioRequest(seed: .library)
    RadioSteering().apply(to: &empty)
    #expect(empty.skipped == nil && empty.finished == nil)
}
