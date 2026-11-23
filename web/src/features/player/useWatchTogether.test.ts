import { describe, expect, it } from "vitest";
import { clockOffset, groupPosition } from "./useWatchTogether";

const at = "2026-11-01T20:00:00.000Z";
const t0 = new Date(at).getTime();

describe("watch together position", () => {
  it("is where the group was plus the time played since, on the server's clock", () => {
    // This clock runs 5 s behind the server's.
    const arrived = t0 - 5_000 + 2_000; // two seconds after the group last changed
    const offset = clockOffset({ serverTime: new Date(t0 + 2_000).toISOString() }, arrived);
    expect(offset).toBe(5_000);
    const g = { playing: true, positionMs: 60_000, at };
    expect(groupPosition(g, offset, arrived)).toBe(62_000);
    // Applied again 8 s later (the video took that long to load): the group has moved on 8 s.
    expect(groupPosition(g, offset, arrived + 8_000)).toBe(70_000);
  });

  it("stays put while the group is paused", () => {
    expect(groupPosition({ playing: false, positionMs: 60_000, at }, 5_000, t0 + 90_000)).toBe(60_000);
  });
});
