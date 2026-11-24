import { describe, expect, it } from "vitest";
import type { ItemSummary, PlaylistEntry } from "@/api/types";
import { duplicateCount, matchesFilter, moveSelected, nudgeSelected, rangeIds, sortEntries, sortKeysFor, withoutDuplicates } from "./editor";

const entry = (entryId: number, itemId: number, title: string, more: Partial<ItemSummary> = {}): PlaylistEntry => ({
  entryId,
  item: { id: itemId, title, type: "track", ...more } as ItemSummary,
});

const ids = (list: PlaylistEntry[]) => list.map((e) => e.entryId);

describe("playlist editor", () => {
  const list = [
    entry(1, 10, "Silent Night", { artistCredit: "Bing", parentTitle: "Christmas", index: 2, durationMs: 200 }),
    entry(2, 11, "Jingle Bells", { artistCredit: "Abba", parentTitle: "Winter", index: 1, durationMs: 100 }),
    entry(3, 12, "White Christmas", { artistCredit: "Bing", parentTitle: "Christmas", index: 1, durationMs: 300 }),
    entry(4, 10, "Silent Night", { artistCredit: "Bing", parentTitle: "Christmas", index: 2, durationMs: 200 }),
  ];

  it("sorts by title, artist (album order kept), length and order added, either way", () => {
    expect(ids(sortEntries(list, "title"))).toEqual([2, 1, 4, 3]);
    expect(ids(sortEntries(list, "artist"))).toEqual([2, 3, 1, 4]);
    expect(ids(sortEntries(list, "duration", true))).toEqual([3, 1, 4, 2]);
    expect(ids(sortEntries([...list].reverse(), "added"))).toEqual([1, 2, 3, 4]);
  });

  it("removes duplicates, keeping the first", () => {
    expect(duplicateCount(list)).toBe(1);
    expect(ids(withoutDuplicates(list))).toEqual([1, 2, 3]);
  });

  it("selects a range either way round", () => {
    expect(rangeIds(list, 3, 1)).toEqual([1, 2, 3]);
    expect(rangeIds(list, 99, 2)).toEqual([2]);
  });

  it("moves the selection together, keeping its order", () => {
    expect(ids(moveSelected(list, new Set([3, 1]), 2))).toEqual([1, 3, 2, 4]);
    expect(ids(moveSelected(list, new Set([1, 2]), null))).toEqual([3, 4, 1, 2]);
    // Dropped on one of themselves: they stay together where they were.
    expect(ids(moveSelected(list, new Set([2, 3]), 3))).toEqual([1, 2, 3, 4]);
    expect(ids(nudgeSelected(list, new Set([2, 3]), -1))).toEqual([2, 3, 1, 4]);
    expect(ids(nudgeSelected(list, new Set([4]), 1))).toEqual([1, 2, 3, 4]);
  });

  it("filters on every word in title, artist or album", () => {
    expect(list.filter((e) => matchesFilter(e, "bing christ")).map((e) => e.entryId)).toEqual([1, 3, 4]);
    expect(list.filter((e) => matchesFilter(e, "  ")).length).toBe(4);
  });

  it("offers a collection no music sorts", () => {
    expect(sortKeysFor("audio")).toContain("artist");
    expect(sortKeysFor("video")).toEqual(["title", "added", "libraryAdded"]);
  });
});
