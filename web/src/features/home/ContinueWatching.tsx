import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronRight, EyeOff, Film, MoreHorizontal, Tv } from "lucide-react";
import { clsx } from "clsx";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { ContinueEntry, Hub, ItemSummary } from "@/api/types";
import { Menu, MenuItem } from "@/components/Menu";
import { Alert, Button, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";
import { useTileMenu } from "../browse/ItemMenu";
import { Poster } from "../browse/Poster";
import { subtitleFor } from "../browse/format";
import { hubsKey } from "../browse/watched";

// Continue Watching: library titles and films and episodes on demand together (Home's row,
// and a page with all of them).

/** A Home row tile for a library item. */
export function HubItem({
  it,
  wide,
  continueWatching,
  className,
}: {
  it: ItemSummary;
  wide: boolean;
  continueWatching: boolean;
  /** Replaces the tile's width (a grid's cell). */
  className?: string;
}) {
  // The "…" menu shows on hover, and right-click opens it too.
  const tile = useTileMenu(it, { continueWatching });
  // Continue Watching episodes open the player directly.
  const playable = (it.type === "episode" || it.type === "movie" || it.type === "video") && wide;
  const title = it.type === "episode" ? it.grandparentTitle : it.title;
  const sub =
    it.type === "episode"
      ? `${it.parentTitle ?? ""} · E${it.index ?? ""}${it.title ? ` · ${it.title}` : ""}`
      : subtitleFor(it);
  const body = (
    <>
      <Poster
        item={it}
        shape={wide ? "wide" : it.type === "album" || it.type === "artist" ? "square" : "poster"}
        width={wide ? 320 : 180}
        className="group-hover:ring-2 group-hover:ring-accent"
      />
      <div className="mt-2 truncate text-sm font-medium">{title}</div>
      <div className="truncate text-xs text-muted">{sub}</div>
    </>
  );
  return (
    <li
      className={clsx("group/tile relative", className ?? (wide ? "w-72 shrink-0" : "w-36 shrink-0"))}
      onContextMenu={tile.onContextMenu}
    >
      {playable ? (
        <Link to="/play/$itemId" params={{ itemId: String(it.id) }} search={{ t: undefined }} className="group block">
          {body}
        </Link>
      ) : (
        <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
          {body}
        </Link>
      )}
      {tile.menu}
    </li>
  );
}

/** Removes a film on demand, or an episode's series, from Continue Watching until it's watched again. */
export function useRemoveVodFromContinue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (e: ContinueEntry) =>
      unwrap(
        api.DELETE("/hubs/continue-watching/vod/{kind}/{vodId}", {
          params: {
            path: e.vodMovie ? { kind: "movie", vodId: e.vodMovie.id } : { kind: "episode", vodId: e.vodEpisode?.id ?? 0 },
          },
        }),
      ),
    onMutate: (e) => {
      const old = qc.getQueryData<Hub[]>(hubsKey);
      qc.setQueryData<Hub[]>(hubsKey, (h) =>
        h?.map((x) =>
          x.id === "continue-watching"
            ? { ...x, entries: x.entries?.filter((y) => y.key !== e.key), total: x.total ? x.total - 1 : x.total }
            : x,
        ),
      );
      return { old };
    },
    onError: (err, _e, ctx) => {
      qc.setQueryData(hubsKey, ctx?.old);
      toast(err.message, { tone: "error" });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: hubsKey });
      qc.invalidateQueries({ queryKey: ["continue-watching"] });
      qc.invalidateQueries({ queryKey: ["vod"] });
    },
  });
}

