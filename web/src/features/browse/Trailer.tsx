import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Clapperboard, ExternalLink, X } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { api, ApiError, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import type { ItemDetail } from "@/api/types";
import { Button } from "@/components/ui";

type ItemTrailer = components["schemas"]["ItemTrailer"];

/** The embed URL for a YouTube trailer; youtube-nocookie keeps YouTube's cookies out until it plays. */
export function youtubeEmbedUrl(key: string) {
  return `https://www.youtube-nocookie.com/embed/${encodeURIComponent(key)}?autoplay=1&rel=0`;
}

/**
 * The Trailer button on a movie or show page (PLAY-22). It appears only once the server says
 * there is a trailer: a local one plays in the normal player, a YouTube one in a modal embed.
 * Nothing is downloaded to the server.
 */
export function TrailerButton({ item }: { item: ItemDetail }) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const enabled = item.type === "movie" || item.type === "show";
  const trailer = useQuery({
    queryKey: ["items", item.id, "trailer"],
    queryFn: () =>
      unwrap(api.GET("/items/{itemId}/trailer", { params: { path: { itemId: item.id } } })).catch((e) => {
        if (e instanceof ApiError && e.status === 404) return null; // no trailer: no button
        throw e;
      }),
    enabled,
    retry: false,
    staleTime: 10 * 60_000,
  });
  const t = trailer.data;
  if (!enabled || !t) return null;
  if (t.source === "local" && !t.itemId) return null;
  if (t.source === "youtube" && !t.youtubeKey) return null;
  return (
    <>
      <Button
        onClick={() =>
          t.source === "local" ? navigate({ to: "/play/$itemId", params: { itemId: String(t.itemId) }, search: {} }) : setOpen(true)
        }
      >
        <Clapperboard className="size-4" /> Trailer
      </Button>
      {open && t.source === "youtube" && <YouTubeTrailer trailer={t} title={item.title} onClose={() => setOpen(false)} />}
    </>
  );
}

function YouTubeTrailer({ trailer, title, onClose }: { trailer: ItemTrailer; title: string; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  const key = trailer.youtubeKey!;
  const watchUrl = trailer.url || `https://www.youtube.com/watch?v=${encodeURIComponent(key)}`;
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => e.target === e.currentTarget && onClose()}
      className="m-auto w-[calc(100%-2rem)] max-w-5xl overflow-hidden rounded-lg border border-border bg-surface p-0 text-text shadow-2xl backdrop:bg-black/80"
    >
      <header className="flex items-center justify-between gap-3 px-4 py-3">
        <h2 id={titleId} className="min-w-0 truncate text-base font-semibold">
          {trailer.name || `${title} – Trailer`}
        </h2>
        <div className="flex shrink-0 items-center gap-1">
          <a
            href={watchUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 rounded px-2 py-1 text-sm text-muted hover:bg-surface-2 hover:text-text"
          >
            <ExternalLink className="size-4" aria-hidden /> Watch on YouTube
          </a>
          <button type="button" onClick={onClose} className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Close">
            <X className="size-5" />
          </button>
        </div>
      </header>
      <div className="aspect-video w-full bg-black">
        <iframe
          src={youtubeEmbedUrl(key)}
          title={trailer.name || `${title} trailer`}
          className="size-full"
          allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
          referrerPolicy="strict-origin-when-cross-origin"
          allowFullScreen
        />
      </div>
    </dialog>
  );
}
