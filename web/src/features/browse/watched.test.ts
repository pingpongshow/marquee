import { describe, expect, it } from "vitest";
import { applyWatched, isWatched, withoutContinueWatching } from "./watched";

const movie = { id: 5, type: "movie", title: "M", available: true, viewCount: 0, viewOffsetMs: 60000 };
const season = { id: 20, type: "season", parentId: 10, title: "S1", available: true, leafCount: 3, watchedLeafCount: 1 };
const ep = (id: number) => ({ id, type: "episode", parentId: 20, grandparentId: 10, title: `E${id}`, available: true, viewCount: 0 });

describe("applyWatched", () => {
  it("marks a movie watched wherever it is cached and clears its position", () => {
    const page = { items: [movie, { ...movie, id: 6 }], total: 2 };
    const out = applyWatched(page, 5, true);
    expect(out.items[0]).toMatchObject({ viewCount: 1, viewOffsetMs: 0 });
    expect(out.items[1]).toBe(page.items[1]); // untouched keeps identity
    expect(isWatched(out.items[0] as never)).toBe(true);
  });

  it("marks a show's seasons and episodes", () => {
    const show = { id: 10, type: "show", title: "Show", available: true, leafCount: 3, watchedLeafCount: 0 };
    const data = { show, seasons: [season], episodes: [ep(31), ep(32)] };
    const out = applyWatched(data, 10, true);
    expect(out.show.watchedLeafCount).toBe(3);
    expect(out.seasons[0]!.watchedLeafCount).toBe(3);
    expect(out.episodes.every((e) => e.viewCount === 1)).toBe(true);
    const back = applyWatched(out, 20, false);
    expect(back.seasons[0]!.watchedLeafCount).toBe(0);
    expect(back.episodes.every((e) => e.viewCount === 0)).toBe(true);
  });

  it("leaves libraries and playlists with the same id alone", () => {
    const libs = [{ id: 5, type: "movie", name: "Movies" }];
    expect(applyWatched(libs, 5, true)).toBe(libs);
    expect(applyWatched(undefined, 5, true)).toBeUndefined();
  });
});

describe("withoutContinueWatching", () => {
  it("drops the item, or a show's episodes, from Continue Watching only", () => {
    const hubs = [
      { id: "continue-watching", items: [movie, ep(31)] as never[] },
      { id: "recent", items: [movie] as never[] },
    ];
    const out = withoutContinueWatching(hubs, 10)!;
    expect(out[0]!.items).toEqual([movie]);
    expect(out[1]!.items).toEqual([movie]);
    expect(withoutContinueWatching(hubs, 5)![0]!.items).toHaveLength(1);
  });
});
