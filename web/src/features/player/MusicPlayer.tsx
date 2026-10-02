import { Pause, Play, SkipBack, SkipForward, Volume2, X } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { api, imageUrl, session as authSession, unwrap } from "@/api/client";
import type { ItemSummary } from "@/api/types";
import { deviceProfile } from "./deviceProfile";

type Ctx = {
  queue: ItemSummary[];
  index: number;
  playing: boolean;
  play: (tracks: ItemSummary[], start?: number) => void;
};

const MusicContext = createContext<Ctx | null>(null);

export function useMusic() {
  const ctx = useContext(MusicContext);
  if (!ctx) throw new Error("useMusic outside MusicProvider");
  return ctx;
}

function fmt(s: number) {
  if (!isFinite(s)) return "0:00";
  return `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
}

/** Plays music in a persistent bar at the bottom of the app, with a queue. */
export function MusicProvider({ children }: { children: ReactNode }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [queue, setQueue] = useState<ItemSummary[]>([]);
  const [index, setIndex] = useState(-1);
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const sessionId = useRef<string | null>(null);
  const track = index >= 0 ? queue[index] : undefined;

  const stopSession = useCallback(() => {
    const id = sessionId.current;
    sessionId.current = null;
    if (id) fetch(`/api/v1/playback/sessions/${id}`, { method: "DELETE", keepalive: true, headers: { Authorization: `Bearer ${authSession.token}` } }).catch(() => {});
  }, []);

  const report = useCallback((state: "playing" | "paused") => {
    const id = sessionId.current;
    const a = audio.current;
    if (id && a) api.PATCH("/playback/sessions/{sessionId}", { params: { path: { sessionId: id } }, body: { positionMs: Math.round(a.currentTime * 1000), state } }).catch(() => {});
  }, []);

  // Start a session for the current track.
  useEffect(() => {
    if (!track) return;
    let cancelled = false;
    (async () => {
      try {
        const s = await unwrap(api.POST("/playback/sessions", { body: { itemId: track.id, profile: deviceProfile(), startMs: 0 } }));
        if (cancelled) return;
        stopSession();
        sessionId.current = s.id;
        const a = audio.current!;
        a.src = s.url;
        await a.play();
      } catch {
        if (!cancelled) setIndex((i) => (i + 1 < queue.length ? i + 1 : i)); // skip unplayable tracks
      }
    })();
    if ("mediaSession" in navigator) {
      navigator.mediaSession.metadata = new MediaMetadata({
        title: track.title,
        artist: track.artistCredit ?? track.grandparentTitle ?? "",
        album: track.parentTitle ?? "",
        artwork: track.images?.poster ? [{ src: imageUrl(track.images.poster, 256), sizes: "512x512" }] : [],
      });
    }
    return () => {
      cancelled = true;
    };
  }, [track, queue.length, stopSession]);

  useEffect(() => {
    const t = window.setInterval(() => audio.current && !audio.current.paused && report("playing"), 15_000);
    window.addEventListener("pagehide", stopSession);
    return () => {
      window.clearInterval(t);
      window.removeEventListener("pagehide", stopSession);
    };
  }, [report, stopSession]);

  const play = useCallback((tracks: ItemSummary[], start = 0) => {
    setQueue(tracks);
    setIndex(start);
  }, []);
  const next = useCallback(() => setIndex((i) => (i + 1 < queue.length ? i + 1 : i)), [queue.length]);
  const prev = useCallback(() => {
    const a = audio.current;
    if (a && a.currentTime > 3) a.currentTime = 0;
    else setIndex((i) => Math.max(0, i - 1));
  }, []);
  const toggle = () => {
    const a = audio.current;
    if (!a) return;
    if (a.paused) a.play();
    else a.pause();
  };
  const close = () => {
    audio.current?.pause();
    report("paused");
    stopSession();
    setQueue([]);
    setIndex(-1);
  };

  useEffect(() => {
    if (!("mediaSession" in navigator)) return;
    navigator.mediaSession.setActionHandler("nexttrack", next);
    navigator.mediaSession.setActionHandler("previoustrack", prev);
    navigator.mediaSession.setActionHandler("play", () => audio.current?.play());
    navigator.mediaSession.setActionHandler("pause", () => audio.current?.pause());
  }, [next, prev]);

  return (
    <MusicContext.Provider value={{ queue, index, playing, play }}>
      {children}
      <audio
        ref={audio}
        onPlay={() => {
          setPlaying(true);
          report("playing");
        }}
        onPause={() => {
          setPlaying(false);
          report("paused");
        }}
        onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
        onDurationChange={(e) => setDuration(e.currentTarget.duration)}
        onEnded={() => {
          report("paused");
          if (index + 1 < queue.length) next();
          else setPlaying(false);
        }}
      />
      {track && (
        <div className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface/95 backdrop-blur">
          <div
            className="h-1 cursor-pointer bg-surface-3"
            onClick={(e) => {
              const r = e.currentTarget.getBoundingClientRect();
              const d = isFinite(duration) && duration > 0 ? duration : (track.durationMs ?? 0) / 1000;
              if (audio.current) audio.current.currentTime = ((e.clientX - r.left) / r.width) * d;
            }}
          >
            <div className="h-full bg-accent" style={{ width: `${(time / (isFinite(duration) && duration > 0 ? duration : (track.durationMs ?? 1) / 1000)) * 100}%` }} />
          </div>
          <div className="flex items-center gap-4 px-4 py-2">
            <div className="size-12 shrink-0 overflow-hidden rounded bg-surface-3">{track.images?.poster && <img src={imageUrl(track.images.poster, 48)} alt="" className="size-full object-cover" />}</div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{track.title}</div>
              <div className="truncate text-xs text-muted">
                {track.artistCredit ?? track.grandparentTitle} · {track.parentTitle}
              </div>
            </div>
            <span className="hidden text-xs text-muted tabular-nums sm:inline">
              {fmt(time)} / {fmt(isFinite(duration) && duration > 0 ? duration : (track.durationMs ?? 0) / 1000)}
            </span>
            <button onClick={prev} className="rounded-full p-2 hover:bg-surface-2" aria-label="Previous track">
              <SkipBack className="size-5" />
            </button>
            <button onClick={toggle} className="rounded-full bg-accent p-2.5 text-black" aria-label={playing ? "Pause" : "Play"}>
              {playing ? <Pause className="size-5 fill-current" /> : <Play className="size-5 fill-current" />}
            </button>
            <button onClick={next} disabled={index + 1 >= queue.length} className="rounded-full p-2 hover:bg-surface-2 disabled:opacity-40" aria-label="Next track">
              <SkipForward className="size-5" />
            </button>
            <Volume2 className="hidden size-4 text-muted md:block" aria-hidden />
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              defaultValue={1}
              aria-label="Volume"
              className="hidden w-24 accent-[var(--color-accent)] md:block"
              onChange={(e) => audio.current && (audio.current.volume = Number(e.target.value))}
            />
            <span className="hidden text-xs text-faint lg:inline">
              {index + 1} / {queue.length}
            </span>
            <button onClick={close} className="rounded-full p-2 text-muted hover:bg-surface-2" aria-label="Close player">
              <X className="size-4" />
            </button>
          </div>
        </div>
      )}
    </MusicContext.Provider>
  );
}
