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

    /// A new tap for one player item (each item needs its own filter state). `gain` (linear)
    /// is applied whether or not the equaliser is on: volume levelling raises quiet tracks
    /// here, where an audio mix's volume can't go above 1.
    nonisolated func makeTap(gain: Double = 1) -> MTAudioProcessingTap? { EqualizerTap.make(state, gain: gain) }

    public static func label(_ hz: Double) -> String { hz >= 1000 ? "\(Int(hz / 1000))k" : "\(Int(hz))" }
}

/// The equaliser settings, shared with the audio threads. Coefficients are worked out here,
/// off the audio thread, for each sample rate a tap has asked for; the render callback only
/// copies them into its own buffers, and never waits: it takes the lock only when it's free
/// and otherwise keeps the settings it had.
final class EQState: @unchecked Sendable {
    /// Five coefficients (b0, b1, b2, a1, a2) per band; a band left flat passes audio through.
    static let stride = 5
    static var coefficientCount: Int { Equalizer.bands.count * stride }

    struct Snapshot {
        var enabled = false
        var gains: [Double] = []
        var version = 0
        /// The preamp (linear) that leaves headroom for boosts.
        var preamp = 1.0
        /// Sample rate → coefficients (`coefficientCount` values).
        var coefficients: [Double: [Double]] = [:]
    }

    private let lock = OSAllocatedUnfairLock(initialState: Snapshot())

    func update(enabled: Bool, gains: [Double]) {
        // Worked out on the caller's (main) thread; the render thread never waits for this lock.
        lock.withLock { s in
            s.enabled = enabled
            s.gains = gains
            s.preamp = Self.preamp(gains)
            for r in Array(s.coefficients.keys) { s.coefficients[r] = Self.coefficients(gains: gains, sampleRate: r) }
            s.version += 1
        }
    }

    /// Makes sure coefficients exist for a tap's sample rate (called when a tap is prepared,
    /// not on the render thread).
    func prepare(sampleRate: Double) {
        lock.withLock { s in
            guard s.coefficients[sampleRate] == nil else { return }
            s.coefficients[sampleRate] = Self.coefficients(gains: s.gains, sampleRate: sampleRate)
            s.version += 1
        }
    }

    /// For the render thread: when the settings changed since `version`, copies the
    /// coefficients for `sampleRate` into `into` (no allocation) and returns the new version,
    /// whether it's on and the preamp. Nil when nothing changed or the lock is busy.
    func load(ifNewerThan version: Int, sampleRate: Double, into: UnsafeMutablePointer<Double>) -> (version: Int, enabled: Bool, preamp: Double)? {
        lock.withLockIfAvailableUnchecked { s -> (version: Int, enabled: Bool, preamp: Double)? in
            guard s.version != version else { return nil }
            guard let c = s.coefficients[sampleRate], c.count == Self.coefficientCount else {
                // Not worked out for this rate yet: pass audio through until it is.
                return (s.version, false, 1)
            }
            c.withUnsafeBufferPointer { into.update(from: $0.baseAddress!, count: c.count) }
            return (s.version, s.enabled, s.preamp)
        } ?? nil
    }

    /// All bands' coefficients for a sample rate; flat bands and bands too high for the rate
    /// pass audio through (1, 0, 0, 0, 0).
    static func coefficients(gains: [Double], sampleRate: Double) -> [Double] {
        var out = [Double](repeating: 0, count: coefficientCount)
        for i in 0..<Equalizer.bands.count {
            let g = i < gains.count ? gains[i] : 0
            let f = Equalizer.bands[i]
            var c: (Double, Double, Double, Double, Double) = (1, 0, 0, 0, 0)
            if abs(g) >= 0.05, f < sampleRate * 0.45 {
                let kind: Biquad = i == 0 ? .lowShelf : i == Equalizer.bands.count - 1 ? .highShelf : .peaking
                c = kind.coefficients(f: f, gainDB: g, sampleRate: sampleRate)
            }
            out[i * stride] = c.0
            out[i * stride + 1] = c.1
            out[i * stride + 2] = c.2
            out[i * stride + 3] = c.3
            out[i * stride + 4] = c.4
        }
        return out
    }

    /// Headroom for boosts, so a boosted band doesn't clip.
    static func preamp(_ gains: [Double]) -> Double {
        pow(10, -max(0, gains.max() ?? 0) * 0.6 / 20)
    }
}

