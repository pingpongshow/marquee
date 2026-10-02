import { useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Activity, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap } from "@/api/client";
import { useCancelScan } from "@/api/queries";
import type { components } from "@/api/schema.gen";

type Task = components["schemas"]["ActivityTask"];

function taskDetail(t: Task) {
  const p = t.progress;
  if (t.state === "queued" || !p) return "Waiting…";
  if (p.phase === "walking") return "Finding files…";
  if (p.phase === "cleanup") return "Finishing up…";
  return `${p.done.toLocaleString()} of ${p.total.toLocaleString()}`;
}

function pct(t: Task) {
  const p = t.progress;
  return p && p.total > 0 && (p.phase === "probing" || p.phase === "metadata") ? Math.round((p.done / p.total) * 100) : null;
}

/**
 * Header activity indicator (like Plex's): pulses while the server is busy and opens a panel
 * listing scans and metadata work. Streams join this list once playback lands (M3).
 */
export function ActivityIndicator() {
  const qc = useQueryClient();
  const cancel = useCancelScan();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const activity = useQuery({
    queryKey: ["activity"],
    queryFn: () => unwrap(api.GET("/activity")),
    refetchInterval: (q) => (q.state.data?.tasks.length ? 2000 : 10000),
    refetchIntervalInBackground: false,
  });
  const tasks = activity.data?.tasks ?? [];
  const busy = tasks.length > 0;

  // When work finishes, refresh library counts and lists.
  const prevBusy = useRef(false);
  useEffect(() => {
    if (prevBusy.current && !busy) {
      qc.invalidateQueries({ queryKey: ["libraries"] });
      qc.invalidateQueries({ queryKey: ["items"] });
    }
    prevBusy.current = busy;
  }, [busy, qc]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((v) => !v)}
        className={clsx("relative rounded p-1.5 hover:bg-surface-2", busy ? "text-accent" : "text-muted hover:text-text")}
        aria-label={busy ? `Activity: ${tasks.length} running` : "Activity"}
        aria-expanded={open}
        title="Activity"
      >
        <Activity className={clsx("size-5", busy && "animate-pulse")} />
        {busy && (
          <span className="absolute -top-0.5 -right-0.5 flex size-4 items-center justify-center rounded-full bg-accent text-[10px] font-bold text-black">
            {tasks.length}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-80 overflow-hidden rounded-lg border border-border bg-surface shadow-2xl">
          <div className="border-b border-border px-4 py-2.5 text-sm font-semibold">Activity</div>
          {!busy && <p className="px-4 py-6 text-center text-sm text-muted">Nothing is running.</p>}
          <ul className="max-h-96 divide-y divide-border overflow-y-auto">
            {tasks.map((t) => {
              const percent = pct(t);
              return (
                <li key={t.id} className="px-4 py-3">
                  <div className="flex items-start gap-2">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">{t.title}</div>
                      <div className="text-xs text-muted">{taskDetail(t)}</div>
                    </div>
                    {t.libraryId && (
                      <button
                        className="rounded p-1 text-muted hover:bg-surface-2 hover:text-text"
                        aria-label={`Stop: ${t.title}`}
                        title="Stop"
                        onClick={() => t.libraryId && cancel.mutate(t.libraryId)}
                      >
                        <Square className="size-3.5" />
                      </button>
                    )}
                  </div>
                  {t.state === "running" && (
                    <div className="mt-2 h-1 overflow-hidden rounded-full bg-surface-3">
                      <div
                        className={clsx("h-full bg-accent", percent === null && "w-1/3 animate-pulse")}
                        style={percent === null ? undefined : { width: `${percent}%` }}
                      />
                    </div>
                  )}
                  {t.progress?.current && <div className="mt-1 truncate text-[11px] text-faint">{t.progress.current}</div>}
                </li>
              );
            })}
          </ul>
          <div className="border-t border-border px-4 py-2 text-[11px] text-faint">Streams will appear here once playback is available.</div>
        </div>
      )}
    </div>
  );
}
