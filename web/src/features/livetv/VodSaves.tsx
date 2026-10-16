import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  ArrowUpToLine,
  Check,
  Download,
  HardDriveDownload,
  Loader2,
  Pause,
  Play,
  RotateCcw,
  ShieldCheck,
  X,
} from "lucide-react";
import { meQuery } from "@/api/queries";
import { Alert, Badge, Button, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";
import {
  saveActive,
  useSaveAction,
  useSaveVod,
  vodSavesQuery,
  type VodSave,
  type VodSaveState,
} from "./vodApi";

// Saving films and episodes on demand into the library (admins only; invisible to others).

export function useIsAdmin() {
  return !!useQuery(meQuery).data?.isAdmin;
}

/** "Saving 42%", "Queued", "Saved"… for a film's or episode's latest save. */
export function saveLabel(s?: VodSaveState | null): string {
  switch (s?.state) {
    case "queued":
      return "Queued";
    case "downloading":
      return `Saving ${Math.floor((s.progress ?? 0) * 100)}%`;
    case "processing":
      return `Converting ${Math.floor((s.progress ?? 0) * 100)}%`;
    case "verifying":
      return "Verifying";
    case "paused":
      return "Paused";
    case "done":
      return "Saved";
    case "skipped":
      return "In library";
    case "failed":
      return "Save failed";
    default:
      return "Save to library";
  }
}

function toastResult(r: { queued: number; already: number; skipped: number }) {
  const parts = [
    r.queued && `${r.queued} queued`,
    r.already && `${r.already} already queued or saved`,
    r.skipped && `${r.skipped} already in the library`,
  ].filter(Boolean);
  toast(parts.length ? parts.join(", ") : "Nothing to save", { link: { to: "/vod/saves", label: "Saves" } });
}

/** A film page's "Save to library" button with its state. */
export function SaveMovieButton({ movieId, save }: { movieId: number; save?: VodSaveState | null }) {
  const m = useSaveVod();
  const state = save?.state;
  const busy = saveActive(state) || state === "paused";
  const done = state === "done" || state === "skipped";
  return (
    <Button
      variant="ghost"
      onClick={() => m.mutate({ movieId }, { onSuccess: toastResult, onError: (e) => toast(e.message, { tone: "error" }) })}
      disabled={busy || done || m.isPending}
      title={save?.error || (done ? "Already in the library" : "Save a copy into the library's movies folder")}
    >
      {done ? (
        <Check className="size-4 text-success" />
      ) : busy ? (
        <Loader2 className="size-4 animate-spin" />
      ) : state === "failed" ? (
        <AlertTriangle className="size-4 text-danger" />
      ) : (
        <HardDriveDownload className="size-4" />
      )}
      {state === "failed" ? "Save failed — try again" : saveLabel(save)}
    </Button>
  );
}

/** Save buttons for a show or one season. */
export function SaveSeriesButton({ seriesId, season, label }: { seriesId: number; season?: number; label: string }) {
  const m = useSaveVod();
  return (
    <Button
      size="sm"
      variant="ghost"
      onClick={() =>
        m.mutate({ seriesId, season }, { onSuccess: toastResult, onError: (e) => toast(e.message, { tone: "error" }) })
      }
      loading={m.isPending}
      title="Save copies of the episodes into the library's shows folder"
    >
      {!m.isPending && <HardDriveDownload className="size-4" />} {label}
    </Button>
  );
}

/** An episode row's save icon, showing its state. */
export function SaveEpisodeIcon({ episodeId, save }: { episodeId: number; save?: VodSaveState | null }) {
  const m = useSaveVod();
  const state = save?.state;
  const label = saveLabel(save);
  const can = !state || state === "failed" || state === "cancelled";
  return (
    <button
      onClick={() => can && m.mutate({ episodeId }, { onError: (e) => toast(e.message, { tone: "error" }) })}
      disabled={!can || m.isPending}
      className="inline-flex items-center gap-1 rounded-full p-1.5 text-xs hover:bg-surface-2 disabled:cursor-default disabled:hover:bg-transparent"
      aria-label={can ? "Save episode to library" : label}
      title={save?.error || (can ? "Save to library" : label)}
    >
      {state === "done" || state === "skipped" ? (
        <HardDriveDownload className="size-4 text-success" />
      ) : saveActive(state) || m.isPending ? (
        <>
          <Loader2 className="size-4 animate-spin text-accent" />
          {state === "downloading" && <span className="text-muted">{Math.floor((save?.progress ?? 0) * 100)}%</span>}
        </>
      ) : state === "paused" ? (
        <Pause className="size-4 text-muted" />
      ) : state === "failed" ? (
        <AlertTriangle className="size-4 text-danger" />
      ) : (
        <Download className="size-4 text-faint" />
      )}
    </button>
  );
}

/** The header link to the save queue (admins only). */
export function SavesLink() {
  const admin = useIsAdmin();
  if (!admin) return null;
  return (
    <Link to="/vod/saves" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text">
      <HardDriveDownload className="size-4" /> Saves
    </Link>
  );
}

function bytes(n?: number) {
  if (!n) return "0 MB";
  if (n >= 1 << 30) return `${(n / (1 << 30)).toFixed(1)} GB`;
  return `${Math.round(n / (1 << 20))} MB`;
}

function eta(s?: number) {
  if (!s) return "";
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h} h ${m} min left` : m ? `${m} min left` : `${s} s left`;
}

const stateText: Record<VodSave["state"], string> = {
  queued: "Queued",
  downloading: "Downloading",
  processing: "Converting",
  verifying: "Verifying",
  paused: "Paused",
  done: "Saved",
  failed: "Failed",
  cancelled: "Cancelled",
  skipped: "Skipped",
};

function SaveRow({ v }: { v: VodSave }) {
  const act = useSaveAction();
  const run = (action: "cancel" | "retry" | "top" | "pause" | "resume") =>
    act.mutate({ id: v.id, action }, { onError: (e) => toast(e.message, { tone: "error" }) });
  const pct =
    v.state === "processing" || v.state === "verifying"
      ? (v.progress ?? 0) * 100
      : v.bytesTotal
        ? (v.bytesDone / v.bytesTotal) * 100
        : 0;
  const running = v.state === "downloading" || v.state === "processing" || v.state === "verifying";
  const details = [
    v.sourceName,
    v.bytesTotal ? `${bytes(v.bytesDone)} of ${bytes(v.bytesTotal)}` : v.bytesDone ? bytes(v.bytesDone) : "",
    v.state === "downloading" && v.speedBps ? `${bytes(v.speedBps)}/s` : "",
    v.state === "downloading" ? eta(v.etaSeconds) : "",
    v.quality !== "original" ? v.quality : "",
  ].filter(Boolean);
  return (
    <li className="space-y-2 py-3" data-testid="vod-save">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{v.title}</span>
            <Badge tone={v.state === "failed" ? "danger" : running || v.state === "done" ? "accent" : "muted"}>
              {stateText[v.state]}
            </Badge>
            {v.verified && (
              <span className="inline-flex items-center gap-1 text-xs text-success" title={v.verifyNote}>
                <ShieldCheck className="size-3.5" /> Verified
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate font-mono text-xs text-faint" title={v.targetPath}>
            {v.targetPath}
          </div>
          {details.length > 0 && <div className="mt-0.5 text-xs text-muted">{details.join(" · ")}</div>}
          {v.state === "queued" && v.pauseReason && <div className="mt-0.5 text-xs text-muted">{v.pauseReason}</div>}
          {v.error && v.state !== "skipped" && (
            <div className={v.state === "failed" ? "mt-0.5 text-xs text-danger" : "mt-0.5 text-xs text-muted"}>{v.error}</div>
          )}
          {v.state === "skipped" && v.error && <div className="mt-0.5 text-xs text-muted">{v.error}</div>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {v.state === "queued" && (
            <Button size="sm" variant="ghost" onClick={() => run("top")} title="Move to top" aria-label="Move to top">
              <ArrowUpToLine className="size-4" />
            </Button>
          )}
          {(v.state === "queued" || running) && (
            <Button size="sm" variant="ghost" onClick={() => run("pause")} title="Pause" aria-label="Pause">
              <Pause className="size-4" />
            </Button>
          )}
          {v.state === "paused" && (
            <Button size="sm" variant="ghost" onClick={() => run("resume")} title="Resume" aria-label="Resume">
              <Play className="size-4" />
            </Button>
          )}
          {(v.state === "failed" || v.state === "cancelled") && (
            <Button size="sm" variant="ghost" onClick={() => run("retry")} title="Retry" aria-label="Retry">
              <RotateCcw className="size-4" />
            </Button>
          )}
          {(v.state === "queued" || v.state === "paused" || v.state === "failed" || running) && (
            <Button size="sm" variant="ghost" onClick={() => run("cancel")} title="Cancel" aria-label="Cancel">
              <X className="size-4" />
            </Button>
          )}
        </div>
      </div>
      {(running || ((v.state === "queued" || v.state === "paused" || v.state === "failed") && pct > 0)) && (
        <div className="h-1.5 overflow-hidden rounded-full bg-surface-3" role="progressbar" aria-valuenow={Math.round(pct)}>
          <div className="h-full bg-accent transition-[width]" style={{ width: `${Math.min(100, pct)}%` }} />
        </div>
      )}
    </li>
  );
}

/** The save queue: progress, speed, ETA, errors and the controls (admins only). */
export function VodSavesPage() {
  const admin = useIsAdmin();
  const q = useQuery({ ...vodSavesQuery, enabled: admin });
  const act = useSaveAction();
  if (!admin)
    return (
      <div className="p-8">
        <Alert tone="error">Only administrators can save to the library.</Alert>
      </div>
    );
  if (q.isPending) return <Spinner />;
  if (q.isError)
    return (
      <div className="p-8">
        <Alert tone="error">{q.error.message}</Alert>
      </div>
    );
  const d = q.data;
  const finished = d.saves.filter((v) => ["done", "failed", "cancelled", "skipped"].includes(v.state));
  const waiting = d.saves.filter((v) => !finished.includes(v));
  return (
    <div className="space-y-6 p-4 lg:p-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-bold">Saves</h1>
        <div className="flex gap-3 text-sm text-muted">
          <Link to="/vod" className="hover:text-text">
            VOD Movies
          </Link>
          <Link to="/vod/shows" className="hover:text-text">
            VOD Shows
          </Link>
          <Link to="/settings/$section" params={{ section: "movies-on-demand" }} className="hover:text-text">
            Settings
          </Link>
        </div>
        <div className="ml-auto flex gap-2">
          <Button size="sm" onClick={() => act.mutate({ action: d.paused ? "resume" : "pause" })}>
            {d.paused ? <Play className="size-4" /> : <Pause className="size-4" />} {d.paused ? "Resume queue" : "Pause queue"}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => act.mutate({ action: "clear" })} disabled={finished.length === 0}>
            Clear finished
          </Button>
        </div>
      </div>
      {!d.moviesFolderSet && !d.showsFolderSet && (
        <Alert>
          Choose where saves go in{" "}
          <Link to="/settings/$section" params={{ section: "movies-on-demand" }} className="underline">
            Settings → Movies on demand → Saving to library
          </Link>
          .
        </Alert>
      )}
      {d.paused && <Alert>The queue is paused. Saves keep what they downloaded and continue when it resumes.</Alert>}
      <section>
        <h2 className="mb-1 text-lg font-semibold">Queue</h2>
        {waiting.length === 0 ? (
          <p className="text-sm text-muted">
            Nothing waiting. Save films and episodes from their pages on VOD Movies and VOD Shows.
          </p>
        ) : (
          <ul className="divide-y divide-border">
            {waiting.map((v) => (
              <SaveRow key={v.id} v={v} />
            ))}
          </ul>
        )}
      </section>
      {finished.length > 0 && (
        <section>
          <h2 className="mb-1 text-lg font-semibold">Finished</h2>
          <ul className="divide-y divide-border">
            {finished.map((v) => (
              <SaveRow key={v.id} v={v} />
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
