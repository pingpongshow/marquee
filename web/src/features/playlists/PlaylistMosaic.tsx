import { clsx } from "clsx";
import { Layers, ListMusic } from "lucide-react";
import { imageUrl, playlistImageUrl } from "@/api/client";
import type { Playlist } from "@/api/types";

/**
 * A playlist's or collection's artwork: the picture its owner gave it (MUSIC-30), else a 2×2
 * mosaic of its items' covers (one image fills the tile when that's all there is).
 */
export function PlaylistMosaic({ playlist, className, size = 120 }: { playlist: Pick<Playlist, "id" | "imageIds" | "imageVersion" | "kind">; className?: string; size?: number }) {
  const ids = playlist.imageVersion ? [] : playlist.imageIds;
  const Icon = playlist.kind === "audio" ? ListMusic : Layers;
  return (
    <div className={clsx("relative shrink-0 overflow-hidden rounded-md bg-surface-3", className)}>
      {playlist.imageVersion && <img src={playlistImageUrl(playlist.id, playlist.imageVersion, size)} alt="" loading="lazy" className="size-full object-cover" />}
      {!playlist.imageVersion && ids.length === 0 && <Icon className="absolute inset-0 m-auto size-1/3 text-faint" aria-hidden />}
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
