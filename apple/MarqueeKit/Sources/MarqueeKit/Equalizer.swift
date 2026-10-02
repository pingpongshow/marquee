import AVFoundation
import Foundation
import MediaToolbox
import Observation
import os

/// A ten-band music equaliser. AVPlayer can't take an AVAudioUnitEQ, so the filters run in an
/// MTAudioProcessingTap on each track's audio mix: biquads at the web client's frequencies
/// (low shelf, eight peaking bands, high shelf). Settings are kept per device.
@MainActor @Observable
public final class Equalizer {
    /// Band centres in Hz.
    public nonisolated static let bands: [Double] = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
    public static let maxGain = 12.0

    public struct Preset: Identifiable, Sendable {
        public let name: String
        public let gains: [Double]
        public var id: String { name }
    }

    public static let presets: [Preset] = [
        .init(name: "Flat", gains: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]),
        .init(name: "Bass Boost", gains: [6, 5, 4, 2, 0.5, 0, 0, 0, 0, 0]),
        .init(name: "Bass Reducer", gains: [-6, -5, -4, -2, -0.5, 0, 0, 0, 0, 0]),
        .init(name: "Treble Boost", gains: [0, 0, 0, 0, 0, 0.5, 2, 4, 5, 6]),
        .init(name: "Vocal", gains: [-2, -2, -1, 1, 3, 4, 3, 1, 0, -1]),
        .init(name: "Rock", gains: [5, 4, 3, 1, -1, -1, 1, 3, 4, 5]),
        .init(name: "Pop", gains: [-1, 0, 2, 3, 4, 3, 1, 0, -1, -1]),
        .init(name: "Jazz", gains: [3, 2, 1, 2, -1, -1, 0, 1, 2, 3]),
        .init(name: "Classical", gains: [4, 3, 2, 1, -1, -1, 0, 2, 3, 4]),
        .init(name: "Electronic", gains: [5, 4, 1, 0, -2, 1, 0, 1, 4, 5]),
        .init(name: "Loudness", gains: [6, 4, 0, 0, -2, 0, -1, -2, 4, 2]),
    ]

    /// The name shown when the bands don't match a preset.
    public static let custom = "Custom"

    public var enabled: Bool {
        didSet {
            UserDefaults.standard.set(enabled, forKey: "marquee.eq.enabled")
            publish()
            onChange?()
        }
    }

    /// Per-band gains in dB, ±12.
    public private(set) var gains: [Double]
    /// The preset in use, or "Custom".
    public private(set) var preset: String

    /// Called when the equaliser is switched on or off (the player re-attaches its taps).
    @ObservationIgnored var onChange: (() -> Void)?
    /// What the audio threads read.
    @ObservationIgnored let state = EQState()

    public init() {
        let d = UserDefaults.standard
        enabled = d.bool(forKey: "marquee.eq.enabled")
        let saved = (d.array(forKey: "marquee.eq.gains") as? [Double]) ?? []
        gains = saved.count == Self.bands.count ? saved.map { min(Self.maxGain, max(-Self.maxGain, $0)) } : Array(repeating: 0, count: Self.bands.count)
        preset = d.string(forKey: "marquee.eq.preset") ?? "Flat"
        publish()
    }

    public func apply(_ p: Preset) {
        gains = p.gains
        preset = p.name
        save()
    }

    /// Sets one band (custom gains).
    public func setGain(_ band: Int, _ db: Double) {
        guard gains.indices.contains(band) else { return }
        gains[band] = min(Self.maxGain, max(-Self.maxGain, (db * 2).rounded() / 2))
        preset = Self.presets.first { $0.gains == gains }?.name ?? Self.custom
        save()
    }

    private func save() {
        let d = UserDefaults.standard
        d.set(gains, forKey: "marquee.eq.gains")
        d.set(preset, forKey: "marquee.eq.preset")
        publish()
    }

    private func publish() {
        state.update(enabled: enabled, gains: gains)
    }

    /// A new tap for one player item (each item needs its own filter state).
    nonisolated func makeTap() -> MTAudioProcessingTap? { EqualizerTap.make(state) }

    public static func label(_ hz: Double) -> String { hz >= 1000 ? "\(Int(hz / 1000))k" : "\(Int(hz))" }
}

/// The equaliser settings, shared with the audio threads. The render callback never waits:
/// it takes the lock only when it's free and otherwise keeps the settings it had.
final class EQState: @unchecked Sendable {
    struct Snapshot {
        var enabled = false
        var gains: [Double] = []
        var version = 0
    }

    private let lock = OSAllocatedUnfairLock(initialState: Snapshot())

    func update(enabled: Bool, gains: [Double]) {
        lock.withLock { s in
            s.enabled = enabled
            s.gains = gains
            s.version += 1
        }
    }

    /// The settings, unless another thread is changing them right now.
    func snapshot(ifNewerThan version: Int) -> Snapshot? {
        lock.withLockIfAvailable { s in s.version != version ? s : nil } ?? nil
    }
}

/// One tap's filters: per band, coefficients and per-channel state (transposed direct form II).
private final class EqualizerTap {
    private let state: EQState
    private var version = -1
    private var enabled = false
    private var sampleRate = 44100.0
    private var usable = false
    private var interleaved = false
    private var channels = 2
    /// Active bands only: b0, b1, b2, a1, a2.
    private var coeffs: [(Double, Double, Double, Double, Double)] = []
    /// [band][channel] → (z1, z2).
    private var z: [[(Double, Double)]] = []
    private var preamp = 1.0

    private init(state: EQState) { self.state = state }

