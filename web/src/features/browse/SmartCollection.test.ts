import { describe, expect, it } from "vitest";
import { cleanRules, describeRules } from "./SmartCollection";

describe("smart collection rules", () => {
  it("drops unset filters before saving", () => {
    expect(
      cleanRules({ itemType: "movie", sort: "title", genre: "", decade: undefined, limit: 0, watch: "unwatched", hdr: false, minRating: Number.NaN, studio: " A24 " }),
    ).toEqual({ itemType: "movie", watch: "unwatched", studio: "A24" });
  });

  it("describes rules in plain words", () => {
    expect(describeRules({ itemType: "movie", watch: "unwatched", genre: "Action", decade: 1990, sort: "-rating" })).toBe("Unwatched · Action · 1990s · By rating");
    expect(describeRules({ itemType: "show" })).toBe("All shows");
  });
});
