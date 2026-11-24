import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { ChevronLeft, Combine, ImagePlus, ListChecks, Pencil, Share2, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, playlistItemsQuery } from "@/api/queries";
import type { ItemSummary, Playlist } from "@/api/types";
import { Alert, Button, Dialog, Input, Spinner } from "@/components/ui";
import { subtitleFor } from "../browse/format";
import { useTileMenu } from "../browse/ItemMenu";
import { Poster } from "../browse/Poster";
import { PinToHomeButton } from "../home/EditHome";
import { CombinePlaylistsDialog } from "./Combine";
import { PictureDialog } from "./Picture";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { countText, RemoveSharedDialog, ShareDialog, SharedIcon, sharedByText, sharedText } from "./Sharing";

/** A title in a collection: its poster, opening its page. */
function Tile({ item: it, shape }: { item: ItemSummary; shape: "poster" | "wide" }) {
  const tile = useTileMenu(it);
  const episode = it.type === "episode";
  return (
    <li className="group/tile relative min-w-0" onContextMenu={tile.onContextMenu}>
      <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
        <Poster item={it} shape={shape} width={shape === "wide" ? 320 : 240} className="transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent" />
        <div className="mt-2 truncate text-sm font-medium" title={it.title}>
          {episode ? (it.grandparentTitle ?? it.title) : it.title}
        </div>
        <div className="truncate text-xs text-muted">{episode ? `${it.parentTitle ?? ""} · E${it.index ?? ""} · ${it.title}` : subtitleFor(it)}</div>
      </Link>
      {tile.menu}
    </li>
  );
}

/**
 * A person's collection (USER-23): titles from one video library as a grid of posters, each
 * opening its own page. Nothing plays from here. Its owner renames, edits, shares, pins and
 * deletes it; someone it's shared with can only take it out of theirs.
 */
