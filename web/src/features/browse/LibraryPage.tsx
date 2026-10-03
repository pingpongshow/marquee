import { useQueries, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { clsx } from "clsx";
import { ChevronLeft, Filter, LayoutGrid, List, Sparkles, Star, X } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, meQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import type { operations } from "@/api/schema.gen";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import { ItemMenu } from "./ItemMenu";
import { isMusicSection, MusicHome, MusicSectionPage, musicSectionTitles } from "../music/MusicHome";
import { useMusicActions } from "../player/MusicPlayer";
import { MuseButton } from "../search/MuseVideo";
import { RowRating } from "../music/Rating";
import { FavoritesPage } from "../music/Favorites";
import { Poster } from "./Poster";
import { formatDuration, subtitleFor } from "./format";
import { sortOptions } from "./sorts";
import { SaveSmartCollectionDialog } from "./SmartCollection";

type ListQuery = NonNullable<operations["listLibraryItems"]["parameters"]["query"]>;
export type LibrarySearch = {
  sort?: ListQuery["sort"];
  watch?: ListQuery["watch"];
  genre?: string;
  decade?: number;
  rating?: string;
  res?: ListQuery["resolution"];
  /** Only what the person rated 4 stars or more (favourites). */
  fav?: boolean;
  view?: "grid" | "list";
  /** Movie libraries: browse collections instead of movies. */
  show?: "collections";
};

const PAGE = 100;
const GAP = 16;


export function validateLibrarySearch(s: Record<string, unknown>): LibrarySearch {
  const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);
  return {
    sort: str(s.sort) as LibrarySearch["sort"],
    watch: str(s.watch) as LibrarySearch["watch"],
    genre: str(s.genre),
    decade: typeof s.decade === "number" ? s.decade : undefined,
    rating: str(s.rating),
    res: str(s.res) as LibrarySearch["res"],
    fav: s.fav === true || s.fav === "1" ? true : undefined,
    view: s.view === "list" ? "list" : undefined,
    show: s.show === "collections" ? "collections" : undefined,
  };
}

/** Measures an element's width. */
function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => e && setWidth(e.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, width] as const;
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

/**
 * A library's page. Music libraries open on a curated home (MUSIC-15); their full grids live
 * at /library/{id}/artists, /albums and /songs, and the other Library pages beside them.
 */
export function LibraryPage() {
  const params = useParams({ strict: false }) as { libraryId: string; section?: string };
  const search = useSearch({ strict: false }) as LibrarySearch;
  const id = Number(params.libraryId);
  const lib = useQuery(librariesQuery).data?.find((l) => l.id === id);
  const filtered = !!(search.watch || search.genre || search.decade || search.rating || search.res || search.fav);
  const section = lib?.type === "music" && isMusicSection(params.section) ? params.section : undefined;
  if (lib?.type === "music") {
    if (section === "favorites") return <FavoritesPage libraryId={id} name={lib.name} />;
    if (section === "genres" || section === "decades" || section === "moods" || section === "muse")
      return <MusicSectionPage key={section} libraryId={id} name={lib.name} section={section} />;
    // Old links with filters on the library itself still show the (artist) grid.
    if (!section && !filtered) return <MusicHome libraryId={id} name={lib.name} />;
  }
  const grid = section === "artists" || section === "albums" || section === "songs" ? section : undefined;
  return <LibraryGrid key={`${id}-${grid ?? ""}`} id={id} section={grid} search={search} filtered={filtered} />;
}

