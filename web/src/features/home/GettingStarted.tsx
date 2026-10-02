import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, Circle, X } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery, settingsQuery, usersQuery } from "@/api/queries";

const KEY = "marquee.gettingStarted.dismissed";

function dismissedBefore() {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

type Step = { done: boolean; title: string; detail: string; section: string; optional?: boolean };

/** First-run checklist for administrators (ADM-1), shown on Home until done or dismissed. */
export function GettingStarted() {
  const [dismissed, setDismissed] = useState(dismissedBefore);
  const libraries = useQuery(librariesQuery);
  const settings = useQuery(settingsQuery);
  const users = useQuery(usersQuery);
  const plex = useQuery({ queryKey: ["plex-import"], queryFn: () => unwrap(api.GET("/plex-import")), enabled: !dismissed });
  if (dismissed || !libraries.data || !settings.data || !users.data) return null;

  const steps: Step[] = [
    { done: libraries.data.length > 0, title: "Add your libraries", detail: "Point Marquee at your movie, TV, anime and music folders.", section: "libraries" },
    { done: !!settings.data.metadata?.tmdbApiKeySet, title: "Add a TMDB API key", detail: "Needed for posters, summaries, cast and ratings.", section: "metadata" },
    { done: !!plex.data?.lastReport, title: "Import from Plex", detail: "Bring over watch history, playlists and fixed matches.", section: "plex-import", optional: !plex.data?.available },
    { done: users.data.length > 1, title: "Add the people you share with", detail: "Accounts or managed profiles, each with their own watch history.", section: "users" },
    { done: !!settings.data.remoteAccess?.remoteUrl, title: "Set up remote access", detail: "Your Tailscale address, so apps can stream away from home.", section: "remote-access" },
  ];
  const remaining = steps.filter((s) => !s.done && !s.optional).length;
  if (remaining === 0) return null;
  const dismiss = () => {
    setDismissed(true);
    try {
      localStorage.setItem(KEY, "1");
    } catch {
      /* storage unavailable */
    }
  };
  return (
    <section className="mx-6 rounded-xl border border-border bg-surface p-5 lg:mx-8">
      <div className="mb-3 flex items-start gap-3">
        <div className="flex-1">
          <h2 className="text-lg font-semibold">Finish setting up Marquee</h2>
          <p className="text-sm text-muted">
            {remaining} {remaining === 1 ? "step" : "steps"} left. Everything can be changed later in Settings.
          </p>
        </div>
        <button onClick={dismiss} className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text" aria-label="Hide setup checklist">
          <X className="size-4" />
        </button>
      </div>
      <ol className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
        {steps.map((s) => (
          <li key={s.section}>
            <Link to="/settings/$section" params={{ section: s.section }} className="flex gap-3 rounded-lg p-3 hover:bg-surface-2">
              {s.done ? <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-success" aria-label="Done" /> : <Circle className="mt-0.5 size-5 shrink-0 text-faint" aria-label="To do" />}
              <span>
                <span className={s.done ? "block font-medium text-muted line-through" : "block font-medium"}>
                  {s.title}
                  {s.optional && !s.done ? <span className="ml-2 text-xs font-normal text-faint">no Plex database found</span> : null}
                </span>
                <span className="block text-sm text-muted">{s.detail}</span>
              </span>
            </Link>
          </li>
        ))}
      </ol>
    </section>
  );
}
