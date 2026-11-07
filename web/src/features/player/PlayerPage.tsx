import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { lazy, Suspense, useState } from "react";
import { api, unwrap } from "@/api/client";
import { itemQuery } from "@/api/queries";
import type { ItemDetail, ItemSummary } from "@/api/types";
import { toast } from "@/components/Toast";
import { Spinner } from "@/components/ui";

// hls.js is large; load the player only when something is played.
const VideoPlayer = lazy(() =>
  import("./VideoPlayer").then((m) => ({ default: m.VideoPlayer })),
);

const loading = (
  <div className="on-black fixed inset-0 z-50 flex items-center justify-center bg-black">
    <Spinner label="Loading player" />
  </div>
);

export function PlayerPage() {
  const { itemId } = useParams({ from: "/play/$itemId" });
  const { t, pl, sh, f, g, vm, vs, ve } = useSearch({ from: "/play/$itemId" });
  const navigate = useNavigate();
  // Started from a film or episode on demand that the library has: if the library's copy
  // can't be played (gone, or it fails), the stream on demand plays instead.
  const onDemand =
    vm || (vs && ve)
      ? () => {
          toast("The library's copy couldn't be played, so this plays on VOD.");
          if (vm) navigate({ to: "/vod/$movieId/play", params: { movieId: String(vm) }, replace: true });
          else navigate({ to: "/vod/shows/$seriesId/play/$episodeId", params: { seriesId: String(vs), episodeId: String(ve) }, replace: true });
        }
      : undefined;
  return (
    <Suspense fallback={loading}>
      {/* Keyed by item so Up Next starts a fresh player (and its own trailers). */}
      <PlayerFlow
        key={itemId}
        itemId={Number(itemId)}
        startMs={t}
        playlistId={pl}
        shuffleSeed={sh}
        fileId={f}
        groupId={g}
        onUnplayable={onDemand}
      />
    </Suspense>
  );
}

/**
 * A movie started from the beginning, alone, gets the cinema trailers first (PLAY-18). Not in a
 * playlist, which goes straight from one item to the next.
 */
function wantsPrerolls(d: ItemDetail, startMs?: number, groupId?: string, playlistId?: number) {
  if (groupId || playlistId || d.type !== "movie") return false;
  return startMs === 0 || (startMs === undefined && !d.viewOffsetMs);
}

function prerollLabel(p: ItemSummary) {
  return p.extraType === "trailer"
    ? `Trailer · ${p.parentTitle ?? p.title}`
    : p.title;
}

function PlayerFlow({
  itemId,
  startMs,
  playlistId,
  shuffleSeed,
  fileId,
  groupId,
  onUnplayable,
}: {
  itemId: number;
  startMs?: number;
  playlistId?: number;
  shuffleSeed?: number;
  fileId?: number;
  groupId?: string;
  onUnplayable?: () => void;
}) {
  const item = useQuery(itemQuery(itemId));
  // Decided once, when the item first loads: later refetches (progress) mustn't change it.
  const [eligible, setEligible] = useState<boolean | null>(null);
  if (eligible === null && (item.data || item.isError))
    setEligible(
      item.data ? wantsPrerolls(item.data, startMs, groupId, playlistId) : false,
    );
  const prerolls = useQuery({
    queryKey: ["prerolls", itemId],
    queryFn: () =>
      unwrap(
        api.GET("/items/{itemId}/prerolls", {
          params: { path: { itemId } },
        }),
      ),
    enabled: eligible === true,
    retry: false,
    staleTime: Infinity,
    gcTime: 0, // a fresh pick of trailers each time
  });
  const [index, setIndex] = useState(0);

  if (eligible === null || (eligible && prerolls.isPending)) return loading;
  const list = (eligible && prerolls.data) || [];
  const pre = list[index];
  if (pre)
    return (
      <VideoPlayer
        key={`preroll-${index}`}
        itemId={pre.id}
        startMs={0}
        preroll={{
          label: prerollLabel(pre),
          movieId: itemId,
          onDone: () => setIndex((i) => i + 1),
          onSkipAll: () => setIndex(list.length),
        }}
      />
    );
  return (
    <VideoPlayer
      key="main"
      itemId={itemId}
      startMs={startMs}
      playlistId={playlistId}
      shuffleSeed={shuffleSeed}
      fileId={fileId}
      groupId={groupId}
      onUnplayable={onUnplayable}
    />
  );
}
