import Foundation
@testable import MarqueeKit
import Testing

/// Volume levelling maths (MUSIC-9).
struct LoudnessTests {
    private func db(_ g: Double) -> Double { 20 * log10(g) }

    @Test func loudTracksAreTurnedDownByTheirGain() {
        let g = Loudness.gain(trackDb: -8, albumDb: nil, peak: 1, useAlbum: false, allowBoost: true)
        #expect(abs(db(g) + 8) < 0.001)
    }

    @Test func autoUsesAlbumGainOnlyInAnAlbumRun() {
        let albums: [Int64?] = [1, 1, 2, nil, 3]
        #expect(Loudness.useAlbumGain(albums: albums, index: 0))
        #expect(Loudness.useAlbumGain(albums: albums, index: 1))
        #expect(!Loudness.useAlbumGain(albums: albums, index: 2))
        #expect(!Loudness.useAlbumGain(albums: albums, index: 3)) // no album
        #expect(!Loudness.useAlbumGain(albums: albums, index: 4))
        #expect(!Loudness.useAlbumGain(albums: albums, index: 9))
        let album = Loudness.gain(trackDb: -6, albumDb: -9, peak: 0.9, useAlbum: true, allowBoost: true)
        #expect(abs(db(album) + 9) < 0.001)
        let track = Loudness.gain(trackDb: -6, albumDb: -9, peak: 0.9, useAlbum: false, allowBoost: true)
        #expect(abs(db(track) + 6) < 0.001)
        // No album gain: the track's.
        let fallback = Loudness.gain(trackDb: -6, albumDb: nil, peak: 0.9, useAlbum: true, allowBoost: true)
        #expect(abs(db(fallback) + 6) < 0.001)
    }

    @Test func quietTracksAreRaisedUpToSixDecibelsAndNeverPastFullScale() {
        // +4 dB with plenty of headroom.
        let g = Loudness.gain(trackDb: 4, albumDb: nil, peak: 0.3, useAlbum: false, allowBoost: true)
        #expect(abs(db(g) - 4) < 0.001)
        // +10 dB stops at +6.
        let capped = Loudness.gain(trackDb: 10, albumDb: nil, peak: 0.2, useAlbum: false, allowBoost: true)
        #expect(abs(db(capped) - 6) < 0.001)
        // The peak limits it: 0.8 × gain ≤ 1.
        let peaked = Loudness.gain(trackDb: 5, albumDb: nil, peak: 0.8, useAlbum: false, allowBoost: true)
        #expect(abs(peaked - 1.25) < 0.0001)
        #expect(peaked * 0.8 <= 1.0000001)
        // A track already at full scale isn't raised (nor lowered).
        #expect(Loudness.gain(trackDb: 3, albumDb: nil, peak: 1.0, useAlbum: false, allowBoost: true) == 1)
        #expect(Loudness.gain(trackDb: 3, albumDb: nil, peak: 1.3, useAlbum: false, allowBoost: true) == 1)
    }

    @Test func noBoostWithoutAPeakOrPermission() {
        #expect(Loudness.gain(trackDb: 4, albumDb: nil, peak: nil, useAlbum: false, allowBoost: true) == 1)
        #expect(Loudness.gain(trackDb: 4, albumDb: nil, peak: 0.3, useAlbum: false, allowBoost: false) == 1)
        // A peak in other units (16-bit sample values) isn't trusted.
        #expect(Loudness.gain(trackDb: 4, albumDb: nil, peak: 30000, useAlbum: false, allowBoost: true) == 1)
    }

    @Test func badTagsNeverSilenceATrack() {
        #expect(Loudness.gain(trackDb: nil, albumDb: nil, peak: nil, useAlbum: false, allowBoost: true) == 1)
        #expect(Loudness.gain(trackDb: .nan, albumDb: nil, peak: 1, useAlbum: false, allowBoost: true) == 1)
        let huge = Loudness.gain(trackDb: -200, albumDb: nil, peak: 1, useAlbum: false, allowBoost: true)
        #expect(abs(db(huge) - Loudness.maxCutDb) < 0.001)
        // A cut ignores a nonsense peak (it used to turn the track down to nothing).
        let cut = Loudness.gain(trackDb: -7, albumDb: nil, peak: 30000, useAlbum: false, allowBoost: true)
        #expect(abs(db(cut) + 7) < 0.001)
    }

    @Test func tapGainBoostsAndClampsSamples() {
        var samples: [Float] = [0.1, -0.2, 0.6, -0.9]
        samples.withUnsafeMutableBufferPointer { EqualizerTap.scale($0.baseAddress!, count: 4, stride: 1, gain: 2) }
        #expect(samples == [0.2, -0.4, 1, -1])
        // Interleaved: only every other sample (one channel).
        var stereo: [Float] = [0.1, 0.1, 0.2, 0.2]
        stereo.withUnsafeMutableBufferPointer { EqualizerTap.scale($0.baseAddress!, count: 4, stride: 2, gain: 2) }
        #expect(stereo == [0.2, 0.1, 0.4, 0.2])
    }
}
