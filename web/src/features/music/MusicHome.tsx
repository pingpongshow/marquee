import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Disc3,
  Guitar,
  ListMusic,
  Loader2,
  Mic2,
  Music2,
  Palette,
  Radio,
  Shuffle,
  Sparkles,
} from "lucide-react";
import type { ReactNode } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery, playlistsQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Alert, Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { subtitleFor } from "../browse/format";
import { useMusicActions } from "../player/MusicPlayer";
import { PlaylistMosaic } from "../playlists/PlaylistMosaic";
import {
  AnalysisNote,
  BrowseMoodsAndStyles,
  MixCard,
  MusePanel,
  StationChips,
  useAlbumFacets,
  useMixes,
  useMusicStatus,
} from "./Discover";
import { RecapCard } from "./RecapCard";
import { useRadio } from "./useRadio";

/** Pages behind the music home's Library list (/library/{id}/{section}). */
export type MusicSection = "artists" | "albums" | "songs" | "genres" | "decades" | "moods" | "muse";
export const musicSectionTitles: Record<MusicSection, string> = {
  artists: "Artists",
  albums: "Albums",
  songs: "Songs",
  genres: "Genres",
  decades: "Decades",
  moods: "Moods & Styles",
  muse: "Muse & Stations",
};
export const isMusicSection = (s: unknown): s is MusicSection => typeof s === "string" && s in musicSectionTitles;

const countQuery = (libraryId: number, type: "artist" | "album" | "track") => ({
  queryKey: ["libraries", libraryId, "count", type],
  queryFn: () =>
    unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { type, limit: 1 } } })).then((p) => p.total),
  staleTime: 60_000,
});

/** A round icon button with a small label, Plexamp style. */
function QuickAction({ icon, label, onClick, to, busy }: { icon: ReactNode; label: string; onClick?: () => void; to?: { libraryId: number; section: MusicSection }; busy?: boolean }) {
  const inner = (
    <>
      <span className="flex size-12 items-center justify-center rounded-full bg-surface-2 text-text transition-colors group-hover:bg-accent group-hover:text-black [&>svg]:size-5">
        {busy ? <Loader2 className="animate-spin" /> : icon}
      </span>
      <span className="text-xs text-muted group-hover:text-text">{label}</span>
    </>
  );
  const cls = "group flex w-20 flex-col items-center gap-1.5 disabled:opacity-50";
  return to ? (
    <Link to="/library/$libraryId/$section" params={{ libraryId: String(to.libraryId), section: to.section }} className={cls}>
      {inner}
    </Link>
  ) : (
    <button type="button" onClick={onClick} disabled={busy} className={cls}>
      {inner}
    </button>
  );
}

/** A horizontal shelf with a "See all" link. */
function Shelf({ title, seeAll, children, testId }: { title: string; seeAll?: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <section data-testid={testId} aria-label={title}>
      <div className="mb-3 flex items-baseline gap-3">
        <h2 className="text-lg font-semibold">{title}</h2>
        {seeAll && <span className="ml-auto text-sm text-muted hover:text-text">{seeAll}</span>}
      </div>
      <ul className="flex gap-4 overflow-x-auto pb-2">{children}</ul>
    </section>
  );
}

function ItemCard({ it }: { it: ItemSummary }) {
  return (
    <li className="w-36 shrink-0">
      <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
        <Poster item={it} shape="square" width={180} className="group-hover:ring-2 group-hover:ring-accent" />
        <div className="mt-2 truncate text-sm font-medium">{it.title}</div>
        <div className="truncate text-xs text-muted">{subtitleFor(it)}</div>
      </Link>
    </li>
  );
}

function SeeAll({ libraryId, section, sort }: { libraryId: number; section: MusicSection; sort?: "-added" | "-viewed" }) {
  return (
    <Link to="/library/$libraryId/$section" params={{ libraryId: String(libraryId), section }} search={{ sort }}>
      See all
    </Link>
  );
}

/** The artists this person plays most (all time) in this library, at most 20. */
function useTopArtists(libraryId: number) {
  const me = useQuery(meQuery);
  return useQuery({
    queryKey: ["music", "top-artists", libraryId, me.data?.id],
    enabled: !!me.data,
    queryFn: async () => {
      const [stats, recent] = await Promise.all([
        unwrap(api.GET("/stats", { params: { query: { days: 0, limit: 30, userId: me.data!.id } } })),
        unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { type: "artist", sort: "-viewed", limit: 200 } } })),
      ]);
      const byId = new Map(recent.items.map((a) => [a.id, a]));
      return stats.artists.flatMap((a) => (a.id && byId.get(a.id) ? [byId.get(a.id)!] : [])).slice(0, 20);
    },
    staleTime: 60_000,
  });
}

/** Library radio and a random 200 tracks, from the quick actions. */
function useShuffleAll(libraryId: number) {
  const music = useMusicActions();
  return useMutation({
    mutationFn: () =>
      unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { type: "track", sort: "random", limit: 200 } } })),
    onSuccess: (p) => p.items.length && music.play(p.items, 0, { source: "Shuffle All" }),
  });
}

