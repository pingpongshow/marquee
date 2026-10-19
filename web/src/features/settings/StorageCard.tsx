import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Card, Select, Spinner } from "@/components/ui";

// The dashboard's storage overview: the free space on the volumes Marquee uses, each
// library's size and how it grew (one sample a day), and the biggest titles.

type Report = components["schemas"]["StorageReport"];
type StorageLibrary = components["schemas"]["StorageLibrary"];
type StorageTitle = components["schemas"]["StorageTitle"];
type StorageVolume = components["schemas"]["StorageVolume"];

/** Decimal units, like disk makers and the rest of the dashboard. */
export function formatBytes(bytes: number) {
  const abs = Math.abs(bytes);
  if (abs >= 1e12) return `${(bytes / 1e12).toFixed(2)} TB`;
  if (abs >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (abs >= 1e6) return `${Math.round(bytes / 1e6)} MB`;
  if (abs >= 1e3) return `${Math.round(bytes / 1e3)} KB`;
  return `${bytes} B`;
}

/** A movie or episode file at least this big is worth a look in Large files. */
const OPTIMIZE_BYTES = 20e9;

function VolumeBar({ v }: { v: StorageVolume }) {
  const used = Math.max(0, v.totalBytes - v.freeBytes);
  const usedPct = v.totalBytes ? (used / v.totalBytes) * 100 : 0;
  const freePct = 100 - usedPct;
  const low = freePct < 10;
  return (
    <li data-testid="storage-volume">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 text-sm">
        <span className="min-w-0 truncate font-medium" title={v.path}>
          {v.label ?? v.path}
        </span>
        <span className={clsx("tabular-nums", low ? "font-medium text-danger" : "text-muted")}>
          {formatBytes(v.freeBytes)} free of {formatBytes(v.totalBytes)} ({Math.round(freePct)}%)
        </span>
      </div>
      <div
        className="mt-1.5 h-2.5 overflow-hidden rounded-full bg-surface-3"
        role="meter"
        aria-label={`${v.label ?? v.path}: ${Math.round(usedPct)}% used`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(usedPct)}
      >
        <div className={clsx("h-full rounded-full", low ? "bg-danger" : "bg-accent")} style={{ width: `${usedPct}%` }} />
      </div>
      <div className="mt-0.5 truncate text-[11px] text-faint">{v.path}</div>
    </li>
  );
}

// Series colours, in library order.
const palette = [
  { fill: "fill-accent/60", dot: "bg-accent" },
  { fill: "fill-sky-400/60", dot: "bg-sky-400" },
  { fill: "fill-emerald-400/60", dot: "bg-emerald-400" },
  { fill: "fill-amber-400/60", dot: "bg-amber-400" },
  { fill: "fill-violet-400/60", dot: "bg-violet-400" },
  { fill: "fill-rose-400/60", dot: "bg-rose-400" },
  { fill: "fill-teal-400/60", dot: "bg-teal-400" },
  { fill: "fill-lime-400/60", dot: "bg-lime-400" },
];

const color = (i: number) => palette[i % palette.length]!;

/** A round axis maximum (1, 2, 2.5, 5 × 10ⁿ bytes) at or above the value. */
function niceMax(v: number) {
  const m = Math.max(v, 1);
  const p = 10 ** Math.floor(Math.log10(m));
  return ([1, 2, 2.5, 5, 10].find((f) => f * p >= m) ?? 10) * p;
}

const W = 600;
const H = 100;

/** Each library's size over the period, stacked, so the top edge is the total. */
function GrowthChart({ libraries }: { libraries: StorageLibrary[] }) {
  const dates = [...new Set(libraries.flatMap((l) => l.history.map((s) => s.date)))].sort();
  if (dates.length < 2) {
    return (
      <p className="text-sm text-muted" data-testid="storage-growth-empty">
        The growth chart fills in from one reading a day; check back tomorrow.
      </p>
    );
  }
  // A library's size on each date: its sample that day, else its latest before (0 before its first).
  const values = libraries.map((l) => {
    const by = new Map(l.history.map((s) => [s.date, s.bytes]));
    let last = 0;
    return dates.map((d) => (last = by.get(d) ?? last));
  });
  const totals = dates.map((_, i) => values.reduce((sum, v) => sum + (v[i] ?? 0), 0));
  const max = niceMax(Math.max(...totals) * 1.05);
  const x = (i: number) => (i / (dates.length - 1)) * W;
  const y = (b: number) => H - (b / max) * H;
  const below = dates.map(() => 0);
  const areas = values.map((v) => {
    const lower = [...below];
    const upper = v.map((b, i) => (below[i] = (below[i] ?? 0) + b));
    const top = upper.map((b, i) => `${x(i).toFixed(1)},${y(b).toFixed(1)}`);
    const bottom = lower.map((b, i) => `${x(i).toFixed(1)},${y(b).toFixed(1)}`).reverse();
    return `M${top.join("L")}L${bottom.join("L")}Z`;
  });
  const first = dates[0]!;
  const last = dates.at(-1)!;
  const day = (d: string) =>
    new Date(`${d}T12:00:00`).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });
  return (
    <figure data-testid="storage-growth">
      <div className="flex gap-2">
        <div className="relative h-28 w-16 shrink-0 text-right text-[10px] text-faint tabular-nums" aria-hidden>
          <span className="absolute right-0 -translate-y-1/2" style={{ top: 0 }}>
            {formatBytes(max)}
          </span>
          <span className="absolute right-0 -translate-y-1/2" style={{ top: "50%" }}>
            {formatBytes(max / 2)}
          </span>
          <span className="absolute right-0 -translate-y-full" style={{ top: "100%" }}>
            0
          </span>
        </div>
        <div className="relative h-28 min-w-0 flex-1">
          <svg
            viewBox={`0 0 ${W} ${H}`}
            preserveAspectRatio="none"
            className="absolute inset-0 h-full w-full overflow-visible"
            role="img"
            aria-label={`Library sizes from ${day(first)} to ${day(last)}`}
          >
            {[0, 0.5, 1].map((f) => (
              <line key={f} x1={0} x2={W} y1={H * f} y2={H * f} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
            ))}
            {areas.map((d, i) => (
              <path key={libraries[i]?.id ?? i} d={d} className={color(i).fill} />
            ))}
          </svg>
        </div>
      </div>
      <div className="mt-1 ml-18 flex justify-between text-[10px] text-faint" aria-hidden>
        <span>{day(first)}</span>
        <span>{day(last)}</span>
      </div>
    </figure>
  );
}

