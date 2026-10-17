import { useMutation, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Heart } from "lucide-react";
import { useState, type MouseEvent } from "react";
import { api, unwrap } from "@/api/client";

/** What can be a favourite (the heart). */
export const favoriteTypes = new Set(["movie", "show", "episode", "track", "album", "artist"]);

/** Sets or clears a favourite; lists and pages that show it refresh once the server has it. */
export function useSetFavorite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ itemId, on }: { itemId: number; on: boolean }) =>
      on
        ? unwrap(api.PUT("/items/{itemId}/favorite", { params: { path: { itemId } } }))
        : unwrap(api.DELETE("/items/{itemId}/favorite", { params: { path: { itemId } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["items"] });
      // Library lists filtered or sorted by favourites, playlists and search results show it too.
      void qc.invalidateQueries({ queryKey: ["libraries"] });
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      void qc.invalidateQueries({ queryKey: ["search"] });
    },
  });
}

/**
 * The heart: filled when the item is one of the person's favourites. The change shows at once
 * and reverts if the server refuses it.
 */
export function FavoriteButton({
  item,
  size = "md",
  className,
}: {
  item: { id: number; title: string; favorite?: boolean };
  size?: "xs" | "sm" | "md";
  className?: string;
}) {
  const setFavorite = useSetFavorite();
  const [local, setLocal] = useState(!!item.favorite);
  // A new value from the server (a refetch) replaces the local one.
  const [seen, setSeen] = useState(!!item.favorite);
  if (seen !== !!item.favorite) {
    setSeen(!!item.favorite);
    setLocal(!!item.favorite);
  }
  const toggle = (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const next = !local;
    setLocal(next);
    setFavorite.mutate({ itemId: item.id, on: next }, { onError: () => setLocal(!next) });
  };
  const px = size === "xs" ? "size-3.5" : size === "sm" ? "size-4" : "size-5";
  return (
    <button
      type="button"
      onClick={toggle}
      aria-pressed={local}
      aria-label={local ? `Remove ${item.title} from favourites` : `Add ${item.title} to favourites`}
      title={local ? "Remove from favourites" : "Add to favourites"}
      data-testid="favorite-button"
      data-favorite={local}
      className={clsx(
        "shrink-0 rounded-full transition-colors",
        size === "md" ? "p-1.5 hover:bg-surface-3" : "p-0.5",
        local ? "text-accent" : "text-muted hover:text-text",
        className,
      )}
    >
      <Heart className={clsx(px, local && "fill-current")} aria-hidden />
    </button>
  );
}

/**
 * A track row's heart: always shown when it's a favourite, otherwise only on hover or focus so
 * rows stay tidy. The row needs the `group` class.
 */
export function RowFavorite({ item, className }: { item: { id: number; title: string; favorite?: boolean }; className?: string }) {
  const [shown, setShown] = useState(!!item.favorite);
  return (
    <span
      className={clsx(
        "inline-flex shrink-0 transition-opacity",
        !shown && !item.favorite && "opacity-0 group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-40",
        className,
      )}
      onClickCapture={() => setShown(true)}
    >
      <FavoriteButton item={item} size="xs" />
    </span>
  );
}
