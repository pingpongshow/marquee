import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Card, Select, Spinner } from "@/components/ui";

// The dashboard's server statistics, the way Plex shows them: the machine's CPU, memory and
// GPU next to Marquee's own over the last hour, and who's been watching what.

type Status = components["schemas"]["SystemStatus"];
type Resource = components["schemas"]["ResourceSample"];

function gb(bytes: number) {
  return `${(bytes / 1e9).toFixed(1)} GB`;
}

type Series = { label: string; className: string; dot: string; value: (r: Resource) => number | undefined };

const W = 600;
const H = 100;

/** Lines over the last hour on a 0–max scale. */
function UsageChart({ samples, series, max, format, label }: { samples: Resource[]; series: Series[]; max: number; format: (v: number) => string; label: string }) {
  if (samples.length < 2) {
    return <p className="text-sm text-muted">Collecting… readings appear within a minute of the server starting.</p>;
  }
  const end = new Date(samples.at(-1)!.at).getTime();
  const start = end - 60 * 60 * 1000;
  const x = (iso: string) => Math.max(0, ((new Date(iso).getTime() - start) / (60 * 60 * 1000)) * W);
  const y = (v: number) => H - (Math.min(v, max) / max) * H;
  const last = samples.at(-1)!;
  return (
    <figure>
      <div className="flex gap-2">
        <div className="relative h-28 w-14 shrink-0 text-right text-[10px] text-faint tabular-nums" aria-hidden>
          <span className="absolute right-0 -translate-y-1/2" style={{ top: 0 }}>
            {format(max)}
          </span>
          <span className="absolute right-0 -translate-y-1/2" style={{ top: "50%" }}>
            {format(max / 2)}
          </span>
          <span className="absolute right-0 -translate-y-full" style={{ top: "100%" }}>
            0
          </span>
        </div>
        <div className="relative h-28 min-w-0 flex-1">
          <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="absolute inset-0 h-full w-full overflow-visible" role="img" aria-label={label}>
            {[0, 0.5, 1].map((f) => (
              <line key={f} x1={0} x2={W} y1={H * f} y2={H * f} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
            ))}
            {series.map((s) => {
              const pts = samples.filter((p) => s.value(p) !== undefined).map((p) => `${x(p.at).toFixed(1)},${y(s.value(p)!).toFixed(1)}`);
              return pts.length > 1 ? <polyline key={s.label} points={pts.join(" ")} fill="none" className={s.className} strokeWidth={2} vectorEffect="non-scaling-stroke" /> : null;
            })}
          </svg>
        </div>
      </div>
      <div className="mt-1 ml-16 flex justify-between text-[10px] text-faint" aria-hidden>
        <span>60 min ago</span>
        <span>30 min ago</span>
        <span>Now</span>
      </div>
      <figcaption className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted">
        {series.map((s) => {
          const v = s.value(last);
          return (
            <span key={s.label} className="flex items-center gap-1.5">
              <span className={`size-2.5 rounded-sm ${s.dot}`} /> {s.label}
              {v !== undefined && <span className="text-text tabular-nums">{format(v)}</span>}
            </span>
          );
        })}
      </figcaption>
    </figure>
  );
}

const pct = (v: number) => `${Math.round(v)}%`;

/** CPU, memory and (NVIDIA) GPU over the last hour. */
export function ResourcesCard({ status }: { status: Status }) {
  const samples = status.resources ?? [];
  const host = status.host;
  const hasGPU = samples.some((s) => s.gpu !== undefined);
  return (
    <Card title="Server" description={host ? [`${host.cpuCores} CPU threads`, `${gb(host.memTotalBytes)} memory`, host.gpuName].filter(Boolean).join(" · ") : undefined}>
      {!host ? (
        <p className="text-sm text-muted">CPU and memory readings aren't available on this system.</p>
      ) : (
        <div className="grid gap-6 lg:grid-cols-2">
          <div>
            <h3 className="mb-2 text-sm font-medium">CPU</h3>
            <UsageChart
              samples={samples}
              max={100}
              format={pct}
              label="CPU use over the last hour"
              series={[
                { label: "System", className: "stroke-sky-400", dot: "bg-sky-400", value: (r) => r.hostCpu },
                { label: "Marquee", className: "stroke-accent", dot: "bg-accent", value: (r) => r.serverCpu },
              ]}
            />
          </div>
          <div>
            <h3 className="mb-2 text-sm font-medium">Memory</h3>
            <UsageChart
              samples={samples}
              max={host.memTotalBytes}
              format={gb}
              label="Memory use over the last hour"
              series={[
                { label: "System", className: "stroke-sky-400", dot: "bg-sky-400", value: (r) => r.hostMemBytes },
                { label: "Marquee", className: "stroke-accent", dot: "bg-accent", value: (r) => r.serverMemBytes },
              ]}
            />
          </div>
          {hasGPU && (
            <div>
              <h3 className="mb-2 text-sm font-medium">GPU</h3>
              <UsageChart
                samples={samples}
                max={100}
                format={pct}
                label="GPU use over the last hour"
                series={[
                  { label: "GPU", className: "stroke-sky-400", dot: "bg-sky-400", value: (r) => r.gpu },
                  { label: "Encoder", className: "stroke-accent", dot: "bg-accent", value: (r) => r.gpuEncoder },
                  { label: "Decoder", className: "stroke-emerald-400", dot: "bg-emerald-400", value: (r) => r.gpuDecoder },
                ]}
              />
            </div>
          )}
        </div>
      )}
      <div className="mt-5 flex flex-wrap gap-x-6 gap-y-1 border-t border-border pt-4 text-xs text-muted">
        <span>Marquee {status.version}</span>
        <span>Up since {new Date(status.startedAt).toLocaleString()}</span>
        {status.libraries.map((l) => (
          <span key={l.id}>
            {l.name}: {l.items.toLocaleString()}
          </span>
        ))}
      </div>
    </Card>
  );
}

