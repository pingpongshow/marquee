import { useMutation, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Star, ThumbsDown } from "lucide-react";
import { useState, type MouseEvent } from "react";
import { api, unwrap } from "@/api/client";
import { useMusicActions } from "../player/MusicPlayer";

/** A rating from here up (3.5 stars) and a thumbs down take each other back, as on the server. */
const TOP_RATING = 7;

type Song = { id: number; title: string; disliked?: boolean };

/**
 * Gives a song a thumbs down or takes it back (MUSIC-25). Everything that shows the song refreshes
 * once the server has it, the heart and high stars it clears included, and a station moves on.
 */
export function useSetDisliked() {
  const qc = useQueryClient();
  const music = useMusicActions();
  return useMutation({
    mutationFn: ({ itemId, on }: { itemId: number; on: boolean }) =>
      on
        ? unwrap(api.PUT("/items/{itemId}/dislike", { params: { path: { itemId } } }))
        : unwrap(api.DELETE("/items/{itemId}/dislike", { params: { path: { itemId } } })),
    onSuccess: (_, { itemId, on }) => {
      void qc.invalidateQueries({ queryKey: ["items"] });
      void qc.invalidateQueries({ queryKey: ["libraries"] });
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      void qc.invalidateQueries({ queryKey: ["search"] });
      // Mixes and samplers are held for a while, and these are generated lists that lose the song.
      void qc.invalidateQueries({ queryKey: ["music"] });
      if (on) music.thumbsDown(itemId);
    },
  });
}

/** A song's thumbs down as it shows: the change shows at once and reverts if the server refuses it. */
function useThumbsDown(song?: Song) {
  const setDisliked = useSetDisliked();
  const value = !!song?.disliked;
  const [on, setOn] = useState(value);
  // A new value from the server (a refetch) replaces the local one.
  const [seen, setSeen] = useState(value);
  if (seen !== value) {
    setSeen(value);
    setOn(value);
  }
  const toggle = (onError: () => void) => {
    if (!song) return;
    const next = !on;
    setOn(next);
    setDisliked.mutate(
      { itemId: song.id, on: next },
      {
        onError: () => {
          setOn(!next);
          onError();
        },
      },
    );
  };
  return { on, setOn, toggle };
}

/** The thumbs down itself, filled when it's given; pressing it again takes it back. */
export function ThumbsDownButton({
  title,
  on,
  onToggle,
  size = "md",
  className,
}: {
  title: string;
  on: boolean;
  onToggle: () => void;
  size?: "xs" | "sm" | "md";
  className?: string;
}) {
  const px = size === "xs" ? "size-3.5" : size === "sm" ? "size-4" : "size-5";
  return (
    <button
      type="button"
      onClick={(e: MouseEvent) => {
        // Works inside a row's link.
        e.preventDefault();
        e.stopPropagation();
        onToggle();
      }}
      aria-pressed={on}
      aria-label={on ? `Take back thumbs down: ${title}` : `Thumbs down: ${title}`}
      title={on ? "Take back thumbs down" : "Thumbs down"}
      data-testid="thumbs-down"
      data-disliked={on}
      className={clsx(
        "shrink-0 rounded-full transition-colors",
        size === "md" ? "p-1.5 hover:bg-surface-3" : "p-0.5",
        on ? "text-accent" : "text-muted hover:text-text",
        className,
      )}
    >
      <ThumbsDown className={clsx(px, on && "fill-current")} aria-hidden />
    </button>
  );
}

/**
 * Five stars with halves (0–10 on the server, MUSIC-11). Clicking the current value clears it.
 * The change shows at once and reverts if the server refuses it. A song also gets its thumbs down,
 * and a high rating and a thumbs down clear each other at once here (the heart follows its refetch).
 */
