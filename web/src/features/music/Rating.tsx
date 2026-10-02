import { useMutation, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Star } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";

/** Five stars with halves (0–10 on the server, MUSIC-11). Clicking the current value clears it. */
export function Rating({ itemId, value, size = "md" }: { itemId: number; value?: number; size?: "sm" | "md" }) {
  const qc = useQueryClient();
  const [hover, setHover] = useState<number | null>(null);
  const [local, setLocal] = useState<number | undefined>(value);
  const rate = useMutation({
    mutationFn: (rating: number | null) => unwrap(api.PUT("/items/{itemId}/rating", { params: { path: { itemId } }, body: { rating } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["items"] }),
  });
  const shown = hover ?? local ?? 0;
  const px = size === "sm" ? "size-4" : "size-5";
  return (
    <div
      className="flex items-center"
      onMouseLeave={() => setHover(null)}
      role="radiogroup"
      aria-label="Your rating"
      onKeyDown={(e) => {
        // Arrow keys move in half stars.
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
        e.preventDefault();
        const next = Math.min(10, Math.max(0, (local ?? 0) + (e.key === "ArrowRight" ? 1 : -1)));
        setLocal(next || undefined);
        rate.mutate(next || null);
      }}
    >
      {[1, 2, 3, 4, 5].map((n) => {
        const full = shown >= n * 2;
        const half = !full && shown >= n * 2 - 1;
        return (
          <button
            key={n}
            type="button"
            className="relative p-0.5"
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
              const v = e.clientX > 0 ? (e.clientX - r.left < r.width / 2 ? n * 2 - 1 : n * 2) : (hover ?? n * 2);
              const next = v === local ? null : v;
              setLocal(next ?? undefined);
              rate.mutate(next);
            }}
          >
            <Star className={clsx(px, "text-faint")} aria-hidden />
            {(full || half) && (
              <span className="absolute inset-0.5 overflow-hidden" style={{ width: half ? "50%" : undefined }}>
                <Star className={clsx(px, "fill-accent text-accent")} aria-hidden />
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
