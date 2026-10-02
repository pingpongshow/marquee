import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { imageUrl } from "@/api/client";
import { searchQuery } from "@/api/queries";
import type { ItemSummary } from "@/api/types";

export const groupLabels: Record<string, string> = { movie: "Movies", show: "Shows", episode: "Episodes", artist: "Artists", album: "Albums", track: "Tracks", video: "Videos" };

export function resultSubtitle(it: ItemSummary) {
  switch (it.type) {
    case "episode":
      return `${it.grandparentTitle ?? ""} · S${it.parentTitle?.replace(/\D/g, "") || "?"} E${it.index ?? "?"}`;
    case "album":
      return it.artistCredit ?? it.parentTitle ?? "";
    case "track":
      return [it.artistCredit, it.parentTitle].filter(Boolean).join(" · ");
    default:
      return it.year ? String(it.year) : "";
  }
}

function useDebounced<T>(value: T, ms: number) {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** Header search with instant results grouped by type; Enter opens the full results page. */
export function SearchBox() {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const debounced = useDebounced(q.trim(), 200);
  const results = useQuery({ ...searchQuery(debounced, 4), placeholderData: (prev) => prev });
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, []);

  const go = (it: ItemSummary) => {
    setOpen(false);
    setQ("");
    navigate({ to: "/item/$itemId", params: { itemId: String(it.id) } });
  };
  const groups = debounced ? (results.data?.groups ?? []) : [];

  return (
    <div className="relative mx-auto hidden w-full max-w-md md:block" ref={ref}>
      <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-faint" aria-hidden />
      <input
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && q.trim()) {
            setOpen(false);
            navigate({ to: "/search", search: { q: q.trim() } });
          }
          if (e.key === "Escape") setOpen(false);
        }}
        placeholder="Search"
        aria-label="Search"
        className="h-9 w-full rounded-full border border-border bg-surface-2 pr-4 pl-9 text-sm placeholder:text-faint focus:border-accent focus:outline-none"
      />
      {open && debounced && (
        <div className="absolute inset-x-0 z-40 mt-2 max-h-[70vh] overflow-y-auto rounded-lg border border-border bg-surface shadow-2xl">
          {groups.length === 0 && !results.isFetching && <p className="px-4 py-6 text-center text-sm text-muted">No results for “{debounced}”.</p>}
          {groups.map((g) => (
            <div key={g.type} className="py-1">
              <div className="px-4 py-1 text-[11px] font-semibold tracking-wider text-faint uppercase">{groupLabels[g.type]}</div>
              {g.items.map((it) => (
                <button key={it.id} onClick={() => go(it)} className="flex w-full items-center gap-3 px-4 py-1.5 text-left hover:bg-surface-2">
                  <span className="h-12 w-8 shrink-0 overflow-hidden rounded bg-surface-3">
                    {it.images?.poster && <img src={imageUrl(it.images.poster, 40)} alt="" className="size-full object-cover" />}
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-sm">{it.title}</span>
                    <span className="block truncate text-xs text-muted">{resultSubtitle(it)}</span>
                  </span>
                </button>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
