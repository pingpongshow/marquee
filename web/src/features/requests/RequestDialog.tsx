import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Dialog, Spinner } from "@/components/ui";
import { availabilityLabel, type DiscoverItem, useRequestAction } from "./api";

/** Ask for a movie, or chosen seasons of a show. An admin approves it before it goes to Seerr. */
export function RequestDialog({
  item,
  onClose,
}: {
  item: DiscoverItem;
  onClose: () => void;
}) {
  const show = useQuery({
    queryKey: ["requests", "tv", item.tmdbId],
    queryFn: () =>
      unwrap(
        api.GET("/requests/tv/{tmdbId}", {
          params: { path: { tmdbId: item.tmdbId } },
        }),
      ),
    enabled: item.mediaType === "tv",
  });
  const open = (show.data?.seasons ?? []).filter(
    (s) => s.availability === "none",
  );
  const [picked, setPicked] = useState<Set<number> | null>(null);
  const chosen = picked ?? new Set(open.map((s) => s.number));
  const create = useRequestAction(() =>
    unwrap(
      api.POST("/requests", {
        body: {
          tmdbId: item.tmdbId,
          mediaType: item.mediaType,
          seasons: item.mediaType === "tv" ? [...chosen] : undefined,
        },
      }),
    ),
  );
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Request ${item.title}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={
              create.isPending ||
              create.isSuccess ||
              (item.mediaType === "tv" && chosen.size === 0)
            }
            onClick={() =>
              create.mutate(undefined, {
                onSuccess: () => setTimeout(onClose, 1200),
              })
            }
          >
            {create.isPending ? "Requesting…" : "Request"}
          </Button>
        </>
      }
    >
      <div className="flex gap-4">
        {item.posterUrl && (
          <img
            src={item.posterUrl}
            alt=""
            className="w-28 shrink-0 rounded-md"
          />
        )}
        <div className="space-y-3 text-sm">
          <p className="text-muted">
            {[item.year, item.mediaType === "tv" ? "Series" : "Movie"]
              .filter(Boolean)
              .join(" · ")}
          </p>
          {item.overview && <p className="line-clamp-6">{item.overview}</p>}
          <p className="text-faint">
            An admin approves requests before they're downloaded.
          </p>
        </div>
      </div>
      {item.mediaType === "tv" &&
        (show.isPending ? (
          <Spinner />
        ) : (
          <fieldset className="mt-4 grid gap-2 sm:grid-cols-2">
            <legend className="mb-2 text-sm font-medium">Seasons</legend>
            {show.data?.seasons.map((s) => {
              const can = s.availability === "none";
              return (
                <label
                  key={s.number}
                  className="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2 text-sm"
                >
                  <input
                    type="checkbox"
                    className="size-4 accent-[var(--color-accent)]"
                    disabled={!can}
                    checked={can && chosen.has(s.number)}
                    onChange={(e) => {
                      const next = new Set(chosen);
                      if (e.target.checked) next.add(s.number);
                      else next.delete(s.number);
                      setPicked(next);
                    }}
                  />
                  <span>{s.name ?? `Season ${s.number}`}</span>
                  <span className="ml-auto text-xs text-faint">
                    {can
                      ? `${s.episodeCount ?? "?"} episodes`
                      : availabilityLabel[s.availability]}
                  </span>
                </label>
              );
            })}
          </fieldset>
        ))}
      {create.isSuccess && (
        <Alert tone="success">
          Requested. You'll see it under My requests.
        </Alert>
      )}
      {create.error && <Alert tone="error">{create.error.message}</Alert>}
    </Dialog>
  );
}
