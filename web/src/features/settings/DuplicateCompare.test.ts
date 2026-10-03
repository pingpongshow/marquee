import { describe, expect, it } from "vitest";
import type { components } from "@/api/schema.gen";
import { best } from "./DuplicateCompare";

type IssueFile = components["schemas"]["IssueFile"];
const file = (id: number, height: number, bitrateKbps: number): IssueFile => ({
  itemId: 1,
  itemTitle: "Heat",
  addedAt: "2026-01-01T00:00:00Z",
  file: { id, size: 1, height, width: height * 2, bitrateKbps, partIndex: 0, available: true, streams: [] },
});

describe("duplicate comparison", () => {
  it("highlights the better file only when files differ", () => {
    const files = [file(1, 1080, 8000), file(2, 2160, 8000)];
    expect(best(files, (f) => (f.width ?? 0) * (f.height ?? 0))).toEqual([false, true]);
    expect(best(files, (f) => f.bitrateKbps ?? 0)).toEqual([false, false]);
  });
  it("highlights ties for best", () => {
    const files = [file(1, 2160, 1), file(2, 2160, 1), file(3, 720, 1)];
    expect(best(files, (f) => f.height ?? 0)).toEqual([true, true, false]);
  });
});
