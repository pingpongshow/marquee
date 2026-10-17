import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Play, Shuffle } from "lucide-react";
import { useState } from "react";
import type { ItemSummary } from "@/api/types";
import { Alert, Button, Spinner } from "@/components/ui";
import { ItemMenu } from "../browse/ItemMenu";
import { Poster } from "../browse/Poster";
import { formatTrackTime, subtitleFor } from "../browse/format";
import { useMusicState } from "../player/MusicPlayer";
import { RowFavorite } from "../browse/Favorite";
import { type FavoriteKind, type FavoriteSource, favoritesQuery, SectionHeader } from "./MusicHome";
import { CommunityScore, RatingStars, RowRating } from "./Rating";

const tabs: { kind: FavoriteKind; label: string }[] = [
  { kind: "track", label: "Songs" },
  { kind: "album", label: "Albums" },
  { kind: "artist", label: "Artists" },
];

const sources: { source: FavoriteSource; label: string }[] = [
  { source: "hearts", label: "Favourites" },
  { source: "rated", label: "Rated 4★+" },
];

/**
 * Favourites: the person's favourite songs, albums and artists (the heart), newest first, or
 * what they rated 4★ or more. Favourite songs play or shuffle as a list.
 */
export function FavoritesPage({ libraryId, name }: { libraryId: number; name: string }) {
  const [kind, setKind] = useState<FavoriteKind>("track");
  const [source, setSource] = useState<FavoriteSource>("hearts");
  const list = useQuery(favoritesQuery(libraryId, kind, 500, source));
  const music = useMusicState();
  const items = list.data?.items ?? [];
  const what = kind === "track" ? "Songs" : kind === "album" ? "Albums" : "Artists";
  const playSource = source === "hearts" ? "Favourite songs" : "Rated 4★+";
  return (
    <div className="p-6 lg:p-8">
      <SectionHeader libraryId={libraryId} name={name} title={kind === "track" && source === "hearts" ? "Favourite songs" : "Favourites"}>
        {list.data && (
          <span className="text-sm text-muted">
            {list.data.total.toLocaleString()} {source === "hearts" ? (list.data.total === 1 ? "favourite" : "favourites") : "rated 4★ or more"}
          </span>
        )}
      </SectionHeader>
      <div className="mb-6 flex flex-wrap items-center gap-3">
        <div className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="tablist" aria-label="Which favourites">
          {sources.map((t) => (
            <button
              key={t.source}
              role="tab"
              aria-selected={source === t.source}
              onClick={() => setSource(t.source)}
              className={clsx("rounded px-3 py-1", source === t.source ? "bg-surface-3 text-text" : "text-muted hover:text-text")}
            >
              {t.label}
            </button>
          ))}
        </div>
        <div className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="tablist" aria-label="Favourites">
          {tabs.map((t) => (
            <button
              key={t.kind}
              role="tab"
              aria-selected={kind === t.kind}
              onClick={() => setKind(t.kind)}
              className={clsx("rounded px-3 py-1", kind === t.kind ? "bg-surface-3 text-text" : "text-muted hover:text-text")}
            >
              {t.label}
            </button>
          ))}
        </div>
        {kind === "track" && items.length > 0 && (
          <div className="flex gap-2">
            <Button size="sm" variant="primary" onClick={() => music.play(items, 0, { source: playSource })}>
              <Play className="size-4 fill-current" /> Play
            </Button>
            <Button size="sm" onClick={() => music.play(items, 0, { shuffle: true, source: playSource })}>
              <Shuffle className="size-4" /> Shuffle
            </Button>
          </div>
        )}
      </div>
      {list.isPending && <Spinner />}
      {list.isError && <Alert tone="error">{list.error.message}</Alert>}
      {list.data && items.length === 0 && (
        <p className="text-muted">
          {source === "hearts"
            ? `Nothing here yet. ${what} you heart show up here; tap the heart on a page, a song or in Now Playing.`
            : `Nothing here yet. ${what} you rate 4 stars or more show up here; rate with the stars on a page or in Now Playing.`}
        </p>
      )}
      {kind === "track" && items.length > 0 && (
        <ol className="divide-y divide-border rounded-lg border border-border bg-surface" data-testid="favorite-tracks">
          {items.map((t, i) => (
            <li key={t.id} className="group flex items-center gap-3 px-3 py-2 hover:bg-surface-2">
              <button
                type="button"
                onClick={() => music.play(items, i, { source: playSource })}
                className={clsx("flex min-w-0 flex-1 items-center gap-3 text-left", music.current?.item.id === t.id && "text-accent")}
              >
                <span className="w-10 shrink-0">
                  <Poster item={t} shape="square" width={80} compact />
                </span>
                <span className="min-w-0 flex-1">
                  <span className={clsx("block truncate text-sm", !t.available && "text-faint line-through")}>{t.title}</span>
                  <span className="block truncate text-xs text-muted">{subtitleFor(t)}</span>
                </span>
              </button>
              <CommunityScore rating={t.communityRating} className="hidden sm:inline-flex" />
              <RowFavorite item={t} />
              <RowRating item={t} />
              <span className="w-12 shrink-0 text-right text-sm text-muted tabular-nums">{formatTrackTime(t.durationMs)}</span>
              <ItemMenu item={t} />
            </li>
          ))}
        </ol>
      )}
      {kind !== "track" && items.length > 0 && (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6" data-testid={`favorite-${kind}s`}>
          {items.map((it) => (
            <FavoriteCard key={it.id} it={it} />
          ))}
        </ul>
      )}
    </div>
  );
}

function FavoriteCard({ it }: { it: ItemSummary }) {
  return (
    <li>
      <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
        <Poster item={it} shape="square" className="group-hover:ring-2 group-hover:ring-accent" />
        <div className="mt-2 truncate text-sm font-medium">{it.title}</div>
        <div className="truncate text-xs text-muted">{subtitleFor(it)}</div>
        <div className="mt-0.5 flex items-center gap-2">
          <RatingStars value={it.userRating} />
          <CommunityScore rating={it.communityRating} />
        </div>
      </Link>
    </li>
  );
}
