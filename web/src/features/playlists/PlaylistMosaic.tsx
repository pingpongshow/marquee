import { clsx } from "clsx";
import { ListMusic, ListVideo } from "lucide-react";
import { imageUrl } from "@/api/client";
import type { Playlist } from "@/api/types";

/** The 2×2 cover mosaic Plex uses for playlists (one image fills the tile when that's all there is). */
export function PlaylistMosaic({ playlist, className, size = 120 }: { playlist: Pick<Playlist, "imageIds" | "kind">; className?: string; size?: number }) {
  const ids = playlist.imageIds;
  const Icon = playlist.kind === "audio" ? ListMusic : ListVideo;
  return (
    <div className={clsx("relative shrink-0 overflow-hidden rounded-md bg-surface-3", className)}>
      {ids.length === 0 && <Icon className="absolute inset-0 m-auto size-1/3 text-faint" aria-hidden />}
      {ids.length > 0 && ids.length < 4 && <img src={imageUrl(ids[0]!, size)} alt="" loading="lazy" className="size-full object-cover" />}
      {ids.length >= 4 && (
        <div className="grid size-full grid-cols-2 grid-rows-2">
          {ids.slice(0, 4).map((id) => (
            <img key={id} src={imageUrl(id, size / 2)} alt="" loading="lazy" className="size-full object-cover" />
          ))}
        </div>
      )}
    </div>
  );
}
