import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Bookmark, BookmarkCheck, Check, Play, RotateCcw, Shuffle } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { fetchLeaves } from "@/api/queries";
import type { ItemDetail } from "@/api/types";
import { Button, Select } from "@/components/ui";
import { useMusic } from "../player/MusicPlayer";
import { formatTrackTime, versionLabel } from "./format";

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
  const [listed, setListed] = useState(!!item.watchlisted);
  const toggleWatchlist = useMutation({
    mutationFn: (on: boolean) =>
      on
        ? unwrap(api.PUT("/items/{itemId}/watchlist", { params: { path: { itemId: item.id } } }))
        : unwrap(api.DELETE("/items/{itemId}/watchlist", { params: { path: { itemId: item.id } } })),
    onMutate: (on) => setListed(on),
    onError: (_e, on) => setListed(!on),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
  });
  // Several versions (4K and 1080p, a director's cut): choose one, or let the server pick the best (LIB-7).
  const [file, setFile] = useState<number | undefined>();
  const playVideo = (id: number, t?: number) => navigate({ to: "/play/$itemId", params: { itemId: String(id) }, search: { t, f: id === item.id ? file : undefined } });
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
            <Play className="size-4 fill-current" /> {resume ? `Resume from ${formatTrackTime(item.viewOffsetMs)}` : "Play"}
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
              const [ep] = await fetchLeaves(item.id, { unwatched: true });
              if (ep) playVideo(ep.id);
            })
          }
        >
          <Play className="size-4 fill-current" /> {item.watchedLeafCount ? "Continue" : "Play"}
        </Button>
      )}
      {(item.type === "album" || item.type === "artist") && (
        <>
          <Button variant="primary" loading={busy} onClick={() => run(async () => music.play(await fetchLeaves(item.id), 0, { source: item.title }))}>
            <Play className="size-4 fill-current" /> Play
          </Button>
          <Button loading={busy} onClick={() => run(async () => music.play(await fetchLeaves(item.id), 0, { shuffle: true, source: item.title }))}>
            <Shuffle className="size-4" /> Shuffle
          </Button>
        </>
      )}
      {item.type === "track" && (
        <Button variant="primary" onClick={() => music.play([item])}>
          <Play className="size-4 fill-current" /> Play
        </Button>
      )}
      {playable && item.versions.length > 1 && (
        <div className="w-56">
          <Select aria-label="Version" value={file ?? ""} onChange={(e) => setFile(e.target.value ? Number(e.target.value) : undefined)}>
            <option value="">Best version</option>
            {item.versions.map((v) => (
              <option key={v.id} value={v.files[0]?.id}>
                {versionLabel(v)}
              </option>
            ))}
          </Select>
        </div>
      )}
      {(item.type === "movie" || item.type === "show" || item.type === "episode" || item.type === "video") && (
        <Button variant="ghost" onClick={() => toggleWatchlist.mutate(!listed)} aria-pressed={listed}>
          {listed ? <BookmarkCheck className="size-4 text-accent" /> : <Bookmark className="size-4" />} {listed ? "On watchlist" : "Watchlist"}
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
