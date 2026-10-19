import Foundation
import MarqueeAPI
import Network
import Observation

// Smart downloads (iPhone/iPad): the next episodes of the shows you're watching, and your
// favourite and most-played music, kept on the device within a storage budget each, using the
// ordinary download machinery (Downloads). What it downloads is tagged, so it only ever removes
// its own downloads, never ones made by hand.

/// How much space one kind of smart download may use.
public enum SmartBudget: Codable, Hashable, Sendable {
    /// Gigabytes (10^9 bytes).
    case gigabytes(Int)
    /// A share of the space that's free (counting what smart downloads already hold).
    case percentOfFree(Int)

    public static let choices: [SmartBudget] = [.gigabytes(1), .gigabytes(2), .gigabytes(5), .gigabytes(10), .gigabytes(20), .gigabytes(50),
                                                .percentOfFree(10), .percentOfFree(25), .percentOfFree(50)]

    public var label: String {
        switch self {
        case let .gigabytes(g): "\(g) GB"
        case let .percentOfFree(p): "Up to \(p)% of free space"
        }
    }

    /// The budget in bytes. `free` is the device's free space, `held` what this kind already
    /// holds (it would be free again without it).
    public func bytes(free: Int64, held: Int64) -> Int64 {
        switch self {
        case let .gigabytes(g): Int64(max(0, g)) * 1_000_000_000
        case let .percentOfFree(p): max(0, free + held) / 100 * Int64(min(100, max(0, p)))
        }
    }
}

/// The person's choices, kept on the device.
public struct SmartDownloadSettings: Codable, Equatable, Sendable {
    public var showsEnabled = false
    /// The next unwatched episodes kept per show.
    public var episodesPerShow = 3
    public var showsBudget: SmartBudget = .gigabytes(5)
    public var musicEnabled = false
    public var musicBudget: SmartBudget = .gigabytes(2)
    /// Only download on Wi-Fi (or Ethernet), not over mobile data.
    public var wifiOnly = true
    /// Episodes' quality (tracks are always the original).
    public var quality: Downloads.Quality = .medium

    public static let episodeChoices = [1, 2, 3, 5]

    public init() {}

    private static let key = "marquee.smartDownloads"

    public static func load() -> SmartDownloadSettings {
        UserDefaults.standard.data(forKey: key).flatMap { try? JSONDecoder().decode(Self.self, from: $0) } ?? .init()
    }

    func save() {
        if let data = try? JSONEncoder().encode(self) { UserDefaults.standard.set(data, forKey: Self.key) }
    }
}

/// The choosing, as pure functions (tested on their own).
public enum SmartSelection {
    /// Something that could be kept: its item and the space it takes (what it already uses on
    /// the device, an estimate before it's downloaded, 0 when it's there anyway, downloaded by hand).
    public struct Candidate: Equatable, Sendable {
        public let id: Int64
        public let bytes: Int64
        public init(id: Int64, bytes: Int64) {
            self.id = id
            self.bytes = bytes
        }
    }

    /// A show being watched: when it was last watched and its unwatched episodes in play order.
    public struct Show: Equatable, Sendable {
        public let id: Int64
        public let lastWatched: Date
        public let unwatched: [Candidate]
        public init(id: Int64, lastWatched: Date, unwatched: [Candidate]) {
            self.id = id
            self.lastWatched = lastWatched
            self.unwatched = unwatched
        }
    }

    /// Takes candidates in order while they fit the budget, stopping at the first that
    /// doesn't (what comes after it matters less). Repeats count once.
    public static func fill(_ candidates: [Candidate], budget: Int64) -> [Int64] {
        var used: Int64 = 0
        var seen = Set<Int64>(), out: [Int64] = []
        for c in candidates where !seen.contains(c.id) {
            seen.insert(c.id)
            guard used + max(0, c.bytes) <= budget else { break }
            used += max(0, c.bytes)
            out.append(c.id)
        }
        return out
    }

    /// The episodes to keep: the next `perShow` unwatched of each show, the most recently
    /// watched shows first, so at the budget the shows watched longest ago go without.
    public static func shows(_ shows: [Show], perShow: Int, budget: Int64) -> [Int64] {
        let ordered = shows.enumerated().sorted { a, b in
            a.element.lastWatched != b.element.lastWatched ? a.element.lastWatched > b.element.lastWatched : a.offset < b.offset
        }
        return fill(ordered.flatMap { $0.element.unwatched.prefix(max(0, perShow)) }, budget: budget)
    }

