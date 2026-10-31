import Foundation

/// The music play queue (same behaviour as the web player's queue.ts). `entries` is the
/// play order; while shuffled, `original` keeps the unshuffled order so turning shuffle
/// off restores it.
public struct PlayQueue: Sendable, Equatable {
    public struct Entry: Identifiable, Sendable, Equatable {
        public let id: Int   // unique per slot, so duplicates of a track stay distinct
        public let item: Item
        /// Set for tracks the DJ wove in (MUSIC-6).
        public var dj: String? = nil
        /// The station or mix it came from (Station.context), for the playlist statistics.
        public var context: String? = nil
        /// Came from the station playing (not queued by hand): a radio may replace it.
        public var fromStation = false
        /// What the play is reported as started from (MUSIC-17): "dj" for the DJ's picks.
        public var playContext: String? { dj != nil ? "dj" : context }
        public static func == (a: Entry, b: Entry) -> Bool { a.id == b.id }
    }
    public enum Repeat: Sendable { case off, all, one }

    public private(set) var entries: [Entry] = []
    public private(set) var index = -1
    public private(set) var shuffled = false
    public var repeatMode: Repeat = .off
    private var original: [Entry]?
    private var nextKey = 1

    public init() {}

    public var current: Entry? { entries.indices.contains(index) ? entries[index] : nil }
    public var upcoming: ArraySlice<Entry> { index >= 0 && index + 1 <= entries.count ? entries[(index + 1)...] : [] }

    /// Marks tracks a station queued: their plays are reported with its context (nil when
    /// the server sent none), and a radio can replace them.
    public struct FromStation: Sendable, Equatable {
        public var context: String?
        public init(context: String?) { self.context = context }
    }

    private mutating func wrap(_ items: [Item], dj: String? = nil, from station: FromStation? = nil) -> [Entry] {
        items.map { item in
            defer { nextKey += 1 }
            return Entry(id: nextKey, item: item, dj: dj, context: station?.context, fromStation: station != nil)
        }
    }

    public mutating func load(_ items: [Item], start: Int = 0, shuffle: Bool = false, from station: FromStation? = nil) {
        let e = wrap(items, from: station)
        original = nil
        shuffled = false
        guard !e.isEmpty else { entries = []; index = -1; return }
        let s = min(max(start, 0), e.count - 1)
        if shuffle {
            // The whole list, the opening track included: a Shuffle button shouldn't always
            // start with the same song.
            entries = e.shuffled()
            index = 0
            original = e
            shuffled = true
        } else {
            entries = e
            index = s
        }
    }

    public mutating func toggleShuffle() {
        guard let cur = current else { shuffled.toggle(); return }
        if shuffled, let o = original {
            entries = o
            index = o.firstIndex(of: cur) ?? 0
            original = nil
            shuffled = false
        } else {
            original = entries
            entries = Array(entries[...index]) + entries[(index + 1)...].shuffled()
            shuffled = true
        }
    }

    public mutating func cycleRepeat() {
        repeatMode = repeatMode == .off ? .all : repeatMode == .all ? .one : .off
    }

    public mutating func playNext(_ items: [Item], dj: String? = nil) {
        guard index >= 0 else { load(items); return }
        let add = wrap(items, dj: dj)
        entries.insert(contentsOf: add, at: index + 1)
        if var o = original, let cur = current, let at = o.firstIndex(of: cur) {
            o.insert(contentsOf: add, at: at + 1)
            original = o
        }
    }

    public mutating func append(_ items: [Item], from station: FromStation? = nil) {
        guard index >= 0 else { load(items, from: station); return }
        let add = wrap(items, from: station)
        entries += add
        original? += add
    }

    /// Replaces the station's tracks still to come with `items` (a radio steered away from
    /// what was skipped); tracks queued by hand and the DJ's picks stay. The new tracks go
    /// where the first of the old ones was, so "play next" picks still come first.
    public mutating func replaceUpcomingStation(with items: [Item], from station: FromStation) {
        guard index >= 0, !items.isEmpty else { return }
        let gone = Set(upcoming.filter(\.fromStation).map(\.id))
        let at = entries.indices.first { $0 > index && gone.contains(entries[$0].id) } ?? entries.count
        let add = wrap(items, from: station)
        entries.removeAll { gone.contains($0.id) }
        // Only upcoming entries went, all at or after `at`: the current track keeps its index.
        entries.insert(contentsOf: add, at: min(at, entries.count))
        if var o = original, let cur = current {
            o.removeAll { gone.contains($0.id) }
            o.insert(contentsOf: add, at: (o.firstIndex(of: cur) ?? o.count - 1) + 1)
            original = o
        }
    }

    /// Removes an entry. Removing the current track moves on to the one that took its place;
    /// returns true when the current track was the last one, so nothing took its place (the
    /// queue has ended: with repeat-all it wraps to the start, otherwise the last remaining
    /// track becomes current but shouldn't play by itself).
    @discardableResult
    public mutating func remove(_ id: Int) -> Bool {
        guard let i = entries.firstIndex(where: { $0.id == id }) else { return false }
        let wasCurrent = i == index
        entries.remove(at: i)
        original?.removeAll { $0.id == id }
        if entries.isEmpty { index = -1; return wasCurrent }
        if i < index {
            index -= 1
        } else if wasCurrent, index >= entries.count {
            // The current track was the last one.
            index = repeatMode == .all ? 0 : entries.count - 1
            return repeatMode != .all
        }
        return false
    }

    public mutating func move(_ id: Int, to: Int) {
        guard let from = entries.firstIndex(where: { $0.id == id }) else { return }
        let cur = current
        let e = entries.remove(at: from)
        let at = min(max(to, 0), entries.count)
        entries.insert(e, at: at)
        if let cur { index = entries.firstIndex(of: cur) ?? index }
        // While shuffled, mirror the move into the unshuffled order: the entry goes after the
        // one it now follows (or first), so turning shuffle off keeps it there.
        if var o = original, let oi = o.firstIndex(of: e) {
            o.remove(at: oi)
            if at == 0 {
                o.insert(e, at: 0)
            } else if let p = o.firstIndex(of: entries[at - 1]) {
                o.insert(e, at: p + 1)
            } else {
                o.append(e)
            }
            original = o
        }
    }

    public mutating func jump(_ id: Int) {
        if let i = entries.firstIndex(where: { $0.id == id }) { index = i }
    }

    public mutating func setIndex(_ i: Int) {
        if entries.indices.contains(i) { index = i }
    }

    /// What follows when the current track ends by itself (-1 = stop).
    public var followingIndex: Int {
        guard index >= 0 else { return -1 }
        if repeatMode == .one { return index }
        if index + 1 < entries.count { return index + 1 }
        return repeatMode == .all ? 0 : -1
    }

    /// Where Next goes (repeat-one doesn't hold the track).
    public var skipIndex: Int {
        if index + 1 < entries.count { return index + 1 }
        return repeatMode != .off && !entries.isEmpty ? 0 : -1
    }
}
