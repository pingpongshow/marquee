import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ImagePlus, Link2, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { api, ApiError, session, unwrap } from "@/api/client";
import { playlistQuery } from "@/api/queries";
import type { Playlist } from "@/api/types";
import { Alert, Button, Dialog, Input } from "@/components/ui";
import { PlaylistMosaic } from "./PlaylistMosaic";

// The file goes up as it is (raw bytes), which the typed client doesn't do.
async function uploadPicture(playlistId: number, file: File): Promise<Playlist> {
  const res = await fetch(`/api/v1/playlists/${playlistId}/image`, {
    method: "POST",
    body: file,
    headers: { Authorization: `Bearer ${session.token}`, "Content-Type": file.type || "application/octet-stream" },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.code ?? "error", data.message ?? res.statusText);
  return data as Playlist;
}

/**
 * A picture of its own for your playlist or collection (MUSIC-30): upload one, give an image
 * link, or go back to the mosaic of what's in it. Each applies at once.
 */
export function PictureDialog({ playlist, onClose }: { playlist: Playlist; onClose: () => void }) {
  const qc = useQueryClient();
  const file = useRef<HTMLInputElement>(null);
  const [link, setLink] = useState("");
  const path = { params: { path: { playlistId: playlist.id } } };
  const done = (p: Playlist) => {
    qc.setQueryData(playlistQuery(playlist.id).queryKey, p);
    // Lists, pickers and Home shelves draw its tile too.
    void qc.invalidateQueries({ queryKey: ["playlists"] });
    void qc.invalidateQueries({ queryKey: ["items", "hubs"] });
    setLink("");
  };
  const upload = useMutation({ mutationFn: (f: File) => uploadPicture(playlist.id, f), onSuccess: done });
  const useLink = useMutation({ mutationFn: (url: string) => unwrap(api.PUT("/playlists/{playlistId}/image", { ...path, body: { url } })), onSuccess: done });
  const remove = useMutation({ mutationFn: () => unwrap(api.DELETE("/playlists/{playlistId}/image", path)), onSuccess: done });
  const busy = upload.isPending || useLink.isPending || remove.isPending;
  // The latest attempt's error only.
  const error = [upload, useLink, remove].filter((m) => m.isError).sort((a, b) => b.submittedAt - a.submittedAt)[0]?.error;
  return (
    <Dialog
      open
      onClose={onClose}
      title="Change picture"
      footer={
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      }
    >
      <div className="space-y-4">
        {error && !busy && <Alert tone="error">{error.message}</Alert>}
        <div className="flex items-center gap-4">
          <PlaylistMosaic playlist={playlist} size={240} className={playlist.kind === "audio" ? "size-28" : "aspect-[2/3] w-24"} />
          <div className="flex flex-col items-start gap-2">
            <input
              ref={file}
              type="file"
              accept="image/jpeg,image/png,image/webp"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) upload.mutate(f);
                e.target.value = "";
              }}
            />
            <Button size="sm" onClick={() => file.current?.click()} loading={upload.isPending} disabled={busy}>
              <ImagePlus className="size-4" /> Upload image
            </Button>
            {playlist.imageVersion && (
              <Button size="sm" variant="ghost" onClick={() => remove.mutate()} loading={remove.isPending} disabled={busy} title="Show its covers again">
                <Trash2 className="size-4" /> Remove picture
              </Button>
            )}
            <p className="text-xs text-muted">JPEG, PNG or WebP, up to 20 MB.</p>
          </div>
        </div>
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (link.trim() && !busy) useLink.mutate(link.trim());
          }}
        >
          <Input aria-label="Image link" placeholder="Or paste an image link (https://…)" value={link} onChange={(e) => setLink(e.target.value)} />
          <Button size="sm" type="submit" loading={useLink.isPending} disabled={!link.trim() || busy}>
            <Link2 className="size-4" /> Use
          </Button>
        </form>
      </div>
    </Dialog>
  );
}
