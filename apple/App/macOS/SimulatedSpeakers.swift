#if DEBUG
import Foundation
import MarqueeKit

/// A pretend Sonos room for trying the picker and the bar (-marquee-test-speakers). It never
/// reaches the network, so no real speaker can start playing while testing.
@MainActor
final class SimulatedSpeakers: SpeakerAPI {
    private var status = Speaker(id: "SIM_ROOM", name: "Test Room", model: "Simulated One", state: .stopped, volume: 25, marquee: false)
    private var queue: [Int64] = []
    private var startedAt: Date?
    private var base: Int64 = 0
    /// Set with -marquee-test-speakers-takeover: another app takes over after this long.
    private let takeoverAfter: Double? = {
        let a = ProcessInfo.processInfo.arguments
        guard let i = a.firstIndex(of: "-marquee-test-speakers-takeover"), a.indices.contains(i + 1) else { return nil }
        return Double(a[i + 1])
    }()
    private var playingSince: Date?

    func speakers() async throws -> [Speaker] { [status] }

    func speaker(_ id: String) async throws -> Speaker {
        if let startedAt, status.state == .playing {
            status.positionMs = base + Int64(Date().timeIntervalSince(startedAt) * 1000)
        }
        if let takeoverAfter, let playingSince, Date().timeIntervalSince(playingSince) > takeoverAfter { status.marquee = false }
        return status
    }

    func play(_ id: String, itemIDs: [Int64], index: Int, startMs: Int64, context: String?) async throws -> Speaker {
        print("SimulatedSpeakers: play \(itemIDs.prefix(5)) (\(itemIDs.count)) at \(startMs) ms")
        queue = itemIDs
        status.marquee = true
        status.state = .playing
        status.queueIndex = index
        status.queueLength = itemIDs.count
        status.durationMs = 40_000
        base = startMs
        startedAt = Date()
        playingSince = Date()
        status.positionMs = startMs
        return status
    }

    func addToQueue(_ id: String, itemIDs: [Int64]) async throws -> Speaker {
        print("SimulatedSpeakers: queue +\(itemIDs.count)")
        queue += itemIDs
        status.queueLength = queue.count
        return status
    }

    func stopper(_ id: String) -> @MainActor () async throws -> Void {
        { [self] in _ = try await control(id, .stop) }
    }

    func control(_ id: String, _ command: SpeakerCommand) async throws -> Speaker {
        print("SimulatedSpeakers: \(command)")
        switch command {
        case .pause:
            _ = try await speaker(id)
            base = status.positionMs ?? 0
            status.state = .paused
        case .resume:
            status.state = .playing
            startedAt = Date()
        case .stop:
            status.state = .stopped
            status.marquee = false
            playingSince = nil
        case .next:
            status.queueIndex = (status.queueIndex ?? 0) + 1
            base = 0
            startedAt = Date()
        case .previous:
            status.queueIndex = max(0, (status.queueIndex ?? 0) - 1)
            base = 0
            startedAt = Date()
        case .seek(let ms):
            base = ms
            startedAt = Date()
            status.positionMs = ms
        case .setVolume(let v):
            status.volume = v
        }
        return status
    }
}
#endif
