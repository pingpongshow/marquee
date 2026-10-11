import type { ItemSummary } from "@/api/types";

/**
 * The rating request at the end of a movie or episode (USER-19). Pure helpers, so the rules
 * are testable without a player.
 */

/** Past this share of the running time an item counts as watched (the server's default). */
export const WATCHED_FRACTION = 0.9;

/**
 * Whether to ask for a rating of this item: the admin has it on, the person hasn't turned it
 * off, and it's a movie or episode (not a trailer, extra or cinema pre-roll) they haven't rated.
 */
export function wantsRatingPrompt(opts: {
  item?: Pick<ItemSummary, "type" | "extraType" | "userRating">;
  serverOn?: boolean;
  personOn?: boolean;
  preroll?: boolean;
}) {
  const { item, serverOn, personOn, preroll } = opts;
  if (!item || preroll || !serverOn || personOn === false) return false;
  if (item.type !== "movie" && item.type !== "episode") return false;
  if (item.extraType) return false;
  return !item.userRating;
}

/** Watched far enough to count as finished. */
export function finishedWatching(timeSec: number, durationSec: number) {
  return durationSec > 0 && timeSec >= durationSec * WATCHED_FRACTION;
}

/** "Show · S01E03 · Title" for an episode, the title otherwise. */
export function promptTitle(
  item: Pick<ItemSummary, "type" | "title" | "index" | "parentTitle" | "grandparentTitle">,
) {
  if (item.type !== "episode") return item.title;
  const season = /(\d+)\s*$/.exec(item.parentTitle ?? "")?.[1];
  const pad = (n: number | string) => String(n).padStart(2, "0");
  const code =
    season && item.index != null
      ? `S${pad(season)}E${pad(item.index)}`
      : [item.parentTitle, item.index != null ? `E${item.index}` : undefined]
          .filter(Boolean)
          .join(" · ");
  return [item.grandparentTitle, code, item.title].filter(Boolean).join(" · ");
}
