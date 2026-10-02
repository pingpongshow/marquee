import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Layers, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import type { ItemSummary } from "@/api/types";
import { Alert, Button, Dialog, Input, Spinner } from "@/components/ui";
import { Poster } from "./Poster";
import { subtitleFor } from "./format";

/** Admins: put an item in one of its library's collections, or a new one (META-7). */
export function AddToCollectionDialog({ item, onClose }: { item: Pick<ItemSummary, "id" | "libraryId" | "title">; onClose: () => void }) {
  const qc = useQueryClient();
  const lists = useQuery({
    queryKey: ["libraries", item.libraryId, "items", "collections-all"],
    queryFn: () => unwrap(api.GET("/libraries/{libraryId}/items", { params: { path: { libraryId: item.libraryId }, query: { type: "collection", limit: 500 } } })),
  });
  const [title, setTitle] = useState("");
  const [done, setDone] = useState<string | null>(null);
  const add = useMutation({
    mutationFn: async ({ c, name }: { c?: ItemSummary; name?: string }) => {
      if (c) {
        await unwrap(api.POST("/collections/{collectionId}/items", { params: { path: { collectionId: c.id } }, body: { itemIds: [item.id] } }));
        return c.title;
      }
      const made = await unwrap(api.POST("/libraries/{libraryId}/collections", { params: { path: { libraryId: item.libraryId } }, body: { title: name!, itemIds: [item.id] } }));
      return made.title;
    },
    onSuccess: (name) => {
      qc.invalidateQueries({ queryKey: ["items"] });
      qc.invalidateQueries({ queryKey: ["libraries", item.libraryId, "items"] });
      setDone(name);
      window.setTimeout(onClose, 900);
    },
  });
  const create = (e: FormEvent) => {
    e.preventDefault();
    if (title.trim()) add.mutate({ name: title.trim() });
  };
  return (
    <Dialog open onClose={onClose} title={`Add “${item.title}” to a collection`}>
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
        <ul className="max-h-80 space-y-1 overflow-y-auto">
          {lists.data?.items.map((c) => (
            <li key={c.id}>
              <button onClick={() => add.mutate({ c })} disabled={add.isPending} className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-surface-2 disabled:opacity-50">
                <div className="w-9 shrink-0">
                  <Poster item={c} width={60} compact />
                </div>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{c.title}</span>
                  <span className="block text-xs text-muted">{subtitleFor(c)}</span>
                </span>
                <Layers className="size-4 text-muted" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
        {lists.data?.items.length === 0 && <p className="text-center text-sm text-muted">No collections yet. Create one above.</p>}
      </div>
    </Dialog>
  );
}
