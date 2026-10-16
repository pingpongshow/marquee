import { describe, expect, it } from "vitest";
import { canRemove, commercialsText, type CommercialScan } from "./commercials";

const scan = (c: Partial<CommercialScan>): CommercialScan => ({ status: "pending", breaks: [], available: true, ...c });

describe("commercialsText", () => {
  it("describes each state", () => {
    expect(commercialsText(scan({}))).toBe("Commercials: not checked yet");
    expect(commercialsText(scan({ status: "detected" }))).toBe("Commercials: none found");
    expect(
      commercialsText(scan({ status: "detected", breaks: [{ startMs: 600_000, endMs: 750_000 }, { startMs: 1_200_000, endMs: 1_290_000 }] })),
    ).toBe("Commercials: found 2 breaks (4:00)");
    expect(commercialsText(scan({ status: "cut", removedMs: 750_000 }))).toBe("Commercials removed (12:30)");
    expect(commercialsText(scan({ status: "failed", error: "Comskip failed" }))).toBe("Commercials: failed. Comskip failed");
  });
  it("offers removal only when it can happen", () => {
    expect(canRemove(scan({ status: "detected" }))).toBe(true);
    expect(canRemove(scan({ status: "cut" }))).toBe(false);
    expect(canRemove(scan({ status: "running" }))).toBe(false);
    expect(canRemove(scan({ status: "detected", available: false }))).toBe(false);
  });
});
