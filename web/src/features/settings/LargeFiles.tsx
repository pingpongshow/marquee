import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { ArrowLeft, Ban, Pause, Pencil, Play, Plus, RotateCcw, Trash2, Undo2 } from "lucide-react";
import { useMemo, useState } from "react";
import { api, unwrap } from "@/api/client";
import { librariesQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Badge, Button, Dialog, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { toast } from "@/components/Toast";

type LargeFile = components["schemas"]["LargeFile"];
type Job = components["schemas"]["OptimizeJob"];
type JobState = components["schemas"]["OptimizeJobState"];
type Preset = components["schemas"]["OptimizePreset"];
type OptimizeSettings = components["schemas"]["OptimizeSettings"];
type OptimizeConfig = components["schemas"]["OptimizeConfig"];

const PAGE = 50;

/** Sizes in decimal units (as drives are sold), one decimal place. */
export function size(n: number) {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (Math.abs(v) >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  return i === 0 ? `${v} B` : `${v.toFixed(1)} ${units[i]}`;
}

function resolutionLabel(w: number, h: number) {
  if (w >= 3200 || h >= 2000) return "4K";
  if (w >= 1700 || h >= 1000) return "1080p";
  if (w >= 1200 || h >= 700) return "720p";
  return "SD";
}

const codecLabels: Record<string, string> = { h264: "H.264", hevc: "HEVC", mpeg2video: "MPEG-2", vc1: "VC-1", mpeg4: "MPEG-4", av1: "AV1", vp9: "VP9" };

function duration(ms: number) {
  const m = Math.round(ms / 60000);
  return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`;
}

function eta(s: number) {
  if (s >= 3600) return `${Math.floor(s / 3600)} h ${Math.round((s % 3600) / 60)} min left`;
  if (s >= 60) return `${Math.round(s / 60)} min left`;
  return `${s} s left`;
}

const stateLabel: Record<JobState, string> = {
  queued: "Queued",
  running: "Optimizing",
  verifying: "Checking",
  done: "Optimized",
  failed: "Failed",
  cancelled: "Cancelled",
  skipped: "Skipped",
  restored: "Restored",
};

function StateBadge({ state }: { state: JobState }) {
  const tone = state === "failed" ? "danger" : state === "running" || state === "verifying" || state === "done" ? "accent" : "muted";
  return <Badge tone={tone}>{stateLabel[state]}</Badge>;
}

const configQuery = { queryKey: ["optimize", "settings"], queryFn: () => unwrap(api.GET("/optimize/settings")) };

const summaryQuery = {
  queryKey: ["optimize", "jobs", "summary"],
  queryFn: () => unwrap(api.GET("/optimize/jobs", { params: { query: { view: "history", limit: 1 } } })),
};

/** The Large files entry on the Library Health overview, with how much space it has saved. */
export function LargeFilesEntry({ onOpen }: { onOpen: () => void }) {
  const summary = useQuery(summaryQuery);
  const s = summary.data;
  return (
    <button
      onClick={onOpen}
      className="flex w-full items-center gap-4 rounded-lg border border-l-4 border-border border-l-sky-400 bg-surface px-4 py-3 text-left hover:bg-surface-2"
      data-testid="large-files-entry"
    >
      <span className="min-w-0 flex-1">
        <span className="block font-medium">Large files</span>
        <span className="block text-sm text-muted">Movies and episodes bigger than they need to be. Queue them to be re-encoded smaller, on the GPU where possible.</span>
      </span>
      {s && s.optimized > 0 && (
        <span className="text-right text-sm">
          <span className="block font-semibold text-success">{size(s.savedBytes)} saved</span>
          <span className="block text-xs text-muted">{s.optimized.toLocaleString()} optimized</span>
        </span>
      )}
    </button>
  );
}

type Tab = "files" | "queue" | "history" | "settings";

/** Library Health → Large files: find oversized files, queue them, follow the queue. Admins only. */
export function LargeFilesTool({ onBack }: { onBack: () => void }) {
  const [tab, setTab] = useState<Tab>("files");
  const config = useQuery(configQuery);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="ghost" size="sm" onClick={onBack}>
          <ArrowLeft className="size-4" /> All checks
        </Button>
        <h2 className="text-lg font-semibold">Large files</h2>
      </div>
      <div role="tablist" className="flex gap-1 border-b border-border">
        {(
          [
            ["files", "Files"],
            ["queue", "Queue"],
            ["history", "History"],
            ["settings", "Settings"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={clsx("-mb-px border-b-2 px-3 py-2 text-sm", tab === id ? "border-accent text-text" : "border-transparent text-muted hover:text-text")}
          >
            {label}
          </button>
        ))}
      </div>
      {config.isPending && <Spinner />}
      {config.isError && <Alert tone="error">{config.error.message}</Alert>}
      {config.data && tab === "files" && <FilesTab config={config.data} onQueued={() => setTab("queue")} />}
      {config.data && tab === "queue" && <JobsTab config={config.data} history={false} />}
      {config.data && tab === "history" && <JobsTab config={config.data} history />}
      {config.data && tab === "settings" && <SettingsTab config={config.data} />}
    </div>
  );
}

// ---------- Files ----------

type Filters = {
  libraryId?: number;
  type?: "movie" | "episode";
  minSizeGb?: number;
  minBitrateMbps?: number;
  resolution?: 2160 | 1080 | 720 | 480;
  codecs: string[];
  hdr?: "yes" | "no";
  sort: "size" | "bitrate" | "title" | "savings";
};

function numberOrUndefined(v: string) {
  const n = Number(v);
  return v.trim() === "" || !Number.isFinite(n) || n <= 0 ? undefined : n;
}

function FilesTab({ config, onQueued }: { config: OptimizeConfig; onQueued: () => void }) {
  const qc = useQueryClient();
  const libraries = useQuery(librariesQuery);
  const [filters, setFilters] = useState<Filters>({ minSizeGb: 10, codecs: [], sort: "size" });
  const [presetId, setPresetId] = useState(config.settings.defaultPreset || config.presets[0]?.id || "");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [allMatching, setAllMatching] = useState(false);
  const videoLibraries = (libraries.data ?? []).filter((l) => ["movies", "shows", "anime", "videos"].includes(l.type));

  const query = {
    libraryId: filters.libraryId,
    type: filters.type,
    minSizeGb: filters.minSizeGb,
    minBitrateMbps: filters.minBitrateMbps,
    resolution: filters.resolution,
    codecs: filters.codecs.length ? filters.codecs.join(",") : undefined,
    hdr: filters.hdr,
    presetId,
    sort: filters.sort,
    offset,
    limit: PAGE,
  };
  const files = useQuery({
    queryKey: ["optimize", "files", query],
    queryFn: () => unwrap(api.GET("/optimize/files", { params: { query } })),
    placeholderData: (prev) => prev,
  });
  const update = (patch: Partial<Filters>) => {
    setFilters((f) => ({ ...f, ...patch }));
    setOffset(0);
    setSelected(new Set());
    setAllMatching(false);
  };
  const queue = useMutation({
    mutationFn: () => {
      const filter = {
        libraryId: filters.libraryId,
        type: filters.type,
        minSizeGb: filters.minSizeGb,
        minBitrateMbps: filters.minBitrateMbps,
        resolution: filters.resolution,
        codecs: filters.codecs.length ? filters.codecs : undefined,
        hdr: filters.hdr,
      };
      return unwrap(api.POST("/optimize/jobs", { body: allMatching ? { presetId, filter } : { presetId, fileIds: [...selected] } }));
    },
    onSuccess: (r) => {
      toast(`Queued ${r.queued.toLocaleString()} file${r.queued === 1 ? "" : "s"}${r.skipped ? ` (${r.skipped} already queued or not available)` : ""}.`);
      setSelected(new Set());
      setAllMatching(false);
      qc.invalidateQueries({ queryKey: ["optimize"] });
      if (r.queued > 0) onQueued();
    },
    onError: (e) => toast(e.message, { tone: "error" }),
  });

  const page = files.data;
  const total = page?.total ?? 0;
  const count = allMatching ? total : selected.size;
  const pageIds = page?.items.map((f) => f.fileId) ?? [];
  const pageAllSelected = pageIds.length > 0 && pageIds.every((id) => allMatching || selected.has(id));
  const toggle = (id: number) => {
    if (allMatching) {
      // Switch to an explicit selection of this page minus the row.
      setAllMatching(false);
      setSelected(new Set(pageIds.filter((x) => x !== id)));
      return;
    }
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  };
  const togglePage = () => {
    if (pageAllSelected) {
      setAllMatching(false);
      setSelected((s) => new Set([...s].filter((id) => !pageIds.includes(id))));
    } else setSelected((s) => new Set([...s, ...pageIds]));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Field label="Library">
          {(id) => (
            <Select id={id} value={filters.libraryId ?? ""} onChange={(e) => update({ libraryId: e.target.value ? Number(e.target.value) : undefined })}>
              <option value="">All video libraries</option>
              {videoLibraries.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="Type">
          {(id) => (
            <Select id={id} value={filters.type ?? ""} onChange={(e) => update({ type: (e.target.value || undefined) as Filters["type"] })}>
              <option value="">Movies and episodes</option>
              <option value="movie">Movies</option>
              <option value="episode">Episodes</option>
            </Select>
          )}
        </Field>
        <Field label="At least (GB)">
          {(id) => (
            <Input id={id} type="number" min={0} step="any" value={filters.minSizeGb ?? ""} placeholder="Any size" onChange={(e) => update({ minSizeGb: numberOrUndefined(e.target.value) })} />
          )}
        </Field>
        <Field label="Bitrate at least (Mb/s)">
          {(id) => (
            <Input
              id={id}
              type="number"
              min={0}
              step="any"
              value={filters.minBitrateMbps ?? ""}
              placeholder="Any bitrate"
              onChange={(e) => update({ minBitrateMbps: numberOrUndefined(e.target.value) })}
            />
          )}
        </Field>
        <Field label="Resolution">
          {(id) => (
            <Select id={id} value={filters.resolution ?? ""} onChange={(e) => update({ resolution: (e.target.value ? Number(e.target.value) : undefined) as Filters["resolution"] })}>
              <option value="">Any</option>
              <option value="2160">4K (2160p)</option>
              <option value="1080">1080p</option>
              <option value="720">720p</option>
              <option value="480">SD</option>
            </Select>
          )}
        </Field>
        <Field label="HDR">
          {(id) => (
            <Select id={id} value={filters.hdr ?? ""} onChange={(e) => update({ hdr: (e.target.value || undefined) as Filters["hdr"] })}>
              <option value="">HDR and SDR</option>
              <option value="yes">Only HDR</option>
              <option value="no">Only SDR</option>
            </Select>
          )}
        </Field>
        <Field label="Preset" help="For the estimates and for queueing.">
          {(id) => (
            <Select id={id} value={presetId} onChange={(e) => setPresetId(e.target.value)}>
              {config.presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="Sort">
          {(id) => (
            <Select id={id} value={filters.sort} onChange={(e) => update({ sort: e.target.value as Filters["sort"] })}>
              <option value="size">Biggest first</option>
              <option value="savings">Most to save first</option>
              <option value="bitrate">Highest bitrate first</option>
              <option value="title">Title</option>
            </Select>
          )}
        </Field>
      </div>
      <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Video codecs">
        <span className="text-sm text-muted">Codecs:</span>
        {Object.entries(codecLabels).map(([c, label]) => {
          const on = filters.codecs.includes(c);
          return (
            <button
              key={c}
              aria-pressed={on}
              onClick={() => update({ codecs: on ? filters.codecs.filter((x) => x !== c) : [...filters.codecs, c] })}
              className={clsx("rounded-full border px-3 py-1 text-xs", on ? "border-accent bg-accent/15 text-accent" : "border-border text-muted hover:text-text")}
            >
              {label}
            </button>
          );
        })}
        <span className="text-xs text-faint">Older codecs (MPEG-2, VC-1, H.264) shrink the most.</span>
      </div>

      {files.isError && <Alert tone="error">{files.error.message}</Alert>}
      {page && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-surface-2 px-4 py-3">
          <p className="text-sm" data-testid="large-files-totals">
            <span className="font-medium">{total.toLocaleString()} files</span>, {size(page.totalSize)}, about {size(page.estimatedSize)} after
            {page.totalSize > page.estimatedSize && <span className="text-success"> (saves about {size(page.totalSize - page.estimatedSize)})</span>}
          </p>
          <div className="flex flex-wrap items-center gap-2">
            {total > 0 && !allMatching && (
              <Button size="sm" variant="ghost" onClick={() => setAllMatching(true)}>
                Select all {total.toLocaleString()} matching
              </Button>
            )}
            {count > 0 && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setSelected(new Set());
                  setAllMatching(false);
                }}
              >
                Clear selection
              </Button>
            )}
            <Button size="sm" variant="primary" disabled={count === 0} loading={queue.isPending} onClick={() => queue.mutate()}>
              Queue {count.toLocaleString()} file{count === 1 ? "" : "s"}
            </Button>
          </div>
        </div>
      )}
      {files.isPending && <Spinner />}
      {page && page.items.length === 0 && <p className="text-muted">No files match these filters.</p>}
      {page && page.items.length > 0 && (
        <div className="overflow-x-auto rounded-lg border border-border bg-surface">
          <table className="w-full min-w-[760px] text-sm" aria-label="Large files">
            <thead className="border-b border-border text-left text-xs text-muted">
              <tr>
                <th className="w-10 px-3 py-2">
                  <input type="checkbox" aria-label="Select this page" checked={pageAllSelected} onChange={togglePage} />
                </th>
                <th className="px-3 py-2 font-medium">Title</th>
                <th className="px-3 py-2 text-right font-medium">Size</th>
                <th className="px-3 py-2 font-medium">Video</th>
                <th className="px-3 py-2 text-right font-medium">Bitrate</th>
                <th className="px-3 py-2 text-right font-medium">Length</th>
                <th className="px-3 py-2 text-right font-medium">After (about)</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {page.items.map((f) => (
                <FileRow key={f.fileId} file={f} checked={allMatching || selected.has(f.fileId)} onToggle={() => toggle(f.fileId)} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {total > PAGE && (
        <div className="flex items-center justify-between text-sm">
          <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>
            Previous
          </Button>
          <span className="text-muted">
            {offset + 1}–{Math.min(total, offset + PAGE)} of {total.toLocaleString()}
          </span>
          <Button size="sm" variant="ghost" disabled={offset + PAGE >= total} onClick={() => setOffset(offset + PAGE)}>
            Next
          </Button>
        </div>
      )}
    </div>
  );
}

function FileRow({ file: f, checked, onToggle }: { file: LargeFile; checked: boolean; onToggle: () => void }) {
  const saving = f.size > 0 ? Math.round(((f.size - f.estimatedSize) / f.size) * 100) : 0;
  return (
    <tr className={clsx(checked && "bg-accent/5")} data-testid={`large-file-${f.fileId}`}>
      <td className="px-3 py-2 align-top">
        <input type="checkbox" aria-label={`Select ${f.title}`} checked={checked} onChange={onToggle} />
      </td>
      <td className="max-w-[22rem] px-3 py-2 align-top">
        <Link to="/item/$itemId" params={{ itemId: String(f.itemId) }} className="block truncate font-medium hover:underline">
          {f.title}
        </Link>
        <div className="truncate text-xs text-faint" title={f.path}>
          {f.subtitle}
          {f.subtitle ? " · " : ""}
          {f.path.split("/").pop()}
        </div>
        {f.jobState && (
          <div className="mt-0.5">
            <StateBadge state={f.jobState} />
          </div>
        )}
      </td>
      <td className="px-3 py-2 text-right align-top tabular-nums">{size(f.size)}</td>
      <td className="px-3 py-2 align-top whitespace-nowrap">
        {resolutionLabel(f.width, f.height)} · {codecLabels[f.videoCodec] ?? f.videoCodec}
        {f.hdr && <span className="ml-1 text-xs text-accent">{f.hdr === "dolby_vision" ? "DV" : f.hdr.toUpperCase()}</span>}
      </td>
      <td className="px-3 py-2 text-right align-top tabular-nums whitespace-nowrap">{f.bitrateKbps ? `${(f.bitrateKbps / 1000).toFixed(1)} Mb/s` : "—"}</td>
      <td className="px-3 py-2 text-right align-top whitespace-nowrap">{duration(f.durationMs)}</td>
      <td className="px-3 py-2 text-right align-top tabular-nums whitespace-nowrap">
        {size(f.estimatedSize)}
        {saving > 0 && <div className="text-xs text-success">−{saving}%</div>}
      </td>
    </tr>
  );
}

// ---------- Queue and history ----------

function JobsTab({ config, history }: { config: OptimizeConfig; history: boolean }) {
  const qc = useQueryClient();
  const [offset, setOffset] = useState(0);
  const key = ["optimize", "jobs", history ? "history" : "queue", offset];
  const jobs = useQuery({
    queryKey: key,
    queryFn: () => unwrap(api.GET("/optimize/jobs", { params: { query: { view: history ? "history" : "queue", offset, limit: PAGE } } })),
    refetchInterval: (q) => (q.state.data?.items.some((j) => j.state === "running" || j.state === "verifying" || j.state === "queued") ? 2000 : false),
  });
  const refresh = () => qc.invalidateQueries({ queryKey: ["optimize"] });
  const action = useMutation({
    mutationFn: ({ job, act }: { job: Job; act: "cancel" | "retry" | "restore" }) =>
      unwrap(api.POST("/optimize/jobs/{jobId}/{action}", { params: { path: { jobId: job.id, action: act } } })),
    onSuccess: (_d, { job, act }) => {
      if (act === "restore") toast(`The original of “${job.title}” is back.`);
      refresh();
    },
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const clear = useMutation({
    mutationFn: () => unwrap(api.POST("/optimize/jobs/clear")),
    onSuccess: refresh,
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const pause = useMutation({
    mutationFn: (paused: boolean) => unwrap(api.PUT("/optimize/settings", { body: { ...config.settings, paused } })),
    onSuccess: (c) => {
      qc.setQueryData(configQuery.queryKey, c);
      refresh();
    },
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const page = jobs.data;
  const total = page?.total ?? 0;
  return (
    <div className="space-y-4">
      {page && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-surface-2 px-4 py-3">
          <p className="text-sm" data-testid="optimize-saved">
            {page.optimized > 0 ? (
              <>
                <span className="font-medium text-success">{size(page.savedBytes)} saved</span> by {page.optimized.toLocaleString()} optimized file{page.optimized === 1 ? "" : "s"}
              </>
            ) : (
              <span className="text-muted">Nothing optimized yet.</span>
            )}
            {!history && page.paused && <span className="ml-2 text-muted">· {page.paused}</span>}
          </p>
          {!history && (
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="secondary" loading={pause.isPending} onClick={() => pause.mutate(!config.settings.paused)}>
                {config.settings.paused ? <Play className="size-4" /> : <Pause className="size-4" />}
                {config.settings.paused ? "Resume queue" : "Pause queue"}
              </Button>
              <Button size="sm" variant="ghost" loading={clear.isPending} onClick={() => clear.mutate()}>
                Clear finished
              </Button>
            </div>
          )}
        </div>
      )}
      {jobs.isPending && <Spinner />}
      {jobs.isError && <Alert tone="error">{jobs.error.message}</Alert>}
      {page?.items.length === 0 && <p className="text-muted">{history ? "No optimized files yet." : "The queue is empty. Pick files in Files and queue them."}</p>}
      <ul className="divide-y divide-border rounded-lg border border-border bg-surface" aria-label={history ? "Optimized files" : "Optimizer queue"}>
        {page?.items.map((j) => (
          <JobRow key={j.id} job={j} busy={action.isPending && action.variables?.job.id === j.id} onAction={(act) => action.mutate({ job: j, act })} />
        ))}
      </ul>
      {total > PAGE && (
        <div className="flex items-center justify-between text-sm">
          <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>
            Previous
          </Button>
          <span className="text-muted">
            {offset + 1}–{Math.min(total, offset + PAGE)} of {total.toLocaleString()}
          </span>
          <Button size="sm" variant="ghost" disabled={offset + PAGE >= total} onClick={() => setOffset(offset + PAGE)}>
            Next
          </Button>
        </div>
      )}
    </div>
  );
}

function JobRow({ job: j, busy, onAction }: { job: Job; busy: boolean; onAction: (a: "cancel" | "retry" | "restore") => void }) {
  const active = j.state === "running" || j.state === "verifying";
  const saved = j.beforeSize - j.afterSize;
  return (
    <li className="space-y-1.5 p-3" data-testid={`optimize-job-${j.id}`}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            {j.itemId ? (
              <Link to="/item/$itemId" params={{ itemId: String(j.itemId) }} className="truncate font-medium hover:underline">
                {j.title}
              </Link>
            ) : (
              <span className="truncate font-medium">{j.title}</span>
            )}
            <StateBadge state={j.state} />
          </div>
          <div className="truncate text-xs text-faint" title={j.path}>
            {j.presetName} · {j.path.split("/").pop()}
          </div>
        </div>
        <div className="flex flex-wrap gap-1">
          {(j.state === "queued" || j.state === "running" || (j.state === "verifying" && !!j.paused)) && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => onAction("cancel")}>
              <Ban className="size-4" /> Cancel
            </Button>
          )}
          {(j.state === "failed" || j.state === "cancelled" || j.state === "skipped") && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => onAction("retry")}>
              <RotateCcw className="size-4" /> Retry
            </Button>
          )}
          {j.canRestore && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => onAction("restore")}>
              <Undo2 className="size-4" /> Restore original
            </Button>
          )}
        </div>
      </div>
      {active && (
        <>
          <div className="h-1.5 overflow-hidden rounded-full bg-surface-3">
            <div className={clsx("h-full bg-accent", j.state === "verifying" && "animate-pulse")} style={{ width: `${Math.round(j.progress * 100)}%` }} />
          </div>
          <div className="text-xs text-muted">
            {j.state === "verifying"
              ? j.paused || "Checking the new file…"
              : [
                  `${Math.round(j.progress * 100)}%`,
                  j.encoder && { nvenc: "NVIDIA GPU", qsv: "Quick Sync", software: "CPU" }[j.encoder],
                  j.fps ? `${Math.round(j.fps)} fps` : "",
                  j.speed ? `${j.speed.toFixed(1)}× real time` : "",
                  j.etaSeconds ? eta(j.etaSeconds) : "",
                  j.paused,
                ]
                  .filter(Boolean)
                  .join(" · ")}
          </div>
        </>
      )}
      {j.state === "queued" && j.paused && <div className="text-xs text-muted">{j.paused}</div>}
      <div className="text-xs text-muted">
        {size(j.beforeSize)}
        {j.afterSize > 0 && (
          <>
            {" → "}
            {size(j.afterSize)}
          </>
        )}
        {(j.state === "done" || j.state === "restored") && saved > 0 && (
          <span className={clsx("ml-1", j.state === "done" ? "text-success" : "text-faint line-through")}>Optimized: saved {size(saved)}</span>
        )}
        {j.state === "done" && !j.canRestore && (
          <span className="ml-1 text-faint">{j.originalDeleted ? "· Original deleted" : "· the original has left the media trash"}</span>
        )}
      </div>
      {j.error && <div className={clsx("text-xs", j.state === "failed" ? "text-danger" : "text-muted")}>{j.error}</div>}
    </li>
  );
}

// ---------- Settings and presets ----------

const presetSummary = (p: Preset) =>
  [
    p.maxHeight ? `${p.maxHeight === 2160 ? "4K" : `${p.maxHeight}p`}` : "Same resolution",
    { hevc: "HEVC", h264: "H.264", av1: "AV1" }[p.codec],
    p.videoKbps ? `${(p.videoKbps / 1000).toFixed(1)} Mb/s` : `${p.quality} quality`,
    { copy: "audio copied", default: "default audio track only", aac_stereo: "AAC stereo", aac_51: "AAC up to 5.1" }[p.audio],
    p.hdr === "tonemap" ? "HDR to SDR" : "HDR kept",
  ]
    .filter(Boolean)
    .join(" · ");

function SettingsTab({ config }: { config: OptimizeConfig }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<OptimizeSettings>(config.settings);
  const [editing, setEditing] = useState<Preset | null>(null);
  const dirty = useMemo(() => JSON.stringify(draft) !== JSON.stringify(config.settings), [draft, config.settings]);
  const save = useMutation({
    mutationFn: (s: OptimizeSettings) => unwrap(api.PUT("/optimize/settings", { body: s })),
    onSuccess: (c) => {
      qc.setQueryData(configQuery.queryKey, c);
      setDraft(c.settings);
      toast("Saved.");
    },
    onError: (e) => toast(e.message, { tone: "error" }),
  });
  const builtIn = config.presets.filter((p) => p.builtIn);
  const all = [...builtIn, ...draft.presets];
  const savePreset = (p: Preset) => {
    const exists = draft.presets.some((x) => x.id === p.id);
    setDraft({ ...draft, presets: exists ? draft.presets.map((x) => (x.id === p.id ? p : x)) : [...draft.presets, p] });
    setEditing(null);
  };
  return (
    <div className="space-y-6">
      <section className="space-y-4 rounded-lg border border-border bg-surface p-5">
        <h3 className="font-semibold">Queue</h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Files at once" help="Each one uses a GPU encoder session or CPU time.">
            {(id) => (
              <Select id={id} value={draft.concurrency} onChange={(e) => setDraft({ ...draft, concurrency: Number(e.target.value) })}>
                {[1, 2, 3, 4].map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Keep the new file when it's at least … smaller" help="Otherwise it's thrown away and the original kept.">
            {(id) => (
              <Select id={id} value={draft.minSavingsPercent} onChange={(e) => setDraft({ ...draft, minSavingsPercent: Number(e.target.value) })}>
                {[5, 10, 15, 20, 25, 30, 40, 50].map((n) => (
                  <option key={n} value={n}>
                    {n}%
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Default preset">
            {(id) => (
              <Select id={id} value={draft.defaultPreset} onChange={(e) => setDraft({ ...draft, defaultPreset: e.target.value })}>
                {all.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        <Toggle
          label="Pause while anyone is watching"
          help="Encoding stops where it is while anything plays and carries on about 15 seconds after. Playback always comes first on the GPU either way."
          checked={draft.pauseWhileWatching}
          onChange={(v) => setDraft({ ...draft, pauseWhileWatching: v })}
        />
        <Toggle
          label="Only during the maintenance window"
          help={
            <>
              Set the window in{" "}
              <Link to="/settings/$section" params={{ section: "tasks" }} className="underline">
                Tasks
              </Link>
              .
            </>
          }
          checked={draft.windowOnly}
          onChange={(v) => setDraft({ ...draft, windowOnly: v })}
        />
        <Toggle
          label="Keep originals in the media trash"
          help="On: each original goes to the media trash once its new file is in place, and can be restored for 30 days. Off frees the space at once; originals can't be restored. Either way the original is only touched after the new file has been checked."
          checked={!!draft.keepOriginals}
          onChange={(v) => setDraft({ ...draft, keepOriginals: v })}
        />
      </section>

      <section className="space-y-3 rounded-lg border border-border bg-surface p-5">
        <div className="flex items-center justify-between gap-3">
          <h3 className="font-semibold">Presets</h3>
          <Button size="sm" variant="secondary" onClick={() => setEditing(newPreset())}>
            <Plus className="size-4" /> New preset
          </Button>
        </div>
        <p className="text-sm text-muted">
          Files are encoded on the NVIDIA GPU when there is one, then Quick Sync, then the CPU.
        </p>
        <ul className="divide-y divide-border rounded-md border border-border" aria-label="Presets">
          {all.map((p) => (
            <li key={p.id} className="flex items-center gap-3 px-3 py-2">
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">
                  {p.name} {p.builtIn && <Badge>Built in</Badge>}
                </div>
                <div className="text-xs text-muted">{presetSummary(p)}</div>
              </div>
              {!p.builtIn && (
                <>
                  <Button size="sm" variant="ghost" aria-label={`Edit ${p.name}`} onClick={() => setEditing(p)}>
                    <Pencil className="size-4" />
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`Delete ${p.name}`}
                    onClick={() =>
                      setDraft({
                        ...draft,
                        presets: draft.presets.filter((x) => x.id !== p.id),
                        defaultPreset: draft.defaultPreset === p.id ? (builtIn[0]?.id ?? "") : draft.defaultPreset,
                      })
                    }
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </>
              )}
            </li>
          ))}
        </ul>
      </section>

      <div className="flex justify-end gap-2">
        <Button variant="ghost" disabled={!dirty} onClick={() => setDraft(config.settings)}>
          Discard
        </Button>
        <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={() => save.mutate(draft)}>
          Save
        </Button>
      </div>
      {editing && <PresetDialog preset={editing} av1={config.av1Available} onSave={savePreset} onClose={() => setEditing(null)} />}
    </div>
  );
}

function randomId() {
  const b = crypto.getRandomValues(new Uint8Array(6));
  return "custom-" + Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

function newPreset(): Preset {
  return { id: randomId(), name: "", maxHeight: 1080, codec: "hevc", quality: "balanced", videoKbps: 0, audio: "copy", hdr: "keep" };
}

function PresetDialog({ preset, av1, onSave, onClose }: { preset: Preset; av1: boolean; onSave: (p: Preset) => void; onClose: () => void }) {
  const [p, setP] = useState<Preset>(preset);
  const [useBitrate, setUseBitrate] = useState(!!preset.videoKbps);
  const set = (patch: Partial<Preset>) => setP((x) => ({ ...x, ...patch }));
  const valid = p.name.trim() !== "" && (!useBitrate || (p.videoKbps ?? 0) > 0);
  return (
    <Dialog
      open
      onClose={onClose}
      title={preset.name ? `Edit “${preset.name}”` : "New preset"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!valid} onClick={() => onSave({ ...p, name: p.name.trim(), videoKbps: useBitrate ? p.videoKbps : 0 })}>
            Done
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Name">{(id) => <Input id={id} value={p.name} onChange={(e) => set({ name: e.target.value })} placeholder="e.g. Kids' shows 720p" />}</Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Resolution" help="Never upscaled.">
            {(id) => (
              <Select id={id} value={p.maxHeight} onChange={(e) => set({ maxHeight: Number(e.target.value) as Preset["maxHeight"] })}>
                <option value={0}>Keep</option>
                <option value={2160}>4K (2160p)</option>
                <option value={1080}>1080p</option>
                <option value={720}>720p</option>
                <option value={480}>480p</option>
              </Select>
            )}
          </Field>
          <Field label="Codec">
            {(id) => (
              <Select id={id} value={p.codec} onChange={(e) => set({ codec: e.target.value as Preset["codec"] })}>
                <option value="hevc">HEVC (H.265)</option>
                <option value="h264">H.264 (plays everywhere, bigger)</option>
                {(av1 || p.codec === "av1") && <option value="av1">AV1 (smallest, slow without a recent GPU)</option>}
              </Select>
            )}
          </Field>
          <Field label="Video">
            {(id) => (
              <Select id={id} value={useBitrate ? "bitrate" : p.quality} onChange={(e) => (e.target.value === "bitrate" ? setUseBitrate(true) : (setUseBitrate(false), set({ quality: e.target.value as Preset["quality"] })))}>
                <option value="high">High quality</option>
                <option value="balanced">Balanced</option>
                <option value="small">Small</option>
                <option value="bitrate">Target bitrate…</option>
              </Select>
            )}
          </Field>
          {useBitrate && (
            <Field label="Video bitrate (Mb/s)">
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={0.3}
                  max={100}
                  step="0.1"
                  value={p.videoKbps ? p.videoKbps / 1000 : ""}
                  onChange={(e) => set({ videoKbps: Math.round(Number(e.target.value) * 1000) || 0 })}
                />
              )}
            </Field>
          )}
          <Field label="Audio">
            {(id) => (
              <Select id={id} value={p.audio} onChange={(e) => set({ audio: e.target.value as Preset["audio"] })}>
                <option value="copy">Copy every track</option>
                <option value="aac_stereo">AAC stereo</option>
                <option value="aac_51">AAC up to 5.1</option>
              </Select>
            )}
          </Field>
          <Field label="HDR" help="HDR is kept only in HEVC or AV1.">
            {(id) => (
              <Select id={id} value={p.hdr} onChange={(e) => set({ hdr: e.target.value as Preset["hdr"] })}>
                <option value="keep">Keep HDR (10-bit)</option>
                <option value="tonemap">Convert to SDR</option>
              </Select>
            )}
          </Field>
        </div>
      </div>
    </Dialog>
  );
}