function hours(h: number) {
  return h >= 10 ? `${Math.round(h)} h` : `${h.toFixed(1)} h`;
}

function TopList({ title, rows }: { title: string; rows: components["schemas"]["StatsCount"][] }) {
  return (
    <div>
      <h3 className="mb-2 text-sm font-medium">{title}</h3>
      {rows.length === 0 ? (
        <p className="text-sm text-muted">Nothing yet.</p>
      ) : (
        <ol className="space-y-1.5 text-sm">
          {rows.map((r, i) => (
            <li key={`${r.id ?? r.title}-${i}`} className="flex items-baseline gap-2">
              <span className="w-4 shrink-0 text-right text-xs text-faint tabular-nums">{i + 1}</span>
              <span className="min-w-0 flex-1">
                <span className="block truncate" title={r.subtitle ? `${r.title} · ${r.subtitle}` : r.title}>
                  {r.title}
                </span>
                <span className="block text-xs text-muted tabular-nums">
                  {r.plays.toLocaleString()} {r.plays === 1 ? "play" : "plays"} · {hours(r.hours)}
                </span>
              </span>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

/** Who's been watching and what: the top people, films and shows, and how things played. */
export function WatchStatsCard() {
  const [days, setDays] = useState(30);
  const stats = useQuery({
    queryKey: ["dashboard", "stats", days],
    queryFn: () => unwrap(api.GET("/stats", { params: { query: { days, limit: 5 } } })),
    refetchInterval: 60_000,
  });
  const s = stats.data;
  const methods = s ? s.methods.directPlay + s.methods.directStream + s.methods.transcode : 0;
  const share = (n: number, of: number) => (of ? `${Math.round((n / of) * 100)}%` : "–");
  return (
    <Card title="Watch stats">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="w-44">
          <Select aria-label="Period" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            <option value={1}>Last 24 hours</option>
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={365}>Last year</option>
            <option value={0}>All time</option>
          </Select>
        </div>
        <Link to="/settings/$section" params={{ section: "statistics" }} className="text-sm text-accent hover:underline">
          More in Statistics
        </Link>
      </div>
      {stats.isPending && <Spinner />}
      {s && (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            <div>
              <div className="text-xs text-faint uppercase">Plays</div>
              <div className="text-lg font-semibold tabular-nums">{s.plays.toLocaleString()}</div>
            </div>
            <div>
              <div className="text-xs text-faint uppercase">Watched</div>
              <div className="text-lg font-semibold tabular-nums">{hours(s.videoHours)}</div>
            </div>
            <div>
              <div className="text-xs text-faint uppercase">Listened</div>
              <div className="text-lg font-semibold tabular-nums">{hours(s.musicHours)}</div>
            </div>
            <div>
              <div className="text-xs text-faint uppercase">People</div>
              <div className="text-lg font-semibold tabular-nums">{s.users}</div>
            </div>
          </div>
          <div className="grid gap-6 md:grid-cols-3">
            <TopList title="Top people" rows={s.people} />
            <TopList title="Top movies" rows={s.movies} />
            <TopList title="Top shows" rows={s.shows} />
          </div>
          <div className="flex flex-wrap gap-x-6 gap-y-1 border-t border-border pt-4 text-xs text-muted">
            <span>Direct Play {share(s.methods.directPlay, methods)}</span>
            <span>Direct Stream {share(s.methods.directStream, methods)}</span>
            <span>Transcode {share(s.methods.transcode, methods)}</span>
            <span>
              Local {share(s.local, s.local + s.remote)} · Remote {share(s.remote, s.local + s.remote)}
            </span>
          </div>
        </div>
      )}
    </Card>
  );
}
