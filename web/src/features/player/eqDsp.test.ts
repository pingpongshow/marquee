import { describe, expect, it } from "vitest";
import { EQ_BANDS, EQ_MAX_DB, EQ_PRESETS, defaultEq, storedEq } from "./eqDsp";

describe("equaliser presets", () => {
  it("has every preset the apps share, with a gain per band within ±12 dB", () => {
    expect(Object.keys(EQ_PRESETS)).toEqual([
      "Flat", "Bass Boost", "Bass Reducer", "Treble Boost", "Vocal", "Rock", "Pop", "Jazz", "Classical", "Electronic", "Loudness",
    ]);
    for (const gains of Object.values(EQ_PRESETS)) {
      expect(gains).toHaveLength(EQ_BANDS.length);
      for (const g of gains) expect(Math.abs(g)).toBeLessThanOrEqual(EQ_MAX_DB);
    }
  });

  it("falls back to off and flat without storage", () => {
    expect(storedEq()).toEqual(defaultEq);
    expect(defaultEq.enabled).toBe(false);
  });
});
