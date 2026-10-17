import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Disc3, Plus, RotateCcw, Search } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { itemQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import type { ItemDetail, ItemSummary } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Spinner } from "@/components/ui";

type MusicCandidate = components["schemas"]["MusicMatchCandidate"];

/** After a music fix: the item (and the albums and artists around it) changed. */
function useMusicUpdated() {
  const qc = useQueryClient();
  return (item: ItemDetail) => {
    qc.setQueryData(["items", item.id], item);
    qc.invalidateQueries({ queryKey: ["items"] });
    qc.invalidateQueries({ queryKey: ["libraries"] });
  };
}

const typeLabels: Record<string, string> = { ep: "EP", person: "Person", group: "Group", album: "Album", single: "Single", live: "Live", compilation: "Compilation", soundtrack: "Soundtrack", remix: "Remix", demo: "Demo" };
const typeLabel = (t?: string) => (t ? (typeLabels[t] ?? t.charAt(0).toUpperCase() + t.slice(1)) : "");

function candidateDetails(c: MusicCandidate, album: boolean): string {
  const years = c.beginYear ? `${c.beginYear}–${c.endYear ?? ""}` : "";
  const parts = album ? [c.artist, typeLabel(c.type), c.year, c.trackCount ? `${c.trackCount} tracks` : ""] : [typeLabel(c.type), c.country, years];
  return parts.filter(Boolean).join(" · ");
}

/** Fix match for a music artist or album: pick the MusicBrainz artist or release group it is. */
export function MusicMatchDialog({ item, onClose }: { item: ItemDetail; onClose: () => void }) {
  const updated = useMusicUpdated();
  const album = item.type === "album";
  const [q, setQ] = useState(item.title);
  const [artist, setArtist] = useState(album ? (item.parentTitle ?? item.artistCredit ?? "") : "");
  const [query, setQuery] = useState({ q: item.title, artist });
  const cands = useQuery({
    queryKey: ["music-match", item.id, query],
    queryFn: () =>
      unwrap(api.GET("/items/{itemId}/music-match", { params: { path: { itemId: item.id }, query: { q: query.q || undefined, artist: (album && query.artist) || undefined } } })),
  });
  const apply = useMutation({
    mutationFn: (mbid: string) => unwrap(api.PUT("/items/{itemId}/music-match", { params: { path: { itemId: item.id } }, body: { mbid } })),
    onSuccess: (it) => {
      updated(it);
      onClose();
    },
  });
  const search = (e: FormEvent) => {
    e.preventDefault();
    setQuery({ q: q.trim(), artist: artist.trim() });
  };
  return (
    <Dialog open wide onClose={onClose} title="Fix match">
      <p className="mb-3 text-sm text-muted">
        Choose the MusicBrainz {album ? "album" : "artist"}. Its details replace the current ones; fields you edited stay as they are. A MusicBrainz link or ID works too.
      </p>
      <form onSubmit={search} className="mb-4 flex flex-wrap gap-2 sm:flex-nowrap">
        <Input aria-label={album ? "Album" : "Artist"} placeholder={album ? "Album title" : "Artist name"} value={q} onChange={(e) => setQ(e.target.value)} />
        {album && <Input aria-label="Artist" placeholder="Artist" className="sm:w-56" value={artist} onChange={(e) => setArtist(e.target.value)} />}
        <Button type="submit" variant="secondary">
          <Search className="size-4" /> Search
        </Button>
      </form>
      {(cands.isError || apply.isError) && <Alert tone="error">{(cands.error ?? apply.error)?.message}</Alert>}
      {cands.isFetching && <Spinner label="Searching MusicBrainz" />}
      {cands.data?.length === 0 && <p className="py-6 text-center text-sm text-muted">No matches. Try a different spelling{album ? " or clear the artist" : ""}.</p>}
      <ul className="space-y-2">
        {cands.data?.map((c) => (
          <li key={c.mbid}>
            <button
              disabled={apply.isPending}
              onClick={() => apply.mutate(c.mbid)}
              className="flex w-full flex-col rounded-lg border border-border p-3 text-left hover:border-accent hover:bg-surface-2 disabled:opacity-60"
            >
              <span className="flex flex-wrap items-center gap-2 font-medium">
                {c.name}
                {c.disambiguation && <span className="font-normal text-muted">({c.disambiguation})</span>}
                {c.current && (
                  <span className="flex items-center gap-1 rounded bg-success/15 px-1.5 text-[11px] text-success">
                    <Check className="size-3" /> Current
                  </span>
                )}
              </span>
              {candidateDetails(c, album) && <span className="mt-0.5 block text-sm text-muted">{candidateDetails(c, album)}</span>}
            </button>
          </li>
        ))}
      </ul>
      {apply.isPending && <Spinner label="Looking up details" />}
    </Dialog>
  );
}

