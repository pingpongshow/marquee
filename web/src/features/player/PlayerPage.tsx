import { useParams, useSearch } from "@tanstack/react-router";
import { lazy, Suspense } from "react";
import { Spinner } from "@/components/ui";

// hls.js is large; load the player only when something is played.
const VideoPlayer = lazy(() => import("./VideoPlayer").then((m) => ({ default: m.VideoPlayer })));

export function PlayerPage() {
  const { itemId } = useParams({ from: "/play/$itemId" });
  const { t, pl } = useSearch({ from: "/play/$itemId" });
  return (
    <Suspense fallback={<div className="fixed inset-0 z-50 flex items-center justify-center bg-black"><Spinner label="Loading player" /></div>}>
      {/* Keyed by item so Up Next starts a fresh player. */}
      <VideoPlayer key={itemId} itemId={Number(itemId)} startMs={t} playlistId={pl} />
    </Suspense>
  );
}
