import { describe, expect, it } from "vitest";
import { isPlayerPath } from "./playerPaths";

describe("isPlayerPath", () => {
  it("knows all four players", () => {
    expect(isPlayerPath("/play/12")).toBe(true);
    expect(isPlayerPath("/vod/7/play")).toBe(true);
    expect(isPlayerPath("/vod/shows/3/play/44")).toBe(true);
    expect(isPlayerPath("/livetv/watch/9")).toBe(true);
  });

  it("leaves other pages alone", () => {
    for (const p of ["/", "/item/12", "/vod", "/vod/7", "/vod/shows/3", "/livetv", "/playlist/4", "/playlists"]) expect(isPlayerPath(p)).toBe(false);
  });
});
