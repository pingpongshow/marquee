import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { EyeOff, Film, MoreHorizontal, Tv } from "lucide-react";
import { useState } from "react";
import { Menu, MenuItem } from "@/components/Menu";
import { toast } from "@/components/Toast";
import { formatDuration } from "@/features/browse/format";
import { Poster } from "@/features/browse/Poster";
import { itemVodSimilarQuery, useHideVodSuggestion, vodStatusQuery, vodSuggestionsQuery, type VodSuggestion } from "./vodApi";

// Suggestions from the films and series on demand: Home's Watch on VOD row, Also on VOD on
// a movie's or show's page, the on-demand pages' Recommended for you, and Muse's on-demand
// matches. Each tile opens the film's or show's page (play from there) and shows why it's
// suggested; its "…" menu has Not interested.

function posterFor(s: VodSuggestion) {
  if (s.movie) {
    const m = s.movie;
    return {
      title: m.title,
      type: "movie" as const,
      available: true,
      images: {},
      viewOffsetMs: m.positionMs,
      durationMs: m.runtimeMs,
      viewCount: m.watched ? 1 : 0,
    };
  }
  const m = s.series;
  return {
    title: m?.title ?? "",
    type: "show" as const,
    available: true,
    images: {},
    viewCount: m?.watched ? 1 : 0,
  };
}

/** One suggestion: the poster, the title and the reason (the whole reason in a tooltip). */
export function VodSuggestionTile({ s, className = "w-36 shrink-0" }: { s: VodSuggestion; className?: string }) {
  const hide = useHideVodSuggestion();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const m = s.movie;
  const sh = s.series;
  const title = m?.title ?? sh?.title ?? "";
  const facts = m
    ? [m.year, formatDuration(m.runtimeMs)].filter(Boolean).join(" · ")
    : [sh?.year, sh?.seasonCount ? `${sh.seasonCount} ${sh.seasonCount === 1 ? "season" : "seasons"}` : ""].filter(Boolean).join(" · ");
  const body = (
    <>
      <Poster item={posterFor(s)} src={m?.posterUrl ?? sh?.posterUrl} width={180} className="group-hover:ring-2 group-hover:ring-accent" />
      <div className="mt-2 flex items-center gap-1.5 truncate text-sm font-medium" title={title}>
        {m ? <Film className="size-3.5 shrink-0 text-faint" aria-hidden /> : <Tv className="size-3.5 shrink-0 text-faint" aria-hidden />}
        <span className="truncate">{title}</span>
      </div>
      <div className="truncate text-xs text-muted" title={s.reason ? `${s.reason}${facts ? ` · ${facts}` : ""}` : facts}>
        {s.reason || facts || "VOD"}
      </div>
    </>
  );
  return (
    <li
      className={clsx("group/tile relative", className)}
      onContextMenu={(ev) => {
        ev.preventDefault();
        setOpen(true);
      }}
    >
      {m ? (
        <Link to="/vod/$movieId" params={{ movieId: String(m.id) }} className="group block">
          {body}
        </Link>
      ) : sh ? (
        <Link to="/vod/shows/$seriesId" params={{ seriesId: String(sh.id) }} className="group block">
          {body}
        </Link>
      ) : null}
      <div
        className={clsx(
          "absolute top-1.5 right-1.5 z-20 rounded-full bg-black/70 text-white transition-opacity [&>div>button]:text-white",
          open ? "opacity-100" : "opacity-0 group-hover/tile:opacity-100 focus-within:opacity-100",
        )}
      >
        <Menu label={`More actions for ${title}`} trigger={<MoreHorizontal className="size-4" />} open={open} onOpenChange={setOpen} floating>
          <MenuItem
            icon={<EyeOff />}
            onClick={() =>
              hide.mutate(s, {
                onSuccess: () => toast(`You won't be offered “${title}” again.`),
                onError: (e) => toast(e.message, { tone: "error" }),
              })
            }
          >
            Not interested
          </MenuItem>
          <MenuItem
            icon={m ? <Film /> : <Tv />}
            onClick={() =>
              m
                ? navigate({ to: "/vod/$movieId", params: { movieId: String(m.id) } })
                : navigate({ to: "/vod/shows/$seriesId", params: { seriesId: String(sh?.id ?? 0) } })
            }
          >
            {m ? "Go to film" : "Go to show"}
          </MenuItem>
        </Menu>
      </div>
    </li>
  );
}

/** A row of suggestions (nothing when empty). */
export function VodSuggestionRow({ title, list, className }: { title: string; list?: VodSuggestion[]; className?: string }) {
  if (!list?.length) return null;
  return (
    <section className={className}>
      <h2 className="mb-3 text-lg font-semibold">{title}</h2>
      <ul className="flex gap-4 overflow-x-auto pb-2">
        {list.map((s) => (
          <VodSuggestionTile key={s.key} s={s} />
        ))}
      </ul>
    </section>
  );
}

/** Recommended for you, at the top of VOD Movies or VOD Shows. */
export function RecommendedRow({ kind }: { kind: "movie" | "series" }) {
  const q = useQuery(vodSuggestionsQuery(kind, 20));
  return <VodSuggestionRow title="Recommended for you" list={q.data} />;
}

/** Also on VOD: films (for a movie) or series (for a show) on demand like this one. */
export function AlsoOnVod({ itemId, type }: { itemId: number; type: string }) {
  const status = useQuery(vodStatusQuery);
  const q = useQuery({ ...itemVodSimilarQuery(itemId), enabled: !!status.data?.enabled && (type === "movie" || type === "show") });
  return <VodSuggestionRow title="Also on VOD" list={q.data} className="mt-10" />;
}
