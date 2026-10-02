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
