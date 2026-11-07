import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { clsx } from "clsx";
import { ChevronRight, Folder, FolderInput, FolderPlus, ListMusic, Pencil, Plus, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { playlistFoldersQuery, playlistsQuery } from "@/api/queries";
import type { Playlist, PlaylistFolder, PlaylistKind } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { formatDuration } from "../browse/format";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { defaultRules, RulesEditor, rulesValid } from "./RulesEditor";
import { PlaylistInvites, SharedIcon, sharedByText } from "./Sharing";
import { DeleteFolderDialog, FolderNameDialog, MoveToFolderDialog } from "./Folders";
import type { SmartRules } from "@/api/types";

function NewPlaylistDialog({ folderId, onClose }: { folderId?: number; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<PlaylistKind>("audio");
  const [smart, setSmart] = useState(false);
  const [rules, setRules] = useState<SmartRules>(defaultRules);
  const create = useMutation({
    mutationFn: async () => {
      const p = await unwrap(api.POST("/playlists", { body: { title: title.trim(), kind, rules: smart ? rules : undefined } }));
      // Made inside a folder, it goes in that folder.
      if (folderId) await unwrap(api.PUT("/playlist-folders/{folderId}/playlists/{playlistId}", { params: { path: { folderId, playlistId: p.id } } }));
      return p;
    },
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["playlists"] });
      navigate({ to: "/playlist/$playlistId", params: { playlistId: String(p.id) } });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (title.trim()) create.mutate();
  };
  return (
    <Dialog
      open
      wide={smart}
      onClose={onClose}
      title="New playlist"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!title.trim() || (smart && !rulesValid(rules))} loading={create.isPending} onClick={() => create.mutate()}>
            Create
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        {create.isError && <Alert tone="error">{create.error.message}</Alert>}
        <Field label="Name">{(id) => <Input id={id} autoFocus maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
        <Field label="Type">
          {(id) => (
            <Select id={id} value={kind} onChange={(e) => setKind(e.target.value as PlaylistKind)}>
              <option value="audio">Music</option>
              <option value="video">Video</option>
            </Select>
          )}
        </Field>
        <Toggle label="Smart playlist" help="Fills itself from rules and stays up to date as you add music and listen." checked={smart} onChange={setSmart} />
        {smart && <RulesEditor value={rules} onChange={setRules} />}
      </form>
    </Dialog>
  );
}

const tabs: { id: "all" | PlaylistKind; label: string }[] = [
  { id: "all", label: "All" },
  { id: "audio", label: "Music" },
  { id: "video", label: "Video" },
];

function PlaylistTile({ playlist: p, onMove }: { playlist: Playlist; onMove: () => void }) {
  return (
    <li className="group relative">
      <Link to="/playlist/$playlistId" params={{ playlistId: String(p.id) }} className="block">
        <PlaylistMosaic playlist={p} size={240} className="aspect-square w-full shadow-md group-hover:ring-2 group-hover:ring-accent" />
        <div className="mt-2 flex items-center gap-1.5">
          <span className="truncate text-sm font-medium">{p.title}</span>
          <SharedIcon playlist={p} />
        </div>
        <div className="truncate text-xs text-muted">
          {p.itemCount} {p.kind === "audio" ? "tracks" : "items"}
          {p.durationMs ? ` · ${formatDuration(p.durationMs)}` : ""}
        </div>
        {p.readOnly && <div className="truncate text-xs text-muted">{sharedByText(p)}</div>}
      </Link>
      <button
        type="button"
        onClick={onMove}
        title={p.folderId ? "Move to another folder, or out of this one" : "Put in a folder"}
        aria-label={`Folder for ${p.title}`}
        className="absolute right-2 top-2 rounded-full bg-black/60 p-1.5 text-white opacity-0 transition-opacity hover:bg-black/80 focus-visible:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100"
      >
        <FolderInput className="size-4" aria-hidden />
      </button>
    </li>
  );
}

function FolderTile({ folder, count, onOpen }: { folder: PlaylistFolder; count: number; onOpen: () => void }) {
  return (
    <li>
      <button type="button" onClick={onOpen} className="group block w-full text-left">
        <div className="flex aspect-square w-full items-center justify-center rounded-md bg-surface-2 shadow-md group-hover:ring-2 group-hover:ring-accent">
          <Folder className="size-16 text-faint" aria-hidden />
        </div>
        <div className="mt-2 truncate text-sm font-medium">{folder.name}</div>
        <div className="truncate text-xs text-muted">{count === 1 ? "1 playlist" : `${count} playlists`}</div>
      </button>
    </li>
  );
}

export function PlaylistsPage() {
  const navigate = useNavigate();
  const { folder: folderId } = useSearch({ from: "/playlists" });
  const [tab, setTab] = useState<"all" | PlaylistKind>("all");
  const [creating, setCreating] = useState(false);
  const [naming, setNaming] = useState<"new" | "rename" | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [moving, setMoving] = useState<Playlist | null>(null);
  const lists = useQuery(playlistsQuery(tab === "all" ? undefined : tab));
  const folders = useQuery(playlistFoldersQuery());
  const openFolder = (id?: number) => navigate({ to: "/playlists", search: id ? { folder: id } : {} });
  const folder = folderId ? folders.data?.find((f) => f.id === folderId) : undefined;
  const known = new Set(folders.data?.map((f) => f.id));
  // A playlist in a folder that's gone (or not loaded yet) shows at the top level.
  const inFolder = (p: Playlist) => (p.folderId && known.has(p.folderId) ? p.folderId : 0);
  const shown = lists.data?.filter((p) => inFolder(p) === (folder?.id ?? 0)) ?? [];
  const countIn = (f: PlaylistFolder) => lists.data?.filter((p) => p.folderId === f.id).length ?? 0;
  // On the Music or Video tab, a folder shows if it has playlists of that kind, or none at all.
  const shownFolders = folder ? [] : (folders.data ?? []).filter((f) => tab === "all" || f.playlistCount === 0 || countIn(f) > 0);
  return (
    <div className="p-6 lg:p-8">
      <div className="mb-6 flex flex-wrap items-center gap-4">
        {folder ? (
          <h1 className="flex min-w-0 items-center gap-2 text-2xl font-bold">
            <Link to="/playlists" className="text-muted hover:text-text">
              Playlists
            </Link>
            <ChevronRight className="size-5 shrink-0 text-faint" aria-hidden />
            <span className="truncate">{folder.name}</span>
          </h1>
        ) : (
          <h1 className="text-2xl font-bold">Playlists</h1>
        )}
        <div className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="tablist">
          {tabs.map((t) => (
            <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)} className={clsx("rounded px-3 py-1", tab === t.id ? "bg-surface-3 font-medium" : "text-muted hover:text-text")}>
              {t.label}
            </button>
          ))}
        </div>
        <div className="ml-auto flex flex-wrap gap-2">
          {folder ? (
            <>
              <Button size="sm" variant="ghost" onClick={() => setNaming("rename")}>
                <Pencil className="size-4" /> Rename folder
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setDeleting(true)}>
                <Trash2 className="size-4" /> Delete folder
              </Button>
            </>
          ) : (
            <Button size="sm" variant="ghost" onClick={() => setNaming("new")}>
              <FolderPlus className="size-4" /> New folder
            </Button>
          )}
          <Button size="sm" variant="primary" onClick={() => setCreating(true)}>
            <Plus className="size-4" /> New playlist
          </Button>
        </div>
      </div>
      {!folder && <PlaylistInvites />}
      {(lists.isPending || (folderId && folders.isPending)) && <Spinner />}
      {(lists.isError || folders.isError) && <Alert tone="error">{(lists.error ?? folders.error)?.message}</Alert>}
      {folderId && folders.isSuccess && !folder && <Alert tone="error">This folder is gone.</Alert>}
      {lists.isSuccess && !folder && !folderId && lists.data.length === 0 && shownFolders.length === 0 && (
        <div className="flex flex-col items-center gap-3 py-16 text-center text-muted">
          <ListMusic className="size-12 text-faint" aria-hidden />
          <p>No playlists yet. Create one, or use “Add to playlist” from any track, album, movie or episode.</p>
        </div>
      )}
      {folder && lists.isSuccess && shown.length === 0 && (
        <div className="flex flex-col items-center gap-3 py-16 text-center text-muted">
          <Folder className="size-12 text-faint" aria-hidden />
          <p>Nothing in this folder yet. Use the folder button on a playlist, or “Folder” on its page, to put it here.</p>
        </div>
      )}
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-x-4 gap-y-6">
        {shownFolders.map((f) => (
          <FolderTile key={`f${f.id}`} folder={f} count={tab === "all" ? f.playlistCount : countIn(f)} onOpen={() => openFolder(f.id)} />
        ))}
        {shown.map((p) => (
          <PlaylistTile key={p.id} playlist={p} onMove={() => setMoving(p)} />
        ))}
      </ul>
      {creating && <NewPlaylistDialog folderId={folder?.id} onClose={() => setCreating(false)} />}
      {naming && <FolderNameDialog folder={naming === "rename" ? folder : undefined} onClose={() => setNaming(null)} />}
      {deleting && folder && <DeleteFolderDialog folder={folder} onClose={() => setDeleting(false)} onDeleted={() => openFolder()} />}
      {moving && <MoveToFolderDialog playlist={moving} onClose={() => setMoving(null)} />}
    </div>
  );
}
