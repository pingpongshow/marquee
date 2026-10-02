import { useMutation, useQuery } from "@tanstack/react-query";
import { Compass } from "lucide-react";
import { useState } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { searchQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Alert, Dialog, Input, Spinner } from "@/components/ui";
import { useMusicActions } from "../player/MusicPlayer";

/** Sonic Adventure (MUSIC-4): pick a destination track and travel there through music that sounds in between. */
export function AdventureDialog({
  from,
  onClose,
}: {
  from: ItemSummary;
  onClose: () => void;
}) {
  const music = useMusicActions();
  const [q, setQ] = useState("");
  const results = useQuery({
    ...searchQuery(q, 15),
    enabled: q.trim().length > 1,
  });
  const tracks =
    results.data?.groups.find((g) => g.type === "track")?.items ?? [];
  const go = useMutation({
    mutationFn: (to: ItemSummary) =>
      unwrap(
        api.POST("/music/adventure", {
          body: { fromId: from.id, toId: to.id, length: 15 },
        }),
      ),
    onSuccess: (st) => {
      music.playStation(st);
      onClose();
    },
  });
  return (
    <Dialog open onClose={onClose} title="Sonic Adventure">
      <div className="space-y-4">
        <p className="text-sm text-muted">
          From <strong className="text-text">{from.title}</strong>, travel to
          any track through music that sounds in between. Where to?
        </p>
        {go.isError && <Alert tone="error">{go.error.message}</Alert>}
        <Input
          autoFocus
          placeholder="Search for a track"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          aria-label="Destination track"
        />
        {results.isFetching && <Spinner />}
        <ul className="max-h-80 space-y-1 overflow-y-auto">
          {tracks.map((t) => (
            <li key={t.id}>
              <button
                onClick={() => go.mutate(t)}
                disabled={go.isPending}
                className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-surface-2 disabled:opacity-50"
              >
                <span className="size-10 shrink-0 overflow-hidden rounded bg-surface-3">
                  {t.images?.poster && (
                    <img
                      src={imageUrl(t.images.poster, 40)}
                      alt=""
                      className="size-full object-cover"
                    />
                  )}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm">{t.title}</span>
                  <span className="block truncate text-xs text-muted">
                    {t.artistCredit ?? t.grandparentTitle}
                  </span>
                </span>
                <Compass className="size-4 text-muted" aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      </div>
    </Dialog>
  );
}