function LibraryRow({ icon, title, count, libraryId, section }: { icon: ReactNode; title: string; count?: number; libraryId: number; section?: MusicSection }) {
  const cls = "flex items-center gap-4 px-4 py-3 hover:bg-surface-2 [&>svg:first-child]:size-5 [&>svg:first-child]:text-accent";
  const body = (
    <>
      {icon}
      <span className="flex-1 font-medium">{title}</span>
      {count !== undefined && <span className="text-sm text-muted tabular-nums">{count.toLocaleString()}</span>}
      <ChevronRight className="size-4 text-faint" aria-hidden />
    </>
  );
  return (
    <li>
      {section ? (
        <Link to="/library/$libraryId/$section" params={{ libraryId: String(libraryId), section }} className={cls}>
          {body}
        </Link>
      ) : (
        <Link to="/playlists" className={cls}>
          {body}
        </Link>
      )}
    </li>
  );
}

/** Music library home (MUSIC-15): quick actions, shelves of what's yours, then the Library list. */
export function MusicHome({ libraryId, name }: { libraryId: number; name: string }) {
  const { ready, status } = useMusicStatus();
  const radio = useRadio();
  const shuffle = useShuffleAll(libraryId);
  const hubs = useQuery({ queryKey: ["items", "hubs"], queryFn: () => unwrap(api.GET("/hubs/home")) });
  const played = hubs.data?.find((h) => h.id === `played-${libraryId}`)?.items ?? [];
  const added = useQuery({
    queryKey: ["libraries", libraryId, "items", "recent-albums"],
    queryFn: () => unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId }, query: { type: "album", sort: "-added", limit: 20 } } })),
  });
  const mixes = useMixes(libraryId);
  const playlists = useQuery(playlistsQuery("audio"));
  const top = useTopArtists(libraryId);
  const facets = useAlbumFacets(libraryId);
  const artists = useQuery(countQuery(libraryId, "artist"));
  const albums = useQuery(countQuery(libraryId, "album"));
  const songs = useQuery(countQuery(libraryId, "track"));
  const error = radio.error ?? shuffle.error;

  return (
    <div className="space-y-10 p-6 lg:p-8">
      <div className="flex flex-wrap items-center gap-4">
        <h1 className="text-2xl font-bold">{name}</h1>
        <a href="#music-library" className="ml-auto text-sm text-muted hover:text-text lg:hidden">
          Library
        </a>
      </div>

      <div className="space-y-3">
        <div className="flex flex-wrap gap-2" role="group" aria-label="Quick actions">
          {ready && <QuickAction icon={<Radio />} label="Library Radio" busy={radio.isPending} onClick={() => radio.mutate({ seed: "library", libraryId })} />}
          <QuickAction icon={<Shuffle />} label="Shuffle All" busy={shuffle.isPending} onClick={() => shuffle.mutate()} />
          {ready && <QuickAction icon={<Sparkles />} label="Muse" to={{ libraryId, section: "muse" }} />}
        </div>
        {error && <Alert tone="error">{error.message}</Alert>}
        <AnalysisNote />
      </div>

      {played.length > 0 && (
        <Shelf title="Recently Played" testId="shelf-played" seeAll={<SeeAll libraryId={libraryId} section="albums" sort="-viewed" />}>
          {played.map((it) => <ItemCard key={it.id} it={it} />)}
        </Shelf>
      )}
      {!!mixes.data?.length && (
        <Shelf title="Mixes for You" testId="shelf-mixes" seeAll={ready && <SeeAll libraryId={libraryId} section="muse" />}>
          {mixes.data.map((m) => <MixCard key={m.id} mix={m} />)}
        </Shelf>
      )}
      <RecapCard />
      {!!added.data?.items.length && (
        <Shelf title="Recently Added" testId="shelf-added" seeAll={<SeeAll libraryId={libraryId} section="albums" sort="-added" />}>
          {added.data.items.map((it) => <ItemCard key={it.id} it={it} />)}
        </Shelf>
      )}
      {!!playlists.data?.length && (
        <Shelf title="Your Playlists" testId="shelf-playlists" seeAll={<Link to="/playlists">See all</Link>}>
          {playlists.data.map((p) => (
            <li key={p.id} className="w-36 shrink-0">
              <Link to="/playlist/$playlistId" params={{ playlistId: String(p.id) }} className="group block">
                <PlaylistMosaic playlist={p} size={180} className="group-hover:ring-2 group-hover:ring-accent" />
                <div className="mt-2 truncate text-sm font-medium">{p.title}</div>
                <div className="truncate text-xs text-muted">{p.itemCount} tracks</div>
              </Link>
            </li>
          ))}
        </Shelf>
      )}
      {!!top.data?.length && (
        <Shelf title="Top Artists" testId="shelf-top-artists" seeAll={<SeeAll libraryId={libraryId} section="artists" sort="-viewed" />}>
          {top.data.map((it) => <ItemCard key={it.id} it={it} />)}
        </Shelf>
      )}

      <section id="music-library" aria-labelledby="music-library-title" className="scroll-mt-4">
        <h2 id="music-library-title" className="mb-3 text-lg font-semibold">
          Library
        </h2>
        <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface" data-testid="music-library-list">
          <LibraryRow icon={<Mic2 />} title="Artists" count={artists.data} libraryId={libraryId} section="artists" />
          <LibraryRow icon={<Disc3 />} title="Albums" count={albums.data} libraryId={libraryId} section="albums" />
          <LibraryRow icon={<Music2 />} title="Songs" count={songs.data} libraryId={libraryId} section="songs" />
          <LibraryRow icon={<ListMusic />} title="Playlists" count={playlists.data?.length} libraryId={libraryId} />
          <LibraryRow icon={<Guitar />} title="Genres" libraryId={libraryId} section="genres" />
          {(ready || !!facets.data?.genres.length) && <LibraryRow icon={<Palette />} title="Moods & Styles" libraryId={libraryId} section="moods" />}
          {!!facets.data?.decades.length && <LibraryRow icon={<CalendarDays />} title="Decades" libraryId={libraryId} section="decades" />}
          {(ready || status?.enabled) && <LibraryRow icon={<Sparkles />} title="Muse & Stations" libraryId={libraryId} section="muse" />}
        </ul>
      </section>
    </div>
  );
}

