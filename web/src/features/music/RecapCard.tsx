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

/** "Your Year in Music" (MUSIC-22): an entry card with a year picker; nothing until there's music history. */
export function RecapCard({ className }: { className?: string }) {
  const years = useQuery(recapYearsQuery);
  const [year, setYear] = useState<number | null>(null);
  const list = years.data ?? [];
  if (!list.length) return null;
  const y = year ?? list[0]!;
  return (
    <div
      className={clsx(
        "flex flex-wrap items-center gap-4 overflow-hidden rounded-xl bg-gradient-to-br from-fuchsia-600 via-violet-700 to-indigo-900 p-5 text-white shadow-lg",
        className,
      )}
    >
      <Sparkles className="size-8 shrink-0 text-amber-300" aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="text-lg font-extrabold tracking-tight">Your Year in Music</div>
        <div className="text-sm text-white/80">Your top artists, songs and listening habits of {y}.</div>
      </div>
      {list.length > 1 && (
        <select
          aria-label="Recap year"
          value={y}
          onChange={(e) => setYear(Number(e.target.value))}
          className="rounded-md border border-white/30 bg-black/20 px-2 py-1.5 text-sm text-white"
        >
          {list.map((v) => (
            <option key={v} value={v} className="text-black">
              {v}
            </option>
          ))}
        </select>
      )}
      <Link
        to="/recap"
        search={{ year: y }}
        className="rounded-full bg-white px-5 py-2 text-sm font-bold text-black hover:bg-amber-200"
      >
        See your {y}
      </Link>
    </div>
  );
}
