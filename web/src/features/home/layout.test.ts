import { describe, expect, it } from "vitest";
import { pinnedTarget } from "./layout";

describe("pinned Home rows", () => {
  it("open their collection or playlist", () => {
    expect(pinnedTarget("collection-12")).toEqual({ to: "/item/$itemId", params: { itemId: "12" } });
    expect(pinnedTarget("playlist-3")).toEqual({ to: "/playlist/$playlistId", params: { playlistId: "3" } });
    expect(pinnedTarget("recent-2")).toBeNull();
  });
});
