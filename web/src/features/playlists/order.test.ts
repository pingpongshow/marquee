import { describe, expect, it } from "vitest";
import { nextInOrder, nextInPlaylist, recallPlayOrder, rememberPlayOrder, seededShuffle } from "./order";

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

  it("goes on past an item that's in the playlist twice, given its place", () => {
    const twice = [{ id: 1 }, { id: 2 }, { id: 1 }, { id: 3 }];
    // By id alone the second copy looks like the first, and the playlist loops.
    expect(nextInOrder(twice, 1)).toEqual({ item: { id: 2 }, at: 1 });
    expect(nextInOrder(twice, 1, 2)).toEqual({ item: { id: 3 }, at: 3 });
    expect(nextInOrder(twice, 3, 3)).toBeNull();
    // A place that doesn't hold the item (the playlist changed) falls back to the id.
    expect(nextInOrder(twice, 2, 3)).toEqual({ item: { id: 1 }, at: 2 });
    expect(nextInOrder(twice, 9, 0)).toBeNull();
    expect(nextInPlaylist(twice, 1, undefined, 2)?.id).toBe(3);
  });

  it("recalls the order a playlist was started with, for that playlist and shuffle only", () => {
    const order = [{ id: 3 }, { id: 1 }, { id: 2 }];
    rememberPlayOrder(5, 42, order);
    expect(recallPlayOrder(5, 42)).toBe(order);
    expect(recallPlayOrder(5, undefined)).toBeNull();
    expect(recallPlayOrder(6, 42)).toBeNull();
  });
});
