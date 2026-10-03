import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clsx } from "clsx";
import { Cpu, Globe, Home, Square, Tv } from "lucide-react";
import { api, imageUrl, unwrap } from "@/api/client";
import type { components } from "@/api/schema.gen";
import { Card, Spinner } from "@/components/ui";

type Session = components["schemas"]["PlaybackSessionInfo"];

const methodLabel = { direct_play: "Direct Play", direct_stream: "Direct Stream", transcode: "Transcode" } as const;
const encoderLabel: Record<string, string> = { nvenc: "NVIDIA NVENC", qsv: "Intel Quick Sync", software: "CPU", copy: "Copy" };

function mbps(kbps: number) {
  return `${(kbps / 1000).toFixed(1)} Mbps`;
}

function time(ms: number) {
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return h ? `${h}:${String(m).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}` : `${m}:${String(s % 60).padStart(2, "0")}`;
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <div className="text-xs tracking-wider text-faint uppercase">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      {hint && <div className="mt-0.5 text-xs text-muted">{hint}</div>}
    </div>
  );
}

function SessionCard({ s }: { s: Session }) {
  const qc = useQueryClient();
  const stop = useMutation({
    mutationFn: () => unwrap(api.DELETE("/playback/sessions/{sessionId}", { params: { path: { sessionId: s.id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dashboard"] }),
  });
  const pct = s.durationMs ? Math.min(100, (s.positionMs / s.durationMs) * 100) : 0;
  return (
    <li className="flex gap-4 rounded-lg border border-border bg-surface p-3">
      <div className="aspect-video w-40 shrink-0 overflow-hidden rounded bg-surface-3">{s.imageId && <img src={imageUrl(s.imageId, 160)} alt="" className="size-full object-cover" />}</div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium">{s.title}</div>
            {s.subtitle && <div className="truncate text-xs text-muted">{s.subtitle}</div>}
          </div>
          <button onClick={() => stop.mutate()} className="rounded p-1.5 text-muted hover:bg-surface-2 hover:text-danger" title="Stop this stream" aria-label={`Stop ${s.title}`}>
            <Square className="size-4" />
          </button>
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          <span className="font-medium text-text">{s.userName}</span>
          <span>{s.deviceName}</span>
          <span className="flex items-center gap-1">
            {s.networkClass === "remote" ? <Globe className="size-3" /> : <Home className="size-3" />}
            {s.networkClass === "remote" ? "Remote" : "Local"}
            {s.clientIp ? ` · ${s.clientIp}` : ""}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span
            className={clsx("rounded px-1.5 py-0.5 font-medium", s.method === "transcode" ? "bg-accent/15 text-accent" : "bg-success/15 text-success")}
            title={s.reasons?.join("\n")}
          >
            {methodLabel[s.method]}
          </span>
          <span className="text-muted">{s.summary}</span>
          {s.encoder && (
            <span className="flex items-center gap-1 text-muted">
              <Cpu className="size-3" /> {encoderLabel[s.encoder] ?? s.encoder}
            </span>
          )}
          <span className="text-muted">{mbps(s.bitrateKbps)}</span>
          {s.state === "paused" && <span className="text-faint">Paused</span>}
        </div>
        {s.reasons && s.reasons.length > 0 && <div className="truncate text-[11px] text-faint">{s.reasons.join(" · ")}</div>}
        <div className="flex items-center gap-2 pt-1">
          <div className="h-1 flex-1 overflow-hidden rounded-full bg-surface-3">
            <div className="h-full bg-accent" style={{ width: `${pct}%` }} />
          </div>
          <span className="text-[11px] text-muted tabular-nums">
            {time(s.positionMs)} / {time(s.durationMs)}
          </span>
        </div>
      </div>
    </li>
  );
}

type Sample = components["schemas"]["BandwidthSample"];

/** Local and remote bandwidth over the last hour, stacked (ADM-10). */
/** A round axis maximum (1, 2, 5 × 10ⁿ Mbps) at or above the value. */
function niceMaxKbps(kbps: number) {
  const m = Math.max(kbps, 1000) / 1000;
  const p = 10 ** Math.floor(Math.log10(m));
  const step = [1, 2, 2.5, 5, 10].find((f) => f * p >= m) ?? 10;
  return step * p * 1000;
}

const axisMbps = (kbps: number) => `${+(kbps / 1000).toFixed(1)} Mbps`;

/**
 * Estimated stream bitrate over the last hour, one sample a minute (kept in memory,
 * so it starts empty after a restart). Labels are HTML over the SVG so they don't stretch.
 */
export function BandwidthChart({ samples, uploadKbps }: { samples: Sample[]; uploadKbps?: number }) {
  const W = 600;
  const H = 140;
  const total = (p: Sample) => p.localKbps + p.remoteKbps;
  const peak = Math.max(0, ...samples.map(total));
  if (samples.length < 2 || peak <= 0) {
    return (
      <div className="flex h-36 flex-col items-center justify-center gap-1 rounded-md border border-dashed border-border text-center" data-testid="bandwidth-empty">
        <p className="text-sm font-medium text-muted">No streaming in the last hour</p>
        <p className="text-xs text-faint">{uploadKbps ? `Upload speed ${mbps(uploadKbps)}. ` : ""}The chart fills in a minute at a time while something plays.</p>
      </div>
    );
  }
  const max = niceMaxKbps(Math.max(peak, uploadKbps ?? 0) * 1.05);
  // The newest sample is "now" on the server, so the window ends there.
  const end = new Date(samples.at(-1)!.at).getTime();
  const start = end - 60 * 60 * 1000;
  const x = (iso: string) => Math.max(0, ((new Date(iso).getTime() - start) / (60 * 60 * 1000)) * W);
  const y = (kbps: number) => H - (kbps / max) * H;
  const pct = (kbps: number) => `${(1 - kbps / max) * 100}%`;
  const area = (top: (x: Sample) => number, bottom: (x: Sample) => number) => {
    const upper = samples.map((p) => `${x(p.at).toFixed(1)},${y(top(p)).toFixed(1)}`);
    const lower = [...samples].reverse().map((p) => `${x(p.at).toFixed(1)},${y(bottom(p)).toFixed(1)}`);
    return `M${upper.join("L")}L${lower.join("L")}Z`;
  };
  return (
    <figure data-testid="bandwidth-chart">
      <div className="flex gap-2">
        {/* Y axis */}
        <div className="relative h-36 w-16 shrink-0 text-right text-[10px] text-faint tabular-nums" aria-hidden>
          <span className="absolute right-0 -translate-y-1/2" style={{ top: 0 }}>
            {axisMbps(max)}
          </span>
          <span className="absolute right-0 -translate-y-1/2" style={{ top: "50%" }}>
            {axisMbps(max / 2)}
          </span>
          <span className="absolute right-0 -translate-y-full" style={{ top: "100%" }}>
            0
          </span>
        </div>
        <div className="relative h-36 min-w-0 flex-1">
          <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="absolute inset-0 h-full w-full overflow-visible" role="img" aria-label={`Bandwidth over the last hour, peak ${mbps(peak)}`}>
            {[0, 0.5, 1].map((f) => (
              <line key={f} x1={0} x2={W} y1={H * f} y2={H * f} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
            ))}
            <path d={area((p) => p.localKbps, () => 0)} className="fill-accent/50" />
            <path d={area(total, (p) => p.localKbps)} className="fill-sky-400/50" />
            <line x1={0} x2={W} y1={y(peak)} y2={y(peak)} className="stroke-muted" strokeDasharray="2 3" strokeWidth={1} vectorEffect="non-scaling-stroke" />
            {uploadKbps ? <line x1={0} x2={W} y1={y(uploadKbps)} y2={y(uploadKbps)} className="stroke-danger" strokeDasharray="4 4" strokeWidth={1.5} vectorEffect="non-scaling-stroke" /> : null}
          </svg>
          <span className="pointer-events-none absolute left-1 -translate-y-full rounded bg-surface/80 px-1 text-[10px] text-muted tabular-nums" style={{ top: pct(peak) }}>
            Peak {mbps(peak)}
          </span>
          {uploadKbps ? (
            <span className="pointer-events-none absolute right-1 -translate-y-full rounded bg-surface/80 px-1 text-[10px] text-danger tabular-nums" style={{ top: pct(uploadKbps) }}>
              Upload speed {mbps(uploadKbps)}
            </span>
          ) : null}
        </div>
      </div>
      {/* X axis */}
      <div className="mt-1 ml-18 flex justify-between text-[10px] text-faint" aria-hidden>
        <span>60 min ago</span>
        <span>30 min ago</span>
        <span>Now</span>
      </div>
      <figcaption className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted">
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-sm bg-accent/70" /> Local
        </span>
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-sm bg-sky-400/70" /> Remote
        </span>
        {uploadKbps ? (
          <span className="flex items-center gap-1.5">
            <span className="h-px w-3 border-t border-dashed border-danger" /> Upload speed
          </span>
        ) : null}
        <span className="ml-auto">Estimated stream bitrate, sampled each minute</span>
      </figcaption>
    </figure>
  );
}

export function DashboardSettings() {
  const sessions = useQuery({ queryKey: ["dashboard", "sessions"], queryFn: () => unwrap(api.GET("/playback/sessions")), refetchInterval: 3000 });
  const status = useQuery({ queryKey: ["dashboard", "status"], queryFn: () => unwrap(api.GET("/system/status")), refetchInterval: 5000 });
  const history = useQuery({ queryKey: ["dashboard", "history"], queryFn: () => unwrap(api.GET("/activity/history", { params: { query: { limit: 40 } } })), refetchInterval: 30000 });
  const st = status.data;
  return (
    <div className="space-y-6">
      {st && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Stat label="Streams" value={String(st.activeStreams)} hint={st.activeStreams ? undefined : "Nothing playing"} />
          <Stat label="Transcodes" value={`${st.activeTranscodes} / ${st.maxTranscodes}`} hint={st.encoders.map((e) => encoderLabel[e] ?? e).join(" → ")} />
          <Stat label="Local" value={mbps(st.localKbps)} />
          <Stat label="Remote" value={mbps(st.remoteKbps)} hint={st.uploadSpeedKbps ? `of ${mbps(st.uploadSpeedKbps)} upload` : "Set upload speed in Remote Access"} />
        </div>
      )}
      {st && (
        <Card title="Bandwidth" description="The last hour. Starts empty after the server restarts.">
          <BandwidthChart samples={st.bandwidthHistory ?? []} uploadKbps={st.uploadSpeedKbps} />
        </Card>
      )}
      <Card title="Now playing">
        {sessions.isPending && <Spinner />}
        {sessions.data?.length === 0 && (
          <p className="flex items-center gap-2 text-sm text-muted">
            <Tv className="size-4" /> Nobody is watching right now.
          </p>
        )}
        <ul className="space-y-3">
          {sessions.data?.map((s) => (
            <SessionCard key={s.id} s={s} />
          ))}
        </ul>
      </Card>
      <Card title="Recent plays">
        {history.isPending && <Spinner />}
        <ul className="-my-2 divide-y divide-border text-sm">
          {history.data?.map((h) => (
            <li key={h.id} className="flex items-center gap-3 py-2">
              <span className="w-32 shrink-0 text-xs text-faint tabular-nums">{new Date(h.startedAt).toLocaleString([], { dateStyle: "short", timeStyle: "short" })}</span>
              <span className="w-28 shrink-0 truncate text-muted">{h.userName}</span>
              <span className="min-w-0 flex-1 truncate">{h.title}</span>
              <span className="hidden shrink-0 text-xs text-faint sm:inline">
                {h.source === "plex" ? "Plex (imported)" : [h.method && methodLabel[h.method as keyof typeof methodLabel], h.networkClass === "remote" ? "Remote" : h.networkClass ? "Local" : ""].filter(Boolean).join(" · ")}
              </span>
            </li>
          ))}
        </ul>
        {st && (
          <div className="flex flex-wrap gap-x-6 gap-y-1 border-t border-border pt-4 text-xs text-muted">
            <span>Marquee {st.version}</span>
            <span>Up since {new Date(st.startedAt).toLocaleString()}</span>
            {st.libraries.map((l) => (
              <span key={l.id}>
                {l.name}: {l.items.toLocaleString()}
              </span>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
