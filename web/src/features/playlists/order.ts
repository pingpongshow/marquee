/**
 * A shuffled copy of `list` that's the same for the same seed, so the player can work out the
 * next item of a shuffled video playlist from just the seed in the URL.
 */
export function seededShuffle<T>(list: readonly T[], seed: number): T[] {
  // mulberry32
  let a = seed >>> 0;
  const rand = () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [out[i], out[j]] = [out[j]!, out[i]!];
  }
  return out;
}

/** A fresh shuffle seed. */
export const newShuffleSeed = () => Math.floor(Math.random() * 2 ** 31) + 1;

/**
 * What follows the playing item in a play order, with its place in it. `at` is where the playing
 * item is (carried in the URL): the same film can be in a playlist twice, and going by its id
 * alone finds the first copy every time, so the playlist went round in a loop. Without a place
 * that fits, the first copy it is. Null at the end, or when the item isn't in the order.
 */
export function nextInOrder<T extends { id: number }>(order: readonly T[], currentId: number, at?: number): { item: T; at: number } | null {
  const i = at !== undefined && order[at]?.id === currentId ? at : order.findIndex((it) => it.id === currentId);
  const item = i >= 0 ? order[i + 1] : undefined;
  return item ? { item, at: i + 1 } : null;
}

/** The item after `currentId` in a playlist's play order (shuffled when there's a seed); null at the end. */
export function nextInPlaylist<T extends { id: number }>(items: readonly T[], currentId: number, seed?: number, at?: number): T | null {
  return nextInOrder(seed ? seededShuffle(items, seed) : items, currentId, at)?.item ?? null;
}

// The play order the playlist page started the player with (the latest one). A smart playlist
// sorted at random comes back in a new order each time it's asked for, so the player goes by
// this one rather than asking again. Kept in memory: after a reload the player asks the server.
let started: { playlistId: number; seed: number | undefined; order: readonly unknown[] } | null = null;

export function rememberPlayOrder<T>(playlistId: number, seed: number | undefined, order: readonly T[]) {
  started = { playlistId, seed, order };
}

export function recallPlayOrder<T>(playlistId: number, seed: number | undefined): readonly T[] | null {
  return started && started.playlistId === playlistId && started.seed === seed ? (started.order as readonly T[]) : null;
}
