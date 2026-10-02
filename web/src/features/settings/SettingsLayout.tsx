import { useQuery } from "@tanstack/react-query";
import { Link, Outlet } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { Alert, Spinner } from "@/components/ui";
import { settingsGroups } from "./sections";

/** Plex-style settings: grouped section nav on the left, the selected section on the right (ADM-8). */
export function SettingsLayout() {
  const me = useQuery(meQuery);
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
        {settingsGroups.map((g) => (
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
