import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Bookmark, BookmarkCheck, Check, ChevronRight, Library, Play, RotateCcw, Tv } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api, unwrap } from "@/api/client";
import { toast } from "@/components/Toast";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import { formatDuration, formatTrackTime } from "@/features/browse/format";
import { Poster } from "@/features/browse/Poster";
import { deviceProfile } from "@/features/player/deviceProfile";
import { VodVideo, showPosterItem, store, stored } from "./Vod";
import { FollowControls } from "./VodFollow";
import { SaveEpisodeIcon, SaveSeriesButton } from "./VodSaves";
import {
  type VodEpisode,
  saveActive,
  useVodEpisodeWatched,
  useVodSeriesWatched,
  useVodWatchlist,
  vodSeriesQuery,
} from "./vodApi";

// VOD Shows (LIVE-6): a show's page with its seasons and episodes, and the episode player
// with Up Next at the end.

function epLabel(e: VodEpisode) {
  return `S${e.season} · E${e.episode}`;
}

/** A show's page: backdrop, poster, overview, a season picker and its episodes. */
export function VodShowPage({ seriesId }: { seriesId: number }) {
  // While episodes are being saved, their icons follow along.
  const show = useQuery({
    ...vodSeriesQuery(seriesId),
    // Also while episodes' qualities are being checked, to compare them with the library's.
    refetchInterval: (q) =>
      q.state.data?.episodes.some((e) => saveActive(e.save?.state))
        ? 3000
        : q.state.data?.library?.pending
          ? 5000
          : false,
  });
  const seriesWatched = useVodSeriesWatched(seriesId);
  const watchlist = useVodWatchlist();
  const episodeWatched = useVodEpisodeWatched();
  const navigate = useNavigate();
  const seasonKey = `marquee.vod.shows.season.${seriesId}`;
  const [season, setSeason] = useState<number | null>(() => {
    const v = stored(seasonKey);
    return v == null ? null : Number(v);
  });
  const m = show.data;
  // Open on the season of the next episode, unless the person picked one.
  const seasons = m?.seasons ?? [];
  const current = seasons.find((z) => z.season === season)?.season ?? m?.nextEpisode?.season ?? seasons[0]?.season;
  useEffect(() => {
    if (season != null) store(seasonKey, String(season));
  }, [season, seasonKey]);
  const episodes = useMemo(() => (m?.episodes ?? []).filter((e) => e.season === current), [m, current]);

  if (show.isPending) return <Spinner label="Loading the episodes" />;
  if (show.isError)
    return (
      <div className="p-8">
        <Alert tone="error">{show.error.message}</Alert>
      </div>
    );
  const s = show.data;
  // Optimistic while the request is in flight; otherwise whatever the server says.
  const listed = watchlist.isPending ? !!watchlist.variables?.on : !!s.watchlisted;
  const next = s.nextEpisode;
  // An episode the library has plays from the library (resuming there); others on demand.
  const play = (e: VodEpisode, t?: number) =>
    e.libraryItemId
      ? navigate({ to: "/play/$itemId", params: { itemId: String(e.libraryItemId) }, search: { t } })
      : navigate({
          to: "/vod/shows/$seriesId/play/$episodeId",
          params: { seriesId: String(s.id), episodeId: String(e.id) },
          search: { t },
        });
  const meta = [
    s.year ? String(s.year) : "",
    s.contentRating ?? "",
    s.seasonCount ? `${s.seasonCount} ${s.seasonCount === 1 ? "season" : "seasons"}` : "",
  ].filter(Boolean);
  const seasonInfo = seasons.find((z) => z.season === current);
  const seasonDone = !!seasonInfo && seasonInfo.episodeCount > 0 && seasonInfo.watchedCount >= seasonInfo.episodeCount;

  return (
    <div className="relative">
      {s.backdropUrl && (
        <div className="pointer-events-none absolute inset-x-0 top-0 h-[28rem] overflow-hidden" aria-hidden>
          <img src={s.backdropUrl} alt="" className="size-full object-cover object-top opacity-35" />
          <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/60 to-transparent" />
          <div className="absolute inset-0 bg-gradient-to-r from-bg/80 to-transparent" />
        </div>
      )}
      <div className="relative p-6 lg:p-8">
        <div className="flex flex-col gap-8 sm:flex-row">
          <div className="w-44 shrink-0 sm:w-56">
            <Poster item={showPosterItem(s)} src={s.posterUrl} />
          </div>
          <div className="min-w-0 flex-1">
            <nav aria-label="Breadcrumb" className="mb-2 flex items-center gap-1 text-sm text-muted">
              <Link to="/vod/shows" className="hover:text-text">
                VOD Shows
              </Link>
              {s.category && (
                <>
                  <ChevronRight className="size-4" aria-hidden />
                  <span>{s.category}</span>
                </>
              )}
            </nav>
            <h1 className="text-3xl font-bold">{s.title}</h1>
            {meta.length > 0 && <div className="mt-2 text-sm text-muted">{meta.join(" · ")}</div>}
            {s.genres.length > 0 && <div className="mt-1 text-sm text-faint">{s.genres.join(", ")}</div>}
            {s.rating != null && (
              <div className="mt-3 flex flex-wrap gap-2">
                <span className="inline-flex items-center gap-1.5 rounded-md bg-surface-2/80 px-2 py-1 text-sm">
                  <span className="text-xs font-semibold text-muted">{s.matched ? "TMDB" : "Rating"}</span>
                  <span className="font-semibold">{s.rating.toFixed(1)}</span>
                </span>
              </div>
            )}
            <div className="mt-5 flex flex-wrap items-center gap-3">
              {s.libraryItemId && (
                <Button variant="primary" onClick={() => navigate({ to: "/item/$itemId", params: { itemId: String(s.libraryItemId) } })}>
                  <Library className="size-4" /> Open in library
                </Button>
              )}
              {next && (
                <Button variant={s.libraryItemId ? "secondary" : "primary"} onClick={() => play(next)}>
                  <Play className="size-4 fill-current" />{" "}
                  {next.positionMs
                    ? `Resume ${epLabel(next)} from ${formatTrackTime(next.positionMs)}`
                    : `${s.watchedCount ? "Play" : "Start"} ${epLabel(next)}`}
                </Button>
              )}
              {next?.positionMs ? (
                <Button onClick={() => play(next, 0)}>
                  <RotateCcw className="size-4" /> Play from start
                </Button>
              ) : null}
              {s.episodeCount > 0 && (
                <Button
                  variant="ghost"
                  onClick={() => seriesWatched.mutate({ watched: !s.watched })}
                  aria-pressed={s.watched}
                  title={s.watched ? "Mark the show unwatched" : "Mark every episode watched"}
                >
                  <Check className={s.watched ? "size-4 text-success" : "size-4"} /> {s.watched ? "Watched" : "Mark watched"}
                </Button>
              )}
              <Button
                variant="ghost"
                onClick={() => watchlist.mutate({ kind: "series", id: s.id, on: !listed }, { onError: (e) => toast(e.message, { tone: "error" }) })}
                aria-pressed={listed}
              >
                {listed ? <BookmarkCheck className="size-4 text-accent" /> : <Bookmark className="size-4" />} {listed ? "On watchlist" : "Watchlist"}
              </Button>
              {s.canSave && <SaveSeriesButton seriesId={s.id} label="Save show" library={s.library} targets={s} />}
              <FollowControls show={s} />
            </div>
            {s.overview && <p className="mt-5 max-w-3xl leading-relaxed text-text/90">{s.overview}</p>}
            {s.cast.length > 0 && (
              <p className="mt-3 max-w-3xl text-sm text-muted">
                <span className="text-faint">Starring </span>
                {s.cast
                  .slice(0, 6)
                  .map((c) => c.name)
                  .join(", ")}
              </p>
            )}
          </div>
        </div>

        <section className="mt-10">
          {s.episodesError && <Alert tone="error">The episodes didn’t load: {s.episodesError}</Alert>}
          {seasons.length > 0 && (
            <div className="mb-4 flex flex-wrap items-center gap-3">
              <div className="w-48">
                <Select aria-label="Season" value={current ?? ""} onChange={(e) => setSeason(Number(e.target.value))}>
                  {seasons.map((z) => (
                    <option key={z.season} value={z.season}>
                      {z.name} ({z.episodeCount})
                    </option>
                  ))}
                </Select>
              </div>
              {seasonInfo && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => seriesWatched.mutate({ watched: !seasonDone, season: seasonInfo.season })}
                >
                  <Check className={seasonDone ? "size-4 text-success" : "size-4"} />{" "}
                  {seasonDone ? "Season watched" : "Mark season watched"}
                </Button>
              )}
              {seasonInfo && s.canSave && (
                <SaveSeriesButton
                  seriesId={s.id}
                  season={seasonInfo.season}
                  label="Save season"
                  library={seasonInfo.library}
                  targets={s}
                />
              )}
            </div>
          )}
          {seasonInfo?.overview && <p className="mb-4 max-w-3xl text-sm text-muted">{seasonInfo.overview}</p>}
          {!s.episodesError && episodes.length === 0 && <p className="text-muted">No episodes yet.</p>}
          <ul className="divide-y divide-border">
            {episodes.map((e) => {
              const pct = e.positionMs && e.runtimeMs ? Math.min(100, (e.positionMs / e.runtimeMs) * 100) : 0;
              const watched = episodeWatched.isPending && episodeWatched.variables?.id === e.id ? episodeWatched.variables.watched : e.watched;
              return (
                <li key={e.id} className="flex gap-4 py-3">
                  <button
                    onClick={() => play(e)}
                    className="group relative aspect-video w-40 shrink-0 overflow-hidden rounded-md bg-surface-2 sm:w-56"
                    aria-label={`Play ${epLabel(e)} ${e.title}`}
                  >
                    {e.stillUrl ? (
                      <img src={e.stillUrl} alt="" loading="lazy" className="size-full object-cover" />
                    ) : (
                      <div className="grid size-full place-items-center text-faint">
                        <Tv className="size-8" aria-hidden />
                      </div>
                    )}
                    <div className="absolute inset-0 grid place-items-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
                      <Play className="size-8 fill-current" />
                    </div>
                    {pct > 0 && (
                      <div className="absolute inset-x-0 bottom-0 h-1 bg-black/60">
                        <div className="h-full bg-accent" style={{ width: `${pct}%` }} />
                      </div>
                    )}
                  </button>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-start gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="font-medium">
                          {e.episode}. {e.title}
                        </div>
                        <div className="text-xs text-muted">
                          {[e.airDate, formatDuration(e.runtimeMs), e.libraryItemId ? "Plays from your library" : ""]
                            .filter(Boolean)
                            .join(" · ")}
                        </div>
                      </div>
                      {s.canSave && <SaveEpisodeIcon episodeId={e.id} save={e.save} library={e.library} targets={s} />}
                      <button
                        onClick={() => episodeWatched.mutate({ id: e.id, watched: !watched })}
                        className="rounded-full p-1.5 hover:bg-surface-2"
                        aria-pressed={watched}
                        aria-label={watched ? "Mark unwatched" : "Mark watched"}
                        title={watched ? "Mark unwatched" : "Mark watched"}
                      >
                        <Check className={watched ? "size-4 text-success" : "size-4 text-faint"} />
                      </button>
                    </div>
                    {e.overview && <p className="mt-1 line-clamp-3 text-sm text-text/80">{e.overview}</p>}
                  </div>
                </li>
              );
            })}
          </ul>
        </section>
      </div>
    </div>
  );
}

