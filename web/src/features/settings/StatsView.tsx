import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { usersQuery } from "@/api/queries";
import type { components } from "@/api/schema.gen";
import { Alert, Card, Select, Spinner } from "@/components/ui";

type Stats = components["schemas"]["Stats"];
type Count = components["schemas"]["StatsCount"];

const periods = [
  { days: 7, label: "Last 7 days" },
  { days: 30, label: "Last 30 days" },
  { days: 90, label: "Last 90 days" },
  { days: 365, label: "Last year" },
  { days: 0, label: "All time" },
];

const hrs = (h: number) => (h >= 100 ? `${Math.round(h).toLocaleString()} h` : h >= 1 ? `${h.toFixed(1)} h` : `${Math.round(h * 60)} min`);

/** Playback statistics (ADM-4): everyone for admins (with a person filter), or your own. */
export function StatsView({ admin }: { admin: boolean }) {
  const [days, setDays] = useState(30);
  const [user, setUser] = useState<number | undefined>();
  const users = useQuery({ ...usersQuery, enabled: admin });
  const stats = useQuery({
    queryKey: ["stats", days, user],
    queryFn: () => unwrap(api.GET("/stats", { params: { query: { days, userId: user, limit: 10 } } })),
  });
  const s = stats.data;
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap gap-3">
        <div className="w-44">
          <Select aria-label="Period" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            {periods.map((p) => (
              <option key={p.days} value={p.days}>
                {p.label}
              </option>
            ))}
          </Select>
        </div>
        {admin && (
          <div className="w-52">
            <Select aria-label="Person" value={user ?? ""} onChange={(e) => setUser(e.target.value ? Number(e.target.value) : undefined)}>
              <option value="">Everyone</option>
              {users.data?.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.displayName}
                </option>
              ))}
            </Select>
          </div>
        )}
      </div>
      {stats.isError && <Alert tone="error">{stats.error.message}</Alert>}
      {stats.isPending && <Spinner />}
      {s && s.plays === 0 && <p className="text-muted">Nothing played in this period.</p>}
      {s && s.plays > 0 && (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Tile label="Plays" value={s.plays.toLocaleString()} />
            <Tile label="Watched" value={hrs(s.videoHours)} />
            <Tile label="Listened" value={hrs(s.musicHours)} />
            {admin && !user ? <Tile label="People" value={String(s.users)} /> : <Tile label="Per day" value={hrs(s.hours / Math.max(1, s.days.length))} />}
          </div>
          <Card title="Plays per day">
            <DailyChart days={s.days} period={days} />
          </Card>
          <div className="grid gap-6 lg:grid-cols-2">
            <TopList title="Movies" items={s.movies} link />
            <TopList title="Shows" items={s.shows} link />
            <TopList title="Artists" items={s.artists} link />
            <TopList title="Albums" items={s.albums} link />
            <TopList title="Tracks" items={s.tracks} link />
            {admin && !user && <TopList title="People" items={s.people} />}
            <TopList title="Apps" items={s.platforms.map((p) => ({ ...p, title: platformName(p.title) }))} />
            <Delivery s={s} />
          </div>
        </>
      )}
    </div>
  );
}

function platformName(p: string) {
  return ({ web: "Web", ios: "iPhone", ipados: "iPad", tvos: "Apple TV", android: "Android", plex: "Plex (imported)", marquee: "Other" } as Record<string, string>)[p] ?? p;
}

