import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import { ListMusic, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "@/api/client";
import { playlistsQuery } from "@/api/queries";
import type { PlaylistKind } from "@/api/types";
import { Alert, Button, Dialog, Field, Input, Select, Spinner } from "@/components/ui";
import { formatDuration } from "../browse/format";
import { PlaylistMosaic } from "./PlaylistMosaic";

function NewPlaylistDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<PlaylistKind>("audio");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/playlists", { body: { title: title.trim(), kind } })),
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
      onClose={onClose}
      title="New playlist"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!title.trim()} loading={create.isPending} onClick={() => create.mutate()}>
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
      </form>
    </Dialog>
  );
}

const tabs: { id: "all" | PlaylistKind; label: string }[] = [
  { id: "all", label: "All" },
  { id: "audio", label: "Music" },
  { id: "video", label: "Video" },
];

export function PlaylistsPage() {
  const [tab, setTab] = useState<"all" | PlaylistKind>("all");
  const [creating, setCreating] = useState(false);
  const lists = useQuery(playlistsQuery(tab === "all" ? undefined : tab));
  return (
    <div className="p-6 lg:p-8">
      <div className="mb-6 flex flex-wrap items-center gap-4">
        <h1 className="text-2xl font-bold">Playlists</h1>
        <div className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="tablist">
          {tabs.map((t) => (
            <button key={t.id} role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)} className={clsx("rounded px-3 py-1", tab === t.id ? "bg-surface-3 font-medium" : "text-muted hover:text-text")}>
              {t.label}
            </button>
          ))}
        </div>
        <Button className="ml-auto" size="sm" variant="primary" onClick={() => setCreating(true)}>
          <Plus className="size-4" /> New playlist
        </Button>
      </div>
      {lists.isPending && <Spinner />}
      {lists.isError && <Alert tone="error">{lists.error.message}</Alert>}
      {lists.data?.length === 0 && (
        <div className="flex flex-col items-center gap-3 py-16 text-center text-muted">
          <ListMusic className="size-12 text-faint" aria-hidden />
          <p>No playlists yet. Create one, or use “Add to playlist” from any track, album, movie or episode.</p>
        </div>
      )}
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-x-4 gap-y-6">
        {lists.data?.map((p) => (
          <li key={p.id}>
            <Link to="/playlist/$playlistId" params={{ playlistId: String(p.id) }} className="group block">
              <PlaylistMosaic playlist={p} size={240} className="aspect-square w-full shadow-md group-hover:ring-2 group-hover:ring-accent" />
              <div className="mt-2 truncate text-sm font-medium">{p.title}</div>
              <div className="truncate text-xs text-muted">
                {p.itemCount} {p.kind === "audio" ? "tracks" : "items"}
                {p.durationMs ? ` · ${formatDuration(p.durationMs)}` : ""}
              </div>
            </Link>
          </li>
        ))}
      </ul>
      {creating && <NewPlaylistDialog onClose={() => setCreating(false)} />}
    </div>
  );
}
