import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Combine, Folder, FolderInput, FolderPlus, ListMusic, Pencil, Plus, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, playlistFoldersQuery, playlistsQuery } from "@/api/queries";
import type { Playlist, PlaylistFolder } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Spinner, Toggle } from "@/components/ui";
import { formatDuration } from "../browse/format";
import { PlaylistMosaic } from "./PlaylistMosaic";
import { defaultRules, RulesEditor, rulesValid } from "./RulesEditor";
import { PlaylistInvites, SharedIcon, sharedByText } from "./Sharing";
import { DeleteFolderDialog, FolderNameDialog, MoveToFolderDialog } from "./Folders";
import { CombinePlaylistsDialog } from "./Combine";
import type { SmartRules } from "@/api/types";

function NewPlaylistDialog({ folderId, onClose }: { folderId?: number; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  const [smart, setSmart] = useState(false);
  const [rules, setRules] = useState<SmartRules>(defaultRules);
  const create = useMutation({
    mutationFn: async () => {
      const p = await unwrap(api.POST("/playlists", { body: { title: title.trim(), kind: "audio", rules: smart ? rules : undefined } }));
      // Made inside a folder, it goes in that folder.
      if (folderId) await unwrap(api.PUT("/playlist-folders/{folderId}/playlists/{playlistId}", { params: { path: { folderId, playlistId: p.id } } }));
      return p;
    },
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["playlists"] });
      navigate({ to: "/playlist/$playlistId", params: { playlistId: String(p.id) } });
    },
  });
  // Enter in the form and the Create button go the same way: a named playlist, a smart one's
  // rules filled in, and one at a time.
  const ready = !!title.trim() && (!smart || rulesValid(rules));
  const go = () => {
    if (ready && !create.isPending) create.mutate();
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    go();
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
          <Button variant="primary" disabled={!ready} loading={create.isPending} onClick={go}>
            Create
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        {create.isError && <Alert tone="error">{create.error.message}</Alert>}
        <Field label="Name">{(id) => <Input id={id} autoFocus maxLength={200} value={title} onChange={(e) => setTitle(e.target.value)} />}</Field>
        <Toggle label="Smart playlist" help="Fills itself from rules and stays up to date as you add music and listen." checked={smart} onChange={setSmart} />
        {smart && <RulesEditor value={rules} onChange={setRules} />}
      </form>
    </Dialog>
  );
}

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
          {p.itemCount} tracks
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

/**
 * /playlists: playlists are music's (MUSIC-29), so this opens them inside the music library.
 * With no music library there's nowhere to put them, and the page shows on its own.
 */
export function PlaylistsRedirect() {
  const { folder } = useSearch({ from: "/playlists" });
  const libraries = useQuery(librariesQuery);
  if (libraries.isPending) return <Spinner />;
  const music = libraries.data?.find((l) => l.type === "music");
  if (!music) return <PlaylistsPage />;
  return <Navigate to="/library/$libraryId/$section" params={{ libraryId: String(music.id), section: "playlists" }} search={folder ? { folder } : {}} replace />;
}

/** The person's music playlists and their folders: the Playlists page of a music library. */
export function PlaylistsPage({ library }: { library?: { id: number; name: string } }) {
  const navigate = useNavigate();
  const { folder: folderId } = useSearch({ strict: false }) as { folder?: number };
  const [creating, setCreating] = useState(false);
  const [naming, setNaming] = useState<"new" | "rename" | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [moving, setMoving] = useState<Playlist | null>(null);
  const [combining, setCombining] = useState(false);
  const lists = useQuery(playlistsQuery("audio"));
  const folders = useQuery(playlistFoldersQuery());
  const openFolder = (id?: number) =>
    library
      ? navigate({ to: "/library/$libraryId/$section", params: { libraryId: String(library.id), section: "playlists" }, search: id ? { folder: id } : {} })
      : navigate({ to: "/playlists", search: id ? { folder: id } : {} });
  const folder = folderId ? folders.data?.find((f) => f.id === folderId) : undefined;
  const known = new Set(folders.data?.map((f) => f.id));
  // A playlist in a folder that's gone (or not loaded yet) shows at the top level.
  const inFolder = (p: Playlist) => (p.folderId && known.has(p.folderId) ? p.folderId : 0);
  const shown = lists.data?.filter((p) => inFolder(p) === (folder?.id ?? 0)) ?? [];
  // Counted from the music playlists: a folder's own count could include what's now a collection.
  const countIn = (f: PlaylistFolder) => (lists.data ? lists.data.filter((p) => p.folderId === f.id).length : f.playlistCount);
  const shownFolders = folder ? [] : (folders.data ?? []);
  return (
    <div className="p-6 lg:p-8">
      {library && (
        <Link to="/library/$libraryId" params={{ libraryId: String(library.id) }} className="mb-1 inline-flex items-center gap-1 text-sm text-muted hover:text-text">
          <ChevronLeft className="size-4" aria-hidden /> {library.name}
        </Link>
      )}
      <div className="mb-6 flex flex-wrap items-center gap-4">
        {folder ? (
          <h1 className="flex min-w-0 items-center gap-2 text-2xl font-bold">
            <button type="button" onClick={() => openFolder()} className="text-muted hover:text-text">
              Playlists
            </button>
            <ChevronRight className="size-5 shrink-0 text-faint" aria-hidden />
            <span className="truncate">{folder.name}</span>
          </h1>
        ) : (
          <h1 className="text-2xl font-bold">Playlists</h1>
        )}
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
          <Button size="sm" variant="ghost" onClick={() => setCombining(true)} disabled={(lists.data?.length ?? 0) < 2} data-testid="combine-playlists">
            <Combine className="size-4" /> Combine playlists
          </Button>
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
          <p>No playlists yet. Create one, or use “Add to playlist” from any song, album or artist.</p>
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
          <FolderTile key={`f${f.id}`} folder={f} count={countIn(f)} onOpen={() => openFolder(f.id)} />
        ))}
        {shown.map((p) => (
          <PlaylistTile key={p.id} playlist={p} onMove={() => setMoving(p)} />
        ))}
      </ul>
      {creating && <NewPlaylistDialog folderId={folder?.id} onClose={() => setCreating(false)} />}
      {naming && <FolderNameDialog folder={naming === "rename" ? folder : undefined} onClose={() => setNaming(null)} />}
      {deleting && folder && <DeleteFolderDialog folder={folder} onClose={() => setDeleting(false)} onDeleted={() => openFolder()} />}
      {moving && <MoveToFolderDialog playlist={moving} onClose={() => setMoving(null)} />}
      {combining && <CombinePlaylistsDialog scope={{ kind: "audio" }} onClose={() => setCombining(false)} />}
    </div>
  );
}
