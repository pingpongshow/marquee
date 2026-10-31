import { describe, expect, it } from "vitest";
import type { ItemSummary } from "@/api/types";
import {
  append,
  current,
  cycleRepeat,
  departure,
  emptyQueue,
  followingIndex,
  load,
  move,
  playContext,
  playNext,
  remove,
  replaceUpcoming,
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

  it("starting shuffled shuffles every track, the first one included", () => {
    const q = load(emptyQueue, [1, 2, 3].map(track), 0, true, reverse);
    expect(ids(q).sort()).toEqual([1, 2, 3]);
    expect(q.index).toBe(0);
    expect(q.shuffled).toBe(true);
    // With real randomness the opening track varies from one Shuffle to the next.
    const firsts = new Set<number>();
    for (let i = 0; i < 200; i++) firsts.add(current(load(emptyQueue, [1, 2, 3, 4, 5].map(track), 0, true))!.item.id);
    expect(firsts.size).toBe(5);
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

describe("stations (MUSIC-17)", () => {
  it("tags a station's tracks with its context, the DJ's picks with dj", () => {
    let q = load(emptyQueue, [track(1), track(2)], 0, false, Math.random, "radio:mood");
    q = append(q, [track(3)], "radio:mood");
    q = playNext(q, [track(9)], "wander");
    q = append(q, [track(8)]);
    expect(q.entries.map(playContext)).toEqual(["radio:mood", "dj", "radio:mood", "radio:mood", undefined]);
  });

  it("replaces only the station's upcoming tracks when it's steered", () => {
    let q = load(emptyQueue, [track(1), track(2), track(3)], 0, false, Math.random, "radio:item");
    q = append(q, [track(7)]); // queued by hand
    q = { ...q, index: 1 };
    const next = replaceUpcoming(q, [track(4), track(5)], "radio:item");
    expect(ids(next)).toEqual([1, 2, 7, 4, 5]);
    expect(current(next)?.item.id).toBe(2);
    expect(next.entries.at(-1)?.ctx).toBe("radio:item");
  });

  it("tells a quick skip from a track played through", () => {
    expect(departure(10, 240)).toBe("skipped");
    expect(departure(200, 240)).toBe("finished");
    expect(departure(239, 240)).toBe("finished");
    expect(departure(90, 240)).toBeNull();
    expect(departure(25, 30)).toBe("finished"); // 80% of a short track
    expect(departure(10, 30)).toBe("skipped"); // before half way
  });
});
