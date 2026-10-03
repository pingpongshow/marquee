import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { Sparkles } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";

export const recapYearsQuery = {
  queryKey: ["recap", "years"],
  queryFn: () => unwrap(api.GET("/me/recap/years")),
  staleTime: 5 * 60_000,
};

/**
 * "Your Year in Music" (MUSIC-22): one compact row with a year picker (only when there's
 * more than one year) and a View link; nothing until there's music history.
 */
export function RecapCard({ className }: { className?: string }) {
  const years = useQuery(recapYearsQuery);
  const [year, setYear] = useState<number | null>(null);
  const list = years.data ?? [];
  if (!list.length) return null;
  const y = year ?? list[0]!;
  return (
    <div
      data-testid="recap-card"
      className={clsx("flex items-center gap-3 rounded-lg border border-border border-l-2 border-l-accent bg-surface px-3 py-2", className)}
    >
      <Sparkles className="size-4 shrink-0 text-accent" aria-hidden />
      <div className="min-w-0 flex-1 truncate text-sm font-medium">Your {y} in Music</div>
      {list.length > 1 && (
        <select
          aria-label="Recap year"
          value={y}
          onChange={(e) => setYear(Number(e.target.value))}
          className="h-7 shrink-0 rounded border border-border bg-surface-2 px-1.5 text-xs text-text"
        >
          {list.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      )}
      <Link
        to="/recap"
        search={{ year: y }}
        aria-label={`View your ${y} in Music`}
        className="shrink-0 rounded-md bg-surface-3 px-3 py-1 text-xs font-medium whitespace-nowrap text-text hover:bg-border"
      >
        View
      </Link>
    </div>
  );
}
