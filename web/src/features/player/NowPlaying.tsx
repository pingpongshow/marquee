import { Link } from "@tanstack/react-router";
import { clsx } from "clsx";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  GripVertical,
  ListMusic,
  ListPlus,
  Maximize2,
  MicVocal,
  Moon,
  Pause,
  Play,
  Repeat,
  Repeat1,
  Shuffle,
  SkipBack,
  SlidersVertical,
  SkipForward,
  Volume2,
  X,
} from "lucide-react";
import { useEffect, useState } from "react";
import { api, imageUrl, unwrap } from "@/api/client";
import { Menu, MenuItem } from "@/components/Menu";
import { Rating } from "../music/Rating";
import type { ItemSummary } from "@/api/types";
import { ignoreShortcut } from "@/lib/keys";
import {
  useMusic,
  useMusicActions,
  useMusicState,
  type DJMode,
} from "./MusicPlayer";
import type { Entry } from "./queue";
import { EqualizerSheet } from "./Equalizer";
import { audioQualityLabel, isHiRes, setShowAudioQuality, streamedLabel, useShowAudioQuality } from "./audioQuality";
import { PlayOnButton } from "../remote/PlayOn";
import { queueTitle, SaveAsPlaylistDialog } from "../playlists/SaveAsPlaylist";

