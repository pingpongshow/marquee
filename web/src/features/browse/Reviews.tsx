import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageSquare, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery } from "@/api/queries";
import type { ItemDetail } from "@/api/types";
import { Alert, Button, Spinner } from "@/components/ui";
import { timeAgo } from "@/lib/time";
import { Rating, RatingStars, starsText } from "../music/Rating";
import { Avatar } from "../users/Avatar";

/** Item types people rate and comment on (USER-17). */
export const reviewable = (t: string) => ["movie", "show", "episode", "video", "album", "artist", "track"].includes(t);

const reviewsQuery = (itemId: number) => ({
  queryKey: ["items", itemId, "reviews"],
  queryFn: () => unwrap(api.GET("/items/{itemId}/reviews", { params: { path: { itemId } } })),
});

/**
 * The header's ratings: yours (tap to rate) and the community's (read-only), which opens the
 * Ratings & comments section below.
 */
export function ItemRatings({ item }: { item: ItemDetail }) {
  const c = item.communityRating;
  const open = () => document.getElementById("ratings")?.scrollIntoView({ behavior: "smooth", block: "start" });
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-2">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium tracking-wide text-muted uppercase">Your rating</span>
        <Rating key={item.id} itemId={item.id} value={item.userRating} />
      </div>
      {reviewable(item.type) && (
        <button type="button" onClick={open} className="flex items-center gap-2 rounded-md text-left hover:text-text" data-testid="community-rating">
          <span className="text-xs font-medium tracking-wide text-muted uppercase">Community</span>
          {c?.count ? (
            <>
              <RatingStars value={Math.round(c.average)} label={`Community rating ${starsText(c.average)} stars`} testId="community-stars" />
              <span className="text-sm text-muted tabular-nums">
                {starsText(c.average)} · {c.count} rating{c.count === 1 ? "" : "s"}
              </span>
            </>
          ) : (
            <span className="text-sm text-muted">Not rated yet</span>
          )}
          <span className="flex items-center gap-1 text-sm text-accent">
            <MessageSquare className="size-4" aria-hidden /> Ratings & comments
          </span>
        </button>
      )}
    </div>
  );
}

/** Everyone's ratings and comments, and a box for yours. Admins can remove anyone's comment. */
export function Reviews({ item }: { item: ItemDetail }) {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const list = useQuery(reviewsQuery(item.id));
  const [draft, setDraft] = useState<string | null>(null);
  const done = () => {
    setDraft(null);
    return qc.invalidateQueries({ queryKey: ["items", item.id, "reviews"] });
  };
  const save = useMutation({
    mutationFn: (comment: string) => unwrap(api.PUT("/items/{itemId}/reviews", { params: { path: { itemId: item.id } }, body: { comment } })),
    onSuccess: done,
  });
  const remove = useMutation({
    mutationFn: (userId: number) => unwrap(api.DELETE("/items/{itemId}/reviews/{userId}", { params: { path: { itemId: item.id, userId } } })),
    onSuccess: done,
  });
  if (!reviewable(item.type)) return null;
  const reviews = list.data?.reviews ?? [];
  const mine = reviews.find((r) => r.mine);
  const others = reviews.filter((r) => !r.mine);
  const text = draft ?? mine?.comment ?? "";
  const changed = text.trim() !== (mine?.comment ?? "");
  const isAdmin = !!me.data?.isAdmin;
  const error = save.error ?? remove.error;
  return (
    <section id="ratings" className="mt-10 max-w-3xl scroll-mt-4" aria-labelledby="ratings-title">
      <div className="mb-4 flex flex-wrap items-baseline gap-3">
        <h2 id="ratings-title" className="text-lg font-semibold">
          Ratings & comments
        </h2>
        {!!list.data?.count && list.data.average !== undefined && (
          <span className="text-sm text-muted tabular-nums">
            ★ {starsText(list.data.average)} · {list.data.count} rating{list.data.count === 1 ? "" : "s"}
          </span>
        )}
      </div>
      <div className="rounded-lg border border-border bg-surface p-4">
        <div className="mb-2 flex items-center gap-3">
          <span className="text-sm font-medium">Your rating</span>
          <Rating itemId={item.id} value={item.userRating} size="sm" label="Rate it" />
        </div>
        <textarea
          aria-label="Your comment"
          value={text}
          maxLength={2000}
          rows={3}
          placeholder="What did you think?"
          onChange={(e) => setDraft(e.target.value)}
          className="w-full resize-y rounded-md border border-border bg-surface-2 p-2 text-sm placeholder:text-faint focus:border-accent focus:outline-none"
        />
        <div className="mt-2 flex items-center gap-2">
          <Button size="sm" variant="primary" disabled={!changed || !text.trim()} loading={save.isPending} onClick={() => save.mutate(text.trim())}>
            {mine?.comment ? "Save" : "Post"}
          </Button>
          {mine?.comment && (
            <Button size="sm" variant="ghost" loading={save.isPending && !text} onClick={() => save.mutate("")}>
              <Trash2 className="size-4" /> Delete
            </Button>
          )}
          {draft !== null && changed && (
            <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
              Cancel
            </Button>
          )}
          <span className="ml-auto text-xs text-faint tabular-nums">{text.length}/2000</span>
        </div>
        {error && <div className="mt-2"><Alert tone="error">{error.message}</Alert></div>}
      </div>
      {list.isPending && <Spinner />}
      {list.isError && <Alert tone="error">{list.error.message}</Alert>}
      {list.data && others.length === 0 && <p className="mt-4 text-sm text-muted">No one else has rated this yet.</p>}
      {reviews.length > 0 && (
        <ul className="mt-4 divide-y divide-border" data-testid="reviews">
          {reviews.map((r) => (
            <li key={r.userId} className="flex gap-3 py-3" aria-label={`${r.mine ? "You" : r.userName}${r.rating ? `, ${r.rating / 2} stars` : ""}`}>
              <Avatar name={r.userName} url={r.avatarUrl} className="size-9 text-sm" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{r.userName}</span>
                  {r.mine && <span className="rounded bg-accent/15 px-1.5 text-xs text-accent">You</span>}
                  <RatingStars value={r.rating} label={`${r.rating ? r.rating / 2 : 0} stars`} testId="review-stars" />
                  <span className="text-xs text-faint">{timeAgo(r.updatedAt)}</span>
                  {isAdmin && !r.mine && r.comment && (
                    <button
                      type="button"
                      onClick={() => remove.mutate(r.userId)}
                      className="ml-auto rounded p-1 text-muted hover:text-danger"
                      aria-label={`Delete ${r.userName}'s comment`}
                    >
                      <Trash2 className="size-4" />
                    </button>
                  )}
                </div>
                {r.comment && <p className="mt-1 text-sm whitespace-pre-line text-text/90">{r.comment}</p>}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
