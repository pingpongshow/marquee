import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ListPlus, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { playlistsQuery } from "@/api/queries";
import type { ItemSummary, PlaylistKind } from "@/api/types";
import { Alert, Button, Dialog, Input, Spinner } from "@/components/ui";
import { PlaylistMosaic } from "./PlaylistMosaic";

const musicTypes = new Set(["track", "album", "artist"]);
export const playlistKindFor = (item: Pick<ItemSummary, "type">): PlaylistKind => (musicTypes.has(item.type) ? "audio" : "video");

/** Picks a playlist (or creates one) and adds the item to it. */
export function AddToPlaylistDialog({ item, onClose }: { item: Pick<ItemSummary, "id" | "type" | "title">; onClose: () => void }) {
  const kind = playlistKindFor(item);
  const qc = useQueryClient();
  const lists = useQuery(playlistsQuery(kind));
  const [title, setTitle] = useState("");
  const [done, setDone] = useState<string | null>(null);
  const add = useMutation({
    mutationFn: async ({ id, name }: { id?: number; name?: string }) => {
      if (id) return unwrap(api.POST("/playlists/{playlistId}/items", { params: { path: { playlistId: id } }, body: { itemIds: [item.id] } }));
      return unwrap(api.POST("/playlists", { body: { title: name!, kind, itemIds: [item.id] } }));
    },
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["playlists"] });
      setDone(p.title);
      window.setTimeout(onClose, 900);
    },
  });
  const create = (e: FormEvent) => {
    e.preventDefault();
    if (title.trim()) add.mutate({ name: title.trim() });
  };
  return (
    <Dialog open onClose={onClose} title={`Add “${item.title}” to a playlist`}>
      <div className="space-y-4">
        {done && <Alert tone="success">Added to {done}.</Alert>}
        {add.isError && <Alert tone="error">{add.error.message}</Alert>}
        <form onSubmit={create} className="flex gap-2">
          <Input aria-label="New playlist name" placeholder={kind === "audio" ? "New music playlist" : "New video playlist"} value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
          <Button type="submit" variant="primary" disabled={!title.trim()} loading={add.isPending && !!title}>
            <Plus className="size-4" /> Create
          </Button>
        </form>
        {lists.isPending && <Spinner />}
        <ul className="max-h-80 space-y-1 overflow-y-auto">
          {lists.data?.map((p) => (
            <li key={p.id}>
              <button
                onClick={() => add.mutate({ id: p.id })}
                disabled={add.isPending}
                className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-surface-2 disabled:opacity-50"
              >
                <PlaylistMosaic playlist={p} className="size-11" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{p.title}</span>
                  <span className="block text-xs text-muted">{p.itemCount} items</span>
                </span>
                <ListPlus className="size-4 text-muted" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
        {lists.data?.length === 0 && <p className="text-center text-sm text-muted">No {kind === "audio" ? "music" : "video"} playlists yet. Create one above.</p>}
      </div>
    </Dialog>
  );
}
