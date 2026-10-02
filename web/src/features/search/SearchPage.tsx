import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useState } from "react";
import { searchQuery } from "@/api/queries";
import { Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { groupLabels, resultSubtitle } from "./SearchBox";

export function SearchPage() {
  const { q } = useSearch({ from: "/search" });
  const navigate = useNavigate({ from: "/search" });
  const [text, setText] = useState(q);
  // Update the URL (and results) shortly after typing stops.
  useEffect(() => {
    if (text.trim() === q) return;
    const t = window.setTimeout(() => navigate({ search: { q: text.trim() }, replace: true }), 250);
    return () => window.clearTimeout(t);
  }, [text, q, navigate]);
  const results = useQuery({ ...searchQuery(q, 50), enabled: q.length > 0 });
  return (
    <div className="p-6 lg:p-8">
      <div className="relative mb-6 max-w-xl">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-5 -translate-y-1/2 text-faint" aria-hidden />
        <input
          type="search"
          autoFocus={!q}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Search movies, shows, music and people"
          aria-label="Search everything"
          className="h-11 w-full rounded-full border border-border bg-surface-2 pr-4 pl-10 placeholder:text-faint focus:border-accent focus:outline-none"
        />
      </div>
      {q && <h1 className="mb-6 text-2xl font-bold">Results for “{q}”</h1>}
      {q && results.isPending && <Spinner />}
      {results.data?.groups.length === 0 && <p className="text-muted">Nothing found.</p>}
      {results.data?.groups.map((g) => (
        <section key={g.type} className="mb-10">
          <h2 className="mb-4 text-lg font-semibold">{groupLabels[g.type]}</h2>
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6">
            {g.items.map((it) => (
              <li key={it.id}>
                <Link to="/item/$itemId" params={{ itemId: String(it.id) }} className="group block">
                  <Poster item={it} shape={it.type === "episode" ? "wide" : it.type === "artist" || it.type === "album" || it.type === "track" ? "square" : "poster"} className="group-hover:ring-2 group-hover:ring-accent" />
                  <div className="mt-2 truncate text-sm font-medium">{it.title}</div>
                  <div className="truncate text-xs text-muted">{resultSubtitle(it)}</div>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
