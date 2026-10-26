import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Check, ChevronLeft, ChevronRight, EyeOff, Filter, MoreHorizontal, Play, RotateCcw, Search, X } from "lucide-react";
import { clsx } from "clsx";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import type { ContinueEntry } from "@/api/types";
import { Menu, MenuItem } from "@/components/Menu";
import { useRemoveVodFromContinue } from "@/features/home/ContinueWatching";
import { Alert, Button, Input, Select, Spinner } from "@/components/ui";
import { formatDuration, formatTrackTime } from "@/features/browse/format";
import { Poster } from "@/features/browse/Poster";
import { deviceProfile } from "@/features/player/deviceProfile";
import {
  type VodFilters,
  type VodMovie,
  type VodSeries,
  type VodSort,
  saveActive,
  useVodWatched,
  vodCategoriesQuery,
  vodContinueQuery,
  vodGenresQuery,
  vodMovieQuery,
  vodMoviesQuery,
  vodSeriesCategoriesQuery,
  vodSeriesContinueQuery,
  vodSeriesGenresQuery,
  vodSeriesListQuery,
  vodFollowsQuery,
  vodStatusQuery,
} from "./vodApi";
import { SaveMovieButton, SavesLink } from "./VodSaves";
import { RecommendedRow } from "./VodSuggestions";

// Movies and shows on demand (LIVE-6): two pages, VOD Movies (/vod) and VOD Shows
// (/vod/shows), each browsed by category, search, sort and filters; a film's page; and the
// player both use.

export function stored(k: string) {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
}
export function store(k: string, v: string) {
  try {
    localStorage.setItem(k, v);
  } catch {
    /* private mode */
  }
}

/** A VOD film shaped for the library's Poster (artwork by URL, resume bar, watched tick). */
function posterItem(m: VodMovie) {
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

/** A show shaped for the library's Poster (watched tick once every episode is watched). */
export function showPosterItem(m: VodSeries) {
  return {
    title: m.title,
    type: "show" as const,
    available: true,
    images: {},
    viewCount: m.watched ? 1 : 0,
    leafCount: m.episodeCount || undefined,
    watchedLeafCount: m.episodeCount ? m.watchedCount : undefined,
  };
}

/** A tile's poster: a green ring when the library already has the title (with a tooltip
 * and a label for screen readers), else the accent ring on hover. */
function TilePoster({ item, src, inLibrary }: { item: Parameters<typeof Poster>[0]["item"]; src?: string; inLibrary?: boolean }) {
  return (
    <div title={inLibrary ? "In your library" : undefined}>
      <Poster
        item={item}
        src={src}
        className={clsx("transition-transform group-hover:scale-[1.03]", inLibrary ? "ring-2 ring-success" : "group-hover:ring-2 group-hover:ring-accent")}
      />
      {inLibrary && <span className="sr-only">In your library</span>}
    </div>
  );
}

function VodTile({ movie: m }: { movie: VodMovie }) {
  return (
    <Link to="/vod/$movieId" params={{ movieId: String(m.id) }} className="group block">
      <TilePoster item={posterItem(m)} src={m.posterUrl} inLibrary={m.inLibrary} />
      <div className="mt-2 truncate text-sm font-medium" title={m.title}>
        {m.title}
      </div>
      <div className="truncate text-xs text-muted">
        {[m.year, formatDuration(m.runtimeMs)].filter(Boolean).join(" · ")}
      </div>
    </Link>
  );
}

function ShowTile({ show: m }: { show: VodSeries }) {
  const next = m.nextEpisode;
  const sub = next
    ? `S${next.season} · E${next.episode}${next.positionMs ? " · resume" : ""}`
    : [m.year, m.seasonCount ? `${m.seasonCount} ${m.seasonCount === 1 ? "season" : "seasons"}` : ""]
        .filter(Boolean)
        .join(" · ");
  return (
    <Link to="/vod/shows/$seriesId" params={{ seriesId: String(m.id) }} className="group block">
      <TilePoster item={showPosterItem(m)} src={m.posterUrl} inLibrary={m.inLibrary} />
      <div className="mt-2 truncate text-sm font-medium" title={m.title}>
        {m.title}
      </div>
      <div className="truncate text-xs text-muted">{sub}</div>
    </Link>
  );
}

/** The VOD Movies page (LIVE-6). */
export function VodPage() {
  return (
    <div className="space-y-4 p-4 lg:p-6">
      <div className="flex items-center gap-4">
        <h1 className="text-2xl font-bold">VOD Movies</h1>
        <SavesLink />
      </div>
      <VodBrowse kind="movies" />
    </div>
  );
}

/** The VOD Shows page: series from Xtream Codes sources. */
export function VodShowsPage() {
  return (
    <div className="space-y-4 p-4 lg:p-6">
      <div className="flex items-center gap-4">
        <h1 className="text-2xl font-bold">VOD Shows</h1>
        <SavesLink />
      </div>
      <VodBrowse kind="shows" />
    </div>
  );
}

function Chip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <span className="flex items-center gap-1 rounded-full bg-accent/15 py-0.5 pr-1 pl-3 text-xs text-accent">
      {label}
      <button onClick={onClear} className="rounded-full p-0.5 hover:bg-accent/20" aria-label={`Remove filter ${label}`}>
        <X className="size-3" />
      </button>
    </span>
  );
}

