import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
  AlertTriangle,
  ArrowUpCircle,
  ArrowUpToLine,
  Check,
  ChevronDown,
  Download,
  HardDriveDownload,
  Library,
  Loader2,
  Pause,
  Play,
  RotateCcw,
  ShieldCheck,
  X,
} from "lucide-react";
import { meQuery } from "@/api/queries";
import { Menu, MenuItem } from "@/components/Menu";
import { Alert, Badge, Button, Spinner } from "@/components/ui";
import { toast } from "@/components/Toast";
import {
  saveActive,
  useSaveAction,
  useSaveVod,
  vodSavesQuery,
  type VodLibraryMatch,
  type VodLibraryRollup,
  type VodSave,
  type VodSaveDestinationRef,
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

const codecNames: Record<string, string> = { h264: "H.264", hevc: "HEVC", av1: "AV1", vp9: "VP9", mpeg2video: "MPEG-2" };

/** "1080p HEVC" for a button; the full summary (with HDR and bitrate) goes in tooltips. */
export function shortQuality(q?: VodLibraryMatch["quality"]) {
  if (!q) return "";
  const codec = q.codec ? (codecNames[q.codec.toLowerCase()] ?? q.codec.toUpperCase()) : "";
  return [q.resolution ? `${q.resolution}p` : "", codec].filter(Boolean).join(" ");
}

/** Both copies' quality and why, for a tooltip. */
export function libraryTitle(l?: VodLibraryMatch | null) {
  if (!l || l.verdict === "none") return "";
  return [
    `This copy: ${l.quality?.summary || "quality not known yet"}`,
    `Library: ${l.libraryQuality?.summary || "quality not known"}`,
    l.reason,
  ].join("\n");
}

/** What a film's or episode's save button offers, given how it compares with the library. */
export function libraryAction(l?: VodLibraryMatch | null): { can: boolean; upgrade: boolean; label: string } {
  switch (l?.verdict) {
    case "better": {
      const from = shortQuality(l.libraryQuality);
      return { can: true, upgrade: true, label: `Upgrade (${shortQuality(l.quality) || "better"}${from ? ` → replaces ${from}` : ""})` };
    }
    case "same":
      return { can: false, upgrade: false, label: l.pending ? "In library (checking quality…)" : "In library (same quality)" };
    case "worse":
      return { can: false, upgrade: false, label: "In library (better quality)" };
  }
  return { can: true, upgrade: false, label: "Save to library" };
}

function toastResult(r: { queued: number; already: number; skipped: number; upgrades?: number }) {
  const parts = [
    r.queued && `${r.queued} queued${r.upgrades ? ` (${r.upgrades} upgrade${r.upgrades === 1 ? "" : "s"})` : ""}`,
    r.already && `${r.already} already queued or saved`,
    r.skipped && `${r.skipped} already in the library`,
  ].filter(Boolean);
  toast(parts.length ? parts.join(", ") : "Nothing to save", { link: { to: "/vod/saves", label: "Saves" } });
}

/** Where a film's or episode's page can save to: the default folder and the extra save folders. */
export type SaveTargets = { saveDefault?: boolean; saveDestinations?: VodSaveDestinationRef[] };

/** A save option: the default folder (no id) or a save folder from Settings. */
type SaveOption = { id?: string; name?: string };

/** The options in order: the default folder first (when it's set), then the save folders. */
function saveOptions(t?: SaveTargets): SaveOption[] {
  return [...(t?.saveDefault === false ? [] : [{}]), ...(t?.saveDestinations ?? [])];
}

const upgradeNote = "Upgrade replaces the copy in its current folder";

/** "Save to Anime" for a save folder, the label as it is for the default one. */
function withDest(label: string, o?: SaveOption) {
  return o?.name ? `${label} to ${o.name}` : label;
}

/**
 * The menu beside a save button listing the other places to save to ("Save to Anime"),
 * showing the same disabled and upgrade states as the button.
 */
function SaveToMenu({
  options,
  verb,
  upgrade,
  disabled,
  onPick,
  floating,
}: {
  options: SaveOption[];
  verb: string;
  upgrade?: boolean;
  disabled?: boolean;
  onPick: (o: SaveOption) => void;
  floating?: boolean;
}) {
  if (options.length === 0) return null;
  return (
    <Menu label="Save to another folder" trigger={<ChevronDown className="size-4" />} align="left" floating={floating}>
      {options.map((o) => (
        <MenuItem
          key={o.id ?? ""}
          disabled={disabled}
          onClick={() => onPick(o)}
          icon={upgrade ? <ArrowUpCircle className="text-accent" /> : <HardDriveDownload />}
        >
          <span className="min-w-0">
            <span className="block">{o.name ? `${verb} to ${o.name}` : `${verb} to the default folder`}</span>
            {upgrade && <span className="block text-xs text-muted">{upgradeNote}</span>}
          </span>
        </MenuItem>
      ))}
    </Menu>
  );
}

/**
 * A film page's save button with its state: Save, Upgrade (replacing the library's copy),
 * or, when the library has it as well or better, disabled with the reason. With save folders
 * besides the default one, a menu beside it offers "Save to Anime" and so on.
 */
export function SaveMovieButton({
  movieId,
  save,
  library,
  targets,
}: {
  movieId: number;
  save?: VodSaveState | null;
  library?: VodLibraryMatch | null;
  targets?: SaveTargets;
}) {
  const admin = useIsAdmin();
  const m = useSaveVod();
  if (!admin) return null;
  const state = save?.state;
  const busy = saveActive(state) || state === "paused";
  const done = state === "done" || state === "skipped";
  const action = libraryAction(library);
  const [main, ...more] = saveOptions(targets);
  // A save on its way, or just finished, shows its state; otherwise what the library allows.
  const showSave = busy || state === "failed" || (done && action.can && !action.upgrade);
  const label = showSave
    ? state === "failed"
      ? "Save failed — try again"
      : saveLabel(save)
    : action.upgrade
      ? action.label
      : withDest(action.label, main);
  const disabled = busy || m.isPending || !action.can || (done && !action.upgrade);
  const run = (o?: SaveOption) =>
    m.mutate(
      { movieId, upgrade: action.upgrade, destination: o?.id },
      { onSuccess: toastResult, onError: (e) => toast(e.message, { tone: "error" }) },
    );
  return (
    <div className="inline-flex items-center">
      <Button
        variant="ghost"
        onClick={() => run(main)}
        disabled={disabled}
        title={
          save?.error ||
          [libraryTitle(library), action.upgrade ? upgradeNote : ""].filter(Boolean).join("\n") ||
          (done
            ? "Already in the library"
            : main?.name
              ? `Save a copy into the ${main.name} folder`
              : "Save a copy into the library's movies folder")
        }
      >
        {showSave && done ? (
          <Check className="size-4 text-success" />
        ) : busy || library?.pending ? (
          <Loader2 className="size-4 animate-spin" />
        ) : state === "failed" ? (
          <AlertTriangle className="size-4 text-danger" />
        ) : action.upgrade ? (
          <ArrowUpCircle className="size-4 text-accent" />
        ) : !action.can ? (
          <Library className="size-4" />
        ) : (
          <HardDriveDownload className="size-4" />
        )}
        {label}
      </Button>
      <SaveToMenu options={more} verb={action.upgrade ? "Upgrade" : "Save"} upgrade={action.upgrade} disabled={disabled} onPick={run} />
    </div>
  );
}

/**
 * Save buttons for a show or one season: "Save missing (N)" queues the episodes the library
 * lacks; "Upgrade (M)" also queues those better than the library's copies, replacing them.
 * With save folders besides the default one, a menu beside "Save missing" offers them.
 */
export function SaveSeriesButton({
  seriesId,
  season,
  label,
  library,
  targets,
}: {
  seriesId: number;
  season?: number;
  label: string;
  library?: VodLibraryRollup | null;
  targets?: SaveTargets;
}) {
  const admin = useIsAdmin();
  const m = useSaveVod();
  if (!admin) return null;
  const [main, ...more] = saveOptions(targets);
  const run = (includeUpgrades: boolean, o?: SaveOption) =>
    m.mutate(
      { seriesId, season, includeUpgrades, destination: o?.id },
      { onSuccess: toastResult, onError: (e) => toast(e.message, { tone: "error" }) },
    );
  const what = season == null ? "show" : "season";
  const into = main?.name ? `the ${main.name} folder` : "the library's shows folder";
  if (!library)
    return (
      <div className="inline-flex items-center">
        <Button
          size="sm"
          variant="ghost"
          onClick={() => run(false, main)}
          loading={m.isPending}
          title={`Save copies of the episodes into ${into}`}
        >
          {!m.isPending && <HardDriveDownload className="size-4" />} {withDest(label, main)}
        </Button>
        <SaveToMenu options={more} verb={label} disabled={m.isPending} onPick={(o) => run(false, o)} />
      </div>
    );
  const have = library.same + library.worse;
  const checking = library.pending ? ` ${library.pending} still being checked.` : "";
  const missing = library.none ? `Save missing (${library.none})` : "Nothing missing";
  return (
    <>
      <div className="inline-flex items-center">
        <Button
          size="sm"
          variant="ghost"
          onClick={() => run(false, main)}
          loading={m.isPending}
          disabled={library.none === 0}
          title={
            library.none
              ? `Save the ${library.none} episode${library.none === 1 ? "" : "s"} of this ${what} the library doesn't have into ${into}.${have ? ` ${have} already in the library as well or better.` : ""}${checking}`
              : `The library has every episode of this ${what}.${checking}`
          }
        >
          {!m.isPending && <HardDriveDownload className="size-4" />} {library.none ? withDest(missing, main) : missing}
        </Button>
        <SaveToMenu
          options={more}
          verb={`Save missing (${library.none})`}
          disabled={m.isPending || library.none === 0}
          onPick={(o) => run(false, o)}
        />
      </div>
      {library.better > 0 && (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => run(true, main)}
          disabled={m.isPending}
          title={`Replace the library's copies of ${library.better} episode${library.better === 1 ? "" : "s"} with better ones${library.none ? `, and save the ${library.none} missing into ${into}` : ""}. ${upgradeNote}; the old files go to the media trash.`}
        >
          <ArrowUpCircle className="size-4 text-accent" /> Upgrade ({library.better})
        </Button>
      )}
    </>
  );
}

