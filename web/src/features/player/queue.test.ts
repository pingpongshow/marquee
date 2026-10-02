import { describe, expect, it } from "vitest";
import type { ItemSummary } from "@/api/types";
import {
  append,
  current,
  cycleRepeat,
  emptyQueue,
  followingIndex,
  load,
  move,
  playNext,
  remove,
  skipIndex,
  toggleShuffle,
} from "./queue";

const track = (id: number) =>
  ({ id, title: `T${id}`, type: "track" }) as ItemSummary;
const ids = (q: { entries: { item: ItemSummary }[] }) =>
  q.entries.map((e) => e.item.id);
const reverse = () => {
  // Deterministic "random" that makes the Fisher–Yates shuffle reverse the list.
  return 0;
};

describe("queue", () => {
  it("loads and starts at the requested track", () => {
    const q = load(emptyQueue, [track(1), track(2), track(3)], 1);
    expect(current(q)?.item.id).toBe(2);
  });

  it("shuffles the upcoming tracks and restores the original order", () => {
    let q = load(emptyQueue, [1, 2, 3, 4, 5].map(track), 1);
    q = toggleShuffle(q, reverse);
    expect(ids(q).slice(0, 2)).toEqual([1, 2]);
    expect(ids(q).slice(2).sort()).toEqual([3, 4, 5]);
    expect(current(q)?.item.id).toBe(2);
    q = toggleShuffle(q);
    expect(ids(q)).toEqual([1, 2, 3, 4, 5]);
    expect(current(q)?.item.id).toBe(2);
  });

  it("starting shuffled plays the chosen track first", () => {
    const q = load(emptyQueue, [1, 2, 3].map(track), 2, true, reverse);
    expect(current(q)?.item.id).toBe(3);
    expect(q.index).toBe(0);
    expect(q.shuffled).toBe(true);
  });

  it("play next inserts after the current track, also in the unshuffled order", () => {
    let q = load(emptyQueue, [1, 2, 3].map(track), 0);
    q = toggleShuffle(q, reverse);
    q = playNext(q, [track(9)]);
    expect(ids(q)[1]).toBe(9);
    q = toggleShuffle(q);
    expect(ids(q)).toEqual([1, 9, 2, 3]);
  });

  it("removes and moves while keeping the current track", () => {
    let q = load(emptyQueue, [1, 2, 3, 4].map(track), 2);
    q = remove(q, q.entries[0]!.key);
    expect(current(q)?.item.id).toBe(3);
    q = move(q, q.entries[2]!.key, 0);
    expect(ids(q)).toEqual([4, 2, 3]);
    expect(current(q)?.item.id).toBe(3);
    q = remove(q, current(q)!.key);
    expect(ids(q)).toEqual([4, 2]);
    expect(current(q)?.item.id).toBe(2);
  });

  it("repeat modes decide what follows the last track", () => {
    let q = load(emptyQueue, [1, 2].map(track), 1);
    expect(followingIndex(q)).toBe(-1);
    q = cycleRepeat(q);
    expect(followingIndex(q)).toBe(0);
    q = cycleRepeat(q);
    expect(followingIndex(q)).toBe(1);
    expect(skipIndex(q)).toBe(0);
    q = append(q, [track(3)]);
    expect(ids(q)).toEqual([1, 2, 3]);
  });
});
