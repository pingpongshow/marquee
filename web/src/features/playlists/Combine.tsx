import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Sparkles } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { playlistsQuery } from "@/api/queries";
import type { Playlist } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { countText, SharedIcon } from "./Sharing";

/**
 * Combines playlists (MUSIC-29): yours, ones shared with you and smart ones (as they are now),
 * in the order picked, into a new playlist or onto the end of one of yours, e.g. several
 * Christmas playlists into one. The scope says which can go together: music playlists, or one
 * video library's collections (USER-23).
 */
export type CombineScope = { kind: "audio" } | { kind: "video"; libraryId: number };

export function CombinePlaylistsDialog({ scope, initial = [], into, onClose }: { scope: CombineScope; initial?: number[]; into?: Playlist; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const lists = useQuery(playlistsQuery(scope.kind, scope.kind === "video" ? scope.libraryId : undefined));
  const audio = scope.kind === "audio";
  const noun = audio ? "playlist" : "collection";
  const [picked, setPicked] = useState<number[]>(initial.filter((id) => id !== into?.id));
  const [dest, setDest] = useState<"new" | number>(into?.id ?? "new");
  const [title, setTitle] = useState("");
  const [skipDuplicates, setSkipDuplicates] = useState(true);
  const [deleteSources, setDeleteSources] = useState(false);

  const all = lists.data ?? [];
  const byId = new Map(all.map((p) => [p.id, p]));
  const chosen = picked.map((id) => byId.get(id)).filter((p): p is Playlist => !!p);
  const destinations = all.filter((p) => !p.readOnly && !p.rules && !picked.includes(p.id));
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
      title={into ? `Add ${noun}s to “${into.title}”` : `Combine ${noun}s`}
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
        <p className="text-sm text-muted">
          Pick the {noun}s in the order they should go in.{audio ? " Smart playlists add what’s in them now." : ""}
        </p>
        {lists.isPending && <Spinner />}
        <ul className="max-h-72 space-y-1 overflow-y-auto rounded-md border border-border p-1" data-testid="combine-sources">
          {all
            .filter((p) => p.id !== into?.id)
            .map((p) => {
              const at = picked.indexOf(p.id);
              return (
                <li key={p.id}>
                  <label className="flex cursor-pointer items-center gap-3 rounded px-2 py-1.5 hover:bg-surface-2">
                    <input type="checkbox" className="accent-accent" checked={at >= 0} onChange={() => toggle(p)} aria-label={p.title} />
                    <PlaylistMosaic playlist={p} size={80} className="size-9 shrink-0" />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5 truncate text-sm">
                        {p.rules && <Sparkles className="size-3.5 shrink-0 text-accent" aria-label="Smart playlist" />}
                        <span className="truncate">{p.title}</span>
                        <SharedIcon playlist={p} />
                      </span>
                      <span className="block text-xs text-muted">{countText(p)}</span>
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
                <option value="new">A new {noun}</option>
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
          <Field label={`New ${noun}’s name`}>
            {(id) => <Input id={id} maxLength={200} value={title} placeholder={chosen[0]?.title ?? "Name"} onChange={(e) => setTitle(e.target.value)} />}
          </Field>
        )}
        {/* A collection holds each title once anyway. */}
        {audio && <Toggle label="Skip duplicates" help="Each song goes in once, even if it’s in several of them." checked={skipDuplicates} onChange={setSkipDuplicates} />}
        <Toggle
          label="Delete the originals afterwards"
          help={ownSources < chosen.length ? `Only your own: ${noun}s shared with you stay.` : `They’re deleted once everything is in the combined ${noun}.`}
          checked={deleteSources}
          onChange={setDeleteSources}
        />
      </div>
    </Dialog>
  );
}