/** The change over the period: the size now against the period's first reading. */
function change(l: StorageLibrary) {
  const first = l.history[0];
  if (!first) return undefined;
  return l.bytes - first.bytes;
}

function LibraryTable({ libraries }: { libraries: StorageLibrary[] }) {
  const total = libraries.reduce((sum, l) => sum + l.bytes, 0);
  const files = libraries.reduce((sum, l) => sum + l.files, 0);
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs text-faint uppercase">
            <th className="pb-2 font-normal">Library</th>
            <th className="pb-2 pl-3 text-right font-normal whitespace-nowrap">Size</th>
            <th className="pb-2 pl-3 text-right font-normal whitespace-nowrap">Files</th>
            <th className="pb-2 pl-3 text-right font-normal whitespace-nowrap">Change</th>
          </tr>
        </thead>
        <tbody className="tabular-nums [&_td+td]:pl-3 [&_td+td]:whitespace-nowrap">
          {libraries.map((l, i) => {
            const c = change(l);
            return (
              <tr key={l.id} className="border-t border-border" data-testid="storage-library">
                <td className="w-full max-w-0 py-1.5">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className={clsx("size-2.5 shrink-0 rounded-sm", color(i).dot)} />
                    <span className="truncate">{l.name}</span>
                  </span>
                </td>
                <td className="py-1.5 text-right">{formatBytes(l.bytes)}</td>
                <td className="py-1.5 text-right text-muted">{l.files.toLocaleString()}</td>
                <td className={clsx("py-1.5 text-right", c ? (c > 0 ? "text-text" : "text-success") : "text-faint")}>
                  {c === undefined ? "–" : c === 0 ? "No change" : `${c > 0 ? "+" : "−"}${formatBytes(Math.abs(c))}`}
                </td>
              </tr>
            );
          })}
        </tbody>
        {libraries.length > 1 && (
          <tfoot className="tabular-nums [&_td+td]:pl-3 [&_td+td]:whitespace-nowrap">
            <tr className="border-t border-border font-medium">
              <td className="py-1.5">Total</td>
              <td className="py-1.5 text-right">{formatBytes(total)}</td>
              <td className="py-1.5 text-right text-muted">{files.toLocaleString()}</td>
              <td />
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}

const typeLabel: Record<string, string> = {
  movie: "Movie",
  show: "Show",
  video: "Video",
};

function LargestList({ titles }: { titles: StorageTitle[] }) {
  if (titles.length === 0) return <p className="text-sm text-muted">No titles yet.</p>;
  return (
    <ol className="space-y-1.5 text-sm">
      {titles.map((t, i) => {
        const perFile = t.files ? t.bytes / t.files : t.bytes;
        const optimize = (t.type === "movie" || t.type === "show") && perFile >= OPTIMIZE_BYTES;
        return (
          <li key={t.itemId} className="flex items-baseline gap-2" data-testid="storage-title">
            <span className="w-5 shrink-0 text-right text-xs text-faint tabular-nums">{i + 1}</span>
            <span className="min-w-0 flex-1">
              <Link to="/item/$itemId" params={{ itemId: String(t.itemId) }} className="block truncate hover:underline">
                {t.title}
                {t.year ? <span className="text-muted"> ({t.year})</span> : null}
              </Link>
              <span className="block text-xs text-muted">
                {typeLabel[t.type] ?? t.type}
                {t.type === "show" && t.files ? ` · ${t.files.toLocaleString()} files` : ""}
                {optimize && (
                  <>
                    {" · "}
                    <Link
                      to="/settings/$section"
                      params={{ section: "library-health" }}
                      hash="large-files"
                      className="text-accent hover:underline"
                      title="Convert big files to a smaller HEVC copy in Library Health → Large files"
                    >
                      Optimize
                    </Link>
                  </>
                )}
              </span>
            </span>
            <span className="shrink-0 tabular-nums">{formatBytes(t.bytes)}</span>
          </li>
        );
      })}
    </ol>
  );
}

/** The storage report laid out (without fetching it). */
export function StorageReportView({ report }: { report: Report }) {
  return (
    <div className="space-y-6">
      {report.volumes.length > 0 && (
        <div>
          <h3 className="mb-2 text-sm font-medium">Disks</h3>
          <ul className="space-y-3">
            {report.volumes.map((v) => (
              <VolumeBar key={v.path} v={v} />
            ))}
          </ul>
        </div>
      )}
      <div className="grid gap-6 xl:grid-cols-2">
        <div>
          <h3 className="mb-2 text-sm font-medium">Libraries</h3>
          {report.libraries.length === 0 ? <p className="text-sm text-muted">No libraries yet.</p> : <LibraryTable libraries={report.libraries} />}
        </div>
        <div>
          <h3 className="mb-2 text-sm font-medium">Growth</h3>
          <GrowthChart libraries={report.libraries} />
        </div>
      </div>
      <div>
        <h3 className="mb-2 text-sm font-medium">Biggest titles</h3>
        <LargestList titles={report.largest} />
      </div>
    </div>
  );
}

/** Dashboard → Storage. */
export function StorageCard() {
  const [days, setDays] = useState(30);
  const report = useQuery({
    queryKey: ["dashboard", "storage", days],
    queryFn: () => unwrap(api.GET("/system/storage", { params: { query: { days, top: 10 } } })),
    refetchInterval: 5 * 60_000,
  });
  return (
    <Card
      title="Storage"
      description="Space used by each library's files and the free space on the disks Marquee uses."
      actions={
        <div className="w-40">
          <Select aria-label="Growth period" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            <option value={30}>Last 30 days</option>
            <option value={90}>Last 90 days</option>
            <option value={365}>Last year</option>
          </Select>
        </div>
      }
    >
      {report.isPending && <Spinner />}
      {report.isError && <p className="text-sm text-danger">Couldn't load the storage overview.</p>}
      {report.data && <StorageReportView report={report.data} />}
    </Card>
  );
}