    /// The tracks to keep: favourites first, then the most played, then the recently played.
    public static func music(favorites: [Candidate], mostPlayed: [Candidate], recent: [Candidate], budget: Int64) -> [Int64] {
        fill(favorites + mostPlayed + recent, budget: budget)
    }

    /// Smart downloads to remove: those it holds that are no longer wanted.
    public static func removals(held: [Int64], wanted: [Int64]) -> [Int64] {
        let keep = Set(wanted)
        return held.filter { !keep.contains($0) }
    }

    /// Wanted items not yet on the device, in order.
    public static func additions(wanted: [Int64], present: Set<Int64>) -> [Int64] {
        wanted.filter { !present.contains($0) }
    }

    /// About how big a download will be: its length at the quality's bitrate (the server's
    /// conversions: 1080p ≈ 4.7 Mbps, 720p ≈ 2.4, 480p ≈ 1.1, audio included). Originals are
    /// guessed at 10 Mbps for video and the file's own bitrate (else 1 Mbps) for music.
    public static func estimatedBytes(durationMs: Int64?, quality: Downloads.Quality, isTrack: Bool = false, audioKbps: Int? = nil) -> Int64 {
        let ms = max(0, durationMs ?? (isTrack ? 240_000 : 45 * 60_000))
        let kbps: Int64
        if isTrack {
            kbps = Int64(audioKbps.flatMap { $0 > 0 ? $0 : nil } ?? 1000)
        } else {
            switch quality {
            case .original: kbps = 10_000
            case .high: kbps = 4_700
            case .medium: kbps = 2_400
            case .low: kbps = 1_100
            }
        }
        return ms * kbps / 8 // ms × kbit/s ÷ 8 = bytes
    }
}

/// Runs smart downloads: works out what should be on the device, removes what it downloaded
/// that's no longer wanted (watched, over the budget, turned off) and starts what's missing.
/// Runs when the app opens or comes back, and after a video ends (the app has no background
/// refresh); transfers carry on in the background as ordinary downloads do.
@MainActor @Observable
public final class SmartDownloads {
    public enum Kind: String, CaseIterable, Sendable { case shows, music }

    public var settings: SmartDownloadSettings {
        didSet {
            guard settings != oldValue else { return }
            settings.save()
        }
    }
    public private(set) var running = false
    public private(set) var lastRun: Date?
    /// Why the last run didn't download anything new (offline, on mobile data…), for the settings page.
    public private(set) var note: String?

    @ObservationIgnored private let downloads: Downloads
    @ObservationIgnored private weak var app: AppSession?
    @ObservationIgnored private let monitor = NWPathMonitor()
    @ObservationIgnored private var onWiFi = true
    @ObservationIgnored private var again = false

    public init(downloads: Downloads) {
        self.downloads = downloads
        settings = .load()
        monitor.pathUpdateHandler = { [weak self] path in
            let wifi = path.status == .satisfied && !path.isExpensive
                && (path.usesInterfaceType(.wifi) || path.usesInterfaceType(.wiredEthernet))
            Task { @MainActor in self?.onWiFi = wifi }
        }
        monitor.start(queue: .global(qos: .utility))
    }

    // MARK: What it holds

    /// "server|user" for whoever is signed in.
    private func owner(_ app: AppSession) -> String? {
        guard let server = app.server?.id, let user = app.me?.id else { return nil }
        return "\(server)|\(user)"
    }

    /// Smart downloads of a kind (everyone's on this device), newest first.
    public func entries(_ kind: Kind) -> [Downloads.Entry] {
        downloads.entries.values.filter { $0.smart == kind.rawValue }.sorted { $0.added > $1.added }
    }

    /// Space a kind uses (finished downloads).
    public func bytes(_ kind: Kind) -> Int64 {
        entries(kind).filter { $0.state == .done }.reduce(0) { $0 + $1.size }
    }

    public var hasAny: Bool { downloads.entries.values.contains { $0.smart != nil } }

    /// Removes everything smart downloads downloaded (a kind, or both). It downloads them again
    /// on its next run while it's switched on.
    public func removeAll(_ kind: Kind? = nil) {
        for e in downloads.entries.values where e.smart != nil && (kind == nil || e.smart == kind?.rawValue) {
            if e.viaPlaylist == true {
                downloads.markSmart(e.id, kind: nil, owner: nil) // a synced playlist keeps it
            } else {
                downloads.remove(e.id)
            }
        }
    }

