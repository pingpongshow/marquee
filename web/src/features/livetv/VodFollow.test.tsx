import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { FollowControls, followSummary } from "./VodFollow";
import type { VodFollow, VodSeriesDetail } from "./vodApi";

const follow: VodFollow = {
  seriesId: 7,
  title: "Kings",
  userId: 2,
  usedBytes: 4.2e9,
  episodesSaved: 3,
  episodesQueued: 1,
  status: "quota",
  statusText: "Paused: your recording space is full (50 GB of 50 GB)",
  createdAt: "2026-10-18T10:00:00Z",
};

const show = (following?: boolean) =>
  ({
    id: 7,
    title: "Kings",
    following,
    seasonCount: 1,
    episodeCount: 3,
    watchedCount: 0,
    watched: false,
    matched: false,
    genres: [],
    cast: [],
    seasons: [],
    episodes: [],
  }) as VodSeriesDetail;

function render(s: VodSeriesDetail, follows?: VodFollow[]) {
  const qc = new QueryClient();
  if (follows) qc.setQueryData(["vod", "follows", "mine"], follows);
  return renderToStaticMarkup(
    <QueryClientProvider client={qc}>
      <FollowControls show={s} />
    </QueryClientProvider>,
  );
}

describe("following a show", () => {
  it("sums up a follow", () => {
    expect(followSummary(follow)).toBe("Following · 3 episodes · 4.2 GB · 1 saving");
    expect(followSummary({ ...follow, episodesSaved: 1, usedBytes: 0, episodesQueued: 0 })).toBe("Following · 1 episode");
  });

  it("shows nothing when the person can't follow", () => {
    expect(render(show(undefined))).toBe("");
  });

  it("offers Follow, and once followed the summary with why it stopped", () => {
    expect(render(show(false), [])).toContain("Follow");
    const html = render(show(true), [follow]);
    expect(html).toContain("Following · 3 episodes · 4.2 GB");
    expect(html).toContain("Unfollow");
    expect(html).toContain("recording space is full");
  });
});
