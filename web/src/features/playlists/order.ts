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

/** The item after `currentId` in a playlist's play order (shuffled when there's a seed); null at the end. */
export function nextInPlaylist<T extends { id: number }>(items: readonly T[], currentId: number, seed?: number): T | null {
  const order = seed ? seededShuffle(items, seed) : items;
  const i = order.findIndex((it) => it.id === currentId);
  return i >= 0 ? (order[i + 1] ?? null) : null;
}