    // MARK: Running

    /// Brings the device in line with the settings. Safe to call often: one run at a time (a
    /// call during a run runs once more after it).
    public func run(_ app: AppSession) async {
        self.app = app
        guard !running else { again = true; return }
        running = true
        defer { running = false }
        repeat {
            again = false
            await runOnce(app)
        } while again
        lastRun = Date()
    }

    private func runOnce(_ app: AppSession) async {
        // Switched off: what it downloaded goes.
        for kind in Kind.allCases where !enabled(kind) { removeOwn(kind, keeping: [], app: app) }
        guard settings.showsEnabled || settings.musicEnabled else { note = nil; return }
        guard app.client != nil, let me = app.me else { note = "Waiting for the server."; return }
        // Plays made offline reach the server first, so what was watched is known.
        await OfflineSync.shared.flush()
        let mayDownload = !settings.wifiOnly || onWiFi
        note = mayDownload ? nil : "Waiting for Wi-Fi to download."
        if settings.showsEnabled { await runShows(app, mayDownload: mayDownload) }
        if settings.musicEnabled { await runMusic(app, me: me, mayDownload: mayDownload) }
    }

    private func enabled(_ kind: Kind) -> Bool {
        kind == .shows ? settings.showsEnabled : settings.musicEnabled
    }

    private var freeBytes: Int64 {
        #if os(tvOS)
        let values = try? Downloads.directory.resourceValues(forKeys: [.volumeAvailableCapacityKey])
        return Int64(values?.volumeAvailableCapacity ?? 0)
        #else
        let values = try? Downloads.directory.resourceValues(forKeys: [.volumeAvailableCapacityForImportantUsageKey])
        return values?.volumeAvailableCapacityForImportantUsage ?? 0
        #endif
    }

    /// The space an item takes against a kind's budget.
    private func cost(_ item: Item, kind: Kind, estimate: () -> Int64) -> Int64 {
        guard let e = downloads.entries[item.id] else { return estimate() }
        if e.smart == nil { return 0 } // downloaded by hand: there anyway
        return e.state == .done && e.size > 0 ? e.size : max(e.size, estimate())
    }

    /// Removes this person's smart downloads of a kind that aren't in `keeping`.
    private func removeOwn(_ kind: Kind, keeping: [Int64], app: AppSession) {
        let me = owner(app)
        let held = downloads.entries.values.filter { $0.smart == kind.rawValue && ($0.smartOwner == nil || $0.smartOwner == me || !enabled(kind)) }
        for id in SmartSelection.removals(held: held.map(\.id), wanted: keeping) {
            if downloads.entries[id]?.viaPlaylist == true {
                downloads.markSmart(id, kind: nil, owner: nil)
            } else {
                downloads.remove(id)
            }
        }
    }

    /// Starts downloads (at most `limit` a run; the next run carries on).
    private func start(_ items: [Item], kind: Kind, app: AppSession, quality: Downloads.Quality, limit: Int) async {
        let me = owner(app)
        for item in items.prefix(limit) where downloads.entries[item.id] == nil {
            do {
                try await downloads.download(item, quality: quality)
                downloads.markSmart(item.id, kind: kind.rawValue, owner: me)
            } catch {
                note = "Couldn't download \(item.title): \(error.localizedDescription)"
            }
        }
    }

    // MARK: Shows

