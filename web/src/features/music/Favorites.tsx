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
import { type FavoriteKind, favoritesQuery, SectionHeader } from "./MusicHome";
import { CommunityScore, RatingStars, RowRating } from "./Rating";

const tabs: { kind: FavoriteKind; label: string }[] = [
  { kind: "track", label: "Tracks" },
  { kind: "album", label: "Albums" },
  { kind: "artist", label: "Artists" },
];

/** Favorites: what the person rated 4★ or more, as Tracks / Albums / Artists. */
export function FavoritesPage({ libraryId, name }: { libraryId: number; name: string }) {
  const [kind, setKind] = useState<FavoriteKind>("track");
  const list = useQuery(favoritesQuery(libraryId, kind));
  const music = useMusicState();
  const items = list.data?.items ?? [];
  return (
    <div className="p-6 lg:p-8">
      <SectionHeader libraryId={libraryId} name={name} title="Favorites">
        {list.data && <span className="text-sm text-muted">{list.data.total.toLocaleString()} rated 4★ or more</span>}
      </SectionHeader>
      <div className="mb-6 flex flex-wrap items-center gap-3">
        <div className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="tablist" aria-label="Favorites">
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
            <Button size="sm" variant="primary" onClick={() => music.play(items, 0, { source: "Favorites" })}>
              <Play className="size-4 fill-current" /> Play
            </Button>
            <Button size="sm" onClick={() => music.play(items, 0, { shuffle: true, source: "Favorites" })}>
              <Shuffle className="size-4" /> Shuffle
            </Button>
          </div>
        )}
      </div>
      {list.isPending && <Spinner />}
      {list.isError && <Alert tone="error">{list.error.message}</Alert>}
      {list.data && items.length === 0 && (
        <p className="text-muted">
          Nothing here yet. {kind === "track" ? "Tracks" : kind === "album" ? "Albums" : "Artists"} you rate 4 stars or more show up here; rate with the stars on a page or in Now Playing.
        </p>
      )}
      {kind === "track" && items.length > 0 && (
        <ol className="divide-y divide-border rounded-lg border border-border bg-surface" data-testid="favorite-tracks">
          {items.map((t, i) => (
            <li key={t.id} className="group flex items-center gap-3 px-3 py-2 hover:bg-surface-2">
              <button
                type="button"
                onClick={() => music.play(items, i, { source: "Favorites" })}
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