const DECADES = [2020, 2010, 2000, 1990, 1980, 1970, 1960, 1950, 1940, 1930];
const WATCHED_LABELS: Record<NonNullable<VodFilters["watched"]>, string> = {
  unwatched: "Unwatched",
  inProgress: "In progress",
  watched: "Watched",
};

function readFilters(key: string): VodFilters {
  try {
    const v = JSON.parse(stored(key) ?? "{}") as VodFilters;
    return v && typeof v === "object" ? v : {};
  } catch {
    return {};
  }
}

/** A tile in a VOD page's Continue Watching row, with a "…" menu (hover, or right-click) to
 * remove it, as on Home. For a show, removing hides the show until it's watched again. */
function ResumeTile({ title, entry, children }: { title: string; entry?: ContinueEntry; children: ReactNode }) {
  const remove = useRemoveVodFromContinue();
  const [open, setOpen] = useState(false);
  return (
    <li
      className="group/tile relative w-36 shrink-0"
      onContextMenu={(ev) => {
        if (!entry) return;
        ev.preventDefault();
        setOpen(true);
      }}
    >
      {children}
      {entry && (
        <div
          className={clsx(
            "absolute top-1.5 right-1.5 z-20 rounded-full bg-black/70 text-white transition-opacity [&>div>button]:text-white",
            open ? "opacity-100" : "opacity-0 group-hover/tile:opacity-100 focus-within:opacity-100",
          )}
        >
          <Menu label={`More actions for ${title}`} trigger={<MoreHorizontal className="size-4" />} open={open} onOpenChange={setOpen} floating>
            <MenuItem icon={<EyeOff />} onClick={() => remove.mutate(entry)}>
              Remove from Continue Watching
            </MenuItem>
          </Menu>
        </div>
      )}
    </li>
  );
}