    private func runShows(_ app: AppSession, mayDownload: Bool) async {
        guard let page = try? await app.continueWatching(limit: 50) else { return }
        // Shows on Continue Watching (On Deck), each once, most recently watched first.
        var seen = Set<Int64>()
        var shows: [(id: Int64, last: Date)] = []
        for entry in page.entries.sorted(by: { $0.lastWatchedAt > $1.lastWatchedAt }) {
            guard let it = entry.item, it._type == .episode, let show = it.grandparentId, !seen.contains(show) else { continue }
            seen.insert(show)
            shows.append((show, entry.lastWatchedAt))
        }
        let perShow = settings.episodesPerShow, quality = settings.quality
        var candidates: [SmartSelection.Show] = []
        var items: [Int64: Item] = [:]
        var estimate: Int64 = 0
        let budget = settings.showsBudget.bytes(free: freeBytes, held: bytes(.shows))
        for show in shows.prefix(25) {
            // Enough to fill the budget: the shows watched longer ago wouldn't fit.
            if estimate > budget { break }
            // A show that can't be listed now keeps what it has (nothing is removed this run).
            guard let eps = try? await app.leaves(show.id, unwatched: true) else { return }
            let next = eps.filter { $0.isPlayableVideo && !$0.isFullyWatched }.prefix(perShow)
            for e in next { items[e.id] = e }
            let list = next.map { e in
                SmartSelection.Candidate(id: e.id, bytes: cost(e, kind: .shows) {
                    SmartSelection.estimatedBytes(durationMs: e.durationMs, quality: quality)
                })
            }
            estimate += list.reduce(0) { $0 + $1.bytes }
            candidates.append(.init(id: show.id, lastWatched: show.last, unwatched: Array(list)))
        }
        let wanted = SmartSelection.shows(candidates, perShow: perShow, budget: budget)
        removeOwn(.shows, keeping: wanted, app: app)
        guard mayDownload else { return }
        let add = SmartSelection.additions(wanted: wanted, present: Set(downloads.entries.keys))
        await start(add.compactMap { items[$0] }, kind: .shows, app: app, quality: quality, limit: 40)
    }

    // MARK: Music

    private func runMusic(_ app: AppSession, me: User, mayDownload: Bool) async {
        guard let libraries = try? await app.libraries() else { return }
        let music = libraries.filter { $0._type == .music }
        guard !music.isEmpty else { removeOwn(.music, keeping: [], app: app); return }
        let budget = settings.musicBudget.bytes(free: freeBytes, held: bytes(.music))
        var items: [Int64: Item] = [:]
        var estimate: Int64 = 0
        func candidates(_ tracks: [Item]) -> [SmartSelection.Candidate] {
            tracks.filter { $0._type == .track && items[$0.id] == nil }.map { t in
                items[t.id] = t
                let c = SmartSelection.Candidate(id: t.id, bytes: cost(t, kind: .music) {
                    SmartSelection.estimatedBytes(durationMs: t.durationMs, quality: .original, isTrack: true, audioKbps: t.audioFormat?.bitrateKbps)
                })
                estimate += c.bytes
                return c
            }
        }
        // Favourites: hearted songs, then the songs of hearted albums, then hearted artists' most played.
        var favorites: [SmartSelection.Candidate] = []
        do {
            for lib in music {
                let page = try await app.items(library: lib.id, sort: ._hyphen_favorited, limit: 500, type: .track, favorites: true)
                favorites += candidates(page.items)
            }
            for lib in music where estimate <= budget {
                let albums = try await app.items(library: lib.id, sort: ._hyphen_favorited, limit: 60, type: .album, favorites: true).items
                for album in albums where estimate <= budget {
                    favorites += candidates(try await app.leaves(album.id))
                }
            }
            for lib in music where estimate <= budget {
                let artists = try await app.items(library: lib.id, sort: ._hyphen_favorited, limit: 40, type: .artist, favorites: true).items
                for artist in artists where estimate <= budget {
                    favorites += candidates(try await app.popularTracks(artist.id))
                }
            }
        } catch {
            return // the server couldn't list them: nothing is removed this run
        }
        // Most played (the last 90 days), then recently played.
        var mostPlayed: [SmartSelection.Candidate] = []
        if estimate <= budget, let stats = try? await app.stats(days: 90, limit: 50, userID: me.id) {
            for t in stats.tracks where estimate <= budget {
                guard let id = t.id else { continue }
                if let known = downloads.entries[id]?.item {
                    mostPlayed += candidates([known])
                } else if let d = try? await app.item(id) {
                    mostPlayed += candidates([d.base])
                }
            }
        }
        var recent: [SmartSelection.Candidate] = []
        if estimate <= budget {
            for lib in music {
                guard let page = try? await app.items(library: lib.id, sort: ._hyphen_viewed, limit: 100, type: .track) else { continue }
                recent += candidates(page.items.filter { $0.lastViewedAt != nil || ($0.viewCount ?? 0) > 0 })
            }
        }
        let wanted = SmartSelection.music(favorites: favorites, mostPlayed: mostPlayed, recent: recent, budget: budget)
        removeOwn(.music, keeping: wanted, app: app)
        guard mayDownload else { return }
        let add = SmartSelection.additions(wanted: wanted, present: Set(downloads.entries.keys))
        await start(add.compactMap { items[$0] }, kind: .music, app: app, quality: .original, limit: 200)
    }
}
