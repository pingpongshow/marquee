import { describe, expect, it } from "vitest";
import type { ItemSummary } from "@/api/types";
import { albumRun, levelGain, MAX_BOOST_DB } from "./levelling";
import type { Entry } from "./queue";

const db = (g: number) => 20 * Math.log10(g);
const entry = (key: number, parentId?: number): Entry => ({
  key,
  item: { id: key, parentId } as unknown as ItemSummary,
});

describe("levelGain", () => {
  it("applies cuts in full", () => {
    expect(db(levelGain({ trackGain: -8, peak: 1 }, "track", false))).toBeCloseTo(-8);
    expect(db(levelGain({ trackGain: -6, peak: 1.2 }, "auto", false, true))).toBeCloseTo(-6);
  });

  it("never deepens a cut because of the peak", () => {
    expect(db(levelGain({ trackGain: -1, peak: 1.4 }, "track", false))).toBeCloseTo(-1);
  });

  it("raises quiet tracks, at most +6 dB", () => {
    expect(db(levelGain({ trackGain: 4, peak: 0.3 }, "track", false))).toBeCloseTo(4);
    expect(db(levelGain({ trackGain: 11, peak: 0.1 }, "track", false))).toBeCloseTo(MAX_BOOST_DB);
  });

  it("keeps a boosted peak under full scale without the limiter", () => {
    // peak 0.8 leaves 1.94 dB of room.
    const g = levelGain({ trackGain: 5, peak: 0.8 }, "track", false);
    expect(g * 0.8).toBeCloseTo(1);
    // A peak already at full scale isn't boosted at all.
    expect(levelGain({ trackGain: 5, peak: 1 }, "track", false)).toBe(1);
  });

  it("lets the limiter take a few dB of peaks", () => {
    const g = levelGain({ trackGain: 5, peak: 1 }, "track", false, true);
    expect(db(g)).toBeCloseTo(3);
  });

  it("uses album gain in album mode and auto within an album", () => {
    const gains = { trackGain: -9, albumGain: -7, peak: 1 };
    expect(db(levelGain(gains, "album", false))).toBeCloseTo(-7);
    expect(db(levelGain(gains, "auto", true))).toBeCloseTo(-7);
    expect(db(levelGain(gains, "auto", false))).toBeCloseTo(-9);
    expect(db(levelGain({ trackGain: -9 }, "album", false))).toBeCloseTo(-9);
  });

  it("is unity when off or without data", () => {
    expect(levelGain({ trackGain: -9 }, "off", false)).toBe(1);
    expect(levelGain({}, "track", false)).toBe(1);
    expect(levelGain(null, "auto", true)).toBe(1);
  });
});

describe("albumRun", () => {
  const q = [entry(1, 10), entry(2, 10), entry(3, 20), entry(4, 30), entry(5)];
  it("counts the first track of an album as well as the rest", () => {
    expect(albumRun(q, 0)).toBe(true);
    expect(albumRun(q, 1)).toBe(true);
  });
  it("is false for a lone track or one without an album", () => {
    expect(albumRun(q, 2)).toBe(false);
    expect(albumRun(q, 4)).toBe(false);
    expect(albumRun(q, 9)).toBe(false);
  });
});