export function CollectionPage({ collection: c }: { collection: Playlist }) {
  const id = c.id;
  const qc = useQueryClient();
  const navigate = useNavigate();
  const entries = useQuery(playlistItemsQuery(id));
  const library = useQuery(librariesQuery).data?.find((l) => l.id === c.libraryId);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [combining, setCombining] = useState(false);
  const [picturing, setPicturing] = useState(false);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["playlists"] });
  // Back to where collections live: the library's Collections tab.
  const leave = () => (c.libraryId ? navigate({ to: "/library/$libraryId", params: { libraryId: String(c.libraryId) }, search: { show: "collections" } }) : navigate({ to: "/" }));

  const rename = useMutation({
    mutationFn: (title: string) => unwrap(api.PATCH("/playlists/{playlistId}", { params: { path: { playlistId: id } }, body: { title } })),
    onSuccess: () => {
      setRenaming(null);
      void invalidate();
    },
  });
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE("/playlists/{playlistId}", { params: { path: { playlistId: id } } })),
    onSuccess: () => {
      void invalidate();
      void leave();
    },
  });

  const items = entries.data?.items.map((e) => e.item) ?? [];
  const shape = library?.type === "videos" ? "wide" : "poster";
  return (
    <div className="p-6 lg:p-8" data-testid="collection-page">
      {c.libraryId && (
        <Link to="/library/$libraryId" params={{ libraryId: String(c.libraryId) }} search={{ show: "collections" }} className="mb-1 inline-flex items-center gap-1 text-sm text-muted hover:text-text">
          <ChevronLeft className="size-4" aria-hidden /> {library?.name ?? "Library"}
        </Link>
      )}
      <header className="mb-6 flex flex-col gap-5 sm:flex-row sm:items-end">
        {/* Its own picture when it has one (MUSIC-30); the grid below already shows what's in it. */}
        {c.imageVersion && <PlaylistMosaic playlist={c} size={400} className={clsx("shadow-xl", shape === "wide" ? "aspect-video w-56" : "aspect-[2/3] w-32")} />}
        <div className="min-w-0 flex-1">
        <div className="text-xs font-semibold tracking-wider text-faint uppercase">Collection</div>
        {renaming === null ? (
          <h1 className="mt-1 flex items-center gap-2 text-3xl font-bold">
            <span className="truncate">{c.title}</span>
            {!c.readOnly && (
              <button onClick={() => setRenaming(c.title)} className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Rename collection">
                <Pencil className="size-4" />
              </button>
            )}
          </h1>
        ) : (
          <form
            className="mt-1 flex max-w-md gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (renaming.trim()) rename.mutate(renaming.trim());
            }}
          >
            <Input aria-label="Collection name" autoFocus value={renaming} maxLength={200} onChange={(e) => setRenaming(e.target.value)} />
            <Button type="submit" variant="primary" loading={rename.isPending}>
              Save
            </Button>
            <Button type="button" variant="ghost" onClick={() => setRenaming(null)}>
              Cancel
            </Button>
          </form>
        )}
        <div className="mt-2 text-sm text-muted">{countText(c)}</div>
        {(c.readOnly || sharedText(c)) && (
          <div className="mt-1 flex items-center gap-1.5 text-sm text-muted" data-testid="playlist-sharing-note">
            <SharedIcon playlist={c} />
            {c.readOnly ? sharedByText(c) : sharedText(c)}
          </div>
        )}
        <div className="mt-4 flex flex-wrap gap-3">
          {!c.readOnly && (
            <Link
              to="/playlist/$playlistId/edit"
              params={{ playlistId: String(id) }}
              className="inline-flex h-10 items-center gap-2 rounded-md px-4 text-sm font-medium text-muted hover:bg-surface-2 hover:text-text"
              data-testid="edit-playlist"
            >
              <ListChecks className="size-4" /> Edit
            </Link>
          )}
          {!c.readOnly && c.libraryId && (
            <Button variant="ghost" onClick={() => setCombining(true)}>
              <Combine className="size-4" /> Combine
            </Button>
          )}
          {!c.readOnly && (
            <Button variant="ghost" onClick={() => setPicturing(true)}>
              <ImagePlus className="size-4" /> Change picture
            </Button>
          )}
          <PinToHomeButton kind="playlist" id={id} />
          {!c.readOnly && (
            <Button variant="ghost" onClick={() => setSharing(true)}>
              <Share2 className="size-4" /> Share
            </Button>
          )}
          {c.readOnly ? (
            <Button variant="ghost" onClick={() => setRemoving(true)}>
              <Trash2 className="size-4" /> Remove
            </Button>
          ) : (
            <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="size-4" /> Delete
            </Button>
          )}
        </div>
        </div>
      </header>
      {rename.isError && <Alert tone="error">{rename.error.message}</Alert>}
      {entries.isPending && <Spinner />}
      {entries.isError && <Alert tone="error">{entries.error.message}</Alert>}
      {entries.isSuccess && items.length === 0 && (
        <p className="text-muted">{c.readOnly ? "This collection is empty." : `This collection is empty. Use “Add to my collection” on any title in ${library?.name ?? "its library"}.`}</p>
      )}
      <ul className={clsx("grid gap-x-4 gap-y-6", shape === "wide" ? "grid-cols-[repeat(auto-fill,minmax(240px,1fr))]" : "grid-cols-[repeat(auto-fill,minmax(150px,1fr))]")}>
        {items.map((it) => (
          <Tile key={it.id} item={it} shape={shape} />
        ))}
      </ul>
      <Dialog
        open={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        title="Delete collection?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
              Cancel
            </Button>
            <Button variant="danger" loading={del.isPending} onClick={() => del.mutate()}>
              Delete
            </Button>
          </>
        }
      >
        {del.isError && (
          <div className="mb-3">
            <Alert tone="error">{del.error.message}</Alert>
          </div>
        )}
        <p className="text-sm text-muted">“{c.title}” will be deleted. The titles in it aren’t affected.</p>
      </Dialog>
      {combining && c.libraryId && <CombinePlaylistsDialog scope={{ kind: "video", libraryId: c.libraryId }} initial={[id]} onClose={() => setCombining(false)} />}
      {picturing && <PictureDialog playlist={c} onClose={() => setPicturing(false)} />}
      {sharing && <ShareDialog playlist={c} onClose={() => setSharing(false)} />}
      {removing && <RemoveSharedDialog playlist={c} onClose={() => setRemoving(false)} onRemoved={() => void leave()} />}
    </div>
  );
}
