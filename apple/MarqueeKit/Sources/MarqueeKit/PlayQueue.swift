import Foundation

/// The music play queue (same behaviour as the web player's queue.ts). `entries` is the
/// play order; while shuffled, `original` keeps the unshuffled order so turning shuffle
/// off restores it.
public struct PlayQueue: Sendable, Equatable {
    public struct Entry: Identifiable, Sendable, Equatable {
        public let id: Int   // unique per slot, so duplicates of a track stay distinct
        public let item: Item
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

    private mutating func wrap(_ items: [Item]) -> [Entry] {
        items.map { item in defer { nextKey += 1 }; return Entry(id: nextKey, item: item) }
    }

    public mutating func load(_ items: [Item], start: Int = 0, shuffle: Bool = false) {
        let e = wrap(items)
        original = nil
        shuffled = false
        guard !e.isEmpty else { entries = []; index = -1; return }
        let s = min(max(start, 0), e.count - 1)
        if shuffle {
            var rest = e
            let first = rest.remove(at: s)
            entries = [first] + rest.shuffled()
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

    public mutating func playNext(_ items: [Item]) {
        guard index >= 0 else { load(items); return }
        let add = wrap(items)
        entries.insert(contentsOf: add, at: index + 1)
        if var o = original, let cur = current, let at = o.firstIndex(of: cur) {
            o.insert(contentsOf: add, at: at + 1)
            original = o
        }
    }

    public mutating func append(_ items: [Item]) {
        guard index >= 0 else { load(items); return }
        let add = wrap(items)
        entries += add
        original? += add
    }

    public mutating func remove(_ id: Int) {
        guard let i = entries.firstIndex(where: { $0.id == id }) else { return }
        entries.remove(at: i)
        original?.removeAll { $0.id == id }
        if entries.isEmpty { index = -1; return }
        if i < index { index -= 1 } else { index = min(index, entries.count - 1) }
    }

    public mutating func move(_ id: Int, to: Int) {
        guard let from = entries.firstIndex(where: { $0.id == id }) else { return }
        let cur = current
        let e = entries.remove(at: from)
        entries.insert(e, at: min(max(to, 0), entries.count))
        if let cur { index = entries.firstIndex(of: cur) ?? index }
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
