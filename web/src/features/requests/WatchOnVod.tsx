import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Play } from "lucide-react";
import type { DiscoverItem } from "./api";

/** Watch now on VOD: the same title on demand (a film's or show's page). */
export function WatchOnVod({ item, size = "md" }: { item: DiscoverItem; size?: "xs" | "sm" | "md" }) {
  const v = item.vod;
  if (!v) return null;
  const cls = clsx(
    "inline-flex items-center gap-1.5 bg-accent font-medium text-black hover:brightness-110",
    size === "xs" ? "justify-center rounded px-2 py-0.5 text-[11px]" : size === "sm" ? "rounded-md px-2 py-1 text-xs" : "rounded-md px-4 py-2 text-sm",
  );
  const label = size === "md" ? "Watch now on VOD" : "VOD";
  return v.kind === "movie" ? (
    <Link to="/vod/$movieId" params={{ movieId: String(v.id) }} className={cls} title="Watch now on VOD">
      <Play className={size === "xs" ? "size-3" : "size-3.5"} aria-hidden /> {label}
    </Link>
  ) : (
    <Link to="/vod/shows/$seriesId" params={{ seriesId: String(v.id) }} className={cls} title="Watch now on VOD">
      <Play className={size === "xs" ? "size-3" : "size-3.5"} aria-hidden /> {label}
    </Link>
  );
}
