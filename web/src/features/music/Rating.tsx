import { useMutation, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Star } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";

/**
 * Five stars with halves (0–10 on the server, MUSIC-11). Clicking the current value clears it.
 * The change shows at once and reverts if the server refuses it.
 */
export function Rating({
  itemId,
  value,
  size = "md",
  label = "Your rating",
  className,
  testId,
}: {
  itemId: number;
  value?: number;
  size?: "xs" | "sm" | "md";
  /** The group's accessible name, or a function of the current value. */
  label?: string | ((value?: number) => string);
  className?: string;
  testId?: string;
}) {
  const qc = useQueryClient();
  const [hover, setHover] = useState<number | null>(null);
  const [local, setLocal] = useState<number | undefined>(value);
  // A new value from the server (a refetch) replaces the local one.
  const [seen, setSeen] = useState(value);
  if (seen !== value) {
    setSeen(value);
    setLocal(value);
  }
  const rate = useMutation({
    mutationFn: (rating: number | null) =>
      unwrap(
        api.PUT("/items/{itemId}/rating", {
          params: { path: { itemId } },
          body: { rating },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["items"] });
      // Library grids and lists (Songs, My rating sort, Rated 4★+) show it too.
      void qc.invalidateQueries({ queryKey: ["libraries"] });
    },
  });
  const set = (next: number | null) => {
    const before = local;
    setLocal(next ?? undefined);
    rate.mutate(next, { onError: () => setLocal(before) });
  };
  const shown = hover ?? local ?? 0;
  const px = size === "xs" ? "size-3" : size === "sm" ? "size-4" : "size-5";
  return (
    <div
      className={clsx("flex shrink-0 items-center", className)}
      onMouseLeave={() => setHover(null)}
      role="radiogroup"
      aria-label={typeof label === "function" ? label(local) : label}
      data-testid={testId}
      data-rating={local ?? 0}
      onKeyDown={(e) => {
        // Arrow keys move in half stars.
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
        e.preventDefault();
        e.stopPropagation();
        const next = Math.min(
          10,
          Math.max(0, (local ?? 0) + (e.key === "ArrowRight" ? 1 : -1)),
        );
        set(next || null);
      }}
    >
      {[1, 2, 3, 4, 5].map((n) => {
        const full = shown >= n * 2;
        const half = !full && shown >= n * 2 - 1;
        return (
          <button
            key={n}
            type="button"
            className={clsx("relative", size === "xs" ? "p-px" : "p-0.5")}
            aria-label={`${n} star${n > 1 ? "s" : ""}`}
            role="radio"
            aria-checked={(local ?? 0) >= n * 2}
            onMouseMove={(e) => {
              const r = e.currentTarget.getBoundingClientRect();
              setHover(e.clientX - r.left < r.width / 2 ? n * 2 - 1 : n * 2);
            }}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              // Where the click landed (works for taps too, which have no hover).
              const r = e.currentTarget.getBoundingClientRect();
              const v =
                e.clientX > 0
                  ? e.clientX - r.left < r.width / 2
                    ? n * 2 - 1
                    : n * 2
                  : (hover ?? n * 2);
              set(v === local ? null : v);
            }}
          >
            {/* The outline and the fill share one box so a half star is exactly half the glyph. */}
            <span className={clsx("relative block", px)}>
              <Star className={clsx(px, "block text-faint")} aria-hidden />
              {(full || half) && (
                <Star
                  data-fill={half ? "half" : "full"}
                  className={clsx(px, "absolute inset-0 block fill-accent text-accent")}
                  style={half ? { clipPath: "inset(0 50% 0 0)" } : undefined}
                  aria-hidden
                />
              )}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/**
 * A track row's rating: small stars you can click (halves on the left half of a star; the
 * current value again clears it). Unrated rows show faint outline stars only on hover or
 * focus, so rows stay tidy. The row needs the `group` class.
 */
export function RowRating({ item, className }: { item: { id: number; title: string; userRating?: number }; className?: string }) {
  const [rated, setRated] = useState(!!item.userRating);
  return (
    <div
      className={clsx(
        "shrink-0 transition-opacity",
        !rated && "opacity-0 group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-40",
        className,
      )}
      onClickCapture={() => setRated(true)}
    >
      <Rating
        itemId={item.id}
        value={item.userRating}
        size="xs"
        label={(v) => (v ? `Rated ${v / 2} star${v === 2 ? "" : "s"}: ${item.title}` : `Rate ${item.title}`)}
        testId="row-rating"
      />
    </div>
  );
}

/**
 * The person's rating as five small read-only stars (halves included), for track rows and cards.
 * Renders nothing when unrated, so rows stay tidy.
 */
export function RatingStars({ value, className, label, testId = "row-rating" }: { value?: number; className?: string; label?: string; testId?: string }) {
  if (!value) return null;
  const stars = value / 2;
  return (
    <span
      className={clsx("inline-flex shrink-0 items-center", className)}
      role="img"
      aria-label={label ?? `Rated ${stars} star${stars === 1 ? "" : "s"}`}
      data-testid={testId}
      data-rating={value}
    >
      {[1, 2, 3, 4, 5].map((n) => {
        const full = value >= n * 2;
        const half = !full && value >= n * 2 - 1;
        return (
          <span key={n} className="relative block size-3">
            <Star className="block size-3 text-faint/60" aria-hidden />
            {(full || half) && (
              <Star
                className="absolute inset-0 block size-3 fill-accent text-accent"
                style={half ? { clipPath: "inset(0 50% 0 0)" } : undefined}
                aria-hidden
              />
            )}
          </span>
        );
      })}
    </span>
  );
}

/** "4.2" for an average of 8.4 out of 10. */
export const starsText = (v: number) => (v / 2).toFixed(1);

/** Everyone's ratings combined, compact: "★ 4.2 (7)". Nothing when nobody rated it. */
export function CommunityScore({ rating, className }: { rating?: { average: number; count: number }; className?: string }) {
  if (!rating?.count) return null;
  return (
    <span
      className={clsx("inline-flex shrink-0 items-center gap-0.5 text-xs text-muted tabular-nums", className)}
      title={`Community rating ${starsText(rating.average)} from ${rating.count} rating${rating.count === 1 ? "" : "s"}`}
      aria-label={`Community rating ${starsText(rating.average)} from ${rating.count} rating${rating.count === 1 ? "" : "s"}`}
      data-testid="community-score"
    >
      <Star className="size-3 fill-current" aria-hidden />
      {starsText(rating.average)} ({rating.count})
    </span>
  );
}
