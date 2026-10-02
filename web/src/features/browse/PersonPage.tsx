import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { UserRound } from "lucide-react";
import { api, personPhotoUrl, unwrap } from "@/api/client";
import { Alert, Spinner } from "@/components/ui";
import { Poster } from "./Poster";

const roleLabel: Record<string, string> = { actor: "Acting", director: "Directing", writer: "Writing", creator: "Created", producer: "Production", composer: "Music" };

/** A person and everything in your libraries they're credited on (META-6). */
export function PersonPage() {
  const { personId } = useParams({ from: "/person/$personId" });
  const person = useQuery({
    queryKey: ["people", Number(personId)],
    queryFn: () => unwrap(api.GET("/people/{personId}", { params: { path: { personId: Number(personId) } } })),
  });
  if (person.isPending) return <Spinner />;
  if (person.isError) return <div className="p-8"><Alert tone="error">{person.error.message}</Alert></div>;
  const p = person.data;
  const groups = new Map<string, typeof p.credits>();
  for (const c of p.credits) groups.set(c.role, [...(groups.get(c.role) ?? []), c]);
  return (
    <div className="p-6 lg:p-8">
      <header className="mb-8 flex items-center gap-6">
        <div className="flex size-32 shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-3">
          {p.hasPhoto ? <img src={personPhotoUrl(p.id, 128)} alt="" className="size-full object-cover" /> : <UserRound className="size-12 text-faint" aria-hidden />}
        </div>
        <div>
          <h1 className="text-3xl font-bold">{p.name}</h1>
          <div className="mt-1 text-sm text-muted">
            {p.credits.length} {p.credits.length === 1 ? "title" : "titles"} in your libraries
          </div>
        </div>
      </header>
      {[...groups.entries()].map(([role, credits]) => (
        <section key={role} className="mb-10">
          <h2 className="mb-4 text-lg font-semibold">{roleLabel[role] ?? role}</h2>
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-x-4 gap-y-6">
            {credits.map((c) => (
              <li key={c.item.id}>
                <Link to="/item/$itemId" params={{ itemId: String(c.item.id) }} className="group block">
                  <Poster item={c.item} className="transition-transform group-hover:scale-[1.03] group-hover:ring-2 group-hover:ring-accent" />
                  <div className="mt-2 truncate text-sm font-medium">{c.item.title}</div>
                  <div className="truncate text-xs text-muted">{[c.item.year, c.character && `as ${c.character}`].filter(Boolean).join(" · ")}</div>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
      {p.credits.length === 0 && <p className="text-muted">Nothing in your libraries features {p.name}.</p>}
    </div>
  );
}