export function SectionHeader({ libraryId, name, title, children }: { libraryId: number; name: string; title: string; children?: ReactNode }) {
  return (
    <div className="mb-6">
      <Link to="/library/$libraryId" params={{ libraryId: String(libraryId) }} className="mb-1 inline-flex items-center gap-1 text-sm text-muted hover:text-text">
        <ChevronLeft className="size-4" aria-hidden /> {name}
      </Link>
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-bold">{title}</h1>
        {children}
      </div>
    </div>
  );
}

const facetRow = "flex items-center gap-4 px-4 py-3 hover:bg-surface-2";

/** Genres, Decades, Moods & Styles, and Muse & Stations: the music pages that aren't item grids. */
export function MusicSectionPage({ libraryId, name, section }: { libraryId: number; name: string; section: "genres" | "decades" | "moods" | "muse" }) {
  const facets = useAlbumFacets(libraryId);
  const { ready } = useMusicStatus();
  const mixes = useMixes(libraryId);
  const title = musicSectionTitles[section];
  if (facets.isPending && (section === "genres" || section === "decades")) return <Spinner />;
  return (
    <div className="p-6 lg:p-8">
      <SectionHeader libraryId={libraryId} name={name} title={title} />
      {section === "genres" && (
        <FacetList
          empty="No genres yet."
          rows={[...(facets.data?.genres ?? [])].sort((a, b) => a.value.localeCompare(b.value)).map((g) => ({ key: g.value, label: g.value, count: g.count, search: { genre: g.value } }))}
          libraryId={libraryId}
        />
      )}
      {section === "decades" && (
        <FacetList
          empty="No decades yet."
          rows={(facets.data?.decades ?? []).map((d) => ({ key: String(d.value), label: `${d.value}s`, count: Number(d.count), search: { decade: Number(d.value) } }))}
          libraryId={libraryId}
        />
      )}
      {section === "moods" && <BrowseMoodsAndStyles libraryId={libraryId} genres={facets.data?.genres ?? []} />}
      {section === "muse" && (
        <div className="space-y-8">
          <AnalysisNote />
          <MusePanel libraryId={libraryId} />
          {ready && (
            <div>
              <h2 className="mb-3 text-lg font-semibold">Stations</h2>
              <StationChips libraryId={libraryId} />
            </div>
          )}
          {!!mixes.data?.length && (
            <Shelf title="Mixes for You">
              {mixes.data.map((m) => <MixCard key={m.id} mix={m} />)}
            </Shelf>
          )}
          {!ready && <p className="text-muted">Radios and Muse need Soundprint, which hasn't analysed this library yet.</p>}
        </div>
      )}
    </div>
  );
}

function FacetList({ rows, libraryId, empty }: { rows: { key: string; label: string; count: number; search: { genre?: string; decade?: number } }[]; libraryId: number; empty: string }) {
  if (!rows.length) return <p className="text-muted">{empty}</p>;
  return (
    <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface">
      {rows.map((r) => (
        <li key={r.key}>
          <Link to="/library/$libraryId/$section" params={{ libraryId: String(libraryId), section: "albums" }} search={r.search} className={clsx(facetRow)}>
            <span className="flex-1 font-medium">{r.label}</span>
            <span className="text-sm text-muted tabular-nums">{r.count.toLocaleString()} albums</span>
            <ChevronRight className="size-4 text-faint" aria-hidden />
          </Link>
        </li>
      ))}
    </ul>
  );
}

