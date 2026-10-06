import Foundation

/// Volume levelling maths (MUSIC-9): ReplayGain values (relative to -18 LUFS) to a linear gain.
public enum Loudness {
    /// The most a quiet track is raised.
    public static let maxBoostDb = 6.0
    /// The most a loud track is lowered (a bad tag never silences a track).
    public static let maxCutDb = -24.0

    /// Automatic levelling uses album gain while an album plays in order: the track before or
    /// after this one is from the same album.
    public static func useAlbumGain(albums: [Int64?], index: Int) -> Bool {
        guard albums.indices.contains(index), let album = albums[index] else { return false }
        return [index - 1, index + 1].contains { albums.indices.contains($0) && albums[$0] == album }
    }

    /// The linear gain for a track. Cuts apply as they are (down to `maxCutDb`). Boosts need
    /// `allowBoost` and a known peak, and stop at `maxBoostDb` and where the peak would reach
    /// full scale (peak × gain ≤ 1). No usable gain: 1.
    public static func gain(trackDb: Double?, albumDb: Double?, peak: Double?, useAlbum: Bool, allowBoost: Bool) -> Double {
        let picked = useAlbum ? (albumDb ?? trackDb) : trackDb
        guard let picked, picked.isFinite else { return 1 }
        let db = min(maxBoostDb, max(maxCutDb, picked))
        let g = pow(10, db / 20)
        guard g > 1 else { return g }
        // A peak outside (0, 4] isn't a sample level (a tag in other units): don't boost on it.
        guard allowBoost, let peak, peak.isFinite, peak > 0, peak <= 4 else { return 1 }
        return max(1, min(g, 1 / peak))
    }
}
