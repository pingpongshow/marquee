import { describe, expect, it } from "vitest";
import type { ItemSummary } from "@/api/types";
import { current, emptyQueue, load, thumbsDown, toggleShuffle, type Queue } from "./queue";

const track = (id: number) => ({ id, title: `T${id}`, type: "track" }) as ItemSummary;
const ids = (q: { entries: { item: ItemSummary }[] }) => q.entries.map((e) => e.item.id);
const queue = (list: number[], index = 0): Queue => load(emptyQueue, list.map(track), index);

describe("a thumbs down (MUSIC-25)", () => {
  it("skips the playing song on a station and drops its upcoming copies", () => {
    const q = thumbsDown(queue([1, 2, 3, 2, 4, 2], 1), 2, true);
    expect(ids(q)).toEqual([1, 2, 3, 4]);
    expect(q.index).toBe(2);
    expect(current(q)?.item.id).toBe(3);
  });

  it("leaves an album or the listener's own playlist alone", () => {
    const q = queue([1, 2, 3, 2], 1);
    expect(thumbsDown(q, 2, false)).toBe(q);
  });

  it("keeps played copies, so going back still works", () => {
    const q = thumbsDown(queue([2, 1, 2, 3], 2), 2, true);
    expect(ids(q)).toEqual([2, 1, 2, 3]);
    expect(current(q)?.item.id).toBe(3);
  });

  it("drops upcoming copies of another song without moving on", () => {
    const q = thumbsDown(queue([1, 2, 3, 2], 0), 2, true);
    expect(ids(q)).toEqual([1, 3]);
    expect(q.index).toBe(0);
  });

  it("changes nothing when the song isn't still to come", () => {
    const before = queue([2, 1, 3], 1);
    expect(thumbsDown(before, 2, true)).toBe(before);
    expect(thumbsDown(before, 9, true)).toBe(before);
  });

  it("stays put when nothing can follow, for the player to stop it", () => {
    const q = thumbsDown(queue([1, 2], 1), 2, true);
    expect(q.index).toBe(1);
    expect(current(q)?.item.id).toBe(2);
  });

  it("doesn't come back round under repeat", () => {
    const all: Queue = { ...queue([2, 1, 2], 2), repeat: "all" };
    const q = thumbsDown(all, 2, true);
    expect(ids(q)).toEqual([1, 2]);
    expect(current(q)?.item.id).toBe(1);
    const alone: Queue = { ...queue([2], 0), repeat: "all" };
    expect(current(thumbsDown(alone, 2, true))?.item.id).toBe(2);
    // Repeat one doesn't hold it either.
    const one: Queue = { ...queue([2, 1], 0), repeat: "one" };
    expect(current(thumbsDown(one, 2, true))?.item.id).toBe(1);
  });

  it("takes copies out of the unshuffled order too", () => {
    const shuffled = toggleShuffle(queue([1, 2, 3, 2], 0), () => 0);
    const q = thumbsDown(shuffled, 2, true);
    expect(ids(q)).toEqual([1, 3]);
    expect(q.original?.map((e) => e.item.id)).toEqual([1, 3]);
  });
});