/** Move a track to another album (or back to where its tags put it), without editing the file. */
export function MoveTrackDialog({ track, onClose }: { track: ItemSummary; onClose: () => void }) {
  const updated = useMusicUpdated();
  const detail = useQuery(itemQuery(track.id));
  const [mode, setMode] = useState<"existing" | "new">("existing");
  const [q, setQ] = useState(track.parentTitle ?? "");
  const [query, setQuery] = useState(track.parentTitle ?? "");
  const [albumArtist, setAlbumArtist] = useState(track.grandparentTitle ?? "");
  const [albumTitle, setAlbumTitle] = useState("");
  const albums = useQuery({
    queryKey: ["search", "albums", query],
    enabled: mode === "existing" && query.length > 0,
    queryFn: () => unwrap(api.GET("/search", { params: { query: { q: query, limit: 50 } } })),
    select: (r) => (r.groups.find((g) => g.type === "album")?.items ?? []).filter((a) => a.libraryId === track.libraryId),
  });
  const place = useMutation({
    mutationFn: (body: components["schemas"]["TrackPlacementRequest"]) =>
      unwrap(api.PUT("/items/{itemId}/music-placement", { params: { path: { itemId: track.id } }, body })),
    onSuccess: (it) => {
      updated(it);
      onClose();
    },
  });
  const undo = useMutation({
    mutationFn: () => unwrap(api.DELETE("/items/{itemId}/music-placement", { params: { path: { itemId: track.id } } })),
    onSuccess: (it) => {
      updated(it);
      onClose();
    },
  });
  const error = place.error ?? undo.error;
  const busy = place.isPending || undo.isPending;
  return (
    <Dialog open wide onClose={onClose} title={`Move “${track.title}”`}>
      <p className="mb-3 text-sm text-muted">The file isn't changed. The track keeps its plays, ratings and favourite, and stays on the album you choose when the library is scanned again.</p>
      {detail.data?.placementOverride && (
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-surface-2 px-3 py-2 text-sm">
          <span className="text-muted">This track was moved by hand.</span>
          <Button variant="secondary" size="sm" loading={undo.isPending} disabled={busy} onClick={() => undo.mutate()}>
            <RotateCcw className="size-4" /> Use the file's tags
          </Button>
        </div>
      )}
      <div className="mb-4 flex gap-2" role="tablist">
        <Button variant={mode === "existing" ? "primary" : "ghost"} size="sm" role="tab" aria-selected={mode === "existing"} onClick={() => setMode("existing")}>
          <Disc3 className="size-4" /> An album in the library
        </Button>
        <Button variant={mode === "new" ? "primary" : "ghost"} size="sm" role="tab" aria-selected={mode === "new"} onClick={() => setMode("new")}>
          <Plus className="size-4" /> New album
        </Button>
      </div>
      {error && (
        <div className="mb-4">
          <Alert tone="error">{error.message}</Alert>
        </div>
      )}
      {mode === "existing" ? (
        <>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setQuery(q.trim());
            }}
            className="mb-4 flex gap-2"
          >
            <Input aria-label="Album" placeholder="Search albums" value={q} onChange={(e) => setQ(e.target.value)} />
            <Button type="submit" variant="secondary">
              <Search className="size-4" /> Search
            </Button>
          </form>
          {albums.isFetching && <Spinner label="Searching" />}
          {albums.data?.length === 0 && <p className="py-6 text-center text-sm text-muted">No albums found. Try another title, or make a new album.</p>}
          <ul className="space-y-2">
            {albums.data?.map((a) => (
              <li key={a.id}>
                <button
                  disabled={busy || a.id === track.parentId}
                  onClick={() => place.mutate({ albumId: a.id })}
                  className="flex w-full items-center gap-3 rounded-lg border border-border p-2 text-left hover:border-accent hover:bg-surface-2 disabled:opacity-60"
                >
                  <span className="size-12 shrink-0 overflow-hidden rounded bg-surface-3">{a.images?.poster && <img src={imageUrl(a.images.poster, 96)} alt="" loading="lazy" className="size-full object-cover" />}</span>
                  <span className="min-w-0">
                    <span className="flex items-center gap-2 truncate font-medium">
                      {a.title}
                      {a.id === track.parentId && <span className="rounded bg-surface-3 px-1.5 text-[11px] font-normal text-muted">Current</span>}
                    </span>
                    <span className="block truncate text-sm text-muted">{[a.artistCredit ?? a.parentTitle, a.year].filter(Boolean).join(" · ")}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            place.mutate({ albumArtist: albumArtist.trim(), album: albumTitle.trim() });
          }}
          className="space-y-4"
        >
          <Field label="Album artist">{(id) => <Input id={id} value={albumArtist} onChange={(e) => setAlbumArtist(e.target.value)} />}</Field>
          <Field label="Album title" help="An album with this artist and title is used if there is one; otherwise it's created.">
            {(id) => <Input id={id} value={albumTitle} onChange={(e) => setAlbumTitle(e.target.value)} />}
          </Field>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" loading={place.isPending} disabled={busy || !albumArtist.trim() || !albumTitle.trim()}>
              Move
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
