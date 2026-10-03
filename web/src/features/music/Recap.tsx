import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useRouter, useSearch } from "@tanstack/react-router";
import { clsx } from "clsx";
import { ChevronLeft, ChevronRight, ListPlus, Play, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import type { ItemSummary, ListeningRecap, RecapEntry } from "@/api/types";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";
import { useMusicActions } from "../player/MusicPlayer";
import { recapYearsQuery } from "./RecapCard";

const recapQuery = (year: number) => ({
  queryKey: ["recap", year],
  queryFn: () => unwrap(api.GET("/me/recap", { params: { query: { year } } })),
  staleTime: 5 * 60_000,
});

const nf = (n: number) => n.toLocaleString();

function artFor(it: ItemSummary) {
  return it.images?.poster ?? it.images?.thumb;
}

function Cover({ item, size, className, round }: { item: ItemSummary; size: number; className?: string; round?: boolean }) {
  const art = artFor(item);
  return (
    <div className={clsx("shrink-0 overflow-hidden bg-black/30 shadow-2xl", round ? "rounded-full" : "rounded-lg", className)}>
      {art ? (
        <img src={imageUrl(art, size)} alt="" className="size-full object-cover" />
      ) : (
        <div className="flex size-full items-center justify-center text-4xl font-black text-white/50">{item.title.slice(0, 1)}</div>
      )}
    </div>
  );
}

function Big({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={clsx("recap-in text-[clamp(3.5rem,14vw,9rem)] leading-none font-black tracking-tighter", className)}>{children}</div>;
}

function Kicker({ children }: { children: ReactNode }) {
  return <div className="recap-in mb-4 text-lg font-bold tracking-wide text-white/80 uppercase sm:text-xl">{children}</div>;
}

function hourLabel(h: number) {
  return h === 0 ? "midnight" : h === 12 ? "noon" : h < 12 ? `${h} am` : `${h - 12} pm`;
}

function listenerType(byHour: number[]) {
  const peak = byHour.reduce((best, v, i) => (v > (byHour[best] ?? 0) ? i : best), 0);
  if (peak >= 22 || peak < 4) return { peak, title: "Night owl", line: "The music really comes alive after dark." };
  if (peak < 9) return { peak, title: "Early riser", line: "You start the day with a soundtrack." };
  if (peak < 17) return { peak, title: "Daytime listener", line: "Music keeps your days moving." };
  return { peak, title: "Evening unwinder", line: "Evenings are for your favourite songs." };
}

const months = ["J", "F", "M", "A", "M", "J", "J", "A", "S", "O", "N", "D"];

function Bars({ values, labels, label, highlight }: { values: number[]; labels?: string[]; label: string; highlight?: number }) {
  const max = Math.max(1, ...values);
  return (
    <figure className="recap-in-late w-full">
      <div className="flex h-32 items-end gap-[3px] sm:h-40" role="img" aria-label={label}>
        {values.map((v, i) => (
          <div key={i} className="flex h-full flex-1 flex-col justify-end">
            <div
              className={clsx("min-h-[3px] rounded-t transition-[height] duration-700", i === highlight ? "bg-white" : "bg-white/45")}
              style={{ height: `${(v / max) * 100}%` }}
              title={`${labels?.[i] ?? i}: ${v}`}
            />
          </div>
        ))}
      </div>
      {labels && (
        <div className="mt-1 flex gap-[3px] text-center text-[10px] text-white/70">
          {labels.map((l, i) => (
            <span key={i} className="flex-1">
              {l}
            </span>
          ))}
        </div>
      )}
      <figcaption className="mt-2 text-sm text-white/70">{label}</figcaption>
    </figure>
  );
}

function EntryRow({ e, rank, onPlay }: { e: RecapEntry; rank: number; onPlay?: () => void }) {
  return (
    <li className="recap-in flex items-center gap-3 text-left sm:gap-4" style={{ animationDelay: `${rank * 70}ms` }}>
      <span className="w-8 text-right text-2xl font-black text-white/70 tabular-nums">{rank}</span>
      <Cover item={e.item} size={64} className="size-12 sm:size-14" round={e.item.type === "artist"} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-lg font-bold">{e.item.title}</div>
        <div className="truncate text-sm text-white/70">
          {e.item.type === "track" || e.item.type === "album" ? `${e.item.artistCredit ?? e.item.grandparentTitle ?? e.item.parentTitle ?? ""} · ` : ""}
          {nf(e.plays)} {e.plays === 1 ? "play" : "plays"}
        </div>
      </div>
      {onPlay && (
        <button onClick={onPlay} className="rounded-full bg-white p-2.5 text-black hover:bg-amber-200" aria-label={`Play ${e.item.title}`}>
          <Play className="size-4 fill-current" />
        </button>
      )}
    </li>
  );
}

type Page = { id: string; bg: string; body: ReactNode };

function buildPages(r: ListeningRecap, actions: { playTop: () => void; playOne: (t: ItemSummary) => void; save: () => void; saving: boolean }): Page[] {
  const top = r.topArtists[0];
  const hours = Math.round(r.minutes / 60);
  const lt = listenerType(r.byHour);
  const peakMonth = r.byMonth.reduce((best, v, i) => (v > (r.byMonth[best] ?? 0) ? i : best), 0);
  const monthName = new Date(2000, peakMonth, 1).toLocaleString(undefined, { month: "long" });
  const topGenre = r.topGenres[0];
  const genreMax = Math.max(1, ...r.topGenres.map((g) => g.plays));
  const pages: Page[] = [
    {
      id: "minutes",
      bg: "from-fuchsia-600 via-pink-600 to-orange-500",
      body: (
        <>
          <Kicker>In {r.year} you listened for</Kicker>
          <Big>{nf(r.minutes)}</Big>
          <div className="recap-in-late mt-3 text-3xl font-extrabold sm:text-4xl">minutes</div>
          <p className="recap-in-late mt-8 max-w-md text-lg text-white/85">
            {hours >= 2 ? `That's about ${nf(hours)} hours, across ` : "Across "}
            {nf(r.plays)} plays of {nf(r.tracks)} different songs by {nf(r.artists)} artists.
          </p>
        </>
      ),
    },
  ];
  if (top)
    pages.push({
      id: "top-artist",
      bg: "from-emerald-500 via-teal-600 to-cyan-900",
      body: (
        <>
          <Kicker>Your top artist</Kicker>
          <Cover item={top.item} size={420} round className="recap-in size-56 sm:size-72" />
          <div className="recap-in-late mt-6 text-center text-[clamp(2.25rem,7vw,4.5rem)] leading-none font-black tracking-tight">{top.item.title}</div>
          <p className="recap-in-late mt-4 text-lg text-white/85">
            {nf(top.plays)} {top.plays === 1 ? "play" : "plays"} · {nf(top.minutes)} {top.minutes === 1 ? "minute" : "minutes"} together
          </p>
        </>
      ),
    });
  if (r.topArtists.length > 1)
    pages.push({
      id: "top-artists",
      bg: "from-indigo-600 via-violet-700 to-purple-950",
      body: (
        <>
          <Kicker>Your top artists</Kicker>
          <ol className="w-full max-w-lg space-y-3">
            {r.topArtists.slice(0, 5).map((e, i) => (
              <EntryRow key={e.item.id} e={e} rank={i + 1} />
            ))}
          </ol>
        </>
      ),
    });
  if (r.topTracks.length)
    pages.push({
      id: "top-songs",
      bg: "from-amber-400 via-orange-500 to-rose-700",
      body: (
        <>
          <Kicker>Your top songs</Kicker>
          <ol className="w-full max-w-lg space-y-3">
            {r.topTracks.slice(0, 5).map((e, i) => (
              <EntryRow key={e.item.id} e={e} rank={i + 1} onPlay={() => actions.playOne(e.item)} />
            ))}
          </ol>
          <button onClick={actions.playTop} className="recap-in-late mt-8 flex items-center gap-2 rounded-full bg-white px-6 py-3 font-bold text-black hover:bg-amber-200">
            <Play className="size-5 fill-current" /> Play your top songs
          </button>
        </>
      ),
    });
  if (r.topAlbums.length)
    pages.push({
      id: "top-albums",
      bg: "from-sky-500 via-blue-700 to-slate-900",
      body: (
        <>
          <Kicker>Albums on repeat</Kicker>
          <ul className="grid w-full max-w-xl grid-cols-2 gap-4 sm:grid-cols-3">
            {r.topAlbums.slice(0, 6).map((e, i) => (
              <li key={e.item.id} className="recap-in" style={{ animationDelay: `${i * 80}ms` }}>
                <Cover item={e.item} size={240} className="aspect-square w-full" />
                <div className="mt-2 truncate font-bold">{e.item.title}</div>
                <div className="truncate text-sm text-white/70">{e.item.artistCredit ?? e.item.parentTitle ?? ""}</div>
              </li>
            ))}
          </ul>
        </>
      ),
    });
  if (r.topGenres.length)
    pages.push({
      id: "genres",
      bg: "from-lime-400 via-green-600 to-emerald-950",
      body: (
        <>
          <Kicker>Your sound</Kicker>
          <ul className="w-full max-w-lg space-y-3">
            {r.topGenres.slice(0, 6).map((g, i) => (
              <li key={g.name} className="recap-in text-left" style={{ animationDelay: `${i * 80}ms` }}>
                <div className="mb-1 flex justify-between text-lg font-bold">
                  <span>{g.name}</span>
                  <span className="text-white/70 tabular-nums">{nf(g.plays)}</span>
                </div>
                <div className="h-3 overflow-hidden rounded-full bg-black/25">
                  <div className="h-full rounded-full bg-white" style={{ width: `${(g.plays / genreMax) * 100}%` }} />
                </div>
              </li>
            ))}
          </ul>
        </>
      ),
    });
  pages.push({
    id: "when",
    bg: "from-rose-500 via-red-600 to-purple-900",
    body: (
      <>
        <Kicker>When you listen</Kicker>
        <div className="recap-in text-[clamp(2.5rem,8vw,5rem)] leading-none font-black tracking-tight">{lt.title}</div>
        <p className="recap-in-late mt-3 mb-8 text-lg text-white/85">
          {lt.line} Your busiest hour was {hourLabel(lt.peak)}, and your biggest month was {monthName}.
        </p>
        <div className="grid w-full max-w-2xl gap-8 sm:grid-cols-2">
          <Bars values={r.byHour} label="Plays by hour of the day" highlight={lt.peak} labels={r.byHour.map((_, h) => (h % 6 === 0 ? String(h) : ""))} />
          <Bars values={r.byMonth} label="Minutes by month" highlight={peakMonth} labels={months} />
        </div>
      </>
    ),
  });
  pages.push({
    id: "streaks",
    bg: "from-cyan-400 via-sky-600 to-indigo-900",
    body: (
      <>
        {r.topDay && (
          <>
            <Kicker>Your biggest day</Kicker>
            <div className="recap-in text-[clamp(2.25rem,7vw,4.5rem)] leading-none font-black tracking-tight">
              {new Date(`${r.topDay.date}T12:00:00`).toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" })}
            </div>
            <p className="recap-in-late mt-3 text-lg text-white/85">{nf(r.topDay.minutes)} minutes of music in one day.</p>
          </>
        )}
        <div className="recap-in-late mt-12 text-lg font-bold tracking-wide text-white/80 uppercase">Longest streak</div>
        <Big className="mt-2">{nf(r.longestStreakDays)}</Big>
        <div className="recap-in-late mt-2 text-2xl font-extrabold">{r.longestStreakDays === 1 ? "day" : "days in a row"}</div>
      </>
    ),
  });
  pages.push({
    id: "new-artists",
    bg: "from-yellow-300 via-amber-500 to-orange-700",
    body: (
      <>
        <Kicker>New discoveries</Kicker>
        <Big>{nf(r.newArtists)}</Big>
        <div className="recap-in-late mt-3 text-3xl font-extrabold">{r.newArtists === 1 ? "new artist" : "new artists"}</div>
        <p className="recap-in-late mt-6 max-w-md text-lg text-white/85">
          {r.newArtists && r.artists ? `${Math.round((r.newArtists / r.artists) * 100)}% of the artists you played in ${r.year} were new to you.` : "You stuck with the artists you love."}
          {r.firstTrack ? ` Your first song of the year was “${r.firstTrack.title}”.` : ""}
        </p>
      </>
    ),
  });
  pages.push({
    id: "summary",
    bg: "from-fuchsia-600 via-violet-700 to-indigo-950",
    body: (
      <div className="recap-in w-full max-w-md rounded-3xl bg-black/30 p-6 text-left shadow-2xl ring-1 ring-white/20 backdrop-blur sm:p-8" data-testid="recap-summary">
        <div className="flex items-center gap-4">
          {top && <Cover item={top.item} size={200} className="size-24" />}
          <div>
            <div className="text-sm font-bold tracking-wide text-white/70 uppercase">Marquee · {r.year}</div>
            <div className="text-3xl leading-tight font-black">Your Year in Music</div>
          </div>
        </div>
        <dl className="mt-6 grid grid-cols-2 gap-x-4 gap-y-5">
          <div>
            <dt className="text-xs font-bold text-white/60 uppercase">Top artists</dt>
            <dd>
              <ol className="mt-1 space-y-0.5 text-sm font-semibold">
                {r.topArtists.slice(0, 5).map((e, i) => (
                  <li key={e.item.id} className="truncate">
                    {i + 1}. {e.item.title}
                  </li>
                ))}
              </ol>
            </dd>
          </div>
          <div>
            <dt className="text-xs font-bold text-white/60 uppercase">Top songs</dt>
            <dd>
              <ol className="mt-1 space-y-0.5 text-sm font-semibold">
                {r.topTracks.slice(0, 5).map((e, i) => (
                  <li key={e.item.id} className="truncate">
                    {i + 1}. {e.item.title}
                  </li>
                ))}
              </ol>
            </dd>
          </div>
          <div>
            <dt className="text-xs font-bold text-white/60 uppercase">Minutes listened</dt>
            <dd className="text-3xl font-black">{nf(r.minutes)}</dd>
          </div>
          <div>
            <dt className="text-xs font-bold text-white/60 uppercase">Top genre</dt>
            <dd className="truncate text-2xl font-black">{topGenre?.name ?? "—"}</dd>
          </div>
        </dl>
        <div className="mt-8 flex flex-wrap gap-3">
          <button onClick={actions.save} disabled={actions.saving || !r.topTracks.length} className="flex items-center gap-2 rounded-full bg-white px-5 py-2.5 text-sm font-bold text-black hover:bg-amber-200 disabled:opacity-60">
            <ListPlus className="size-4" /> {actions.saving ? "Saving…" : "Save as playlist"}
          </button>
          <button onClick={actions.playTop} disabled={!r.topTracks.length} className="flex items-center gap-2 rounded-full border border-white/50 px-5 py-2.5 text-sm font-bold hover:bg-white/10 disabled:opacity-60">
            <Play className="size-4 fill-current" /> Play your top songs
          </button>
        </div>
      </div>
    ),
  });
  return pages;
}

/** The Year in Music story (MUSIC-22): full-screen pages, tapped, swiped or arrowed through. */
export function RecapPage() {
  const { year: wanted } = useSearch({ from: "/recap" });
  const years = useQuery(recapYearsQuery);
  const year = wanted ?? years.data?.[0] ?? new Date().getFullYear();
  const recap = useQuery({ ...recapQuery(year), enabled: wanted !== undefined || years.isSuccess });
  const navigate = useNavigate();
  const router = useRouter();
  const music = useMusicActions();
  const [index, setIndex] = useState(0);
  const touch = useRef<{ x: number; y: number } | null>(null);
  const save = useMutation({
    mutationFn: () => unwrap(api.POST("/me/recap/playlist", { params: { query: { year } } })),
    onSuccess: (pl) =>
      toast(`Saved “${pl.title}”.`, {
        link: { label: "Open playlist", to: "/playlist/$playlistId", params: { playlistId: String(pl.id) } },
      }),
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const close = useCallback(() => {
    if (window.history.length > 1) router.history.back();
    else navigate({ to: "/" });
  }, [router, navigate]);

  const r = recap.data;
  const pages = r
    ? buildPages(r, {
        playTop: () => music.play(r.topTracks.map((e) => e.item), 0, { source: `Your Top Songs ${r.year}` }),
        playOne: (t) => music.play([t]),
        save: () => save.mutate(),
        saving: save.isPending,
      })
    : [];
  const count = pages.length;
  const go = useCallback((d: number) => setIndex((i) => Math.max(0, Math.min(count - 1, i + d))), [count]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowRight" || e.key === " ") {
        e.preventDefault();
        go(1);
      } else if (e.key === "ArrowLeft") go(-1);
      else if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, close]);

  if (recap.isPending || years.isPending)
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black text-white">
        <Spinner label="Wrapping up your year" />
      </div>
    );
  if (recap.isError || !r)
    return (
      <div className="p-8">
        <Alert tone="error">{recap.error?.message ?? "Couldn't load your year in music."}</Alert>
      </div>
    );
  if (!r.plays)
    return (
      <div className="mx-auto max-w-md space-y-4 p-8 text-center">
        <h1 className="text-2xl font-bold">No music in {r.year} yet</h1>
        <p className="text-muted">Play some music and come back for your Year in Music.</p>
        <Button onClick={close}>Back</Button>
      </div>
    );
  const page = pages[Math.min(index, count - 1)]!;
  return (
    <div
      className={clsx("fixed inset-0 z-50 flex flex-col overflow-hidden bg-gradient-to-br text-white transition-colors", page.bg)}
      role="dialog"
      aria-label={`Your Year in Music ${r.year}`}
      onTouchStart={(e) => (touch.current = { x: e.touches[0]!.clientX, y: e.touches[0]!.clientY })}
      onTouchEnd={(e) => {
        const t = touch.current;
        touch.current = null;
        if (!t) return;
        const dx = e.changedTouches[0]!.clientX - t.x;
        const dy = e.changedTouches[0]!.clientY - t.y;
        if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy)) go(dx < 0 ? 1 : -1);
      }}
    >
      {/* Story progress */}
      <div className="flex gap-1 px-3 pt-3" aria-hidden>
        {pages.map((p, i) => (
          <div key={p.id} className="h-1 flex-1 overflow-hidden rounded-full bg-white/30">
            <div className={clsx("h-full bg-white transition-[width] duration-500", i <= index ? "w-full" : "w-0")} />
          </div>
        ))}
      </div>
      <header className="flex items-center gap-3 px-4 py-3">
        <span className="text-sm font-bold tracking-wide">Your {r.year} in Music</span>
        {(years.data?.length ?? 0) > 1 && (
          <Select
            aria-label="Recap year"
            className="h-8 w-24 border-white/30 bg-black/25 text-white"
            value={year}
            onChange={(e) => {
              setIndex(0);
              navigate({ to: "/recap", search: { year: Number(e.target.value) }, replace: true });
            }}
          >
            {years.data!.map((y) => (
              <option key={y} value={y}>
                {y}
              </option>
            ))}
          </Select>
        )}
        <span className="ml-auto text-sm text-white/70 tabular-nums" aria-live="polite">
          {index + 1} / {count}
        </span>
        <button onClick={close} className="rounded-full p-2 hover:bg-white/15" aria-label="Close Year in Music">
          <X className="size-6" />
        </button>
      </header>
      <main
        key={page.id}
        className="relative flex min-h-0 flex-1 flex-col items-center justify-center-safe overflow-y-auto px-6 pb-16 text-center"
        data-testid={`recap-page-${page.id}`}
        onClick={(e) => {
          // Tap the edges to move, like a story; buttons and links do their own thing.
          if ((e.target as HTMLElement).closest("button,a,select,input")) return;
          const x = e.clientX / window.innerWidth;
          if (x < 0.25) go(-1);
          else if (x > 0.75) go(1);
        }}
      >
        {page.body}
      </main>
      <button
        onClick={() => go(-1)}
        disabled={index === 0}
        className="absolute top-1/2 left-2 hidden -translate-y-1/2 rounded-full bg-black/20 p-3 hover:bg-black/40 disabled:opacity-0 sm:block"
        aria-label="Previous page"
      >
        <ChevronLeft className="size-7" />
      </button>
      <button
        onClick={() => go(1)}
        disabled={index >= count - 1}
        className="absolute top-1/2 right-2 hidden -translate-y-1/2 rounded-full bg-black/20 p-3 hover:bg-black/40 disabled:opacity-0 sm:block"
        aria-label="Next page"
      >
        <ChevronRight className="size-7" />
      </button>
    </div>
  );
}
