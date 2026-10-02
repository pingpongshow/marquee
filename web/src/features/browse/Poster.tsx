import { clsx } from "clsx";
import { Disc3, Film, Music, Tv, Video } from "lucide-react";
import { useState } from "react";
import { imageUrl } from "@/api/client";
import type { ItemSummary } from "@/api/types";

const icons = { movie: Film, show: Tv, season: Tv, episode: Tv, artist: Music, album: Disc3, track: Music, video: Video };

// Deterministic hue per title so placeholder posters are distinguishable.
function hue(s: string) {
  let h = 0;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 360;
  return h;
}

type Shape = "poster" | "square" | "wide";

/** Item artwork with a titled placeholder when there's no image (or it fails to load). */
export function Poster({
  item,
  shape = "poster",
  width = 240,
  className,
}: {
  item: Pick<ItemSummary, "title" | "type" | "available" | "images"> & Partial<Pick<ItemSummary, "viewOffsetMs" | "durationMs" | "viewCount" | "leafCount" | "watchedLeafCount">>;
  shape?: Shape;
  width?: number;
  className?: string;
}) {
  const [failed, setFailed] = useState(false);
  const Icon = icons[item.type] ?? Film;
  const artId = shape === "wide" ? (item.images?.thumb ?? item.images?.backdrop) : item.images?.poster;
  const h = hue(item.title);
  return (
    <div
      className={clsx(
        "relative flex w-full items-end overflow-hidden rounded-md bg-surface-2 shadow-md",
        shape === "poster" && "aspect-[2/3]",
        shape === "square" && "aspect-square",
        shape === "wide" && "aspect-video",
        !item.available && "opacity-40 grayscale",
        className,
      )}
      style={artId && !failed ? undefined : { background: `linear-gradient(160deg, hsl(${h} 35% 28%), hsl(${(h + 40) % 360} 30% 12%))` }}
    >
      <WatchBadges item={item} />
      {artId && !failed ? (
        <img src={imageUrl(artId, width)} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} className="absolute inset-0 size-full object-cover" />
      ) : (
        <>
          <Icon className="absolute top-3 left-3 size-5 text-white/30" aria-hidden />
          <span className="line-clamp-3 p-3 text-sm leading-tight font-semibold text-white/90">{item.title}</span>
        </>
      )}
    </div>
  );
}

/** Plex-style overlays: resume progress, a watched tick, and unwatched-episode counts. */
function WatchBadges({ item }: { item: Parameters<typeof Poster>[0]["item"] }) {
  const leaf = item.type === "movie" || item.type === "episode" || item.type === "video";
  const unwatched = (item.type === "show" || item.type === "season") && item.leafCount ? item.leafCount - (item.watchedLeafCount ?? 0) : 0;
  return (
    <>
      {leaf && (item.viewOffsetMs ?? 0) > 0 && item.durationMs ? (
        <div className="absolute inset-x-0 bottom-0 z-10 h-1 bg-black/60">
          <div className="h-full bg-accent" style={{ width: `${Math.min(100, ((item.viewOffsetMs ?? 0) / item.durationMs) * 100)}%` }} />
        </div>
      ) : null}
      {leaf && (item.viewCount ?? 0) > 0 && !item.viewOffsetMs && (
        <span className="absolute top-1.5 right-1.5 z-10 flex size-5 items-center justify-center rounded-full bg-black/70 text-[11px] text-accent" title="Watched">
          ✓
        </span>
      )}
      {unwatched > 0 && (
        <span className="absolute top-1.5 right-1.5 z-10 min-w-5 rounded bg-accent px-1 text-center text-[11px] font-bold text-black" title={`${unwatched} unwatched`}>
          {unwatched}
        </span>
      )}
    </>
  );
}