/** Seconds before the end at which Up Next shows; at the end it plays. */
const UP_NEXT_AT = 30;

/** Full-screen playback of an episode, with Up Next (a countdown) at the end. */
export function VodEpisodePlayerPage({ seriesId, episodeId, startMs }: { seriesId: number; episodeId: number; startMs?: number }) {
  const show = useQuery(vodSeriesQuery(seriesId));
  const navigate = useNavigate();
  const upNext = useQuery({
    queryKey: ["vod", "next-episode", episodeId],
    queryFn: () => unwrap(api.GET("/livetv/vod/episodes/{episodeId}/next", { params: { path: { episodeId } } })),
  });
  const next = upNext.data?.episode ?? null;
  const [dismissedFor, setDismissedFor] = useState<number | null>(null);
  const dismissed = dismissedFor === episodeId;
  const back = () => navigate({ to: "/vod/shows/$seriesId", params: { seriesId: String(seriesId) } });
  const playNext = () =>
    next &&
    navigate({
      to: "/vod/shows/$seriesId/play/$episodeId",
      params: { seriesId: String(seriesId), episodeId: String(next.id) },
      search: { t: undefined },
      replace: true,
    });
  const e = show.data?.episodes.find((x) => x.id === episodeId);
  return (
    <VodVideo
      key={`episode-${episodeId}`}
      sessionKey={`episode-${episodeId}`}
      start={() =>
        unwrap(
          api.POST("/livetv/vod/episodes/{episodeId}/play", {
            params: { path: { episodeId } },
            body: { profile: deviceProfile(), startMs },
          }),
        )
      }
      startMs={startMs}
      title={e ? `${e.seriesTitle} · ${epLabel(e)}` : (show.data?.title ?? "Episode")}
      subtitle={e ? [e.title, formatDuration(e.runtimeMs)].filter(Boolean).join(" · ") : undefined}
      onBack={back}
      backLabel="Back to the show"
      onEnded={() => (next && !dismissed ? playNext() : back())}
      overlay={(time, duration) =>
        next && !dismissed && duration > 0 && duration - time < UP_NEXT_AT ? (
          <div className="absolute right-8 bottom-28 w-80 overflow-hidden rounded-lg border border-white/15 bg-black/85 text-white shadow-2xl backdrop-blur">
            {next.stillUrl && <img src={next.stillUrl} alt="" className="aspect-video w-full object-cover" />}
            <div className="space-y-2 p-3">
              <div className="text-xs text-white/60 uppercase">Up next in {Math.max(0, Math.ceil(duration - time))}s</div>
              <div className="truncate font-semibold">
                {epLabel(next)} · {next.title}
              </div>
              <div className="flex gap-2">
                <button onClick={playNext} className="flex-1 rounded bg-accent px-3 py-1.5 font-semibold text-black">
                  Play now
                </button>
                <button onClick={() => setDismissedFor(episodeId)} className="rounded bg-white/10 px-3 py-1.5">
                  Hide
                </button>
              </div>
            </div>
          </div>
        ) : null
      }
    />
  );
}
