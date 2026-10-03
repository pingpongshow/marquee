import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ListPlus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Dialog, Field, Input } from "@/components/ui";
import { toast } from "@/components/Toast";

const MAX = 500;

/** Track ids in order, each once, at most 500 (a playlist from a queue or station). */
export function playlistIds(ids: number[]) {
  return [...new Set(ids)].slice(0, MAX);
}

/** "Queue – 2 Oct 2026" for a queue with no station or album behind it. */
export function queueTitle(date = new Date()) {
  return `Queue – ${date.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}`;
}

/**
 * Saves tracks (a Muse mix, radio, Sound Journey, daily mix or the queue) as an audio playlist,
 * with an editable title; a toast links to the new playlist.
 */
export function SaveAsPlaylistDialog({ defaultTitle, itemIds, onClose }: { defaultTitle: string; itemIds: number[]; onClose: () => void }) {
  const qc = useQueryClient();
  const [title, setTitle] = useState(defaultTitle.slice(0, 200));
  const ids = playlistIds(itemIds);
  const save = useMutation({
    mutationFn: () => unwrap(api.POST("/playlists", { body: { title: title.trim(), kind: "audio", itemIds: ids } })),
    onSuccess: (pl) => {
      qc.invalidateQueries({ queryKey: ["playlists"] });
      toast(`Saved “${pl.title}”.`, { link: { label: "Open playlist", to: "/playlist/$playlistId", params: { playlistId: String(pl.id) } } });
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (title.trim()) save.mutate();
  };
  return (
    <Dialog open onClose={onClose} title="Save as playlist">
      <form onSubmit={submit} className="space-y-4">
        <Field label="Title" help={ids.length === 1 ? "1 track" : `${ids.length} tracks`}>
          {(id) => <Input id={id} autoFocus value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />}
        </Field>
        {save.isError && <Alert tone="error">{save.error.message}</Alert>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!title.trim() || !ids.length}>
            <ListPlus className="size-4" /> Save playlist
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
