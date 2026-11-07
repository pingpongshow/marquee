import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { playlistFoldersQuery } from "@/api/queries";
import type { Playlist, PlaylistFolder } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Select } from "@/components/ui";

// Playlist folders (MUSIC-28): optional, and each person's own.

const NEW = -1;

function useInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ["playlists"] });
}

async function moveToFolder(p: Playlist, folderId: number) {
  const playlistId = p.id;
  if (folderId) {
    await unwrap(api.PUT("/playlist-folders/{folderId}/playlists/{playlistId}", { params: { path: { folderId, playlistId } } }));
  } else if (p.folderId) {
    await unwrap(api.DELETE("/playlist-folders/{folderId}/playlists/{playlistId}", { params: { path: { folderId: p.folderId, playlistId } } }));
  }
}

/** Puts a playlist in one of your folders, a new one, or takes it out of its folder. */
export function MoveToFolderDialog({ playlist, onClose }: { playlist: Playlist; onClose: () => void }) {
  const invalidate = useInvalidate();
  const folders = useQuery(playlistFoldersQuery());
  const [choice, setChoice] = useState(playlist.folderId ?? 0);
  const [name, setName] = useState("");
  const save = useMutation({
    mutationFn: async () => {
      let folderId = choice;
      if (choice === NEW) folderId = (await unwrap(api.POST("/playlist-folders", { body: { name: name.trim() } }))).id;
      if (folderId !== (playlist.folderId ?? 0)) await moveToFolder(playlist, folderId);
    },
    onSuccess: () => {
      invalidate();
      onClose();
    },
  });
  const ready = choice !== NEW || name.trim() !== "";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (ready) save.mutate();
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title="Folder"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!ready} loading={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        {(save.isError || folders.isError) && <Alert tone="error">{(save.error ?? folders.error)?.message}</Alert>}
        <Field label={`Put “${playlist.title}” in`}>
          {(id) => (
            <Select id={id} value={choice} onChange={(e) => setChoice(Number(e.target.value))}>
              <option value={0}>No folder</option>
              {folders.data?.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
              <option value={NEW}>New folder…</option>
            </Select>
          )}
        </Field>
        {choice === NEW && <Field label="Folder name">{(id) => <Input id={id} autoFocus maxLength={200} value={name} placeholder="Electronic" onChange={(e) => setName(e.target.value)} />}</Field>}
      </form>
    </Dialog>
  );
}

/** Names a new folder, or renames one. */
export function FolderNameDialog({ folder, onClose, onSaved }: { folder?: PlaylistFolder; onClose: () => void; onSaved?: (f: PlaylistFolder) => void }) {
  const invalidate = useInvalidate();
  const [name, setName] = useState(folder?.name ?? "");
  const save = useMutation({
    mutationFn: () =>
      folder
        ? unwrap(api.PATCH("/playlist-folders/{folderId}", { params: { path: { folderId: folder.id } }, body: { name: name.trim() } }))
        : unwrap(api.POST("/playlist-folders", { body: { name: name.trim() } })),
    onSuccess: (f) => {
      invalidate();
      onSaved?.(f);
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) save.mutate();
  };
  return (
    <Dialog
      open
      onClose={onClose}
      title={folder ? "Rename folder" : "New folder"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!name.trim()} loading={save.isPending} onClick={() => save.mutate()}>
            {folder ? "Rename" : "Create"}
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        {save.isError && <Alert tone="error">{save.error.message}</Alert>}
        <Field label="Name">{(id) => <Input id={id} autoFocus maxLength={200} value={name} placeholder="Electronic" onChange={(e) => setName(e.target.value)} />}</Field>
      </form>
    </Dialog>
  );
}

export function DeleteFolderDialog({ folder, onClose, onDeleted }: { folder: PlaylistFolder; onClose: () => void; onDeleted: () => void }) {
  const invalidate = useInvalidate();
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE("/playlist-folders/{folderId}", { params: { path: { folderId: folder.id } } })),
    onSuccess: () => {
      invalidate();
      onDeleted();
    },
  });
  return (
    <Dialog
      open
      onClose={onClose}
      title="Delete folder?"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger" loading={del.isPending} onClick={() => del.mutate()}>
            Delete folder
          </Button>
        </>
      }
    >
      {del.isError && <Alert tone="error">{del.error.message}</Alert>}
      <p className="text-sm text-muted">“{folder.name}” will be deleted. Its playlists aren’t: they go back to Playlists.</p>
    </Dialog>
  );
}
