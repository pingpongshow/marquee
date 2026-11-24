import type { ItemSummary, PlaylistEntry } from "@/api/types";

/** The playlist editor's ways of sorting (MUSIC-29). */
export type SortKey = "title" | "artist" | "album" | "duration" | "added" | "libraryAdded";

export const sortLabels: Record<SortKey, string> = {
  title: "Title",
  artist: "Artist",
  album: "Album",
  duration: "Length",
  added: "Order added",
  libraryAdded: "Date added to the library",
};

/** The sorts a list offers: a collection (kind video, USER-23) has no artists, albums or lengths. */
export const sortKeysFor = (kind: "audio" | "video"): SortKey[] =>
  kind === "audio" ? ["title", "artist", "album", "duration", "added", "libraryAdded"] : ["title", "added", "libraryAdded"];

/** The artist a row shows: the track's credit, else its album artist (or an episode's show). */
export const artistOf = (it: ItemSummary) => it.artistCredit ?? it.grandparentTitle ?? "";

const text = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: "base", numeric: true });

/**
 * The entries sorted by key (a stable sort: equal ones keep their order). Artist then album keep
 * an album's tracks in their order. Entry ids grow as entries are added, so they give the order
 * things went into the playlist.
 */
export function sortEntries(list: readonly PlaylistEntry[], key: SortKey, descending = false): PlaylistEntry[] {
  const byKey = (a: PlaylistEntry, b: PlaylistEntry): number => {
    const x = a.item;
    const y = b.item;
    switch (key) {
      case "title":
        return text(x.title, y.title);
      case "artist":
        return text(artistOf(x), artistOf(y)) || text(x.parentTitle ?? "", y.parentTitle ?? "") || (x.disc ?? 0) - (y.disc ?? 0) || (x.index ?? 0) - (y.index ?? 0);
      case "album":
        return text(x.parentTitle ?? "", y.parentTitle ?? "") || (x.disc ?? 0) - (y.disc ?? 0) || (x.index ?? 0) - (y.index ?? 0);
      case "duration":
        return (x.durationMs ?? 0) - (y.durationMs ?? 0);
      case "added":
        return a.entryId - b.entryId;
      case "libraryAdded":
        return Date.parse(x.addedAt ?? "") - Date.parse(y.addedAt ?? "") || 0;
    }
  };
  return [...list].sort((a, b) => (descending ? byKey(b, a) : byKey(a, b)));
}

/** A shuffled copy. */
export function shuffleEntries(list: readonly PlaylistEntry[], rand: () => number = Math.random): PlaylistEntry[] {
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [out[i], out[j]] = [out[j]!, out[i]!];
  }
  return out;
}

/** Each song or video once, where it first comes. */
export function withoutDuplicates(list: readonly PlaylistEntry[]): PlaylistEntry[] {
  const seen = new Set<number>();
  return list.filter((e) => (seen.has(e.item.id) ? false : (seen.add(e.item.id), true)));
}

/** How many entries are a second (or later) copy of something. */
export const duplicateCount = (list: readonly PlaylistEntry[]) => list.length - new Set(list.map((e) => e.item.id)).size;

/** The entry ids from one to the other, either way round (a shift-click). */
export function rangeIds(list: readonly PlaylistEntry[], fromId: number, toId: number): number[] {
  const a = list.findIndex((e) => e.entryId === fromId);
  const b = list.findIndex((e) => e.entryId === toId);
  if (a < 0 || b < 0) return b >= 0 ? [toId] : [];
  const [lo, hi] = a < b ? [a, b] : [b, a];
  return list.slice(lo, hi + 1).map((e) => e.entryId);
}

/**
 * Moves the selected entries, in their order, to where `beforeId` is (null = the end). Dropping
 * them on one of themselves puts them where the first of the rest after it was.
 */
export function moveSelected(list: readonly PlaylistEntry[], selected: ReadonlySet<number>, beforeId: number | null): PlaylistEntry[] {
  const moving = list.filter((e) => selected.has(e.entryId));
  if (!moving.length) return [...list];
  let target = beforeId;
  if (target !== null && selected.has(target)) {
    const i = list.findIndex((e) => e.entryId === target);
    target = list.slice(i).find((e) => !selected.has(e.entryId))?.entryId ?? null;
  }
  const rest = list.filter((e) => !selected.has(e.entryId));
  const at = target === null ? rest.length : rest.findIndex((e) => e.entryId === target);
  rest.splice(at < 0 ? rest.length : at, 0, ...moving);
  return rest;
}

/** Moves the selected entries one place up (-1) or down (+1), as a block where they touch. */
export function nudgeSelected(list: readonly PlaylistEntry[], selected: ReadonlySet<number>, by: -1 | 1): PlaylistEntry[] {
  const out = [...list];
  const order = by < 0 ? out.map((_, i) => i) : out.map((_, i) => out.length - 1 - i);
  for (const i of order) {
    const j = i + by;
    if (!selected.has(out[i]!.entryId) || j < 0 || j >= out.length || selected.has(out[j]!.entryId)) continue;
    [out[i], out[j]] = [out[j]!, out[i]!];
  }
  return out;
}

/** Whether an entry matches what's typed in the filter (title, artist or album, any case). */
export function matchesFilter(e: PlaylistEntry, q: string): boolean {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = [e.item.title, artistOf(e.item), e.item.parentTitle ?? "", e.item.grandparentTitle ?? ""].join(" ").toLowerCase();
  return words.every((w) => hay.includes(w));
}

/** Whether two orders are the same (nothing to save). */
export const sameOrder = (a: readonly PlaylistEntry[], b: readonly PlaylistEntry[]) => a.length === b.length && a.every((e, i) => e.entryId === b[i]!.entryId);
