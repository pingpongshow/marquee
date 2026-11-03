import { describe, expect, it } from "vitest";
import { formatDistance, formatShowDate, formatShowTime, guessCountry, nearLine, showsHeadline, unitForCountry } from "./inTownFormat";

describe("in town wording", () => {
  it("counts shows", () => {
    expect(showsHeadline("Radiohead", 1)).toBe("Radiohead has 1 upcoming show");
    expect(showsHeadline("Radiohead", 3)).toBe("Radiohead has 3 upcoming shows");
  });
  it("says what's near", () => {
    expect(nearLine(0)).toBe("");
    expect(nearLine(1)).toBe("One near you");
    expect(nearLine(4)).toBe("4 near you");
  });
  it("gives distance only when it's known", () => {
    expect(formatDistance(undefined, undefined)).toBe("");
    expect(formatDistance(3.26, "miles")).toBe("3.3 miles away");
    expect(formatDistance(12.6, "km")).toBe("13 km away");
  });
  it("picks a sensible country and unit", () => {
    expect(guessCountry("en-GB")).toBe("GB");
    expect(guessCountry("en")).toBe("US");
    expect(guessCountry("ja-JP")).toBe("US");
    expect(unitForCountry("GB")).toBe("miles");
    expect(unitForCountry("DE")).toBe("km");
  });
});

describe("venue-local dates", () => {
  // These are the venue's, so they mustn't move with the viewer's zone: the day of the month
  // stays what was sent wherever this runs.
  it("keeps the venue's date", () => {
    expect(formatShowDate("2026-11-20", new Date(2026, 0, 1))).toContain("20");
    expect(formatShowDate("2026-11-20", new Date(2026, 0, 1))).not.toContain("2026");
    expect(formatShowDate("2027-01-02", new Date(2026, 0, 1))).toContain("2027");
    expect(formatShowDate("nonsense")).toBe("nonsense");
  });
  it("reads a time as it is", () => {
    expect(formatShowTime(undefined)).toBe("");
    expect(formatShowTime("19:30")).toMatch(/30/);
  });
});
