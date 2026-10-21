import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { EyeOff, Film, MoreHorizontal, Tv } from "lucide-react";
import { useState } from "react";
import { Menu, MenuItem } from "@/components/Menu";
import { toast } from "@/components/Toast";
import { formatDuration } from "@/features/browse/format";
import { Poster } from "@/features/browse/Poster";
import { Spinner } from "@/components/ui";
import { itemVodSimilarQuery, useHideVodSuggestion, vodRecommendationsQuery, vodStatusQuery, vodSuggestionsQuery, type VodSuggestion } from "./vodApi";

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
export function VodSuggestionTile({
  s,
  className = "w-36 shrink-0",
  badge,
}: {
  s: VodSuggestion;
  className?: string;
  /** Marks it as on demand, among library titles. */
  badge?: boolean;
}) {
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
      {badge && (
        <span className="pointer-events-none absolute top-1.5 left-1.5 z-10 rounded bg-black/75 px-1.5 py-0.5 text-[10px] font-bold tracking-wider text-accent shadow ring-1 ring-white/15 backdrop-blur-sm">
          VOD
        </span>
      )}
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
export function VodSuggestionRow({ title, list, className, kind }: { title: string; list?: VodSuggestion[]; className?: string; kind?: "movie" | "series" }) {
  if (!list?.length) return null;
  return (
    <section className={className}>
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <h2 className="text-lg font-semibold">{title}</h2>
        {kind && (
          <Link to="/vod/recommended" search={{ kind }} className="text-sm text-accent hover:underline">
            See all
          </Link>
        )}
      </div>
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
  return <VodSuggestionRow title="Recommended for you" list={q.data} kind={kind} />;
}

/** The recommendations page: many rows, each with its own angle, from the person's own taste. */
export function VodRecommendationsPage({ kind }: { kind: "movie" | "series" }) {
  const q = useQuery(vodRecommendationsQuery(kind));
  return (
    <div className="space-y-8 p-4 sm:p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">Recommended {kind === "series" ? "shows" : "movies"} on VOD</h1>
        <nav className="flex gap-1 rounded-lg border border-border p-1 text-sm" aria-label="Movies or shows">
          {(["movie", "series"] as const).map((k) => (
            <Link
              key={k}
              to="/vod/recommended"
              search={{ kind: k }}
              className={clsx("rounded-md px-3 py-1", k === kind ? "bg-accent/15 text-accent" : "text-muted hover:text-text")}
            >
              {k === "movie" ? "Movies" : "Shows"}
            </Link>
          ))}
        </nav>
      </header>
      <p className="text-sm text-muted">Picked from your favourites, ratings and what you watch. Nothing here comes from what other people watch.</p>
      {q.isPending && <Spinner />}
      {q.isError && <p className="text-danger">{q.error.message}</p>}
      {q.data?.length === 0 && <p className="text-muted">Nothing to recommend yet. Watch, rate or heart a few titles and come back.</p>}
      {q.data?.map((r) => (
        <VodSuggestionRow key={r.id} title={r.title} list={r.entries} />
      ))}
    </div>
  );
}

/** Also on VOD: films (for a movie) or series (for a show) on demand like this one. */
export function AlsoOnVod({ itemId, type }: { itemId: number; type: string }) {
  const status = useQuery(vodStatusQuery);
  const q = useQuery({ ...itemVodSimilarQuery(itemId), enabled: !!status.data?.enabled && (type === "movie" || type === "show") });
  return <VodSuggestionRow title="Also on VOD" list={q.data} className="mt-10" />;
}
