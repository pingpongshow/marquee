import { describe, expect, it } from "vitest";
import { chartSpan } from "./statsSpan";

const today = new Date(2026, 10, 23); // 23 Nov 2026

describe("chartSpan", () => {
  it("is the chosen period", () => {
    expect(chartSpan(7, ["2026-01-01"], today)).toBe(7);
  });

  it("for all time, runs from the first day played to today, not the number of days played", () => {
    // Three days with plays, the first 100 days ago.
    expect(chartSpan(0, ["2026-11-20", "2026-08-16", "2026-10-01"], today)).toBe(100);
  });

  it("is at least 30 days and at most a year", () => {
    expect(chartSpan(0, ["2026-11-22"], today)).toBe(30);
    expect(chartSpan(0, ["2024-01-01", "2026-11-22"], today)).toBe(365);
    expect(chartSpan(0, [], today)).toBe(30);
  });
});
