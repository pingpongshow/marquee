import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { clsx } from "clsx";
import {
  ChevronLeft,
  ChevronRight,
  Eye,
  Heart,
  Maximize2,
  Volume2,
  VolumeX,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Alert, Button, Select, Spinner } from "@/components/ui";
import {
  type LiveChannel,
  type LiveFilter,
  type LiveProgramme,
  liveChannelsQuery,
  liveGroupsQuery,
  liveGuideQuery,
  liveStatusQuery,
  minutesLeft,
  timeLabel,
  useFavorite,
  useHideChannel,
  type Recording,
  recordingRulesQuery,
  recordingsQuery,
  formatSize,
  useCancelRecording,
  useDeleteRule,
  useSchedule,
} from "./api";
import { LivePlayer } from "./LivePlayer";

const PX_PER_MIN = 6; // 30 minutes = 180 px
const WINDOW_HOURS = 6;
const ROW = 72;
const CHANNEL_COL = 132;

/** Live TV (LIVE-2): a Plex-style guide and What's On, with a live preview. */
export function LiveTvPage() {
  const status = useQuery(liveStatusQuery);
  const groups = useQuery(liveGroupsQuery);
  const [tab, setTab] = useState<"guide" | "now" | "recordings">("guide");
  const [filter, setFilter] = useState<LiveFilter>(
    () => localStorageGet("marquee.livetv.filter") ?? "all",
  );
  const channels = useQuery(liveChannelsQuery(filter));
  const [preview, setPreview] = useState<number | null>(null);
  const [muted, setMuted] = useState(true);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const navigate = useNavigate();
  useEffect(() => localStorageSet("marquee.livetv.filter", filter), [filter]);
  const current =
    channels.data?.find((c) => c.id === preview) ?? channels.data?.[0];

  if (status.isPending) return <Spinner />;
  if (!status.data?.enabled)
    return (
      <div className="p-6">
        <Alert>
          Live TV isn't set up yet. An admin can add an M3U playlist or
          Dispatcharr in Settings → Live TV.
        </Alert>
      </div>
    );

  return (
    <div className="space-y-4 p-4 lg:p-6">
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-bold whitespace-nowrap">Live TV</h1>
        <div className="ml-4 flex gap-1" role="tablist">
          {(status.data?.canRecord
            ? (["guide", "now", "recordings"] as const)
            : (["guide", "now"] as const)
          ).map((t) => (
            <button
              key={t}
              role="tab"
              aria-selected={tab === t}
              onClick={() => setTab(t)}
              className={clsx(
                "rounded-full px-4 py-1.5 text-sm font-medium",
                tab === t
                  ? "border border-text text-text"
                  : "text-muted hover:text-text",
              )}
            >
              {t === "guide"
                ? "Guide"
                : t === "now"
                  ? "What's On"
                  : "Recordings"}
            </button>
          ))}
        </div>
        <div
          className={clsx(
            "ml-auto w-48 shrink-0",
            tab === "recordings" && "invisible",
          )}
        >
          <Select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label="Channels to show"
          >
            <option value="all">All channels</option>
            <option value="favorites">Favorites</option>
            <option value="recent">Recently watched</option>
            <option value="hidden">Hidden channels</option>
            {groups.data?.map((g) => (
              <option key={g.name} value={g.name}>
                {g.name} ({g.channels})
              </option>
            ))}
          </Select>
        </div>
      </div>

      {current && tab !== "recordings" && (
        <div className="relative mx-auto aspect-video max-h-[42vh] overflow-hidden rounded-lg bg-black">
          <LivePlayer
            key={current.id}
            channelId={current.id}
            muted={muted}
            className="size-full"
            onError={setPreviewError}
          />
          <button
            className="absolute top-3 left-3 rounded-full bg-black/60 p-2"
            onClick={() => setMuted((m) => !m)}
            aria-label={muted ? "Unmute" : "Mute"}
          >
            {muted ? (
              <VolumeX className="size-5" />
            ) : (
              <Volume2 className="size-5" />
            )}
          </button>
          <button
            className="absolute top-3 right-3 rounded-full bg-black/60 p-2"
            onClick={() =>
              navigate({
                to: "/livetv/watch/$channelId",
                params: { channelId: String(current.id) },
              })
            }
            aria-label="Watch full screen"
          >
            <Maximize2 className="size-5" />
          </button>
          <div className="absolute inset-x-0 bottom-0 flex items-center gap-3 bg-gradient-to-t from-black/90 to-transparent px-4 py-3">
            <ChannelLogo channel={current} className="h-8 w-14" />
            <span className="truncate text-sm">
              {previewError ? (
                <span className="text-danger">{previewError}</span>
              ) : (
                <>Now On: {current.now?.title ?? current.name}</>
              )}
            </span>
          </div>
        </div>
      )}

      {tab === "recordings" ? (
        <Recordings />
      ) : channels.isPending ? (
        <Spinner />
      ) : !channels.data?.length ? (
        <p className="text-muted">
          {filter === "favorites"
            ? "No favourites yet. Tap the heart next to a channel."
            : filter === "recent"
              ? "Channels you watch will appear here."
              : filter === "hidden"
                ? "No hidden channels. Hide one from a programme's details in the guide."
                : "No channels."}
        </p>
      ) : tab === "guide" ? (
        <Guide
          channels={channels.data}
          filter={filter}
          selected={current?.id}
          onPick={setPreview}
          canRecord={!!status.data?.canRecord}
        />
      ) : (
        <WhatsOn
          channels={channels.data}
          onPick={(id) =>
            navigate({
              to: "/livetv/watch/$channelId",
              params: { channelId: String(id) },
            })
          }
        />
      )}
    </div>
  );
}

