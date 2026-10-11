import { describe, expect, it } from "vitest";
import { finishedWatching, promptTitle, wantsRatingPrompt } from "./ratingRules";

describe("wantsRatingPrompt", () => {
  const movie = { type: "movie" as const };
  it("asks for an unrated movie or episode when both switches are on", () => {
    expect(wantsRatingPrompt({ item: movie, serverOn: true })).toBe(true);
    expect(wantsRatingPrompt({ item: { type: "episode" }, serverOn: true, personOn: true })).toBe(true);
  });
  it("doesn't ask when the server or the person has it off", () => {
    expect(wantsRatingPrompt({ item: movie, serverOn: false })).toBe(false);
    expect(wantsRatingPrompt({ item: movie })).toBe(false);
    expect(wantsRatingPrompt({ item: movie, serverOn: true, personOn: false })).toBe(false);
  });
  it("skips trailers, extras, pre-rolls, other types and rated items", () => {
    expect(wantsRatingPrompt({ item: { type: "movie", extraType: "trailer" }, serverOn: true })).toBe(false);
    expect(wantsRatingPrompt({ item: movie, serverOn: true, preroll: true })).toBe(false);
    expect(wantsRatingPrompt({ item: { type: "track" }, serverOn: true })).toBe(false);
    expect(wantsRatingPrompt({ item: { type: "movie", userRating: 6 }, serverOn: true })).toBe(false);
  });
});

describe("finishedWatching", () => {
  it("counts 90% as watched", () => {
    expect(finishedWatching(89, 100)).toBe(false);
    expect(finishedWatching(90, 100)).toBe(true);
    expect(finishedWatching(5, 0)).toBe(false);
  });
});

describe("promptTitle", () => {
  it("formats episodes as show · SxxEyy · title", () => {
    expect(
      promptTitle({ type: "episode", title: "Pilot", index: 3, parentTitle: "Season 1", grandparentTitle: "Show" }),
    ).toBe("Show · S01E03 · Pilot");
    expect(
      promptTitle({ type: "episode", title: "Pilot", index: 3, parentTitle: "Specials", grandparentTitle: "Show" }),
    ).toBe("Show · Specials · E3 · Pilot");
    expect(promptTitle({ type: "movie", title: "Heat" })).toBe("Heat");
  });
});
