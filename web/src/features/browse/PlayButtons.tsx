import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Check, Play, RotateCcw, Shuffle } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { ItemDetail, ItemSummary } from "@/api/types";
import { Button } from "@/components/ui";
import { useMusic } from "../player/MusicPlayer";
import { formatDuration } from "./format";

async function children(id: number) {
  return (await unwrap(api.GET("/items/{itemId}/children", { params: { path: { itemId: id }, query: { limit: 500 } } }))).items;
}

/** First unwatched episode of a show (or the first episode if all are watched). */
async function nextEpisode(showId: number): Promise<ItemSummary | undefined> {
  const seasons = (await children(showId)).filter((s) => (s.index ?? 0) > 0);
  let first: ItemSummary | undefined;
  for (const s of seasons) {
    for (const e of await children(s.id)) {
      first ??= e;
      if (!e.viewCount) return e;
    }
  }
  return first;
}

async function albumTracks(album: number) {
  return (await children(album)).filter((t) => t.type === "track");
}

export function PlayButtons({ item }: { item: ItemDetail }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const music = useMusic();
  const [busy, setBusy] = useState(false);
  const watched = item.type === "show" || item.type === "season" ? item.leafCount > 0 && item.watchedLeafCount === item.leafCount : (item.viewCount ?? 0) > 0;
  const toggleWatched = useMutation({
    mutationFn: () =>
      watched ? unwrap(api.DELETE("/items/{itemId}/watched", { params: { path: { itemId: item.id } } })) : unwrap(api.POST("/items/{itemId}/watched", { params: { path: { itemId: item.id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
  });
  const playVideo = (id: number, t?: number) => navigate({ to: "/play/$itemId", params: { itemId: String(id) }, search: { t } });
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } finally {
      setBusy(false);
    }
  };

  const playable = item.type === "movie" || item.type === "episode" || item.type === "video";
  const resume = playable && (item.viewOffsetMs ?? 0) > 0;
  return (
    <div className="mt-5 flex flex-wrap items-center gap-3">
      {playable && (
        <>
          <Button variant="primary" onClick={() => playVideo(item.id)} disabled={!item.available}>
            <Play className="size-4 fill-current" /> {resume ? `Resume from ${formatDuration(item.viewOffsetMs)}` : "Play"}
          </Button>
          {resume && (
            <Button onClick={() => playVideo(item.id, 0)}>
              <RotateCcw className="size-4" /> Play from start
            </Button>
          )}
        </>
      )}
      {(item.type === "show" || item.type === "season") && (
        <Button
          variant="primary"
          loading={busy}
          onClick={() =>
            run(async () => {
              const ep = item.type === "show" ? await nextEpisode(item.id) : (await children(item.id)).find((e) => !e.viewCount) ?? (await children(item.id))[0];
              if (ep) playVideo(ep.id);
            })
          }
        >
          <Play className="size-4 fill-current" /> {item.watchedLeafCount ? "Continue" : "Play"}
        </Button>
      )}
      {item.type === "album" && (
        <>
          <Button variant="primary" loading={busy} onClick={() => run(async () => music.play(await albumTracks(item.id)))}>
            <Play className="size-4 fill-current" /> Play
          </Button>
          <Button loading={busy} onClick={() => run(async () => music.play([...(await albumTracks(item.id))].sort(() => Math.random() - 0.5)))}>
            <Shuffle className="size-4" /> Shuffle
          </Button>
        </>
      )}
      {item.type === "artist" && (
        <Button
          variant="primary"
          loading={busy}
          onClick={() =>
            run(async () => {
              const tracks: ItemSummary[] = [];
              for (const al of await children(item.id)) tracks.push(...(await albumTracks(al.id)));
              music.play(tracks.sort(() => Math.random() - 0.5));
            })
          }
        >
          <Shuffle className="size-4" /> Shuffle artist
        </Button>
      )}
      {item.type === "track" && (
        <Button variant="primary" onClick={() => music.play([item])}>
          <Play className="size-4 fill-current" /> Play
        </Button>
      )}
      {(playable || item.type === "show" || item.type === "season") && (
        <Button variant="ghost" onClick={() => toggleWatched.mutate()} loading={toggleWatched.isPending}>
          <Check className={watched ? "size-4 text-success" : "size-4"} /> {watched ? "Watched" : "Mark watched"}
        </Button>
      )}
    </div>
  );
}
