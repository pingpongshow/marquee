import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { X } from "lucide-react";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { meQuery, useUpdateMe } from "@/api/queries";
import type { ItemSummary } from "@/api/types";
import { Rating } from "../music/Rating";
import { promptTitle } from "./ratingRules";

/**
 * The rating request at the end of a movie or episode (USER-19): a small, non-blocking card
 * with the art, the title, five stars (halves too), an optional comment, "Not now" and
 * "Don't ask again". Choosing stars saves at once, says thanks and closes.
 */
export function RatingPromptCard({
  item,
  onClose,
  onInteract,
  autoHideMs,
  className,
}: {
  item: ItemSummary;
  onClose: () => void;
  /** The person touched the card (Up Next holds its autoplay while they do). */
  onInteract?: () => void;
  /** Close on its own after this long, unless the person interacts. */
  autoHideMs?: number;
  className?: string;
}) {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const updateMe = useUpdateMe();
  const [thanks, setThanks] = useState(false);
  const [commenting, setCommenting] = useState(false);
  const [comment, setComment] = useState("");
  const [touched, setTouched] = useState(false);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  });

  const touch = () => {
    if (!touched) setTouched(true);
    onInteract?.();
  };
  useEffect(() => {
    if (!autoHideMs || touched || thanks) return;
    const t = window.setTimeout(() => closeRef.current(), autoHideMs);
    return () => window.clearTimeout(t);
  }, [autoHideMs, touched, thanks]);
  useEffect(() => {
    if (!thanks) return;
    const t = window.setTimeout(() => closeRef.current(), 1500);
    return () => window.clearTimeout(t);
  }, [thanks]);

  const review = useMutation({
    mutationFn: (text: string) =>
      unwrap(
        api.PUT("/items/{itemId}/reviews", {
          params: { path: { itemId: item.id } },
          body: { comment: text },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["items", item.id, "reviews"] });
      setThanks(true);
    },
  });

  const title = promptTitle(item);
  const art =
    item.type === "episode"
      ? (item.images?.thumb ?? item.images?.poster)
      : (item.images?.poster ?? item.images?.thumb);

  return (
    <div
      role="region"
      aria-label={`Rate ${title}`}
      data-testid="rating-prompt"
      className={clsx(
        "w-80 overflow-hidden rounded-lg border border-border bg-surface/95 text-text shadow-2xl backdrop-blur",
        className,
      )}
      onPointerDown={touch}
      onFocus={touch}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      {thanks ? (
        <div role="status" className="p-4 text-center font-semibold">
          Thanks!
        </div>
      ) : (
        <div className="flex gap-3 p-3">
          {art && (
            <img
              src={imageUrl(art, 160)}
              alt=""
              className={clsx(
                "shrink-0 rounded object-cover",
                item.type === "episode" && item.images?.thumb
                  ? "aspect-video w-24 self-start"
                  : "aspect-[2/3] w-14",
              )}
            />
          )}
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex items-start gap-2">
              <div className="min-w-0 flex-1">
                <div className="text-xs text-muted">How was it?</div>
                <div className="line-clamp-2 text-sm font-semibold">{title}</div>
              </div>
              <button
                type="button"
                onClick={onClose}
                className="shrink-0 rounded p-0.5 text-muted hover:text-text"
                aria-label="Close"
              >
                <X className="size-4" />
              </button>
            </div>
            <Rating
              itemId={item.id}
              value={item.userRating}
              label={`Rate ${title}`}
              testId="rating-prompt-stars"
              onChange={(v) => v && !commenting && setThanks(true)}
            />
            {commenting ? (
              <form
                className="space-y-1.5"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (comment.trim()) review.mutate(comment.trim());
                }}
              >
                <textarea
                  autoFocus
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  rows={2}
                  maxLength={2000}
                  aria-label="Comment"
                  placeholder="What did you think?"
                  className="w-full resize-none rounded border border-border bg-surface-2 px-2 py-1 text-sm"
                />
                <div className="flex justify-end gap-2 text-xs">
                  <button type="button" onClick={() => setCommenting(false)} className="text-muted hover:text-text">
                    Cancel
                  </button>
                  <button
                    type="submit"
                    disabled={!comment.trim() || review.isPending}
                    className="rounded bg-accent px-2 py-1 font-semibold text-black disabled:opacity-50"
                  >
                    Post comment
                  </button>
                </div>
              </form>
            ) : (
              <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
                <button type="button" onClick={() => setCommenting(true)} className="text-accent hover:underline">
                  Add a comment
                </button>
                <button type="button" onClick={onClose} className="text-muted hover:text-text">
                  Not now
                </button>
                <button
                  type="button"
                  disabled={updateMe.isPending}
                  onClick={() =>
                    updateMe.mutate(
                      { preferences: { ...me.data?.preferences, ratingPrompt: false } },
                      { onSuccess: onClose },
                    )
                  }
                  className="text-muted hover:text-text"
                >
                  Don't ask again
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// The card shown after the player closes (over the item page) or at the end with no Up Next.
let pending: { key: number; item: ItemSummary } | null = null;
let nextKey = 1;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((l) => l());

/** Ask for a rating of this item, over whatever page is showing. */
export function showRatingPrompt(item: ItemSummary) {
  pending = { key: nextKey++, item };
  emit();
}

function hide(key: number) {
  if (pending?.key !== key) return;
  pending = null;
  emit();
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/** Mounted once, above the players. */
export function RatingPromptHost() {
  const p = useSyncExternalStore(subscribe, () => pending);
  if (!p) return null;
  return (
    <div className="fixed right-4 bottom-24 z-[60] max-w-[calc(100vw-2rem)]">
      <RatingPromptCard key={p.key} item={p.item} autoHideMs={12_000} onClose={() => hide(p.key)} />
    </div>
  );
}