function LibraryGrid({ id, section, search, filtered }: { id: number; section?: "artists" | "albums" | "songs"; search: LibrarySearch; filtered: boolean }) {
  const navigate = useNavigate();
  const music = useMusicActions();
  const lib = useQuery(librariesQuery).data?.find((l) => l.id === id);
  const isMusic = lib?.type === "music";
  const itemType = section === "albums" ? "album" : section === "songs" ? "track" : undefined;
  const isVideo = !isMusic;
  const shape = isMusic ? "square" : lib?.type === "videos" ? "wide" : "poster";
  const sort = search.sort ?? "title";
  const view = search.view ?? (section === "songs" ? "list" : "grid");
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [savingSmart, setSavingSmart] = useState(false);
  const isAdmin = !!useQuery(meQuery).data?.isAdmin;
  // Movie and show libraries can save their filters as a smart collection (META-7).
  const smartType = lib?.type === "movies" ? "movie" : lib?.type === "shows" || lib?.type === "anime" ? "show" : null;
  // A random sort gets a fresh seed each time it's chosen, but stays stable while scrolling.
  const [seed] = useState(() => Date.now());

  const set = (patch: Partial<LibrarySearch>) => void navigate({ to: ".", search: (s: LibrarySearch) => ({ ...s, ...patch }), replace: true });
  const collections = lib?.type === "movies" && search.show === "collections";
  const query: ListQuery = collections
    ? { sort, type: "collection" }
    : { sort, type: itemType, watch: search.watch, genre: search.genre, decade: search.decade, contentRating: search.rating, resolution: search.res, minMyRating: search.fav ? 8 : undefined };

  const filters = useQuery({
    queryKey: ["libraries", id, "filters", itemType ?? ""],
    queryFn: () => unwrap(api.GET("/libraries/{libraryId}/filters", { params: { path: { libraryId: id }, query: itemType ? { type: itemType } : {} } })),
  });

  const fetchPage = (page: number) => unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: id }, query: { ...query, offset: page * PAGE, limit: PAGE } } }));
  const keyBase = ["libraries", id, "items", query, sort === "random" ? seed : 0] as const;
  const first = useQuery({ queryKey: [...keyBase, 0], queryFn: () => fetchPage(0), placeholderData: (prev) => prev });
  const total = first.data?.total ?? 0;

  // Layout: columns from the container width; rows are virtualized inside <main>.
  const [gridRef, width] = useWidth<HTMLDivElement>();
  const minCol = shape === "wide" ? 240 : 150;
  const cols = view === "list" ? 1 : Math.max(2, Math.floor((width + GAP) / (minCol + GAP)));
  const colWidth = view === "list" ? width : (width - GAP * (cols - 1)) / cols;
  const artHeight = shape === "poster" ? colWidth * 1.5 : shape === "square" ? colWidth : (colWidth * 9) / 16;
  const rowHeight = view === "list" ? 64 : artHeight + 52 + 8;
  const rows = Math.ceil(total / cols);
  const [scrollEl, setScrollEl] = useState<HTMLElement | null>(null);
  useEffect(() => setScrollEl(gridRef.current?.closest("main") ?? null), [gridRef]);
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const grid = gridRef.current;
    if (!grid || !scrollEl) return;
    const measure = () => setScrollMargin(grid.getBoundingClientRect().top - scrollEl.getBoundingClientRect().top + scrollEl.scrollTop);
    measure();
    // Content above the grid (filters, the music Discover panel) can change height after it loads.
    const ro = new ResizeObserver(measure);
    if (grid.parentElement) for (const el of Array.from(grid.parentElement.children)) if (el !== grid) ro.observe(el);
    return () => ro.disconnect();
  }, [gridRef, scrollEl, filtersOpen, filtered, total]);
  const virt = useVirtualizer({ count: rows, getScrollElement: () => scrollEl, estimateSize: () => rowHeight, overscan: 4, scrollMargin });
  useEffect(() => virt.measure(), [rowHeight, virt]);
  const visible = virt.getVirtualItems();

  // Fetch the pages the visible rows need.
  const pages = useMemo(() => {
    const set = new Set<number>();
    for (const r of visible) {
      set.add(Math.floor((r.index * cols) / PAGE));
      set.add(Math.floor(((r.index + 1) * cols - 1) / PAGE));
    }
    return [...set].filter((p) => p * PAGE < total);
  }, [visible, cols, total]);
  const pageResults = useQueries({
    queries: pages.map((p) => ({ queryKey: [...keyBase, p], queryFn: () => fetchPage(p), staleTime: 30_000 })),
  });
  const itemAt = (i: number): ItemSummary | undefined => {
    const p = Math.floor(i / PAGE);
    const data = p === 0 ? first.data : pageResults[pages.indexOf(p)]?.data;
    return data?.items[i - p * PAGE];
  };

  const jump = (offset: number) => virt.scrollToIndex(Math.floor(offset / cols), { align: "start" });
  const showJumpBar = sort === "title" && !filtered && total > 40 && (filters.data?.letters.length ?? 0) > 1;
  const genreValue = search.genre ?? "";

  return (
    <div className="flex">
      <div className="min-w-0 flex-1 p-6 lg:p-8">
        {section && (
          <Link to="/library/$libraryId" params={{ libraryId: String(id) }} className="mb-1 inline-flex items-center gap-1 text-sm text-muted hover:text-text">
            <ChevronLeft className="size-4" aria-hidden /> {lib?.name}
          </Link>
        )}
        <div className="mb-4 flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-bold">{section ? musicSectionTitles[section] : (lib?.name ?? "Library")}</h1>
          {lib?.type === "movies" && (
            <div className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="tablist" aria-label="Show">
              {(
                [
                  [undefined, "Movies"],
                  ["collections", "Collections"],
                ] as const
              ).map(([v, label]) => (
                <button
                  key={label}
                  role="tab"
                  aria-selected={search.show === v}
                  onClick={() => set({ show: v, watch: undefined, genre: undefined, decade: undefined, rating: undefined, res: undefined, fav: undefined })}
                  className={clsx("rounded px-3 py-1", search.show === v ? "bg-surface-3 text-text" : "text-muted hover:text-text")}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
          {first.data && <span className="text-sm text-muted">{total.toLocaleString()} {filtered ? "matching" : collections ? "collections" : "items"}</span>}
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {isVideo && lib && lib.type !== "photos" && <MuseButton libraryId={id} />}
            {!collections && (
              <Button size="sm" variant={filtersOpen || filtered ? "primary" : "secondary"} onClick={() => setFiltersOpen((v) => !v)} aria-expanded={filtersOpen}>
                <Filter className="size-4" /> Filter
              </Button>
            )}
            {!collections && (
              <Button size="sm" variant={search.fav ? "primary" : "secondary"} onClick={() => set({ fav: search.fav ? undefined : true })} aria-pressed={!!search.fav}>
                <Star className="size-4" aria-hidden /> Rated 4★+
              </Button>
            )}
            <div className="w-44">
            <Select aria-label="Sort by" className="h-8" value={sort} onChange={(e) => set({ sort: e.target.value === "title" ? undefined : (e.target.value as LibrarySearch["sort"]) })}>
              {sortOptions
                .filter((o) => (isVideo || !o.video) && (!collections || !o.personal))
                .map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
            </Select>
            </div>
            <div className="flex rounded-md bg-surface-2 p-0.5" role="group" aria-label="View">
              <button onClick={() => set({ view: undefined })} className={clsx("rounded p-1.5", view === "grid" ? "bg-surface-3 text-text" : "text-muted")} aria-label="Grid view" aria-pressed={view === "grid"}>
                <LayoutGrid className="size-4" />
              </button>
              <button onClick={() => set({ view: "list" })} className={clsx("rounded p-1.5", view === "list" ? "bg-surface-3 text-text" : "text-muted")} aria-label="List view" aria-pressed={view === "list"}>
                <List className="size-4" />
              </button>
            </div>
          </div>
        </div>

        {filtersOpen && !collections && (
          <div className="mb-4 rounded-lg border border-border bg-surface p-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
            <Select aria-label="Watched state" value={search.watch ?? ""} onChange={(e) => set({ watch: (e.target.value || undefined) as LibrarySearch["watch"] })}>
              <option value="">{isMusic ? "Any play state" : "Watched or not"}</option>
              <option value="unwatched">{isMusic ? "Unplayed" : "Unwatched"}</option>
              <option value="in_progress">In progress</option>
              <option value="watched">{isMusic ? "Played" : "Watched"}</option>
            </Select>
            <Select aria-label="Genre" value={genreValue} onChange={(e) => set({ genre: e.target.value || undefined })}>
              <option value="">All genres</option>
              {filters.data?.genres.map((g) => (
                <option key={g.value} value={g.value}>
                  {g.value} ({g.count})
                </option>
              ))}
            </Select>
            <Select aria-label="Decade" value={search.decade ?? ""} onChange={(e) => set({ decade: e.target.value ? Number(e.target.value) : undefined })}>
              <option value="">Any decade</option>
              {filters.data?.decades.map((d) => (
                <option key={d.value} value={d.value}>
                  {d.value}s ({d.count})
                </option>
              ))}
            </Select>
            {isVideo && (
              <Select aria-label="Content rating" value={search.rating ?? ""} onChange={(e) => set({ rating: e.target.value || undefined })}>
                <option value="">Any rating</option>
                {filters.data?.contentRatings.map((r) => (
                  <option key={r.value} value={r.value}>
                    {r.value} ({r.count})
                  </option>
                ))}
              </Select>
            )}
            {isVideo && (
              <Select aria-label="Resolution" value={search.res ?? ""} onChange={(e) => set({ res: (e.target.value || undefined) as LibrarySearch["res"] })}>
                <option value="">Any resolution</option>
                <option value="4k">4K</option>
                <option value="1080">1080p</option>
                <option value="720">720p</option>
                <option value="sd">SD</option>
              </Select>
            )}
          </div>
          {isAdmin && smartType && (
            <div className="mt-3 flex justify-end">
              <Button size="sm" variant="ghost" onClick={() => setSavingSmart(true)}>
                <Sparkles className="size-4" /> Save as smart collection
              </Button>
            </div>
          )}
          </div>
        )}
        {savingSmart && smartType && (
          <SaveSmartCollectionDialog
            libraryId={id}
            rules={{ itemType: smartType, sort: sort === "-myRating" ? undefined : sort, watch: search.watch, genre: search.genre, decade: search.decade, contentRating: search.rating, resolution: search.res }}
            onClose={() => setSavingSmart(false)}
          />
        )}
        {filtered && (
          <div className="mb-4 flex flex-wrap items-center gap-2">
            {search.watch && <Chip label={search.watch.replace("_", " ")} onClear={() => set({ watch: undefined })} />}
            {search.genre && <Chip label={search.genre} onClear={() => set({ genre: undefined })} />}
            {search.decade && <Chip label={`${search.decade}s`} onClear={() => set({ decade: undefined })} />}
            {search.rating && <Chip label={search.rating} onClear={() => set({ rating: undefined })} />}
            {search.fav && <Chip label="Rated 4★+" onClear={() => set({ fav: undefined })} />}
            {search.res && <Chip label={search.res === "4k" ? "4K" : search.res === "sd" ? "SD" : `${search.res}p`} onClear={() => set({ res: undefined })} />}
            <button onClick={() => set({ watch: undefined, genre: undefined, decade: undefined, rating: undefined, res: undefined, fav: undefined })} className="text-xs text-muted hover:text-text">
              Clear all
            </button>
          </div>
        )}

        {first.isPending && <Spinner />}
        {first.isError && <Alert tone="error">{first.error.message}</Alert>}
        {first.data?.total === 0 &&
          (filtered ? (
            <p className="text-muted">Nothing matches these filters.</p>
          ) : (
            <p className="text-muted">Nothing here yet. {lib?.scanStatus !== "idle" ? "A scan is in progress." : "Scan the library from Settings → Libraries."}</p>
          ))}

        <div ref={gridRef} className="relative" style={{ height: virt.getTotalSize() }}>
          {width > 0 &&
            visible.map((row) => (
              <div
                key={row.key}
                className="absolute inset-x-0 top-0"
                style={{ transform: `translateY(${row.start - scrollMargin}px)`, height: rowHeight, display: "grid", gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, columnGap: GAP }}
              >
                {Array.from({ length: cols }, (_, c) => {
                  const i = row.index * cols + c;
                  if (i >= total) return <div key={c} />;
                  const it = itemAt(i);
                  if (view === "list")
                    return <ListRow key={c} item={it} shape={shape} onPlay={it?.type === "track" ? () => music.play([it], 0, { source: musicSectionTitles.songs }) : undefined} />;
                  return (
                    <div key={c} className="min-w-0">
                      {it ? (
                        <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
                          <Poster item={it} shape={shape} width={Math.round(colWidth)} className="transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent" />
                          <div className="mt-2 truncate text-sm font-medium" title={it.title}>
                            {it.title}
                          </div>
                          <div className="truncate text-xs text-muted">{subtitleFor(it)}</div>
                        </Link>
                      ) : (
                        <div className="animate-pulse rounded-md bg-surface-2" style={{ height: artHeight }} />
                      )}
                    </div>
                  );
                })}
              </div>
            ))}
        </div>
      </div>

      {showJumpBar && (
        <nav aria-label="Jump to letter" className="sticky top-0 flex h-[calc(100vh-3.5rem)] w-7 shrink-0 flex-col items-center justify-center py-4 text-[11px] font-semibold text-muted">
          {filters.data!.letters.map((l) => (
            <button key={l.letter} onClick={() => jump(l.offset)} className="w-full py-px text-center hover:text-accent">
              {l.letter}
            </button>
          ))}
        </nav>
      )}
    </div>
  );
}

function ListRow({ item, shape, onPlay }: { item?: ItemSummary; shape: "poster" | "square" | "wide"; onPlay?: () => void }) {
  if (!item) return <div className="h-14 animate-pulse rounded bg-surface-2" />;
  const watched = item.type === "show" ? item.leafCount > 0 && item.watchedLeafCount === item.leafCount : (item.viewCount ?? 0) > 0;
  return (
    <div className="group flex h-14 items-center gap-4 border-b border-border">
      <Link
        to="/item/$itemId"
        params={{ itemId: String(item.id) }}
        onClick={(e) => {
          if (!onPlay) return;
          e.preventDefault(); // songs play instead of opening a page
          onPlay();
        }}
        className="flex min-w-0 flex-1 items-center gap-4 hover:text-accent"
      >
        <div className={clsx("shrink-0", shape === "wide" ? "w-20" : shape === "square" ? "w-12" : "w-9")}>
          <Poster item={item} shape={shape} width={80} compact />
        </div>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium">{item.title}</span>
          <span className="block truncate text-xs text-muted">{subtitleFor(item)}</span>
        </span>
      </Link>
      {item.type === "track" && <RowRating item={item} />}
      <span className="hidden w-16 text-sm text-muted sm:block">{item.year ?? ""}</span>
      <span className="hidden w-20 text-right text-sm text-muted tabular-nums md:block">{item.type === "movie" || item.type === "video" ? formatDuration(item.durationMs) : item.type === "show" ? `${item.childCount} seasons` : ""}</span>
      <span className={clsx("hidden w-20 text-right text-xs md:block", watched ? "text-success" : "text-faint")}>{item.type === "artist" ? "" : watched ? "Watched" : ""}</span>
      <ItemMenu item={item} />
    </div>
  );
}