export function fmtTime(s: number) {
  if (!isFinite(s) || s < 0) return "0:00";
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(Math.floor(s % 60)).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

const artist = (t: ItemSummary) => t.artistCredit ?? t.grandparentTitle ?? "";

/** Average colour of an artwork image, for tinting the Now Playing background. */
function useArtworkColor(artworkId?: number) {
  const [color, setColor] = useState<string | null>(null);
  useEffect(() => {
    if (!artworkId) return;
    let cancelled = false;
    const img = new Image();
    img.onload = () => {
      if (cancelled) return;
      const c = document.createElement("canvas");
      c.width = c.height = 16;
      const ctx = c.getContext("2d");
      if (!ctx) return;
      ctx.drawImage(img, 0, 0, 16, 16);
      const d = ctx.getImageData(0, 0, 16, 16).data;
      let r = 0,
        g = 0,
        b = 0,
        n = 0;
      for (let i = 0; i < d.length; i += 4) {
        // Skip near-black and near-white pixels so borders don't wash the colour out.
        const [pr = 0, pg = 0, pb = 0] = [d[i], d[i + 1], d[i + 2]];
        const sum = pr + pg + pb;
        if (sum < 60 || sum > 720) continue;
        r += pr;
        g += pg;
        b += pb;
        n++;
      }
      if (n)
        setColor(
          `rgb(${Math.round(r / n)} ${Math.round(g / n)} ${Math.round(b / n)})`,
        );
    };
    img.src = imageUrl(artworkId, 32);
    return () => {
      cancelled = true;
    };
  }, [artworkId]);
  return artworkId ? color : null;
}

function Scrubber({ className }: { className?: string }) {
  const m = useMusic();
  const d =
    isFinite(m.duration) && m.duration > 0
      ? m.duration
      : (m.current?.item.durationMs ?? 0) / 1000;
  return (
    <input
      type="range"
      min={0}
      max={d || 1}
      step={0.5}
      value={Math.min(m.time, d || 1)}
      onChange={(e) => m.seek(Number(e.target.value))}
      aria-label="Seek"
      className={clsx("w-full accent-[var(--color-accent)]", className)}
    />
  );
}

function Controls({ large }: { large?: boolean }) {
  const m = useMusicState();
  const btn = large
    ? "rounded-full p-3 hover:bg-white/10"
    : "rounded-full p-2 hover:bg-surface-2";
  const icon = large ? "size-7" : "size-5";
  const RepeatIcon = m.queue.repeat === "one" ? Repeat1 : Repeat;
  return (
    <div className={clsx("flex items-center", large ? "gap-4" : "gap-1")}>
      <button
        onClick={m.toggleShuffle}
        className={clsx(btn, m.queue.shuffled ? "text-accent" : "opacity-70")}
        aria-label="Shuffle"
        aria-pressed={m.queue.shuffled}
      >
        <Shuffle className={large ? "size-5" : "size-4"} />
      </button>
      <button onClick={m.prev} className={btn} aria-label="Previous track">
        <SkipBack className={clsx(icon, "fill-current")} />
      </button>
      <button
        onClick={m.toggle}
        className={clsx(
          "rounded-full bg-accent text-black",
          large ? "p-4" : "p-2.5",
        )}
        aria-label={m.playing ? "Pause" : "Play"}
      >
        {m.playing ? (
          <Pause className={clsx(icon, "fill-current")} />
        ) : (
          <Play className={clsx(icon, "fill-current")} />
        )}
      </button>
      <button onClick={m.next} className={btn} aria-label="Next track">
        <SkipForward className={clsx(icon, "fill-current")} />
      </button>
      <button
        onClick={m.cycleRepeat}
        className={clsx(
          btn,
          m.queue.repeat !== "off" ? "text-accent" : "opacity-70",
        )}
        aria-label={`Repeat: ${m.queue.repeat}`}
      >
        <RepeatIcon className={large ? "size-5" : "size-4"} />
      </button>
    </div>
  );
}

function Volume({ className }: { className?: string }) {
  const m = useMusicState();
  return (
    <div className={clsx("items-center gap-2", className)}>
      <Volume2 className="size-4 opacity-70" aria-hidden />
      <input
        type="range"
        min={0}
        max={1}
        step={0.02}
        value={m.volume}
        aria-label="Volume"
        className="w-24 accent-[var(--color-accent)]"
        onChange={(e) => m.setVolume(Number(e.target.value))}
      />
    </div>
  );
}

function Art({
  item,
  size,
  className,
}: {
  item: ItemSummary;
  size: number;
  className?: string;
}) {
  return (
    <div
      className={clsx(
        "shrink-0 overflow-hidden rounded bg-surface-3",
        className,
      )}
    >
      {item.images?.poster && (
        <img
          src={imageUrl(item.images.poster, size)}
          alt=""
          className="size-full object-cover"
        />
      )}
    </div>
  );
}

/** The persistent bar at the bottom of the app. */
export function MiniPlayer() {
  const m = useMusic();
  const t = m.current!.item;
  const [queueOpen, setQueueOpen] = useState(false);
  return (
    <div className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface/95 backdrop-blur">
      <Scrubber className="-mt-2 block h-2" />
      <div className="flex items-center gap-3 px-3 pb-2 sm:gap-4 sm:px-4">
        <button
          onClick={() => m.setExpanded(true)}
          className="flex min-w-0 flex-1 items-center gap-3 text-left"
          aria-label="Open Now Playing"
        >
          <Art item={t} size={48} className="size-12" />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{t.title}</div>
            <div className="truncate text-xs text-muted">
              {artist(t)}
              {t.parentTitle ? ` · ${t.parentTitle}` : ""}
            </div>
          </div>
        </button>
        <span className="hidden text-xs text-muted tabular-nums md:inline">
          {fmtTime(m.time)} /{" "}
          {fmtTime(
            isFinite(m.duration) ? m.duration : (t.durationMs ?? 0) / 1000,
          )}
        </span>
        <div className="hidden sm:block">
          <Controls />
        </div>
        <button
          onClick={m.toggle}
          className="rounded-full bg-accent p-2.5 text-black sm:hidden"
          aria-label={m.playing ? "Pause" : "Play"}
        >
          {m.playing ? (
            <Pause className="size-5 fill-current" />
          ) : (
            <Play className="size-5 fill-current" />
          )}
        </button>
        <Volume className="hidden lg:flex" />
        <button
          onClick={() => setQueueOpen((v) => !v)}
          className={clsx(
            "hidden rounded-full p-2 hover:bg-surface-2 sm:block",
            queueOpen && "text-accent",
          )}
          aria-label="Queue"
          aria-expanded={queueOpen}
        >
          <ListMusic className="size-5" />
        </button>
        <button
          onClick={() => m.setExpanded(true)}
          className="hidden rounded-full p-2 hover:bg-surface-2 sm:block"
          aria-label="Full screen player"
        >
          <Maximize2 className="size-4" />
        </button>
        <button
          onClick={m.close}
          className="rounded-full p-2 text-muted hover:bg-surface-2"
          aria-label="Close player"
        >
          <X className="size-4" />
        </button>
      </div>
      {queueOpen && (
        <div className="absolute right-2 bottom-full mb-2 flex max-h-[70vh] w-96 max-w-[calc(100vw-1rem)] flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-2xl">
          <QueueList />
        </div>
      )}
    </div>
  );
}

/** The track's audio quality, and what's streamed when it isn't the original (MUSIC-23). */
function QualityBadge({ item }: { item: ItemSummary }) {
  const show = useShowAudioQuality();
  const m = useMusicState();
  const f = item.audioFormat;
  if (!show || !f) return null;
  const streamed = streamedLabel(m.stream);
  const text = audioQualityLabel(f) + (streamed ? ` → ${streamed}` : "");
  return (
    <div className="mt-2 flex min-w-0 items-center justify-center gap-1.5 text-xs text-white/60" data-testid="audio-quality">
      {isHiRes(f) && <span className="shrink-0 rounded border border-accent/60 px-1 text-[10px] font-semibold text-accent">Hi-Res</span>}
      <span className="truncate" title={text}>
        {text}
      </span>
    </div>
  );
}

function QualityToggle() {
  const show = useShowAudioQuality();
  return (
    <label className="flex cursor-pointer items-center gap-1.5 text-xs text-white/80">
      <input type="checkbox" checked={show} onChange={(e) => setShowAudioQuality(e.target.checked)} className="accent-current" />
      Show audio quality
    </label>
  );
}

/** Saves what's playing (the station, mix or queue: played and upcoming tracks) as a playlist. */
function SaveQueueButton({ label, className, iconClass }: { label: string; className: string; iconClass: string }) {
  const m = useMusicState();
  const [open, setOpen] = useState(false);
  if (!m.queue.entries.length) return null;
  return (
    <>
      <button onClick={() => setOpen(true)} className={className} aria-label={label} title={label}>
        <ListPlus className={iconClass} />
      </button>
      {open && (
        <SaveAsPlaylistDialog
          defaultTitle={m.source?.title || queueTitle()}
          itemIds={m.queue.entries.map((e) => e.item.id)}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}

/** Up next, with drag to reorder, click to jump and remove. */
export function QueueList({ dark }: { dark?: boolean }) {
  const m = useMusicState();
  const [dragKey, setDragKey] = useState<number | null>(null);
  const [overIndex, setOverIndex] = useState<number | null>(null);
  const upcoming = m.queue.entries.slice(m.queue.index + 1);
  const row = (e: Entry, i: number, isCurrent: boolean) => (
    <li
      key={e.key}
      draggable={!isCurrent}
      onDragStart={() => setDragKey(e.key)}
      onDragOver={(ev) => {
        ev.preventDefault();
        setOverIndex(i);
      }}
      onDragEnd={() => {
        setDragKey(null);
        setOverIndex(null);
      }}
      onDrop={(ev) => {
        ev.preventDefault();
        if (dragKey !== null) m.move(dragKey, i);
        setDragKey(null);
        setOverIndex(null);
      }}
      className={clsx(
        "group flex items-center gap-3 px-3 py-2",
        dark ? "hover:bg-white/10" : "hover:bg-surface-2",
        isCurrent && "text-accent",
        overIndex === i && dragKey !== null && "border-t-2 border-accent",
        dragKey === e.key && "opacity-40",
      )}
    >
      {!isCurrent && (
        <GripVertical
          className="size-4 shrink-0 cursor-grab opacity-40"
          aria-hidden
        />
      )}
      <button
        onClick={() => m.jump(e.key)}
        className="flex min-w-0 flex-1 items-center gap-3 text-left"
      >
        <Art item={e.item} size={40} className="size-10" />
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 truncate text-sm">
            {e.dj && (
              <span
                className="shrink-0 rounded bg-accent/20 px-1 text-[10px] font-semibold text-accent"
                title={djLabels[e.dj as DJMode]}
              >
                DJ
              </span>
            )}
            <span className="truncate">{e.item.title}</span>
          </div>
          <div
            className={clsx(
              "truncate text-xs",
              dark ? "text-white/60" : "text-muted",
            )}
          >
            {artist(e.item)}
          </div>
        </div>
      </button>
      {!isCurrent && (
        <button
          onClick={() => m.remove(e.key)}
          className="rounded p-1 opacity-0 group-hover:opacity-70 hover:opacity-100 focus:opacity-100"
          aria-label={`Remove ${e.item.title} from queue`}
        >
          <X className="size-4" />
        </button>
      )}
    </li>
  );
  return (
    <>
      <div
        className={clsx(
          "flex items-center justify-between px-3 py-2 text-xs font-semibold tracking-wider uppercase",
          dark ? "text-white/60" : "text-faint",
        )}
      >
        <span>Playing from queue</span>
        <span className="flex items-center gap-2">
          {m.queue.index + 1} / {m.queue.entries.length}
          <SaveQueueButton label="Save queue as playlist" className={clsx("rounded p-1", dark ? "hover:bg-white/10" : "hover:bg-surface-2")} iconClass="size-4" />
        </span>
      </div>
      <ul className="flex-1 overflow-y-auto pb-2">
        {m.current && row(m.current, m.queue.index, true)}
        {upcoming.length > 0 && (
          <li
            className={clsx(
              "px-3 pt-3 pb-1 text-xs",
              dark ? "text-white/60" : "text-faint",
            )}
          >
            Up next
          </li>
        )}
        {upcoming.map((e, i) => row(e, m.queue.index + 1 + i, false))}
      </ul>
    </>
  );
}

/** Full-screen Now Playing, tinted with the artwork's colour. */
export function NowPlaying() {
  const m = useMusic();
  const t = m.current!.item;
  const color = useArtworkColor(t.images?.poster);
  const [panel, setPanel] = useState<"queue" | "lyrics" | null>(() =>
    window.matchMedia("(min-width: 1024px)").matches ? "queue" : null,
  );
  const [eqOpen, setEqOpen] = useState(false);
  const d =
    isFinite(m.duration) && m.duration > 0
      ? m.duration
      : (t.durationMs ?? 0) / 1000;

  // Actions are stable, so this subscribes once rather than on every playhead tick.
  const { setExpanded, toggle, next, prev } = useMusicActions();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (ignoreShortcut(e)) return;
      if (e.key === "Escape") setExpanded(false);
      else if (e.key === " ") {
        e.preventDefault();
        toggle();
      } else if (e.key === "ArrowRight" && e.shiftKey) next();
      else if (e.key === "ArrowLeft" && e.shiftKey) prev();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setExpanded, toggle, next, prev]);

  return (
    <div
      className="fixed inset-0 z-50 flex flex-col text-white transition-[background] duration-700"
      style={{
        background: `linear-gradient(160deg, ${color ?? "#3a3a44"} 0%, #0b0b0f 75%)`,
      }}
      role="dialog"
      aria-label="Now Playing"
    >
      <header className="flex items-center gap-2 p-4">
        <button
          onClick={() => m.setExpanded(false)}
          className="rounded-full p-2 hover:bg-white/10"
          aria-label="Close Now Playing"
        >
          <ChevronDown className="size-6" />
        </button>
        <div className="min-w-0 flex-1 text-center">
          <div className="text-xs font-semibold tracking-wider text-white/70 uppercase">
            {m.source ? "Playing from" : "Now Playing"}
          </div>
          {m.source && (
            <div className="truncate text-sm font-medium">{m.source.title}</div>
          )}
        </div>
        <SaveQueueButton label="Save as playlist" className="rounded-full p-2 hover:bg-white/10" iconClass="size-5" />
        <SleepMenu />
        <PlayOnButton
          itemIds={m.queue.entries.map((e) => e.item.id)}
          index={m.queue.index}
          startMs={() => Math.round(m.time * 1000)}
          onHandoff={() => {
            if (m.playing) m.toggle();
            m.setExpanded(false);
          }}
          className="rounded-full p-2 hover:bg-white/10"
        />
        <button
          onClick={() => setEqOpen(true)}
          className={clsx(
            "rounded-full p-2 hover:bg-white/10",
            m.eq.enabled && "text-accent",
          )}
          aria-label="Equaliser"
          title="Equaliser"
        >
          <SlidersVertical className="size-5" />
        </button>
        <button
          onClick={() => setPanel((p) => (p === "lyrics" ? null : "lyrics"))}
          className={clsx(
            "rounded-full p-2 hover:bg-white/10",
            panel === "lyrics" && "text-accent",
          )}
          aria-label="Lyrics"
          aria-pressed={panel === "lyrics"}
        >
          <MicVocal className="size-5" />
        </button>
        <button
          onClick={() => setPanel((p) => (p === "queue" ? null : "queue"))}
          className={clsx(
            "rounded-full p-2 hover:bg-white/10",
            panel === "queue" && "text-accent",
          )}
          aria-label="Queue"
          aria-pressed={panel === "queue"}
        >
          <ListMusic className="size-5" />
        </button>
      </header>
      <div className="flex min-h-0 flex-1 gap-8 px-6 pb-8 lg:px-12">
        <div
          className={clsx(
            "flex min-h-0 min-w-0 flex-1 flex-col items-center justify-center-safe gap-6 overflow-y-auto",
            panel && "max-lg:hidden",
          )}
        >
          <Art
            item={t}
            size={640}
            className="aspect-square w-full max-w-[max(8rem,min(32rem,calc(100dvh-24rem)))] shadow-2xl"
          />
          <div className="w-full max-w-[32rem] text-center">
            <div className="truncate text-2xl font-bold">{t.title}</div>
            <div className="mt-1 truncate text-white/70">
              {t.grandparentId ? (
                <Link
                  to="/item/$itemId"
                  params={{ itemId: String(t.grandparentId) }}
                  onClick={() => m.setExpanded(false)}
                  className="hover:underline"
                >
                  {artist(t)}
                </Link>
              ) : (
                artist(t)
              )}
              {t.parentTitle && t.parentId && (
                <>
                  {" · "}
                  <Link
                    to="/item/$itemId"
                    params={{ itemId: String(t.parentId) }}
                    onClick={() => m.setExpanded(false)}
                    className="hover:underline"
                  >
                    {t.parentTitle}
                  </Link>
                </>
              )}
            </div>
            <QualityBadge item={t} />
          </div>
          <div className="w-full max-w-[32rem]">
            <Scrubber />
            <div className="flex justify-between text-xs text-white/60 tabular-nums">
              <span>{fmtTime(m.time)}</span>
              <span>-{fmtTime(d - m.time)}</span>
            </div>
          </div>
          <Controls large />
          <div className="flex flex-wrap items-center justify-center gap-6">
            <Rating key={t.id} itemId={t.id} value={t.userRating} />
            <Volume className="flex" />
            <CrossfadeSelect />
            <DJSelect />
            <QualityToggle />
          </div>
        </div>
        {panel && (
          <aside className="flex w-full flex-col overflow-hidden rounded-xl bg-black/30 lg:w-[28rem]">
            {panel === "queue" ? <QueueList dark /> : <LyricsPanel item={t} />}
          </aside>
        )}
      </div>
      {eqOpen && <EqualizerSheet onClose={() => setEqOpen(false)} />}
    </div>
  );
}

/** Synced lyrics follow the music (the current line is highlighted and kept in view); plain lyrics just scroll. */
function LyricsPanel({ item }: { item: ItemSummary }) {
  const m = useMusic();
  const lyrics = useQuery({
    queryKey: ["lyrics", item.id],
    queryFn: () =>
      unwrap(
        api.GET("/items/{itemId}/lyrics", {
          params: { path: { itemId: item.id } },
        }),
      ),
    retry: false,
    staleTime: Infinity,
  });
  const ms = m.time * 1000;
  const lines = lyrics.data?.lines ?? [];
  let current = -1;
  if (lyrics.data?.synced) {
    for (let i = 0; i < lines.length; i++)
      if ((lines[i]!.timeMs ?? 0) <= ms + 250) current = i;
  }
  useEffect(() => {
    if (current >= 0)
      document
        .getElementById(`lyric-${current}`)
        ?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [current]);
  if (lyrics.isPending)
    return <p className="p-6 text-white/60">Looking for lyrics…</p>;
  if (lyrics.isError || !lines.length)
    return <p className="p-6 text-white/60">No lyrics for this track.</p>;
  return (
    <div className="flex-1 overflow-y-auto px-6 py-10">
      {lines.map((l, i) => (
        <p
          key={i}
          id={`lyric-${i}`}
          onClick={() => l.timeMs !== undefined && m.seek(l.timeMs / 1000)}
          className={clsx(
            "py-1.5 text-xl leading-snug font-bold transition-colors",
            lyrics.data!.synced
              ? i === current
                ? "text-white"
                : "cursor-pointer text-white/35 hover:text-white/60"
              : "text-white/85",
            !l.text && "h-4",
          )}
        >
          {l.text}
        </p>
      ))}
      <p className="mt-6 text-xs text-white/40">
        Lyrics:{" "}
        {lyrics.data!.source === "lrclib"
          ? "LRCLIB"
          : lyrics.data!.source === "sidecar"
            ? ".lrc file"
            : "embedded in the file"}
      </p>
    </div>
  );
}

const sleepOptions = [15, 30, 45, 60, 90];

function SleepMenu() {
  const m = useMusicState();
  const active = m.sleep !== null;
  return (
    <Menu
      label="Sleep timer"
      trigger={
        <Moon
          className={clsx("size-5", active ? "text-accent" : "text-white")}
        />
      }
    >
      {active && (
        <MenuItem onClick={() => m.setSleep(null)}>
          Cancel sleep timer ({m.sleep === "track" ? "end of track" : "on"})
        </MenuItem>
      )}
      <MenuItem onClick={() => m.setSleep("track")}>
        At the end of this track
      </MenuItem>
      {sleepOptions.map((n) => (
        <MenuItem key={n} onClick={() => m.setSleep(Date.now() + n * 60_000)}>
          In {n} minutes
        </MenuItem>
      ))}
    </Menu>
  );
}


const selectCls =
  "rounded-md border border-white/20 bg-black/30 px-2 py-1 text-xs text-white/80";

function CrossfadeSelect() {
  const m = useMusicState();
  return (
    <select
      value={m.crossfade}
      onChange={(e) => m.setCrossfade(Number(e.target.value))}
      aria-label="Crossfade"
      className={selectCls}
      title="Albums played in order stay gapless"
    >
      {[0, 2, 4, 6, 8, 12].map((s) => (
        <option key={s} value={s}>
          {s ? `Crossfade: ${s}s` : "Crossfade: off"}
        </option>
      ))}
    </select>
  );
}

export const djLabels: Record<DJMode, string> = {
  wander: "DJ: Wander",
  superfan: "DJ: Superfan",
  deep_cuts: "DJ: Deep Cuts",
  same_era: "DJ: Same Era",
};
const djHelp: Record<DJMode, string> = {
  wander: "A similar sound, by other artists",
  superfan: "More from the same artists",
  deep_cuts: "The artist's tracks you play least",
  same_era: "A similar sound from the same years",
};

function DJSelect() {
  const m = useMusicState();
  return (
    <select
      value={m.dj ?? ""}
      onChange={(e) => m.setDJ((e.target.value || null) as DJMode | null)}
      aria-label="DJ"
      className={selectCls}
      title={m.dj ? djHelp[m.dj] : "A DJ weaves a track in every few songs"}
    >
      <option value="">DJ: off</option>
      {(Object.keys(djLabels) as DJMode[]).map((k) => (
        <option key={k} value={k}>
          {djLabels[k]}
        </option>
      ))}
    </select>
  );
}

