import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { clsx } from "clsx";
import { GripVertical, Pencil, Play, Shuffle, Trash2, X } from "lucide-react";
import { useState } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { playlistItemsQuery, playlistQuery } from "@/api/queries";
import type { ItemSummary, PlaylistEntry } from "@/api/types";
import { Alert, Button, Dialog, Input, Spinner } from "@/components/ui";
import { formatDuration, formatTrackTime } from "../browse/format";
import { ItemMenu } from "../browse/ItemMenu";
import { useMusic } from "../player/MusicPlayer";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { RulesEditor, rulesValid } from "./RulesEditor";
import type { SmartRules } from "@/api/types";
import { Sparkles } from "lucide-react";

function shuffled<T>(list: T[]) {
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [out[i], out[j]] = [out[j]!, out[i]!];
  }
  return out;
}

function subtitle(it: ItemSummary) {
  if (it.type === "track") return [it.artistCredit ?? it.grandparentTitle, it.parentTitle].filter(Boolean).join(" · ");
  if (it.type === "episode") return `${it.grandparentTitle ?? ""} · S${it.parentTitle?.replace(/\D/g, "") || "?"} E${it.index ?? "?"}`;
  return it.year ? String(it.year) : "";
}

export function PlaylistPage() {
  const { playlistId } = useParams({ from: "/playlist/$playlistId" });
  const id = Number(playlistId);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const music = useMusic();
  const pl = useQuery(playlistQuery(id));
  const entries = useQuery(playlistItemsQuery(id));
  const [renaming, setRenaming] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [drag, setDrag] = useState<number | null>(null);
  const [editingRules, setEditingRules] = useState<SmartRules | null>(null);
  const saveRules = useMutation({
    mutationFn: (rules: SmartRules) => unwrap(api.PATCH("/playlists/{playlistId}", { params: { path: { playlistId: id } }, body: { rules } })),
    onSuccess: () => {
      setEditingRules(null);
      invalidate();
    },
  });
  const [over, setOver] = useState<number | null>(null);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["playlists"] });

  const rename = useMutation({
    mutationFn: (title: string) => unwrap(api.PATCH("/playlists/{playlistId}", { params: { path: { playlistId: id } }, body: { title } })),
    onSuccess: () => {
      setRenaming(null);
      invalidate();
    },
  });
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE("/playlists/{playlistId}", { params: { path: { playlistId: id } } })),
    onSuccess: () => {
      invalidate();
      navigate({ to: "/playlists" });
    },
  });
  const remove = useMutation({
    mutationFn: (entryId: number) => unwrap(api.DELETE("/playlists/{playlistId}/items/{entryId}", { params: { path: { playlistId: id, entryId } } })),
    onSuccess: invalidate,
  });
  const move = useMutation({
    mutationFn: ({ entryId, after }: { entryId: number; after?: number }) =>
      unwrap(api.PATCH("/playlists/{playlistId}/items/{entryId}", { params: { path: { playlistId: id, entryId } }, body: after ? { afterEntryId: after } : {} })),
    onMutate: ({ entryId, after }) => {
      // Reorder optimistically so the drop feels instant.
      qc.setQueryData(playlistItemsQuery(id).queryKey, (old) => {
        if (!old) return old;
        const items = old.items.filter((e) => e.entryId !== entryId);
        const moved = old.items.find((e) => e.entryId === entryId)!;
        const at = after ? items.findIndex((e) => e.entryId === after) + 1 : 0;
        items.splice(at, 0, moved);
        return { ...old, items };
      });
    },
    onSettled: invalidate,
  });

  if (pl.isPending) return <Spinner />;
  if (pl.isError) return <div className="p-8"><Alert tone="error">{pl.error.message}</Alert></div>;
  const p = pl.data;
  const list: PlaylistEntry[] = entries.data?.items ?? [];
  const items = list.map((e) => e.item);

  const play = (start: number, shuffle = false) => {
    if (p.kind === "audio") {
      music.play(items, start, { shuffle, source: p.title });
      return;
    }
    const order = shuffle ? shuffled(items) : items.slice(start);
    if (order[0]) navigate({ to: "/play/$itemId", params: { itemId: String(order[0].id) }, search: { t: 0, pl: id } });
  };
  // Dropping on a row moves the dragged entry into that row's position.
  const drop = (target: number) => {
    if (drag === null) return;
    const from = list.findIndex((e) => e.entryId === drag);
    if (from < 0 || from === target) return;
    const without = list.filter((e) => e.entryId !== drag);
    move.mutate({ entryId: drag, after: target > 0 ? without[target - 1]?.entryId : undefined });
  };

  return (
    <div className="p-6 lg:p-8">
      <header className="mb-8 flex flex-col gap-6 sm:flex-row sm:items-end">
        <PlaylistMosaic playlist={p} size={400} className="size-48 shadow-xl" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 text-xs font-semibold tracking-wider text-faint uppercase">
            {p.rules && <Sparkles className="size-3.5 text-accent" aria-hidden />}
            {p.rules ? "Smart playlist" : p.kind === "audio" ? "Music playlist" : "Video playlist"}
          </div>
          {renaming === null ? (
            <h1 className="mt-1 flex items-center gap-2 text-3xl font-bold">
              <span className="truncate">{p.title}</span>
              <button onClick={() => setRenaming(p.title)} className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Rename playlist">
                <Pencil className="size-4" />
              </button>
            </h1>
          ) : (
            <form
              className="mt-1 flex max-w-md gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                if (renaming.trim()) rename.mutate(renaming.trim());
              }}
            >
              <Input aria-label="Playlist name" autoFocus value={renaming} maxLength={200} onChange={(e) => setRenaming(e.target.value)} />
              <Button type="submit" variant="primary" loading={rename.isPending}>
                Save
              </Button>
              <Button type="button" variant="ghost" onClick={() => setRenaming(null)}>
                Cancel
              </Button>
            </form>
          )}
          <div className="mt-2 text-sm text-muted">
            {p.itemCount} {p.kind === "audio" ? "tracks" : "items"} · {formatDuration(p.durationMs)}
          </div>
          <div className="mt-5 flex flex-wrap gap-3">
            <Button variant="primary" disabled={!items.length} onClick={() => play(0)}>
              <Play className="size-4 fill-current" /> Play
            </Button>
            <Button disabled={!items.length} onClick={() => play(0, true)}>
              <Shuffle className="size-4" /> Shuffle
            </Button>
            {p.rules && (
              <Button variant="ghost" onClick={() => setEditingRules(p.rules!)}>
                <Sparkles className="size-4" /> Edit rules
              </Button>
            )}
            <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
              <Trash2 className="size-4" /> Delete
            </Button>
          </div>
        </div>
      </header>
      {(rename.isError || remove.isError || move.isError) && <Alert tone="error">{(rename.error ?? remove.error ?? move.error)?.message}</Alert>}
      {entries.isPending && <Spinner />}
      {list.length === 0 && entries.isSuccess && (
        <p className="text-muted">
          {p.rules ? "No tracks match these rules yet. Try loosening them with “Edit rules”." : "This playlist is empty. Use “Add to playlist” on tracks, albums, movies or episodes."}
        </p>
      )}
      <ol className="empty:hidden divide-y divide-border rounded-lg border border-border bg-surface">
        {list.map((e, i) => {
          const it = e.item;
          const art = it.type === "episode" ? (it.images?.thumb ?? it.images?.poster) : it.images?.poster;
          return (
            <li
              key={e.entryId}
              draggable={!p.rules}
              onDragStart={() => setDrag(e.entryId)}
              onDragOver={(ev) => {
                ev.preventDefault();
                setOver(i);
              }}
              onDragEnd={() => {
                setDrag(null);
                setOver(null);
              }}
              onDrop={(ev) => {
                ev.preventDefault();
                drop(i);
                setDrag(null);
                setOver(null);
              }}
              className={clsx(
                "group flex items-center gap-3 px-3 py-2 hover:bg-surface-2",
                music.current?.item.id === it.id && p.kind === "audio" && "text-accent",
                drag === e.entryId && "opacity-40",
                over === i && drag !== null && drag !== e.entryId && "border-t-2 border-t-accent",
              )}
            >
              {!p.rules && <GripVertical className="size-4 shrink-0 cursor-grab text-faint" aria-hidden />}
              <span className="w-6 shrink-0 text-right text-sm text-faint tabular-nums">{i + 1}</span>
              <button onClick={() => play(i)} className="flex min-w-0 flex-1 items-center gap-3 text-left" disabled={!it.available}>
                <span className={clsx("shrink-0 overflow-hidden rounded bg-surface-3", it.type === "track" ? "size-10" : "aspect-video w-20")}>
                  {art && <img src={imageUrl(art, it.type === "track" ? 40 : 80)} alt="" loading="lazy" className="size-full object-cover" />}
                </span>
                <span className="min-w-0">
                  <span className={clsx("block truncate text-sm", !it.available && "text-faint line-through")}>{it.title}</span>
                  <span className="block truncate text-xs text-muted">{subtitle(it)}</span>
                </span>
              </button>
              <Link to="/item/$itemId" params={{ itemId: String(it.type === "track" && it.parentId ? it.parentId : it.id) }} className="hidden text-xs text-muted hover:underline md:block">
                {it.type === "track" ? "Album" : "Details"}
              </Link>
              <span className="w-14 shrink-0 text-right text-sm text-muted tabular-nums">{it.type === "track" ? formatTrackTime(it.durationMs) : formatDuration(it.durationMs)}</span>
              <ItemMenu item={it} />
              <button hidden={!!p.rules} onClick={() => remove.mutate(e.entryId)} className="rounded p-1 text-muted opacity-0 group-hover:opacity-100 hover:text-text focus:opacity-100" aria-label={`Remove ${it.title}`}>
                <X className="size-4" />
              </button>
            </li>
          );
        })}
      </ol>
      <Dialog
        open={!!editingRules}
        wide
        onClose={() => setEditingRules(null)}
        title="Smart playlist rules"
        footer={
          <>
            <Button variant="ghost" onClick={() => setEditingRules(null)}>
              Cancel
            </Button>
            <Button variant="primary" disabled={!editingRules || !rulesValid(editingRules)} loading={saveRules.isPending} onClick={() => editingRules && saveRules.mutate(editingRules)}>
              Save
            </Button>
          </>
        }
      >
        {saveRules.isError && <Alert tone="error">{saveRules.error.message}</Alert>}
        {editingRules && <RulesEditor value={editingRules} onChange={setEditingRules} />}
      </Dialog>
      <Dialog
        open={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        title="Delete playlist?"
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
        <p className="text-sm text-muted">“{p.title}” will be deleted. The media in it isn’t affected.</p>
      </Dialog>
    </div>
  );
}
