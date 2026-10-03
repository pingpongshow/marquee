import { describe, expect, it } from "vitest";
import { playlistIds, queueTitle } from "./SaveAsPlaylist";

describe("save as playlist", () => {
  it("keeps order, drops repeats and caps at 500", () => {
    expect(playlistIds([3, 1, 3, 2, 1])).toEqual([3, 1, 2]);
    expect(playlistIds(Array.from({ length: 900 }, (_, i) => i))).toHaveLength(500);
  });
  it("names a plain queue by date", () => {
    expect(queueTitle(new Date(2026, 9, 2))).toMatch(/^Queue – .*2026$/);
  });
});
