import { describe, expect, it } from "vitest";
import { nextInPlaylist, seededShuffle } from "./order";

const items = Array.from({ length: 20 }, (_, i) => ({ id: i + 1 }));

describe("playlist order", () => {
  it("shuffles the same way for the same seed and keeps every item", () => {
    const a = seededShuffle(items, 42);
    expect(seededShuffle(items, 42)).toEqual(a);
    expect(a).not.toEqual(items);
    expect([...a].sort((x, y) => x.id - y.id)).toEqual(items);
  });

  it("finds the next item in order or in the shuffled order", () => {
    expect(nextInPlaylist(items, 1)?.id).toBe(2);
    expect(nextInPlaylist(items, 20)).toBeNull();
    const order = seededShuffle(items, 7);
    expect(nextInPlaylist(items, order[0]!.id, 7)?.id).toBe(order[1]!.id);
    expect(nextInPlaylist(items, order[19]!.id, 7)).toBeNull();
  });
});
