import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useState } from "react";
import { meQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
import { settingsGroups, type SettingsSection } from "./sections";

function matches(s: SettingsSection, q: string) {
  const hay = `${s.label} ${s.description} ${s.keywords ?? ""}`.toLowerCase();
  return q
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w));
}

/** Plex-style settings: grouped section nav on the left, the selected section on the right (ADM-8). */
export function SettingsLayout() {
  const me = useQuery(meQuery);
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  if (me.isPending) return <Spinner />;
  if (!me.data?.isAdmin)
    return (
      <div className="p-8">
        <Alert tone="error">Only administrators can change server settings.</Alert>
      </div>
    );
  return (
    <div className="flex min-h-full flex-col md:flex-row">
      <nav aria-label="Settings" className="shrink-0 border-b border-border bg-surface/50 p-3 md:sticky md:top-0 md:h-[calc(100vh-3.5rem)] md:w-56 md:overflow-y-auto md:border-r md:border-b-0">
        <div className="relative mb-4">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" aria-hidden />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              const first = settingsGroups.flatMap((g) => g.sections).find((s) => matches(s, query));
              if (e.key === "Enter" && first) {
                navigate({ to: "/settings/$section", params: { section: first.id } });
                setQuery("");
              }
            }}
            placeholder="Search settings"
            aria-label="Search settings"
            className="h-8 w-full rounded-md border border-border bg-surface-2 pr-2 pl-8 text-sm focus:border-accent focus:outline-none"
          />
        </div>
        {settingsGroups.map((g) => ({ ...g, sections: g.sections.filter((s) => !query.trim() || matches(s, query)) }))
          .filter((g) => g.sections.length)
          .map((g) => (
          <div key={g.label} className="mb-4">
            <div className="mb-1 px-3 text-xs font-semibold tracking-wider text-faint uppercase">{g.label}</div>
            <ul className="flex flex-wrap gap-1 md:block md:space-y-0.5">
              {g.sections.map((s) => (
                <li key={s.id}>
                  <Link
                    to="/settings/$section"
                    params={{ section: s.id }}
                    className="flex items-center gap-2.5 rounded-md px-3 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-text"
                    activeProps={{ className: "bg-surface-2 !text-text font-medium" }}
                  >
                    <s.icon className="size-4" aria-hidden />
                    {s.label}
                    {s.milestone && <span className="ml-auto text-[10px] text-faint">{s.milestone}</span>}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>
      <div className="min-w-0 flex-1 px-6 pt-6 lg:px-8 lg:pt-8">
        <Outlet />
      </div>
    </div>
  );
}
