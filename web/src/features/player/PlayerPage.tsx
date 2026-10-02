import { useQuery } from "@tanstack/react-query";
import { useParams, useSearch } from "@tanstack/react-router";
import { lazy, Suspense, useState } from "react";
import { api, unwrap } from "@/api/client";
import { itemQuery } from "@/api/queries";
import type { ItemDetail, ItemSummary } from "@/api/types";
import { Spinner } from "@/components/ui";

// hls.js is large; load the player only when something is played.
const VideoPlayer = lazy(() =>
  import("./VideoPlayer").then((m) => ({ default: m.VideoPlayer })),
);

const loading = (
  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black">
    <Spinner label="Loading player" />
  </div>
);

export function PlayerPage() {
  const { itemId } = useParams({ from: "/play/$itemId" });
  const { t, pl, f, g } = useSearch({ from: "/play/$itemId" });
  return (
    <Suspense fallback={loading}>
      {/* Keyed by item so Up Next starts a fresh player (and its own trailers). */}
      <PlayerFlow
        key={itemId}
        itemId={Number(itemId)}
        startMs={t}
        playlistId={pl}
        fileId={f}
        groupId={g}
      />
    </Suspense>
  );
}

/** A movie started from the beginning, alone, gets the cinema trailers first (PLAY-18). */
function wantsPrerolls(d: ItemDetail, startMs?: number, groupId?: string) {
  if (groupId || d.type !== "movie") return false;
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
  fileId,
  groupId,
}: {
  itemId: number;
  startMs?: number;
  playlistId?: number;
  fileId?: number;
  groupId?: string;
}) {
  const item = useQuery(itemQuery(itemId));
  // Decided once, when the item first loads: later refetches (progress) mustn't change it.
  const [eligible, setEligible] = useState<boolean | null>(null);
  if (eligible === null && (item.data || item.isError))
    setEligible(
      item.data ? wantsPrerolls(item.data, startMs, groupId) : false,
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
      fileId={fileId}
      groupId={groupId}
    />
  );
}
