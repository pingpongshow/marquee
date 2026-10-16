import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Check, ChevronLeft, ChevronRight, Play, RotateCcw, Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Input, Select, Spinner } from "@/components/ui";
import { formatDuration, formatTrackTime } from "@/features/browse/format";
import { Poster } from "@/features/browse/Poster";
import { deviceProfile } from "@/features/player/deviceProfile";
import {
  type VodMovie,
  type VodSort,
  useVodWatched,
  vodCategoriesQuery,
  vodContinueQuery,
  vodMovieQuery,
  vodMoviesQuery,
} from "./vodApi";

// Movies on demand under Live TV (LIVE-6): browse by category, a film's page, and the player.

function stored(k: string) {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
}
function store(k: string, v: string) {
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

function VodTile({ movie: m }: { movie: VodMovie }) {
  return (
    <Link to="/vod/$movieId" params={{ movieId: String(m.id) }} className="group block">
      <Poster
        item={posterItem(m)}
        src={m.posterUrl}
        className="transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent"
      />
      <div className="mt-2 truncate text-sm font-medium" title={m.title}>
        {m.title}
      </div>
      <div className="truncate text-xs text-muted">
        {[m.year, formatDuration(m.runtimeMs)].filter(Boolean).join(" · ")}
      </div>
    </Link>
  );
}

/** The VOD page (LIVE-6). */
export function VodPage() {
  return (
    <div className="space-y-4 p-4 lg:p-6">
      <h1 className="text-2xl font-bold">VOD</h1>
      <VodBrowse />
    </div>
  );
}

/** VOD browsing: categories, search, sort, Continue Watching and a poster grid. */
export function VodBrowse() {
  const [category, setCategory] = useState(() => stored("marquee.vod.category") ?? "");
  const [sort, setSort] = useState<VodSort>(() => (stored("marquee.vod.sort") as VodSort | null) ?? "title");
  const [text, setText] = useState("");
  const [q, setQ] = useState("");
  useEffect(() => store("marquee.vod.category", category), [category]);
  useEffect(() => store("marquee.vod.sort", sort), [sort]);
  useEffect(() => {
    const t = setTimeout(() => setQ(text.trim()), 250);
    return () => clearTimeout(t);
  }, [text]);

  const categories = useQuery(vodCategoriesQuery);
  const resume = useQuery(vodContinueQuery);
  const movies = useInfiniteQuery(vodMoviesQuery(category, q, sort));
  const list = movies.data?.pages.flatMap((p) => p.movies) ?? [];
  const total = movies.data?.pages[0]?.total;
  const all = categories.data?.reduce((n, c) => n + c.count, 0);

  // Load the next page as the end of the grid comes into view.
  const sentinel = useRef<HTMLDivElement>(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = movies;
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

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        <div className="w-full sm:w-56">
          <Select value={category} onChange={(e) => setCategory(e.target.value)} aria-label="Category">
            <option value="">All movies{all != null ? ` (${all})` : ""}</option>
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
            placeholder="Search movies"
            aria-label="Search movies"
            className="pl-9"
          />
        </div>
        <div className="w-full sm:ml-auto sm:w-44">
          <Select value={sort} onChange={(e) => setSort(e.target.value as VodSort)} aria-label="Sort">
            <option value="title">Title</option>
            <option value="added">Recently added</option>
            <option value="year">Release year</option>
            <option value="rating">Rating</option>
          </Select>
        </div>
      </div>

      {!category && !q && !!resume.data?.length && (
        <section>
          <h2 className="mb-3 text-lg font-semibold">Continue Watching</h2>
          <ul className="flex gap-4 overflow-x-auto pb-2">
            {resume.data.map((m) => (
              <li key={m.id} className="w-36 shrink-0">
                <VodTile movie={m} />
              </li>
            ))}
          </ul>
        </section>
      )}

      <section>
        {(category || q) && total != null && (
          <p className="mb-3 text-sm text-muted">
            {total} {total === 1 ? "movie" : "movies"}
          </p>
        )}
        {movies.isPending ? (
          <Spinner />
        ) : movies.isError ? (
          <Alert tone="error">{movies.error.message}</Alert>
        ) : !list.length ? (
          <p className="text-muted">{q ? `No movies match “${q}”.` : "No movies here yet."}</p>
        ) : (
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4">
            {list.map((m) => (
              <li key={m.id}>
                <VodTile movie={m} />
              </li>
            ))}
          </ul>
        )}
        <div ref={sentinel} />
        {isFetchingNextPage && <Spinner />}
      </section>
    </div>
  );
}

/** A film's page: backdrop, poster, details from TMDB (or the source), and Play. */
export function VodMoviePage({ movieId }: { movieId: number }) {
  const movie = useQuery(vodMovieQuery(movieId));
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
                VOD
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

/** Full-screen playback of a film through the server (the source's own HLS, or a file made into HLS), seekable. */
export function VodPlayerPage({ movieId, startMs }: { movieId: number; startMs?: number }) {
  const movie = useQuery(vodMovieQuery(movieId));
  const video = useRef<HTMLVideoElement>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const back = () => navigate({ to: "/vod/$movieId", params: { movieId: String(movieId) } });

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
    const onEnded = () => void report().then(() => back());
    v?.addEventListener("pause", onPause);
    v?.addEventListener("ended", onEnded);
    (async () => {
      const [s, { default: Hls }] = await Promise.all([
        unwrap(
          api.POST("/livetv/vod/movies/{movieId}/play", {
            params: { path: { movieId } },
            // Films that are files are made into HLS for this browser (copied when it plays the codec).
            body: { profile: deviceProfile() },
          }),
        ),
        import("hls.js"),
      ]);
      if (cancelled) return stop(s.id);
      session = s.id;
      if (!v) return;
      const start = (startMs ?? s.startMs ?? 0) / 1000;
      if (Hls.isSupported()) {
        const h = new Hls({ startPosition: start, maxBufferLength: 30, backBufferLength: 60 });
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
          } else setError("The movie stopped playing.");
        });
      } else {
        v.src = s.url; // Safari plays HLS itself
        v.addEventListener(
          "loadedmetadata",
          () => {
            if (start) v.currentTime = start;
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
      v?.removeEventListener("pause", onPause);
      v?.removeEventListener("ended", onEnded);
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
  }, [movieId]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && back();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const m = movie.data;
  return (
    <div className="fixed inset-0 z-40 bg-black">
      <video ref={video} className="size-full object-contain" controls playsInline autoPlay aria-label={m?.title ?? "Movie"} />
      {loading && !error && <div className="absolute inset-0 grid place-items-center text-sm text-muted">Loading…</div>}
      <div className="absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent p-4">
        <button onClick={back} className="rounded-full bg-black/50 p-2" aria-label="Back to the movie">
          <ChevronLeft className="size-5" />
        </button>
        <div className="min-w-0">
          <div className="truncate font-semibold">{m?.title}</div>
          {m && <div className="truncate text-xs text-muted">{[m.year, formatDuration(m.runtimeMs)].filter(Boolean).join(" · ")}</div>}
        </div>
      </div>
      {error && <div className="absolute inset-x-0 bottom-24 text-center text-danger">{error}</div>}
    </div>
  );
}
