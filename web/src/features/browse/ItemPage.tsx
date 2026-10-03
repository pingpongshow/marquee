import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { clsx } from "clsx";
import { AlertTriangle, ChevronRight, Sparkles } from "lucide-react";
import { useState } from "react";
import { api, imageUrl, personPhotoUrl, unwrap } from "@/api/client";
import { itemChildrenQuery, itemQuery, meQuery } from "@/api/queries";
import type { Credit, ItemDetail, ItemSummary, MediaStream } from "@/api/types";
import { Alert, Button, Spinner } from "@/components/ui";
import { PinToHomeButton } from "../home/EditHome";
import { describeRules, SmartRulesDialog } from "./SmartCollection";
import { useMusicState } from "../player/MusicPlayer";
import { ItemActions } from "./ItemActions";
import { ItemMenu } from "./ItemMenu";
import { PlayOnButton } from "../remote/PlayOn";
import { Rating } from "../music/Rating";
import { PlayButtons } from "./PlayButtons";
import { Poster } from "./Poster";
import { formatBytes, formatDuration, formatTrackTime, languageName, subtitleFor } from "./format";

function Breadcrumbs({ item }: { item: ItemDetail }) {
  const crumbs: { id?: number; title: string }[] = [];
  if (item.grandparentId) crumbs.push({ id: item.grandparentId, title: item.grandparentTitle ?? "" });
  if (item.parentId) crumbs.push({ id: item.parentId, title: item.parentTitle ?? "" });
  if (!crumbs.length) return null;
  return (
    <nav aria-label="Breadcrumb" className="mb-2 flex items-center gap-1 text-sm text-muted">
      {crumbs.map((c) => (
        <span key={c.id} className="flex items-center gap-1">
          <Link to="/item/$itemId" params={{ itemId: String(c.id) }} className="hover:text-text">
            {c.title}
          </Link>
          <ChevronRight className="size-4" aria-hidden />
        </span>
      ))}
    </nav>
  );
}

function streamLabel(s: MediaStream) {
  if (s.kind === "video") return [s.height ? `${s.height}p` : "", s.codec.toUpperCase(), s.bitDepth === 10 ? "10-bit" : "", s.frameRate ? `${s.frameRate} fps` : ""].filter(Boolean).join(" · ");
  if (s.kind === "audio")
    return [languageName(s.language), s.codec.toUpperCase(), s.channelLayout ?? (s.channels ? `${s.channels} ch` : ""), s.title ?? ""].filter(Boolean).join(" · ");
  return [languageName(s.language), s.codec.toUpperCase(), s.forced ? "Forced" : "", s.hearingImpaired ? "SDH" : "", s.external ? "External" : "", s.title ?? ""].filter(Boolean).join(" · ");
}