/**
 * An episode row's save icon, showing its state and how it compares with the library. With
 * save folders besides the default one, it opens a menu of where to save to.
 */
export function SaveEpisodeIcon({
  episodeId,
  save,
  library,
  targets,
}: {
  episodeId: number;
  save?: VodSaveState | null;
  library?: VodLibraryMatch | null;
  targets?: SaveTargets;
}) {
  const admin = useIsAdmin();
  const m = useSaveVod();
  if (!admin) return null;
  const state = save?.state;
  const action = libraryAction(library);
  const idle = !state || state === "failed" || state === "cancelled" || ((state === "done" || state === "skipped") && action.upgrade);
  const can = idle && action.can;
  const label = idle ? action.label : saveLabel(save);
  const options = saveOptions(targets);
  const run = (o?: SaveOption) =>
    m.mutate({ episodeId, upgrade: action.upgrade, destination: o?.id }, { onError: (e) => toast(e.message, { tone: "error" }) });
  const icon =
    idle && action.upgrade && !m.isPending ? (
      <ArrowUpCircle className="size-4 text-accent" />
    ) : idle && !action.can ? (
      library?.pending ? <Loader2 className="size-4 animate-spin text-faint" /> : <Library className="size-4 text-success" />
    ) : state === "done" || state === "skipped" ? (
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
    );
  // Where to save to, when there's a choice (an upgrade has none: it replaces the copy where it is).
  if (can && !m.isPending && !action.upgrade && options.length > 1)
    return (
      <Menu label={label} trigger={<span className="inline-flex items-center gap-1 text-xs">{icon}</span>} floating>
        {options.map((o) => (
          <MenuItem key={o.id ?? ""} onClick={() => run(o)} icon={<HardDriveDownload />}>
            {withDest("Save", o)}
          </MenuItem>
        ))}
      </Menu>
    );
  return (
    <button
      onClick={() => can && run(options[0])}
      disabled={!can || m.isPending}
      className="inline-flex items-center gap-1 rounded-full p-1.5 text-xs hover:bg-surface-2 disabled:cursor-default disabled:hover:bg-transparent"
      aria-label={idle && !action.upgrade ? withDest(label, options[0]) : label}
      title={[save?.error, idle && !action.upgrade ? withDest(label, options[0]) : label, action.upgrade && idle ? upgradeNote : "", libraryTitle(library)]
        .filter(Boolean)
        .join("\n")}
    >
      {icon}
    </button>
  );
}