/** Browsing films or shows: categories, search, sort, filters, Continue Watching and a poster grid. */
export function VodBrowse({ kind }: { kind: "movies" | "shows" }) {
  const shows = kind === "shows";
  // Remembered per page (the shows' keys are their own).
  const k = (name: string) => (shows ? `marquee.vod.shows.${name}` : `marquee.vod.${name}`);
  const [chosenCategory, setCategory] = useState(() => stored(k("category")) ?? "");
  const [sort, setSort] = useState<VodSort>(() => (stored(k("sort")) as VodSort | null) ?? "title");
  const [filters, setFilters] = useState<VodFilters>(() => readFilters(k("filters")));
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [text, setText] = useState("");
  const [q, setQ] = useState("");
  useEffect(() => store(k("category"), chosenCategory), [chosenCategory]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => store(k("sort"), sort), [sort]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => store(k("filters"), JSON.stringify(filters)), [filters]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const t = setTimeout(() => setQ(text.trim()), 250);
    return () => clearTimeout(t);
  }, [text]);
  const set = (patch: Partial<VodFilters>) => setFilters((f) => ({ ...f, ...patch }));

  const categories = useQuery(shows ? vodSeriesCategoriesQuery : vodCategoriesQuery);
  // A remembered category that no longer exists would leave an empty grid under a select
  // showing "All": it means all.
  const category =
    chosenCategory && categories.data && !categories.data.some((c) => c.name === chosenCategory) ? "" : chosenCategory;
  const genres = useQuery(shows ? vodSeriesGenresQuery : vodGenresQuery);
  const status = useQuery(vodStatusQuery);
  const sources = (status.data?.sources ?? []).filter((s) => (shows ? s.series > 0 : s.movies > 0));
  const movieResume = useQuery({ ...vodContinueQuery, enabled: !shows });
  const showResume = useQuery({ ...vodSeriesContinueQuery, enabled: shows });
  const movies = useInfiniteQuery({ ...vodMoviesQuery(category, q, sort, filters), enabled: !shows });
  const series = useInfiniteQuery({ ...vodSeriesListQuery(category, q, sort, filters), enabled: shows });
  const list = shows ? series : movies;
  const items: (VodMovie | VodSeries)[] = shows
    ? (series.data?.pages.flatMap((p) => p.series) ?? [])
    : (movies.data?.pages.flatMap((p) => p.movies) ?? []);
  const total = shows ? series.data?.pages[0]?.total : movies.data?.pages[0]?.total;
  const all = categories.data?.reduce((n, c) => n + c.count, 0);
  const noun = shows ? "shows" : "movies";
  const filtered = !!(filters.genre || filters.decade || filters.minRating || filters.watched || filters.sourceId || filters.inLibrary);
  const sourceName = sources.find((s) => s.id === filters.sourceId)?.name ?? filters.sourceId;

  // Load the next page as the end of the grid comes into view.
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = list;
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasNextPage) return;
    const io = new IntersectionObserver(
      ([e]) => e?.isIntersecting && !isFetchingNextPage && void fetchNextPage(),
      { rootMargin: "800px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const resumeMovies = movieResume.data ?? [];
  const resumeShows = showResume.data ?? [];
  const showResumeRow = !category && !q && !filtered && (shows ? resumeShows.length : resumeMovies.length) > 0;

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <div className="w-full sm:w-56">
            <Select value={category} onChange={(e) => setCategory(e.target.value)} aria-label="Category">
              <option value="">
                All {noun}
                {all != null ? ` (${all})` : ""}
              </option>
              {categories.data?.map((c) => (
                <option key={c.name} value={c.name}>
                  {c.name} ({c.count})
                </option>
              ))}
            </Select>
          </div>
          <div className="relative w-full sm:w-64">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-faint" aria-hidden />
            <Input
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={`Search ${noun}`}
              aria-label={`Search ${noun}`}
              className="pl-9"
            />
          </div>
          <div className="flex w-full items-center gap-2 sm:ml-auto sm:w-auto">
            <Button
              size="sm"
              variant={filtersOpen || filtered ? "primary" : "secondary"}
              onClick={() => setFiltersOpen((v) => !v)}
              aria-expanded={filtersOpen}
            >
              <Filter className="size-4" /> Filter
            </Button>
            <div className="w-full sm:w-44">
              <Select value={sort} onChange={(e) => setSort(e.target.value as VodSort)} aria-label="Sort">
                <option value="title">Title</option>
                <option value="added">Recently added</option>
                <option value="year">Release year</option>
                <option value="rating">Rating</option>
              </Select>
            </div>
          </div>
        </div>
        {filtersOpen && (
          <div className="rounded-lg border border-border bg-surface p-4">
            <div className={clsx("grid gap-3 sm:grid-cols-2", sources.length > 1 ? "lg:grid-cols-6" : "lg:grid-cols-5")}>
              <Select
                aria-label="Watched state"
                value={filters.watched ?? ""}
                onChange={(e) => set({ watched: (e.target.value || undefined) as VodFilters["watched"] })}
              >
                <option value="">Watched or not</option>
                <option value="unwatched">{shows ? "Unwatched episodes" : "Unwatched"}</option>
                <option value="inProgress">In progress</option>
                <option value="watched">Watched</option>
              </Select>
              <Select aria-label="Genre" value={filters.genre ?? ""} onChange={(e) => set({ genre: e.target.value || undefined })}>
                <option value="">All genres</option>
                {genres.data?.map((g) => (
                  <option key={g.name} value={g.name}>
                    {g.name} ({g.count})
                  </option>
                ))}
              </Select>
              <Select
                aria-label="Decade"
                value={filters.decade ?? ""}
                onChange={(e) => set({ decade: e.target.value ? Number(e.target.value) : undefined })}
              >
                <option value="">Any decade</option>
                {DECADES.map((d) => (
                  <option key={d} value={d}>
                    {d}s
                  </option>
                ))}
              </Select>
              <Select
                aria-label="Minimum rating"
                value={filters.minRating ?? ""}
                onChange={(e) => set({ minRating: e.target.value ? Number(e.target.value) : undefined })}
              >
                <option value="">Any rating</option>
                {[9, 8, 7, 6, 5].map((r) => (
                  <option key={r} value={r}>
                    Rated {r}+
                  </option>
                ))}
              </Select>
              <Select
                aria-label="In my library"
                value={filters.inLibrary ?? ""}
                onChange={(e) => set({ inLibrary: (e.target.value || undefined) as VodFilters["inLibrary"] })}
              >
                <option value="">In my library: any</option>
                <option value="yes">In my library: yes</option>
                <option value="no">In my library: no</option>
              </Select>
              {sources.length > 1 && (
                <Select aria-label="Source" value={filters.sourceId ?? ""} onChange={(e) => set({ sourceId: e.target.value || undefined })}>
                  <option value="">All sources</option>
                  {sources.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              )}
            </div>
          </div>
        )}
        {filtered && (
          <div className="flex flex-wrap items-center gap-2">
            {filters.watched && (
              <Chip label={WATCHED_LABELS[filters.watched]} onClear={() => set({ watched: undefined })} />
            )}
            {filters.genre && <Chip label={filters.genre} onClear={() => set({ genre: undefined })} />}
            {filters.decade && <Chip label={`${filters.decade}s`} onClear={() => set({ decade: undefined })} />}
            {filters.minRating && <Chip label={`Rated ${filters.minRating}+`} onClear={() => set({ minRating: undefined })} />}
            {filters.sourceId && <Chip label={sourceName ?? "Source"} onClear={() => set({ sourceId: undefined })} />}
            {filters.inLibrary && (
              <Chip label={filters.inLibrary === "yes" ? "In my library" : "Not in my library"} onClear={() => set({ inLibrary: undefined })} />
            )}
            <button onClick={() => setFilters({})} className="text-xs text-muted hover:text-text">
              Clear all
            </button>
          </div>
        )}
      </div>

      {!category && !q && !filtered && <RecommendedRow kind={shows ? "series" : "movie"} />}

      {shows && !category && !q && !filtered && <FollowingRow />}

      {showResumeRow && (
        <section>
          <h2 className="mb-3 text-lg font-semibold">Continue Watching</h2>
          <ul className="flex gap-4 overflow-x-auto pb-2">
            {shows
              ? resumeShows.map((m) => (
                  <ResumeTile key={m.id} title={m.title} entry={m.nextEpisode ? ({ key: `vodEpisode:${m.nextEpisode.id}`, vodEpisode: m.nextEpisode } as ContinueEntry) : undefined}>
                    <ShowTile show={m} />
                  </ResumeTile>
                ))
              : resumeMovies.map((m) => (
                  <ResumeTile key={m.id} title={m.title} entry={{ key: `vodMovie:${m.id}`, vodMovie: m } as ContinueEntry}>
                    <VodTile movie={m} />
                  </ResumeTile>
                ))}
          </ul>
        </section>
      )}

      <section>
        {(category || q || filtered) && total != null && (
          <p className="mb-3 text-sm text-muted">
            {total} {total === 1 ? noun.slice(0, -1) : noun}
          </p>
        )}
        {list.isPending ? (
          <Spinner />
        ) : list.isError ? (
          <Alert tone="error">{list.error.message}</Alert>
        ) : !items.length ? (
          <p className="text-muted">
            {q ? `No ${noun} match “${q}”.` : filtered ? "Nothing matches these filters." : `No ${noun} here yet.`}
          </p>
        ) : (
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4">
            {items.map((m) => (
              <li key={m.id}>{shows ? <ShowTile show={m as VodSeries} /> : <VodTile movie={m as VodMovie} />}</li>
            ))}
          </ul>
        )}
        <div ref={sentinel} />
        {isFetchingNextPage && <Spinner />}
      </section>
    </div>
  );
}

/** VOD Shows: the shows the person follows (new episodes saved automatically), with what
 * each saved and why it stopped, if it has. */
function FollowingRow() {
  const follows = useQuery(vodFollowsQuery());
  const list = follows.data ?? [];
  if (list.length === 0) return null;
  return (
    <section data-testid="vod-following">
      <h2 className="mb-3 text-lg font-semibold">Following</h2>
      <ul className="flex gap-4 overflow-x-auto pb-2">
        {list.map((f) => (
          <li key={f.seriesId} className="w-36 shrink-0">
            <Link to="/vod/shows/$seriesId" params={{ seriesId: String(f.seriesId) }} className="group block">
              <TilePoster item={{ title: f.title, type: "show" as const, available: true, images: {} }} src={f.posterUrl} />
              <div className="mt-2 truncate text-sm font-medium" title={f.title}>
                {f.title}
              </div>
              <div className={clsx("truncate text-xs", f.status === "following" ? "text-muted" : "text-amber-500")} title={f.statusText}>
                {f.statusText || `${f.episodesSaved} saved${f.episodesQueued ? ` · ${f.episodesQueued} saving` : ""}`}
              </div>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** A film's page: backdrop, poster, details from TMDB (or the source), and Play. */
export function VodMoviePage({ movieId }: { movieId: number }) {
  // While it's being saved, the button's state follows along.
  const movie = useQuery({
    ...vodMovieQuery(movieId),
    // Also while the copy's quality is being checked, to compare it with the library's.
    refetchInterval: (q) =>
      saveActive(q.state.data?.save?.state) ? 2000 : q.state.data?.library?.pending ? 4000 : false,
  });
  const watched = useVodWatched(movieId);
  const navigate = useNavigate();
  if (movie.isPending) return <Spinner />;
  if (movie.isError)
    return (
      <div className="p-8">
        <Alert tone="error">{movie.error.message}</Alert>
      </div>
    );
  const m = movie.data;
  const play = (t?: number) =>
    navigate({ to: "/vod/$movieId/play", params: { movieId: String(m.id) }, search: { t } });
  const resume = (m.positionMs ?? 0) > 0;
  const isWatched = watched.isPending ? !!watched.variables : m.watched;
  const meta = [m.year ? String(m.year) : "", m.contentRating ?? "", formatDuration(m.runtimeMs)].filter(Boolean);

  return (
    <div className="relative">
      {m.backdropUrl && (
        <div className="pointer-events-none absolute inset-x-0 top-0 h-[28rem] overflow-hidden" aria-hidden>
          <img src={m.backdropUrl} alt="" className="size-full object-cover object-top opacity-35" />
          <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/60 to-transparent" />
          <div className="absolute inset-0 bg-gradient-to-r from-bg/80 to-transparent" />
        </div>
      )}
      <div className="relative p-6 lg:p-8">
        <div className="flex flex-col gap-8 sm:flex-row">
          <div className="w-44 shrink-0 sm:w-56">
            <Poster item={posterItem(m)} src={m.posterUrl} />
          </div>
          <div className="min-w-0 flex-1">
            <nav aria-label="Breadcrumb" className="mb-2 flex items-center gap-1 text-sm text-muted">
              <Link to="/vod" className="hover:text-text">
                VOD Movies
              </Link>
              {m.category && (
                <>
                  <ChevronRight className="size-4" aria-hidden />
                  <span>{m.category}</span>
                </>
              )}
            </nav>
            <h1 className="text-3xl font-bold">{m.title}</h1>
            {meta.length > 0 && <div className="mt-2 text-sm text-muted">{meta.join(" · ")}</div>}
            {m.genres.length > 0 && <div className="mt-1 text-sm text-faint">{m.genres.join(", ")}</div>}
            {m.rating != null && (
              <div className="mt-3 flex flex-wrap gap-2">
                <span title="TMDB user score" className="inline-flex items-center gap-1.5 rounded-md bg-surface-2/80 px-2 py-1 text-sm">
                  <span className="text-xs font-semibold text-muted">TMDB</span>
                  <span className="font-semibold">{m.rating.toFixed(1)}</span>
                </span>
              </div>
            )}
            <div className="mt-5 flex flex-wrap items-center gap-3">
              <Button variant="primary" onClick={() => play()}>
                <Play className="size-4 fill-current" /> {resume ? `Resume from ${formatTrackTime(m.positionMs)}` : "Play"}
              </Button>
              {resume && (
                <Button onClick={() => play(0)}>
                  <RotateCcw className="size-4" /> Play from start
                </Button>
              )}
              <Button
                variant="ghost"
                onClick={() => watched.mutate(!isWatched)}
                aria-pressed={isWatched}
                title={isWatched ? "Mark as unwatched" : "Mark as watched"}
              >
                <Check className={isWatched ? "size-4 text-success" : "size-4"} /> {isWatched ? "Watched" : "Mark watched"}
              </Button>
              {m.canSave && <SaveMovieButton movieId={m.id} save={m.save} library={m.library} targets={m} />}
            </div>
            {m.tagline && <p className="mt-4 text-lg text-muted italic">{m.tagline}</p>}
            {m.overview && <p className="mt-5 max-w-3xl leading-relaxed text-text/90">{m.overview}</p>}
            {!m.matched && <p className="mt-4 text-sm text-faint">Details from the source (not matched to TMDB).</p>}
          </div>
        </div>
        {(m.director || m.cast.length > 0) && (
          <section className="mt-10">
            {m.director && (
              <div className="mb-4 text-sm">
                <span className="text-muted">Directed by </span>
                <span className="font-medium">{m.director}</span>
              </div>
            )}
            {m.cast.length > 0 && (
              <>
                <h2 className="mb-3 text-lg font-semibold">Cast</h2>
                <ul className="grid grid-cols-2 gap-x-8 gap-y-2 text-sm sm:grid-cols-3 lg:grid-cols-4">
                  {m.cast.map((c) => (
                    <li key={c.name + (c.character ?? "")} className="min-w-0">
                      <div className="truncate font-medium">{c.name}</div>
                      {c.character && <div className="truncate text-muted">{c.character}</div>}
                    </li>
                  ))}
                </ul>
              </>
            )}
          </section>
        )}
      </div>
    </div>
  );
}

/** What VodVideo shows over the picture: the current time and length, for Up Next. */
export type VodOverlay = (time: number, duration: number) => ReactNode;

/**
 * Full-screen playback through the server (the source's own HLS, or a file made into HLS),
 * seekable, reporting progress against the session. Films and episodes both use it.
 */
export function VodVideo({
  sessionKey,
  start,
  startMs,
  title,
  subtitle,
  onBack,
  backLabel,
  onEnded,
  overlay,
}: {
  /** The film or episode; render with it as the key, so another one starts afresh. */
  sessionKey: string;
  start: () => Promise<{ id: string; url: string; startMs?: number }>;
  startMs?: number;
  title?: string;
  subtitle?: string;
  onBack: () => void;
  backLabel: string;
  /** Called after the last position is reported; defaults to onBack. */
  onEnded?: () => void;
  overlay?: VodOverlay;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const qc = useQueryClient();
  const ended = useRef(onEnded ?? onBack);
  useEffect(() => {
    ended.current = onEnded ?? onBack;
  });

  useEffect(() => {
    let hls: { destroy: () => void } | null = null;
    let session: string | null = null;
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;
    const v = video.current;
    const report = () => {
      if (!session || !v || v.currentTime <= 0) return Promise.resolve();
      return api
        .PATCH("/livetv/vod/sessions/{sessionId}", {
          params: { path: { sessionId: session } },
          body: {
            positionMs: Math.round(v.currentTime * 1000),
            durationMs: Number.isFinite(v.duration) ? Math.round(v.duration * 1000) : undefined,
          },
        })
        .catch(() => {});
    };
    const stop = (id: string) =>
      void api.DELETE("/livetv/vod/sessions/{sessionId}", { params: { path: { sessionId: id } } }).catch(() => {});
    const onPause = () => void report();
    let finished = false;
    const onEnd = () => {
      if (finished) return;
      finished = true;
      void report().then(() => ended.current());
    };
    // A file a little shorter than its stated length ends early on the server, and hls.js then
    // waits at the end for data that never comes: near the end, a stall that lasts is the end.
    let stall: ReturnType<typeof setTimeout> | undefined;
    const onWaiting = () => {
      if (!v) return;
      clearTimeout(stall);
      const at = v.currentTime;
      stall = setTimeout(() => {
        const left = v.duration - v.currentTime;
        if (!v.paused && Number.isFinite(left) && left < 60 && v.currentTime - at < 0.5) onEnd();
      }, 5000);
    };
    const onTime = () => {
      if (!v) return;
      setTime(v.currentTime);
      if (Number.isFinite(v.duration)) setDuration(v.duration);
    };
    v?.addEventListener("pause", onPause);
    v?.addEventListener("ended", onEnd);
    v?.addEventListener("waiting", onWaiting);
    v?.addEventListener("timeupdate", onTime);
    (async () => {
      // The player code loads while the session starts; if it fails to load, the session is ended.
      const hlsModule = import("hls.js");
      hlsModule.catch(() => {});
      const s = await start();
      let Hls: typeof import("hls.js").default;
      try {
        Hls = (await hlsModule).default;
      } catch (e) {
        stop(s.id);
        throw e;
      }
      if (cancelled) return stop(s.id);
      session = s.id;
      if (!v) return;
      const at = (startMs ?? s.startMs ?? 0) / 1000;
      if (Hls.isSupported()) {
        const h = new Hls({ startPosition: at, maxBufferLength: 30, backBufferLength: 60 });
        hls = h;
        h.loadSource(s.url);
        h.attachMedia(v);
        h.on(Hls.Events.MANIFEST_PARSED, () => void v.play().catch(() => {}));
        let recovered = false;
        h.on(Hls.Events.ERROR, (_, d) => {
          if (!d.fatal) return;
          if (!recovered && d.type === Hls.ErrorTypes.NETWORK_ERROR) {
            recovered = true;
            h.startLoad();
          } else if (!recovered && d.type === Hls.ErrorTypes.MEDIA_ERROR) {
            recovered = true;
            h.recoverMediaError();
          } else setError("Playback stopped.");
        });
      } else {
        v.src = s.url; // Safari plays HLS itself
        v.addEventListener(
          "loadedmetadata",
          () => {
            if (at) v.currentTime = at;
            void v.play().catch(() => {});
          },
          { once: true },
        );
      }
      timer = setInterval(() => !v.paused && void report(), 10_000);
    })()
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
      clearInterval(timer);
      clearTimeout(stall);
      v?.removeEventListener("pause", onPause);
      v?.removeEventListener("ended", onEnd);
      v?.removeEventListener("waiting", onWaiting);
      v?.removeEventListener("timeupdate", onTime);
      const id = session;
      // Record where they stopped before ending the session (the position is reported against it).
      void report().finally(() => {
        if (id) stop(id);
        void qc.invalidateQueries({ queryKey: ["vod"] });
      });
      hls?.destroy();
      if (v?.getAttribute("src")) {
        v.removeAttribute("src");
        v.load();
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionKey]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onBack();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  return (
    <div className="fixed inset-0 z-40 bg-black">
      <video ref={video} className="size-full object-contain" controls playsInline autoPlay aria-label={title ?? "Video"} />
      {loading && !error && <div className="absolute inset-0 grid place-items-center text-sm text-muted">Loading…</div>}
      <div className="absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent p-4">
        <button onClick={onBack} className="rounded-full bg-black/50 p-2" aria-label={backLabel}>
          <ChevronLeft className="size-5" />
        </button>
        <div className="min-w-0">
          <div className="truncate font-semibold">{title}</div>
          {subtitle && <div className="truncate text-xs text-muted">{subtitle}</div>}
        </div>
      </div>
      {error && <div className="absolute inset-x-0 bottom-24 text-center text-danger">{error}</div>}
      {overlay?.(time, duration)}
    </div>
  );
}

/** Full-screen playback of a film. */
export function VodPlayerPage({ movieId, startMs }: { movieId: number; startMs?: number }) {
  const movie = useQuery(vodMovieQuery(movieId));
  const navigate = useNavigate();
  const back = () => navigate({ to: "/vod/$movieId", params: { movieId: String(movieId) } });
  const m = movie.data;
  return (
    <VodVideo
      key={`movie-${movieId}`}
      sessionKey={`movie-${movieId}`}
      start={() =>
        unwrap(
          api.POST("/livetv/vod/movies/{movieId}/play", {
            params: { path: { movieId } },
            // Films that are files are made into HLS for this browser (copied when it plays the codec).
            body: { profile: deviceProfile() },
          }),
        )
      }
      startMs={startMs}
      title={m?.title ?? "Movie"}
      subtitle={m ? [m.year, formatDuration(m.runtimeMs)].filter(Boolean).join(" · ") : undefined}
      onBack={back}
      backLabel="Back to the movie"
    />
  );
}
