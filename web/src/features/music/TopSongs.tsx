import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { clsx } from "clsx";
import { api, unwrap } from "@/api/client";
import { itemQuery, topSongsQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Alert, Button, Spinner } from "@/components/ui";
import { RowFavorite } from "../browse/Favorite";
import { formatTrackTime } from "../browse/format";
import { ItemMenu } from "../browse/ItemMenu";
import { useMusicState } from "../player/MusicPlayer";
import { TrackRadioButton } from "./RadioButtons";
import { RowRating } from "./Rating";

/** How many an artist page shows. */
const SHOWN = 15;
const PAGE = 200;

const playSource = (artist?: string) => (artist ? `${artist} – Top Songs` : "Top Songs");

/** Songs in the list's order; a row plays the list from there. A phone keeps the title's room: no stars or time. */
function TopSongRows({ tracks, source }: { tracks: ItemSummary[]; source: string }) {
  const music = useMusicState();
  return (
    <ol className="divide-y divide-border rounded-lg border border-border bg-surface" data-testid="top-songs">
      {tracks.map((t, i) => (
        <li key={t.id} className={clsx("group flex items-center gap-3 pr-2 hover:bg-surface-2 sm:gap-4", music.current?.item.id === t.id && "text-accent")}>
          <button type="button" onClick={() => music.play(tracks, i, { source })} className="flex min-w-0 flex-1 items-center gap-3 py-3 pl-3 text-left sm:gap-4 sm:pl-4">
            <span className="w-6 shrink-0 text-right text-sm text-faint tabular-nums">{i + 1}</span>
            <span className="min-w-0 flex-1">
              <span className={clsx("block truncate", !t.available && "text-faint line-through")}>{t.title}</span>
              <span className="block truncate text-xs text-muted">{t.parentTitle}</span>
            </span>
          </button>
          <RowFavorite item={t} />
          <RowRating item={t} className="max-sm:hidden" />
          <TrackRadioButton item={t} />
          <span className="shrink-0 text-sm text-muted tabular-nums max-sm:hidden">{formatTrackTime(t.durationMs)}</span>
          <ItemMenu item={t} />
        </li>
      ))}
    </ol>
  );
}

/**
 * The top of an artist's page: this person's favourites, songs they rated 3.5 stars or more and
 * what's popular, best first (MUSIC-24). A longer list goes on to its own page.
 */
export function TopSongs({ artist }: { artist: ItemSummary }) {
  const top = useQuery(topSongsQuery(artist.id, SHOWN));
  const tracks = top.data?.items ?? [];
  if (!tracks.length) return null;
  return (
    <section className="mt-10">
      <div className="mb-4 flex items-baseline justify-between gap-3">
        <h2 className="text-lg font-semibold">Top Songs</h2>
        {(top.data?.total ?? 0) > SHOWN && (
          <Link to="/item/$itemId/top-songs" params={{ itemId: String(artist.id) }} className="text-sm text-muted hover:text-text">
            More
          </Link>
        )}
      </div>
      <TopSongRows tracks={tracks} source={playSource(artist.title)} />
    </section>
  );
}

/** An artist's whole Top Songs list. */
export function TopSongsPage() {
  const { itemId } = useParams({ from: "/item/$itemId/top-songs" });
  const id = Number(itemId);
  const artist = useQuery(itemQuery(id)).data;
  const q = useInfiniteQuery({
    queryKey: ["items", id, "top-songs", "all"],
    queryFn: ({ pageParam }) => unwrap(api.GET("/items/{itemId}/top-songs", { params: { path: { itemId: id }, query: { offset: pageParam, limit: PAGE } } })),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0);
      return last.items.length && loaded < last.total ? loaded : undefined;
    },
  });
  if (q.isPending) return <Spinner />;
  if (q.isError)
    return (
      <div className="p-8">
        <Alert tone="error">{q.error.message}</Alert>
      </div>
    );
  const tracks = q.data.pages.flatMap((p) => p.items);
  return (
    <div className="space-y-6 p-6 lg:p-8">
      <div>
        {artist && (
          <Link to="/item/$itemId" params={{ itemId }} className="text-sm text-muted hover:text-text">
            {artist.title}
          </Link>
        )}
        <div className="flex items-baseline gap-3">
          <h1 className="text-2xl font-bold">Top Songs</h1>
          <span className="text-sm text-muted">{q.data.pages[0]?.total ?? 0}</span>
        </div>
      </div>
      {tracks.length === 0 ? <p className="text-muted">No top songs yet. Songs you favourite or rate highly show up here.</p> : <TopSongRows tracks={tracks} source={playSource(artist?.title)} />}
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