/** A film or episode on demand in Continue Watching: opens the player where it stopped. */
function VodTile({ e, className }: { e: ContinueEntry; className?: string }) {
  const remove = useRemoveVodFromContinue();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const m = e.vodMovie;
  const ep = e.vodEpisode;
  const title = m ? m.title : (ep?.seriesTitle ?? "");
  const sub = m
    ? ["VOD", m.year ? String(m.year) : ""].filter(Boolean).join(" · ")
    : ep
      ? `VOD · S${ep.season} · E${ep.episode}${ep.title ? ` · ${ep.title}` : ""}`
      : "";
  // Wide artwork: the film's backdrop (its poster's sibling), the episode's still.
  const art = m ? `/api/v1/livetv/vod/movies/${m.id}/image?kind=backdrop` : (ep?.stillUrl ?? e.seriesPosterUrl);
  const poster = { title, type: (m ? "movie" : "episode") as ItemSummary["type"], available: true, images: {},
    viewOffsetMs: e.positionMs, durationMs: e.durationMs || undefined };
  const body = (
    <>
      <Poster item={poster} src={art} shape="wide" width={320} className="group-hover:ring-2 group-hover:ring-accent" />
      <div className="mt-2 flex items-center gap-1.5 truncate text-sm font-medium">
        {m ? <Film className="size-3.5 shrink-0 text-faint" aria-hidden /> : <Tv className="size-3.5 shrink-0 text-faint" aria-hidden />}
        <span className="truncate">{title}</span>
      </div>
      <div className="truncate text-xs text-muted">{sub}</div>
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
        <Link to="/vod/$movieId/play" params={{ movieId: String(m.id) }} search={{ t: undefined }} className="group block">
          {body}
        </Link>
      ) : ep ? (
        <Link
          to="/vod/shows/$seriesId/play/$episodeId"
          params={{ seriesId: String(ep.seriesId), episodeId: String(ep.id) }}
          search={{ t: undefined }}
          className="group block"
        >
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
          <MenuItem icon={<EyeOff />} onClick={() => remove.mutate(e)}>
            Remove from Continue Watching
          </MenuItem>
          <MenuItem
            icon={m ? <Film /> : <Tv />}
            onClick={() =>
              m
                ? navigate({ to: "/vod/$movieId", params: { movieId: String(m.id) } })
                : navigate({ to: "/vod/shows/$seriesId", params: { seriesId: String(ep?.seriesId ?? 0) } })
            }
          >
            {m ? "Go to film" : "Go to show"}
          </MenuItem>
        </Menu>
      </div>
    </li>
  );
}

/** One Continue Watching entry: a library item or a film or episode on demand. */
export function ContinueTile({ e, className = "w-72 shrink-0" }: { e: ContinueEntry; className?: string }) {
  if (e.item) return <HubItem it={e.item} wide continueWatching className={className} />;
  return <VodTile e={e} className={className} />;
}

/** The row's last tile when there's more than it shows. */
export function ViewMoreTile({ total }: { total: number }) {
  return (
    <li className="w-48 shrink-0">
      <Link
        to="/continue-watching"
        className="flex aspect-video w-full flex-col items-center justify-center gap-1 rounded-md bg-surface-2 text-sm font-medium text-muted shadow-md hover:text-text hover:ring-2 hover:ring-accent"
      >
        View more
        <span className="text-xs text-faint">{total} in all</span>
        <ChevronRight className="size-5" aria-hidden />
      </Link>
    </li>
  );
}

const PAGE = 60;

/** Everything in Continue Watching, newest first, with remove on every tile. */
export function ContinueWatchingPage() {
  const q = useInfiniteQuery({
    queryKey: ["continue-watching"],
    queryFn: ({ pageParam }) =>
      unwrap(api.GET("/hubs/continue-watching", { params: { query: { offset: pageParam, limit: PAGE } } })),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.entries.length, 0);
      return loaded < last.total ? loaded : undefined;
    },
  });
  if (q.isPending) return <Spinner />;
  if (q.isError)
    return (
      <div className="p-8">
        <Alert tone="error">{q.error.message}</Alert>
      </div>
    );
  const entries = q.data.pages.flatMap((p) => p.entries);
  const total = q.data.pages[0]?.total ?? 0;
  return (
    <div className="space-y-6 p-6 lg:p-8">
      <div className="flex items-baseline gap-3">
        <h1 className="text-2xl font-bold">Continue Watching</h1>
        <span className="text-sm text-muted">{total}</span>
      </div>
      {entries.length === 0 ? (
        <p className="text-muted">Nothing in progress. Titles you stop part-way through show up here.</p>
      ) : (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-x-4 gap-y-6">
          {entries.map((e) => (
            <ContinueTile key={e.key} e={e} className="min-w-0" />
          ))}
        </ul>
      )}
      {q.hasNextPage && (
        <div className="flex justify-center">
          <Button onClick={() => q.fetchNextPage()} loading={q.isFetchingNextPage}>
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