function localStorageGet(k: string) {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
}
function localStorageSet(k: string, v: string) {
  try {
    localStorage.setItem(k, v);
  } catch {
    /* private mode */
  }
}

export function ChannelLogo({
  channel,
  className,
}: {
  channel: LiveChannel;
  className?: string;
}) {
  const [broken, setBroken] = useState(false);
  if (!channel.logoUrl || broken)
    return (
      <div
        className={clsx(
          "grid place-items-center rounded bg-surface-2 text-[10px] font-semibold text-muted",
          className,
        )}
      >
        {channel.number ?? channel.name.slice(0, 4)}
      </div>
    );
  return (
    <img
      src={channel.logoUrl}
      alt=""
      onError={() => setBroken(true)}
      className={clsx("object-contain", className)}
    />
  );
}

function startOfHalfHour(d: Date) {
  const x = new Date(d);
  x.setMinutes(x.getMinutes() < 30 ? 0 : 30, 0, 0);
  return x;
}

function Guide({
  channels,
  filter,
  selected,
  onPick,
  canRecord,
}: {
  channels: LiveChannel[];
  filter: LiveFilter;
  selected?: number;
  onPick: (id: number) => void;
  canRecord: boolean;
}) {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(t);
  }, []);
  const [dayOffset, setDayOffset] = useState(0);
  const [shift, setShift] = useState(0); // half-hours from the default start
  const start = useMemo(() => {
    const base = startOfHalfHour(now);
    base.setMinutes(base.getMinutes() - 30 + shift * 30);
    if (dayOffset) base.setDate(base.getDate() + dayOffset);
    return base;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dayOffset, shift, Math.floor(now.getTime() / 1_800_000)]);
  const end = new Date(start.getTime() + WINDOW_HOURS * 3_600_000);
  const guide = useQuery(liveGuideQuery(filter, start, end));
  const rows = new Map(guide.data?.map((r) => [r.channelId, r.programmes]));
  const fav = useFavorite();
  const hide = useHideChannel();
  const [details, setDetails] = useState<{
    p: LiveProgramme;
    c: LiveChannel;
  } | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const x = (t: Date | string) =>
    ((new Date(t).getTime() - start.getTime()) / 60_000) * PX_PER_MIN;
  const width = WINDOW_HOURS * 60 * PX_PER_MIN;
  const ticks = Array.from(
    { length: WINDOW_HOURS * 2 },
    (_, i) => new Date(start.getTime() + i * 1_800_000),
  );
  const nowX = x(now);

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <div className="w-36 shrink-0">
          <Select
            value={dayOffset}
            onChange={(e) => (
              setDayOffset(Number(e.target.value)),
              setShift(0)
            )}
            aria-label="Day"
          >
            <option value={0}>Today</option>
            <option value={1}>Tomorrow</option>
          </Select>
        </div>
        <span className="text-sm text-muted">{timeLabel(start)}</span>
        <div className="ml-auto flex gap-2">
          <button
            className="rounded-full bg-surface-2 p-2 disabled:opacity-40"
            onClick={() => setShift((s) => s - 3)}
            disabled={dayOffset === 0 && shift <= -6}
            aria-label="Earlier"
          >
            <ChevronLeft className="size-4" />
          </button>
          <button
            className="rounded-full bg-surface-2 p-2"
            onClick={() => setShift((s) => s + 3)}
            aria-label="Later"
          >
            <ChevronRight className="size-4" />
          </button>
        </div>
      </div>
      <div
        ref={scroller}
        className="relative overflow-x-auto rounded-lg bg-surface"
        role="grid"
        aria-label="TV guide"
      >
        <div style={{ width: CHANNEL_COL + width }} className="relative">
          <div className="sticky top-0 z-10 flex h-9 border-b border-border bg-surface text-xs text-muted">
            <div
              style={{ width: CHANNEL_COL }}
              className="sticky left-0 z-20 shrink-0 bg-surface"
            />
            {ticks.map((t) => (
              <div
                key={t.getTime()}
                style={{ width: 30 * PX_PER_MIN }}
                className="shrink-0 border-l border-border/60 px-2 py-2"
              >
                {timeLabel(t)}
              </div>
            ))}
          </div>
          {channels.map((c) => (
            <div
              key={c.id}
              className="flex border-b border-border/60"
              style={{ height: ROW }}
              role="row"
            >
              <div
                style={{ width: CHANNEL_COL }}
                className={clsx(
                  "sticky left-0 z-10 flex shrink-0 items-center gap-2 bg-surface px-2",
                  c.id === selected && "ring-1 ring-inset ring-accent",
                )}
              >
                <button
                  onClick={() => onPick(c.id)}
                  className="flex min-w-0 flex-1 items-center gap-2"
                  aria-label={`Preview ${c.name}`}
                >
                  <ChannelLogo channel={c} className="h-9 w-14 shrink-0" />
                  <span className="text-xs text-faint">{c.number}</span>
                </button>
                {c.hidden ? (
                  <button
                    onClick={() => hide.mutate({ id: c.id, hidden: false })}
                    aria-label={`Show ${c.name} in the guide`}
                    className="p-1"
                  >
                    <Eye className="size-4 text-faint" />
                  </button>
                ) : (
                  <button
                    onClick={() => fav.mutate({ id: c.id, on: !c.favorite })}
                    aria-label={
                      c.favorite
                        ? `Remove ${c.name} from favourites`
                        : `Add ${c.name} to favourites`
                    }
                    className="p-1"
                  >
                    <Heart
                      className={clsx(
                        "size-4",
                        c.favorite ? "fill-accent text-accent" : "text-faint",
                      )}
                    />
                  </button>
                )}
              </div>
              <div className="relative shrink-0" style={{ width }}>
                {(rows.get(c.id) ?? []).map((p) => {
                  const left = Math.max(0, x(p.start));
                  const right = Math.min(width, x(p.end));
                  if (right <= left) return null;
                  const onAir =
                    new Date(p.start) <= now && new Date(p.end) > now;
                  return (
                    <button
                      key={p.id}
                      role="gridcell"
                      onClick={() =>
                        onAir && c.id !== selected
                          ? onPick(c.id)
                          : setDetails({ p, c })
                      }
                      style={{ left: left + 2, width: right - left - 4 }}
                      className={clsx(
                        "absolute top-1.5 bottom-1.5 overflow-hidden rounded-md px-3 py-1.5 text-left",
                        onAir
                          ? "border border-border bg-surface-3 hover:border-accent"
                          : "bg-surface-2 hover:bg-surface-3",
                      )}
                    >
                      <div className="flex items-center gap-1.5 truncate text-sm font-medium">
                        {p.recording && (
                          <span
                            className={clsx(
                              "size-2 shrink-0 rounded-full bg-red-500",
                              p.recording === "recording" && "animate-pulse",
                            )}
                            aria-label={
                              p.recording === "recording"
                                ? "Recording"
                                : "Will record"
                            }
                          />
                        )}
                        <span className="truncate">{p.title}</span>
                      </div>
                      <div className="truncate text-xs text-muted">
                        {onAir ? `${minutesLeft(p)}m left` : timeLabel(p.start)}
                      </div>
                    </button>
                  );
                })}
                {!guide.isPending && !(rows.get(c.id) ?? []).length && (
                  <div className="absolute inset-y-1.5 left-0.5 right-0.5 grid place-items-center rounded-md bg-surface-2/50 text-xs text-faint">
                    {c.name}
                  </div>
                )}
              </div>
            </div>
          ))}
          {nowX > 0 && nowX < width && (
            <div
              className="pointer-events-none absolute top-0 bottom-0 z-10 w-0.5 bg-accent"
              style={{ left: CHANNEL_COL + nowX }}
              aria-hidden
            >
              <div className="absolute -top-0 -left-1.5 size-0 border-x-[7px] border-t-[8px] border-x-transparent border-t-accent" />
            </div>
          )}
        </div>
      </div>
      {details && (
        <div
          className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4"
          onClick={() => setDetails(null)}
        >
          <div
            className="max-w-md space-y-2 rounded-lg bg-surface p-5"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-3">
              <ChannelLogo channel={details.c} className="h-8 w-14" />
              <span className="text-sm text-muted">{details.c.name}</span>
            </div>
            <h2 className="text-lg font-semibold">{details.p.title}</h2>
            <p className="text-sm text-muted">
              {new Date(details.p.start).toLocaleDateString([], {
                weekday: "long",
              })}{" "}
              {timeLabel(details.p.start)}–{timeLabel(details.p.end)}
              {details.p.episode && ` · ${details.p.episode}`}
              {details.p.subtitle && ` · ${details.p.subtitle}`}
            </p>
            {details.p.description && (
              <p className="text-sm">{details.p.description}</p>
            )}
            {canRecord && new Date(details.p.end) > now && (
              <RecordButtons
                programme={details.p}
                channel={details.c}
                onDone={() => setDetails(null)}
              />
            )}
            {!details.c.hidden && (
              <button
                className="pt-2 text-sm text-muted hover:text-text"
                onClick={() =>
                  hide.mutate(
                    { id: details.c.id, hidden: true },
                    { onSuccess: () => setDetails(null) },
                  )
                }
              >
                Hide {details.c.name} from the guide
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function WhatsOn({
  channels,
  onPick,
}: {
  channels: LiveChannel[];
  onPick: (id: number) => void;
}) {
  const now = Date.now();
  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3">
      {channels.map((c) => {
        const p = c.now;
        const pct = p
          ? Math.min(
              100,
              ((now - new Date(p.start).getTime()) /
                (new Date(p.end).getTime() - new Date(p.start).getTime())) *
                100,
            )
          : 0;
        return (
          <button
            key={c.id}
            onClick={() => onPick(c.id)}
            className="space-y-2 rounded-lg bg-surface p-3 text-left hover:bg-surface-2"
            aria-label={`Watch ${c.name}`}
          >
            <div className="flex items-center gap-3">
              <ChannelLogo channel={c} className="h-10 w-16" />
              <div className="min-w-0">
                <div className="truncate font-medium">{p?.title ?? c.name}</div>
                <div className="truncate text-xs text-muted">
                  {p
                    ? `${minutesLeft(p)}m left · ${c.name}`
                    : c.number
                      ? `Channel ${c.number}`
                      : ""}
                </div>
              </div>
            </div>
            {p && (
              <div className="h-1 overflow-hidden rounded-full bg-surface-3">
                <div
                  className="h-full bg-accent"
                  style={{ width: `${pct}%` }}
                />
              </div>
            )}
            {c.next && (
              <div className="truncate text-xs text-faint">
                Next: {c.next.title} at {timeLabel(c.next.start)}
              </div>
            )}
          </button>
        );
      })}
    </div>
  );
}

/** Full-screen live TV with channel up/down (LIVE-3). */
export function LiveWatchPage({ channelId }: { channelId: number }) {
  const channels = useQuery(liveChannelsQuery("all"));
  const navigate = useNavigate();
  const list = channels.data ?? [];
  const i = list.findIndex((c) => c.id === channelId);
  const c = list[i];
  const [error, setError] = useState<string | null>(null);
  const go = (d: number) => {
    if (!list.length) return;
    const next = list[(i + d + list.length) % list.length];
    if (!next) return;
    navigate({
      to: "/livetv/watch/$channelId",
      params: { channelId: String(next.id) },
      replace: true,
    });
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowUp" || e.key === "PageUp") go(-1);
      if (e.key === "ArrowDown" || e.key === "PageDown") go(1);
      if (e.key === "Escape") navigate({ to: "/livetv" });
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
  return (
    <div className="fixed inset-0 z-40 bg-black">
      <LivePlayer
        key={channelId}
        channelId={channelId}
        controls
        className="size-full"
        onError={setError}
      />
      <div className="absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent p-4">
        <Link
          to="/livetv"
          className="rounded-full bg-black/50 p-2"
          aria-label="Back to the guide"
        >
          <ChevronLeft className="size-5" />
        </Link>
        {c && <ChannelLogo channel={c} className="h-9 w-16" />}
        <div className="min-w-0">
          <div className="truncate font-semibold">
            {c?.now?.title ?? c?.name}
          </div>
          <div className="truncate text-xs text-muted">
            {c?.name}
            {c?.now && ` · ${timeLabel(c.now.start)}–${timeLabel(c.now.end)}`}
            {c?.next && ` · Next: ${c.next.title}`}
          </div>
        </div>
        <div className="ml-auto flex gap-2">
          <button
            className="rounded-full bg-black/50 px-3 py-2 text-sm"
            onClick={() => go(-1)}
            aria-label="Channel up"
          >
            CH ▲
          </button>
          <button
            className="rounded-full bg-black/50 px-3 py-2 text-sm"
            onClick={() => go(1)}
            aria-label="Channel down"
          >
            CH ▼
          </button>
        </div>
      </div>
      {error && (
        <div className="absolute inset-x-0 bottom-24 text-center text-danger">
          {error}
        </div>
      )}
    </div>
  );
}

/** Record, record the series, or cancel (LIVE-5). */
function RecordButtons({
  programme: p,
  channel: c,
  onDone,
}: {
  programme: LiveProgramme;
  channel: LiveChannel;
  onDone: () => void;
}) {
  const schedule = useSchedule();
  const cancel = useCancelRecording();
  const recordings = useQuery(recordingsQuery);
  const rec = recordings.data?.find(
    (r) =>
      r.channelId === c.id &&
      new Date(r.start).getTime() === new Date(p.start).getTime() &&
      (r.status === "scheduled" || r.status === "recording"),
  );
  const error = schedule.error ?? cancel.error;
  if (recordings.isPending) return <Spinner />;
  return (
    <div className="space-y-2 pt-2">
      {p.series && (
        <p className="text-xs text-muted">
          A series recording covers this title.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {rec ? (
          <Button
            variant="ghost"
            loading={cancel.isPending}
            onClick={() => cancel.mutate({ id: rec.id }, { onSuccess: onDone })}
          >
            {rec.status === "recording" ? "Stop recording" : "Don't record"}
          </Button>
        ) : (
          <Button
            variant="primary"
            loading={schedule.isPending && !schedule.variables?.series}
            onClick={() =>
              schedule.mutate(
                { channelId: c.id, start: p.start },
                { onSuccess: onDone },
              )
            }
          >
            Record
          </Button>
        )}
        {!p.series && (
          <Button
            variant="ghost"
            loading={schedule.isPending && !!schedule.variables?.series}
            onClick={() =>
              schedule.mutate(
                {
                  channelId: c.id,
                  start: p.start,
                  series: true,
                  anyChannel: true,
                },
                { onSuccess: onDone },
              )
            }
          >
            Record series
          </Button>
        )}
      </div>
      {error && <Alert tone="error">{error.message}</Alert>}
    </div>
  );
}

function when(r: Recording) {
  const d = new Date(r.start);
  return `${d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })} ${timeLabel(d)}–${timeLabel(r.end)}`;
}

/** The DVR: in progress, upcoming, series and finished recordings (LIVE-5). */
function Recordings() {
  const recordings = useQuery(recordingsQuery);
  const rules = useQuery(recordingRulesQuery);
  const cancel = useCancelRecording();
  const stopSeries = useDeleteRule();
  if (recordings.isPending) return <Spinner />;
  const list = recordings.data ?? [];
  const active = list.filter((r) => r.status === "recording");
  const upcoming = list.filter((r) => r.status === "scheduled");
  const done = list.filter(
    (r) => r.status === "completed" || r.status === "failed",
  );
  const row = (r: Recording, action: React.ReactNode) => (
    <li
      key={r.id}
      className="flex items-center gap-3 rounded-lg bg-surface px-4 py-3"
    >
      {r.status === "recording" && (
        <span className="size-2.5 shrink-0 animate-pulse rounded-full bg-red-500" />
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">
          {r.itemId ? (
            <Link
              to="/item/$itemId"
              params={{ itemId: String(r.itemId) }}
              className="hover:underline"
            >
              {r.title}
            </Link>
          ) : (
            r.title
          )}
          {r.series && (
            <span className="ml-2 text-xs font-normal text-muted">Series</span>
          )}
        </div>
        <div className="truncate text-sm text-muted">
          {[r.episode, r.subtitle].filter(Boolean).join(" · ")}
          {(r.episode || r.subtitle) && " · "}
          {r.channelName} · {when(r)}
          {r.sizeBytes ? ` · ${formatSize(r.sizeBytes)}` : ""}
        </div>
        {r.status === "failed" && (
          <div className="truncate text-sm text-danger">
            Failed: {r.error || "unknown error"}
          </div>
        )}
      </div>
      {action}
    </li>
  );
  const section = (title: string, items: React.ReactNode[]) =>
    items.length > 0 && (
      <section className="space-y-2">
        <h2 className="text-sm font-semibold tracking-wide text-muted uppercase">
          {title}
        </h2>
        <ul className="space-y-2">{items}</ul>
      </section>
    );
  if (!list.length && !rules.data?.length)
    return (
      <p className="text-muted">
        Nothing recorded yet. Pick a programme in the guide and choose Record.
      </p>
    );
  return (
    <div className="space-y-6">
      {cancel.error && <Alert tone="error">{cancel.error.message}</Alert>}
      {section(
        "Recording now",
        active.map((r) =>
          row(
            r,
            <Button variant="ghost" onClick={() => cancel.mutate({ id: r.id })}>
              Stop
            </Button>,
          ),
        ),
      )}
      {section(
        "Upcoming",
        upcoming.map((r) =>
          row(
            r,
            <Button variant="ghost" onClick={() => cancel.mutate({ id: r.id })}>
              Cancel
            </Button>,
          ),
        ),
      )}
      {section(
        "Series",
        (rules.data ?? []).map((s) => (
          <li
            key={s.id}
            className="flex items-center gap-3 rounded-lg bg-surface px-4 py-3"
          >
            <div className="min-w-0 flex-1">
              <div className="truncate font-medium">{s.title}</div>
              <div className="text-sm text-muted">
                {s.channelName || "Any channel"} · {s.upcoming} upcoming
              </div>
            </div>
            <Button variant="ghost" onClick={() => stopSeries.mutate(s.id)}>
              Stop series
            </Button>
          </li>
        )),
      )}
      {section(
        "Recorded",
        done.map((r) =>
          row(
            r,
            <Button
              variant="ghost"
              onClick={() => {
                if (
                  r.status === "failed" ||
                  window.confirm(`Delete the recording of ${r.title}?`)
                )
                  cancel.mutate({
                    id: r.id,
                    deleteFile: r.status !== "failed",
                  });
              }}
            >
              {r.status === "failed" ? "Dismiss" : "Delete"}
            </Button>,
          ),
        ),
      )}
    </div>
  );
}
