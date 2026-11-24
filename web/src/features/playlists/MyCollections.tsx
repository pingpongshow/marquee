import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { playlistsQuery } from "@/api/queries";
import type { ItemSummary, Playlist } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Spinner } from "@/components/ui";
import { useLater } from "@/lib/useLater";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { countText, SharedIcon, sharedByText } from "./Sharing";

/**
 * The person's own collections of a video library (USER-23), and the ones shared with them,
 * as tiles on the library's Collections tab. A collection is a playlist of kind video.
 */
export function MyCollections({ collections, shape }: { collections: Playlist[]; shape: "poster" | "wide" }) {
  return (
    <ul className={clsx("grid gap-x-4 gap-y-6", shape === "wide" ? "grid-cols-[repeat(auto-fill,minmax(240px,1fr))]" : "grid-cols-[repeat(auto-fill,minmax(150px,1fr))]")}>
      {collections.map((c) => (
        <li key={c.id} className="min-w-0">
          <Link to="/playlist/$playlistId" params={{ playlistId: String(c.id) }} className="group block">
            <PlaylistMosaic
              playlist={c}
              size={400}
              className={clsx("w-full shadow-md transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent", shape === "wide" ? "aspect-video" : "aspect-[2/3]")}
            />
            <div className="mt-2 flex items-center gap-1.5">
              <span className="truncate text-sm font-medium" title={c.title}>
                {c.title}
              </span>
              <SharedIcon playlist={c} />
            </div>
            <div className="truncate text-xs text-muted">{c.readOnly ? `${sharedByText(c)} · ${countText(c)}` : countText(c)}</div>
          </Link>
        </li>
      ))}
    </ul>
  );
}

/** Names a new collection in a library and opens it. */
export function NewCollectionDialog({ libraryId, onClose }: { libraryId: number; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/playlists", { body: { title: title.trim(), kind: "video", libraryId } })),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      void navigate({ to: "/playlist/$playlistId", params: { playlistId: String(c.id) } });
    },
  });
  const go = () => {
    if (title.trim() && !create.isPending) create.mutate();
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    go();
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title="New collection"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!title.trim()} loading={create.isPending} onClick={go}>
            Create
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        {create.isError && <Alert tone="error">{create.error.message}</Alert>}
        <Field label="Name">{(id) => <Input id={id} autoFocus maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
      </form>
    </Dialog>
  );
}

/**
 * Picks one of the person's collections in the item's library (or makes one) and adds the item
 * to it as it is: a show stays a show.
 */
export function AddToMyCollectionDialog({ item, onClose }: { item: Pick<ItemSummary, "id" | "title" | "libraryId">; onClose: () => void }) {
  const qc = useQueryClient();
  const lists = useQuery(playlistsQuery("video", item.libraryId));
  // A collection someone shared with you can't be changed, so it's no target.
  const own = lists.data?.filter((c) => !c.readOnly);
  const [title, setTitle] = useState("");
  const [done, setDone] = useState<string | null>(null);
  const later = useLater();
  const add = useMutation({
    mutationFn: ({ id, name }: { id?: number; name?: string }) =>
      id
        ? unwrap(api.POST("/playlists/{playlistId}/items", { params: { path: { playlistId: id } }, body: { itemIds: [item.id] } }))
        : unwrap(api.POST("/playlists", { body: { title: name!, kind: "video", libraryId: item.libraryId, itemIds: [item.id] } })),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      setDone(c.title);
      later(onClose, 900);
    },
  });
  const create = (e: FormEvent) => {
    e.preventDefault();
    if (title.trim()) add.mutate({ name: title.trim() });
  };
  return (
    <Dialog open onClose={onClose} title={`Add “${item.title}” to my collection`}>
      <div className="space-y-4">
        {done && <Alert tone="success">Added to {done}.</Alert>}
        {add.isError && <Alert tone="error">{add.error.message}</Alert>}
        <form onSubmit={create} className="flex gap-2">
          <Input aria-label="New collection name" placeholder="New collection" value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
          <Button type="submit" variant="primary" disabled={!title.trim()} loading={add.isPending && !!title}>
            <Plus className="size-4" /> Create
          </Button>
        </form>
        {lists.isPending && <Spinner />}
        {lists.isError && <Alert tone="error">{lists.error.message}</Alert>}
        <ul className="max-h-80 space-y-1 overflow-y-auto">
          {own?.map((c) => (
            <li key={c.id}>
              <button onClick={() => add.mutate({ id: c.id })} disabled={add.isPending} className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-surface-2 disabled:opacity-50">
                <PlaylistMosaic playlist={c} className="size-11" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{c.title}</span>
                  <span className="block text-xs text-muted">{countText(c)}</span>
                </span>
                <Plus className="size-4 text-muted" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
        {own?.length === 0 && <p className="text-center text-sm text-muted">No collections in this library yet. Create one above.</p>}
      </div>
    </Dialog>
  );
}
