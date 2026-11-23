import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Sparkles } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { playlistsQuery } from "@/api/queries";
import type { Playlist } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { SharedIcon } from "./Sharing";

/**
 * Combines playlists (MUSIC-29): yours, ones shared with you and smart ones (as they are now),
 * in the order picked, into a new playlist or onto the end of one of yours, e.g. several
 * Christmas playlists into one. Music and video playlists don't mix.
 */
export function CombinePlaylistsDialog({ initial = [], into, onClose }: { initial?: number[]; into?: Playlist; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const lists = useQuery(playlistsQuery());
  const [picked, setPicked] = useState<number[]>(initial.filter((id) => id !== into?.id));
  const [dest, setDest] = useState<"new" | number>(into?.id ?? "new");
  const [title, setTitle] = useState("");
  const [skipDuplicates, setSkipDuplicates] = useState(true);
  const [deleteSources, setDeleteSources] = useState(false);

  const all = lists.data ?? [];
  const byId = new Map(all.map((p) => [p.id, p]));
  const chosen = picked.map((id) => byId.get(id)).filter((p): p is Playlist => !!p);
  // The kind is the first pick's (or the destination's); the other kind can't be picked.
  const kind = chosen[0]?.kind ?? (typeof dest === "number" ? byId.get(dest)?.kind : undefined);
  const destinations = all.filter((p) => !p.readOnly && !p.rules && (!kind || p.kind === kind) && !picked.includes(p.id));
  const ownSources = chosen.filter((p) => !p.readOnly).length;
  const toggle = (p: Playlist) => {
    // A playlist picked to go in can't also be where they go: back to a new one.
    if (dest === p.id) setDest("new");
    setPicked((now) => (now.includes(p.id) ? now.filter((id) => id !== p.id) : [...now, p.id]));
  };

  const merge = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/playlists/merge", {
          body: {
            sourceIds: picked,
            intoId: dest === "new" ? undefined : dest,
            title: dest === "new" ? title.trim() || undefined : undefined,
            skipDuplicates,
            deleteSources,
          },
        }),
      ),
    onSuccess: (p) => {
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      onClose();
      void navigate({ to: "/playlist/$playlistId", params: { playlistId: String(p.id) } });
    },
  });
  const ready = chosen.length > 0 && (dest !== "new" || chosen.length > 1 || !!title.trim());

  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title={into ? `Add playlists to “${into.title}”` : "Combine playlists"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!ready} loading={merge.isPending} onClick={() => merge.mutate()} data-testid="combine-submit">
            {dest === "new" ? "Combine" : "Add"}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {merge.isError && <Alert tone="error">{merge.error.message}</Alert>}
        <p className="text-sm text-muted">Pick the playlists in the order they should go in. Smart playlists add what’s in them now.</p>
        {lists.isPending && <Spinner />}
        <ul className="max-h-72 space-y-1 overflow-y-auto rounded-md border border-border p-1" data-testid="combine-sources">
          {all
            .filter((p) => p.id !== into?.id)
            .map((p) => {
              const at = picked.indexOf(p.id);
              const off = !!kind && p.kind !== kind && at < 0;
              return (
                <li key={p.id}>
                  <label className={clsx("flex cursor-pointer items-center gap-3 rounded px-2 py-1.5 hover:bg-surface-2", off && "cursor-not-allowed opacity-40")}>
                    <input type="checkbox" className="accent-accent" checked={at >= 0} disabled={off} onChange={() => toggle(p)} aria-label={p.title} />
                    <PlaylistMosaic playlist={p} size={80} className="size-9 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5 truncate text-sm">
                        {p.rules && <Sparkles className="size-3.5 shrink-0 text-accent" aria-label="Smart playlist" />}
                        <span className="truncate">{p.title}</span>
                        <SharedIcon playlist={p} />
                      </span>
                      <span className="block text-xs text-muted">
                        {p.itemCount} {p.kind === "audio" ? "tracks" : "items"}
                      </span>
                    </span>
                    {at >= 0 && <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent text-xs font-semibold text-on-accent">{at + 1}</span>}
                  </label>
                </li>
              );
            })}
        </ul>
        {!into && (
          <Field label="Into">
            {(id) => (
              <Select id={id} value={String(dest)} onChange={(e) => setDest(e.target.value === "new" ? "new" : Number(e.target.value))}>
                <option value="new">A new playlist</option>
                {destinations.map((p) => (
                  <option key={p.id} value={p.id}>
                    The end of “{p.title}”
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}
        {dest === "new" && (
          <Field label="New playlist’s name">
            {(id) => <Input id={id} maxLength={200} value={title} placeholder={chosen[0]?.title ?? "Name"} onChange={(e) => setTitle(e.target.value)} />}
          </Field>
        )}
        <Toggle label="Skip duplicates" help="Each song or video goes in once, even if it’s in several of them." checked={skipDuplicates} onChange={setSkipDuplicates} />
        <Toggle
          label="Delete the originals afterwards"
          help={ownSources < chosen.length ? "Only your own: playlists shared with you stay." : "They’re deleted once everything is in the combined playlist."}
          checked={deleteSources}
          onChange={setDeleteSources}
        />
      </div>
    </Dialog>
  );
}