/** The header link to the save queue (admins only). */
export function SavesLink() {
  const admin = useIsAdmin();
  if (!admin) return null;
  return (
    <Link to="/vod/saves" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text">
      <HardDriveDownload className="size-4" /> Downloads
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
            {v.destinationName && (
              <span className="text-sm text-muted" title={v.destinationFolder}>
                → {v.destinationName}
              </span>
            )}
            <Badge tone={v.state === "failed" ? "danger" : running || v.state === "done" ? "accent" : "muted"}>
              {v.state === "done" && v.replacedPath ? "Upgraded" : stateText[v.state]}
            </Badge>
            {v.upgrade && v.state !== "done" && (
              <span
                className="inline-flex items-center gap-1 text-xs text-accent"
                title="Replaces the library's copy, which goes to the media trash once this one is verified"
              >
                <ArrowUpCircle className="size-3.5" /> Upgrade
              </span>
            )}
            {v.verified && (
              <span className="inline-flex items-center gap-1 text-xs text-success" title={v.verifyNote}>
                <ShieldCheck className="size-3.5" /> Verified
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate font-mono text-xs text-faint" title={v.targetPath}>
            {v.targetPath}
          </div>
          {v.replacedPath && (
            <div className="mt-0.5 truncate text-xs text-muted" title={v.replacedPath}>
              Replaced {v.replacedPath} (moved to the media trash)
            </div>
          )}
          {details.length > 0 && <div className="mt-0.5 text-xs text-muted">{details.join(" · ")}</div>}
          {v.noLibrary && (
            <div className="mt-0.5 flex items-center gap-1 text-xs text-accent">
              <AlertTriangle className="size-3.5 shrink-0" /> No library includes this folder, so saved titles won&apos;t appear in
              Marquee until you add it to a library.
            </div>
          )}
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
        <h1 className="text-2xl font-bold">VOD Downloads</h1>
        <div className="flex gap-3 text-sm text-muted">
          <Link to="/vod" className="hover:text-text">
            VOD Movies
          </Link>
          <Link to="/vod/shows" className="hover:text-text">
            VOD Shows
          </Link>
          <Link to="/settings/$section" params={{ section: "vod" }} className="hover:text-text">
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
          <Link to="/settings/$section" params={{ section: "vod" }} className="underline">
            Settings → VOD → Saving to library
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
