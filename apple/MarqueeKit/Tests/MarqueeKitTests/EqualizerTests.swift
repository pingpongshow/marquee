import Foundation
@testable import MarqueeKit
import Testing

/// The equaliser's filters do what their gains say.
struct EqualizerTests {
    /// Gain in dB of a biquad at frequency f.
    private func response(_ c: (Double, Double, Double, Double, Double), f: Double, rate: Double = 44100) -> Double {
        let w = 2 * Double.pi * f / rate
        func mag(_ x0: Double, _ x1: Double, _ x2: Double) -> Double {
            let re = x0 + x1 * cos(w) + x2 * cos(2 * w)
            let im = -(x1 * sin(w) + x2 * sin(2 * w))
            return (re * re + im * im).squareRoot()
        }
        return 20 * log10(mag(c.0, c.1, c.2) / mag(1, c.3, c.4))
    }

    @Test func peakingBandHitsItsGainAtTheCentre() {
        let c = Biquad.peaking.coefficients(f: 1000, gainDB: 6, sampleRate: 44100)
        #expect(abs(response(c, f: 1000) - 6) < 0.05)
        #expect(abs(response(c, f: 100)) < 0.5) // far away: untouched
    }

    @Test func shelvesLiftTheirEnds() {
        let low = Biquad.lowShelf.coefficients(f: 31, gainDB: -6, sampleRate: 48000)
        #expect(abs(response(low, f: 5, rate: 48000) + 6) < 0.3)
        #expect(abs(response(low, f: 2000, rate: 48000)) < 0.1)
        let high = Biquad.highShelf.coefficients(f: 16000, gainDB: 4, sampleRate: 48000)
        #expect(abs(response(high, f: 23000, rate: 48000) - 4) < 0.5)
        #expect(abs(response(high, f: 500, rate: 48000)) < 0.1)
    }

    @Test @MainActor func presetsCoverTenBandsWithinRange() {
        for p in Equalizer.presets {
            #expect(p.gains.count == Equalizer.bands.count)
            #expect(p.gains.allSatisfy { abs($0) <= Equalizer.maxGain })
        }
    }
}

/// Coefficients are worked out ahead of the audio thread, one set per sample rate.
struct EqualizerStateTests {
    @Test func flatBandsPassThrough() {
        let c = EQState.coefficients(gains: Array(repeating: 0, count: Equalizer.bands.count), sampleRate: 48000)
        #expect(c.count == EQState.coefficientCount)
        for b in 0..<Equalizer.bands.count {
            #expect(Array(c[(b * 5)..<(b * 5 + 5)]) == [1, 0, 0, 0, 0])
        }
    }

    @Test func bandsAboveTheRateAreSkipped() {
        var gains = Array(repeating: 0.0, count: Equalizer.bands.count)
        gains[9] = 6 // 16 kHz: too high for 22.05 kHz audio
        let c = EQState.coefficients(gains: gains, sampleRate: 22050)
        #expect(Array(c[45..<50]) == [1, 0, 0, 0, 0])
    }

    @Test func tapsGetCoefficientsForTheirRateWithoutAllocating() {
        let state = EQState()
        var gains = Array(repeating: 0.0, count: Equalizer.bands.count)
        gains[5] = 6
        state.update(enabled: true, gains: gains)
        state.prepare(sampleRate: 44100)
        let buffer = UnsafeMutablePointer<Double>.allocate(capacity: EQState.coefficientCount)
        defer { buffer.deallocate() }
        let first = state.load(ifNewerThan: -1, sampleRate: 44100, into: buffer)
        #expect(first?.enabled == true)
        #expect(abs((first?.preamp ?? 1) - pow(10, -6 * 0.6 / 20)) < 1e-9)
        let peak = Biquad.peaking.coefficients(f: 1000, gainDB: 6, sampleRate: 44100)
        #expect(buffer[25] == peak.0)
        // Nothing new: nothing copied.
        #expect(state.load(ifNewerThan: first!.version, sampleRate: 44100, into: buffer) == nil)
        // A change reaches the prepared rate.
        gains[5] = -6
        state.update(enabled: true, gains: gains)
        let second = state.load(ifNewerThan: first!.version, sampleRate: 44100, into: buffer)
        #expect(second != nil)
        #expect(buffer[25] == Biquad.peaking.coefficients(f: 1000, gainDB: -6, sampleRate: 44100).0)
    }
}