export function Rating({
  itemId,
  save,
  value,
  size = "md",
  label = "Your rating",
  className,
  testId,
  onChange,
  song,
  row,
  thumbClassName,
}: {
  /** The library item rated (PUT /items/{itemId}/rating), unless save is given. */
  itemId?: number;
  /** Saves the rating somewhere else (a film or series on demand). */
  save?: (rating: number | null) => Promise<unknown>;
  value?: number;
  size?: "xs" | "sm" | "md";
  /** The group's accessible name, or a function of the current value. */
  label?: string | ((value?: number) => string);
  className?: string;
  testId?: string;
  /** Called once the server has saved a new value. */
  onChange?: (value: number | null) => void;
  /** When rating a song: adds the thumbs-down control, after the stars. */
  song?: Song;
  /** In a song row: the thumbs down shows on hover, and in place of the stars once it's given. */
  row?: boolean;
  thumbClassName?: string;
}) {
  const qc = useQueryClient();
  const thumbs = useThumbsDown(song);
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
      save
        ? save(rating)
        : unwrap(
            api.PUT("/items/{itemId}/rating", {
              params: { path: { itemId: itemId ?? 0 } },
              body: { rating },
            }),
          ),
    onSuccess: () => {
      if (save) return;
      void qc.invalidateQueries({ queryKey: ["items"] });
      // Library grids and lists (Songs, My rating sort, Rated 4★+) show it too.
      void qc.invalidateQueries({ queryKey: ["libraries"] });
      // So do playlists and search results, whose rows a rating from the player bar can change.
      void qc.invalidateQueries({ queryKey: ["playlists"] });
      void qc.invalidateQueries({ queryKey: ["search"] });
    },
  });
  const set = (next: number | null) => {
    const before = local;
    const dislikedBefore = thumbs.on;
    setLocal(next ?? undefined);
    if ((next ?? 0) >= TOP_RATING) thumbs.setOn(false);
    rate.mutate(next, {
      onError: () => {
        setLocal(before);
        thumbs.setOn(dislikedBefore);
      },
      onSuccess: () => onChange?.(next),
    });
  };
  const toggleThumb = () => {
    const before = local;
    if (!thumbs.on && (local ?? 0) >= TOP_RATING) setLocal(undefined);
    thumbs.toggle(() => setLocal(before));
  };
  const shown = hover ?? local ?? 0;
  const px = size === "xs" ? "size-3" : size === "sm" ? "size-4" : "size-5";
  const stars = (
    <div
      // A given thumbs down takes the row's stars' place; they keep their room so columns line up,
      // except on a phone, where rows have no room for stars.
      className={clsx("flex shrink-0 items-center", !song && className, row && thumbs.on && "invisible max-sm:hidden")}
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
  if (!song) return stars;
  return (
    <div className={clsx("flex shrink-0 items-center gap-0.5", className)}>
      {stars}
      <ThumbsDownButton
        title={song.title}
        on={thumbs.on}
        onToggle={toggleThumb}
        size={size}
        className={clsx(
          row && !thumbs.on && "opacity-0 group-hover:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100",
          thumbClassName,
        )}
      />
    </div>
  );
}

/**
 * A track row's rating: small stars you can click (halves on the left half of a star; the
 * current value again clears it). Unrated rows show faint outline stars only on hover or
 * focus, so rows stay tidy. A song with a thumbs down shows that instead, on a phone too, where
 * className usually hides the stars (it's one small icon). The row needs the `group` class.
 */
export function RowRating({ item, className }: { item: Song & { userRating?: number }; className?: string }) {
  const [rated, setRated] = useState(false);
  return (
    <div
      className={clsx(
        "shrink-0 transition-opacity",
        !rated && !item.userRating && !item.disliked && "opacity-0 group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-40",
        !item.disliked && className,
      )}
    >
      <Rating
        itemId={item.id}
        value={item.userRating}
        size="xs"
        label={(v) => (v ? `Rated ${v / 2} star${v === 2 ? "" : "s"}: ${item.title}` : `Rate ${item.title}`)}
        testId="row-rating"
        song={item}
        row
        onChange={() => setRated(true)}
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
