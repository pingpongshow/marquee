import type { ItemSummary } from "@/api/types";

/** One queue slot. The key keeps duplicates of the same track distinct. */
/** dj marks a track the Guest DJ wove in (MUSIC-6). */
export type Entry = { key: number; item: ItemSummary; dj?: string };
export type Repeat = "off" | "all" | "one";

/**
 * The play queue. `entries` is the play order; while shuffled, `original` keeps the
 * unshuffled order so turning shuffle off restores it (Plexamp behaviour).
 */
export type Queue = { entries: Entry[]; index: number; shuffled: boolean; original: Entry[] | null; repeat: Repeat };

export const emptyQueue: Queue = { entries: [], index: -1, shuffled: false, original: null, repeat: "off" };

let nextKey = 1;
const wrap = (items: ItemSummary[], dj?: string): Entry[] => items.map((item) => ({ key: nextKey++, item, ...(dj ? { dj } : {}) }));

function shuffled<T>(list: T[], random = Math.random): T[] {
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j]!, out[i]!];
  }
  return out;
}

export const current = (q: Queue): Entry | undefined => q.entries[q.index];

/** Replaces the queue and starts at `start`; with shuffle the started track plays first. */
export function load(q: Queue, items: ItemSummary[], start = 0, shuffle = false, random = Math.random): Queue {
  const entries = wrap(items);
  if (!entries.length) return { ...emptyQueue, repeat: q.repeat };
  if (!shuffle) return { entries, index: Math.min(Math.max(start, 0), entries.length - 1), shuffled: false, original: null, repeat: q.repeat };
  const first = entries[Math.min(Math.max(start, 0), entries.length - 1)]!;
  const rest = shuffled(
    entries.filter((e) => e !== first),
    random,
  );
  return { entries: [first, ...rest], index: 0, shuffled: true, original: entries, repeat: q.repeat };
}

export function toggleShuffle(q: Queue, random = Math.random): Queue {
  const cur = current(q);
  if (!cur) return { ...q, shuffled: !q.shuffled };
  if (q.shuffled && q.original) {
    return { ...q, entries: q.original, index: Math.max(0, q.original.indexOf(cur)), shuffled: false, original: null };
  }
  const upcoming = shuffled(q.entries.slice(q.index + 1), random);
  return { ...q, entries: [...q.entries.slice(0, q.index + 1), ...upcoming], shuffled: true, original: q.entries };
}

export const cycleRepeat = (q: Queue): Queue => ({ ...q, repeat: q.repeat === "off" ? "all" : q.repeat === "all" ? "one" : "off" });

/** Inserts items right after the current track. */
export function playNext(q: Queue, items: ItemSummary[], dj?: string): Queue {
  if (q.index < 0) return load(q, items);
  const add = wrap(items, dj);
  const at = q.index + 1;
  const entries = [...q.entries.slice(0, at), ...add, ...q.entries.slice(at)];
  let original = q.original;
  if (original) {
    const cur = current(q)!;
    const o = original.indexOf(cur) + 1;
    original = [...original.slice(0, o), ...add, ...original.slice(o)];
  }
  return { ...q, entries, original };
}

export function append(q: Queue, items: ItemSummary[]): Queue {
  if (q.index < 0) return load(q, items);
  const add = wrap(items);
  return { ...q, entries: [...q.entries, ...add], original: q.original ? [...q.original, ...add] : null };
}

export function remove(q: Queue, key: number): Queue {
  const i = q.entries.findIndex((e) => e.key === key);
  if (i < 0) return q;
  const entries = q.entries.filter((e) => e.key !== key);
  if (!entries.length) return { ...emptyQueue, repeat: q.repeat };
  // Removing the current track moves on to the one that took its place.
  const index = i < q.index ? q.index - 1 : Math.min(q.index, entries.length - 1);
  return { ...q, entries, index, original: q.original?.filter((e) => e.key !== key) ?? null };
}

/** Moves an entry to position `to` in the play order. */
export function move(q: Queue, key: number, to: number): Queue {
  const from = q.entries.findIndex((e) => e.key === key);
  if (from < 0) return q;
  const cur = current(q);
  const entries = [...q.entries];
  const [e] = entries.splice(from, 1) as [Entry];
  entries.splice(Math.min(Math.max(to, 0), entries.length), 0, e);
  return { ...q, entries, index: cur ? entries.indexOf(cur) : q.index };
}

export function jump(q: Queue, key: number): Queue {
  const i = q.entries.findIndex((e) => e.key === key);
  return i < 0 ? q : { ...q, index: i };
}

/** The index that follows the current track when it ends naturally, or -1 to stop. */
export function followingIndex(q: Queue): number {
  if (q.index < 0) return -1;
  if (q.repeat === "one") return q.index;
  if (q.index + 1 < q.entries.length) return q.index + 1;
  return q.repeat === "all" ? 0 : -1;
}

/** Skip forward (the user pressed Next): repeat-one doesn't hold the track. */
export function skipIndex(q: Queue): number {
  if (q.index + 1 < q.entries.length) return q.index + 1;
  return q.repeat !== "off" && q.entries.length ? 0 : -1;
}