function MediaInfo({ item, isAdmin }: { item: ItemDetail; isAdmin: boolean }) {
  if (!item.versions.length) return null;
  return (
    <details className="mt-8 rounded-lg border border-border bg-surface">
      <summary className="cursor-pointer px-5 py-3 text-sm font-medium">Media info</summary>
      <div className="space-y-5 border-t border-border px-5 py-4 text-sm">
        {item.versions.map((v, vi) => (
          <div key={v.id} className="space-y-3">
            {item.versions.length > 1 && <div className="font-semibold">{v.label || `Version ${vi + 1}`}</div>}
            {v.files.map((f) => (
              <div key={f.id} className="space-y-2">
                {isAdmin && f.path && <div className="font-mono text-xs break-all text-muted">{f.path}</div>}
                <div className="text-muted">
                  {[f.container?.toUpperCase(), f.width && f.height ? `${f.width}×${f.height}` : "", f.bitrateKbps ? `${(f.bitrateKbps / 1000).toFixed(1)} Mbps` : "", formatBytes(f.size), f.hdrFormat?.toUpperCase().replace("_", " ")]
                    .filter(Boolean)
                    .join(" · ")}
                  {!f.available && <span className="ml-2 text-danger">File unavailable</span>}
                </div>
                <ul className="space-y-1">
                  {f.streams.map((s) => (
                    <li key={s.id} className="flex gap-3">
                      <span className="w-20 shrink-0 text-xs tracking-wide text-faint uppercase">{s.kind}</span>
                      <span>{streamLabel(s)}</span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        ))}
      </div>
    </details>
  );
}

function RatingBadges({ item }: { item: ItemDetail }) {
  const r = item.ratings;
  const badges: { label: string; value: string; title: string }[] = [];
  if (r?.imdb) badges.push({ label: "IMDb", value: r.imdb.toFixed(1), title: r.imdbVotes ? `${r.imdbVotes.toLocaleString()} votes` : "IMDb rating" });
  if (r?.rottenTomatoes != null) badges.push({ label: r.rottenTomatoes >= 60 ? "🍅" : "🤢", value: `${r.rottenTomatoes}%`, title: "Rotten Tomatoes critics" });
  if (r?.metacritic != null) badges.push({ label: "Metacritic", value: String(r.metacritic), title: "Metacritic" });
  if (r?.anilist != null) badges.push({ label: "AniList", value: `${r.anilist}%`, title: "AniList average score" });
  if (item.audienceRating) badges.push({ label: "TMDB", value: item.audienceRating.toFixed(1), title: "TMDB user score" });
  if (!badges.length) return null;
  return (
    <div className="mt-3 flex flex-wrap gap-2">
      {badges.map((b) => (
        <span key={b.label} title={b.title} className="inline-flex items-center gap-1.5 rounded-md bg-surface-2/80 px-2 py-1 text-sm">
          <span className="text-xs font-semibold text-muted">{b.label}</span>
          <span className="font-semibold">{b.value}</span>
        </span>
      ))}
    </div>
  );
}

function Cast({ credits }: { credits: Credit[] }) {
  const people = credits.filter((c) => c.role === "actor").slice(0, 20);
  const crew = credits.filter((c) => c.role === "director" || c.role === "creator" || c.role === "writer");
  if (!people.length && !crew.length) return null;
  return (
    <section className="mt-10">
      {crew.length > 0 && (
        <div className="mb-4 flex flex-wrap gap-x-8 gap-y-1 text-sm">
          {(["director", "creator", "writer"] as const).map((role) => {
            const people = crew.filter((c) => c.role === role).slice(0, 4);
            return people.length ? (
              <div key={role}>
                <span className="text-faint capitalize">{role === "writer" ? "Writers" : role + (people.length > 1 ? "s" : "")}</span>{" "}
                {people.map((c, i) => (
                  <span key={c.personId}>
                    {i > 0 && ", "}
                    <Link to="/person/$personId" params={{ personId: String(c.personId) }} className="text-text hover:underline">
                      {c.name}
                    </Link>
                  </span>
                ))}
              </div>
            ) : null;
          })}
        </div>
      )}
      {people.length > 0 && (
        <>
          <h2 className="mb-4 text-lg font-semibold">Cast</h2>
          <ul className="flex gap-4 overflow-x-auto pb-2">
            {people.map((c) => (
              <li key={c.personId} className="w-24 shrink-0 text-center">
                <Link to="/person/$personId" params={{ personId: String(c.personId) }} className="group block">
                  <div className="mx-auto mb-2 size-24 overflow-hidden rounded-full bg-surface-3 group-hover:ring-2 group-hover:ring-accent">
                    {c.hasPhoto && <img src={personPhotoUrl(c.personId, 96)} alt="" loading="lazy" className="size-full object-cover" />}
                  </div>
                  <div className="truncate text-sm font-medium group-hover:underline" title={c.name}>
                    {c.name}
                  </div>
                  {c.character && <div className="truncate text-xs text-muted" title={c.character}>{c.character}</div>}
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

function Children({ item }: { item: ItemDetail }) {
  const music = useMusicState();
  const children = useQuery({ ...itemChildrenQuery(item.id), enabled: item.childCount > 0 });
  if (!children.data?.items.length) return null;
  const list = children.data.items;
  const heading = { show: "Seasons", season: "Episodes", artist: "Albums", album: "Tracks", collection: "In this collection" }[item.type as string] ?? "Contents";

  if (item.type === "artist")
    return (
      <>
        <PopularTracks artist={item} />
        <ArtistReleases list={list} />
      </>
    );
  // Episodes and tracks are rows; seasons and albums are poster cards.
  const rows = item.type === "season" || item.type === "album";
  return (
    <section className="mt-10">
      <h2 className="mb-4 text-lg font-semibold">{heading}</h2>
      {rows ? (
        <ol className="divide-y divide-border rounded-lg border border-border bg-surface">
          {list.map((c: ItemSummary) => (
            <li key={c.id}>
              <Link
                to="/item/$itemId"
                params={{ itemId: String(c.id) }}
                onClick={(e) => {
                  if (c.type === "track") {
                    e.preventDefault(); // tracks play instead of opening a page
                    music.play(list.filter((t) => t.type === "track"), list.filter((t) => t.type === "track").indexOf(c), { source: item.title });
                  }
                }}
                className={music.current?.item.id === c.id ? "flex items-center gap-4 px-4 py-3 text-accent hover:bg-surface-2" : "flex items-center gap-4 px-4 py-3 hover:bg-surface-2"}
              >
                <span className="w-8 shrink-0 text-right text-sm text-faint tabular-nums">{c.index ?? ""}</span>
                {c.type === "episode" && c.images?.thumb && <img src={imageUrl(c.images.thumb, 160)} alt="" loading="lazy" className="aspect-video w-32 shrink-0 rounded object-cover" />}
                <span className="min-w-0 flex-1">
                  <span className={c.available ? "block truncate" : "block truncate text-faint line-through"}>{c.title}</span>
                  {c.type === "track" && c.artistCredit && c.artistCredit !== item.artistCredit && <span className="block truncate text-xs text-muted">{c.artistCredit}</span>}
                </span>
                <span className="shrink-0 text-sm text-muted tabular-nums">{c.type === "track" ? formatTrackTime(c.durationMs) : formatDuration(c.durationMs)}</span>
                {c.type === "track" && <ItemMenu item={c} className="-my-1" />}
              </Link>
            </li>
          ))}
        </ol>
      ) : (
        <CardGrid list={list} />
      )}
    </section>
  );
}

function CardGrid({ list }: { list: ItemSummary[] }) {
  return (
    <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6">
      {list.map((c) => (
        <li key={c.id}>
          <Link to="/item/$itemId" params={{ itemId: String(c.id) }} className="group block">
            <Poster item={c} shape={c.type === "album" ? "square" : "poster"} className="group-hover:ring-2 group-hover:ring-accent" />
            <div className="mt-2 truncate text-sm font-medium">{c.title}</div>
            <div className="truncate text-xs text-muted">{subtitleFor(c)}</div>
          </Link>
        </li>
      ))}
    </ul>
  );
}

/** The artist's most-listened tracks in the library (ListenBrainz, MUSIC-15). */
function PopularTracks({ artist }: { artist: ItemDetail }) {
  const music = useMusicState();
  const [all, setAll] = useState(false);
  const popular = useQuery({
    queryKey: ["items", artist.id, "popular"],
    queryFn: () => unwrap(api.GET("/items/{itemId}/popular", { params: { path: { itemId: artist.id } } })),
  });
  const tracks = popular.data?.items ?? [];
  if (!tracks.length) return null;
  const shown = all ? tracks : tracks.slice(0, 5);
  return (
    <section className="mt-10">
      <h2 className="mb-4 text-lg font-semibold">Popular</h2>
      <ol className="divide-y divide-border rounded-lg border border-border bg-surface">
        {shown.map((t, i) => (
          <li key={t.id}>
            <button
              type="button"
              onClick={() => music.play(tracks, i, { source: `${artist.title} – Popular` })}
              className={clsx("flex w-full items-center gap-4 px-4 py-3 text-left hover:bg-surface-2", music.current?.item.id === t.id && "text-accent")}
            >
              <span className="w-6 shrink-0 text-right text-sm text-faint tabular-nums">{i + 1}</span>
              <span className="min-w-0 flex-1">
                <span className="block truncate">{t.title}</span>
                <span className="block truncate text-xs text-muted">{t.parentTitle}</span>
              </span>
              <span className="shrink-0 text-sm text-muted tabular-nums">{formatTrackTime(t.durationMs)}</span>
            </button>
          </li>
        ))}
      </ol>
      {tracks.length > 5 && (
        <button type="button" className="mt-2 text-sm text-muted hover:text-text" onClick={() => setAll((v) => !v)}>
          {all ? "Show fewer" : `Show all ${tracks.length}`}
        </button>
      )}
    </section>
  );
}

/** Plexamp-style sections of an artist's releases, by MusicBrainz release type (META-3). */
const releaseSections: [string, string[]][] = [
  ["Albums", ["album", ""]],
  ["Singles & EPs", ["ep", "single"]],
  ["Live Albums", ["live"]],
  ["Compilations", ["compilation"]],
  ["Soundtracks", ["soundtrack"]],
  ["Remixes", ["remix"]],
  ["Demos", ["demo"]],
  ["Other", ["other"]],
];

function ArtistReleases({ list }: { list: ItemSummary[] }) {
  const groups = releaseSections
    .map(([title, types]) => [title, list.filter((c) => types.includes(c.releaseType ?? ""))] as const)
    .filter(([, l]) => l.length > 0);
  return groups.map(([title, l]) => (
    <section key={title} className="mt-10">
      <h2 className="mb-4 text-lg font-semibold">{title}</h2>
      <CardGrid list={l} />
    </section>
  ));
}

const extraNames: Record<string, string> = {
  trailer: "Trailer", featurette: "Featurette", behind_the_scenes: "Behind the scenes", deleted_scene: "Deleted scene",
  interview: "Interview", scene: "Scene", short: "Short", other: "Extra",
};

/** Trailers, featurettes and other extras (LIB-8); they play like any video. */
function Extras({ item }: { item: ItemDetail }) {
  if (!item.extras?.length) return null;
  return (
    <section className="mt-10">
      <h2 className="mb-4 text-lg font-semibold">Extras</h2>
      <ul className="flex gap-4 overflow-x-auto pb-2">
        {item.extras.map((x) => (
          <li key={x.id} className="w-60 shrink-0">
            <Link to="/play/$itemId" params={{ itemId: String(x.id) }} search={{}} className="group block">
              <Poster item={x} shape="wide" width={300} className="group-hover:ring-2 group-hover:ring-accent" />
              <div className="mt-2 truncate text-sm font-medium">{x.title}</div>
              <div className="truncate text-xs text-muted">
                {extraNames[x.extraType ?? "other"]}
                {x.durationMs ? ` · ${formatTrackTime(x.durationMs)}` : ""}
              </div>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** Other titles in the item's collections (film series). */
function CollectionShelves({ item }: { item: ItemDetail }) {
  return (item.collections ?? []).map((c) => <CollectionShelf key={c.id} collection={c} current={item.id} />);
}

function CollectionShelf({ collection, current }: { collection: ItemSummary; current: number }) {
  const members = useQuery(itemChildrenQuery(collection.id));
  const list = members.data?.items.filter((m) => m.id !== current) ?? [];
  if (!list.length) return null;
  return (
    <section className="mt-10">
      <h2 className="mb-4 text-lg font-semibold">
        <Link to="/item/$itemId" params={{ itemId: String(collection.id) }} className="hover:underline">
          {collection.title}
        </Link>
      </h2>
      <ul className="flex gap-4 overflow-x-auto pb-2">
        {list.map((r) => (
          <li key={r.id} className="w-36 shrink-0">
            <Link to="/item/$itemId" params={{ itemId: String(r.id) }} className="group block">
              <Poster item={r} width={180} className="group-hover:ring-2 group-hover:ring-accent" />
              <div className="mt-2 truncate text-sm font-medium">{r.title}</div>
              <div className="truncate text-xs text-muted">{subtitleFor(r)}</div>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

function Related({ item }: { item: ItemDetail }) {
  const music = item.type === "artist" || item.type === "album" || item.type === "track";
  const related = useQuery({
    queryKey: ["items", item.id, "related"],
    queryFn: () =>
      music
        ? unwrap(api.GET("/items/{itemId}/sonic-similar", { params: { path: { itemId: item.id } } })).catch(() => [])
        : unwrap(api.GET("/items/{itemId}/related", { params: { path: { itemId: item.id } } })),
    enabled: ["movie", "show", "artist", "album", "track"].includes(item.type),
  });
  if (!related.data?.length) return null;
  const square = music;
  return (
    <section className="mt-10">
      <h2 className="mb-4 text-lg font-semibold">{square ? (item.type === "track" ? "Sonically similar tracks" : "Sounds like") : "More like this"}</h2>
      <ul className="flex gap-4 overflow-x-auto pb-2">
        {related.data.map((r) => (
          <li key={r.id} className="w-36 shrink-0">
            <Link to="/item/$itemId" params={{ itemId: String(r.id) }} className="group block">
              <Poster item={r} shape={square ? "square" : "poster"} width={180} className="group-hover:ring-2 group-hover:ring-accent" />
              <div className="mt-2 truncate text-sm font-medium">{r.title}</div>
              <div className="truncate text-xs text-muted">{subtitleFor(r)}</div>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function ItemPage() {
  const { itemId } = useParams({ from: "/item/$itemId" });
  const item = useQuery(itemQuery(Number(itemId)));
  const me = useQuery(meQuery);
  const [editingRules, setEditingRules] = useState(false);
  if (item.isPending) return <Spinner />;
  if (item.isError) return <div className="p-8"><Alert tone="error">{item.error.message}</Alert></div>;
  const d = item.data;
  const square = d.type === "album" || d.type === "artist" || d.type === "track";
  const meta = [
    d.type === "episode" && d.parentTitle ? `${d.parentTitle} · Episode ${d.index}` : "",
    d.year ? String(d.year) : "",
    d.contentRating ?? "",
    formatDuration(d.durationMs),
    d.type === "show" || d.type === "artist" ? subtitleFor(d) : "",
  ].filter(Boolean);

  const backdrop = d.images?.backdrop;
  return (
    <div className="relative">
      {backdrop && (
        <div className="pointer-events-none absolute inset-x-0 top-0 h-[28rem] overflow-hidden" aria-hidden>
          <img src={imageUrl(backdrop, 1280)} alt="" className="size-full object-cover object-top opacity-35" />
          <div className="absolute inset-0 bg-gradient-to-t from-bg via-bg/60 to-transparent" />
          <div className="absolute inset-0 bg-gradient-to-r from-bg/80 to-transparent" />
        </div>
      )}
    <div className="relative p-6 lg:p-8">
      <div className="flex flex-col gap-8 sm:flex-row">
        <div className={square ? "w-48 shrink-0" : "w-44 shrink-0 sm:w-56"}>
          <Poster item={d} shape={d.type === "episode" ? "wide" : square ? "square" : "poster"} width={square ? 192 : 224} />
        </div>
        <div className="min-w-0 flex-1">
          <Breadcrumbs item={d} />
          {d.smartRules && (
            <div className="mb-1 flex items-center gap-1.5 text-xs font-semibold tracking-wider text-faint uppercase">
              <Sparkles className="size-3.5 text-accent" aria-hidden /> Smart collection
            </div>
          )}
          <h1 className="text-3xl font-bold">{d.title}</h1>
          {d.smartRules && <div className="mt-1 text-sm text-muted">{describeRules(d.smartRules)}</div>}
          {d.artistCredit && d.type !== "artist" && <div className="mt-1 text-lg text-muted">{d.artistCredit}</div>}
          <div className="mt-2 text-sm text-muted">{meta.join(" · ")}</div>
          {d.genres.length > 0 && <div className="mt-1 text-sm text-faint">{d.genres.join(", ")}</div>}
          <RatingBadges item={d} />
          <div className="mt-2">
            <Rating key={d.id} itemId={d.id} value={d.userRating} />
          </div>
          <PlayButtons key={d.id} item={d} />
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <ItemMenu item={d} />
            {d.type !== "collection" && (
              <PlayOnButton itemIds={[d.id]} className="rounded-full p-1.5 text-muted hover:bg-surface-3 hover:text-text" />
            )}
            {me.data?.isAdmin && <ItemActions item={d} />}
            {d.type === "collection" && <PinToHomeButton kind="collection" id={d.id} />}
            {d.smartRules && me.data?.isAdmin && (
              <Button variant="ghost" onClick={() => setEditingRules(true)}>
                <Sparkles className="size-4" /> Edit rules
              </Button>
            )}
          </div>
          {editingRules && <SmartRulesDialog collection={d} onClose={() => setEditingRules(false)} />}
          {d.tagline && <p className="mt-4 text-lg text-muted italic">{d.tagline}</p>}
          {!d.available && (
            <div className="mt-4 flex items-center gap-2 text-sm text-danger">
              <AlertTriangle className="size-4" aria-hidden /> The files for this item are currently unavailable.
            </div>
          )}
          {d.summary ? (
            <p className="mt-5 max-w-3xl leading-relaxed text-text/90">{d.summary}</p>
          ) : (
            (d.matchState === "unmatched" || d.matchState === "failed") &&
            (d.type === "movie" || d.type === "show") && (
              <p className="mt-5 text-sm text-faint">
                {d.matchState === "failed" ? "No metadata match found. Use ⋯ → Fix match to choose the right title." : "Not matched yet. Add a TMDB API key in Settings → Metadata."}
              </p>
            )
          )}
        </div>
      </div>
      <Children item={d} />
      <Extras item={d} />
      <CollectionShelves item={d} />
      <Cast credits={d.credits} />
      <Related item={d} />
      <MediaInfo item={d} isAdmin={!!me.data?.isAdmin} />
    </div>
    </div>
  );
}