function Tile({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-surface px-4 py-3">
      <div className="text-xs text-muted uppercase">{label}</div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

/** Stacked daily bars: video and music plays. Days with nothing played are filled in. */
function DailyChart({ days, period }: { days: Stats["days"]; period: number }) {
  const byDate = new Map(days.map((d) => [d.date, d]));
  const span = period || Math.min(365, Math.max(30, days.length));
  const list: { date: string; video: number; music: number }[] = [];
  const end = new Date();
  for (let i = span - 1; i >= 0; i--) {
    const d = new Date(end);
    d.setDate(end.getDate() - i);
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    const v = byDate.get(key);
    list.push({ date: key, video: v?.video ?? 0, music: v?.music ?? 0 });
  }
  const max = Math.max(1, ...list.map((d) => d.video + d.music));
  return (
    <figure>
      <div className="flex h-36 items-end gap-px" role="img" aria-label={`Plays per day over ${span} days, at most ${max} in a day`}>
        {list.map((d) => (
          <div key={d.date} className="flex h-full min-w-0 flex-1 flex-col justify-end" title={`${d.date}: ${d.video} video, ${d.music} music`}>
            <div className="bg-sky-400/70" style={{ height: `${(d.music / max) * 100}%` }} />
            <div className="bg-accent/80" style={{ height: `${(d.video / max) * 100}%` }} />
          </div>
        ))}
      </div>
      <figcaption className="mt-2 flex gap-5 text-xs text-muted">
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-sm bg-accent/80" /> Video
        </span>
        <span className="flex items-center gap-1.5">
          <span className="size-2.5 rounded-sm bg-sky-400/70" /> Music
        </span>
        <span className="ml-auto">busiest day: {max} plays</span>
      </figcaption>
    </figure>
  );
}

function TopList({ title, items, link }: { title: string; items: Count[]; link?: boolean }) {
  if (!items.length) return null;
  const max = items[0]!.plays;
  return (
    <Card title={title}>
      <ol className="-my-2 space-y-1">
        {items.map((c, i) => (
          <li key={`${c.id ?? c.title}-${i}`} className="relative flex items-center gap-3 overflow-hidden rounded px-2 py-1.5">
            <div className="absolute inset-y-0 left-0 rounded bg-surface-2" style={{ width: `${(c.plays / max) * 100}%` }} aria-hidden />
            <span className="relative w-5 text-right text-xs text-faint tabular-nums">{i + 1}</span>
            <span className="relative min-w-0 flex-1">
              {link && c.id ? (
                <Link to="/item/$itemId" params={{ itemId: String(c.id) }} className="block truncate text-sm font-medium hover:underline">
                  {c.title}
                </Link>
              ) : (
                <span className="block truncate text-sm font-medium">{c.title}</span>
              )}
              {c.subtitle && <span className="block truncate text-xs text-muted">{c.subtitle}</span>}
            </span>
            <span className="relative text-right text-xs text-muted tabular-nums">
              {c.plays.toLocaleString()} {c.plays === 1 ? "play" : "plays"}
              <br />
              {hrs(c.hours)}
            </span>
          </li>
        ))}
      </ol>
    </Card>
  );
}

function Delivery({ s }: { s: Stats }) {
  const rows = [
    { label: "Direct play", n: s.methods.directPlay, cls: "bg-success/70" },
    { label: "Direct stream", n: s.methods.directStream, cls: "bg-sky-400/70" },
    { label: "Transcode", n: s.methods.transcode, cls: "bg-accent/80" },
  ];
  const total = Math.max(1, rows.reduce((a, r) => a + r.n, 0));
  const net = Math.max(1, s.local + s.remote);
  return (
    <Card title="How it played">
      <div className="space-y-3">
        <div className="flex h-3 overflow-hidden rounded-full bg-surface-2" role="img" aria-label="Delivery method">
          {rows.map((r) => (
            <div key={r.label} className={r.cls} style={{ width: `${(r.n / total) * 100}%` }} />
          ))}
        </div>
        <ul className="grid grid-cols-3 gap-2 text-xs text-muted">
          {rows.map((r) => (
            <li key={r.label}>
              <span className="font-medium text-text tabular-nums">{Math.round((r.n / total) * 100)}%</span> {r.label.toLowerCase()}
            </li>
          ))}
        </ul>
        <p className="text-sm text-muted">
          {Math.round((s.local / net) * 100)}% at home, {Math.round((s.remote / net) * 100)}% away (over Tailscale).
        </p>
      </div>
    </Card>
  );
}