    static func make(_ state: EQState) -> MTAudioProcessingTap? {
        let ctx = EqualizerTap(state: state)
        var callbacks = MTAudioProcessingTapCallbacks(
            version: kMTAudioProcessingTapCallbacksVersion_0,
            clientInfo: Unmanaged.passRetained(ctx).toOpaque(),
            init: { _, clientInfo, storage in storage.pointee = clientInfo },
            finalize: { tap in Unmanaged<EqualizerTap>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).release() },
            prepare: { tap, _, format in
                Unmanaged<EqualizerTap>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).takeUnretainedValue().prepare(format.pointee)
            },
            unprepare: nil,
            process: { tap, frames, _, buffers, framesOut, flagsOut in
                guard MTAudioProcessingTapGetSourceAudio(tap, frames, buffers, flagsOut, nil, framesOut) == noErr else { return }
                let ctx = Unmanaged<EqualizerTap>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).takeUnretainedValue()
                ctx.process(UnsafeMutableAudioBufferListPointer(buffers), frames: Int(framesOut.pointee))
            })
        var tap: MTAudioProcessingTap?
        guard MTAudioProcessingTapCreate(kCFAllocatorDefault, &callbacks, kMTAudioProcessingTapCreationFlag_PostEffects, &tap) == noErr else {
            // The context was retained for the tap that never came.
            Unmanaged<EqualizerTap>.fromOpaque(callbacks.clientInfo!).release()
            return nil
        }
        return tap
    }

    private func prepare(_ f: AudioStreamBasicDescription) {
        sampleRate = f.mSampleRate > 0 ? f.mSampleRate : 44100
        usable = f.mFormatID == kAudioFormatLinearPCM && (f.mFormatFlags & kAudioFormatFlagIsFloat) != 0 && f.mBitsPerChannel == 32
        interleaved = (f.mFormatFlags & kAudioFormatFlagIsNonInterleaved) == 0
        channels = max(1, Int(f.mChannelsPerFrame))
        version = -1 // recompute for this rate
    }

    private func refresh() {
        guard let s = state.snapshot(ifNewerThan: version) else { return }
        version = s.version
        enabled = s.enabled
        var c: [(Double, Double, Double, Double, Double)] = []
        for (i, g) in s.gains.enumerated() where abs(g) >= 0.05 && i < Equalizer.bands.count {
            let f = Equalizer.bands[i]
            guard f < sampleRate * 0.45 else { continue }
            let kind: Biquad = i == 0 ? .lowShelf : i == Equalizer.bands.count - 1 ? .highShelf : .peaking
            c.append(kind.coefficients(f: f, gainDB: g, sampleRate: sampleRate))
        }
        // Headroom for boosts, so a boosted band doesn't clip.
        let boost = max(0, s.gains.max() ?? 0)
        preamp = pow(10, -boost * 0.6 / 20)
        // Fresh filter state for the new settings (changes are rare; this is sized for the format).
        z = Array(repeating: Array(repeating: (0, 0), count: channels), count: c.count)
        coeffs = c
    }

    private func process(_ buffers: UnsafeMutableAudioBufferListPointer, frames: Int) {
        refresh()
        guard enabled, usable, frames > 0 else { return }
        let gain = Float(preamp)
        for (bi, buffer) in buffers.enumerated() {
            guard let data = buffer.mData?.assumingMemoryBound(to: Float.self) else { continue }
            let chans = interleaved ? Int(buffer.mNumberChannels) : 1
            for ch in 0..<chans {
                let channel = interleaved ? ch : bi
                guard channel < channels else { continue }
                var i = ch
                let end = frames * chans
                while i < end {
                    var x = Double(data[i] * gain)
                    for b in 0..<coeffs.count {
                        let (b0, b1, b2, a1, a2) = coeffs[b]
                        let (z1, z2) = z[b][channel]
                        let y = b0 * x + z1
                        z[b][channel] = (b1 * x - a1 * y + z2, b2 * x - a2 * y)
                        x = y
                    }
                    data[i] = Float(min(1, max(-1, x)))
                    i += chans
                }
            }
        }
    }
}

/// RBJ audio-cookbook filters.
enum Biquad {
    case lowShelf, peaking, highShelf

    func coefficients(f: Double, gainDB: Double, sampleRate: Double) -> (Double, Double, Double, Double, Double) {
        let a = pow(10, gainDB / 40)
        let w0 = 2 * Double.pi * f / sampleRate
        let cosw = cos(w0), sinw = sin(w0)
        var b0, b1, b2, a0, a1, a2: Double
        switch self {
        case .peaking:
            let alpha = sinw / (2 * 1.4)
            b0 = 1 + alpha * a; b1 = -2 * cosw; b2 = 1 - alpha * a
            a0 = 1 + alpha / a; a1 = -2 * cosw; a2 = 1 - alpha / a
        case .lowShelf:
            let s = 2 * sqrt(a) * sinw / 2 * sqrt(2.0)
            b0 = a * ((a + 1) - (a - 1) * cosw + s)
            b1 = 2 * a * ((a - 1) - (a + 1) * cosw)
            b2 = a * ((a + 1) - (a - 1) * cosw - s)
            a0 = (a + 1) + (a - 1) * cosw + s
            a1 = -2 * ((a - 1) + (a + 1) * cosw)
            a2 = (a + 1) + (a - 1) * cosw - s
        case .highShelf:
            let s = 2 * sqrt(a) * sinw / 2 * sqrt(2.0)
            b0 = a * ((a + 1) + (a - 1) * cosw + s)
            b1 = -2 * a * ((a - 1) + (a + 1) * cosw)
            b2 = a * ((a + 1) + (a - 1) * cosw - s)
            a0 = (a + 1) - (a - 1) * cosw + s
            a1 = 2 * ((a - 1) - (a + 1) * cosw)
            a2 = (a + 1) - (a - 1) * cosw - s
        }
        return (b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0)
    }
}
