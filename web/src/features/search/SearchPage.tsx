import { useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { searchQuery } from "@/api/queries";
import { Spinner } from "@/components/ui";
import { Poster } from "../browse/Poster";
import { groupLabels, resultSubtitle } from "./SearchBox";

export function SearchPage() {
  const { q } = useSearch({ from: "/search" });
  const results = useQuery(searchQuery(q, 50));
  return (
    <div className="p-6 lg:p-8">
      <h1 className="mb-6 text-2xl font-bold">Results for “{q}”</h1>
      {results.isPending && <Spinner />}
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
