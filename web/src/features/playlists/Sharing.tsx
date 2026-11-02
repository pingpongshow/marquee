import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Users } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery, playlistInvitesQuery, playlistShareTargetsQuery, playlistSharingQuery } from "@/api/queries";
import type { Playlist } from "@/api/types";
import { Alert, Badge, Button, Dialog, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";
import { PlaylistMosaic } from "./PlaylistMosaic";

const countText = (p: Pick<Playlist, "itemCount" | "kind">) => `${p.itemCount} ${p.kind === "audio" ? (p.itemCount === 1 ? "track" : "tracks") : p.itemCount === 1 ? "item" : "items"}`;

/** "Shared by Sam" for a playlist someone shared with you (MUSIC-24). */
export const sharedByText = (p: Pick<Playlist, "ownerName">) => (p.ownerName ? `Shared by ${p.ownerName}` : "Shared with you");

/** Who your own playlist is shared with, in words; empty when it isn't. */
export function sharedText(p: Pick<Playlist, "readOnly" | "sharedWithAll" | "shareCount">) {
  if (p.readOnly) return "";
  if (p.sharedWithAll) return "Shared with everyone";
  const n = p.shareCount ?? 0;
  return n ? `Shared with ${n} ${n === 1 ? "person" : "people"}` : "";
}

/** The small mark on your own playlist when it's shared. */
export function SharedIcon({ playlist }: { playlist: Pick<Playlist, "readOnly" | "sharedWithAll" | "shareCount"> }) {
  const text = sharedText(playlist);
  if (!text) return null;
  return (
    <span className="inline-flex shrink-0 text-faint" title={text} role="img" aria-label={text} data-testid="playlist-shared">
      <Users className="size-3.5" aria-hidden />
    </span>
  );
}

/** Your playlist's Share dialog: choose people (and, for admins, everyone) and see who's answered. */
export function ShareDialog({ playlist, onClose }: { playlist: Pick<Playlist, "id" | "title">; onClose: () => void }) {
  const qc = useQueryClient();
  const admin = !!useQuery(meQuery).data?.isAdmin;
  const sharing = useQuery(playlistSharingQuery(playlist.id));
  const targets = useQuery(playlistShareTargetsQuery);
  // Null until changed here: what the server says until then.
  const [everyoneChoice, setEveryoneChoice] = useState<boolean | null>(null);
  const [chosen, setChosen] = useState<Set<number> | null>(null);
  const shares = sharing.data?.shares ?? [];
  const everyone = everyoneChoice ?? sharing.data?.all ?? false;
  const picked = chosen ?? new Set(shares.map((s) => s.userId));
  const toggle = (id: number, on: boolean) => {
    const next = new Set(picked);
    if (on) next.add(id);
    else next.delete(id);
    setChosen(next);
  };
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT("/playlists/{playlistId}/shares", {
          params: { path: { playlistId: playlist.id } },
          // Only admins may choose everyone; the server refuses anyone else.
          body: { userIds: [...picked], ...(admin ? { all: everyone } : {}) },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      onClose();
    },
  });
  const loading = sharing.isPending || targets.isPending;
  const error = sharing.error ?? targets.error ?? save.error;
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Share “${playlist.title}”`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={loading || sharing.isError || targets.isError} loading={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-muted">It's private until you share it. People you choose see it once they accept, and can play it but not change it.</p>
        {error && <Alert tone="error">{error.message}</Alert>}
        {loading && <Spinner />}
        {!loading && admin && (
          <label className="flex cursor-pointer items-center gap-3 rounded-md p-2 hover:bg-surface-2">
            <input type="checkbox" checked={everyone} onChange={(e) => setEveryoneChoice(e.target.checked)} className="size-4 accent-[var(--color-accent)]" data-testid="share-everyone" />
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-medium">Everyone</span>
              <span className="block text-xs text-muted">Everyone on this server, including people added later</span>
            </span>
          </label>
        )}
        {targets.data?.length === 0 && <p className="text-sm text-muted">There's no one else on this server to share it with yet.</p>}
        <ul className="max-h-72 space-y-1 overflow-y-auto" data-testid="share-people">
          {targets.data?.map((t) => {
            const status = shares.find((s) => s.userId === t.id)?.status;
            return (
              <li key={t.id}>
                <label className="flex cursor-pointer items-center gap-3 rounded-md p-2 hover:bg-surface-2">
                  <input type="checkbox" checked={everyone || picked.has(t.id)} disabled={everyone} onChange={(e) => toggle(t.id, e.target.checked)} className="size-4 accent-[var(--color-accent)]" />
                  <span className="min-w-0 flex-1 truncate text-sm">{t.name}</span>
                  {status && <Badge tone={status === "accepted" ? "accent" : "muted"}>{status === "accepted" ? "Accepted" : "Pending"}</Badge>}
                </label>
              </li>
            );
          })}
        </ul>
      </div>
    </Dialog>
  );
}

/** Takes a playlist shared with you out of your playlists, once confirmed. */
export function RemoveSharedDialog({ playlist, onClose, onRemoved }: { playlist: Pick<Playlist, "id" | "title" | "ownerName">; onClose: () => void; onRemoved: () => void }) {
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/me/playlist-shares/{playlistId}", { params: { path: { playlistId: playlist.id } } })),
    onSuccess: () => {
      onRemoved();
      void qc.invalidateQueries({ queryKey: ["playlists"] });
    },
  });
  return (
    <Dialog
      open
      onClose={onClose}
      title="Remove from your playlists?"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
            Remove
          </Button>
        </>
      }
    >
      {remove.isError && (
        <div className="mb-3">
          <Alert tone="error">{remove.error.message}</Alert>
        </div>
      )}
      <p className="text-sm text-muted">
        “{playlist.title}” will leave your playlists. To get it back you'd need {playlist.ownerName ?? "its owner"} to share it with you again.
      </p>
    </Dialog>
  );
}

/** Playlists waiting for an answer, at the top of the Playlists page. */
export function PlaylistInvites() {
  const qc = useQueryClient();
  const invites = useQuery(playlistInvitesQuery);
  const answer = useMutation({
    mutationFn: ({ id, accept }: { id: number; title: string; accept: boolean }) =>
      accept
        ? unwrap(api.PUT("/me/playlist-shares/{playlistId}", { params: { path: { playlistId: id } } }))
        : unwrap(api.DELETE("/me/playlist-shares/{playlistId}", { params: { path: { playlistId: id } } })),
    onSuccess: (_, { id, title, accept }) => {
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      if (accept) toast(`Added “${title}” to your playlists.`, { link: { label: "Open playlist", to: "/playlist/$playlistId", params: { playlistId: String(id) } } });
    },
    onError: (e: Error) => toast(e.message, { tone: "error" }),
  });
  if (!invites.data?.length) return null;
  return (
    <section className="mb-8" aria-labelledby="playlist-invites-title" data-testid="playlist-invites">
      <h2 id="playlist-invites-title" className="mb-3 text-lg font-semibold">
        Shared with you
      </h2>
      <ul className="divide-y divide-border rounded-lg border border-border bg-surface">
        {invites.data.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-3 px-3 py-2">
            <PlaylistMosaic playlist={p} className="size-11" />
            <div className="min-w-0 flex-1 basis-40">
              <div className="truncate text-sm font-medium">{p.title}</div>
              <div className="truncate text-xs text-muted">
                {sharedByText(p)} · {countText(p)}
              </div>
            </div>
            <div className="flex gap-2">
              <Button size="sm" variant="primary" disabled={answer.isPending} onClick={() => answer.mutate({ id: p.id, title: p.title, accept: true })}>
                Accept
              </Button>
              <Button size="sm" variant="ghost" disabled={answer.isPending} onClick={() => answer.mutate({ id: p.id, title: p.title, accept: false })}>
                Decline
              </Button>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