/// One tap's filters: per band, coefficients and per-channel state (transposed direct form II),
/// in buffers allocated once. The filter state carries over when the settings change, and the
/// preamp moves smoothly, so dragging a slider doesn't click. It also carries the track's
/// levelling boost (a fixed gain for the item's life).
final class EqualizerTap {
    /// Channels the filter state has room for (more are left untouched).
    private static let maxChannels = 16
    private let state: EQState
    private var version = -1
    private var enabled = false
    private var sampleRate = 44100.0
    private var usable = false
    private var interleaved = false
    private var channels = 2
    private let bandCount = Equalizer.bands.count
    /// `bandCount` × (b0, b1, b2, a1, a2).
    private let coeffs: UnsafeMutablePointer<Double>
    /// [band][channel] × (z1, z2).
    private let z: UnsafeMutablePointer<Double>
    private var preamp = 1.0
    /// The preamp actually applied, gliding to `preamp` over each buffer.
    private var appliedPreamp = 1.0
    /// Volume levelling's boost for this item (1 = none).
    private let gain: Double

    private init(state: EQState, gain: Double) {
        self.state = state
        self.gain = gain.isFinite && gain > 0 ? gain : 1
        coeffs = .allocate(capacity: EQState.coefficientCount)
        z = .allocate(capacity: Equalizer.bands.count * Self.maxChannels * 2)
        for b in 0..<bandCount {
            (coeffs + b * EQState.stride).initialize(repeating: 0, count: EQState.stride)
            coeffs[b * EQState.stride] = 1
        }
        z.initialize(repeating: 0, count: bandCount * Self.maxChannels * 2)
    }

    deinit {
        coeffs.deallocate()
        z.deallocate()
    }

    static func make(_ state: EQState, gain: Double) -> MTAudioProcessingTap? {
        let ctx = EqualizerTap(state: state, gain: gain)
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
        channels = min(Self.maxChannels, max(1, Int(f.mChannelsPerFrame)))
        // A new format: fresh filter state, and coefficients for this rate.
        z.update(repeating: 0, count: bandCount * Self.maxChannels * 2)
        state.prepare(sampleRate: sampleRate)
        version = -1
    }

    /// Picks up new settings (copying, never allocating; the filter state is kept).
    private func refresh() {
        guard let s = state.load(ifNewerThan: version, sampleRate: sampleRate, into: coeffs) else { return }
        version = s.version
        if s.enabled, !enabled {
            // Switched on: start from silence in the filters and the current level.
            z.update(repeating: 0, count: bandCount * Self.maxChannels * 2)
            appliedPreamp = s.preamp
        }
        enabled = s.enabled
        preamp = s.preamp
    }

    /// Multiplies every `stride`th sample by `gain`, clamped to full scale (the levelling
    /// boost while the equaliser is off).
    static func scale(_ data: UnsafeMutablePointer<Float>, count: Int, stride: Int, gain: Float) {
        var i = 0
        while i < count {
            data[i] = min(1, max(-1, data[i] * gain))
            i += stride
        }
    }

    private func process(_ buffers: UnsafeMutableAudioBufferListPointer, frames: Int) {
        refresh()
        guard usable, frames > 0 else { return }
        guard enabled else {
            // Only the levelling boost.
            guard gain != 1 else { return }
            for buffer in buffers {
                guard let data = buffer.mData?.assumingMemoryBound(to: Float.self) else { continue }
                let count = interleaved ? frames * Int(buffer.mNumberChannels) : frames
                Self.scale(data, count: count, stride: 1, gain: Float(gain))
            }
            return
        }
        let from = appliedPreamp * gain, to = preamp * gain
        appliedPreamp = preamp
        let step = (to - from) / Double(frames)
        let bands = bandCount
        let stride = EQState.stride
        for (bi, buffer) in buffers.enumerated() {
            guard let data = buffer.mData?.assumingMemoryBound(to: Float.self) else { continue }
            let chans = interleaved ? Int(buffer.mNumberChannels) : 1
            for ch in 0..<chans {
                let channel = interleaved ? ch : bi
                guard channel < channels else { continue }
                var i = ch
                var frame = 0
                let end = frames * chans
                while i < end {
                    var x = Double(data[i]) * (from + step * Double(frame))
                    for b in 0..<bands {
                        let c = coeffs + b * stride
                        let zp = z + (b * Self.maxChannels + channel) * 2
                        let y = c[0] * x + zp[0]
                        zp[0] = c[1] * x - c[3] * y + zp[1]
                        zp[1] = c[2] * x - c[4] * y
                        x = y
                    }
                    data[i] = Float(min(1, max(-1, x)))
                    i += chans
                    frame += 1
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
